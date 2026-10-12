package isotopo

import (
	"math"
	"sort"
)

type quadratic [3][2]float64

func (q quadratic) at(t float64) [2]float64 {
	u := 1 - t
	return [2]float64{
		u*u*q[0][0] + 2*u*t*q[1][0] + t*t*q[2][0],
		u*u*q[0][1] + 2*u*t*q[1][1] + t*t*q[2][1],
	}
}

// portion preserves the exact locus and tangents of q on [lo, hi]. Its
// control follows from the derivative at lo, with the parameter rescaled.
func (q quadratic) portion(lo, hi float64) quadratic {
	a, b := q.at(lo), q.at(hi)
	var c [2]float64
	for k := range c {
		c[k] = a[k] + (hi-lo)*((1-lo)*(q[1][k]-q[0][k])+lo*(q[2][k]-q[1][k]))
	}
	return quadratic{a, c, b}
}

// quadraticRoots uses the stable quadratic formula to avoid cancelling a
// near-zero root. Scaling also makes the linear/tangent limits predictable.
func quadraticRoots(a, b, c float64) []float64 {
	s := math.Max(math.Abs(a), math.Max(math.Abs(b), math.Abs(c)))
	if s == 0 {
		return nil // the curve lies on the supporting line
	}
	a, b, c = a/s, b/s, c/s
	if math.Abs(a) < 1e-14 {
		if math.Abs(b) < 1e-14 {
			return nil
		}
		return []float64{-c / b}
	}
	d := b*b - 4*a*c
	if d < -1e-14*(b*b+math.Abs(4*a*c)) {
		return nil
	}
	if d <= 0 {
		return []float64{-b / (2 * a)}
	}
	u := -.5 * (b + math.Copysign(math.Sqrt(d), b))
	return []float64{u / a, c / u}
}

// polygonContains includes the boundary and supports concave simple outlines.
// Tangencies are classified by open intervals, not by an assumed root order.
func polygonContains(p [2]float64, poly [][2]float64) bool {
	const epsilon = 1e-8
	inside := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		if math.Abs((p[0]-a[0])*dy-(p[1]-a[1])*dx) <= epsilon*math.Max(1, math.Hypot(dx, dy)) &&
			p[0] >= math.Min(a[0], b[0])-epsilon && p[0] <= math.Max(a[0], b[0])+epsilon &&
			p[1] >= math.Min(a[1], b[1])-epsilon && p[1] <= math.Max(a[1], b[1])+epsilon {
			return true
		}
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}

// endpointBoundary finds the boundary of the contiguous endpoint portion
// inside poly. Intersections are exact quadratic/edge roots. A tangent alone
// never clips the curve. All-inside curves have no exposed endpoint to dock;
// leave them intact for the existing renderer's overlap behavior.
func (q quadratic) endpointBoundary(poly [][2]float64, start bool) (float64, bool) {
	return q.endpointBoundaryRegion(screenRegion{poly}, start)
}

func (q quadratic) endpointBoundaryRegion(region screenRegion, start bool) (float64, bool) {
	end := 1.0
	if start {
		end = 0
	}
	if !region.containsEndpoint(q.at(end)) {
		return end, false
	}
	ts := []float64{0, 1}
	for _, poly := range region {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			dx, dy := b[0]-a[0], b[1]-a[1]
			den := dx*dx + dy*dy
			if den < 1e-20 {
				continue
			}
			cross := func(x, y float64) float64 { return x*dy - y*dx }
			roots := quadraticRoots(
				cross(q[0][0]-2*q[1][0]+q[2][0], q[0][1]-2*q[1][1]+q[2][1]),
				2*cross(q[1][0]-q[0][0], q[1][1]-q[0][1]),
				cross(q[0][0]-a[0], q[0][1]-a[1]))
			for _, t := range roots {
				if t < -1e-10 || t > 1+1e-10 {
					continue
				}
				t = math.Max(0, math.Min(1, t))
				p := q.at(t)
				u := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / den
				if u >= -1e-10 && u <= 1+1e-10 {
					ts = append(ts, t)
				}
			}
		}
	}
	sort.Float64s(ts)
	boundary, found := end, false
	for i := 1; i < len(ts); i++ {
		if ts[i]-ts[i-1] < 1e-12 || region.contains(q.at((ts[i]+ts[i-1])/2)) {
			continue
		}
		if start {
			return ts[i-1], true
		}
		boundary, found = ts[i], true
	}
	return boundary, found
}

