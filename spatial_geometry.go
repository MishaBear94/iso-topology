package isotopo

import (
	"math"
)

// A depthPlane gives x+y+z along the fixed (1,1,1) viewing ray as an affine
// function of projected screen coordinates. Larger values are nearer.
type depthPlane [3]float64

func (p depthPlane) at(q [2]float64) float64 { return p[0]*q[0] + p[1]*q[1] + p[2] }
func planeThrough(a, b, c [2]float64, da, db, dc float64) (depthPlane, bool) {
	dx, dy, ex, ey := b[0]-a[0], b[1]-a[1], c[0]-a[0], c[1]-a[1]
	den := dx*ey - dy*ex
	if math.Abs(den) < 1e-10 {
		return depthPlane{}, false
	}
	x, y := ((db-da)*ey-(dc-da)*dy)/den, (dx*(dc-da)-ex*(db-da))/den
	return depthPlane{x, y, da - x*a[0] - y*a[1]}, true
}
func polygonSignedArea(p [][2]float64) float64 {
	if len(p) < 3 {
		return 0
	}
	// Translate before taking cross products: world offsets must not turn
	// zero-width clipped edges into large phantom areas by cancellation.
	origin := p[0]
	area := 0.
	for i := 1; i+1 < len(p); i++ {
		a, b := p[i], p[i+1]
		area += (a[0]-origin[0])*(b[1]-origin[1]) - (a[1]-origin[1])*(b[0]-origin[0])
	}
	return area / 2
}
func spatialCCW(p [][2]float64) [][2]float64 {
	q := append([][2]float64(nil), p...)
	if polygonSignedArea(q) < 0 {
		for i, j := 0, len(q)-1; i < j; i, j = i+1, j-1 {
			q[i], q[j] = q[j], q[i]
		}
	}
	return q
}

// Clip a convex polygon to a*x+b*y+c >= 0, without widening its boundary.
func spatialHalfPlane(p [][2]float64, h depthPlane) [][2]float64 {
	if len(p) < 3 {
		return nil
	}
	return spatialHalfPlaneInto(p, h, make([][2]float64, 0, len(p)+1))
}

// The scratch buffer must not alias p. A convex cut adds at most one vertex;
// append still handles inputs with more crossings without truncating them.
func spatialHalfPlaneInto(p [][2]float64, h depthPlane, out [][2]float64) [][2]float64 {
	if len(p) < 3 {
		return nil
	}
	out = out[:0]
	a := p[len(p)-1]
	fa := h.at(a)
	for _, b := range p {
		fb := h.at(b)
		if (fa >= 0) != (fb >= 0) {
			t := fa / (fa - fb)
			out = append(out, [2]float64{a[0] + t*(b[0]-a[0]), a[1] + t*(b[1]-a[1])})
		}
		if fb >= 0 {
			out = append(out, b)
		}
		a, fa = b, fb
	}
	if len(out) < 3 || math.Abs(polygonSignedArea(out)) < 1e-12 {
		return nil
	}
	return out
}

// Convex subtraction yields disjoint convex pieces. No raster geometry, hull
// replacement, or accumulated area is used as the visible-region definition.
func spatialSubtract(subject, cut [][2]float64) [][][2]float64 {
	if len(subject) < 3 {
		return nil
	}
	if len(cut) < 3 {
		return [][][2]float64{subject}
	}
	remain := subject
	var out [][][2]float64
	for i, a := range cut {
		b := cut[(i+1)%len(cut)]
		h := depthPlane{a[1] - b[1], b[0] - a[0], a[0]*b[1] - a[1]*b[0]}
		if p := spatialHalfPlane(remain, depthPlane{-h[0], -h[1], -h[2]}); len(p) > 0 {
			out = append(out, p)
		}
		remain = spatialHalfPlane(remain, h)
		if len(remain) == 0 {
			return out
		}
	}
	return out
}
func spatialBoundsOverlap(a, b [][2]float64) bool {
	x, y := regionBounds(screenRegion{a}), regionBounds(screenRegion{b})
	return x.x0 <= y.x1 && x.x1 >= y.x0 && x.y0 <= y.y1 && x.y1 >= y.y0
}

const spatialCurveError = .002

// Circumscribed circles never trim an unobstructed round join. Their maximum
// outward error is bounded; the final SVG still draws the original path once.
func spatialDisk(p [2]float64, r float64) ([][2]float64, bool) {
	if math.IsNaN(r) || math.IsInf(r, 0) || math.IsNaN(p[0]+p[1]) || math.IsInf(p[0]+p[1], 0) {
		return nil, false
	}
	if r <= 0 {
		return nil, true
	}
	// Check before conversion: very large radii round the ratio to 1 and
	// produce +Inf. Float-to-int overflow is not a reliable budget check;
	// NaN/Inf previously could select twelve sides and bypass the bound.
	sides := math.Ceil(math.Pi / math.Acos(r/(r+spatialCurveError)))
	if math.IsNaN(sides) || sides > 512 {
		return nil, false
	}
	n := max(12, int(sides))
	out := make([][2]float64, n)
	rr := r / math.Cos(math.Pi/float64(n))
	for i := range out {
		a := 2 * math.Pi * float64(i) / float64(n)
		out[i] = [2]float64{p[0] + rr*math.Cos(a), p[1] + rr*math.Sin(a)}
	}
	return out, true
}

type spatialPiece struct {
	poly  [][2]float64
	depth depthPlane
	owner string
}

func spatialRibbon(a, b [2]float64, da, db, r float64) ([]spatialPiece, bool) {
	dx, dy := b[0]-a[0], b[1]-a[1]
	length := math.Hypot(dx, dy)
	if length < 1e-9 {
		return nil, true
	}
	nx, ny := -dy/length*r, dx/length*r
	z := depthPlane{(db - da) * dx / (length * length), (db - da) * dy / (length * length), 0}
	z[2] = da - z[0]*a[0] - z[1]*a[1]
	out := []spatialPiece{{spatialCCW([][2]float64{{a[0] + nx, a[1] + ny}, {a[0] - nx, a[1] - ny}, {b[0] - nx, b[1] - ny}, {b[0] + nx, b[1] + ny}}), z, ""}}
	for i, end := range []struct {
		p [2]float64
		d float64
	}{{a, da}, {b, db}} {
		poly, ok := spatialDisk(end.p, r)
		if !ok {
			return nil, false
		}
		// A round cap occupies only the outside half-disk. A full disk at
		// constant endpoint depth overlaps the sloping line interior and can
		// incorrectly reveal that interior through a nearer surface. This
		// partitions the capsule by the closest centreline point, continuously
		// matching the rectangle's depth on each cap diameter.
		ux, uy := dx/length, dy/length
		if i == 0 {
			ux, uy = -ux, -uy
		}
		poly = spatialHalfPlane(poly, depthPlane{ux, uy, -ux*end.p[0] - uy*end.p[1]})
		out = append(out, spatialPiece{poly, depthPlane{0, 0, end.d}, ""})
	}
	return out, true
}
