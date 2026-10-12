package isotopo

import (
	"context"
	"fmt"
	"math"
)

// planeval.go scores auto-layout connection quality from the FLAT top-down
// geometry. The plan view is the right basis for this: it carries true world
// (x, y) — the isometric projection skews distance and angle, so crossings and
// lengths can't be measured there. Node positions here ARE the layout solver's
// real output; edges are the plan router's orthogonal routes (the same ones the
// plan view draws), so the metrics measure exactly what you see.

// PlanPoint is a world-space point (a crossing location).
type PlanPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PlanEdgeIssue flags one edge that tunnels through unrelated node footprints.
type PlanEdgeIssue struct {
	From    string       `json:"from"`
	To      string       `json:"to"`
	Through []string     `json:"through"`
	Pts     [][2]float64 `json:"-"` // overlay geometry, not serialised
}

// PlanReport is the layout-quality scorecard. Lower is better for every count;
// a clean DAG layout scores Crossings=0, EdgesThroughNodes=0, BackwardEdges=0,
// NodeOverlaps=0.
type PlanReport struct {
	Nodes             int     `json:"nodes"`
	Edges             int     `json:"edges"`
	Crossings         int     `json:"crossings"`
	EdgesThroughNodes int     `json:"edges_through_nodes"`
	BackwardEdges     int     `json:"backward_edges"`
	NodeOverlaps      int     `json:"node_overlaps"`
	TotalBends        int     `json:"total_bends"`
	TotalEdgeLen      float64 `json:"total_edge_len"`
	MaxEdgeLen        float64 `json:"max_edge_len"`
	FlowAxis          string  `json:"flow_axis"` // vertical | horizontal | none

	CrossingsAt   []PlanPoint        `json:"crossings_at,omitempty"`
	ProblemEdges  []PlanEdgeIssue    `json:"problem_edges,omitempty"`
	RouteCrowding *PlanRouteCrowding `json:"route_crowding,omitempty"`
}

// EvaluatePlanText parses a document and scores its plan-view layout.
func EvaluatePlanText(format string, src []byte) (*PlanReport, error) {
	doc, err := LoadInput(context.Background(), format, src, LayoutDagre)
	if err != nil {
		return nil, err
	}
	scene := doc.Scene()
	if scene == nil {
		return nil, fmt.Errorf("document has no scene to evaluate")
	}
	return EvaluatePlan(scene, doc.Theme, doc.Canvas), nil
}

// EvaluatePlan computes the scorecard from a scene's flat geometry.
func EvaluatePlan(n *Node, theme *Theme, canvas *Canvas) *PlanReport {
	return resolvePlanScene(n, theme, canvas, true, false).evaluate(nil)
}

// evalGeom computes the scorecard from already-resolved leaf footprints and
// routed edges — shared by EvaluatePlan and the A/B harness so both score
// identically.
func evalGeom(leaves []planRect, edges []planEdge) *PlanReport {
	return evalGeomWithDefects(leaves, edges, nil)
}

