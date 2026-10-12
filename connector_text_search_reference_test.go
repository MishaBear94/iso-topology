package isotopo

import (
	"math"
	"slices"
	"sort"
)

// Frozen pre-index search: independent differential reference for candidate
// identity and tie ordering. Do not update this when optimizing the search.
func referenceConnectorTextPosition(points [][2]float64, curved bool, w, h float64, obstacles []screenRect, strokes ...connectorTextStroke) (float64, float64, bool) {
	if len(points) < 2 {
		return 0, 0, false
	}
	box := func(x, y float64) screenRect {
		rx, ry := svgCoord(x-w/2), svgCoord(y-h/2)
		return screenRect{rx, ry, rx + svgCoord(w), ry + svgCoord(h)}
	}
	var forbidden [][][2]float64
	for _, stroke := range strokes {
		forbidden = append(forbidden, referenceConnectorTextForbidden(stroke, w, h))
	}
	total := 1.0
	if !curved {
		total = planLen(points)
	}
	best := math.Inf(1)
	bx, by := points[0][0], points[0][1]
	if curved {
		bx = .25*points[0][0] + .5*points[1][0] + .25*points[2][0]
		by = .25*points[0][1] + .5*points[1][1] + .25*points[2][1]
	} else {
		bx, by = planMid(points)
	}
	search := func(ax, bx0, cx, ay, by0, cy, walked, length float64) {
		at := func(t float64) (float64, float64) { return (ax*t+bx0)*t + cx, (ay*t+by0)*t + cy }
		cuts := []float64{0, 1}
		root := func(a, b, c float64) {
			add := func(t float64) {
				if t > 0 && t < 1 {
					cuts = append(cuts, t)
				}
			}
			if math.Abs(a) < 1e-12 {
				if math.Abs(b) > 1e-12 {
					add(-c / b)
				}
				return
			}
			d := b*b - 4*a*c
			if d < 0 {
				return
			}
			q := -.5 * (b + math.Copysign(math.Sqrt(d), b))
			if q == 0 {
				add(-b / (2 * a))
			} else {
				add(q / a)
				add(c / q)
			}
		}
		for _, o := range obstacles {
			root(ax, bx0, cx-(o.x0-w/2-6.03))
			root(ax, bx0, cx-(o.x1+w/2+6.03))
			root(ay, by0, cy-(o.y0-h/2-6.03))
			root(ay, by0, cy-(o.y1+h/2+6.03))
		}
		for _, polygon := range forbidden {
			for i, a := range polygon {
				b := polygon[(i+1)%len(polygon)]
				dx, dy := b[0]-a[0], b[1]-a[1]
				root(-dy*ax+dx*ay, -dy*bx0+dx*by0, -dy*(cx-a[0])+dx*(cy-a[1]))
			}
		}
		slices.Sort(cuts)
		cuts = slices.Compact(cuts)
		preferred := .5
		if length > 0 {
			preferred = (total/2 - walked) / length
		}
		for i := 1; i < len(cuts); i++ {
			t := math.Max(cuts[i-1], math.Min(cuts[i], preferred))
			x, y := at(t)
			if collides(box(x, y), obstacles) || connectorTextStrokeHits(box(x, y), strokes) {
				continue
			}
			distance := math.Abs(walked + length*t - total/2)
			if distance < best {
				best, bx, by = distance, x, y
			}
		}
	}
	if curved && len(points) == 3 {
		p, q, r := points[0], points[1], points[2]
		search(p[0]-2*q[0]+r[0], 2*(q[0]-p[0]), p[0], p[1]-2*q[1]+r[1], 2*(q[1]-p[1]), p[1], 0, 1)
	} else {
		walked := 0.0
		for i := 1; i < len(points); i++ {
			p, q := points[i-1], points[i]
			length := math.Hypot(q[0]-p[0], q[1]-p[1])
			search(0, q[0]-p[0], p[0], 0, q[1]-p[1], p[1], walked, length)
			walked += length
		}
	}
	return bx, by, !math.IsInf(best, 1)
}

func referenceConnectorTextForbidden(s connectorTextStroke, w, h float64) [][2]float64 {
	x, y := w/2+s.radius+6.03, h/2+s.radius+6.03
	pts := make([][2]float64, 0, 8)
	for _, p := range [][2]float64{s.a, s.b} {
		pts = append(pts, [2]float64{p[0] - x, p[1] - y}, [2]float64{p[0] - x, p[1] + y}, [2]float64{p[0] + x, p[1] - y}, [2]float64{p[0] + x, p[1] + y})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] || pts[i][0] == pts[j][0] && pts[i][1] < pts[j][1] })
	cross := func(a, b, c [2]float64) float64 { return (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0]) }
	hull := make([][2]float64, 0, 12)
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
	return hull[:len(hull)-1]
}
