package isotopo

import (
	"fmt"
	"math"
	"strings"
)

// planview.go renders a composite scene as a flat TOP-DOWN (plan) view —
// the "projection: top" mode. Height (z) is dropped: every part becomes its
// footprint rectangle at world (wx, wy) sized (w, d), and connectors route
// orthogonally between footprint faces. It reads as a normal 2D node-and-edge
// diagram (a floor plan / flowchart), which is what you want when eyeballing
// the layout rather than the 3D form.
//
// This is a SEPARATE renderer from the isometric pipeline (render.go / iso25d):
// it shares the layout solver (applyLayout) and the style cascade (ResolveStyle)
// but emits its own flat SVG, so isometric output — and every golden file — is
// untouched.

const (
	planPad       = 28.0 // breathing margin around the content bbox (px)
	planRadius    = 6.0
	planFontTitle = 13.0
	planFontLabel = 11.0
)

type planRect struct {
	id    string
	label string
	x, y  float64 // absolute world corner
	w, d  float64
	z, h  float64 // world z-floor + height — kept only so the evaluator can
	// tell same-floor obstacles from things stacked above/below
	fill           string
	stroke         string
	textColor      string
	container      bool
	boundary       bool // dashed outline-only container
	family, weight string
	fontSize       float64
	parent         int // planCollect index; -1 for roots
}

// planEdge is one connector resolved to a world-space orthogonal route between
// two footprints — the shared geometry the renderer draws and the evaluator
// measures.
type planEdge struct {
	ci       int // index in the scene's Connectors (matches data-connector)
	c        *Connector
	from, to planRect
	pts      [][2]float64 // world route, source→target
	refined  bool         // content-aware detours also contribute their ink to export bounds
	framePts [][2]float64 // retain the baseline coordinate origin for fixed-port detours
}

// buildPlanModel resolves a scene to its flat geometry with the scorecard-
// guided router (the better out-of-box default).
func buildPlanModel(n *Node, theme *Theme, canvas *Canvas) (rects []planRect, byID map[string]planRect, edges []planEdge) {
	return buildPlanModelOpt(n, theme, canvas, true)
}

// buildPlanModelOpt resolves footprint rects (planCollect order, containers
// before children), an id index, and the routed edges. With optimize=false it
// uses the baseline dominant-axis router; with optimize=true it picks, per edge,
// the candidate route that minimises the scorecard cost (tunnelling, crossings,
// bends, length) against the nodes and the edges placed so far. The two modes
// share everything else, so an A/B holds node positions fixed and varies only
// the routing decision.
func resolvedPlanGeometry(n *Node, theme *Theme, canvas *Canvas) (*Node, []planRect) {
	n = resolvedLayout(n, canvas)
	var rects []planRect
	if n.Shape == "composite" {
		planCollect(n.Parts, 0, 0, 0, theme, &rects, n.GridStep)
	} else {
		st := ResolveStyleWithRole(theme, n.Shape, n.Role, n.Preset, n.Style)
		w, d, h := planDims(n.Shape, n.Geom)
		rects = append(rects, planRectFor(n.Label, n.Label, 0, 0, 0, w, d, h, n.Shape, st, false))
	}
	return n, rects
}

func buildPlanModelOpt(n *Node, theme *Theme, canvas *Canvas, optimize bool) (rects []planRect, byID map[string]planRect, edges []planEdge) {
	p := resolvePlanScene(n, theme, canvas, optimize, false)
	return p.rects, p.byID, p.edges
}

// Keep the original router available as the complete compatibility baseline.
// Refinement only replaces fully verified automatic routes.
func buildPlanModelBase(n *Node, theme *Theme, canvas *Canvas, optimize bool) (rects []planRect, byID map[string]planRect, edges []planEdge) {
	n, rects = resolvedPlanGeometry(n, theme, canvas)
	byID, edges = routePlanGeometry(n, rects, optimize)
	return
}

