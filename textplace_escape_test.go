package isotopo

import (
	"math"
	"math/rand"
	"testing"
)

func TestPlaceTextEscapesExhaustedCandidates(t *testing.T) {
	anchor := screenRect{0, 0, 100, 80}
	obstacles := []screenRect{{-500, -500, 500, 500}}
	x, y := placeTextBox(80, 30, anchor, 10, 94, 50, 40, obstacles)
	if collides(screenRect{x, y, x + 80, y + 30}, obstacles) {
		t.Fatal("all fixed candidates exhausted; returned the known collision")
	}
	if math.Abs(y-506.02) > .000001 || x != 10 {
		t.Fatalf("expected nearest free outward position, got %v,%v", x, y)
	}
}
func TestPlaceTextEscapeIndependentGeometry(t *testing.T) {
	// Dense center forces escape. Check emitted two-decimal rectangles directly,
	// independently of the interval implementation and collides helper.
	rng := rand.New(rand.NewSource(260712))
	for i := 0; i < 600; i++ {
		anchor := screenRect{-50, -40, 50, 40}
		w, h := 10+rng.Float64()*170, 10+rng.Float64()*90
		obstacles := []screenRect{{-400, -400, 400, 400}}
		for j := 0; j < 30; j++ {
			x, y := rng.Float64()*2400-1200, rng.Float64()*2400-1200
			obstacles = append(obstacles, screenRect{x, y, x + 10 + rng.Float64()*180, y + 10 + rng.Float64()*160})
		}
		x, y := placeTextBox(w, h, anchor, -w/2, 54, 0, 0, obstacles)
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			t.Fatal("invalid escape")
		}
		rounded := func(v float64) float64 { return math.Round(v*100) / 100 }
		left, top := rounded(x), rounded(y)
		right, bottom := left+rounded(w), top+rounded(h)
		for _, o := range append(obstacles, anchor) {
			if left < o.x1+6 && right > o.x0-6 && top < o.y1+6 && bottom > o.y0-6 {
				t.Fatalf("case %d emitted label violates clearance", i)
			}
		}
		// Obstacle input order is not part of the placement semantics.
		for a, b := 0, len(obstacles)-1; a < b; a, b = a+1, b-1 {
			obstacles[a], obstacles[b] = obstacles[b], obstacles[a]
		}
		nx, ny := placeTextBox(w, h, anchor, -w/2, 54, 0, 0, obstacles)
		if x != nx || y != ny {
			t.Fatalf("case %d depends on obstacle order", i)
		}
	}
}
func TestPlaceTextEscapeUsesFirstFreeGap(t *testing.T) {
	anchor := screenRect{0, 0, 100, 80}
	// A distant obstacle below must not push the label beyond the whole scene.
	obstacles := []screenRect{{-500, -500, 500, 500}, {-200, 600, 200, 900}}
	x, y := placeTextBox(80, 30, anchor, 10, 94, 50, 40, obstacles)
	if math.Abs(y-506.02) > 1e-6 || x != 10 {
		t.Fatalf("skipped first valid gap: %v,%v", x, y)
	}
}
