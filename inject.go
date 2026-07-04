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

	var sb strings.Builder
	sb.WriteString(`<g data-layer="screen-labels">`)
	maxLabelY := 0.0
	maxLabelX := 0.0
	minLabelX, minLabelY := math.Inf(1), math.Inf(1)
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
		// Surgical CJK fix: wide runes advance one em (BYTE counting
		// inflated CJK labels ~3× — each ideograph is 3 bytes in
		// UTF-8), while narrow runes keep the legacy per-byte advance
		// so every frozen non-CJK golden (incl. em-dash / middle-dot
		// labels) stays byte-identical.
		boxW := textmetrics.TextPxWideBytes(text, fontSize, textmetrics.NarrowScreenLabel) + 16
		boxH := fontSize + 10

		// Own silhouette doesn't block the label hanging off its edge —
		// every OTHER part and already-placed label does.
		others := make([]screenRect, 0, len(obstacles))
		for j, o := range obstacles {
			if j == i {
				continue
			}
			others = append(others, o)
		}
		bx, by := placeTextBox(boxW, boxH, partRects[i], cx-boxW/2, baseY, sceneCx, sceneCy, others)
		cx, baseY = bx+boxW/2, by
		placed = append(placed, screenRect{bx, by, bx + boxW, by + boxH})
		obstacles = append(obstacles, screenRect{bx, by, bx + boxW, by + boxH})
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
		fmt.Fprintf(&sb,
			`<text x="%.2f" y="%.2f" dy=".35em" font-family="%s" font-size="%.1f" font-weight="%s" fill="%s" text-anchor="middle">%s</text>`,
			cx, baseY+boxH/2, escAttr(family), fontSize, escAttr(weight), color, escapeXML(text),
		)
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

