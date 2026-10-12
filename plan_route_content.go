package isotopo

import (
	"math"
	"slices"
	"sort"
	"strings"
)

// Author-authored bends, waypoints, straight lines and legacy anchors are not
// routing hints. A fixed contour port is supported without moving either end.
func planRouteAutomatic(e planEdge) bool {
	c := e.c
	if c == nil || e.from.id == e.to.id || c.Bend != nil || c.Elbow != "" || len(c.Waypoints) > 0 || len(c.Waypoints3D) > 0 {
		return false
	}
	if c.Routing != "" && c.Routing != "orthogonal" && c.Routing != "orthogonal2" {
		return false
	}
	for _, ref := range []string{c.From, c.To} {
		if strings.Contains(ref, ".") && !isFixedPortRef(ref) {
			return false
		}
	}
	return true
}

// Title translation is cheaper and preserves every existing route. Only its
// unresolved route/arrow conflicts trigger this second, bounded search. Every
// candidate is checked as a complete content arrangement before submission.
func refinePlanRoutes(rects []planRect, edges []planEdge, canvas *Canvas) []planEdge {
	return refinePlanRoutesWithBudget(rects, edges, canvas, 32)
}

func refinePlanRoutesWithBudget(rects []planRect, edges []planEdge, canvas *Canvas, budget int) []planEdge {
	ox, oy, _, _ := planFrame(rects, edges, canvas)
	edges, _ = refinePlanRoutesContent(rects, edges, ox, oy, budget)
	return edges
}

// The accepted whole-scene content is already the final layout. Return it to
// consumers rather than discarding it and shaping/searching everything again.
// A nil content means no observation was needed, not an empty text layer.
func refinePlanRoutesContent(rects []planRect, edges []planEdge, ox, oy float64, budget int) ([]planEdge, *planContentLayout) {
	if budget <= 0 || len(rects) == 0 || len(edges) == 0 {
		return edges, nil
	}
	known, automatic := false, false
	for _, r := range rects {
		known = known || r.family != "" && r.label != ""
	}
	for _, e := range edges {
		automatic = automatic || planRouteAutomatic(e)
	}
	if !known || !automatic {
		return edges, nil
	}
	before := resolvePlanContent(rects, edges, ox, oy, true)
	for i, r := range rects {
		if r.label != "" && before.texts[i].font == nil {
			return edges, &before // unknown ink cannot certify another route's safety
		}
	}
	if len(before.pills) > 0 {
		return edges, &before
	}
	out := slices.Clone(edges)
	for i, e := range out {
		if budget <= 0 {
			break
		}
		if !planRouteAutomatic(e) || !planRouteBlocksTitle(before, e.ci) {
			continue
		}
		oldDefects, blocked := planRouteContentDefects(before)
		best, bestContent, bestBlocked := e, before, blocked
		bestCost := math.Inf(1)
		for _, pts := range planContentRouteCandidates(e, rects, before, ox, oy) {
			if budget <= 0 {
				break
			}
			budget--
			if slices.Equal(pts, e.pts) || !planAttachmentsValid(pts, e.from, e.to) {
				continue
			}
			cand := e
			cand.pts, cand.refined = pts, true
			cand.framePts = e.pts
			if !planRouteSurfaceClear(cand, rects, out, ox, oy) {
				continue
			}
			trial := slices.Clone(out)
			trial[i] = cand
			after := resolvePlanContent(rects, trial, ox, oy, true)
			defects, count := planRouteContentDefects(after)
			if count >= blocked || !defects.noWorseThan(oldDefects) || !planRoutePreservesClear(before, after, e.ci) {
				continue
			}
			cost := float64(planBends(pts))*1000 + planLen(pts)
			if count < bestBlocked || count == bestBlocked && cost < bestCost {
				best, bestContent, bestBlocked, bestCost = cand, after, count, cost
			}
		}
		if best.refined {
			out[i], before = best, bestContent
		}
	}
	return refinePlanRouteLanes(rects, out, before, ox, oy)
}

