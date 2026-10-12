// Public rendering API: Document/Node → SVG string.
//
// This file is intentionally small. It holds only the four exported
// entry points and the renderComposite orchestrator that calls the
// lowering pass (lower.go) and the SVG layer injectors (inject.go).
//
// Anything below the API in the stack lives in dedicated files:
//
//	lower.go      composite/group/stack expansion
//	inject.go     canvas-bg / screen-label / connector / annotation injectors
//	svgutil.go    SVG-string parse/edit helpers used by injectors
package isotopo

import (
	"math"
	"sort"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func Render(n *Node, theme *Theme) string {
	return RenderWithCanvas(n, theme, nil, nil)
}

// RenderWithCanvas is Render plus document-level canvas + annotations:
// the canvas controls the iso-aware background (ground grid / dots /
// hatch); the annotations are screen-space callouts pinned to specific
// composite parts via leader lines. Both layers are optional — passing
// nil produces exactly what Render does.
func RenderWithCanvas(n *Node, theme *Theme, canvas *Canvas, anns []*Annotation) string {
	if n == nil {
		return ""
	}
	// v0.8 — flat top-down plan view is a separate renderer (planview.go);
	// it shares the layout solver and style cascade but never touches the
	// isometric path, so iso output is byte-for-byte unchanged.
	if canvas != nil && canvas.Projection == "top" {
		return RenderPlan(n, theme, canvas, anns)
	}
	if n.Shape == "composite" {
		return renderComposite(n, theme, canvas, anns, nil)
	}
	shape, opts := Flatten(n, theme)
	// Same integral-dimension contract as the composite path (v3.0):
	// fractional root width/height grows scrollbars in 1:1 captures.
	return ceilOuterDims(iso25d.Convert2DTo25D(shape, opts))
}

// RenderWithRouteInfo is RenderWithCanvas plus WS-E's in-memory record of
// every rendered connector's pre-clip tracks and final submitted path/opaque body coverage (see
// ConnectorRoute in routecollect.go). The SVG string is byte-for-byte
// identical to RenderWithCanvas — collection is pure observation — so this
// is the structured replacement for regexing data-route out of the output.
// Non-composite nodes and the top-down plan projection carry no iso
// connector layer and return nil routes.
func RenderWithRouteInfo(n *Node, theme *Theme, canvas *Canvas, anns []*Annotation) (string, []ConnectorRoute) {
	return renderWithRouteInfo(n, theme, canvas, anns, false)
}

func renderWithRouteInfo(n *Node, theme *Theme, canvas *Canvas, anns []*Annotation, preClipOnly bool) (string, []ConnectorRoute) {
	if n == nil {
		return "", nil
	}
	if n.Shape == "composite" && (canvas == nil || canvas.Projection != "top") {
		rc := &routeCollector{preClipOnly: preClipOnly}
		svg := renderComposite(n, theme, canvas, anns, rc)
		return svg, rc.routes
	}
	return RenderWithCanvas(n, theme, canvas, anns), nil
}

// renderComposite walks a composite node's parts, calls the lowering
// pass to expand groups and stacks, delegates the iso geometry to
// iso25d.RenderComposite, then layers in canvas / connectors / screen
// labels / annotations through the injectors in inject.go. rc, when
// non-nil, collects each connector's real route in memory (WS-E) and is
// forbidden from altering the output bytes.
func renderComposite(n *Node, theme *Theme, canvas *Canvas, anns []*Annotation, rc *routeCollector) string {
	return renderCompositeWithVisibility(n, theme, canvas, anns, rc, false)
}

// renderCompositeWithVisibility keeps the depth compositor under an explicit,
// per-render internal experiment. Public rendering preserves the accepted
// compositor until port semantics, mixed-shape locality and body order pass
// independent verification. Never use a process-global flag for this choice.
func renderCompositeWithVisibility(n *Node, theme *Theme, canvas *Canvas, anns []*Annotation, rc *routeCollector, spatialVisibility bool) string {
	// v2.2 — solve layout/place declarations into concrete Offsets
	// before lowering. Issues are render-time-silent; Validate surfaces
	// the same list to agents via layoutIssues.
	n = resolvedLayout(n, canvas)
	flat := lowerCompositeParts(n.Parts, 0, 0, 0, n.GridStep)
	if len(flat) == 0 {
		return ""
	}
	flat, nSubstrates := orderCompositeParts(flat, canvas, n.GridStep)
	flat = resolveGroupLabelOcclusion(flat, theme)
	depthSorted := canvas != nil && canvas.DepthSort

	infos := make([]partInfo, len(flat))
	glowPad := 0.0

	parts := make([]iso25d.CompositePart, 0, len(flat))
	for i, p := range flat {
		// v1.6 — screen-orient labels are rendered separately below as
		// SVG-screen-space boxes. Strip Label from the iso flatten so the
		// shape doesn't double-print it on the top face. We must look at
		// the *merged* style (theme → per-shape → part) so theme-level
		// `text.orient: screen` propagates to every part.
		mergedForLabel := ResolveStyleWithRole(theme, p.Shape, p.Role, p.Preset, p.Style)
		if mergedForLabel != nil && mergedForLabel.Effects != nil && mergedForLabel.Effects.Backglow != nil {
			// Halo blur extends well past the silhouette; reserve room so
			// the glow gradient never terminates at the canvas edge.
			glowPad = 36
		}
		isoLabel := p.Label
		var screenLabel, labelBg, labelBorder, labelColor string
		var labelFamily, labelWeight string
		var labelSize float64 = 13
		if mergedForLabel != nil && mergedForLabel.Text != nil && mergedForLabel.Text.Orient == "screen" {
			screenLabel = p.Label
			// v3.1 — stack clones (id~1, id~2, …) repeat the base part's
			// label; one screen label per stack, on the base copy only.
			if strings.Contains(p.ID, "~") {
				screenLabel = ""
			}
			isoLabel = ""
			labelBg = mergedForLabel.Text.BoxBg
			labelBorder = mergedForLabel.Text.BoxBorder
			labelColor = mergedForLabel.Text.Color
			labelFamily = mergedForLabel.Text.Family
			labelWeight = mergedForLabel.Text.Weight
			if mergedForLabel.Text.Size != nil && *mergedForLabel.Text.Size > 0 {
				labelSize = *mergedForLabel.Text.Size
			}
		}
		sub := &Node{
			Shape:   p.Shape,
			Geom:    p.Geom,
			Style:   p.Style,
			Preset:  p.Preset,
			Role:    p.Role,
			Label:   isoLabel,
			Icon:    p.Icon,
			Content: p.Content,
		}
		shape, opts := Flatten(sub, theme)

		ox, oy, oz := 0.0, 0.0, 0.0
		if p.Position != nil && n.GridStep > 0 {
			ox = float64(p.Position.I) * n.GridStep
			oy = float64(p.Position.J) * n.GridStep
		}
		if p.Offset != nil {
			ox += p.Offset.WX
			oy += p.Offset.WY
			oz += p.Offset.WZ
		}

		// v2.10 — an injected group-label (the caption iso_text) is given the
		// PARENT group's id as its SVG data-part-id, so clicking/dragging the
		// caption selects the group in Studio. It is kept OUT of infos (id="")
		// below so the tiny label never overwrites the slab's anchor geometry
		// in the connector/model map.
		svgID := p.ID
		infoID := p.ID
		if p.groupLabel && p.labelFor != "" {
			svgID = p.labelFor
			infoID = ""
		}
		cp := iso25d.CompositePart{ID: svgID, Shape: shape, Opts: opts, OffWX: ox, OffWY: oy, OffWZ: oz}
		parts = append(parts, cp)
		if len(n.Connectors) > 0 {
			rc.addPaint(svgID, shape, opts, ox, oy, oz)
		}

		// partInfo dims MUST match what RenderComposite draws (p.Opts.*),
		// NOT the raw geom: an auto-sized group/boundary substrate has no
		// geom W/D, so geom would fall back to the 140 default while the
		// renderer uses the true derived footprint. That mismatch makes
		// partsScreenOrigin() compute a different projection origin (tx,ty)
		// than RenderComposite, shifting the ENTIRE connector/label layer
		// off the parts — the cause of edges detaching from their nodes.
		// Resolve shape defaults through the same dimensions as RenderComposite;
		// label-only sub-parts retain their non-solid geometry.
		w, d, h := iso25d.ShapeDimensions(shape, opts)
		var body *iso25d.ProjectedBody
		if len(n.Connectors) > 0 && infoID != "" {
			resolvedBody := iso25d.ShapeBody(shape, opts)
			body = &resolvedBody
		}
		infos[i] = partInfo{
			id: infoID, shape: strings.ToLower(strings.TrimSpace(shape)),
			w: w, d: d, h: h, offWX: ox, offWY: oy, offWZ: oz,
			screenLabel: screenLabel, labelBg: labelBg, labelBorder: labelBorder,
			labelColor: labelColor, labelFamily: labelFamily, labelWeight: labelWeight,
			labelFontSize: labelSize,
			sides:         sidesOf(p),
			isSubstrate:   p.isSubstrate,
			body:          body,
		}
	}

	svg := iso25d.RenderComposite(parts)
	// Paint order matters: each "insert after <svg>" call pushes the
	// previous insertion deeper into the body, so the LAST call ends up
	// first (= behind everything). Connectors must paint UNDER parts but
	// OVER the canvas backdrop, so the order here is:
	//
	//   1. connectors first (will end up just below parts)
	//   2. canvas-bg last  (will end up at the very top of doc order
	//                       = painted first = furthest back)
	var connRects []screenRect
	protection := screenLabelProtection{uncertain: len(anns) > 0 || len(n.Annotations) > 0}
	var labelProtection *screenLabelProtection
	for _, p := range infos {
		if p.screenLabel != "" {
			labelProtection = &protection
			break
		}
	}
	if labelProtection != nil && !protection.uncertain {
		protection.prepare = func(p *screenLabelProtection) { protectScreenLabelContent(parts, infos, p) }
	}
	if len(n.Connectors) > 0 {
		workRC := rc
		var spatial connectorSpatialScene
		if depthSorted && spatialVisibility {
			spatial = prepareConnectorSpatial(parts)
			if spatial.reason == "" && (rc == nil || rc.preClipOnly) {
				workRC = &routeCollector{}
				for _, p := range parts {
					workRC.addPaint(p.ID, p.Shape, p.Opts, p.OffWX, p.OffWY, p.OffWZ)
				}
			}
		}
		bodySource := func() (connectorTextBodies, bool) {
			if len(anns) > 0 || len(n.Annotations) > 0 {
				return connectorTextBodies{}, false
			}
			return connectorTextBodyRegions(parts, infos)
		}
		svg, connRects = injectCompositeConnectors(svg, n.Connectors, infos, n.Parts, nSubstrates, workRC, depthSorted, labelProtection, bodySource)
		if depthSorted && spatialVisibility {
			svg = applyConnectorSpatial(svg, workRC, spatial, len(parts))
		}
		if rc != nil && workRC != rc {
			rc.routes = workRC.routes
			rc.paint, rc.partPaint = workRC.paint, workRC.partPaint
			if rc.preClipOnly {
				for i := range rc.routes {
					rc.routes[i].Drawing = nil
					rc.routes[i].World3D = nil
				}
			}
		}
	}
	if canvas != nil {
		svg = injectCanvasBackground(svg, canvas)
	}
	var screenLabels *[]ScreenLabel
	if rc != nil {
		screenLabels = rc.screenLabels
	}
	svg, labelRects := injectScreenLabelsProtected(svg, infos, connRects, screenLabels, protection)
	// v3.0 — node-level annotations join the document-level list so
	// `nodes.X.annotations` is honoured instead of silently dropped.
	allAnns := append(append([]*Annotation(nil), anns...), n.Annotations...)
	if len(allAnns) > 0 {
		svg = injectAnnotations(svg, allAnns, infos, append(labelRects, connRects...))
	}
	// v3.1 — breathing margin: explicit canvas.padding plus implicit glow
	// reserve, applied uniformly so backglow halos and sparse hero shots
	// don't kiss the frame.
	pad := glowPad
	if canvas != nil && canvas.Padding > 0 {
		p := canvas.Padding
		if math.IsInf(p, 1) || p > maxGeomDim {
			p = maxGeomDim // clamp a hostile/absurd padding before it overflows the viewBox
		}
		pad = math.Max(pad, p)
	}
	if pad > 0 {
		svg = padViewBox(svg, pad)
	}
	// Enforce minimum aspect ratio (default 16:10 for Mac displays).
	// canvas.AspectRatio == -1 disables; 0 uses the 16/10 default.
	ar := 16.0 / 10.0
	if canvas != nil && canvas.AspectRatio != 0 {
		ar = canvas.AspectRatio
	}
	svg = enforceAspectRatio(svg, ar)
	// v3.0 — integer outer dimensions: fractional width/height attrs make
	// 1:1 raster captures grow scrollbars and clip the bottom row.
	return ceilOuterDims(svg)
}

// RenderDocument renders every node in a Document and returns a map of
// node-id → SVG string. The scene node (resolved via doc.Scene()) also
// picks up the document's Canvas and Annotations layers; all other
// nodes are rendered without them.
func RenderDocument(doc *Document) map[string]string {
	out := make(map[string]string, len(doc.Nodes))
	scene := doc.Scene()
	for id, n := range doc.Nodes {
		if n == scene {
			out[id] = RenderWithCanvas(n, doc.Theme, doc.Canvas, doc.Annotations)
			continue
		}
		out[id] = Render(n, doc.Theme)
	}
	return out
}

// RenderParts produces one standalone SVG per "atomic" element in the
// document — the second tier of the dsl2topo output.
//
// For composite scenes (the canonical case — both .d2 auto-layout and
// most YAML examples produce a single "scene" composite), each
// CompositePart is lifted into its own Node and rendered in isolation
// so callers can embed an individual icon independently. v2 — recurses
// into `group` parts so nested elements get their own per-part output
// too (otherwise grouping would hide individual nodes from the gallery).
//
// For non-composite documents, the top-level Nodes ARE the atomic
// elements and this delegates to RenderDocument.
func RenderParts(doc *Document) map[string]string {
	out := make(map[string]string)
	for id, n := range doc.Nodes {
		if n.Shape != "composite" {
			out[id] = Render(n, doc.Theme)
			continue
		}
		walkAtomicParts(n.Parts, func(p *CompositePart) {
			sub := &Node{
				Shape:   p.Shape,
				Geom:    p.Geom,
				Style:   p.Style,
				Preset:  p.Preset,
				Role:    p.Role,
				Label:   p.Label,
				Icon:    p.Icon,
				Content: p.Content,
			}
			out[p.ID] = Render(sub, doc.Theme)
		})
		_ = id
	}
	return out
}

// sidesOf extracts geom.sides for the prism family (0 = unset).
func sidesOf(p *CompositePart) int {
	if p.Geom != nil {
		return p.Geom.Sides
	}
	return 0
}

// depthSortParts (v5.2, canvas.depthSort) reorders the lowered parts
// back-to-front by projected depth so the painter order is derived from
// world geometry, not from the author's writing order. The two painter
// partitions renderComposite already established are preserved — the
// substrate/platform block [0:nSubstrates) and the body block
// [nSubstrates:) each sort independently, so slabs always stay under
// bodies. sort.SliceStable keeps equal-depth parts in author order, so
// the output stays deterministic (contract: same input, same bytes).
func depthSortParts(flat []*CompositePart, nSubstrates int, gridStep float64) {
	sortSeg := func(seg []*CompositePart) {
		sort.SliceStable(seg, func(i, j int) bool {
			return partDepthKey(seg[i], gridStep) < partDepthKey(seg[j], gridStep)
		})
	}
	if nSubstrates > len(flat) {
		nSubstrates = len(flat)
	}
	sortSeg(flat[:nSubstrates])
	sortSeg(flat[nSubstrates:])
}

// partDepthKey is the painter depth of one lowered part: the corner sum
// x+y+z of its world back-bottom-left origin. Under this iso camera the
// view axis is (1, 1, 1) — for two axis-aligned boxes with disjoint
// footprints, the one with the LARGER origin corner sum is the one that
// can occlude, so ascending corner sum is a valid back-to-front order
// (and it naturally interleaves parts from different groups). Injected
// group captions pin to +Inf so they keep painting above the children
// they caption, exactly as the lowering pass intended.
func partDepthKey(p *CompositePart, gridStep float64) float64 {
	if p.groupLabel {
		return math.Inf(1)
	}
	x, y, z := 0.0, 0.0, 0.0
	if p.Position != nil && gridStep > 0 {
		x = float64(p.Position.I) * gridStep
		y = float64(p.Position.J) * gridStep
	}
	if p.Offset != nil {
		x += p.Offset.WX
		y += p.Offset.WY
		z += p.Offset.WZ
	}
	return x + y + z
}