func evalGeomWithDefects(leaves []planRect, edges []planEdge, defects visualDefects) *PlanReport {
	rep := &PlanReport{Nodes: len(leaves), Edges: len(edges)}

	// Per-edge: length, bends, flow contribution, tunnelling through nodes.
	var sumX, sumY float64
	for _, e := range edges {
		rep.TotalBends += planBends(e.pts)
		l := planLen(e.pts)
		rep.TotalEdgeLen += l
		if l > rep.MaxEdgeLen {
			rep.MaxEdgeLen = l
		}
		sumX += (e.to.x + e.to.w/2) - (e.from.x + e.from.w/2)
		sumY += (e.to.y + e.to.d/2) - (e.from.y + e.from.d/2)

		ez := edgeZLevel(e.from, e.to)
		var through []string
		for _, r := range leaves {
			if r.id == e.from.id || r.id == e.to.id || r.h <= planThinH ||
				!sameFloor(ez, r) || enclosesBoth(r, e.from, e.to) {
				continue // self/endpoint, flat decoration, stacked on another
				// z-floor, or a shared substrate both nodes sit on — none are
				// real obstacles (the plan view just flattens the height away)
			}
			if routeHitsRect(e.pts, r) {
				through = append(through, r.id)
				defects.add(visualDefect{kind: "tunnel", edge: e.ci, a: r.id})
			}
		}
		if len(through) > 0 {
			rep.EdgesThroughNodes++
			rep.ProblemEdges = append(rep.ProblemEdges, PlanEdgeIssue{
				From: e.from.id, To: e.to.id, Through: through, Pts: e.pts,
			})
		}
	}

	// Dominant flow axis + backward (against-flow) edges. A clean DAG drawn
	// top-down/left-right should have almost none.
	dir := 0.0
	if math.Abs(sumY) >= math.Abs(sumX) && math.Abs(sumY) > 1e-6 {
		rep.FlowAxis = "vertical"
		dir = math.Copysign(1, sumY)
	} else if math.Abs(sumX) > 1e-6 {
		rep.FlowAxis = "horizontal"
		dir = math.Copysign(1, sumX)
	} else {
		rep.FlowAxis = "none"
	}
	for _, e := range edges {
		var d float64
		if rep.FlowAxis == "vertical" {
			d = (e.to.y + e.to.d/2) - (e.from.y + e.from.d/2)
		} else if rep.FlowAxis == "horizontal" {
			d = (e.to.x + e.to.w/2) - (e.from.x + e.from.w/2)
		}
		if dir != 0 && math.Copysign(1, d) != dir && math.Abs(d) > 1e-6 {
			rep.BackwardEdges++
		}
	}

	// Crossings between non-adjacent edges (edges meeting at a shared node are
	// not crossings — that's just a hub).
	for i := 0; i < len(edges); i++ {
		for j := i + 1; j < len(edges); j++ {
			if edgesAdjacent(edges[i], edges[j]) {
				continue
			}
			for a := 1; a < len(edges[i].pts); a++ {
				for b := 1; b < len(edges[j].pts); b++ {
					if p, ok := segInt(edges[i].pts[a-1], edges[i].pts[a], edges[j].pts[b-1], edges[j].pts[b]); ok {
						rep.Crossings++
						defects.add(visualDefect{kind: "crossing", edge: edges[i].ci, peer: edges[j].ci})
						rep.CrossingsAt = append(rep.CrossingsAt, p)
					}
				}
			}
		}
	}

	// Node overlaps among leaf footprints.
	for i := 0; i < len(leaves); i++ {
		for j := i + 1; j < len(leaves); j++ {
			if rectsOverlap(leaves[i], leaves[j]) {
				rep.NodeOverlaps++
				defects.add(nodePairDefect("leaf-overlap", leaves[i].id, leaves[j].id))
			}
		}
	}
	return rep
}

// RenderPlanAnnotated renders the plan view with every crossing and tunnelling
// edge highlighted in red, and returns the scorecard alongside it.
func RenderPlanAnnotated(n *Node, theme *Theme, canvas *Canvas) (string, *PlanReport) {
	p := resolvePlanScene(n, theme, canvas, true, false)
	rep := p.evaluate(nil)
	return renderResolvedPlan(p, canvas, rep, nil, nil), rep
}

// planZTol absorbs small z-gaps (a chip resting on a plate) so "same floor"
// stays robust. planThinH is the height below which a part is flat decoration
// (a silkscreen frame, an inlay) rather than a body worth routing around.
const (
	planZTol  = 6.0
	planThinH = 2.0
)

// edgeZLevel is the mid-height of an edge's two endpoints.
func edgeZLevel(a, b planRect) float64 {
	return ((a.z + a.h/2) + (b.z + b.h/2)) / 2
}

// sameFloor reports whether an obstacle shares the edge's height band. Things
// stacked clearly above or below (e.g. the substrate plates a chip sits on) are
// not obstacles in the flattened top-down view, only co-located by projection.
func sameFloor(edgeZ float64, r planRect) bool {
	return edgeZ >= r.z-planZTol && edgeZ <= r.z+r.h+planZTol
}

// enclosesBoth reports whether r contains BOTH endpoints' footprints in x,y —
// i.e. r is a shared backplane/substrate the two nodes sit on. A route between
// them runs across that plate (a PCB trace), not "through" an obstacle, so it
// must not be flagged as tunnelling.
func enclosesBoth(r, a, b planRect) bool {
	in := func(c planRect) bool {
		return r.x <= c.x && c.x+c.w <= r.x+r.w && r.y <= c.y && c.y+c.d <= r.y+r.d
	}
	return in(a) && in(b)
}

