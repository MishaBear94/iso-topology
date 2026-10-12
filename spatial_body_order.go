package isotopo

import "math"

// painterBodyRelation compares nearest envelopes, not arbitrary face pairs.
// A hidden face or border is allowed to be behind the other body even when
// this body's visible surface is wholly in front. Querying both directions
// supplies the two existential strict-front predicates without building a
// global arrangement of every face and round border.
func painterBodyRelation(a, b painterNode, work *int) (int, bool) {
	*work--
	if *work < 0 {
		return 0, false
	}
	if a.orderLocked || b.orderLocked {
		// Body geometry does not yet certify text/image ink or its depth.
		// Preserve those complete groups, including off-face content.
		return 2, true
	}
	// Reuse per-piece bounds and charge even rejected pairs. Large separated
	// scenes must not bypass the work budget by repeating polygon scans.
	bound := func(n painterNode) (occRect, bool) {
		b := occRect{x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)}
		for _, p := range n.pieces {
			*work--
			if *work < 0 {
				return b, false
			}
			b.x0, b.y0 = math.Min(b.x0, p.bounds.x0), math.Min(b.y0, p.bounds.y0)
			b.x1, b.y1 = math.Max(b.x1, p.bounds.x1), math.Max(b.y1, p.bounds.y1)
		}
		return b, true
	}
	aa, ok := bound(a)
	if !ok {
		return 0, false
	}
	bb, ok := bound(b)
	if !ok {
		return 0, false
	}
	guard := 2 * spatialPainterGuard
	if aa.x1+guard < bb.x0 || bb.x1+guard < aa.x0 || aa.y1+guard < bb.y0 || bb.y1+guard < aa.y0 {
		return 0, true
	}
	// Affine extrema occur at polygon vertices. Disjoint depth ranges prove
	// a uniform order without subtraction, including coincident silhouettes.
	ranges := [2][2]float64{{math.Inf(1), math.Inf(-1)}, {math.Inf(1), math.Inf(-1)}}
	var world [2][2][3]float64
	for i := range world {
		for k := 0; k < 3; k++ {
			world[i][0][k], world[i][1][k] = math.Inf(1), math.Inf(-1)
		}
	}
	for i, node := range []painterNode{a, b} {
		for _, p := range node.pieces {
			for _, v := range p.poly {
				*work--
				if *work < 0 {
					return 0, false
				}
				d := p.depth.at(v)
				ranges[i][0], ranges[i][1] = math.Min(ranges[i][0], d), math.Max(ranges[i][1], d)
				z := (d - 2*v[1]) / 3
				x, y := isoUnproject(v[0], v[1], z)
				for k, c := range [3]float64{x, y, z} {
					world[i][0][k], world[i][1][k] = math.Min(world[i][0][k], c), math.Max(world[i][1][k], c)
				}
			}
		}
	}
	// Every view ray advances along all three positive world axes. If the
	// complete ink surfaces (including round borders) separate on any axis,
	// that separation certifies their order wherever their projections meet.
	// The inverse is affine, so vertex extrema bound entire surface pieces.
	separated := 0
	for k := 0; k < 3; k++ {
		if world[0][0][k] > world[1][1][k]+.0001 {
			separated |= 1
		}
		if world[1][0][k] > world[0][1][k]+.0001 {
			separated |= 2
		}
	}
	if separated == 1 || separated == 2 {
		return separated, true
	}
	if separated == 3 {
		return 2, true
	} // no shared ray; preserve AA tie order
	if ranges[0][0] > ranges[1][1]+.0001 {
		return 1, true
	}
	if ranges[1][0] > ranges[0][1]+.0001 {
		return 2, true
	}
	// A negative contact tolerance removes ties from the visible set. Thus
	// bit 1 certifies a strictly nearer piece of the nearest body envelope.
	ab, ok := painterSubjectRelationTolerance(a, b, work, -.0001)
	if !ok {
		return 0, false
	}
	ba, ok := painterSubjectRelationTolerance(b, a, work, -.0001)
	if !ok {
		return 0, false
	}
	relation := ab&1 | (ba&1)<<1
	if relation == 0 {
		// Coplanar contact, or nearby disjoint ink whose antialias guards may
		// meet: retain established order rather than inventing a depth tie.
		return 2, true
	}
	return relation, true
}
