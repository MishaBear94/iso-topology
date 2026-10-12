package isotopo

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
)

func TestConnectorTextDiagonalEnvelopeReleasesEmptyCorners(t *testing.T) {
	path := [][2]float64{{0, 100}, {100, 100}}
	other := [][2]float64{{0, 0}, {100, 100}}
	old := connectorTextStrokeObstacles(1, other, false, 2)
	if _, _, clear := connectorTextPosition(path, false, 20, 20, []screenRect{old[0].rect}); clear {
		t.Fatal("fixture must expose false AABB obstruction")
	}
	x, y, clear := connectorTextPosition(path, false, 20, 20, nil, connectorTextStrokes(1, other, false, 2)...)
	if !clear || x != 50 || y != 100 {
		t.Fatalf("empty triangle not released: %g %g %v", x, y, clear)
	}
	// Retain thick caps: the endpoint is not just a zero-width centreline.
	if !connectorTextStrokeHits(screenRect{102, 98, 108, 102}, connectorTextStrokes(1, other, false, 16)) {
		t.Fatal("round cap lost")
	}
}

func TestConnectorTextRetainsExactCurveBoundsCertificate(t *testing.T) {
	// At the subdivision cap the curve's error square extends beyond its
	// y=250000 extremum and the old AABB's extra one-unit export guard.
	points := [][2]float64{{0, 0}, {100, 500000}, {200, 0}}
	strokes := connectorTextStrokes(1, points, true, 2)
	old := connectorTextStrokeObstacles(1, points, true, 2)
	req := connectorTextRequest{index: 0, points: [][2]float64{{-10, 250018.04}, {210, 250018.04}}, block: screenTextBlock{width: 20, height: 20}}
	x, y, clear := connectorTextPosition(req.points, false, 20, 20, []screenRect{old[0].rect})
	if !clear || !connectorTextStrokeHits(connectorTextBox(req, x, y), strokes) {
		t.Fatal("fixture must expose the conservative chord-error fringe")
	}
	got := resolveConnectorText([]connectorTextRequest{req}, nil, nil, old, strokes)[0]
	if !got.clear || got.x != x || got.y != y {
		t.Fatalf("valid exact-curve certificate was lost: %+v", got)
	}
}

func BenchmarkConnectorTextStrokeSearch(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			var strokes []connectorTextStroke
			for i := 0; i < count; i++ {
				x := float64(i) * 4
				strokes = append(strokes, connectorTextStrokes(i, [][2]float64{{x, -100}, {x + 150, 100}}, false, 3)...)
			}
			path := [][2]float64{{-200, 0}, {float64(count)*4 + 200, 0}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				connectorTextPosition(path, false, 40, 20, nil, strokes...)
			}
		})
	}
}

func TestConnectorTextQuadraticEnvelopeContainsActualCurve(t *testing.T) {
	cases := [][][2]float64{
		{{0, 0}, {100, 300}, {300, 0}}, {{0, 0}, {0, 500}, {0, 0}},
		{{-1e6, 0}, {0, 5e6}, {1e6, 0}}, {{1e8, 1e8}, {1e8 + 10, 1e8 + 200}, {1e8 + 300, 1e8}},
		{{0, 0}, {.000001, .000002}, {.000003, 0}}, {{0, 0}, {100, 0}, {300, 0}},
	}
	for _, points := range cases {
		segments := connectorTextStrokes(3, points, true, 8)
		if len(segments) > 256 {
			t.Fatal("curve budget unbounded")
		}
		for i, s := range segments {
			for j := 0; j <= 31; j++ {
				u := (float64(i) + float64(j)/31) / float64(len(segments))
				p := [2]float64{(1-u)*(1-u)*points[0][0] + 2*u*(1-u)*points[1][0] + u*u*points[2][0], (1-u)*(1-u)*points[0][1] + 2*u*(1-u)*points[1][1] + u*u*points[2][1]}
				dx, dy := s.b[0]-s.a[0], s.b[1]-s.a[1]
				v := 0.0
				if den := dx*dx + dy*dy; den > 0 {
					v = math.Max(0, math.Min(1, ((p[0]-s.a[0])*dx+(p[1]-s.a[1])*dy)/den))
				}
				if math.Hypot(p[0]-s.a[0]-v*dx, p[1]-s.a[1]-v*dy) > s.radius-4+1e-6 {
					t.Fatal("curve escapes stroke envelope at cap")
				}
			}
		}
	}
}

func TestConnectorTextStrokeIndependentOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(1307, 89))
	for n := 0; n < 400; n++ {
		p := [][2]float64{{-200, -60 + rng.Float64()*120}, {200, -60 + rng.Float64()*120}}
		curved := n%2 == 0
		if curved {
			p = append(p[:1], [2]float64{0, 100}, p[1])
		}
		other := [][2]float64{{-150 + rng.Float64()*200, -150}, {150 - rng.Float64()*200, 150}}
		otherCurved := n%3 == 0
		if otherCurved {
			other = append(other[:1], [2]float64{200, 0}, other[1])
		}
		width := 1 + rng.Float64()*24
		w, h := 20+rng.Float64()*80, 12+rng.Float64()*40
		x, y, clear := connectorTextPosition(p, curved, w, h, nil, connectorTextStrokes(1, other, otherCurved, width)...)
		if !clear {
			continue
		}
		x0, y0 := math.Round((x-w/2)*100)/100, math.Round((y-h/2)*100)/100
		x1, y1 := x0+math.Round(w*100)/100, y0+math.Round(h*100)/100
		// Independent Bernstein interpolation plus square-width occupancy.
		for i := 0; i <= 10000; i++ {
			u := float64(i) / 10000
			px, py := (1-u)*other[0][0]+u*other[len(other)-1][0], (1-u)*other[0][1]+u*other[len(other)-1][1]
			if otherCurved {
				px = (1-u)*(1-u)*other[0][0] + 2*u*(1-u)*other[1][0] + u*u*other[2][0]
				py = (1-u)*(1-u)*other[0][1] + 2*u*(1-u)*other[1][1] + u*u*other[2][1]
			}
			pad := width/2 + 6 - .001
			if px > x0-pad && px < x1+pad && py > y0-pad && py < y1+pad {
				t.Fatalf("unsafe clear claim case %d at %g", n, u)
			}
		}
	}
}

func TestConnectorTextRefinementProtectsWholeBaseline(t *testing.T) {
	rng := rand.New(rand.NewPCG(41, 7919))
	improved := 0
	for n := 0; n < 250; n++ {
		var requests []connectorTextRequest
		var obs []connectorTextObstacle
		var strokes []connectorTextStroke
		for i := 0; i < 6; i++ {
			points := [][2]float64{{rng.Float64() * 300, rng.Float64() * 300}, {rng.Float64() * 300, rng.Float64() * 300}}
			requests = append(requests, connectorTextRequest{index: i, points: points, block: screenTextBlock{width: 20 + rng.Float64()*30, height: 12 + rng.Float64()*10}})
			obs = append(obs, connectorTextStrokeObstacles(i, points, false, 2)...)
			strokes = append(strokes, connectorTextStrokes(i, points, false, 2)...)
		}
		// Freeze the pre-refinement AABB layout independently of the resolver.
		old := make([]connectorTextPlacement, len(requests))
		var placed []screenRect
		for i, req := range requests {
			all := append([]screenRect(nil), placed...)
			for _, o := range obs {
				if o.owner != i {
					all = append(all, o.rect)
				}
			}
			x, y, clear := connectorTextPosition(req.points, false, req.block.width, req.block.height, all)
			old[i] = connectorTextPlacement{x, y, clear, connectorTextBox(req, x, y)}
			placed = append(placed, old[i].box)
		}
		overlaps := func(a, b screenRect) bool { return a.x0 < b.x1+6 && a.x1 > b.x0-6 && a.y0 < b.y1+6 && a.y1 > b.y0-6 }
		for i := range old {
			for j := range old {
				if i != j && overlaps(old[i].box, old[j].box) {
					old[i].clear = false
				}
			}
		}
		next := resolveConnectorText(requests, nil, nil, obs, strokes)
		for i, p := range next {
			if old[i].clear && (p.x != old[i].x || p.y != old[i].y || !p.clear) {
				t.Fatal("clean baseline label moved or became blocked")
			}
			if !old[i].clear && p.clear {
				improved++
			}
			for j := 0; j < i; j++ {
				if overlaps(p.box, next[j].box) && !overlaps(old[i].box, old[j].box) {
					t.Fatal("refinement introduced a new pill pair")
				}
			}
		}
	}
	if improved == 0 {
		t.Fatal("fixture never exercises refinement")
	}
	t.Logf("improved %d labels in 250 layouts", improved)
}
