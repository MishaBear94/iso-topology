package isotopo

import (
	"math"
	"math/rand"
	"testing"
)

func regionRect(x, y, w, h float64) [][2]float64 {
	return [][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
}

func TestRegionUnionArea(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    screenRegion
		want float64
	}{
		{"concave L", screenRegion{{{0, 0}, {10, 0}, {10, 2}, {2, 2}, {2, 10}, {0, 10}}}, 36},
		{"overlapping faces", screenRegion{regionRect(0, 0, 10, 10), regionRect(5, 5, 10, 10)}, 175},
		{"hole remains empty", screenRegion{regionRect(0, 0, 10, 2), regionRect(0, 8, 10, 2), regionRect(0, 2, 2, 6), regionRect(8, 2, 2, 6)}, 64},
		{"duplicate faces", screenRegion{regionRect(0, 0, 10, 10), regionRect(0, 0, 10, 10)}, 100},
		{"edge crossing inside slab", screenRegion{{{0, 0}, {10, 0}, {5, 10}}, {{0, 10}, {10, 10}, {5, 0}}}, 75},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := regionArea(tc.r); math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("area %g want %g", got, tc.want)
			}
		})
	}
}

func TestRegionIntersectionAgainstIndependentCellOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(517))
	for trial := 0; trial < 100; trial++ {
		var regions [2]screenRegion
		var occupied [2][20][20]bool
		for k := range regions {
			for n := 0; n < 8; n++ {
				x, y := rng.Intn(16), rng.Intn(16)
				w, h := 1+rng.Intn(5), 1+rng.Intn(5)
				regions[k] = append(regions[k], regionRect(float64(x), float64(y), float64(w), float64(h)))
				for i := x; i < x+w; i++ {
					for j := y; j < y+h; j++ {
						occupied[k][i][j] = true
					}
				}
			}
		}
		area, intersection := 0., 0.
		for i := 0; i < 20; i++ {
			for j := 0; j < 20; j++ {
				if occupied[0][i][j] {
					area++
				}
				if occupied[0][i][j] && occupied[1][i][j] {
					intersection++
				}
			}
		}
		// A nonsingular affine map gives slanted concave regions and crossing edges
		// without changing the independent occupancy oracle. determinant = 1.25.
		for _, r := range regions {
			for _, p := range r {
				for i, v := range p {
					p[i] = [2]float64{v[0] + .5*v[1] + 17, -.5*v[0] + v[1] - 23}
				}
			}
		}
		if got := regionArea(regions[0]); math.Abs(got-1.25*area) > 1e-7 {
			t.Fatalf("trial %d union=%g want=%g", trial, got, 1.25*area)
		}
		for _, pair := range [][2]int{{0, 1}, {1, 0}} {
			if got := regionIntersectionArea(regions[pair[0]], regions[pair[1]]); math.Abs(got-1.25*intersection) > 1e-7 {
				t.Fatalf("trial %d intersection=%g want=%g", trial, got, 1.25*intersection)
			}
		}
	}
}