func planRouteBlocksTitle(content planContentLayout, edge int) bool {
	for _, t := range content.texts {
		if p := t.record.Placement; p != nil && p.Status == "blocked" {
			for _, c := range p.Conflicts {
				if (c.Kind == "route" || c.Kind == "arrow") && c.ConnectorIndex != nil && *c.ConnectorIndex == edge {
					return true
				}
			}
		}
	}
	return false
}

// Reuse the final label observer without formatting SVG or re-entering a
// scene renderer (which would recursively call this router).
func planRouteContentDefects(content planContentLayout) (visualDefects, int) {
	d := visualDefects{}
	addPlanTitlePlacementDefects(d, content.texts)
	blocked := 0
	for _, t := range content.texts {
		if p := t.record.Placement; p != nil && p.Status == "blocked" {
			blocked++
		}
	}
	var records []ConnectorLabelRecord
	for i, req := range content.requests {
		p := content.placements[i]
		state := "clear-within-scope"
		if !p.clear {
			state = "blocked"
			d[visualDefect{kind: "connector-label-blocked", edge: req.index}] = 1
		}
		records = append(records, ConnectorLabelRecord{req.index, ConnectorLabel{
			Rect:       [4]float64{p.box.x0, p.box.y0, p.box.x1 - p.box.x0, p.box.y1 - p.box.y0},
			Typography: &ConnectorTypography{Placement: state},
		}})
	}
	observeConnectorTextConflicts(records, slices.Clone(content.targets), content.strokes)
	addConnectorTextConflictDefects(d, records)
	return d, blocked
}

func planRoutePreservesClear(before, after planContentLayout, changedEdge int) bool {
	for i, t := range before.texts {
		if p := t.record.Placement; p != nil && p.Status == "clear-within-scope" {
			u := after.texts[i]
			if len(t.record.Lines) != len(u.record.Lines) {
				return false
			}
			for j, l := range t.record.Lines {
				m := u.record.Lines[j]
				if l.Text != m.Text || l.X != m.X || l.Baseline != m.Baseline {
					return false
				}
			}
		}
	}
	for i, p := range before.placements {
		q := after.placements[i]
		if p.clear && (!q.clear || before.requests[i].index != changedEdge && p.box != q.box) {
			return false
		}
	}
	return true
}

func planRouteWidth(c *Connector) float64 {
	_, _, w, dash := planEdgeStyle(c)
	if dash == "" {
		w += 3
	}
	return svgCoord(w)
}

// The marker's rear is 8.5 viewBox units behind its reference point, scaled
// by markerHeight/viewBoxHeight and the serialized stroke width. Keep it off
// the preceding elbow, including casing and a six-unit reading gap.
func planRouteLead(c *Connector) float64 {
	if c.Arrow == "none" {
		return 12
	}
	_, _, sw, _ := planEdgeStyle(c)
	return math.Max(12, 8.5*.9*svgCoord(sw)+planRouteWidth(c)/2+6.1)
}

func planRouteScreenPoints(e planEdge, ox, oy float64) [][2]float64 {
	out := make([][2]float64, len(e.pts))
	for i, p := range e.pts {
		out[i] = [2]float64{svgCoord(p[0] + ox), svgCoord(p[1] + oy)}
	}
	return out
}

func planRouteArrow(e planEdge, points [][2]float64) (screenRect, bool) {
	if e.c.Arrow == "none" || len(points) == 0 {
		return screenRect{}, false
	}
	_, _, sw, _ := planEdgeStyle(e.c)
	p := points[len(points)-1]
	r := math.Hypot(8.5, 5)*.9*svgCoord(sw) + .03
	return screenRect{p[0] - r, p[1] - r, p[0] + r, p[1] + r}, true
}

