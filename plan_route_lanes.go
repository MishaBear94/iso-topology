package isotopo

import (
	"math"
	"slices"
	"sort"
)

// Run only after the existing title/route search. Successful arrangements keep
// their routes; a still-blocked title can use the free lanes beside other
// strokes and unused positions on an automatic endpoint's face. No shared-node
// exemption is needed: each accepted route clears every other stroke/arrow.
func refinePlanRouteLanes(rects []planRect, edges []planEdge, before planContentLayout, ox, oy float64) ([]planEdge, *planContentLayout) {
	if len(edges) < 2 {
		return edges, &before
	}
	// These are whole-scene budgets, independent of the original search. Cheap
	// geometric rejects must not consume the expensive content-layout budget.
	probes, layouts := 128, 8
	for i, e := range edges {
		if probes == 0 || layouts == 0 {
			break
		}
		if !planRouteAutomatic(e) || isFixedPortRef(e.c.From) || isFixedPortRef(e.c.To) || !planRouteBlocksTitle(before, e.ci) {
			continue
		}
		oldDefects, blocked := planRouteContentDefects(before)
		oldContacts := planRouteTitleContacts(before, e.ci)
		for _, pts := range planRouteLaneCandidates(e, edges, rects, before, ox, oy) {
			if probes == 0 || layouts == 0 {
				break
			}
			probes--
			if slices.Equal(pts, e.pts) || !planAttachmentsValid(pts, e.from, e.to) {
				continue
			}
			cand := e
			cand.pts, cand.refined = pts, true
			if !e.refined {
				cand.framePts = e.pts
			}
			if !planRouteSurfaceClear(cand, rects, edges, ox, oy) || !planRouteLaneBordersClear(cand, rects, ox, oy) {
				continue
			}
			layouts--
			trial := slices.Clone(edges)
			trial[i] = cand
			after := resolvePlanContent(rects, trial, ox, oy, true)
			defects, count := planRouteContentDefects(after)
			if count <= blocked && planRouteTitleContacts(after, e.ci) < oldContacts && defects.noWorseThan(oldDefects) && planRoutePreservesClear(before, after, e.ci) {
				edges, before = trial, after
				break // candidates are ordered by bends, length, then port motion
			}
		}
	}
	return edges, &before
}

// Crossing an enclosure boundary is meaningful; running alongside its outline
// without a reading gap looks like a merged/broken border. The existing body
// guard deliberately allows enclosure crossings, so check parallel runs here.
func planRouteLaneBordersClear(e planEdge, rects []planRect, ox, oy float64) bool {
	points := planRouteScreenPoints(e, ox, oy)
	arrow, hasArrow := planRouteArrow(e, points)
	for _, r := range rects {
		if !r.container || r.id == e.from.id || r.id == e.to.id {
			continue
		}
		x, y := svgCoord(r.x+ox), svgCoord(r.y+oy)
		w, h := svgCoord(r.w), svgCoord(r.d)
		corners := [][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}, {x, y}}
		borders := connectorTextStrokes(-1, corners, false, planLineW(r))
		for _, border := range borders {
			if hasArrow && connectorTextStrokeShortfall(arrow, border) > 0 {
				return false
			}
			for _, s := range connectorTextStrokes(e.ci, points, false, planRouteWidth(e.c)) {
				if (s.a[0] == s.b[0]) == (border.a[0] == border.b[0]) && connectorTextStrokeShortfall(planStrokeBox(s), border) > 0 {
					return false
				}
			}
		}
	}
	return true
}

// Two edges can obstruct the same title. Removing either obstruction is real
// progress even while the other keeps the title blocked. Require an eliminated
// (title, edge, primitive-kind) contact, not an aggregate score improvement.
func planRouteTitleContacts(content planContentLayout, edge int) int {
	count := 0
	for _, text := range content.texts {
		if p := text.record.Placement; p != nil {
			for _, c := range p.Conflicts {
				if (c.Kind == "route" || c.Kind == "arrow") && c.ConnectorIndex != nil && *c.ConnectorIndex == edge {
					count++
				}
			}
		}
	}
	return count
}

