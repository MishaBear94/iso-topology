package isotopo

import "math"

// Bound the submitted path, including the full round stroke. Quadratic control
// points are not on the curve; use its analytic extrema so a large control hull
// does not needlessly shrink the diagram when fit to the viewport. The one-unit
// guard also absorbs viewBox decimal rounding. This is SVG-user-space geometry,
// not a promise about the antialias footprint at arbitrary export scales.
func connectorStrokeBounds(points [][2]float64, curved bool, width float64) minSvgRect {
	b := minSvgRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	add := func(p [2]float64) {
		b.minX, b.minY = math.Min(b.minX, p[0]), math.Min(b.minY, p[1])
		b.maxX, b.maxY = math.Max(b.maxX, p[0]), math.Max(b.maxY, p[1])
	}
	if curved && len(points) == 3 {
		q := quadratic{points[0], points[1], points[2]}
		add(q[0])
		add(q[2])
		for k := 0; k < 2; k++ {
			den := (q[0][k] - q[1][k]) + (q[2][k] - q[1][k])
			if den != 0 {
				t := (q[0][k] - q[1][k]) / den
				if t > 0 && t < 1 {
					add(q.at(t))
				}
			}
		}
	} else {
		for _, p := range points {
			add(p)
		}
	}
	radius := width/2 + 1
	b.minX, b.minY, b.maxX, b.maxY = b.minX-radius, b.minY-radius, b.maxX+radius, b.maxY+radius
	return b
}