func planStrokeBox(s connectorTextStroke) screenRect {
	return screenRect{math.Min(s.a[0], s.b[0]), math.Min(s.a[1], s.b[1]), math.Max(s.a[0], s.b[0]), math.Max(s.a[1], s.b[1])}.inflate(s.radius)
}

// A rerouted edge must clear ALL unrelated leaf footprints (even another z
// floor), strokes and arrows. This deliberately declines congested shared-port
// hubs: common ownership alone must not exempt a long coincident segment.
// Only the edge's own source/target contacts and own arrow are exempt.
func planRouteSurfaceClear(e planEdge, rects []planRect, edges []planEdge, ox, oy float64) bool {
	if routeHitsRect(e.pts, e.from) || routeHitsRect(e.pts, e.to) {
		return false // no re-entry through an endpoint after its outward lead
	}
	points := planRouteScreenPoints(e, ox, oy)
	strokes := connectorTextConflictStrokes(connectorTextStrokes(e.ci, points, false, planRouteWidth(e.c)))
	arrow, hasArrow := planRouteArrow(e, points)
	if hasArrow && len(points) > 2 {
		a, b := points[len(points)-2], points[len(points)-1]
		if math.Hypot(a[0]-b[0], a[1]-b[1]) < planRouteLead(e.c)-.01 {
			return false
		}
	}
	for _, r := range rects {
		if r.container && r.id != e.from.id && r.id != e.to.id {
			continue
		}
		x, y := svgCoord(r.x+ox), svgCoord(r.y+oy)
		body := screenRect{x, y, x + svgCoord(r.w), y + svgCoord(r.d)}.inflate(planLineW(r) / 2)
		for i, s := range strokes {
			if r.id == e.from.id && i == 0 || r.id == e.to.id && i == len(strokes)-1 {
				continue // only the actual terminal segment may dock here
			}
			if connectorTextStrokeShortfall(body, s) > 0 {
				return false
			}
		}
		if hasArrow && r.id != e.to.id && connectorTextRectShortfall(body, arrow) > 0 {
			return false
		}
	}
	for _, other := range edges {
		if other.ci == e.ci {
			continue
		}
		p := planRouteScreenPoints(other, ox, oy)
		otherStrokes := connectorTextStrokes(other.ci, p, false, planRouteWidth(other.c))
		otherArrow, otherHasArrow := planRouteArrow(other, p)
		for _, s := range strokes {
			box := planStrokeBox(s) // candidate segments are orthogonal
			for _, o := range otherStrokes {
				if connectorTextStrokeShortfall(box, o) > 0 {
					return false
				}
			}
			if otherHasArrow && connectorTextRectShortfall(box, otherArrow) > 0 {
				return false
			}
		}
		if hasArrow {
			for _, s := range otherStrokes {
				if connectorTextStrokeShortfall(arrow, s) > 0 {
					return false
				}
			}
			if otherHasArrow && connectorTextRectShortfall(arrow, otherArrow) > 0 {
				return false
			}
		}
	}
	for i, s := range strokes {
		for j := i + 2; j < len(strokes); j++ {
			if connectorTextStrokeShortfall(planStrokeBox(s), strokes[j]) > 0 {
				return false
			}
		}
	}
	return true
}

