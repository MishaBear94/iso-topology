package isotopo

import (
	"math"
	"math/rand/v2"
	"testing"
)

// Frozen allocation-heavy clipper: buffer reuse must preserve coordinates,
// ordering, degeneracy and work-budget decisions, not just a similar area.
func referenceHalfPlane(p [][2]float64, h depthPlane) [][2]float64 {
	if len(p) < 3 {
		return nil
	}
	var out [][2]float64
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

func referencePainterIntersection(a, b [][2]float64, work *int) ([][2]float64, bool) {
	q := a
	for i, p := range b {
		*work -= len(q) + 1
		if *work < 0 {
			return nil, false
		}
		n := b[(i+1)%len(b)]
		q = referenceHalfPlane(q, depthPlane{p[1] - n[1], n[0] - p[0], p[0]*n[1] - p[1]*n[0]})
		if len(q) == 0 {
			break
		}
	}
	return q, true
}

func clipCoordinatesEqual(a, b [][2]float64) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for i := range a {
		for j := range a[i] {
			if math.Float64bits(a[i][j]) != math.Float64bits(b[i][j]) {
				return false
			}
		}
	}
	return true
}

func TestSpatialClipParityAndOwnership(t *testing.T) {
	polys := [][][2]float64{
		nil, {}, {{0, 0}}, {{0, 0}, {1, 1}},
		{{0, 0}, {1, 0}, {2, 0}},
		{{0, 0}, {1e-8, 0}, {0, 1e-8}},
		{{-10, -10}, {10, -10}, {10, 10}, {-10, 10}},
		{{0, 0}, {0, 0}, {10, 0}, {10, 10}, {0, 10}},
		{{0, math.Copysign(0, -1)}, {10, 0}, {0, 10}},
		{{math.NaN(), 0}, {10, 0}, {0, 10}},
		{{math.Inf(1), 0}, {10, 0}, {0, 10}},
	}
	rng := rand.New(rand.NewPCG(20261012, 81427))
	for trial := 0; trial < 300; trial++ {
		n := 3 + rng.IntN(30)
		x, y := 40*rng.Float64()-20, 40*rng.Float64()-20
		if trial%3 == 0 {
			x, y = x+1e6, y-1e6
		}
		rx, ry := 1+30*rng.Float64(), 1+30*rng.Float64()
		poly := make([][2]float64, n)
		for i := range poly {
			a := 2 * math.Pi * float64(i) / float64(n)
			poly[i] = [2]float64{x + rx*math.Cos(a), y + ry*math.Sin(a)}
		}
		polys = append(polys, poly)
	}
	for i, a := range polys {
		// Include containment, rejection, partial cuts, boundary cuts and
		// non-finite coefficients. Source polygons must never be overwritten.
		for _, h := range []depthPlane{{0, 0, 1}, {0, 0, -1}, {0, 0, 0}, {1, 0, 0}, {0, -1, 5}, {1, 2, -3}, {1, 1, math.NaN()}} {
			got, want := spatialHalfPlane(a, h), referenceHalfPlane(a, h)
			if !clipCoordinatesEqual(got, want) {
				t.Fatalf("half-plane %d %v: got %v want %v", i, h, got, want)
			}
		}
		for _, b := range [][][2]float64{nil, polys[6], a, polys[(i+1)%len(polys)], polys[(i*13+7)%len(polys)]} {
			for _, budget := range []int{-1, 0, 1, 10, 30, 10000} {
				gw, ww := budget, budget
				got, gok := painterIntersection(a, b, &gw)
				want, wok := referencePainterIntersection(a, b, &ww)
				if gok != wok || gw != ww || !clipCoordinatesEqual(got, want) {
					t.Fatalf("intersection %d budget %d: coordinates=%v ok=%v/%v remaining=%d/%d", i, budget, clipCoordinatesEqual(got, want), gok, wok, gw, ww)
				}
				if len(got) > 0 && len(b) > 0 {
					ac, bc := append([][2]float64{}, a...), append([][2]float64{}, b...)
					got[0] = [2]float64{1234567, 7654321}
					if !clipCoordinatesEqual(a, ac) || !clipCoordinatesEqual(b, bc) {
						t.Fatal("returned intersection aliases an input polygon")
					}
				}
			}
		}
	}
}

func BenchmarkPainterIntersection(b *testing.B) {
	subject := [][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}
	for _, tc := range []struct {
		name string
		cut  [][2]float64
	}{
		{"contained", [][2]float64{{-10, -10}, {30, -10}, {30, 30}, {-10, 30}}},
		{"partial", [][2]float64{{10, -5}, {25, 10}, {10, 25}, {-5, 10}}},
		{"disjoint", [][2]float64{{30, 30}, {40, 30}, {40, 40}, {30, 40}}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				work := 1000
				_, ok := painterIntersection(subject, tc.cut, &work)
				if !ok {
					b.Fatal("unexpected budget exhaustion")
				}
			}
		})
	}
}
