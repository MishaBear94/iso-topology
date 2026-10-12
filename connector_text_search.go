package isotopo

import (
	"cmp"
	"math"
	"slices"
)

// Intervals are search hints, never clearance certificates. A hint carries
// its original obstacle so the existing serialized-box predicate can confirm
// a rejection. A missed or inaccurate interval only causes a full scan.
type connectorTextBlockedSpan struct {
	lo, hi   float64
	obstacle int // rects first, then strokes
}

// Count actual predicate work so a cheap rejection near the front of the
// obstacle list does not pay for an index it cannot amortize. Keep these
// predicates shared with the direct scan and final scene validation.
func connectorTextCollisionWork(box screenRect, obstacles []screenRect, strokes []connectorTextStroke) (witness, work int) {
	return connectorTextCollisionWorkInRegions(box, obstacles, strokes, nil)
}

func connectorTextCollisionWorkInRegions(box screenRect, obstacles []screenRect, strokes []connectorTextStroke, interiors screenRegion) (witness, work int) {
	for i := range obstacles {
		if collides(box, obstacles[i:i+1]) {
			return i, i + 1
		}
	}
	for i := range strokes {
		if connectorTextStrokeHits(box, strokes[i:i+1]) {
			return len(obstacles) + i, len(obstacles) + i + 1
		}
	}
	if connectorTextInteriorHits(box, interiors) {
		return len(obstacles) + len(strokes), len(obstacles) + len(strokes) + len(interiors)
	}
	return -1, len(obstacles) + len(strokes) + len(interiors)
}

// Keep the original root arithmetic: these roots also define the historic
// candidate set, including its rounding guard and preferred-position ties.
func connectorTextRoots(dst []float64, a, b, c float64) []float64 {
	add := func(t float64) {
		if t > 0 && t < 1 {
			dst = append(dst, t)
		}
	}
	if math.Abs(a) < 1e-12 {
		if math.Abs(b) > 1e-12 {
			add(-c / b)
		}
		return dst
	}
	d := b*b - 4*a*c
	if d < 0 {
		return dst
	}
	q := -.5 * (b + math.Copysign(math.Sqrt(d), b))
	if q == 0 {
		add(-b / (2 * a))
	} else {
		add(q / a)
		add(c / q)
	}
	return dst
}

func connectorTextPolygonRoots(dst []float64, poly [][2]float64, ax, bx, cx, ay, by, cy float64) []float64 {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		dst = connectorTextRoots(dst, -dy*ax+dx*ay, -dy*bx+dx*by, -dy*(cx-a[0])+dx*(cy-a[1]))
	}
	return dst
}

// A straight or quadratic route has at most two roots per polygon edge.
// Classify each open interval separately: a quadratic can leave and re-enter
// a convex obstacle. Treating its first/last roots as one block would be wrong.
func connectorTextBlockedIntervals(dst []connectorTextBlockedSpan, poly [][2]float64, obstacle int, ax, bx, cx, ay, by, cy float64) []connectorTextBlockedSpan {
	if ax == 0 && ay == 0 {
		// A line intersects a convex polygon in one interval. Clip against
		// its half-planes directly instead of sorting roots and classifying
		// every interval; most submitted routes consist of straight segments.
		lo, hi := 0.0, 1.0
		for i, p := range poly {
			q := poly[(i+1)%len(poly)]
			dx, dy := q[0]-p[0], q[1]-p[1]
			a, b := dx*by-dy*bx, dx*(cy-p[1])-dy*(cx-p[0])
			if a == 0 {
				if b < 0 {
					return dst
				}
				continue
			}
			if t := -b / a; a > 0 {
				lo = math.Max(lo, t)
			} else {
				hi = math.Min(hi, t)
			}
			if hi < lo {
				return dst
			}
		}
		return append(dst, connectorTextBlockedSpan{lo, hi, obstacle})
	}
	var storage [20]float64
	cuts := connectorTextPolygonRoots(append(storage[:0], 0, 1), poly, ax, bx, cx, ay, by, cy)
	slices.Sort(cuts)
	cuts = slices.Compact(cuts)
	for i := 1; i < len(cuts); i++ {
		t := cuts[i-1] + (cuts[i]-cuts[i-1])/2
		x, y := (ax*t+bx)*t+cx, (ay*t+by)*t+cy
		inside := true
		for j, a := range poly {
			b := poly[(j+1)%len(poly)]
			if (b[0]-a[0])*(y-a[1])-(b[1]-a[1])*(x-a[0]) < 0 {
				inside = false
				break
			}
		}
		if inside {
			dst = append(dst, connectorTextBlockedSpan{cuts[i-1], cuts[i], obstacle})
		}
	}
	return dst
}

// Each prefix stores the furthest-reaching interval and its witness. A binary
// search then finds an obstacle covering t in O(log n), without retaining all
// overlapping obstacles or losing which original predicate must be checked.
func indexConnectorTextSpans(spans []connectorTextBlockedSpan) {
	slices.SortFunc(spans, func(a, b connectorTextBlockedSpan) int {
		if c := cmp.Compare(a.lo, b.lo); c != 0 {
			return c
		}
		if c := cmp.Compare(b.hi, a.hi); c != 0 {
			return c
		}
		return cmp.Compare(a.obstacle, b.obstacle)
	})
	for i := 1; i < len(spans); i++ {
		if spans[i-1].hi > spans[i].hi {
			spans[i].hi, spans[i].obstacle = spans[i-1].hi, spans[i-1].obstacle
		}
	}
}

func connectorTextSpanWitness(spans []connectorTextBlockedSpan, t float64) (int, bool) {
	lo, hi := 0, len(spans)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if spans[mid].lo <= t {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 || spans[lo-1].hi < t {
		return 0, false
	}
	return spans[lo-1].obstacle, true
}

func sortConnectorTextCandidates(candidates []float64, walked, length, midpoint float64) {
	slices.SortFunc(candidates, func(a, b float64) int {
		ad := math.Abs(walked + length*a - midpoint)
		bd := math.Abs(walked + length*b - midpoint)
		if c := cmp.Compare(ad, bd); c != 0 {
			return c
		}
		// The historic scan visited intervals in ascending t. Preserve its
		// strict-improvement tie rule even when searching by distance first.
		return cmp.Compare(a, b)
	})
}
