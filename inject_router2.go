package isotopo

// inject_router2.go — the opt-in v2 orthogonal router (routing: "orthogonal2").
//
// The v1 router in inject.go is a 2-candidate greedy: between the two stubs it
// only ever considers the xFirst / yFirst L-shapes, so when BOTH candidates
// tunnel a node it can merely pick the lesser evil — it cannot insert a third
// segment and detour around the blocker. It also never prices edge-edge
// crossings (the plan-side router's routeCost does, at +100 per crossing;
// see planview.go). v2 fixes both by searching a sparse Hanan-style grid —
// every obstacle boundary ± clearance, plus the two stub coordinates and the
// corridor midlines — with Dijkstra over (grid node, heading) states, so a
// route may take arbitrarily many turns to clear obstacles and dodge edges
// that were already placed.
//
// The cost function deliberately mirrors planview.go routeCost so the router
// optimises EXACTLY what the scorecard (planeval.go evalGeom) measures:
//
//	tunnel an unrelated node   +1000   (per obstacle per segment)
//	cross an already-placed edge +100  (proper interior intersection, segInt)
//	bend                          +3
//	length                        +0.01 / world unit
//
// Obstacle filtering reuses the scorecard kernel (planThinH / sameFloor /
// enclosesBoth) so the router and the evaluator agree on what counts as an
// obstacle. Everything here is pure and deterministic: no time, no rand, no
// map iteration — same input, same route.

import (
	"container/heap"
	"math"
	"sort"
)

// routerAnchor is one endpoint of a v2 route: the world-ground stub point the
// searched route starts/ends at, the outward face normal it exits along, the
// owning part id (its own footprint is never an obstacle) and the endpoint's
// base height (the route plane is the min of the two, matching the v1
// ground-hugging routeZ).
type routerAnchor struct {
	ID     string  // owning part id — excluded from the obstacle set
	X, Y   float64 // world ground coords of the stub point
	DX, DY float64 // unit outward face normal (axis-aligned)
	Z      float64 // endpoint base (ground) height
}

// Cost weights — keep in lockstep with planview.go routeCost.
const (
	r2TunnelCost = 1000.0
	r2CrossCost  = 100.0
	r2BendCost   = 3.0
	r2LenCost    = 0.01
	// r2Clearance is how far off an obstacle boundary the grid tracks run, so
	// a detour never grazes the body it is avoiding (the evaluator's
	// routeHitsRect insets rects by 3; 12 leaves a visible gap too).
	r2Clearance = 12.0
	// r2MaxGrid caps the per-axis coordinate count; beyond it the search is
	// abandoned and the caller falls back to the v1 elbow route.
	r2MaxGrid = 160
)

// r2Dir maps an axis-aligned direction vector to a heading index:
// 0 = +x, 1 = -x, 2 = +y, 3 = -y. Opposite heading of d is d^1.
func r2Dir(dx, dy float64) int {
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return 0
		}
		return 1
	}
	if dy >= 0 {
		return 2
	}
	return 3
}

// r2Coords sorts and dedupes a coordinate set (1e-6 tolerance).
func r2Coords(v []float64) []float64 {
	sort.Float64s(v)
	out := v[:1]
	for _, x := range v[1:] {
		if x-out[len(out)-1] > 1e-6 {
			out = append(out, x)
		}
	}
	return out
}

// r2Index returns the index of the grid coordinate closest to x.
func r2Index(v []float64, x float64) int {
	best, bd := 0, math.Inf(1)
	for i, c := range v {
		if d := math.Abs(c - x); d < bd {
			best, bd = i, d
		}
	}
	return best
}

// r2SegCross prices a grid edge [a,b] against a placed segment [p,q]. The
// evaluator (segInt) scores whole polylines, where a transversal crossing is
// always interior; the router prices CHOPPED grid edges, and a crossing that
// falls exactly on a grid node (e.g. a placed route lying on the corridor
// midline) is an ENDPOINT of two adjacent edges — segInt would miss it and
// the router would sail through a crossing the scorecard then counts. So:
// the intersection must be interior to the PLACED segment, but on our edge
// the span is half-open [start, end) — the crossing-on-a-node is charged
// exactly once, to the edge that leaves the node.
func r2SegCross(a, b, p, q [2]float64) bool {
	rx, ry := b[0]-a[0], b[1]-a[1]
	sx, sy := q[0]-p[0], q[1]-p[1]
	den := rx*sy - ry*sx
	if math.Abs(den) < 1e-9 {
		return false // parallel / collinear-overlap: segInt doesn't count it either
	}
	t := ((p[0]-a[0])*sy - (p[1]-a[1])*sx) / den // along our grid edge
	u := ((p[0]-a[0])*ry - (p[1]-a[1])*rx) / den // along the placed segment
	const eps = 1e-6
	return u > eps && u < 1-eps && t >= -eps && t < 1-eps
}