func planLen(pts [][2]float64) float64 {
	var l float64
	for i := 1; i < len(pts); i++ {
		l += math.Abs(pts[i][0]-pts[i-1][0]) + math.Abs(pts[i][1]-pts[i-1][1])
	}
	return l
}

// planBends counts genuine turns (a non-zero cross product between consecutive
// segment directions); collinear or zero-length joints don't count.
func planBends(pts [][2]float64) int {
	b := 0
	for i := 1; i < len(pts)-1; i++ {
		ax, ay := pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1]
		bx, by := pts[i+1][0]-pts[i][0], pts[i+1][1]-pts[i][1]
		if math.Abs(ax*by-ay*bx) > 1e-9 {
			b++
		}
	}
	return b
}

func edgesAdjacent(a, b planEdge) bool {
	return a.from.id == b.from.id || a.from.id == b.to.id ||
		a.to.id == b.from.id || a.to.id == b.to.id
}

// segInt returns the proper (interior) intersection of segments ab and cd, if
// any. Endpoints that merely touch (shared corners, T-junctions) are excluded.
func segInt(a, b, c, d [2]float64) (PlanPoint, bool) {
	rx, ry := b[0]-a[0], b[1]-a[1]
	sx, sy := d[0]-c[0], d[1]-c[1]
	den := rx*sy - ry*sx
	if math.Abs(den) < 1e-9 {
		return PlanPoint{}, false // parallel / degenerate
	}
	t := ((c[0]-a[0])*sy - (c[1]-a[1])*sx) / den
	u := ((c[0]-a[0])*ry - (c[1]-a[1])*rx) / den
	const eps = 1e-6
	if t > eps && t < 1-eps && u > eps && u < 1-eps {
		return PlanPoint{X: a[0] + t*rx, Y: a[1] + t*ry}, true
	}
	return PlanPoint{}, false
}

// routeHitsRect reports whether any segment of a route passes through the
// interior of a rect (inset slightly so grazing a face or corner doesn't count).
func routeHitsRect(pts [][2]float64, r planRect) bool {
	const in = 3.0
	x0, y0, x1, y1 := r.x+in, r.y+in, r.x+r.w-in, r.y+r.d-in
	if x1 <= x0 || y1 <= y0 {
		return false
	}
	inside := func(p [2]float64) bool { return p[0] > x0 && p[0] < x1 && p[1] > y0 && p[1] < y1 }
	corners := [4][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if inside(a) || inside(b) {
			return true
		}
		for k := 0; k < 4; k++ {
			if _, ok := segInt(a, b, corners[k], corners[(k+1)%4]); ok {
				return true
			}
		}
	}
	return false
}

// routeTunnels counts unrelated nodes a candidate world route passes through,
// reusing the scorecard kernel so the engine router (inject.go) and the
// evaluator judge tunnelling identically. routeZ is the route's world plane;
// obstacles exclude substrates already.
func routeTunnels(pts [][2]float64, srcID, tgtID string, routeZ float64, obstacles []planRect) int {
	var src, tgt planRect
	for _, r := range obstacles {
		if r.id == srcID {
			src = r
		}
		if r.id == tgtID {
			tgt = r
		}
	}
	n := 0
	for _, r := range obstacles {
		if r.id == srcID || r.id == tgtID || r.h <= planThinH ||
			!sameFloor(routeZ, r) || enclosesBoth(r, src, tgt) {
			continue
		}
		if routeHitsRect(pts, r) {
			n++
		}
	}
	return n
}

func rectsOverlap(a, b planRect) bool {
	const eps = 1.0
	// Ground footprint (x,y) must overlap.
	if !(a.x+eps < b.x+b.w && b.x+eps < a.x+a.w &&
		a.y+eps < b.y+b.d && b.y+eps < a.y+a.d) {
		return false
	}
	// ...AND the two parts must share a vertical band. Parts stacked on
	// different floors (one placed `above` another — a topper, a chip resting on
	// a plate) deliberately share an x,y footprint but do NOT collide; counting
	// them as overlaps produced a phantom the layout could never resolve by
	// nudging gaps. planZTol absorbs a small resting/rounding gap, and a near-
	// flat decoration (h≈0) contributes no band, so neither reads as a collision.
	zLo, zHi := a.z, a.z+a.h
	if b.z > zLo {
		zLo = b.z
	}
	if b.z+b.h < zHi {
		zHi = b.z + b.h
	}
	return zHi-zLo > planZTol
}
