// Post-render SVG layer injectors. Each one splices a new <g
// data-layer="…"> into the SVG produced by iso25d.RenderComposite —
// canvas backdrop, screen-space labels, composite connectors, and
// screen-space annotation callouts.
//
// They all operate on the SVG as a string (parse + edit + emit) so
// they're decoupled from iso25d's internal data structures and stay
// composable.
package isotopo

import (
	"fmt"
	"math"

	"sort"
	"strconv"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// scaleDashForWidth keeps a dashed/dotted connector legible at thick strokes.
// SVG dash lengths are absolute, so an authored "6 4" smears into a near-solid
// band once the line is wider than its gaps. At or below a baseline width (2px —
// where authored hairline patterns already read fine) the pattern is returned
// byte-identical; above it, every length scales with the width so the on/off
// rhythm survives (e.g. "6 4" at width 14 → "42 28"). Non-numeric patterns are
// left untouched.
func scaleDashForWidth(dash string, width float64) string {
	factor := width / 2.0
	if factor <= 1 {
		return dash
	}
	parts := strings.Fields(dash)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return dash
		}
		out = append(out, strconv.FormatFloat(v*factor, 'f', -1, 64))
	}
	if len(out) == 0 {
		return dash
	}
	return strings.Join(out, " ")
}

// partsScreenOrigin returns the (tx, ty) translation that maps world-projected
// coords into the composite's screen space. It derives from the SAME
// iso25d.ProjectedBounds that RenderComposite uses, so the overlay layers
// (connectors, screen labels, annotations) can never drift from the parts.
func partsScreenOrigin(infos []partInfo) (tx, ty float64) {
	boxes := make([][6]float64, len(infos))
	for i, p := range infos {
		boxes[i] = [6]float64{p.offWX, p.offWY, p.offWZ, p.w, p.d, p.h}
	}
	minX, minY, _, _ := iso25d.ProjectedBounds(boxes)
	const pad = 12.0
	return -minX + pad, -minY + pad
}

// projectIso mirrors iso25d's project(), including its coordinate
// quantization — both must agree exactly or the connector/label layer
// drifts off the parts (see iso25d.Cos30's single-source-of-truth note).
func projectIso(wx, wy, wz float64) (float64, float64) {
	return quantIso(wx*cos30 - wy*cos30), quantIso(wx*sin30 + wy*sin30 - wz)
}