// r2Item / r2Heap — a deterministic min-heap for the Dijkstra frontier.
// Ties on cost break on the state index so the search order (and therefore
// the reconstructed route among equal-cost alternatives) is reproducible.
type r2Item struct {
	cost  float64
	state int
}

type r2Heap []r2Item

func (h r2Heap) Len() int { return len(h) }
func (h r2Heap) Less(a, b int) bool {
	if h[a].cost != h[b].cost {
		return h[a].cost < h[b].cost
	}
	return h[a].state < h[b].state
}
func (h r2Heap) Swap(a, b int) { h[a], h[b] = h[b], h[a] }
func (h *r2Heap) Push(x any)   { *h = append(*h, x.(r2Item)) }
func (h *r2Heap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

// route finds an obstacle-avoiding, crossing-aware orthogonal path from the
// src stub to the dst stub on the ground plane (min of the two base heights).
// It returns the world waypoints INCLUDING both stub points, collinear runs
// merged, or nil when no useful path exists (degenerate anchors, oversized
// grid, unreachable goal) — the caller then falls back to the v1 elbow route.
//
// The first leg is seeded with the source's exit heading (turning off it costs
// a bend, exactly like the rendered polyline), immediate 180° reversals are
// forbidden, and a route may not arrive at the target stub moving OUTWARD
// along the target's face normal (that would fold back into the face); if it
// arrives on any heading other than the inward normal, the final stub→anchor
// leg is charged one extra bend.
func route(src, dst routerAnchor, obstacles []planRect, placedRoutes [][][2]float64) [][2]float64 {
	if math.Abs(src.X-dst.X) < 1e-6 && math.Abs(src.Y-dst.Y) < 1e-6 {
		return nil
	}
	routeZ := math.Min(src.Z, dst.Z)

	// Filter obstacles with the scorecard kernel: endpoints' own parts, flat
	// decorations, other z-floors and shared substrates are not obstacles.
	var srcR, dstR planRect
	for _, r := range obstacles {
		if r.id == src.ID {
			srcR = r
		}
		if r.id == dst.ID {
			dstR = r
		}
	}
	var obs []planRect
	for _, r := range obstacles {
		if r.id == src.ID || r.id == dst.ID || r.h <= planThinH ||
			!sameFloor(routeZ, r) || enclosesBoth(r, srcR, dstR) {
			continue
		}
		obs = append(obs, r)
	}
	var placedSegs [][2][2]float64
	for _, pl := range placedRoutes {
		for i := 1; i < len(pl); i++ {
			placedSegs = append(placedSegs, [2][2]float64{pl[i-1], pl[i]})
		}
	}

	// Sparse grid: obstacle boundaries ± clearance, the stub coordinates, and
	// the corridor midlines (so an unobstructed route can still take the
	// balanced staircase v1 would pick).
	xs := []float64{src.X, dst.X, (src.X + dst.X) / 2}
	ys := []float64{src.Y, dst.Y, (src.Y + dst.Y) / 2}
	for _, r := range obs {
		xs = append(xs, r.x-r2Clearance, r.x+r.w+r2Clearance)
		ys = append(ys, r.y-r2Clearance, r.y+r.d+r2Clearance)
	}
	xs, ys = r2Coords(xs), r2Coords(ys)
	nx, ny := len(xs), len(ys)
	if nx > r2MaxGrid || ny > r2MaxGrid {
		return nil
	}
	si, sj := r2Index(xs, src.X), r2Index(ys, src.Y)
	ti, tj := r2Index(xs, dst.X), r2Index(ys, dst.Y)
	if si == ti && sj == tj {
		return nil
	}

	// Per-grid-edge cost, precomputed once (Dijkstra visits edges from both
	// directions). hCost[j*nx+i] prices (i,j)→(i+1,j); vCost (i,j)→(i,j+1).
	segCost := func(ax, ay, bx, by float64) float64 {
		c := r2LenCost * (math.Abs(bx-ax) + math.Abs(by-ay))
		pts := [][2]float64{{ax, ay}, {bx, by}}
		for _, r := range obs {
			if routeHitsRect(pts, r) {
				c += r2TunnelCost
			}
		}
		for _, s := range placedSegs {
			if r2SegCross(pts[0], pts[1], s[0], s[1]) {
				c += r2CrossCost
			}
		}
		return c
	}
	hCost := make([]float64, nx*ny)
	vCost := make([]float64, nx*ny)
	for j := 0; j < ny; j++ {
		for i := 0; i < nx; i++ {
			if i+1 < nx {
				hCost[j*nx+i] = segCost(xs[i], ys[j], xs[i+1], ys[j])
			}
			if j+1 < ny {
				vCost[j*nx+i] = segCost(xs[i], ys[j], xs[i], ys[j+1])
			}
		}
	}

	// Dijkstra over (cell, heading) states.
	nStates := nx * ny * 4
	dist := make([]float64, nStates)
	prev := make([]int, nStates)
	for i := range dist {
		dist[i] = math.Inf(1)
		prev[i] = -1
	}
	stateOf := func(i, j, d int) int { return (j*nx+i)*4 + d }
	start := stateOf(si, sj, r2Dir(src.DX, src.DY))
	dist[start] = 0
	h := &r2Heap{{0, start}}
	for h.Len() > 0 {
		it := heap.Pop(h).(r2Item)
		if it.cost > dist[it.state] {
			continue
		}
		cell, d := it.state/4, it.state%4
		i, j := cell%nx, cell/nx
		for nd := 0; nd < 4; nd++ {
			if nd == d^1 {
				continue // no immediate reversal (doubled-back segment)
			}
			ni, nj, ec := i, j, 0.0
			switch nd {
			case 0:
				if i+1 >= nx {
					continue
				}
				ni, ec = i+1, hCost[j*nx+i]
			case 1:
				if i == 0 {
					continue
				}
				ni, ec = i-1, hCost[j*nx+i-1]
			case 2:
				if j+1 >= ny {
					continue
				}
				nj, ec = j+1, vCost[j*nx+i]
			case 3:
				if j == 0 {
					continue
				}
				nj, ec = j-1, vCost[(j-1)*nx+i]
			}
			c := it.cost + ec
			if nd != d {
				c += r2BendCost
			}
			ns := stateOf(ni, nj, nd)
			if c < dist[ns] {
				dist[ns] = c
				prev[ns] = it.state
				heap.Push(h, r2Item{c, ns})
			}
		}
	}

	// Pick the cheapest arrival heading at the target stub. Arriving while
	// moving OUTWARD along the target normal is forbidden; any heading other
	// than the inward normal costs one terminal bend (the stub→anchor leg).
	outDir := r2Dir(dst.DX, dst.DY)
	inDir := outDir ^ 1
	best, bestCost := -1, math.Inf(1)
	for d := 0; d < 4; d++ {
		if d == outDir {
			continue
		}
		s := stateOf(ti, tj, d)
		c := dist[s]
		if d != inDir {
			c += r2BendCost
		}
		if c < bestCost {
			best, bestCost = s, c
		}
	}
	if best < 0 || math.IsInf(bestCost, 1) {
		return nil
	}

	// Reconstruct, then merge collinear runs so the polyline carries only
	// genuine corners.
	var cells []int
	for s := best; s >= 0; s = prev[s] {
		cells = append(cells, s/4)
	}
	pts := make([][2]float64, 0, len(cells))
	for k := len(cells) - 1; k >= 0; k-- {
		cell := cells[k]
		p := [2]float64{xs[cell%nx], ys[cell/nx]}
		if n := len(pts); n > 0 && pts[n-1] == p {
			continue
		}
		if n := len(pts); n >= 2 {
			a, b := pts[n-2], pts[n-1]
			if (a[0] == b[0] && b[0] == p[0]) || (a[1] == b[1] && b[1] == p[1]) {
				pts[n-1] = p
				continue
			}
		}
		pts = append(pts, p)
	}
	if len(pts) < 2 {
		return nil
	}
	return pts
}

// router2ChannelW is the v2 fan-out channel width for one endpoint: the face
// span (the part's extent along the face tangent) divided evenly across the
// n staggered exits, clamped to [6, 24] world units. v1's hard-coded 14 only
// holds for the default 70..200-unit parts; narrow faces overflowed and wide
// faces bunched every exit in the middle.
func router2ChannelW(ar *anchorResolver, ref string, n int) float64 {
	if n < 2 {
		return 14
	}
	id, _ := ar.parse(ref)
	p, ok := ar.byID[id]
	if !ok {
		return 14
	}
	dx, dy := ar.exit(ref)
	span := p.d // exit along x → the face runs along y
	if math.Abs(dy) > math.Abs(dx) {
		span = p.w // exit along y → the face runs along x
	}
	w := span / float64(n+1)
	if w < 6 {
		w = 6
	}
	if w > 24 {
		w = 24
	}
	return w
}
