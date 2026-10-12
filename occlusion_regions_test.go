package isotopo

import (
	"fmt"
	"math"
	"testing"
)

func labelProbeAt(x, y float64) *CompositePart {
	// A tiny box whose top-centre independently projects to the test point.
	return &CompositePart{ID: "probe", Shape: "rectangle", Label: "P", Geom: &Geom{W: 20, D: 20, H: 1}, Style: &Style{Text: &Text{Size: ptrFloat(1)}}, Offset: &WorldPoint{WX: y - 9 + x/math.Sqrt(3), WY: y - 9 - x/math.Sqrt(3)}}
}

func TestOcclusionUsesActualBodyRegion(t *testing.T) {
	for _, tc := range []struct {
		name, shape string
		x, y        float64
		geom        *Geom
		style       *Style
		hidden      bool
	}{
		{"sphere empty bbox corner", "circle", 90, 0, &Geom{W: 140}, nil, false},
		{"concave empty notch", "custom_path", 0, 130, &Geom{W: 200, D: 200, H: 20, Params: map[string]any{"path": "M 0 0 L 200 0 L 200 40 L 40 40 L 40 200 L 0 200 Z"}}, nil, false},
		{"rounded empty corner", "rectangle", -79, 34, &Geom{W: 200, D: 100, H: 20}, &Style{Effects: &Effects{CornerRadius: ptrFloat(50)}}, false},
		{"person head beyond box", "person", 0, -52, nil, nil, true},
		{"sphere body present", "circle", 0, 0, &Geom{W: 140}, nil, true},
		{"open roof solid side", "rectangle", 65, 45, &Geom{W: 100, D: 100, H: 100}, &Style{Palette: &Palette{Top: "none", Left: "#345678", Right: "#345678"}}, true},
		{"open roof empty roof", "rectangle", 0, -70, &Geom{W: 100, D: 100, H: 100}, &Style{Palette: &Palette{Top: "none", Left: "#345678", Right: "#345678"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := labelProbeAt(tc.x, tc.y)
			q := &CompositePart{ID: "cover", Shape: tc.shape, Geom: tc.geom, Style: tc.style, Offset: &WorldPoint{}}
			issues := labelOcclusionInFlat([]*CompositePart{probe, q}, nil, "scene")
			if got := len(issues) > 0; got != tc.hidden {
				t.Fatalf("label occluded=%v, want %v; issues=%v", got, tc.hidden, issues)
			}
		})
	}
}

func TestBodyOcclusionCircleIntersectionMatchesAnalyticArea(t *testing.T) {
	// Two projected discs, r=20 and R=40, separated by 30 screen units.
	// World footprints are disjoint, so the body occlusion metric owns the pair.
	p := &CompositePart{ID: "small", Shape: "circle", Geom: &Geom{W: 40}, Offset: &WorldPoint{}}
	q := &CompositePart{ID: "large", Shape: "circle", Geom: &Geom{W: 80}, Offset: &WorldPoint{WX: 100 + 30/math.Sqrt(3), WY: 100 - 30/math.Sqrt(3), WZ: 100}}
	occ := bodyOcclusionsInFlat([]*CompositePart{p, q}, nil, .001)
	if len(occ) != 1 {
		t.Fatalf("want disc intersection, got %v", occ)
	}
	r, R, d := 20., 40., 30.
	area := r*r*math.Acos((d*d+r*r-R*R)/(2*d*r)) + R*R*math.Acos((d*d+R*R-r*r)/(2*d*R)) - .5*math.Sqrt((-d+r+R)*(d+r-R)*(d-r+R)*(d+r+R))
	want := area / (math.Pi * r * r)
	if math.Abs(occ[0].Frac-want) > .003 {
		t.Fatalf("disc fraction %g, analytic %g", occ[0].Frac, want)
	}
}

func BenchmarkBodyOcclusionRegions(b *testing.B) {
	for _, size := range []struct {
		name string
		n    int
	}{{"50-nodes", 50}, {"200-nodes", 200}} {
		b.Run(size.name, func(b *testing.B) {
			shapes := []string{"rectangle", "circle", "cylinder", "person", "hexprism"}
			parts := make([]*CompositePart, size.n)
			for i := range parts {
				parts[i] = &CompositePart{ID: fmt.Sprintf("p%d", i), Shape: shapes[i%len(shapes)], Geom: &Geom{W: 100, D: 100, H: 60}, Offset: &WorldPoint{WX: float64(i%10) * 220, WY: float64(i/10) * 220}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				bodyOcclusionsInFlat(parts, nil, .4)
			}
		})
	}
}