type curveSample struct {
	p [2]float64
	t float64
}

// samples adapts to SCREEN-space curvature, with a bounded subdivision count.
// A control point within tolerance of its chord segment bounds the quadratic's
// deviation by that tolerance (convex hull property). Samples serve labels,
// collision rectangles and short endpoint insets; SVG still emits an exact Q.
func (q quadratic) samples(tolerance float64) []curveSample {
	out := []curveSample{{q[0], 0}}
	var split func(quadratic, float64, float64, int)
	split = func(c quadratic, lo, hi float64, depth int) {
		dx, dy := c[2][0]-c[0][0], c[2][1]-c[0][1]
		t := 0.0
		if den := dx*dx + dy*dy; den > 0 {
			t = math.Max(0, math.Min(1, ((c[1][0]-c[0][0])*dx+(c[1][1]-c[0][1])*dy)/den))
		}
		d := math.Hypot(c[1][0]-c[0][0]-t*dx, c[1][1]-c[0][1]-t*dy)
		if d <= tolerance || depth == 16 {
			out = append(out, curveSample{c[2], hi})
			return
		}
		mid := (lo + hi) / 2
		split(c.portion(0, .5), lo, mid, depth+1)
		split(c.portion(.5, 1), mid, hi, depth+1)
	}
	split(q, 0, 1, 0)
	return out
}

func clipQuadraticEndpoints(pts, source, target [][2]float64, startInset, endInset float64) [][2]float64 {
	return clipQuadraticRegions(pts, screenRegion{source}, screenRegion{target}, startInset, endInset)
}

func clipQuadraticRegions(pts [][2]float64, source, target screenRegion, startInset, endInset float64) [][2]float64 {
	return clipQuadraticRegionsTracked(pts, source, target, startInset, endInset, nil)
}

func clipQuadraticRegionsTracked(pts [][2]float64, source, target screenRegion, startInset, endInset float64, trace *connectorPathTrace) [][2]float64 {
	q := quadratic{pts[0], pts[1], pts[2]}
	lo, trimStart := q.endpointBoundaryRegion(source, true)
	hi, trimEnd := q.endpointBoundaryRegion(target, false)
	if lo >= hi-1e-12 || (!trimStart && !trimEnd) {
		return pts
	}
	visible := q.portion(lo, hi)
	// Measure insets along the curve, never along its control polygon. Keep
	// very short exposed spans rather than reversing or deleting the route.
	samples := visible.samples(.01)
	lengths := make([]float64, len(samples))
	for i := 1; i < len(samples); i++ {
		lengths[i] = lengths[i-1] + math.Hypot(samples[i].p[0]-samples[i-1].p[0], samples[i].p[1]-samples[i-1].p[1])
	}
	if !trimStart {
		startInset = 0
	}
	if !trimEnd {
		endInset = 0
	}
	total := lengths[len(lengths)-1]
	if total > startInset+endInset {
		parameterAt := func(distance float64) float64 {
			for i := 1; i < len(samples); i++ {
				if lengths[i] >= distance && lengths[i] > lengths[i-1] {
					u := (distance - lengths[i-1]) / (lengths[i] - lengths[i-1])
					return samples[i-1].t + u*(samples[i].t-samples[i-1].t)
				}
			}
			return 1
		}
		a, b := parameterAt(startInset), parameterAt(total-endInset)
		visible = visible.portion(a, b)
		lo, hi = lo+a*(hi-lo), lo+b*(hi-lo)
	}
	if trace != nil {
		trace.legs[0].portion(lo, hi)
	}
	return [][2]float64{visible[0], visible[1], visible[2]}
}
