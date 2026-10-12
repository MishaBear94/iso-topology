package isotopo

import (
	"math"
	"slices"
)

// Joint placement is an optional bounded improvement after the complete old
// arrangement (including body refinement) has settled. Clear labels are fixed.
// Every variable can retain its exact blocked placement or choose a fully clear
// route position. A changed pill must clear even the unchanged blocked pills.
// Thus partial solutions cannot buy a gain by moving a remaining obstruction.
// Budgets bound geometry work/search, not elapsed time or mathematical feasibility.
type connectorTextJointBudget struct {
	roots, checks int
}

type connectorTextJointChoice struct {
	placement connectorTextPlacement
	distance  float64
}

func connectorTextJointCandidates(req connectorTextRequest, old connectorTextPlacement, boxes []screenRect, strokes []connectorTextStroke, budget *connectorTextJointBudget, regions ...screenRegion) []connectorTextJointChoice {
	if len(req.points) < 2 || req.curved && len(req.points) != 3 {
		return nil
	}
	segments := len(req.points) - 1
	if req.curved {
		segments = 1
	}
	work := 4*len(boxes) + 8*len(strokes) + 2
	if segments > budget.roots/work {
		return nil
	}
	budget.roots -= segments * work
	var polygons screenRegion
	storage := make([][2]float64, 0, 8*len(strokes))
	for _, stroke := range strokes {
		start := len(storage)
		storage = stroke.appendForbidden(storage, req.block.width, req.block.height)
		polygons = append(polygons, storage[start:len(storage)])
	}
	total := 1.
	if !req.curved {
		total = planLen(req.points)
	}
	seen := map[screenRect]bool{}
	width, height := svgCoord(req.block.width), svgCoord(req.block.height)
	var interiors screenRegion
	if len(regions) > 0 {
		interiors = regions[0]
	}
	var out []connectorTextJointChoice
	search := func(ax, bx, cx, ay, by, cy, walked, length float64) {
		// Merge actual forbidden intervals before enumerating candidates.
		// Infinite support-line cuts alone produce many interior subdivisions
		// that all represent the same blocked corridor.
		var spans []connectorTextBlockedSpan
		for _, b := range boxes {
			x0, x1 := b.x0-req.block.width/2-6.03, b.x1+req.block.width/2+6.03
			y0, y1 := b.y0-req.block.height/2-6.03, b.y1+req.block.height/2+6.03
			poly := [4][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
			spans = connectorTextBlockedIntervals(spans, poly[:], 0, ax, bx, cx, ay, by, cy)
		}
		for _, poly := range polygons {
			spans = connectorTextBlockedIntervals(spans, poly, 0, ax, bx, cx, ay, by, cy)
		}
		indexConnectorTextSpans(spans)
		var free [][2]float64
		end := 0.
		for _, span := range spans {
			if span.lo >= end {
				free = append(free, [2]float64{end, span.lo})
			}
			end = math.Max(end, span.hi)
		}
		if end <= 1 {
			free = append(free, [2]float64{end, 1})
		}
		preferred := .5
		if length > 0 {
			preferred = (total/2 - walked) / length
		}
		for _, span := range free {
			lo, hi := span[0], span[1]
			// Interval ends let neighboring labels share a corridor instead
			// of each reserving its middle. Interior alternatives retain room
			// for three or more labels; this finite domain is not a complete
			// solver of continuous multi-label packing.
			for _, t := range []float64{math.Max(lo, math.Min(hi, preferred)), lo, hi, (lo + hi) / 2, lo + (hi-lo)/4, hi - (hi-lo)/4} {
				if budget.checks <= 0 {
					return
				}
				x, y := (ax*t+bx)*t+cx, (ay*t+by)*t+cy
				left, top := svgCoord(x-req.block.width/2), svgCoord(y-req.block.height/2)
				box := screenRect{left, top, left + width, top + height}
				if seen[box] {
					continue
				}
				seen[box] = true
				hit, checked := connectorTextCollisionWorkInRegions(box, boxes, strokes, interiors)
				budget.checks -= max(1, checked)
				if hit >= 0 {
					continue
				}
				out = append(out, connectorTextJointChoice{connectorTextPlacement{x, y, true, box}, math.Hypot(x-old.x, y-old.y)})
			}
		}
	}
	if req.curved {
		p, q, r := req.points[0], req.points[1], req.points[2]
		search(p[0]-2*q[0]+r[0], 2*(q[0]-p[0]), p[0], p[1]-2*q[1]+r[1], 2*(q[1]-p[1]), p[1], 0, 1)
	} else {
		walked := 0.
		for i := 1; i < len(req.points) && budget.checks > 0; i++ {
			p, q := req.points[i-1], req.points[i]
			length := math.Hypot(q[0]-p[0], q[1]-p[1])
			search(0, q[0]-p[0], p[0], 0, q[1]-p[1], p[1], walked, length)
			walked += length
		}
	}
	slices.SortStableFunc(out, func(a, b connectorTextJointChoice) int {
		if a.distance < b.distance {
			return -1
		}
		if a.distance > b.distance {
			return 1
		}
		return 0
	})
	return out
}

func refineConnectorTextJoint(positions []connectorTextPlacement, requests []connectorTextRequest, bodies, legacyPills []screenRect, obstacles []connectorTextObstacle, strokes []connectorTextStroke, source connectorTextBodySource) {
	blocked := 0
	for _, p := range positions {
		if !p.clear {
			blocked++
		}
	}
	if blocked < 2 || source == nil || len(legacyPills) > 0 {
		return
	}
	static := append(append([]screenRect(nil), bodies...), legacyPills...)
	for _, o := range obstacles {
		if o.owner < 0 {
			static = append(static, o.rect)
		}
	}
	for _, p := range positions {
		if p.clear {
			static = append(static, p.box)
		}
	}
	budget := connectorTextJointBudget{roots: 8192, checks: 65536}
	domains := make([][]connectorTextJointChoice, len(positions))
	var content connectorTextBodies
	loaded := false
	other := make([]connectorTextStroke, 0, len(strokes))
	for i, req := range requests {
		if positions[i].clear {
			continue
		}
		other = other[:0]
		for _, s := range strokes {
			if s.owner != req.index {
				other = append(other, s)
			}
		}
		domains[i] = connectorTextJointCandidates(req, positions[i], static, other, &budget)
		if len(domains[i]) > 0 && !loaded {
			var ok bool
			content, ok = source()
			if !ok {
				return
			}
			loaded = true
		}
		// AABB clearance alone says nothing about text overhangs or effects.
		// Keep the old capability fallback and independently protect measured
		// body/content geometry before offering a candidate to the solver.
		beforeContent := len(domains[i])
		domains[i] = slices.DeleteFunc(domains[i], func(c connectorTextJointChoice) bool {
			if budget.checks <= 0 {
				return true
			}
			hit, checked := connectorTextCollisionWorkInRegions(c.placement.box, nil, content.borders, content.interiors)
			budget.checks -= max(1, checked)
			return hit >= 0
		})
		if len(domains[i]) < beforeContent && budget.checks > 0 {
			// Body outlines/content can cut a little further into a free AABB
			// corridor. Rebuild its real boundaries instead of just discarding
			// endpoint choices and losing an otherwise usable narrow slot.
			withBody := append(append([]connectorTextStroke(nil), other...), content.borders...)
			more := connectorTextJointCandidates(req, positions[i], static, withBody, &budget, content.interiors)
			for _, c := range more {
				if !slices.ContainsFunc(domains[i], func(p connectorTextJointChoice) bool { return p.placement.box == c.placement.box }) {
					domains[i] = append(domains[i], c)
				}
			}
			slices.SortStableFunc(domains[i], func(a, b connectorTextJointChoice) int {
				if a.distance < b.distance {
					return -1
				}
				if a.distance > b.distance {
					return 1
				}
				return 0
			})
		}
	}
	// Requests without a candidate remain fixed obstacles. Propagate that
	// fact before building components; no candidate may borrow their slots.
	for again := true; again; {
		again = false
		for i, domain := range domains {
			if len(domain) == 0 {
				continue
			}
			domains[i] = slices.DeleteFunc(domain, func(c connectorTextJointChoice) bool {
				for j, p := range positions {
					if j != i && !p.clear && len(domains[j]) == 0 && collides(c.placement.box, []screenRect{p.box}) {
						return true
					}
				}
				return false
			})
			again = again || len(domains[i]) == 0
		}
	}
	// Conservative domain bounds split independent components cheaply. They
	// only determine work grouping, never certify a placement as clear.
	bounds := make([]screenRect, len(positions))
	for i, domain := range domains {
		bounds[i] = positions[i].box
		for _, c := range domain {
			b := c.placement.box
			bounds[i] = screenRect{math.Min(bounds[i].x0, b.x0), math.Min(bounds[i].y0, b.y0), math.Max(bounds[i].x1, b.x1), math.Max(bounds[i].y1, b.y1)}
		}
	}
	visited := make([]bool, len(positions))
	searchBudget := 65536
	for root, domain := range domains {
		if visited[root] || len(domain) == 0 {
			continue
		}
		component := []int{root}
		visited[root] = true
		for head := 0; head < len(component); head++ {
			for j := range positions {
				if !visited[j] && len(domains[j]) > 0 && collides(bounds[component[head]], []screenRect{bounds[j]}) {
					visited[j] = true
					component = append(component, j)
				}
			}
		}
		solveConnectorTextJointComponent(positions, domains, component, &searchBudget)
	}
}

func solveConnectorTextJointComponent(positions []connectorTextPlacement, domains [][]connectorTextJointChoice, component []int, budget *int) {
	// Fewest alternatives first tends to reserve narrow corridors before
	// flexible routes. Stable ordering makes equal candidates deterministic.
	slices.SortStableFunc(component, func(a, b int) int { return len(domains[a]) - len(domains[b]) })
	current := make([]connectorTextPlacement, len(component))
	best := make([]connectorTextPlacement, len(component))
	for i, index := range component {
		best[i] = positions[index]
	}
	bestCount, bestDistance := 0, math.Inf(1)
	var search func(int, int, float64)
	search = func(depth, count int, distance float64) {
		if *budget <= 0 || count+len(component)-depth < bestCount || count+len(component)-depth == bestCount && distance >= bestDistance {
			return
		}
		*budget--
		if depth == len(component) {
			if count > bestCount || count == bestCount && distance < bestDistance {
				copy(best, current)
				bestCount, bestDistance = count, distance
			}
			return
		}
		index := component[depth]
		try := func(p connectorTextPlacement, extra float64) {
			for j := 0; j < depth; j++ {
				*budget--
				if *budget <= 0 || (p.clear || current[j].clear) && collides(p.box, []screenRect{current[j].box}) {
					return
				}
			}
			current[depth] = p
			nextCount := count
			if p.clear {
				nextCount++
			}
			search(depth+1, nextCount, distance+extra)
		}
		for _, c := range domains[index] {
			if *budget <= 0 {
				break
			}
			try(c.placement, c.distance)
		}
		try(positions[index], 0)
	}
	search(0, 0, 0)
	if bestCount == 0 {
		return
	}
	for i, index := range component {
		positions[index] = best[i]
	}
}