func injectCompositeConnectors(svg string, conns []*Connector, infos []partInfo, sceneParts []*CompositePart, nSubstrates int) (string, []screenRect) {
	project := projectIso
	tx, ty := partsScreenOrigin(infos)

	byID := map[string]partInfo{}
	for _, p := range infos {
		if p.id != "" {
			byID[p.id] = p
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
		if c.Routing != "orthogonal" {
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
			srcSideCount[ar.sideKey(effFrom[i])]++
			tgtSideCount[ar.sideKey(effTo[i])]++
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

	var sb strings.Builder
	var overlay strings.Builder
	sb.WriteString(`<g data-layer="connectors">`)

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
	var drawnLines []drawnLine
	for ci, c := range conns {
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
		switch c.Routing {
		case "orthogonal":
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
			const channelW = 14.0
			var sStagger, tStagger float64
			// Collinear edges keep stagger 0 — shifting them would
			// break the single-segment route the user laid out for.
			if !isCollinearConn[ci] {
				srcKey := ar.sideKey(effFrom[ci])
				tgtKey := ar.sideKey(effTo[ci])
				sN, sIdx := srcSideCount[srcKey], srcSideIdx[srcKey]
				tN, tIdx := tgtSideCount[tgtKey], tgtSideIdx[tgtKey]
				srcSideIdx[srcKey]++
				tgtSideIdx[tgtKey]++
				sStagger = (float64(sIdx) - float64(sN-1)/2) * channelW
				tStagger = (float64(tIdx) - float64(tN-1)/2) * channelW
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
					pts = append(pts, [3]float64{tStubX, tStubY, routeZ})
				}
				return pts
			}
			if xFirst {
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
			if (allSameX || allSameY) && allSameZ {
				x1, y1 := project(worldPts[0][0], worldPts[0][1], worldPts[0][2])
				last := worldPts[len(worldPts)-1]
				x2, y2 := project(last[0], last[1], last[2])
				pts = append(pts, [2]float64{x1 + tx, y1 + ty})
				pts = append(pts, [2]float64{x2 + tx, y2 + ty})
				routeWorld = [][2]float64{{worldPts[0][0], worldPts[0][1]}, {last[0], last[1]}}
				routeScreen = [][2]float64{{x1 + tx, y1 + ty}, {x2 + tx, y2 + ty}}
				break
			}
			// v3.1 despike: stub-driven waypoints can walk an axis one way
			// and immediately back (hub→diagonal-neighbour cases), which
			// renders as a doubled line / hook. Collapse any waypoint whose
			// outgoing segment reverses its incoming segment's direction.
			for changed := true; changed; {
				changed = false
				for i := 1; i < len(worldPts)-1; i++ {
					inX, inY := worldPts[i][0]-worldPts[i-1][0], worldPts[i][1]-worldPts[i-1][1]
					outX, outY := worldPts[i+1][0]-worldPts[i][0], worldPts[i+1][1]-worldPts[i][1]
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
		default: // "straight" or empty
			x1, y1, ok1 := ar.screen(c.From)
			x2, y2, ok2 := ar.screen(c.To)
			if !ok1 || !ok2 {
				continue
			}
			pts = [][2]float64{{x1, y1}, {x2, y2}}
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
					p = append(p[:n-2], p[n-1])
					continue
				}
				break
			}
			return p
		}
		pts = trimTail(pts)
		// mirror for the start side
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
		pts = trimTail(pts)
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
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
			if tp, ok := byID[toID]; ok {
				pts = clipRouteEnd(pts, partSilhouette(tp, tx, ty), 2)
			}
			if sp, ok := byID[fromID]; ok {
				pts = clipRouteStart(pts, partSilhouette(sp, tx, ty), 1)
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
		if c.Stroke != nil && c.Stroke.Gradient != nil {
			styleKey += "|grad:" + c.Stroke.Gradient.From + ">" + c.Stroke.Gradient.To
		}
		matched := false
		for _, dl := range drawnLines {
			if dl.key == styleKey && coincidentFwd(pts, dl.pts, 4.0) {
				pts = dl.pts
				matched = true
				break
			}
		}
		if !matched {
			drawnLines = append(drawnLines, drawnLine{styleKey, pts})
		}

		for _, p := range pts {
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
		for i := 1; i < len(pts); i++ {
			inf := width/2 + 3
			segRects = append(segRects, screenRect{
				math.Min(pts[i-1][0], pts[i][0]) - inf, math.Min(pts[i-1][1], pts[i][1]) - inf,
				math.Max(pts[i-1][0], pts[i][0]) + inf, math.Max(pts[i-1][1], pts[i][1]) + inf,
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
			fmt.Fprintf(&overlay,
				`<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="%s"/>`,
				tipX, tipY, b1x, b1y, b2x, b2y, escAttr(stroke),
			)
		}

		// v3.0 — label pill slides along the polyline to a body-free
		// segment (longest first); a midpoint that lands inside a part's
		// projection swallowed the pill entirely under the old rule.
		if strings.TrimSpace(c.Label) != "" && len(pts) >= 2 {
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
			for i := 1; i < len(pts); i++ {
				total += math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			}
			segs := make([]seg, 0, len(pts)-1)
			walked := 0.0
			for i := 1; i < len(pts); i++ {
				l := math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
				segs = append(segs, seg{
					(pts[i-1][0] + pts[i][0]) / 2, (pts[i-1][1] + pts[i][1]) / 2,
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
			placedPills = append(placedPills, screenRect{mx - textW/2, my - 10, mx + textW/2, my + 10})
			trackPt(mx-textW/2, my-10)
			trackPt(mx+textW/2, my+10)
			fmt.Fprintf(&overlay,
				`<rect x="%.2f" y="%.2f" width="%.2f" height="20" rx="4" ry="4" fill="%s"/>`,
				mx-textW/2, my-10, textW, escAttr(bg),
			)
			fmt.Fprintf(&overlay,
				`<text x="%.2f" y="%.2f" dy=".35em" font-family="Inter, sans-serif" font-size="%.1f" font-weight="600" fill="%s" text-anchor="middle">%s</text>`,
				mx, my, lfs, escAttr(ink), escapeXML(c.Label),
			)
		}
	}
	sb.WriteString(`</g>`)

	// Grow the viewBox to cover every connector waypoint (plus a pad
	// for stroke width + arrowheads) BEFORE splicing the layer in, so
	// routes that bend past the part bbox are never clipped.
	if cMinX <= cMaxX {
		const pad = 16.0
		svg = growViewBoxAround(svg, minSvgRect{
			minX: cMinX - pad, minY: cMinY - pad,
			maxX: cMaxX + pad, maxY: cMaxY + pad,
		})
	}

	// v3.0 — splice the route layer above the substrate block: after the
	// nSubstrates-th <g data-part=…> group (substrates are partitioned to
	// the front of the painter order by renderComposite). Routes still
	// paint under every BODY part — iso silhouettes occlude crossing
	// lines — but group slabs no longer swallow them. With nSubstrates ==
	// 0 this degrades to the v1.6.5 behaviour (just after the <svg> tag).
	start := strings.Index(svg, "<svg")
	if start < 0 {
		return svg, segRects
	}
	tagEnd := strings.Index(svg[start:], ">")
	if tagEnd < 0 {
		return svg, segRects
	}
	insertAt := start + tagEnd + 1
	for k := 0; k < nSubstrates; k++ {
		end, ok := partGroupEnd(svg, insertAt)
		if !ok {
			break
		}
		insertAt = end
	}
	svg = svg[:insertAt] + sb.String() + svg[insertAt:]

	// Arrowheads and label pills go in a top overlay so a tip landing on
	// a part's projection (occluded entry face) stays visible.
	if overlay.Len() > 0 {
		if closeIdx := strings.LastIndex(svg, "</svg>"); closeIdx >= 0 {
			svg = svg[:closeIdx] + `<g data-layer="connector-overlay">` + overlay.String() + `</g>` + svg[closeIdx:]
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

// partSilhouette returns the part's screen silhouette in composite
// coords. Shapes with a registered geometry provider (M1+) get their
// EXACT outline; everything else falls back to the bbox hexagon.
func partSilhouette(p partInfo, tx, ty float64) [][2]float64 {
	prov := iso25d.LookupShape(p.shape)
	if prov == nil {
		return silhouetteHex(p, tx, ty)
	}
	var params map[string]any
	if n := prismSidesFor(p.shape, p.sides); n >= 3 {
		params = map[string]any{"sides": n}
	} else if p.sides >= 3 {
		params = map[string]any{"sides": p.sides}
	}
	local := prov.Silhouette(p.w, p.d, p.h, params)
	if len(local) < 3 {
		return silhouetteHex(p, tx, ty)
	}
	// Provider local frame: origin sits at the projected bbox extremes,
	// i.e. world-origin projection + (d*cos30, h). Convert to composite.
	const c30 = 0.8660254037844386
	ox, oy := projectIso(p.offWX, p.offWY, p.offWZ)
	out := make([][2]float64, len(local))
	for i, q := range local {
		out[i] = [2]float64{
			q[0] - p.d*c30 + ox + tx,
			q[1] - p.h + oy + ty,
		}
	}
	return out
}

// silhouetteHex returns the 6-corner screen silhouette of a part's
// world bbox under the iso camera, in composite coords. Non-prismatic
// shapes (cylinder, sphere, cloud) are approximated by their bbox hex —
// slightly loose, which only makes arrow tips sit a touch earlier.
func silhouetteHex(p partInfo, tx, ty float64) [][2]float64 {
	w, d, h := p.w, p.d, p.h
	x, y, z := p.offWX, p.offWY, p.offWZ
	corners := [][3]float64{
		{x, y, z + h}, {x + w, y, z + h}, {x + w, y, z},
		{x + w, y + d, z}, {x, y + d, z}, {x, y + d, z + h},
	}
	out := make([][2]float64, 6)
	for i, c := range corners {
		sx, sy := projectIso(c[0], c[1], c[2])
		out[i] = [2]float64{sx + tx, sy + ty}
	}
	return out
}

func pointInConvex(pt [2]float64, poly [][2]float64) bool {
	n := len(poly)
	sign := 0.0
	for i := 0; i < n; i++ {
		a, b := poly[i], poly[(i+1)%n]
		cross := (b[0]-a[0])*(pt[1]-a[1]) - (b[1]-a[1])*(pt[0]-a[0])
		if math.Abs(cross) < 1e-9 {
			continue
		}
		if sign == 0 {
			sign = cross
		} else if sign*cross < 0 {
			return false
		}
	}
	return true
}

// segPolyEntry returns the intersection of segment a→b with a convex
// polygon's boundary that is closest to a (the outside end).
func segPolyEntry(a, b [2]float64, poly [][2]float64) ([2]float64, bool) {
	bestT := math.Inf(1)
	var best [2]float64
	n := len(poly)
	for i := 0; i < n; i++ {
		p1, p2 := poly[i], poly[(i+1)%n]
		d1x, d1y := b[0]-a[0], b[1]-a[1]
		d2x, d2y := p2[0]-p1[0], p2[1]-p1[1]
		den := d1x*d2y - d1y*d2x
		if math.Abs(den) < 1e-9 {
			continue
		}
		t := ((p1[0]-a[0])*d2y - (p1[1]-a[1])*d2x) / den
		u := ((p1[0]-a[0])*d1y - (p1[1]-a[1])*d1x) / den
		if t >= -1e-9 && t <= 1+1e-9 && u >= -1e-9 && u <= 1+1e-9 && t < bestT {
			bestT = t
			best = [2]float64{a[0] + t*d1x, a[1] + t*d1y}
		}
	}
	return best, !math.IsInf(bestT, 1)
}

// clipRouteEnd trims a polyline where it dives inside `poly` near its
// final point, so the visible line (and its arrowhead) terminate ON the
// part's silhouette instead of underneath the body. The inset pulls the
// tip slightly outside so the triangle stays fully visible.
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

func clipRouteEnd(pts [][2]float64, poly [][2]float64, inset float64) [][2]float64 {
	if len(pts) < 2 || !pointInConvex(pts[len(pts)-1], poly) {
		return pts
	}
	for i := len(pts) - 1; i >= 1; i-- {
		if pointInConvex(pts[i-1], poly) {
			continue
		}
		entry, ok := segPolyEntry(pts[i-1], pts[i], poly)
		if !ok {
			return pts
		}
		dx, dy := entry[0]-pts[i-1][0], entry[1]-pts[i-1][1]
		if l := math.Hypot(dx, dy); l > inset {
			entry[0] -= dx / l * inset
			entry[1] -= dy / l * inset
		}
		return append(append([][2]float64{}, pts[:i]...), entry)
	}
	return pts
}

// clipRouteStart is clipRouteEnd from the other side.
func clipRouteStart(pts [][2]float64, poly [][2]float64, inset float64) [][2]float64 {
	rev := make([][2]float64, len(pts))
	for i, p := range pts {
		rev[len(pts)-1-i] = p
	}
	rev = clipRouteEnd(rev, poly, inset)
	out := make([][2]float64, len(rev))
	for i, p := range rev {
		out[len(rev)-1-i] = p
	}
	return out
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
