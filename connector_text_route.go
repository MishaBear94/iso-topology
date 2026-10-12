package isotopo

import "math"

// A stroke envelope is a segment swept by an axis-aligned square. It contains
// the actual round stroke, joins and caps. A quadratic is covered by chords
// whose radius includes a proven deviation, even when the work cap is reached.
type connectorTextStroke struct {
	owner  int
	a, b   [2]float64
	radius float64
}

func connectorTextStrokes(owner int, points [][2]float64, curved bool, width float64) []connectorTextStroke {
	var out []connectorTextStroke
	if curved && len(points) == 3 {
		q := quadratic{points[0], points[1], points[2]}
		// On each equal parameter interval, the difference between Q and
		// its chord is D*u*(1-u)/n², where D=P0-2P1+P2.
		d := math.Hypot(q[0][0]-2*q[1][0]+q[2][0], q[0][1]-2*q[1][1]+q[2][1])
		n := max(1, int(math.Min(256, math.Ceil(math.Sqrt(d/(4*.125))))))
		error := d / (4 * float64(n*n))
		out = make([]connectorTextStroke, 0, n)
		a := q[0]
		for i := 1; i <= n; i++ {
			b := q.at(float64(i) / float64(n))
			out = append(out, connectorTextStroke{owner, a, b, width/2 + error})
			a = b
		}
		return out
	}
	for i := 1; i < len(points); i++ {
		out = append(out, connectorTextStroke{owner, points[i-1], points[i], width / 2})
	}
	return out
}

// Centre positions forbidden by a label of width w / height h are the convex
// hull of the endpoint rectangles. Unlike a segment AABB this preserves the
// free triangular space on either side of a diagonal route.
// Eight input corners have a hull of at most six vertices. Keep the tiny sort
// and hull scratch on the stack; callers can pack all obstacle polygons into
// one backing buffer instead of allocating several slices per stroke.
func (s connectorTextStroke) appendForbidden(dst [][2]float64, w, h float64) [][2]float64 {
	x, y := w/2+s.radius+6.03, h/2+s.radius+6.03
	var pts [8][2]float64
	for i, p := range [][2]float64{s.a, s.b} {
		pts[4*i], pts[4*i+1] = [2]float64{p[0] - x, p[1] - y}, [2]float64{p[0] - x, p[1] + y}
		pts[4*i+2], pts[4*i+3] = [2]float64{p[0] + x, p[1] - y}, [2]float64{p[0] + x, p[1] + y}
	}
	less := func(a, b [2]float64) bool { return a[0] < b[0] || a[0] == b[0] && a[1] < b[1] }
	for i := 1; i < len(pts); i++ {
		p, j := pts[i], i
		for j > 0 && less(p, pts[j-1]) {
			pts[j] = pts[j-1]
			j--
		}
		pts[j] = p
	}
	cross := func(a, b, c [2]float64) float64 { return (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0]) }
	var storage [16][2]float64
	hull := storage[:0]
	for _, p := range pts {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull)
	for i := len(pts) - 2; i >= 0; i-- {
		p := pts[i]
		for len(hull) > lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	return append(dst, hull[:len(hull)-1]...)
}

// Test the final serialized box against the original swept segment, rather
// than accepting solely on the root solver's numerical classification.
func connectorTextStrokeHits(box screenRect, strokes []connectorTextStroke) bool {
	for _, s := range strokes {
		r := box.inflate(s.radius + 6)
		lo, hi := 0.0, 1.0
		for axis, bounds := range [][2]float64{{r.x0, r.x1}, {r.y0, r.y1}} {
			delta := s.b[axis] - s.a[axis]
			if delta == 0 {
				if s.a[axis] <= bounds[0] || s.a[axis] >= bounds[1] {
					hi = lo
					break
				}
				continue
			}
			u, v := (bounds[0]-s.a[axis])/delta, (bounds[1]-s.a[axis])/delta
			if u > v {
				u, v = v, u
			}
			lo, hi = math.Max(lo, u), math.Min(hi, v)
		}
		if hi > lo {
			return true
		}
	}
	return false
}