func planContentRouteCandidates(e planEdge, rects []planRect, content planContentLayout, ox, oy float64) [][][2]float64 {
	gap := planRouteWidth(e.c)/2 + 6.1
	var xs, ys []float64
	leadLength := planRouteLead(e.c)
	for _, r := range []planRect{e.from, e.to} {
		xs = append(xs, r.x-leadLength, r.x+r.w+leadLength)
		ys = append(ys, r.y-leadLength, r.y+r.d+leadLength)
	}
	for i, t := range content.texts {
		if len(xs) >= 128 || len(ys) >= 128 {
			break
		}
		if p := t.record.Placement; p == nil || p.Status != "blocked" {
			continue
		}
		for _, b := range t.inkRects() {
			if len(xs) >= 124 || len(ys) >= 124 {
				break
			}
			xs = append(xs, b.x0-ox-gap, b.x1-ox+gap)
			ys = append(ys, b.y0-oy-gap, b.y1-oy+gap)
		}
		r := rects[i]
		xs = append(xs, r.x-gap, r.x+r.w+gap)
		ys = append(ys, r.y-gap, r.y+r.d+gap)
	}
	// The nearest node-side corridors supplement title boundaries. Limit hint
	// growth independently of the global full-candidate evaluation budget.
	for _, r := range rects {
		if len(xs) >= 128 || len(ys) >= 128 {
			break
		}
		if !r.container {
			xs = append(xs, r.x-gap-1, r.x+r.w+gap+1)
			ys = append(ys, r.y-gap-1, r.y+r.d+gap+1)
		}
	}
	var out [][][2]float64
	fixed := isFixedPortRef(e.c.From) || isFixedPortRef(e.c.To)
	if !fixed {
		out = planRouteCandidates(e.from, e.to)
	}
	for axis, channels := range [][]float64{xs, ys} {
		for _, channel := range channels {
			if !fixed {
				a, b := [2]float64{e.from.x + e.from.w/2, e.from.y + e.from.d/2}, [2]float64{e.to.x + e.to.w/2, e.to.y + e.to.d/2}
				loA, hiA, loB, hiB := e.from.x, e.from.x+e.from.w, e.to.x, e.to.x+e.to.w
				if axis == 1 {
					loA, hiA, loB, hiB = e.from.y, e.from.y+e.from.d, e.to.y, e.to.y+e.to.d
				}
				side := func(lo, hi float64) (float64, bool) {
					if channel < lo-6 {
						return lo, true
					}
					return hi, channel > hi+6
				}
				var okA, okB bool
				a[axis], okA = side(loA, hiA)
				b[axis], okB = side(loB, hiB)
				if okA && okB {
					p, q := a, b
					p[axis], q[axis] = channel, channel
					out = append(out, [][2]float64{a, p, q, b})
				}
				continue
			}
			// Both old endpoints and their outward directions stay fixed.
			a, b := e.pts[0], e.pts[len(e.pts)-1]
			lead := func(r planRect, p [2]float64) [2]float64 {
				q := p
				distance := math.Max(24, leadLength)
				switch {
				case p[0] == r.x:
					q[0] -= distance
				case p[0] == r.x+r.w:
					q[0] += distance
				case p[1] == r.y:
					q[1] -= distance
				default:
					q[1] += distance
				}
				return q
			}
			p, q := lead(e.from, a), lead(e.to, b)
			x, y := p, q
			x[axis], y[axis] = channel, channel
			out = append(out, [][2]float64{a, p, x, y, q, b})
		}
	}
	// Deduplicate and strip zero/collinear subdivisions before scoring. A
	// reversal is not removed: it must fail the endpoint/body/self checks.
	unique := out[:0]
	for _, pts := range out {
		p := make([][2]float64, 0, len(pts))
		for _, v := range pts {
			if len(p) > 0 && p[len(p)-1] == v {
				continue
			}
			if len(p) >= 2 {
				a, b := p[len(p)-2], p[len(p)-1]
				if (b[0]-a[0])*(v[1]-b[1]) == (b[1]-a[1])*(v[0]-b[0]) && (b[0]-a[0])*(v[0]-b[0])+(b[1]-a[1])*(v[1]-b[1]) >= 0 {
					p = p[:len(p)-1]
				}
			}
			p = append(p, v)
		}
		if !slices.ContainsFunc(unique, func(q [][2]float64) bool { return slices.Equal(p, q) }) {
			unique = append(unique, p)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool {
		a, b := planBends(unique[i]), planBends(unique[j])
		return a < b || a == b && planLen(unique[i]) < planLen(unique[j])
	})
	return unique
}