func routePlanGeometry(n *Node, rects []planRect, optimize bool) (byID map[string]planRect, edges []planEdge) {
	byID = map[string]planRect{}
	var leaves []planRect
	for _, r := range rects {
		if r.id != "" {
			byID[r.id] = r
		}
		if !r.container {
			leaves = append(leaves, r)
		}
	}
	var placed [][][2]float64
	for ci, c := range n.Connectors {
		if c == nil {
			continue
		}
		fr, okF := byID[connectorTarget(c.From)]
		to, okT := byID[connectorTarget(c.To)]
		// Plan collapses the displayed stack, but replica references still
		// connect to that footprint instead of silently losing the edge.
		if !okF {
			fr, okF = byID[stackBase(connectorTarget(c.From))]
		}
		if !okT {
			to, okT = byID[stackBase(connectorTarget(c.To))]
		}
		if !okF || !okT {
			continue // dangling endpoint — Validate already flags it
		}
		var pts [][2]float64
		switch {
		case len(c.Waypoints3D) > 0 && (c.Routing == "orthogonal" || c.Routing == "orthogonal2"):
			a, b := planConnectionPort(fr, to, c.From), planConnectionPort(to, fr, c.To)
			path := [][3]float64{{a.WX, a.WY, 0}}
			for _, wp := range c.Waypoints3D {
				path = append(path, [3]float64{wp.WX, wp.WY, 0})
			}
			path = append(path, [3]float64{b.WX, b.WY, 0})
			for _, p := range orthogonalThread3D(path) {
				pts = append(pts, [2]float64{p[0], p[1]})
			}
		case isFixedPortRef(c.From) || isFixedPortRef(c.To):
			pts = planFixedPortRoute(fr, to, c, leaves, placed)
		case c.Routing == "straight":
			// A straight connector renders as a direct line, so it must be
			// SCORED as one — not the orthogonal staircase the router would pick.
			// Otherwise two crossing straight diagonals (or a line tunnelling a
			// node) read as clean, because the staircase avoids what the straight
			// line does not.
			pts = [][2]float64{
				{fr.x + fr.w/2, fr.y + fr.d/2},
				{to.x + to.w/2, to.y + to.d/2},
			}
		case optimize:
			pts = selectRoute(fr, to, leaves, placed)
		default:
			pts = planRoute(fr, to)
		}
		placed = append(placed, pts)
		edges = append(edges, planEdge{ci: ci, c: c, from: fr, to: to, pts: pts})
	}
	return
}

// planRouteCandidates enumerates the orthogonal 2-bend routes between two
// footprints: an x-first and a y-first staircase, each with the cross leg at
// the midpoint OR flush against the target. The selector scores these and keeps
// the cheapest; the baseline planRoute is just the dominant-axis midpoint pick.
func planRouteCandidates(fr, to planRect) [][][2]float64 {
	fcx, fcy := fr.x+fr.w/2, fr.y+fr.d/2
	tcx, tcy := to.x+to.w/2, to.y+to.d/2
	sx, ex := fr.x+fr.w, to.x
	if tcx < fcx {
		sx, ex = fr.x, to.x+to.w
	}
	sy, ey := fr.y+fr.d, to.y
	if tcy < fcy {
		sy, ey = fr.y, to.y+to.d
	}
	var c [][][2]float64
	for _, mx := range []float64{(sx + ex) / 2, ex} { // x-first staircases
		c = append(c, [][2]float64{{sx, fcy}, {mx, fcy}, {mx, tcy}, {ex, tcy}})
	}
	for _, my := range []float64{(sy + ey) / 2, ey} { // y-first staircases
		c = append(c, [][2]float64{{fcx, sy}, {fcx, my}, {tcx, my}, {tcx, ey}})
	}
	// Outward U-routes keep a real lead at both ports when the midpoint
	// channels collapse or an obstacle blocks the direct corridor. In
	// particular, equal-height nodes cannot safely detour along their own
	// top/bottom borders using a zero-length terminal leg.
	const lead = 12.0
	for _, side := range []int{-1, 1} {
		fy, ty, channel := fr.y, to.y, math.Min(fr.y, to.y)-lead
		if side > 0 {
			fy, ty, channel = fr.y+fr.d, to.y+to.d, math.Max(fr.y+fr.d, to.y+to.d)+lead
		}
		c = append(c, [][2]float64{{fcx, fy}, {fcx, channel}, {tcx, channel}, {tcx, ty}})
		fx, tx, channel := fr.x, to.x, math.Min(fr.x, to.x)-lead
		if side > 0 {
			fx, tx, channel = fr.x+fr.w, to.x+to.w, math.Max(fr.x+fr.w, to.x+to.w)+lead
		}
		c = append(c, [][2]float64{{fx, fcy}, {channel, fcy}, {channel, tcy}, {tx, tcy}})
	}
	return c
}

