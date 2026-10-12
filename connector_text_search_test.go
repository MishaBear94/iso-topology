package isotopo

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestConnectorTextSearchMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(4139, 20261012))
	clear, blocked := 0, 0
	for n := 0; n < 1600; n++ {
		offset := 0.0
		if n%5 == 0 {
			offset = 1e8
		}
		scale := []float64{.01, 1, 10, 100}[n%4]
		point := func() [2]float64 {
			return [2]float64{offset + (rng.Float64()*400-200)*scale, offset + (rng.Float64()*400-200)*scale}
		}
		curved := n%3 == 0
		points := [][2]float64{point(), point()}
		if curved || n%3 == 1 {
			points = append(points, point())
		}
		if n%17 == 0 {
			points[1] = points[0]
		}
		w, h := 5+rng.Float64()*70, 5+rng.Float64()*30
		var obstacles []screenRect
		for i := 0; i < n%19; i++ {
			p := point()
			obstacles = append(obstacles, screenRect{p[0], p[1], p[0] + 5 + rng.Float64()*80*scale, p[1] + 5 + rng.Float64()*80*scale})
		}
		var strokes []connectorTextStroke
		for i := 0; i < n%5; i++ {
			p := [][2]float64{point(), point()}
			curve := i%2 == 1
			if curve {
				p = append(p, point())
			}
			strokes = append(strokes, connectorTextStrokes(i, p, curve, rng.Float64()*20)...)
		}
		x, y, ok := connectorTextPosition(points, curved, w, h, obstacles, strokes...)
		rx, ry, rok := referenceConnectorTextPosition(points, curved, w, h, obstacles, strokes...)
		if x != rx || y != ry || ok != rok {
			t.Fatalf("case %d changed accepted position: got (%g,%g,%v), reference (%g,%g,%v)", n, x, y, ok, rx, ry, rok)
		}
		if ok {
			clear++
		} else {
			blocked++
		}
	}
	if clear < 300 || blocked < 100 {
		t.Fatalf("weak corpus: clear=%d blocked=%d", clear, blocked)
	}
	t.Logf("exact reference parity: %d clear, %d blocked", clear, blocked)
}

func TestConnectorTextSearchRoundingAndTies(t *testing.T) {
	for _, offset := range []float64{0, -1e8, 1e8} {
		for _, width := range []float64{20, 20.0049, 20.005, 20.0051} {
			for _, halfGap := range []float64{15.99, 16, 16.005, 16.03, 16.04} {
				points := [][2]float64{{offset - 100, 0}, {offset, 0}, {offset + 100, 0}}
				obstacles := []screenRect{{offset - 120, -50, offset - halfGap, 50}, {offset + halfGap, -50, offset + 120, 50}}
				x, y, ok := connectorTextPosition(points, false, width, 20, obstacles)
				rx, ry, rok := referenceConnectorTextPosition(points, false, width, 20, obstacles)
				if x != rx || y != ry || ok != rok {
					t.Fatalf("rounding/tie changed: offset=%g width=%g gap=%g", offset, width, halfGap)
				}
			}
		}
	}
}

func TestConnectorTextSpanIndexMatchesUnion(t *testing.T) {
	rng := rand.New(rand.NewPCG(813, 99))
	for n := 0; n < 400; n++ {
		var spans []connectorTextBlockedSpan
		for i := 0; i < n%40; i++ {
			a, b := rng.Float64(), rng.Float64()
			spans = append(spans, connectorTextBlockedSpan{min(a, b), max(a, b), i})
		}
		original := slices.Clone(spans)
		indexConnectorTextSpans(spans)
		points := []float64{0, 1}
		for _, s := range original {
			points = append(points, s.lo, s.hi, (s.lo+s.hi)/2)
		}
		for i := 0; i < 100; i++ {
			points = append(points, rng.Float64())
		}
		for _, t0 := range points {
			want := false
			for _, s := range original {
				want = want || s.lo <= t0 && t0 <= s.hi
			}
			witness, got := connectorTextSpanWitness(spans, t0)
			if got != want {
				t.Fatal("prefix index changed interval union")
			}
			if got && (original[witness].lo > t0 || original[witness].hi < t0) {
				t.Fatal("witness does not cover query")
			}
		}
	}
}