// quantIso quantizes a projected coordinate to the same 1e-6 quantum as
// iso25d.project, so sub-ULP FMA differences between Go toolchains can't
// flip threshold decisions (anchor picks, facing tests) between builds.
func quantIso(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func injectCanvasBackground(svg string, c *Canvas) string {
	if c == nil {
		return svg
	}
	bg := strings.TrimSpace(c.Background)
	grid := strings.ToLower(strings.TrimSpace(c.Grid))
	if bg == "" && grid == "" {
		return svg
	}
	start := strings.Index(svg, "<svg")
	if start < 0 {
		return svg
	}
	tagEnd := strings.Index(svg[start:], ">")
	if tagEnd < 0 {
		return svg
	}
	openTag := svg[start : start+tagEnd+1]
	vb := extractAttr(openTag, "viewBox")
	if vb == "" {
		return svg
	}
	var x, y, w, h float64
	if _, err := fmt.Sscanf(vb, "%f %f %f %f", &x, &y, &w, &h); err != nil {
		return svg
	}

	var defs strings.Builder
	fillRef := bg
	if grid != "" && grid != "none" {
		gridColor := c.GridColor
		if gridColor == "" {
			gridColor = "#E2E6EE"
		}
		opts := &RenderOpts{
			BgColor:     bg,
			BgGridColor: gridColor,
			BgGridStep:  c.GridStep,
		}
		switch grid {
		case "iso", "grid":
			opts.Background = BgGrid
		case "dots":
			opts.Background = BgDots
		case "hatch":
			opts.Background = BgHatch
		case "solid":
			opts.Background = BgSolid
		default:
			opts.Background = BgGrid
		}
		fillRef = emitBackgroundDefs(&defs, opts)
	}
	if fillRef == "" {
		return svg
	}

	var sb strings.Builder
	if defs.Len() > 0 {
		fmt.Fprintf(&sb, `<defs>%s</defs>`, defs.String())
	}
	fmt.Fprintf(&sb,
		`<rect data-layer="canvas-bg" x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`,
		x, y, w, h, escAttr(fillRef),
	)
	insertAt := start + tagEnd + 1
	return svg[:insertAt] + sb.String() + svg[insertAt:]
}

// injectScreenLabels appends screen-space horizontal label boxes for any
// part flagged with `style.text.orient: screen`. The label sits centred
// horizontally on the part's projected (top-face centre) and slightly
// below the iso shape's projected bounding box.
func injectScreenLabels(svg string, infos []partInfo, extraObstacles []screenRect) (string, []screenRect) {
	return injectScreenLabelsWithInfo(svg, infos, extraObstacles, nil)
}

func injectScreenLabelsWithInfo(svg string, infos []partInfo, extraObstacles []screenRect, labels *[]ScreenLabel) (string, []screenRect) {
	return injectScreenLabelsProtected(svg, infos, extraObstacles, labels, screenLabelProtection{})
}

func injectScreenLabelsProtected(svg string, infos []partInfo, extraObstacles []screenRect, labels *[]ScreenLabel, protection screenLabelProtection) (string, []screenRect) {
	var observed []ScreenLabel
	project := projectIso
	any := false
	for _, p := range infos {
		if p.screenLabel != "" {
			any = true
			break
		}
	}
	if !any {
		return svg, nil
	}
	tx, ty := partsScreenOrigin(infos)

	// v2.8 — screen-space text contract: labels must not cross or
	// touch any part's projection, and prefer the picture's periphery.
	partRects := partScreenRects(infos)
	sceneCx, sceneCy := sceneCenter(partRects)
	obstacles := append(append([]screenRect(nil), partRects...), extraObstacles...)
	var placed []screenRect

	var placements []screenLabelPlacement
	for i, p := range infos {
		if p.screenLabel == "" {
			continue
		}
		// Preferred (legacy) spot: under the part's bottom-front corner.
		bottomFrontX, bottomFrontY := project(p.offWX+p.w/2, p.offWY+p.d, p.offWZ)
		cx := bottomFrontX + tx
		baseY := bottomFrontY + ty + 14 // 14px gap under the part

		text := p.screenLabel
		family := p.labelFamily
		if family == "" {
			family = "Inter, sans-serif"
		}
		weight := p.labelWeight
		if weight == "" {
			weight = "600"
		}
		fontSize := p.labelFontSize
		block := layoutScreenText(text, family, weight, fontSize)
		boxW, boxH := block.width, block.height
		if block.font != nil {
			family, weight, fontSize = "Source Sans Pro", block.font.Weight(), block.size
		}

		// Bound-font chips reserve their full measured area, including clearance
		// from their own projected body. Legacy estimates retain the existing
		// hanging-caption policy for the owning part.
		others := make([]screenRect, 0, len(obstacles))
		for j, o := range obstacles {
			if j == i && block.font == nil {
				continue
			}
			others = append(others, o)
		}
		bx, by := placeTextBox(boxW, boxH, partRects[i], cx-boxW/2, baseY, sceneCx, sceneCy, others)
		placements = append(placements, screenLabelPlacement{index: i, block: block, family: family, weight: weight, x: bx, y: by, preferred: bx == cx-boxW/2 && by == baseY})
		obstacles = append(obstacles, screenRect{bx, by, bx + boxW, by + boxH})
	}
	compactScreenLabels(placements, partRects, obstacles[:len(partRects)+len(extraObstacles)], protection)
	var sb strings.Builder
	sb.WriteString(`<g data-layer="screen-labels">`)
	boundFonts := map[string]bool{}
	for _, f := range textmetrics.FontsInSVG(svg) {
		boundFonts[f.Hash()] = true
	}
	maxLabelY, maxLabelX := 0.0, 0.0
	minLabelX, minLabelY := math.Inf(1), math.Inf(1)
	for _, placement := range placements {
		i, block := placement.index, placement.block
		p := infos[i]
		text, family, weight := p.screenLabel, placement.family, placement.weight
		boxW, boxH, fontSize := block.width, block.height, block.size
		cx, baseY := placement.x+boxW/2, placement.y
		if block.font != nil && !boundFonts[block.font.Hash()] {
			sb.WriteString(block.font.CSS())
			boundFonts[block.font.Hash()] = true
		}
		if labels != nil {
			observed = append(observed, observeScreenLabel(i, p, block, cx, baseY, family, weight))
		}
		placed = append(placed, screenRect{placement.x, placement.y, placement.x + boxW, placement.y + boxH})
		bg := p.labelBg
		if bg == "" {
			bg = "transparent"
		}
		border := p.labelBorder
		if border == "" {
			border = "none"
		}
		color := p.labelColor
		if color == "" {
			color = "#FFFFFF"
		}
		// v2.10 — a screen-space label is now a first-class interactive node:
		// wrap it in a <g data-part-id> (so Studio's hover/click/drag wiring
		// picks it up) and lay a transparent full-box hit-rect under it with
		// pointer-events:all, so the WHOLE label area is grabbable — not just
		// the glyph strokes — and clicks map back to this part's id/source.
		gidAttr := ""
		if p.id != "" {
			gidAttr = fmt.Sprintf(` data-part-id="%s"`, escAttr(p.id))
		}
		fmt.Fprintf(&sb, `<g data-screen-label="1"%s>`, gidAttr)
		fmt.Fprintf(&sb,
			`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="transparent" pointer-events="all"/>`,
			cx-boxW/2, baseY, boxW, boxH,
		)
		if bg != "transparent" || border != "none" {
			strokeAttr := ""
			if border != "none" {
				strokeAttr = fmt.Sprintf(` stroke="%s" stroke-width="1"`, border)
			}
			fmt.Fprintf(&sb,
				`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="2" ry="2" fill="%s"%s/>`,
				cx-boxW/2, baseY, boxW, boxH, bg, strokeAttr,
			)
		}
		if block.font != nil {
			for _, line := range block.lines {
				fmt.Fprintf(&sb,
					`<text x="%.2f" y="%.2f" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle" xml:space="preserve" xml:lang="en" lang="en" style="white-space:pre;font-kerning:normal;font-variant-ligatures:normal">%s</text>`,
					cx, baseY+line.baseline, escAttr(family), fontSize, escAttr(weight), color, escapeXML(line.text),
				)
			}
		} else {
			fmt.Fprintf(&sb,
				`<text x="%.2f" y="%.2f" dy=".35em" font-family="%s" font-size="%.1f" font-weight="%s" fill="%s" text-anchor="middle">%s</text>`,
				cx, baseY+boxH/2, escAttr(family), fontSize, escAttr(weight), color, escapeXML(text),
			)
		}
		sb.WriteString(`</g>`)
		if labelBottom := baseY + boxH; labelBottom > maxLabelY {
			maxLabelY = labelBottom
		}
		if labelRight := cx + boxW/2; labelRight > maxLabelX {
			maxLabelX = labelRight
		}
		minLabelX = math.Min(minLabelX, cx-boxW/2)
		minLabelY = math.Min(minLabelY, baseY)
	}
	sb.WriteString(`</g>`)
	idx := strings.LastIndex(svg, "</svg>")
	if idx < 0 {
		return svg, placed
	}
	const pad = 12.0
	out := growViewBox(svg[:idx]+sb.String()+svg[idx:], maxLabelX+pad, maxLabelY+pad)
	// Periphery placement can spill past the LEFT/TOP origin too.
	if minLabelX < pad || minLabelY < pad {
		out = growViewBoxAround(out, minSvgRect{
			minX: minLabelX - pad, minY: minLabelY - pad,
			maxX: maxLabelX + pad, maxY: maxLabelY + pad,
		})
	}
	if labels != nil {
		*labels = append(*labels, observed...)
	}
	return out, placed
}

// growViewBox parses the leading <svg ...> tag and, if needed, expands
// viewBox/width/height so the post-hoc-inserted screen labels are not
// clipped. Idempotent — shrinks to a no-op when the current viewBox is

// injectCompositeConnectors splices the route layer in ABOVE the first
// nSubstrates part groups (group slabs) and BELOW every body part, and
// paints arrowheads + label pills in a separate top overlay so they are
// never occluded. Returns the inflated screen rects of every emitted
// orthoThread inserts an L corner between any consecutive pair of world
// points that differ in BOTH the x and y axes, so the rendered polyline is
// always iso-axis-aligned (every segment ±30° or vertical in screen). Used
// to thread an edge's endpoints through user-set waypoints even if a moved
// node left a pair off-axis.
func orthoThread(p [][3]float64) [][3]float64 {
	const eps = 0.01
	out := make([][3]float64, 0, len(p)*2)
	out = append(out, p[0])
	for i := 1; i < len(p); i++ {
		a, b := out[len(out)-1], p[i]
		if math.Abs(b[0]-a[0]) > eps && math.Abs(b[1]-a[1]) > eps {
			out = append(out, [3]float64{b[0], a[1], a[2]}) // x-first corner
		}
		out = append(out, b)
	}
	return out
}

// route segment so later layers (screen labels, annotations) can treat
// routes as collision obstacles.
// collectDescendantObstacles recursively walks the CompositePart subtree and
// appends every named descendant as a planRect obstacle. baseX/Y/Z are the
// absolute world offsets of the parent (already accumulated from ancestors).
// This supplements the flat infos-based obstacle set so that grandchildren of
// boundary containers are seen by the elbow picker even when the boundary
// itself was lowered to a substrate (and thus skipped in the primary loop).
func collectDescendantObstacles(parts []*CompositePart, baseX, baseY, baseZ float64, obstacles *[]planRect) {
	for _, p := range parts {
		if p == nil || p.ID == "" {
			continue
		}
		ox, oy, oz := baseX, baseY, baseZ
		if p.Offset != nil {
			ox += p.Offset.WX
			oy += p.Offset.WY
			oz += p.Offset.WZ
		}
		w, d, h := 140.0, 140.0, 80.0
		if p.Geom != nil {
			if p.Geom.W > 0 {
				w = p.Geom.W
			}
			if p.Geom.D > 0 {
				d = p.Geom.D
			}
			if p.Geom.H > 0 {
				h = p.Geom.H
			}
		}
		*obstacles = append(*obstacles, planRect{id: p.ID, x: ox, y: oy, z: oz, w: w, d: d, h: h})
		collectDescendantObstacles(p.Parts, ox, oy, oz, obstacles)
	}
}

// rc is the WS-E in-memory route collector (routecollect.go): when non-nil,
// every emitted connector's real pre-clip geometry is recorded so evaluators
// can consume structured routes instead of regexing data-route out of the
// SVG. Collection is pure observation — a non-nil collector never changes a
// single output byte (straight edges, by contract, still emit NO data-route).
// depthSort is the WS-A canvas.depthSort flag: routes are layered by endpoint
// depth instead of pinned under all bodies.
func injectCompositeConnectors(svg string, conns []*Connector, infos []partInfo, sceneParts []*CompositePart, nSubstrates int, rc *routeCollector, depthSort bool, protection *screenLabelProtection, bodySources ...connectorTextBodySource) (string, []screenRect) {
	project := projectIso
	tx, ty := partsScreenOrigin(infos)

	byID := map[string]partInfo{}
	paintIndexes := map[string]int{}
	for i, p := range infos {
		if p.id != "" {
			byID[p.id] = p
			paintIndexes[p.id] = i
		}
	}

	// P2 — obstacle footprints for routing avoidance: every non-substrate part
	// as a world rect, so the default elbow pick can prefer the staircase that
	// doesn't tunnel an unrelated node. Reuses the scorecard's planRect kernel
	// (routeTunnels), so the router and the evaluator judge tunnelling alike.
	var obstacles []planRect
	for _, p := range infos {
		if p.id == "" || p.isSubstrate {
			continue
		}
		obstacles = append(obstacles, planRect{id: p.id, x: p.offWX, y: p.offWY, z: p.offWZ, w: p.w, d: p.d, h: p.h})
	}
	// Fix 1 — recursive obstacle expansion: also add grandchildren of boundary
	// containers. The flat infos loop above skips substrates (group slabs), so
	// it may already include the nested parts; this explicit walk guarantees
	// coverage even when a child uses pre-lowering geometry (Geom) rather than
	// the opts.* dims that infos records.
	for _, sp := range sceneParts {
		if sp == nil || !isContainerShape(sp.Shape) {
			continue
		}
		ox, oy, oz := 0.0, 0.0, 0.0
		if sp.Offset != nil {
			ox, oy, oz = sp.Offset.WX, sp.Offset.WY, sp.Offset.WZ
		}
		collectDescendantObstacles(sp.Parts, ox, oy, oz, &obstacles)
	}

	// Fix 2 — cross-hierarchy anchor promotion: build a map from nested-part ID
	// to its scene-level ancestor ID. When a connector endpoint is a nested part
	// and the other endpoint lives outside that same container, the anchor is
	// promoted to the container's face so the route exits the boundary cleanly
	// rather than crossing siblings inside it.
	parentOf := map[string]string{} // nested ID → scene-level container ID
	var walkParent func(parts []*CompositePart, ancestorID string)
	walkParent = func(parts []*CompositePart, ancestorID string) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			if p.ID != "" && ancestorID != "" {
				parentOf[p.ID] = ancestorID
			}
			if isContainerShape(p.Shape) {
				// children of this container get this container's ID as ancestor
				// (only if this container itself is a direct scene child; deeper
				// nesting keeps pointing to the scene-level ancestor)
				aid := ancestorID
				if aid == "" {
					aid = p.ID
				}
				walkParent(p.Parts, aid)
			} else {
				walkParent(p.Parts, ancestorID)
			}
		}
	}
	// Walk scene-level parts; top-level containers become the anchor ancestors.
	for _, sp := range sceneParts {
		if sp == nil {
			continue
		}
		if isContainerShape(sp.Shape) {
			walkParent(sp.Parts, sp.ID)
		}
	}

	ar := &anchorResolver{byID: byID, tx: tx, ty: ty, parentOf: parentOf}

	// v1.6.2 fan-out: count how many orthogonal connectors share each
	// (partID, side) so the per-connector pass can stagger their stubs
	// along the face's tangent and stop them overlapping on the first
	// 24-unit lead-out. Source-side and target-side are bucketed
	// separately — a single anchor may legitimately host both incoming
	// and outgoing channels.
	//
	// v1.6.7 collinear immunity: a connector whose source and target
	// already lie on a single iso axis (so the L collapses to a single
	// segment) does NOT participate in fan-out — shifting it along the
	// tangent would break the collinearity and force a useless kink.
	// Only the genuinely-overlapping subset of edges per anchor needs
	// to be staggered.
	isCollinearConn := make([]bool, len(conns))
	srcSideCount := map[string]int{}
	tgtSideCount := map[string]int{}
	effFrom := make([]string, len(conns))
	effTo := make([]string, len(conns))
	for i, c := range conns {
		// Fix 2 — cross-hierarchy anchor promotion: promote only the SOURCE
		// endpoint when it's a nested part leaving its container. The destination
		// endpoint is kept as-is so the connector still visually targets the
		// specific nested part (the route enters through the boundary).
		promFrom := ar.promoteToContainer(c.From, c.To)
		effFrom[i] = ar.auto(promFrom, c.To)
		effTo[i] = ar.auto(c.To, promFrom)
		if c.Routing != "orthogonal" && c.Routing != "orthogonal2" {
			continue
		}
		sWX, sWY, _, ok1 := ar.world(effFrom[i])
		tWX, tWY, _, ok2 := ar.world(effTo[i])
		if !ok1 || !ok2 {
			continue
		}
		sdx, sdy := ar.exit(effFrom[i])
		tdx, tdy := ar.exit(effTo[i])
		// Ground-hugging route: silhouette refinement uses each endpoint's
		// own offWZ so sphere/cloud/prism anchors are computed at the height
		// where the face actually exists. The route plane (min of the two) is
		// only needed in the main routing pass below.
		srcZi, tgtZi := ar.baseZ(effFrom[i]), ar.baseZ(effTo[i])
		sWX, sWY = ar.refineSilhouette(effFrom[i], sWX, sWY, srcZi)
		tWX, tWY = ar.refineSilhouette(effTo[i], tWX, tWY, tgtZi)

		// Collinear iff face normals oppose AND perpendicular distance
		// is zero (within a small tolerance — refinement and fan-out
		// could otherwise nudge things by a fraction).
		const tol = 0.5
		collinear := false
		if sdx == -tdx && sdy == -tdy {
			if math.Abs(sdx) > math.Abs(sdy) {
				if math.Abs(sWY-tWY) < tol {
					collinear = true
				}
			} else if math.Abs(sdy) > math.Abs(sdx) {
				if math.Abs(sWX-tWX) < tol {
					collinear = true
				}
			}
		}
		isCollinearConn[i] = collinear
		if !collinear {
			if !isFixedPortRef(effFrom[i]) {
				srcSideCount[ar.sideKey(effFrom[i])]++
			}
			if !isFixedPortRef(effTo[i]) {
				tgtSideCount[ar.sideKey(effTo[i])]++
			}
		}
	}
	srcSideIdx := map[string]int{}
	tgtSideIdx := map[string]int{}

	// v2.3 — connector geometry participates in the viewBox. The part
	// bbox alone is NOT enough: orthogonal stubs/staggers and bezier
	// control points can swing past the lowest part and get clipped at
	// the SVG edge. Track the extremes of every emitted waypoint and
	// grow the viewBox afterwards.
	cMinX, cMinY := math.Inf(1), math.Inf(1)
	cMaxX, cMaxY := math.Inf(-1), math.Inf(-1)
	trackPt := func(x, y float64) {
		cMinX = math.Min(cMinX, x)
		cMinY = math.Min(cMinY, y)
		cMaxX = math.Max(cMaxX, x)
		cMaxY = math.Max(cMaxY, y)
	}
	ink := minSvgRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	trackInk := func(b minSvgRect) {
		ink.minX, ink.minY = math.Min(ink.minX, b.minX), math.Min(ink.minY, b.minY)
		ink.maxX, ink.maxY = math.Max(ink.maxX, b.maxX), math.Max(ink.maxY, b.maxY)
	}

	var sb strings.Builder
	var overlay strings.Builder
	// v5.2 — sb holds ONLY the route markup (no <g data-layer> wrapper);
	// connStart[ci] records where connector ci's markup begins so the
	// depthSort mode can re-bucket individual routes into depth layers.
	// The default mode wraps the whole builder in the single legacy
	// layer, producing byte-identical output to the pre-v5.2 renderer.
	connStart := make([]int, len(conns)+1)

	// Obstacles for pill placement + segment rects handed back to later
	// layers. Substrate slabs are not pill obstacles — a pill ON a slab
	// is fine; bodies are not.
	var bodyRects []screenRect
	allRects := partScreenRects(infos)
	for i, p := range infos {
		if !p.isSubstrate {
			bodyRects = append(bodyRects, allRects[i])
		}
	}
	var segRects []screenRect
	var placedPills []screenRect
	var measuredLabels []connectorTextRequest
	var measuredRecords []ConnectorLabelRecord
	var labelObstacles []connectorTextObstacle
	var labelStrokes []connectorTextStroke
	var labelTargets []connectorTextRectTarget
	collectLabelConflicts := rc != nil && !rc.preClipOnly && rc.labelConflicts
	measureLabels := false
	for _, c := range conns {
		measureLabels = measureLabels || c.LabelFontFamily != ""
	}
	if measureLabels && collectLabelConflicts {
		for i, p := range infos {
			if !p.isSubstrate {
				labelTargets = append(labelTargets, connectorTextBodyTarget(p.id, i, allRects[i]))
			}
		}
	}
	var drawnLines []drawnLine
	// v2 router — world polylines of connectors already routed this pass, so
	// each orthogonal2 route can price crossings against them (the +100
	// scorecard weight; see inject_router2.go). v1 connectors contribute their
	// routes too but never read the slice, so the default path is unchanged.
	var placedRoutes [][][2]float64
	for ci, c := range conns {
		connStart[ci] = sb.Len()
		stroke, width, dash := "#7A8390", 1.4, ""
		if c.Stroke != nil {
			if c.Stroke.Color != "" {
				stroke = c.Stroke.Color
			}
			if c.Stroke.Width != nil && *c.Stroke.Width > 0 {
				width = *c.Stroke.Width
			}
			dash = c.Stroke.Dash
		}
		dashAttr := ""
		if dash != "" {
			dashAttr = fmt.Sprintf(` stroke-dasharray="%s"`, escAttr(scaleDashForWidth(dash, width)))
		}

		// Build the polyline waypoints in screen coords.
		var pts [][2]float64
		// routeWorld/routeScreen mirror pts in WORLD (wx,wy) and pre-clip
		// SCREEN-user coords for the orthogonal route, emitted together as
		// data-route so Studio can hit-test a segment (screen) and edit it in
		// world space without re-deriving the route server-side.
		var routeWorld, routeScreen [][2]float64
		// collWorld/collScreen are the WS-E collector's counterparts for the
		// straight/bezier branches, which by contract never emit data-route:
		// keeping them separate from routeWorld/routeScreen guarantees route
		// collection can't leak a new attribute into the SVG.
		var collWorld, collScreen [][2]float64
		var world3D [][3]float64
		switch c.Routing {
		case "orthogonal", "orthogonal2":
			// Anchor-aware L/Z in the iso world ground plane.
			//
			// Each endpoint first walks `stub` along its face normal (so the
			// line cleanly exits the part's side), then bends along the two
			// world axes to meet the other endpoint's stub. The intermediate
			// axis order is chosen by which axis the source exits along, so
			// the very first segment never crosses the source's footprint.
			//
			// Ground-hugging route (v5.1):
			// Connectors must lie on the ground and read as physical links,
			// not bridges flying over the tops (see baseZ / side-anchor docs).
			// So the flat L is routed on the LOWER of the two endpoints'
			// base planes — routeZ = min(srcZ, tgtZ) — and the HIGHER
			// endpoint gets a single vertical riser at its stub that drops
			// it from its floating face down to that ground plane. World-axis
			// +x / +y project to the iso diamond grid; the riser is world +z,
			// which projects to a clean pure-vertical screen segment (the iso
			// "up" direction), so the path stays visually orthogonal. Routing
			// at min (not max) keeps the long flat run hugging the floor where
			// intervening bodies can occlude it — that occlusion is the 2.5D
			// depth cue.
			sWX, sWY, _, ok1 := ar.world(effFrom[ci])
			tWX, tWY, _, ok2 := ar.world(effTo[ci])
			if !ok1 || !ok2 {
				continue
			}
			sdx, sdy := ar.exit(effFrom[ci])
			tdx, tdy := ar.exit(effTo[ci])
			srcZ := ar.baseZ(effFrom[ci])
			tgtZ := ar.baseZ(effTo[ci])
			routeZ := math.Min(srcZ, tgtZ)

			// v1.6.3 / v5.1 shape-aware anchor refinement: use each
			// endpoint's own offWZ so sphere/cloud/prism silhouettes are
			// queried at the height where the face actually exists.
			sWX, sWY = ar.refineSilhouette(effFrom[ci], sWX, sWY, srcZ)
			tWX, tWY = ar.refineSilhouette(effTo[ci], tWX, tWY, tgtZ)

			// v1.6.2 fan-out: when N>1 connectors share a side, slide
			// each endpoint along the face's tangent (perpendicular to
			// its outward normal, in the world xy plane) so they exit
			// from distinct points. The tangent is a 90° rotation of
			// the normal: (-sdy, sdx). Channel width is fixed in world
			// units; stays well inside the face span for the default
			// 70..200-wide parts used in the demos.
			// v1 keeps the historical fixed 14-unit channel; orthogonal2
			// adapts the width to each endpoint's own face span (see
			// router2ChannelW) so a fan-out neither overflows a narrow face
			// nor bunches every exit at the middle of a wide one.
			sChannelW, tChannelW := 14.0, 14.0
			if c.Routing == "orthogonal2" {
				sChannelW = router2ChannelW(ar, effFrom[ci], srcSideCount[ar.sideKey(effFrom[ci])])
				tChannelW = router2ChannelW(ar, effTo[ci], tgtSideCount[ar.sideKey(effTo[ci])])
			}
			var sStagger, tStagger float64
			// Collinear edges keep stagger 0 — shifting them would
			// break the single-segment route the user laid out for.
			if !isCollinearConn[ci] {
				srcKey := ar.sideKey(effFrom[ci])
				tgtKey := ar.sideKey(effTo[ci])
				sN, sIdx := srcSideCount[srcKey], srcSideIdx[srcKey]
				tN, tIdx := tgtSideCount[tgtKey], tgtSideIdx[tgtKey]
				if !isFixedPortRef(effFrom[ci]) {
					srcSideIdx[srcKey]++
					sStagger = (float64(sIdx) - float64(sN-1)/2) * sChannelW
				}
				if !isFixedPortRef(effTo[ci]) {
					tgtSideIdx[tgtKey]++
					tStagger = (float64(tIdx) - float64(tN-1)/2) * tChannelW
				}
			}
			sTanX, sTanY := -sdy, sdx
			tTanX, tTanY := -tdy, tdx
			sWX += sTanX * sStagger
			sWY += sTanY * sStagger
			tWX += tTanX * tStagger
			tWY += tTanY * tStagger
			// v3.2.2 — the fan-out displacement runs along the BBOX face
			// tangent, which pushes the point back OFF a non-rectangular
			// boundary (and outside points are invisible to silhouette
			// clipping — the re-acceptance round's floating endpoints).
			// Re-refine so the stagger becomes an angular spread along
			// the part's real outline.
			if sStagger != 0 {
				sWX, sWY = ar.refineSilhouette(effFrom[ci], sWX, sWY, srcZ)
			}
			if tStagger != 0 {
				tWX, tWY = ar.refineSilhouette(effTo[ci], tWX, tWY, tgtZ)
			}

			// v3.1 — the v1.6.6 arrow-gap pullback is gone: arrowheads now
			// paint in the top overlay and endpoints clip at the target's
			// silhouette, so retracting the tip 8 world units only left it
			// floating on bare canvas (worst on near-flat tiles whose
			// silhouette is a sliver).

			const stub = 24.0
			sStubX, sStubY := sWX+sdx*stub, sWY+sdy*stub
			tStubX, tStubY := tWX+tdx*stub, tWY+tdy*stub

			// orthogonal2 (opt-in v2): search a multi-segment obstacle- and
			// crossing-aware route between the two stub points (see
			// inject_router2.go). Explicit waypoints / bend keep their
			// existing override semantics, so v2 only replaces the AUTO
			// route; nil (degenerate anchors, oversized grid, unreachable)
			// falls back to the v1 elbow route below.
			var v2Pts [][2]float64
			if c.Routing == "orthogonal2" && c.Bend == nil && len(c.Waypoints) == 0 && len(c.Waypoints3D) == 0 {
				fromID2, _ := ar.parse(effFrom[ci])
				toID2, _ := ar.parse(effTo[ci])
				v2Pts = route(
					routerAnchor{ID: fromID2, X: sStubX, Y: sStubY, DX: sdx, DY: sdy, Z: srcZ},
					routerAnchor{ID: toID2, X: tStubX, Y: tStubY, DX: tdx, DY: tdy, Z: tgtZ},
					obstacles, placedRoutes)
			}

			var worldPts [][3]float64
			xFirst := math.Abs(sdx) > math.Abs(sdy)
			switch c.Elbow {
			case "xFirst":
				xFirst = true
			case "yFirst":
				xFirst = false
			default:
				// P2 — obstacle-aware elbow: prefer the staircase order whose
				// route tunnels fewer unrelated nodes. Both orders share the
				// same five corners bar the elbow, so we score each against the
				// node footprints with the scorecard kernel.
				srcID, _ := ar.parse(c.From)
				tgtID, _ := ar.parse(c.To)
				xPts := [][2]float64{{sWX, sWY}, {sStubX, sStubY}, {tStubX, sStubY}, {tStubX, tStubY}, {tWX, tWY}}
				yPts := [][2]float64{{sWX, sWY}, {sStubX, sStubY}, {sStubX, tStubY}, {tStubX, tStubY}, {tWX, tWY}}
				xHits := routeTunnels(xPts, srcID, tgtID, routeZ, obstacles)
				yHits := routeTunnels(yPts, srcID, tgtID, routeZ, obstacles)
				if xHits != yHits {
					xFirst = xHits < yHits
				} else {
					// v4.3 — kill the "tent": in iso, screen-y ∝ (x+y) at a
					// fixed routeZ, so an L whose corner has (x+y) OUTSIDE the
					// endpoints' range projects to a ∧/∨ detour that climbs
					// then plunges. The two elbow orders put the corner at
					// (tStub,sStub) or (sStub,tStub); pick the one whose corner
					// sum stays between the endpoints (monotonic screen-y), and
					// on a tie the one closest to the straight-line midpoint.
					const eps0 = 0.5
					srcSum, tgtSum := sWX+sWY, tWX+tWY
					lo, hi := math.Min(srcSum, tgtSum), math.Max(srcSum, tgtSum)
					cornerXFirst := tStubX + sStubY
					cornerYFirst := sStubX + tStubY
					xIn := cornerXFirst >= lo-eps0 && cornerXFirst <= hi+eps0
					yIn := cornerYFirst >= lo-eps0 && cornerYFirst <= hi+eps0
					if xIn != yIn {
						xFirst = xIn
					} else {
						mid := (srcSum + tgtSum) / 2
						xFirst = math.Abs(cornerXFirst-mid) <= math.Abs(cornerYFirst-mid)
					}
				}
			}
			// v5.1 — z-riser helpers: when an endpoint sits ABOVE the flat
			// routing plane (routeZ = min of both), insert a vertical screen
			// segment at the stub so the connector drops from the elevated
			// node's floating face down to the ground plane, then runs flat.
			// A z-only change projects to a pure-vertical screen line — the
			// iso "up" direction — so the line still reads as orthogonal while
			// hugging the floor for its long run.
			appendRiserSrc := func(pts [][3]float64) [][3]float64 {
				if srcZ > routeZ {
					pts = append(pts, [3]float64{sStubX, sStubY, routeZ})
				}
				return pts
			}
			appendRiserTgt := func(pts [][3]float64) [][3]float64 {
				if tgtZ > routeZ {
					pts = append(pts, [3]float64{tStubX, tStubY, tgtZ})
				}
				return pts
			}
			if len(v2Pts) >= 2 {
				// v2 route found: thread the searched waypoints at routeZ
				// through the same anchor/stub/riser scaffolding v1 uses
				// (v2Pts[0] is the source stub itself — the riser helper
				// already emits it at routeZ when the endpoint floats).
				worldPts = [][3]float64{
					{sWX, sWY, srcZ},
					{sStubX, sStubY, srcZ},
				}
				worldPts = appendRiserSrc(worldPts)
				for _, p := range v2Pts[1:] {
					worldPts = append(worldPts, [3]float64{p[0], p[1], routeZ})
				}
				worldPts = appendRiserTgt(worldPts)
				worldPts = append(worldPts, [3]float64{tWX, tWY, tgtZ})
			} else if xFirst {
				// Source exits along world x → walk x then y.
				worldPts = [][3]float64{
					{sWX, sWY, srcZ},
					{sStubX, sStubY, srcZ},
				}
				worldPts = appendRiserSrc(worldPts)
				worldPts = append(worldPts,
					[3]float64{tStubX, sStubY, routeZ},
					[3]float64{tStubX, tStubY, routeZ},
				)
				worldPts = appendRiserTgt(worldPts)
				worldPts = append(worldPts, [3]float64{tWX, tWY, tgtZ})
			} else {
				// Source exits along world y → walk y then x.
				worldPts = [][3]float64{
					{sWX, sWY, srcZ},
					{sStubX, sStubY, srcZ},
				}
				worldPts = appendRiserSrc(worldPts)
				worldPts = append(worldPts,
					[3]float64{sStubX, tStubY, routeZ},
					[3]float64{tStubX, tStubY, routeZ},
				)
				worldPts = appendRiserTgt(worldPts)
				worldPts = append(worldPts, [3]float64{tWX, tWY, tgtZ})
			}
			// v4.5 — bend: an edge-drag relocates the route's CORNER by a
			// world delta. Rebuild an orthogonal (iso-axis-aligned) path
			// through the moved corner rather than uniformly translating the
			// interior points: the latter leaves the docked endpoints behind,
			// slanting the two end-legs into arbitrary diagonals. Each half is
			// a simple L that leaves/arrives along the part's face normal, so
			// every segment stays on a world axis and the endpoints stay
			// docked. Redundant/degenerate points are collapsed just below.
			if c.Bend != nil && (c.Bend.WX != 0 || c.Bend.WY != 0) {
				baseCX, baseCY := tStubX, sStubY
				if !xFirst {
					baseCX, baseCY = sStubX, tStubY
				}
				cx, cy := baseCX+c.Bend.WX, baseCY+c.Bend.WY
				sAxisX := math.Abs(sdx) >= math.Abs(sdy)
				tAxisX := math.Abs(tdx) >= math.Abs(tdy)
				np := [][3]float64{{sWX, sWY, srcZ}}
				if srcZ > routeZ {
					np = append(np, [3]float64{sWX, sWY, routeZ})
				}
				if sAxisX { // source exits along world x → x-leg then y-leg
					np = append(np, [3]float64{cx, sWY, routeZ})
				} else {
					np = append(np, [3]float64{sWX, cy, routeZ})
				}
				np = append(np, [3]float64{cx, cy, routeZ})
				if tAxisX { // target arrives along world x → y-leg then x-leg
					np = append(np, [3]float64{cx, tWY, routeZ})
				} else {
					np = append(np, [3]float64{tWX, cy, routeZ})
				}
				if tgtZ > routeZ {
					np = append(np, [3]float64{tWX, tWY, routeZ})
				}
				np = append(np, [3]float64{tWX, tWY, tgtZ})
				worldPts = np
			}
			// v4.6 — explicit waypoints supersede the auto route + bend:
			// thread the docked endpoints through the user-set interior
			// corners, inserting an orthogonal corner for any pair that is
			// not axis-aligned (e.g. after a connected node moved) so every
			// segment stays iso-clean.
			if len(c.Waypoints) > 0 {
				np := [][3]float64{{sWX, sWY, srcZ}}
				if srcZ > routeZ {
					np = append(np, [3]float64{sWX + sdx*stub, sWY + sdy*stub, srcZ})
					np = append(np, [3]float64{sWX + sdx*stub, sWY + sdy*stub, routeZ})
				}
				for _, wp := range c.Waypoints {
					np = append(np, [3]float64{wp.WX, wp.WY, routeZ})
				}
				if tgtZ > routeZ {
					np = append(np, [3]float64{tWX - tdx*stub, tWY - tdy*stub, routeZ})
					np = append(np, [3]float64{tWX - tdx*stub, tWY - tdy*stub, tgtZ})
				}
				np = append(np, [3]float64{tWX, tWY, tgtZ})
				worldPts = orthoThread(np)
			}
			if len(c.Waypoints3D) > 0 {
				np := [][3]float64{{sWX, sWY, srcZ}}
				for _, wp := range c.Waypoints3D {
					np = append(np, [3]float64{wp.WX, wp.WY, wp.WZ})
				}
				np = append(np, [3]float64{tWX, tWY, tgtZ})
				worldPts = orthogonalThread3D(np)
			}
			// v1.6 — if every waypoint shares the same world x OR the same
			// world y, the L-shape has degenerated to a single iso-axis line.
			// Emit just (source, target) so the path doesn't render multiple
			// collinear bends (which look like a thicker line at line joints).
			const eps = 0.01
			// v5.0 — track allSameZ so we don't collapse paths that have
			// z-riser segments (z-only changes are valid vertical screen lines).
			allSameX, allSameY, allSameZ := true, true, true
			for _, p := range worldPts[1:] {
				if math.Abs(p[0]-worldPts[0][0]) > eps {
					allSameX = false
				}
				if math.Abs(p[1]-worldPts[0][1]) > eps {
					allSameY = false
				}
				if math.Abs(p[2]-worldPts[0][2]) > eps {
					allSameZ = false
				}
			}
			if len(c.Waypoints3D) == 0 && (allSameX || allSameY) && allSameZ {
				x1, y1 := project(worldPts[0][0], worldPts[0][1], worldPts[0][2])
				last := worldPts[len(worldPts)-1]
				x2, y2 := project(last[0], last[1], last[2])
				pts = append(pts, [2]float64{x1 + tx, y1 + ty})
				pts = append(pts, [2]float64{x2 + tx, y2 + ty})
				routeWorld = [][2]float64{{worldPts[0][0], worldPts[0][1]}, {last[0], last[1]}}
				routeScreen = [][2]float64{{x1 + tx, y1 + ty}, {x2 + tx, y2 + ty}}
				if rc != nil {
					world3D = [][3]float64{worldPts[0], last}
				}
				break
			}
			// v3.1 despike: stub-driven waypoints can walk an axis one way
			// and immediately back (hub→diagonal-neighbour cases), which
			// renders as a doubled line / hook. Collapse any waypoint whose
			// outgoing segment reverses its incoming segment's direction.
			for changed := len(c.Waypoints3D) == 0; changed; {
				changed = false
				for i := 1; i < len(worldPts)-1; i++ {
					inX, inY := worldPts[i][0]-worldPts[i-1][0], worldPts[i][1]-worldPts[i-1][1]
					outX, outY := worldPts[i+1][0]-worldPts[i][0], worldPts[i+1][1]-worldPts[i][1]
					if math.Abs(worldPts[i][2]-worldPts[i-1][2]) > eps || math.Abs(worldPts[i+1][2]-worldPts[i][2]) > eps {
						continue // a vertical riser must not be collapsed by its XY shadow
					}
					if (inX*outX < 0 && math.Abs(inY)+math.Abs(outY) < eps) ||
						(inY*outY < 0 && math.Abs(inX)+math.Abs(outX) < eps) {
						worldPts = append(worldPts[:i], worldPts[i+1:]...)
						changed = true
						break
					}
				}
			}

			// Drop coincident consecutive waypoints so straight-shot
			// connectors don't emit zero-length segments.
			for _, p := range worldPts {
				x, y := project(p[0], p[1], p[2])
				sx, sy := x+tx, y+ty
				if n := len(pts); n > 0 &&
					math.Abs(pts[n-1][0]-sx) < eps && math.Abs(pts[n-1][1]-sy) < eps {
					continue
				}
				pts = append(pts, [2]float64{sx, sy})
				routeWorld = append(routeWorld, [2]float64{p[0], p[1]})
				routeScreen = append(routeScreen, [2]float64{sx, sy})
				if rc != nil {
					world3D = append(world3D, p)
				}
			}
		case "bezier":
			// v2.1 — single quadratic curve from c.From to c.To, control
			// point sits on the perpendicular bisector ¼ of the chord's
			// length away. Produces a natural S-free arc that reads as
			// "data flow" instead of the rigid L-corner of orthogonal.
			x1, y1, ok1 := ar.screen(c.From)
			x2, y2, ok2 := ar.screen(c.To)
			if !ok1 || !ok2 {
				continue
			}
			dx, dy := x2-x1, y2-y1
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > 0 {
				nx, ny := -dy/dist, dx/dist
				bend := dist * 0.25
				cx, cy := (x1+x2)/2+nx*bend, (y1+y2)/2+ny*bend
				pts = [][2]float64{{x1, y1}, {cx, cy}, {x2, y2}}
			} else {
				pts = [][2]float64{{x1, y1}, {x2, y2}}
			}
			// WS-E collection: recover the drawn curve's WORLD track by
			// sampling the quadratic and inverse-projecting (pre-clip).
			if rc != nil {
				_, _, srcZ, _ := ar.world(c.From)
				_, _, tgtZ, _ := ar.world(c.To)
				collScreen, collWorld = sampleBezierRoute(pts, srcZ, tgtZ, tx, ty)
				for i, p := range collWorld {
					t := float64(i) / float64(len(collWorld)-1)
					world3D = append(world3D, [3]float64{p[0], p[1], srcZ + t*(tgtZ-srcZ)})
				}
			}
		default: // "straight" or empty
			x1, y1, ok1 := ar.screen(c.From)
			x2, y2, ok2 := ar.screen(c.To)
			if !ok1 || !ok2 {
				continue
			}
			pts = [][2]float64{{x1, y1}, {x2, y2}}
			// WS-E collection: the drawn line IS the segment between the two
			// world anchors (linear projection), so record those (pre-clip).
			if rc != nil {
				sWX, sWY, sZ, _ := ar.world(c.From)
				tWX, tWY, tZ, _ := ar.world(c.To)
				collWorld = [][2]float64{{sWX, sWY}, {tWX, tWY}}
				collScreen = [][2]float64{{x1, y1}, {x2, y2}}
				world3D = [][3]float64{{sWX, sWY, sZ}, {tWX, tWY, tZ}}
			}
		}

		// Record the finished world polyline (orthogonal family only — the
		// other routings never fill routeWorld) so later orthogonal2 routes
		// can price crossings against every edge already placed. Pre-clip
		// coords, matching the data-route frame the evaluator scores.
		if len(routeWorld) >= 2 {
			placedRoutes = append(placedRoutes, routeWorld)
		}

		curved := c.Routing == "bezier" && len(pts) == 3
		var pathTrace *connectorPathTrace
		if rc != nil && !rc.preClipOnly {
			pathTrace = newConnectorPathTrace(pts, world3D, curved, [2]float64{tx, ty})
		}

		// v3.2.2 — terminal despike: drop a sub-7px final segment that
		// doubles back against its predecessor (stub overshoot at the
		// clipped boundary renders as a V-kink under the arrowhead).
		trimTail := func(p [][2]float64) [][2]float64 {
			for len(p) >= 3 {
				n := len(p)
				lx, ly := p[n-1][0]-p[n-2][0], p[n-1][1]-p[n-2][1]
				px, py := p[n-2][0]-p[n-3][0], p[n-2][1]-p[n-3][1]
				if math.Hypot(lx, ly) < 7 && lx*px+ly*py < 0 {
					pathTrace.shortcutTail()
					p = append(p[:n-2], p[n-1])
					continue
				}
				break
			}
			return p
		}
		if !curved && len(c.Waypoints3D) == 0 {
			pts = trimTail(pts)
			// mirror for the start side
			pathTrace.reverse()
			for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
				pts[i], pts[j] = pts[j], pts[i]
			}
			pts = trimTail(pts)
			pathTrace.reverse()
			for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}

		// v3.0 — endpoint silhouette clipping: when an anchor face is
		// occluded by its own body (back/right faces under this camera),
		// the projected endpoint lands INSIDE the silhouette; the line
		// vanishes behind the part while the overlay arrowhead floats
		// disconnected on the top face. Trim both ends at the silhouette
		// boundary so line + arrow terminate visibly on the entry edge.
		if len(pts) >= 2 {
			toID, _ := ar.parse(effTo[ci])
			fromID, _ := ar.parse(effFrom[ci])
			// Ground-routed connectors run UNDER the bodies, so every endpoint
			// clips to the part's silhouette edge: the line emerges at the
			// boundary and is occluded where it passes behind a body — that
			// occlusion IS the 2.5D depth cue. Both ends clip (substrates too)
			// so a line never invades a face.
			if curved {
				var source, target screenRegion
				if p, ok := byID[fromID]; ok && !isFixedPortRef(c.From) {
					source = partBodyRegion(p, tx, ty)
				}
				if p, ok := byID[toID]; ok && !isFixedPortRef(c.To) {
					target = partBodyRegion(p, tx, ty)
				}
				pts = clipQuadraticRegionsTracked(pts, source, target, 1, 2, pathTrace)
			} else {
				if tp, ok := byID[toID]; ok && !isFixedPortRef(c.To) {
					pts = clipRouteEndRegionTracked(pts, partBodyRegion(tp, tx, ty), 2, pathTrace)
				}
				if sp, ok := byID[fromID]; ok && !isFixedPortRef(c.From) {
					pts = clipRouteStartRegionTracked(pts, partBodyRegion(sp, tx, ty), 1, pathTrace)
				}
			}
		}
		if len(pts) < 2 {
			continue
		}

		// Coincident-line de-dup: if an earlier connector with the SAME style key
		// traces the same polyline (every vertex within eps), reuse its exact
		// coordinates so the two render perfectly on top of each other instead of
		// a few px apart (which reads as one fat line). Path elements stay
		// separate, so data-connector / Studio hit-testing is unaffected.
		styleKey := stroke + "|" + fmt.Sprintf("%.2f", width) + "|" + dash
		coincidenceTolerance := 4.0
		if curved {
			// A control triple must never be reused as a three-point polyline,
			// nor snapped off its curve to a merely nearby control triple.
			styleKey += "|quadratic"
			coincidenceTolerance = 1e-6
		}
		if isFixedPortRef(c.From) || isFixedPortRef(c.To) {
			// Never let a later legacy edge adopt this fixed path under its
			// looser coincidence tolerance (or the reverse).
			styleKey += "|fixed-port"
			coincidenceTolerance = 1e-6
		}
		if c.Stroke != nil && c.Stroke.Gradient != nil {
			styleKey += "|grad:" + c.Stroke.Gradient.From + ">" + c.Stroke.Gradient.To
		}
		matched := false
		for _, dl := range drawnLines {
			if dl.key == styleKey && coincidentFwd(pts, dl.pts, coincidenceTolerance) {
				pathTrace.reuse(pts, dl.pts)
				pts = dl.pts
				matched = true
				break
			}
		}
		if !matched {
			drawnLines = append(drawnLines, drawnLine{styleKey, pts})
		}

		// Labels and occupied rectangles follow the actual visible curve;
		// its off-curve control point is only for Q emission and the tangent.
		visibleRoute := pts
		if curved {
			samples := (quadratic{pts[0], pts[1], pts[2]}).samples(.1)
			visibleRoute = make([][2]float64, len(samples))
			for i, sample := range samples {
				visibleRoute[i] = sample.p
			}
		}
		for _, p := range visibleRoute {
			trackPt(p[0], p[1])
		}

		// Emit path. Bezier routing emits a single quadratic curve
		// (M start Q ctrl end); everything else lays down a polyline.
		// Arrow + label still consume `pts[0]` and `pts[len-1]` so they
		// stay correct in both modes.
		var d strings.Builder
		if c.Routing == "bezier" && len(pts) == 3 {
			fmt.Fprintf(&d, "M %.2f,%.2f Q %.2f,%.2f %.2f,%.2f",
				pts[0][0], pts[0][1], pts[1][0], pts[1][1], pts[2][0], pts[2][1])
		} else {
			for i, p := range pts {
				if i == 0 {
					fmt.Fprintf(&d, "M %.2f,%.2f", p[0], p[1])
				} else {
					fmt.Fprintf(&d, " L %.2f,%.2f", p[0], p[1])
				}
			}
		}
		// data-route = "sx,sy,wx,wy sx,sy,wx,wy ..." — each rendered corner in
		// pre-clip SCREEN-user and WORLD coords, source→target. Studio reads it
		// to move one segment at a time (see wireDrag/editSegment).
		var routeAttr string
		if len(routeScreen) == len(routeWorld) && len(routeWorld) >= 2 {
			var rb strings.Builder
			for i := range routeWorld {
				if i > 0 {
					rb.WriteByte(' ')
				}
				fmt.Fprintf(&rb, "%.2f,%.2f,%.3f,%.3f",
					routeScreen[i][0], routeScreen[i][1], routeWorld[i][0], routeWorld[i][1])
			}
			routeAttr = fmt.Sprintf(` data-route="%s"`, rb.String())
		}
		// data-from / data-to = the connected part ids, so Studio can live-
		// follow a dragged node: the endpoint docked to that node tracks it
		// during the drag (intermediate consistency), not just on drop.
		fromID, _ := ar.parse(c.From)
		toID, _ := ar.parse(c.To)
		// WS-E — record this connector's real route (pure observation; the
		// SVG bytes below are untouched). Recording sits HERE, at the same
		// gate the <path> emission passes, so the collector's coverage is
		// exactly "connectors that actually got drawn": for orthogonal edges
		// that means the very corners serialized as data-route — the legacy
		// regex evaluator's coverage — while straight/bezier edges add the
		// geometry the regex path never saw.
		var collected *ConnectorRoute
		submittedPoints := svgPoints(pts)
		trackInk(connectorStrokeBounds(submittedPoints, curved, svgCoord(width)))
		if measureLabels {
			labelStrokes = append(labelStrokes, connectorTextStrokes(ci, submittedPoints, curved, svgCoord(width))...)
			labelObstacles = append(labelObstacles, connectorTextStrokeObstacles(ci, submittedPoints, curved, svgCoord(width))...)
		}
		if protection != nil {
			if curved {
				b := connectorStrokeBounds(submittedPoints, true, svgCoord(width))
				protection.rects = append(protection.rects, screenRect{b.minX, b.minY, b.maxX, b.maxY})
			} else {
				for j := 1; j < len(submittedPoints); j++ {
					b := connectorStrokeBounds(submittedPoints[j-1:j+1], false, svgCoord(width))
					protection.rects = append(protection.rects, screenRect{b.minX, b.minY, b.maxX, b.maxY})
				}
			}
		}
		if rc != nil {
			switch c.Routing {
			case "orthogonal", "orthogonal2":
				// orthogonal2 must record its REAL routed corners: dropping it to
				// the straight default made the evaluator score the plan router's
				// (crossing-avoiding) fallback instead of the drawn route, so a
				// repair could "clear" a crossing that was still on screen.
				collected = rc.add(ci, fromID, toID, c.Routing, routeWorld, routeScreen)
			case "bezier":
				collected = rc.add(ci, fromID, toID, "bezier", collWorld, collScreen)
			default:
				collected = rc.add(ci, fromID, toID, "straight", collWorld, collScreen)
			}
			if collected != nil && !rc.preClipOnly {
				kind := "polyline"
				if curved {
					kind = "quadratic"
				}
				resolvedSource, resolvedTarget := c.From, c.To
				if c.Routing == "orthogonal" || c.Routing == "orthogonal2" {
					resolvedSource, resolvedTarget = effFrom[ci], effTo[ci]
				}
				collected.World3D = world3D
				collected.Drawing = &ConnectorDrawing{
					Kind: kind, Points: submittedPoints, SourceRef: c.From, TargetRef: c.To,
					ResolvedSource: resolvedSource, ResolvedTarget: resolvedTarget,
					ProjectionOrigin: [2]float64{tx, ty}, StrokeWidth: svgCoord(width),
					PaintBeforePart: connectorPaintLayer(effFrom[ci], effTo[ci], paintIndexes, nSubstrates, depthSort),
				}
				collected.Drawing.Coverage = observeConnectorCoverage(collected.Drawing, rc.paint)
				collected.Drawing.Provenance = pathTrace.finish(collected.Drawing)
				collected.Drawing.Attachments = connectorAttachments(ar, collected.Drawing)
				collected.Drawing.Editable = connectorEditSpans(collected)
			}
		}
		// Connector gradient: a userSpaceOnUse linear gradient laid along the
		// route (source endpoint -> target endpoint). Stroke then references it,
		// so the line (and its dash segments + arrowhead) fade source->target.
		if c.Stroke != nil && c.Stroke.Gradient != nil && c.Stroke.Gradient.To != "" {
			// Gradient "to" alone is enough: the stroke color doubles as the
			// start, so the editor offers one "stroke color + optional gradient
			// to" model (matching node faces).
			gfrom := c.Stroke.Gradient.From
			if gfrom == "" {
				gfrom = stroke
			}
			gradID := fmt.Sprintf("conn-grad-%d", ci)
			fmt.Fprintf(&sb,
				`<linearGradient id="%s" gradientUnits="userSpaceOnUse" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient>`,
				gradID, pts[0][0], pts[0][1], pts[len(pts)-1][0], pts[len(pts)-1][1],
				escAttr(gfrom), escAttr(c.Stroke.Gradient.To),
			)
			stroke = "url(#" + gradID + ")"
		}
		fmt.Fprintf(&sb,
			`<path data-connector="%d" data-from="%s" data-to="%s"%s d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"%s/>`,
			ci, escAttr(fromID), escAttr(toID), routeAttr, d.String(), escAttr(stroke), width, dashAttr,
		)

		// Record inflated segment rects (route obstacles for later layers).
		for i := 1; i < len(visibleRoute); i++ {
			inf := width/2 + 3
			segRects = append(segRects, screenRect{
				math.Min(visibleRoute[i-1][0], visibleRoute[i][0]) - inf, math.Min(visibleRoute[i-1][1], visibleRoute[i][1]) - inf,
				math.Max(visibleRoute[i-1][0], visibleRoute[i][0]) + inf, math.Max(visibleRoute[i-1][1], visibleRoute[i][1]) + inf,
			})
		}

		// Arrow on last segment — painted in the top overlay so an
		// occluded entry face still shows its tip.
		if c.Arrow == "triangle" && len(pts) >= 2 {
			end := pts[len(pts)-1]
			prev := pts[len(pts)-2]
			theta := math.Atan2(end[1]-prev[1], end[0]-prev[0])
			size := 6.0 + width
			tipX, tipY := end[0], end[1]
			b1x := tipX - size*math.Cos(theta) - size*0.5*math.Sin(theta)
			b1y := tipY - size*math.Sin(theta) + size*0.5*math.Cos(theta)
			b2x := tipX - size*math.Cos(theta) + size*0.5*math.Sin(theta)
			b2y := tipY - size*math.Sin(theta) - size*0.5*math.Cos(theta)
			arrowPoints := svgPoints([][2]float64{{tipX, tipY}, {b1x, b1y}, {b2x, b2y}})
			arrowBounds := screenRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
			for _, p := range arrowPoints {
				trackInk(minSvgRect{p[0] - 1, p[1] - 1, p[0] + 1, p[1] + 1})
				arrowBounds.x0, arrowBounds.y0 = math.Min(arrowBounds.x0, p[0]), math.Min(arrowBounds.y0, p[1])
				arrowBounds.x1, arrowBounds.y1 = math.Max(arrowBounds.x1, p[0]), math.Max(arrowBounds.y1, p[1])
			}
			if measureLabels {
				labelObstacles = append(labelObstacles, connectorTextObstacle{-1, arrowBounds})
				if collectLabelConflicts {
					labelTargets = append(labelTargets, connectorTextRectTarget{kind: "arrow", index: ci, rect: arrowBounds})
				}
			}
			if protection != nil {
				protection.rects = append(protection.rects, arrowBounds)
			}
			if collected != nil && !rc.preClipOnly {
				collected.Drawing.Arrow = arrowPoints
			}
			markup := fmt.Sprintf(
				`<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="%s"/>`,
				tipX, tipY, b1x, b1y, b2x, b2y, escAttr(stroke),
			)
			if collected != nil && !rc.preClipOnly {
				rc.arrowFragments = append(rc.arrowFragments, connectorArrowFragment{ci, overlay.Len(), markup})
			}
			overlay.WriteString(markup)
		}

		// v3.0 — label pill slides along the polyline to a body-free
		// segment (longest first); a midpoint that lands inside a part's
		// projection swallowed the pill entirely under the old rule.
		if strings.TrimSpace(c.Label) != "" && len(pts) >= 2 {
			block := layoutConnectorText(c, 13)
			if block.font != nil {
				req := connectorTextRequest{index: ci, c: c, block: block, points: submittedPoints, curved: curved}
				if collected != nil && !rc.preClipOnly {
					req.drawn = collected.Drawing
				}
				measuredLabels = append(measuredLabels, req)
				continue
			}
			// Connector labels still use an unbound browser font; a pill alone
			// cannot certify their glyph envelope for new caption placements.
			if protection != nil {
				protection.uncertain = true
			}
			bg := c.LabelBg
			if bg == "" {
				bg = "#FFFFFFEE"
			}
			ink := c.LabelColor
			if ink == "" {
				ink = "#1F2433"
			}
			lfs := c.LabelFontSize
			if lfs <= 0 {
				lfs = 13
			}
			textW := textWidthPx(c.Label, lfs, textmetrics.NarrowConnectorLabel) + 12

			type seg struct{ mx, my, length, fromMid float64 }
			total := 0.0
			for i := 1; i < len(visibleRoute); i++ {
				total += math.Hypot(visibleRoute[i][0]-visibleRoute[i-1][0], visibleRoute[i][1]-visibleRoute[i-1][1])
			}
			segs := make([]seg, 0, len(visibleRoute)-1)
			walked := 0.0
			for i := 1; i < len(visibleRoute); i++ {
				l := math.Hypot(visibleRoute[i][0]-visibleRoute[i-1][0], visibleRoute[i][1]-visibleRoute[i-1][1])
				segs = append(segs, seg{
					(visibleRoute[i-1][0] + visibleRoute[i][0]) / 2, (visibleRoute[i-1][1] + visibleRoute[i][1]) / 2,
					l, math.Abs(walked + l/2 - total/2),
				})
				walked += l
			}
			// v3.1 — candidates ordered by closeness to the route's VISUAL
			// midpoint (then by length); a dogleg's pill used to teleport
			// to whichever segment happened to be longest.
			sort.Slice(segs, func(a, b int) bool {
				if math.Abs(segs[a].fromMid-segs[b].fromMid) > 1 {
					return segs[a].fromMid < segs[b].fromMid
				}
				return segs[a].length > segs[b].length
			})
			mx, my := segs[0].mx, segs[0].my
			for _, sg := range segs {
				r := screenRect{sg.mx - textW/2 - 2, sg.my - 12, sg.mx + textW/2 + 2, sg.my + 12}
				if !collides(r, bodyRects) && !collides(r, placedPills) {
					mx, my = sg.mx, sg.my
					break
				}
			}
			if collected != nil && !rc.preClipOnly {
				collected.Drawing.Label = &ConnectorLabel{Text: c.Label, Rect: [4]float64{svgCoord(mx - textW/2), svgCoord(my - 10), svgCoord(textW), 20}, Anchor: [2]float64{svgCoord(mx), svgCoord(my)}}
			}
			placedPills = append(placedPills, screenRect{mx - textW/2, my - 10, mx + textW/2, my + 10})
			if measureLabels && collectLabelConflicts {
				x, y, w := svgCoord(mx-textW/2), svgCoord(my-10), svgCoord(textW)
				labelTargets = append(labelTargets, connectorTextRectTarget{kind: "label", index: ci, rect: screenRect{x, y, x + w, y + 20}})
			}
			trackPt(mx-textW/2, my-10)
			trackPt(mx+textW/2, my+10)
			fmt.Fprintf(&overlay,
				`<rect x="%.2f" y="%.2f" width="%.2f" height="20" rx="4" ry="4" fill="%s"/>`,
				mx-textW/2, my-10, textW, escAttr(bg),
			)
			family, weight := connectorFont(c)
			fmt.Fprintf(&overlay,
				`<text x="%.2f" y="%.2f" dy=".35em" font-family="%s" font-size="%.1f" font-weight="%s" fill="%s" text-anchor="middle">%s</text>`,
				mx, my, escAttr(family), lfs, escAttr(weight), escAttr(ink), escapeXML(c.Label),
			)
		}
	}
	connStart[len(conns)] = sb.Len()
	// Append only after all arrows have been emitted: their recorded overlay
	// byte offsets stay valid, and a late route cannot run through an early pill.
	fonts := map[string]bool{}
	for _, f := range textmetrics.FontsInSVG(svg) {
		fonts[f.Hash()] = true
	}
	placements := resolveConnectorText(measuredLabels, bodyRects, placedPills, labelObstacles, labelStrokes)
	var refined []bool
	var bodySource connectorTextBodySource
	if len(bodySources) > 0 && bodySources[0] != nil {
		var body connectorTextBodies
		loaded, supported := false, false
		bodySource = func() (connectorTextBodies, bool) {
			if !loaded {
				body, supported = bodySources[0]()
				loaded = true
			}
			return body, supported
		}
		refined = refineConnectorTextBodies(placements, measuredLabels, placedPills, labelObstacles, labelStrokes, bodySource)
	}
	refineConnectorTextJoint(placements, measuredLabels, bodyRects, placedPills, labelObstacles, labelStrokes, bodySource)
	for i, req := range measuredLabels {
		bg, ink := req.c.LabelBg, req.c.LabelColor
		if bg == "" {
			bg = "#FFFFFFEE"
		}
		if ink == "" {
			ink = "#1F2433"
		}
		bodyRefined := len(refined) > i && refined[i]
		label := emitConnectorText(&overlay, req, placements[i], bg, ink, fonts, bodyRefined)
		measuredRecords = append(measuredRecords, ConnectorLabelRecord{req.index, label})
		if req.drawn != nil {
			req.drawn.Label = &label
		}
		r := connectorLabelRect(label)
		placedPills = append(placedPills, r)
		segRects = append(segRects, r)
		if protection != nil {
			protection.rects = append(protection.rects, r)
			protection.uncertain = protection.uncertain || label.Typography.Placement == "blocked"
		}
		trackPt(r.x0, r.y0)
		trackPt(r.x1, r.y1)
	}

	// Keep the established layout margin, expanding only when actual submitted
	// stroke/arrow ink exceeds it. Fixed padding alone clips wide connectors.
	if cMinX <= cMaxX {
		const pad = 16.0
		svg = growViewBoxAround(svg, minSvgRect{
			minX: math.Min(cMinX-pad, ink.minX), minY: math.Min(cMinY-pad, ink.minY),
			maxX: math.Max(cMaxX+pad, ink.maxX), maxY: math.Max(cMaxY+pad, ink.maxY),
		})
	}

	// v3.0 — splice the route layer above the substrate block: after the
	// nSubstrates-th <g data-part=…> group (substrates are partitioned to
	// the front of the painter order by renderComposite). Routes still
	// paint under every BODY part — iso silhouettes occlude crossing
	// lines — but group slabs no longer swallow them. With nSubstrates ==
	// 0 this degrades to the v1.6.5 behaviour (just after the <svg> tag).
	//
	// v5.2 — canvas.depthSort layers routes by endpoint depth instead:
	// each connector is spliced just BELOW its back-most endpoint part
	// (paint index min(from, to) in the depth-sorted order, clamped to
	// stay above the substrate block). Parts strictly behind both
	// endpoints can no longer swallow a route between two foreground
	// parts, while parts in front still occlude it — the 2.5D depth cue.
	spliceAfterParts := func(svg, layer string, nParts int) string {
		start := strings.Index(svg, "<svg")
		if start < 0 {
			return svg
		}
		tagEnd := strings.Index(svg[start:], ">")
		if tagEnd < 0 {
			return svg
		}
		insertAt := start + tagEnd + 1
		for k := 0; k < nParts; k++ {
			end, ok := partGroupEnd(svg, insertAt)
			if !ok {
				break
			}
			insertAt = end
		}
		return svg[:insertAt] + layer + svg[insertAt:]
	}
	if !depthSort {
		svg = spliceAfterParts(svg, `<g data-layer="connectors">`+sb.String()+`</g>`, nSubstrates)
	} else {
		layerOf := func(ci int) int {
			return connectorPaintLayer(effFrom[ci], effTo[ci], paintIndexes, nSubstrates, depthSort)
		}
		routes := sb.String()
		layers := map[int]string{nSubstrates: ""} // base layer always exists (Studio parity)
		for ci := range conns {
			m := routes[connStart[ci]:connStart[ci+1]]
			if m == "" {
				continue
			}
			layers[layerOf(ci)] += m
		}
		ks := make([]int, 0, len(layers))
		for k := range layers {
			ks = append(ks, k)
		}
		sort.Ints(ks)
		// Each splice recounts <g data-part=…> groups from the top, and a
		// route layer contains none, so earlier splices never skew later
		// insertion points.
		for _, k := range ks {
			svg = spliceAfterParts(svg, `<g data-layer="connectors">`+layers[k]+`</g>`, k)
		}
	}

	// Arrowheads and label pills go in a top overlay so a tip landing on
	// a part's projection (occluded entry face) stays visible.
	if overlay.Len() > 0 {
		if closeIdx := strings.LastIndex(svg, "</svg>"); closeIdx >= 0 {
			svg = svg[:closeIdx] + `<g data-layer="connector-overlay">` + finishConnectorText(overlay.String(), measuredRecords) + `</g>` + svg[closeIdx:]
			if collectLabelConflicts {
				observeConnectorTextConflicts(measuredRecords, labelTargets, labelStrokes)
			}
		}
	}
	return svg, segRects
}