// selectRoute picks the candidate route with the lowest scorecard cost given
// the node obstacles and the routes already placed this pass.
func selectRoute(fr, to planRect, nodes []planRect, placed [][][2]float64) [][2]float64 {
	best := planRoute(fr, to)
	bestCost := math.Inf(1)
	if planAttachmentsValid(best, fr, to) {
		bestCost = routeCost(best, fr, to, nodes, placed)
	}
	for _, cand := range planRouteCandidates(fr, to) {
		if !planAttachmentsValid(cand, fr, to) {
			continue
		}
		if cost := routeCost(cand, fr, to, nodes, placed); cost < bestCost {
			best, bestCost = cand, cost
		}
	}
	return best
}

// A route must leave and enter a face transversely. A zero terminal leg in a
// flush candidate can otherwise win the bend score while placing the arrow
// along the target's border, which visually loses the connection entirely.
func planAttachmentsValid(pts [][2]float64, fr, to planRect) bool {
	if len(pts) < 2 {
		return false
	}
	outward := func(r planRect, p, q [2]float64) bool {
		const eps = 1e-7
		dx, dy := q[0]-p[0], q[1]-p[1]
		if math.Abs(p[0]-r.x) < eps {
			return dx < -eps && math.Abs(dy) < eps
		}
		if math.Abs(p[0]-(r.x+r.w)) < eps {
			return dx > eps && math.Abs(dy) < eps
		}
		if math.Abs(p[1]-r.y) < eps {
			return dy < -eps && math.Abs(dx) < eps
		}
		if math.Abs(p[1]-(r.y+r.d)) < eps {
			return dy > eps && math.Abs(dx) < eps
		}
		return false
	}
	i, j := 1, len(pts)-2
	for i < len(pts) && pts[i] == pts[0] {
		i++
	}
	for j >= 0 && pts[j] == pts[len(pts)-1] {
		j--
	}
	return i < len(pts) && j >= 0 && outward(fr, pts[0], pts[i]) && outward(to, pts[len(pts)-1], pts[j])
}

// routeCost is the per-edge slice of the scorecard: tunnelling a node is worst,
// then crossing an existing edge, then bends, then a small length tiebreak.
func routeCost(pts [][2]float64, fr, to planRect, nodes []planRect, placed [][][2]float64) float64 {
	cost := 0.0
	ez := edgeZLevel(fr, to)
	for _, r := range nodes {
		if r.id == fr.id || r.id == to.id || r.h <= planThinH ||
			!sameFloor(ez, r) || enclosesBoth(r, fr, to) {
			continue
		}
		if routeHitsRect(pts, r) {
			cost += 1000
		}
	}
	for _, pl := range placed {
		for a := 1; a < len(pts); a++ {
			for b := 1; b < len(pl); b++ {
				if _, ok := segInt(pts[a-1], pts[a], pl[b-1], pl[b]); ok {
					cost += 100
				}
			}
		}
	}
	cost += 3 * float64(planBends(pts))
	cost += 0.01 * planLen(pts)
	return cost
}

// RenderPlan emits the top-down plan SVG for a scene. A nil or empty scene
// yields "". Non-composite single nodes render as a single footprint.
func RenderPlan(n *Node, theme *Theme, canvas *Canvas, _ []*Annotation) string {
	return renderPlan(n, theme, canvas, nil)
}

// renderPlan does the work; a non-nil report overlays its findings (crossings,
// edges through nodes) in red on top of the diagram.
func renderPlan(n *Node, theme *Theme, canvas *Canvas, hl *PlanReport) string {
	return renderPlanObserved(n, theme, canvas, hl, nil)
}

func renderPlanObserved(n *Node, theme *Theme, canvas *Canvas, hl *PlanReport, labels *[]ConnectorLabelRecord, conflicts ...bool) string {
	return renderPlanContentObserved(n, theme, canvas, hl, labels, nil, conflicts...)
}

func renderPlanContentObserved(n *Node, theme *Theme, canvas *Canvas, hl *PlanReport, labels *[]ConnectorLabelRecord, titles *[]PlanLabel, conflicts ...bool) string {
	collectConflicts := labels != nil && len(conflicts) > 0 && conflicts[0]
	if n == nil {
		return ""
	}
	p := resolvePlanScene(n, theme, canvas, true, collectConflicts)
	return renderResolvedPlan(p, canvas, hl, labels, titles)
}