func TestConnectorTextCurveReentersObstacle(t *testing.T) {
	poly := [][2]float64{{-3, -.5}, {3, -.5}, {3, .5}, {-3, .5}}
	spans := connectorTextBlockedIntervals(nil, poly, 0, 0, 4, -2, 8, -8, 1)
	if len(spans) != 2 || spans[0].hi >= .5 || spans[1].lo <= .5 {
		t.Fatalf("quadratic's free middle interval disappeared: %+v", spans)
	}
}

func TestConnectorTextPackedHullsMatchReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(2026, 1012))
	buffer := make([][2]float64, 0, 8000)
	var polygons [][][2]float64
	for n := 0; n < 1000; n++ {
		s := connectorTextStroke{a: [2]float64{rng.Float64() * 500, rng.Float64() * 500}, b: [2]float64{rng.Float64() * 500, rng.Float64() * 500}, radius: rng.Float64() * 100}
		if n%4 == 0 {
			s.b[0] = s.a[0]
		}
		if n%5 == 0 {
			s.b[1] = s.a[1]
		}
		w, h := rng.Float64()*50, rng.Float64()*30
		start := len(buffer)
		buffer = s.appendForbidden(buffer, w, h)
		polygon := buffer[start:len(buffer)]
		if !slices.Equal(polygon, referenceConnectorTextForbidden(s, w, h)) {
			t.Fatalf("packed hull changed vertices: %d", n)
		}
		polygons = append(polygons, slices.Clone(polygon))
	}
	// Later appends must not mutate polygons already indexed in the buffer.
	at := 0
	for _, polygon := range polygons {
		if !slices.Equal(polygon, buffer[at:at+len(polygon)]) {
			t.Fatal("packed hull changed an earlier obstacle")
		}
		at += len(polygon)
	}
}

func BenchmarkConnectorTextSearchComparison(b *testing.B) {
	for _, count := range []int{100, 1000} {
		for _, algorithm := range []string{"reference", "indexed"} {
			b.Run(fmt.Sprintf("%d/%s", count, algorithm), func(b *testing.B) {
				var strokes []connectorTextStroke
				for i := 0; i < count; i++ {
					x := float64(i) * 4
					strokes = append(strokes, connectorTextStrokes(i, [][2]float64{{x, -100}, {x + 150, 100}}, false, 3)...)
				}
				path := [][2]float64{{-200, 0}, {float64(count)*4 + 200, 0}}
				search := connectorTextPosition
				if algorithm == "reference" {
					search = referenceConnectorTextPosition
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					search(path, false, 40, 20, nil, strokes...)
				}
			})
		}
	}
}

func connectorTextDenseSearchScene(count int) *Document {
	n := &Node{Shape: "composite"}
	for i := 0; i < 2*count; i++ {
		n.Parts = append(n.Parts, &CompositePart{ID: fmt.Sprintf("n%d", i), Shape: "rectangle", Geom: &Geom{W: 60, D: 40, H: 16}, Offset: &WorldPoint{WX: float64(i%16) * 100, WY: float64(i/16) * 100}})
	}
	for i := 0; i < count; i++ {
		n.Connectors = append(n.Connectors, &Connector{From: fmt.Sprintf("n%d", i), To: fmt.Sprintf("n%d", 2*count-1-i), Routing: "straight", Arrow: "triangle", Label: "request", LabelFontSize: 13, LabelFontFamily: "Source Sans Pro"})
	}
	return &Document{Nodes: map[string]*Node{"scene": n}}
}

func BenchmarkConnectorTextDenseRender(b *testing.B) {
	for _, count := range []int{16, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			doc := connectorTextDenseSearchScene(count)
			RenderScene(doc) // font/cache warmup is outside the timed work
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				RenderScene(doc)
			}
		})
	}
}
