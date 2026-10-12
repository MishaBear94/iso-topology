package isotopo

import (
	"math"
	"slices"
)

// PlanRouteCrowding observes final submitted plan routes. Envelopes include a
// six-unit reading gap, continuous coverage of dash gaps, square cap bounds,
// and the actual triangle marker. Opaque leaf cores and bounded shared docks
// are removed. This is a conservative clearance observation, not pixel area.
// Only complete status with no conflicts establishes clearance in this scope.
type PlanRouteCrowding struct {
	Scope     string              `json:"scope"`
	Status    string              `json:"status"` // complete | unresolved
	Reason    string              `json:"reason,omitempty"`
	Conflicts []PlanRouteConflict `json:"conflicts"`
}

type PlanRouteConflict struct {
	ConnectorIndex int             `json:"connector_index"`
	PeerIndex      int             `json:"peer_index"`
	Kind           string          `json:"kind"` // stroke-stroke | stroke-arrow | arrow-stroke | arrow-arrow
	Bounds         ScreenLabelRect `json:"bounds"`
	Area           float64         `json:"area"` // union of clearance-envelope intersections, SVG units squared
}

const planRouteCrowdingScope = "submitted-stroke-envelope,triangle-marker,clearance-6,opaque-leaf-core-mask,shared-dock-12-plus-clearance"

type planCrowdingInk struct {
	stroke, arrow screenRegion
	box           occRect
	points        [][2]float64
}

func planCrowdingRect(r screenRect) [][2]float64 {
	return [][2]float64{{r.x0, r.y0}, {r.x1, r.y0}, {r.x1, r.y1}, {r.x0, r.y1}}
}

func planCrowdingEnvelope(points [][2]float64, radius float64) [][2]float64 {
	var corners [][2]float64
	for _, p := range points {
		corners = append(corners, [2]float64{p[0] - radius, p[1] - radius}, [2]float64{p[0] + radius, p[1] - radius}, [2]float64{p[0] + radius, p[1] + radius}, [2]float64{p[0] - radius, p[1] + radius})
	}
	return convexHull(corners)
}

func planCrowdingGeometry(e planEdge, ox, oy float64) planCrowdingInk {
	out := planCrowdingInk{points: planRouteScreenPoints(e, ox, oy)}
	for _, s := range connectorTextConflictStrokes(connectorTextStrokes(e.ci, out.points, false, planRouteWidth(e.c))) {
		out.stroke = append(out.stroke, planCrowdingEnvelope([][2]float64{s.a, s.b}, s.radius+3))
	}
	if e.c.Arrow != "none" && len(out.points) >= 2 {
		b := out.points[len(out.points)-1]
		for i := len(out.points) - 2; i >= 0; i-- {
			a := out.points[i]
			dx, dy := b[0]-a[0], b[1]-a[1]
			l := math.Hypot(dx, dy)
			if l == 0 {
				continue
			}
			_, _, sw, _ := planEdgeStyle(e.c)
			dx, dy = dx/l*.9*svgCoord(sw), dy/l*.9*svgCoord(sw)
			var triangle [][2]float64
			for _, v := range [][2]float64{{-8.5, -5}, {1.5, 0}, {-8.5, 5}} {
				triangle = append(triangle, [2]float64{b[0] + v[0]*dx - v[1]*dy, b[1] + v[0]*dy + v[1]*dx})
			}
			out.arrow = screenRegion{planCrowdingEnvelope(triangle, 3)}
			break
		}
	}
	out.box = regionBounds(append(slices.Clone(out.stroke), out.arrow...))
	return out
}