func renderResolvedPlan(p *resolvedPlanScene, canvas *Canvas, hl *PlanReport, labels *[]ConnectorLabelRecord, titles *[]PlanLabel) string {
	rects, edges := p.rects, p.edges
	if len(rects) == 0 {
		return ""
	}

	ox, oy, W, H := p.ox, p.oy, p.width, p.height
	tx := func(x float64) float64 { return x + ox }
	ty := func(y float64) float64 { return y + oy }
	content := p.layoutContent()
	collectConflicts := p.details

	var sb strings.Builder
	fmt.Fprintf(&sb,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.2f %.2f" width="%.2f" height="%.2f" font-family="Inter,system-ui,sans-serif" data-projection="top" data-scene-tx="%.4f" data-scene-ty="%.4f">`,
		W, H, W, H, tx(0), ty(0))

	// Background + arrowhead marker.
	bg := "#ffffff"
	if canvas != nil && canvas.Background != "" {
		bg = canvas.Background
	}
	// Connector ink is a saturated dark slate that pops on the light lane
	// substrates and white boxes; a light CASING (drawn under each line, see
	// below) carries it across the dark gaps between lanes. This dark-line +
	// light-halo pair stays legible on every background the route crosses —
	// the iso author stroke colours, tuned for 3D faces, would vanish on one.
	edgeInk := "#27364b"
	fmt.Fprintf(&sb, `<rect x="0" y="0" width="%.2f" height="%.2f" fill="%s"/>`, W, H, planEsc(bg))
	fmt.Fprintf(&sb, `<defs><marker id="planarrow" viewBox="0 0 10 10" refX="8.5" refY="5" markerWidth="9" markerHeight="9" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="%s"/></marker></defs>`, edgeInk)

	texts := content.texts
	fonts := map[string]bool{}
	drawRect := func(i int, r planRect) {
		if r.id != "" {
			fmt.Fprintf(&sb, `<g data-part-id="%s">`, planEsc(r.id))
			defer sb.WriteString(`</g>`)
		}
		px, py := tx(r.x), ty(r.y)
		fill := r.fill
		dash := ""
		if r.boundary {
			dash = ` stroke-dasharray="6 4"`
			fill = "none"
		}
		fmt.Fprintf(&sb,
			`<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s" stroke-width="%.1f"%s/>`,
			px, py, r.w, r.d, planRadius, planRadius, planEsc(fill), planEsc(r.stroke), planLineW(r), dash)
		if r.label == "" {
			return
		}
		if titles != nil {
			record := texts[i].record
			// Routing gates need detailed conflicts even for a light preview.
			// Preserve that internal proof without publishing report-only data.
			if !collectConflicts && record.Placement != nil && len(record.Placement.Conflicts) > 0 {
				placement := *record.Placement
				placement.Conflicts = nil
				record.Placement = &placement
			}
			*titles = append(*titles, record)
		}
		if texts[i].font != nil {
			emitPlanLabel(&sb, texts[i], r.textColor, fonts)
			return
		}
		if r.container {
			// lane label hugs the top-left, inside the child-free padding strip.
			fmt.Fprintf(&sb,
				`<text x="%.2f" y="%.2f" font-size="%.1f" font-weight="600" fill="%s">%s</text>`,
				px+8, py+16, planFontTitle, planEsc(r.textColor), planEsc(r.label))
		} else {
			fmt.Fprintf(&sb,
				`<text x="%.2f" y="%.2f" font-size="%.1f" text-anchor="middle" dominant-baseline="central" fill="%s">%s</text>`,
				px+r.w/2, py+r.d/2, planFontLabel, planEsc(r.textColor), planEsc(r.label))
		}
	}

	// Paint order: substrates → connectors → leaf boxes → connector labels, so
	// routes tuck BEHIND the node boxes (no lines crossing labels) while the
	// pills stay on top. Containers come first in `rects` (planCollect order).
	for i, r := range rects {
		if r.container {
			drawRect(i, r)
		}
	}

	for _, e := range edges {
		stroke, casing, sw, dash := planEdgeStyle(e.c)
		path := planPathStr(e.pts, tx, ty)
		head := ` marker-end="url(#planarrow)"`
		if e.c.Arrow == "none" {
			head = ""
		}
		if dash == "" {
			fmt.Fprintf(&sb, `<path d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"/>`, path, planEsc(casing), sw+3)
		}
		fmt.Fprintf(&sb, `<path d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linecap="round" stroke-linejoin="round"%s%s/>`, path, planEsc(stroke), sw, dash, head)
	}
	for i, r := range rects {
		if !r.container {
			drawRect(i, r)
		}
	}
	for _, p := range content.pills {
		planLegacyPill(&sb, p)
	}
	measuredBounds := content.bounds
	var measuredRecords []ConnectorLabelRecord
	for i, req := range content.requests {
		bg, ink := req.c.LabelBg, req.c.LabelColor
		if bg == "" {
			bg = "#ffffff"
		}
		if ink == "" {
			ink = "#334155"
		}
		label := emitConnectorText(&sb, req, content.placements[i], bg, ink, fonts)
		if content.hasTitleInk {
			label.Typography.Scope += ",plan-title-ink-aabb"
		}
		measuredRecords = append(measuredRecords, ConnectorLabelRecord{req.index, label})
		if labels != nil {
			*labels = append(*labels, ConnectorLabelRecord{req.index, label})
		}
		measuredBounds = append(measuredBounds, connectorLabelRect(label))
	}

	// Evaluation overlay: redraw each through-node edge in red, then mark every
	// crossing with a red ✕, so layout problems jump out on the diagram.
	if hl != nil {
		for _, pe := range hl.ProblemEdges {
			fmt.Fprintf(&sb,
				`<path d="%s" fill="none" stroke="#e11d48" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="5 4"/>`,
				planPathStr(pe.Pts, tx, ty))
		}
		for _, x := range hl.CrossingsAt {
			cx, cy := tx(x.X), ty(x.Y)
			fmt.Fprintf(&sb,
				`<path d="M%.2f %.2f L%.2f %.2f M%.2f %.2f L%.2f %.2f" stroke="#e11d48" stroke-width="2.4" stroke-linecap="round"/>`,
				cx-5, cy-5, cx+5, cy+5, cx-5, cy+5, cx+5, cy-5)
		}
	}

	sb.WriteString(`</svg>`)
	svg := finishConnectorText(sb.String(), measuredRecords)
	if collectConflicts {
		observeConnectorTextConflicts(measuredRecords, content.targets, content.strokes)
	}
	if len(measuredBounds) > 0 {
		// Let viewport growth resize the existing backdrop, including dark canvases.
		svg = strings.Replace(svg, `<rect x="0" y="0"`, `<rect data-layer="canvas-bg" x="0" y="0"`, 1)
	}
	for _, r := range measuredBounds {
		svg = growViewBoxAround(svg, minSvgRect{r.x0 - 16, r.y0 - 16, r.x1 + 16, r.y1 + 16})
	}
	ar := 16.0 / 10.0
	if canvas != nil && canvas.AspectRatio != 0 {
		ar = canvas.AspectRatio
	}
	return ceilOuterDims(enforceAspectRatio(svg, ar))
}

