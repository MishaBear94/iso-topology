package isotopo

import (
	"math"
	"sort"
)

// regionArea integrates a polygon union. Unlike summing face areas it neither
// fills concave notches nor counts overlapping faces twice. Every edge crossing
// is a slab boundary, so scanline interval endpoints are linear in each slab;
// midpoint integration is exact for the polygonal approximation.
func regionArea(a screenRegion) float64 { return regionIntersectionArea(a, nil) }

func regionBounds(r screenRegion) occRect {
	b := occRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range r {
		for _, v := range p {
			b.x0 = math.Min(b.x0, v[0])
			b.x1 = math.Max(b.x1, v[0])
			b.y0 = math.Min(b.y0, v[1])
			b.y1 = math.Max(b.y1, v[1])
		}
	}
	return b
}

type regionEdge struct{ a, b [2]float64 }
type regionInterval struct{ lo, hi float64 }

// Each area calculation owns two scanlines. Reuse their storage between slabs,
// keeping the same sorting and floating-point accumulation order.
type regionScanline struct {
	ys    []float64
	spans []regionInterval
}

func (s *regionScanline) intervals(r screenRegion, x float64) []regionInterval {
	spans := s.spans[:0]
	for _, poly := range r {
		ys := s.ys[:0]
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			if (a[0] > x) != (b[0] > x) {
				ys = append(ys, a[1]+(x-a[0])*(b[1]-a[1])/(b[0]-a[0]))
			}
		}
		sort.Float64s(ys)
		for i := 1; i < len(ys); i += 2 {
			if ys[i] > ys[i-1] {
				spans = append(spans, regionInterval{ys[i-1], ys[i]})
			}
		}
		s.ys = ys
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].lo < spans[j].lo })
	out := spans[:0]
	for _, s := range spans {
		n := len(out)
		if n > 0 && s.lo <= out[n-1].hi {
			out[n-1].hi = math.Max(out[n-1].hi, s.hi)
		} else {
			out = append(out, s)
		}
	}
	s.spans = out
	return out
}

func regionIntersectionArea(a, b screenRegion) float64 {
	if len(a) == 0 || (b != nil && len(b) == 0) {
		return 0
	}
	box := regionBounds(a)
	if b != nil {
		q := regionBounds(b)
		if !occOverlap(box, q) {
			return 0
		}
		box.x0, box.x1 = math.Max(box.x0, q.x0), math.Min(box.x1, q.x1)
	}
	xs := []float64{box.x0, box.x1}
	var edges []regionEdge
	for _, r := range []screenRegion{a, b} {
		for _, poly := range r {
			for i, p := range poly {
				q := poly[(i+1)%len(poly)]
				if p[0] > box.x0 && p[0] < box.x1 {
					xs = append(xs, p[0])
				}
				if p[0] == q[0] || math.Max(p[0], q[0]) <= box.x0 || math.Min(p[0], q[0]) >= box.x1 {
					continue
				}
				if p[0] > q[0] {
					p, q = q, p
				}
				edges = append(edges, regionEdge{p, q})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].a[0] < edges[j].a[0] })
	for i, e := range edges {
		for j := i + 1; j < len(edges) && edges[j].a[0] < e.b[0]; j++ {
			f := edges[j]
			ex, ey := e.b[0]-e.a[0], e.b[1]-e.a[1]
			fx, fy := f.b[0]-f.a[0], f.b[1]-f.a[1]
			den := ex*fy - ey*fx
			if den == 0 {
				continue
			}
			dx, dy := f.a[0]-e.a[0], f.a[1]-e.a[1]
			t, u := (dx*fy-dy*fx)/den, (dx*ey-dy*ex)/den
			if t > 0 && t < 1 && u > 0 && u < 1 {
				x := e.a[0] + t*ex
				if x > box.x0 && x < box.x1 {
					xs = append(xs, x)
				}
			}
		}
	}
	sort.Float64s(xs)
	total := 0.0
	var scanA, scanB regionScanline
	for i := 1; i < len(xs); i++ {
		width := xs[i] - xs[i-1]
		if width <= 0 {
			continue
		}
		x := (xs[i] + xs[i-1]) / 2
		aa := scanA.intervals(a, x)
		length := 0.0
		if b == nil {
			for _, s := range aa {
				length += s.hi - s.lo
			}
		} else {
			bb := scanB.intervals(b, x)
			for j, k := 0, 0; j < len(aa) && k < len(bb); {
				length += math.Max(0, math.Min(aa[j].hi, bb[k].hi)-math.Max(aa[j].lo, bb[k].lo))
				if aa[j].hi < bb[k].hi {
					j++
				} else {
					k++
				}
			}
		}
		total += width * length
	}
	return total
}