func observePlanRouteCrowding(rects []planRect, edges []planEdge, ox, oy float64) *PlanRouteCrowding {
	out := &PlanRouteCrowding{Scope: planRouteCrowdingScope, Status: "complete", Conflicts: []PlanRouteConflict{}}
	if len(edges) < 2 {
		return out
	}
	// Limits apply to the entire observation. Incomplete observations never
	// authorize a candidate; reports explicitly retain unresolved status.
	work, pairs, primitives := 1_000_000, 262144, 8192
	var cores screenRegion
	for _, r := range rects {
		if core, ok := planOpaqueCore(r, ox, oy); ok && !r.container {
			if !connectorFinite(core.x0) || !connectorFinite(core.y0) || !connectorFinite(core.x1) || !connectorFinite(core.y1) {
				out.Status, out.Reason = "unresolved", "non-finite opaque core"
				return out
			}
			cores = append(cores, planCrowdingRect(core))
		}
	}
	ink := make([]planCrowdingInk, len(edges))
	for i, e := range edges {
		primitives -= len(e.pts) + 1
		if primitives < 0 {
			out.Status, out.Reason = "unresolved", "scene route primitive budget"
			return out
		}
		if e.c == nil || len(e.pts) < 2 || len(e.pts) > 512 {
			out.Status, out.Reason = "unresolved", "missing route or route segment budget"
			return out
		}
		for _, p := range e.pts {
			if !connectorFinite(p[0]+ox) || !connectorFinite(p[1]+oy) || !connectorFinite(planRouteWidth(e.c)) {
				out.Status, out.Reason = "unresolved", "non-finite submitted geometry"
				return out
			}
		}
		ink[i] = planCrowdingGeometry(e, ox, oy)
		for _, region := range []screenRegion{ink[i].stroke, ink[i].arrow} {
			for _, poly := range region {
				if !connectorFinite(polygonSignedArea(poly)) {
					out.Status, out.Reason = "unresolved", "clearance envelope exceeds numeric range"
					return out
				}
				for _, p := range poly {
					if !connectorFinite(p[0]) || !connectorFinite(p[1]) {
						out.Status, out.Reason = "unresolved", "non-finite clearance envelope"
						return out
					}
				}
			}
		}
	}
	for i, a := range edges {
		for j := i + 1; j < len(edges); j++ {
			pairs--
			if pairs < 0 {
				out.Status, out.Reason = "unresolved", "route pair budget"
				return out
			}
			if !painterBoundsMeet(ink[i].box, ink[j].box) {
				continue
			}
			b := edges[j]
			masks := slices.Clone(cores)
			// Distinct parallel semantic edges joining the same node pair must
			// remain distinguishable, even when their entire path is short.
			samePair := a.from.id == b.from.id && a.to.id == b.to.id || a.from.id == b.to.id && a.to.id == b.from.id
			if !samePair {
				radius := 12 + (planRouteWidth(a.c)+planRouteWidth(b.c))/2 + 6
				for ai, ownerA := range []string{a.from.id, a.to.id} {
					for bi, ownerB := range []string{b.from.id, b.to.id} {
						pa, pb := ink[i].points[ai*(len(ink[i].points)-1)], ink[j].points[bi*(len(ink[j].points)-1)]
						if ownerA != "" && ownerA == ownerB && pa == pb {
							masks = append(masks, planCrowdingRect(screenRect{pa[0] - radius, pa[1] - radius, pa[0] + radius, pa[1] + radius}))
						}
					}
				}
			}
			for _, pair := range []struct {
				kind string
				a, b screenRegion
			}{{"stroke-stroke", ink[i].stroke, ink[j].stroke}, {"stroke-arrow", ink[i].stroke, ink[j].arrow}, {"arrow-stroke", ink[i].arrow, ink[j].stroke}, {"arrow-arrow", ink[i].arrow, ink[j].arrow}} {
				region, ok := planCrowdingIntersection(pair.a, pair.b, masks, &work)
				if !ok {
					out.Status, out.Reason = "unresolved", "clearance intersection budget"
					return out
				}
				if area := regionArea(region); area > 1e-7 {
					box := regionBounds(region)
					out.Conflicts = append(out.Conflicts, PlanRouteConflict{a.ci, b.ci, pair.kind, ScreenLabelRect{box.x0, box.y0, box.x1 - box.x0, box.y1 - box.y0}, area})
				}
			}
		}
	}
	return out
}

func planCrowdingIntersection(a, b, masks screenRegion, work *int) (screenRegion, bool) {
	var out screenRegion
	for _, x := range a {
		for _, y := range b {
			*work--
			if *work < 0 {
				return nil, false
			}
			if !spatialBoundsOverlap(x, y) {
				continue
			}
			poly, ok := painterIntersection(x, y, work)
			if !ok {
				return nil, false
			}
			if len(poly) == 0 {
				continue
			}
			pieces := screenRegion{poly}
			for _, mask := range masks {
				var next screenRegion
				changed := false
				for i, p := range pieces {
					*work -= len(p) * len(mask)
					if *work < 0 {
						return nil, false
					}
					if spatialBoundsOverlap(p, mask) {
						if !changed {
							next = append(next, pieces[:i]...)
							changed = true
						}
						next = append(next, spatialSubtract(p, mask)...)
					} else if changed {
						next = append(next, p)
					}
				}
				if changed {
					pieces = next
				}
				if len(pieces) > 256 {
					return nil, false
				}
			}
			out = append(out, pieces...)
			if len(out) > 256 {
				return nil, false
			}
		}
	}
	return out, true
}

func planCrowdingDefects(c *PlanRouteCrowding) visualDefects {
	d := visualDefects{}
	if c.Status != "complete" {
		d[visualDefect{kind: "plan-route-clearance-unresolved"}] = 1
	}
	for _, conflict := range c.Conflicts {
		d[visualDefect{kind: "plan-route-" + conflict.Kind, edge: conflict.ConnectorIndex, peer: conflict.PeerIndex}] = conflict.Area
	}
	return d
}