// planCollect walks the part tree, accumulating each part's ABSOLUTE world
// corner (parent corner + own resolved offset). applyLayout has already filled
// in computed footprints (auto-sized containers included) and parent-relative
// offsets, so a plain accumulation is exact.
func planCollect(parts []*CompositePart, ox, oy, oz float64, theme *Theme, out *[]planRect, grid ...float64) {
	gridStep := 0.0
	if len(grid) > 0 {
		gridStep = grid[0]
	}
	planCollectAt(parts, ox, oy, oz, theme, out, gridStep, -1)
}

func planCollectAt(parts []*CompositePart, ox, oy, oz float64, theme *Theme, out *[]planRect, gridStep float64, parent int) {
	for _, p := range parts {
		if p == nil {
			continue
		}
		x, y, z := partLocalOffset(p, gridStep)
		x, y, z = x+ox, y+oy, z+oz
		w, d, h := planDims(p.Shape, p.Geom)
		container := isContainerShape(p.Shape) || len(p.Parts) > 0
		st := ResolveStyleWithRole(theme, p.Shape, p.Role, p.Preset, p.Style)
		i := len(*out)
		r := planRectFor(p.ID, p.Label, x, y, z, w, d, h, p.Shape, st, container)
		r.parent = parent
		*out = append(*out, r)
		if len(p.Parts) > 0 {
			childZ := z
			if isContainerShape(p.Shape) {
				childZ += groupSubstrateHeight(p)
			}
			planCollectAt(p.Parts, x, y, childZ, theme, out, gridStep, i)
		}
	}
}

func planDims(shape string, g *Geom) (w, d, h float64) {
	return partDimensions(&CompositePart{Shape: shape, Geom: g})
}

