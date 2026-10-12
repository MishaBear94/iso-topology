package iso25d

import "testing"

func TestConcaveRectangleRejectsNotchWithInteriorCorners(t *testing.T) {
	// All four corners are inside the C, but two edges cross its empty notch.
	p := [][2]float64{{0, 0}, {100, 0}, {100, 20}, {20, 20}, {20, 80}, {100, 80}, {100, 100}, {0, 100}}
	if rectInSimplePolygon(p, 10, 10, 90, 90) {
		t.Fatal("corner-only containment swallowed a notch")
	}
	if !rectInSimplePolygon(p, 0, 0, 100, 20) {
		t.Fatal("boundary-coincident rectangle should be allowed")
	}
	r := concaveContentRect(p, 100, 100)
	if !rectInSimplePolygon(p, r.X, r.Y, r.X+r.W, r.Y+r.H) || r.W*r.H < 1999 {
		t.Fatalf("expected verified interior arm, got %+v", r)
	}
}

func TestConcaveRectangleThinArmsAndWinding(t *testing.T) {
	for _, width := range []float64{.01, 1, 5, 20} {
		p := [][2]float64{{0, 0}, {100, 0}, {100, width}, {width, width}, {width, 100}, {0, 100}}
		for pass := 0; pass < 2; pass++ {
			r := concaveContentRect(p, 100, 100)
			if !rectInSimplePolygon(p, r.X, r.Y, r.X+r.W, r.Y+r.H) || r.W*r.H < 99.9*width {
				t.Fatalf("thin arm lost (width %g, winding %d): %+v", width, pass, r)
			}
			for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
				p[i], p[j] = p[j], p[i]
			}
		}
	}
}
