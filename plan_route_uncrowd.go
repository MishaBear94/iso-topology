package isotopo

import "slices"

// Route clearance is useful even without titles. Keep it separate from the
// title search so existing successful routes retain precedence. The final
// observation also serves the report/evaluator without another route solve.
func refinePlanRouteCrowding(rects []planRect, edges []planEdge, content *planContentLayout, ox, oy float64) ([]planEdge, *planContentLayout, *PlanRouteCrowding) {
	before := observePlanRouteCrowding(rects, edges, ox, oy)
	if before.Status != "complete" || len(before.Conflicts) == 0 {
		return edges, content, before
	}
	eligible := func(e planEdge) bool {
		if !planRouteAutomatic(e) || isFixedPortRef(e.c.From) || isFixedPortRef(e.c.To) {
			return false
		}
		for _, c := range before.Conflicts {
			if c.ConnectorIndex == e.ci || c.PeerIndex == e.ci {
				return true
			}
		}
		return false
	}
	if !slices.ContainsFunc(edges, eligible) {
		return edges, content, before
	}
	if content == nil {
		v := resolvePlanContent(rects, edges, ox, oy, true)
		content = &v
	}
	for i, r := range rects {
		if r.label != "" && content.texts[i].font == nil {
			return edges, content, before
		}
	}
	if len(content.pills) != 0 {
		return edges, content, before
	}
	probes, layouts := 128, 8
	for i, e := range edges {
		if probes == 0 || layouts == 0 {
			break
		}
		if !eligible(e) {
			continue
		}
		oldDefects, _ := planRouteContentDefects(*content)
		for _, pts := range planRouteLaneCandidates(e, edges, rects, *content, ox, oy) {
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
			trial := slices.Clone(edges)
			trial[i] = cand
			layouts-- // bounds both full-scene observations and content solves
			crowding := observePlanRouteCrowding(rects, trial, ox, oy)
			if crowding.Status != "complete" || len(crowding.Conflicts) >= len(before.Conflicts) || !planCrowdingDefects(crowding).noWorseThan(planCrowdingDefects(before)) {
				continue
			}
			after := resolvePlanContent(rects, trial, ox, oy, true)
			defects, _ := planRouteContentDefects(after)
			if !defects.noWorseThan(oldDefects) || !planRoutePreservesClear(*content, after, e.ci) {
				continue
			}
			edges, content, before = trial, &after, crowding
			break
		}
	}
	return edges, content, before
}