func planRouteLaneCandidates(e planEdge, edges []planEdge, rects []planRect, content planContentLayout, ox, oy float64) [][][2]float64 {
	const maxHints = 32
	var hints [2][]float64
	add := func(axis int, v float64) {
		if len(hints[axis]) < maxHints && !slices.Contains(hints[axis], v) {
			hints[axis] = append(hints[axis], v)
		}
	}
	gap := planRouteWidth(e.c)/2 + 6.2
	lead := math.Max(12, planRouteLead(e.c))
	for axis := range hints {
		for _, r := range []planRect{e.from, e.to} {
			lo, hi := r.x, r.x+r.w
			if axis == 1 {
				lo, hi = r.y, r.y+r.d
			}
			add(axis, lo-lead)
			add(axis, hi+lead)
		}
	}
	// Actual stroke envelopes supply lanes missed by node-only routing. Include
	// both sides, regardless of ownership: nearby edges can have distinct ends.
	for _, other := range edges {
		if other.ci == e.ci {
			continue
		}
		clearance := gap + planRouteWidth(other.c)/2
		for _, p := range other.pts {
			for axis := range hints {
				add(axis, p[axis]-clearance)
				add(axis, p[axis]+clearance)
			}
		}
	}
	for _, t := range content.texts {
		if p := t.record.Placement; p == nil || p.Status != "blocked" {
			continue
		}
		for _, b := range t.inkRects() {
			add(0, b.x0-ox-gap)
			add(0, b.x1-ox+gap)
			add(1, b.y0-oy-gap)
			add(1, b.y1-oy+gap)
		}
	}
	for _, r := range rects {
		add(0, r.x-gap-1)
		add(0, r.x+r.w+gap+1)
		add(1, r.y-gap-1)
		add(1, r.y+r.d+gap+1)
	}
	var out [][][2]float64
	for axis, channels := range hints {
		aSlots := planRouteFaceSlots(e.from, axis, e, edges)
		bSlots := planRouteFaceSlots(e.to, axis, e, edges)
		face := func(r planRect, channel float64) (float64, bool) {
			lo, hi := r.x, r.x+r.w
			if axis == 1 {
				lo, hi = r.y, r.y+r.d
			}
			if channel <= lo-lead {
				return lo, true
			}
			return hi, channel >= hi+lead
		}
		for _, channel := range channels {
			fa, okA := face(e.from, channel)
			fb, okB := face(e.to, channel)
			if !okA || !okB {
				continue
			}
			for _, sa := range aSlots {
				for _, sb := range bSlots {
					a, b := [2]float64{}, [2]float64{}
					a[axis], a[1-axis], b[axis], b[1-axis] = fa, sa, fb, sb
					p, q := a, b
					p[axis], q[axis] = channel, channel
					// Equal transverse positions would double back along one line.
					if p == q {
						continue
					}
					out = append(out, [][2]float64{a, p, q, b})
				}
			}
		}
	}
	motion := func(p [][2]float64) float64 {
		a, b := e.pts[0], e.pts[len(e.pts)-1]
		return math.Hypot(p[0][0]-a[0], p[0][1]-a[1]) + math.Hypot(p[3][0]-b[0], p[3][1]-b[1])
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := planLen(out[i]), planLen(out[j])
		return a < b || a == b && motion(out[i]) < motion(out[j])
	})
	return out
}

// Slots stay away from rounded corners. Positions adjacent to occupied docks
// use the complete casing widths plus a reading gap; final geometry is still
// checked against every stroke and arrow, including shared-endpoint edges.
func planRouteFaceSlots(r planRect, axis int, e planEdge, edges []planEdge) []float64 {
	lo, hi := r.y, r.y+r.d
	if axis == 1 {
		lo, hi = r.x, r.x+r.w
	}
	center := (lo + hi) / 2
	inset := 6 + planRouteWidth(e.c)/2
	lo, hi = lo+inset, hi-inset
	if lo > hi {
		return nil // not enough straight contour to dock safely
	}
	out := []float64{center}
	add := func(v float64) {
		if len(out) < 5 && v >= lo && v <= hi && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	for _, other := range edges {
		if other.ci == e.ci || len(other.pts) < 2 {
			continue
		}
		gap := (planRouteWidth(e.c)+planRouteWidth(other.c))/2 + 6.2
		for end, owner := range []string{other.from.id, other.to.id} {
			if owner == r.id {
				p := other.pts[end*(len(other.pts)-1)][1-axis]
				add(p - gap)
				add(p + gap)
			}
		}
	}
	add(lo)
	add(hi)
	return out
}