// injectAnnotations paints each callout as a rounded text box plus a
// thin leader line back to its anchor's projected silhouette. Same
// projection math as injectScreenLabels — reused inline so we don't

func injectAnnotations(svg string, anns []*Annotation, infos []partInfo, extraObstacles []screenRect) string {
	if len(anns) == 0 || len(infos) == 0 {
		return svg
	}
	project := projectIso
	tx, ty := partsScreenOrigin(infos)

	// v2.8 — same screen-space contract as labels: never cross or
	// touch a part; pick the most peripheral collision-free spot.
	partRects := partScreenRects(infos)
	sceneCx, sceneCy := sceneCenter(partRects)
	obstacles := append(append([]screenRect(nil), partRects...), extraObstacles...)

	byID := make(map[string]partInfo, len(infos))
	anchorRectByID := make(map[string]screenRect, len(infos))
	for i, p := range infos {
		if p.id != "" {
			byID[p.id] = p
			anchorRectByID[p.id] = partRects[i]
		}
	}

	var sb strings.Builder
	sb.WriteString(`<g data-layer="annotations" font-family="Inter, sans-serif">`)
	maxRightX, maxBottomY := 0.0, 0.0
	minAnnX, minAnnY := math.Inf(1), math.Inf(1)
	for _, a := range anns {
		p, ok := byID[a.Anchor]
		if !ok {
			continue
		}
		// Anchor on the top-face centre — visually the part's "head".
		ax, ay := project(p.offWX+p.w/2, p.offWY+p.d/2, p.offWZ+p.h)
		ax += tx
		ay += ty
		// v3.0 — box positions are measured from the part's projected
		// RECT edges. Measuring every side from the top-face centre made
		// side:bottom land inside the body (a part extends far below its
		// top centre), so the collision search always bounced it to the
		// top — side: bottom literally behaved like side: top.
		ar := anchorRectByID[a.Anchor]
		acx, acy := (ar.x0+ar.x1)/2, (ar.y0+ar.y1)/2

		side := a.Side
		if side == "" {
			side = "right"
		}
		dist := a.Distance
		if dist <= 0 {
			dist = 28
		}
		fontSize := a.FontSize
		if fontSize <= 0 {
			fontSize = 12
		}
		bg := a.Bg
		if bg == "" {
			bg = "#FFFFFF"
		}
		border := a.Border
		if border == "" {
			border = "#1F2937"
		}
		color := a.Color
		if color == "" {
			color = "#1F2937"
		}

		lines := strings.Split(a.Text, "\n")
		// v3.1 — measure display width, not bytes: CJK and box-drawing
		// glyphs are multi-byte AND wider than latin, so byte counting
		// over- and under-sizes the box depending on script.
		widest := 0.0
		for _, l := range lines {
			w := 0.0
			for _, rn := range l {
				if rn >= 0x2500 && rn <= 0x257F { // box-drawing rules
					w += textmetrics.BoxDrawingEm
				} else {
					w += textmetrics.RuneEm(rn, textmetrics.NarrowAnnotation)
				}
			}
			if w > widest {
				widest = w
			}
		}
		boxW := widest*fontSize + 20
		boxH := float64(len(lines))*(fontSize+4) + 14

		posFor := func(s string, d float64) (float64, float64) {
			switch s {
			case "top":
				return acx - boxW/2, ar.y0 - d - boxH
			case "bottom":
				return acx - boxW/2, ar.y1 + d
			case "left":
				return ar.x0 - d - boxW, acy - boxH/2
			default: // right
				return ar.x1 + d, acy - boxH/2
			}
		}
		// Candidate order is SIDE-major for consistency: the author's
		// side is exhausted at every distance before any other side is
		// tried; fallback sides are ordered periphery-first.
		outX, outY := ax-sceneCx, ay-sceneCy
		sideScore := func(s string) float64 {
			switch s {
			case "top":
				return -outY
			case "bottom":
				return outY
			case "left":
				return -outX
			default:
				return outX
			}
		}
		sides := []string{side}
		for _, s2 := range []string{"top", "right", "bottom", "left"} {
			if s2 != side {
				sides = append(sides, s2)
			}
		}
		rest := sides[1:]
		for i := 0; i < len(rest); i++ {
			for j := i + 1; j < len(rest); j++ {
				if sideScore(rest[j]) > sideScore(rest[i])+1e-9 {
					rest[i], rest[j] = rest[j], rest[i]
				}
			}
		}
		bx, by := posFor(side, dist)
		found := false
		for _, s2 := range sides {
			for _, d := range []float64{dist, dist + 45, dist + 90, dist + 140} {
				cx2, cy2 := posFor(s2, d)
				c := screenRect{cx2, cy2, cx2 + boxW, cy2 + boxH}
				if !collides(c, obstacles) {
					bx, by, side = cx2, cy2, s2
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		obstacles = append(obstacles, screenRect{bx, by, bx + boxW, by + boxH})

		// Leader line: from the silhouette edge FACING the box to the
		// box's nearest edge — never across the node's own body.
		var lx2, ly2 float64
		switch side {
		case "top":
			lx2, ly2 = bx+boxW/2, by+boxH
		case "bottom":
			lx2, ly2 = bx+boxW/2, by
		case "left":
			lx2, ly2 = bx+boxW, by+boxH/2
		default:
			lx2, ly2 = bx, by+boxH/2
		}
		lx1, ly1 := leaderStart(anchorRectByID[a.Anchor], lx2, ly2)
		fmt.Fprintf(&sb,
			`<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="1" stroke-dasharray="3 3"/>`,
			lx1, ly1, lx2, ly2, escAttr(border),
		)
		fmt.Fprintf(&sb,
			`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="6" ry="6" fill="%s" stroke="%s" stroke-width="1"/>`,
			bx, by, boxW, boxH, escAttr(bg), escAttr(border),
		)
		for i, line := range lines {
			ty := by + 12 + float64(i+1)*(fontSize+1) - 4
			fmt.Fprintf(&sb,
				`<text x="%.2f" y="%.2f" font-size="%.2f" fill="%s" text-anchor="middle">%s</text>`,
				bx+boxW/2, ty, fontSize, escAttr(color), escapeXML(line),
			)
		}

		if r := bx + boxW; r > maxRightX {
			maxRightX = r
		}
		if b := by + boxH; b > maxBottomY {
			maxBottomY = b
		}
		if bx < minAnnX {
			minAnnX = bx
		}
		if by < minAnnY {
			minAnnY = by
		}
	}
	sb.WriteString(`</g>`)
	if math.IsInf(minAnnX, 1) {
		minAnnX = 0
	}
	if math.IsInf(minAnnY, 1) {
		minAnnY = 0
	}

	// Annotations on top/left side spill PAST the viewBox origin, so
	// growing only width/height won't help. Re-expand the viewBox in all
	// four directions (and shift the canvas-bg rect to match) before
	// splicing the layer in just before </svg>.
	svg = growViewBoxAround(svg, minSvgRect{
		minX: minAnnX - 8, minY: minAnnY - 8,
		maxX: maxRightX + 8, maxY: maxBottomY + 8,
	})
	closeIdx := strings.LastIndex(svg, "</svg>")
	if closeIdx < 0 {
		return svg
	}
	return svg[:closeIdx] + sb.String() + svg[closeIdx:]
}

// partGroupEnd finds the index just past the closing </g> of the next
// <g data-part="..."> group at or after `from`. Part markup nests <g>
// elements freely, so the close is found by depth-counting, not by the
// first </g>.
func partGroupEnd(svg string, from int) (int, bool) {
	g := strings.Index(svg[from:], `<g data-part="`)
	if g < 0 {
		return 0, false
	}
	i := from + g
	depth := 0
	for i < len(svg) {
		open := strings.Index(svg[i:], "<g")
		close := strings.Index(svg[i:], "</g>")
		if close < 0 {
			return 0, false
		}
		if open >= 0 && open < close {
			depth++
			i += open + 2
			continue
		}
		depth--
		i += close + len("</g>")
		if depth == 0 {
			return i, true
		}
	}
	return 0, false
}

type drawnLine struct {
	key string
	pts [][2]float64
}

// coincidentFwd reports whether two equal-length polylines are within eps at
// every vertex, in the same direction.
func coincidentFwd(a, b [][2]float64, eps float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i][0]-b[i][0]) > eps || math.Abs(a[i][1]-b[i][1]) > eps {
			return false
		}
	}
	return true
}

// prismSidesFor resolves the side count for a prism-family shape:
// named variants carry their count in the NAME; bare "prism" reads
// geom.sides. Returning 0 means "not a prism".
func prismSidesFor(shape string, geomSides int) int {
	switch shape {
	case "diamond":
		return 4
	case "triprism":
		return 3
	case "hexprism":
		return 6
	case "octprism":
		return 8
	case "prism":
		if geomSides >= 3 {
			if geomSides > maxPrismSides {
				return maxPrismSides // cap: a huge side count is a render-time DoS
			}
			return geomSides
		}
		return 6
	}
	return 0
}

// maxPrismSides caps polygon sides at the render boundary so a fat-fingered or
// hostile geom.sides (e.g. 1e9) can't exhaust CPU/memory building a useless
// near-circle. 512 sides is already visually a smooth circle.
const maxPrismSides = 512
