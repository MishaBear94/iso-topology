package iso25d

import (
	"math"
	"sort"
)

// concaveContentRect searches rectangles made from fully interior grid cells.
// Polygon vertices augment the grid so narrow axis-aligned arms aren't lost.
// Whole-cell containment, rather than testing only rectangle corners, keeps
// U/C-shaped notches out of the result. This is a bounded conservative search,
// not a claim of the globally largest continuous rectangle.
func concaveContentRect(poly [][2]float64, w, d float64) ContentRect {
	axes := func(axis int, extent float64) []float64 {
		a := make([]float64, 25)
		for i := range a {
			a[i] = float64(i) * extent / 24
		}
		if len(poly) <= 128 {
			for _, p := range poly {
				a = append(a, p[axis])
			}
		}
		sort.Float64s(a)
		out := a[:0]
		for _, v := range a {
			if len(out) == 0 || v > out[len(out)-1] {
				out = append(out, v)
			}
		}
		return out
	}
	xs, ys := axes(0, w), axes(1, d)
	heights := make([]float64, len(xs)-1)
	best := ContentRect{}
	bestArea := 0.
	for j := 0; j < len(ys)-1; j++ {
		for i := range heights {
			if rectInSimplePolygon(poly, xs[i], ys[j], xs[i+1], ys[j+1]) {
				heights[i] += ys[j+1] - ys[j]
			} else {
				heights[i] = 0
			}
		}
		type bar struct {
			start int
			h     float64
		}
		var stack []bar
		for i := 0; i <= len(heights); i++ {
			h := 0.
			if i < len(heights) {
				h = heights[i]
			}
			start := i
			for len(stack) > 0 && stack[len(stack)-1].h > h {
				v := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				start = v.start
				rw := xs[i] - xs[v.start]
				area := rw * v.h
				if area > bestArea {
					bestArea = area
					best = ContentRect{X: xs[v.start], Y: ys[j+1] - v.h, W: rw, H: v.h}
				}
			}
			if h > 0 && (len(stack) == 0 || stack[len(stack)-1].h < h) {
				stack = append(stack, bar{start, h})
			}
		}
	}
	if bestArea > 0 {
		return best
	}
	// A thin diagonal polygon may contain no grid cell. Try a verified local
	// box around a known interior scanline point instead of using the bbox void.
	if x, y, ok := interiorTopPoint(poly, w/2, d/2); ok {
		r := math.Min(w, d) / 2
		for i := 0; i < 48; i++ {
			if rectInSimplePolygon(poly, x-r, y-r, x+r, y+r) {
				return ContentRect{X: x - r, Y: y - r, W: 2 * r, H: 2 * r}
			}
			r /= 2
		}
	}
	return ContentRect{}
}

func pointInSimpleGround(p [2]float64, poly [][2]float64) bool {
	inside := false
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		cross := (b[0]-a[0])*(p[1]-a[1]) - (b[1]-a[1])*(p[0]-a[0])
		if math.Abs(cross) < 1e-9 && p[0] >= math.Min(a[0], b[0])-1e-9 && p[0] <= math.Max(a[0], b[0])+1e-9 && p[1] >= math.Min(a[1], b[1])-1e-9 && p[1] <= math.Max(a[1], b[1])+1e-9 {
			return true
		}
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}

func rectInSimplePolygon(poly [][2]float64, x0, y0, x1, y1 float64) bool {
	for _, p := range [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}} {
		if !pointInSimpleGround(p, poly) {
			return false
		}
	}
	// No boundary segment may pass through the open rectangle's interior.
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		lo, hi := 0., 1.
		clip := func(v, d, min, max float64) bool {
			if d == 0 {
				return v > min && v < max
			}
			t0, t1 := (min-v)/d, (max-v)/d
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			lo = math.Max(lo, t0)
			hi = math.Min(hi, t1)
			return lo < hi
		}
		if clip(a[0], b[0]-a[0], x0, x1) && clip(a[1], b[1]-a[1], y0, y1) && hi-lo > 1e-12 {
			return false
		}
	}
	return true
}

func convexGround(poly [][2]float64) bool {
	sign := 0.
	for i, a := range poly {
		b, c := poly[(i+1)%len(poly)], poly[(i+2)%len(poly)]
		v := (b[0]-a[0])*(c[1]-b[1]) - (b[1]-a[1])*(c[0]-b[0])
		if math.Abs(v) < 1e-9 {
			continue
		}
		if sign*v < 0 {
			return false
		}
		sign = v
	}
	return true
}
