package isotopo

import (
	"math"
	"testing"
)

func TestSpatialDiskErrorAndBudget(t *testing.T) {
	for _, r := range []float64{.0001, .5, 2, 5, 100} {
		p, ok := spatialDisk([2]float64{17, -23}, r)
		if !ok || len(p) < 12 || len(p) > 512 {
			t.Fatalf("radius %v: count %d ok=%v", r, len(p), ok)
		}
		for i, a := range p {
			b := p[(i+1)%len(p)]
			outer := math.Hypot(a[0]-17, a[1]+23)
			inner := math.Abs((b[0]-a[0])*(-23-a[1])-(b[1]-a[1])*(17-a[0])) / math.Hypot(b[0]-a[0], b[1]-a[1])
			if outer-r > spatialCurveError+1e-10 || inner < r-1e-10 {
				t.Fatalf("radius %v: inward %v outward %v", r, inner-r, outer-r)
			}
		}
	}
	for _, r := range []float64{1e8, 1e100, math.MaxFloat64, math.Inf(1), math.NaN()} {
		if _, ok := spatialDisk([2]float64{}, r); ok {
			t.Errorf("unbounded radius %v accepted", r)
		}
	}
}

func TestSpatialSubtractPreservesDifference(t *testing.T) {
	subject := [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	for _, cut := range [][][2]float64{
		{{2, 2}, {8, 2}, {8, 8}, {2, 8}},
		{{-1, -1}, {11, -1}, {11, 11}, {-1, 11}},
		{{10, 0}, {20, 0}, {20, 10}, {10, 10}},
		{{5, -5}, {15, 5}, {5, 15}, {-5, 5}},
	} {
		got := spatialSubtract(subject, cut)
		for i := 0; i < 43; i++ {
			for j := 0; j < 41; j++ {
				p := [2]float64{-2 + 14*(float64(i)+.371)/43, -2 + 14*(float64(j)+.613)/41}
				want := spatialOracleContains(p, [][][2]float64{subject}) && !spatialOracleContains(p, [][][2]float64{cut})
				if spatialOracleContains(p, got) != want {
					t.Fatalf("difference mismatch at %v", p)
				}
			}
		}
	}
}