func planRectFor(id, label string, x, y, z, w, d, h float64, shape string, st *Style, container bool) planRect {
	r := planRect{id: id, label: label, x: x, y: y, z: z, w: w, d: d, h: h, container: container, parent: -1}
	r.fontSize, r.weight = planFontLabel, "400"
	if container {
		r.fontSize, r.weight = planFontTitle, "600"
	}
	if st != nil && st.Text != nil {
		r.family = st.Text.Family
		if st.Text.Weight != "" {
			r.weight = st.Text.Weight
		}
		if st.Text.Size != nil {
			r.fontSize = *st.Text.Size
		}
	}
	r.boundary = shape == "boundary"
	// Fill from the resolved top face (gradient → its start colour).
	fill := "#cfd8e3"
	if st != nil && st.Palette != nil {
		if st.Palette.Top != "" {
			fill = st.Palette.Top
		} else if st.Palette.TopGradient != nil && st.Palette.TopGradient.From != "" {
			fill = st.Palette.TopGradient.From
		}
	}
	r.fill = fill
	r.stroke = "#334155"
	if st != nil && st.Stroke != nil && st.Stroke.Color != "" {
		r.stroke = st.Stroke.Color
	}
	if container && !r.boundary {
		// translucent substrate so children read on top; lane label stays dark.
		r.fill = planTint(fill)
		r.textColor = "#475569"
	} else {
		// Leaf label: pick black/white by the footprint's luminance. The
		// theme's own text colour is tuned for the iso TOP face (often a dark
		// slab), so honouring it here would print invisible light-on-light.
		if planIsDark(r.fill) {
			r.textColor = "#f1f5f9"
		} else {
			r.textColor = "#0f172a"
		}
	}
	return r
}

func planLineW(r planRect) float64 {
	if r.container {
		return 1.4
	}
	return 1.2
}

// planRoute builds an orthogonal route between two footprints along the
// dominant axis and returns its corner points in WORLD coords, source→target.
// Both the renderer and the layout evaluator read these same points, so the
// metrics measure exactly what is drawn.
func planRoute(fr, to planRect) [][2]float64 {
	fcx, fcy := fr.x+fr.w/2, fr.y+fr.d/2
	tcx, tcy := to.x+to.w/2, to.y+to.d/2
	if math.Abs(tcx-fcx) >= math.Abs(tcy-fcy) {
		// horizontal-dominant: exit left/right faces, elbow at mid-x.
		sx, ex := fr.x+fr.w, to.x
		if tcx < fcx {
			sx, ex = fr.x, to.x+to.w
		}
		midx := (sx + ex) / 2
		return [][2]float64{{sx, fcy}, {midx, fcy}, {midx, tcy}, {ex, tcy}}
	}
	// vertical-dominant: exit top/bottom faces, elbow at mid-y.
	sy, ey := fr.y+fr.d, to.y
	if tcy < fcy {
		sy, ey = fr.y, to.y+to.d
	}
	midy := (sy + ey) / 2
	return [][2]float64{{fcx, sy}, {fcx, midy}, {tcx, midy}, {tcx, ey}}
}

// planPathStr renders a world-space polyline to an SVG path in screen coords.
func planPathStr(pts [][2]float64, tx, ty func(float64) float64) string {
	var b strings.Builder
	for i, p := range pts {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&b, "%s%.2f %.2f ", cmd, tx(p[0]), ty(p[1]))
	}
	return strings.TrimSpace(b.String())
}

// planMid returns the midpoint of a route's middle (cross) segment in world
// coords — where a connector label sits.
func planMid(pts [][2]float64) (x, y float64) {
	if len(pts) < 2 {
		return 0, 0
	}
	i := len(pts) / 2
	return (pts[i-1][0] + pts[i][0]) / 2, (pts[i-1][1] + pts[i][1]) / 2
}

// planTint lightens a hex colour toward white for substrate fills; non-hex
// inputs pass through (the renderer falls back to the raw value).
func planTint(hex string) string {
	r, g, b, ok := parseHex6(hex)
	if !ok {
		return hex
	}
	mix := func(c int) int { return c + (255-c)*72/100 } // 72% toward white
	return fmt.Sprintf("#%02X%02X%02X", mix(r), mix(g), mix(b))
}

// planIsDark reports whether a colour is dark enough to want light ink on top
// (relative luminance < 0.5). Non-hex inputs default to light (false).
func planIsDark(hex string) bool {
	r, g, b, ok := parseHex6(hex)
	if !ok {
		return false
	}
	lum := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
	return lum < 0.5
}

func parseHex6(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	var v int
	if _, err := fmt.Sscanf(s[1:], "%06x", &v); err != nil {
		return 0, 0, 0, false
	}
	return v >> 16 & 0xff, v >> 8 & 0xff, v & 0xff, true
}

func planEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
