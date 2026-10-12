package isotopo

import (
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func compactTestPlacement(index int, x, y float64) screenLabelPlacement {
	b := layoutScreenText("Label", "Source Sans Pro", "600", 16)
	b.width, b.height = 60, 20
	return screenLabelPlacement{index: index, block: b, x: x, y: y}
}
func TestScreenLabelCompactionKeepsWholeScene(t *testing.T) {
	bodies := []screenRect{{0, 0, 100, 80}, {200, 0, 300, 80}}
	// First label's old distance ladder skips a legal gap after this obstacle.
	static := append(append([]screenRect(nil), bodies...), screenRect{0, 95, 100, 145})
	labels := []screenLabelPlacement{compactTestPlacement(0, 20, 176), compactTestPlacement(1, 220, 90)}
	before := append([]screenLabelPlacement(nil), labels...)
	compactScreenLabels(labels, bodies, static, screenLabelProtection{})
	if math.Abs(labels[0].y-151.02) > 1e-8 {
		t.Fatalf("did not use nearer legal gap: %+v", labels[0].rect())
	}
	if !reflect.DeepEqual(labels[1], before[1]) {
		t.Fatal("early improvement displaced an already clean peer")
	}
	for i, p := range labels {
		obs := append([]screenRect(nil), static...)
		for j, l := range labels {
			if i != j {
				obs = append(obs, l.rect())
			}
		}
		if collides(p.rect(), obs) {
			t.Fatal("compaction introduced an overlap")
		}
	}
}
func TestScreenLabelCompactionDirections(t *testing.T) {
	bodies := []screenRect{{0, 0, 100, 80}}
	for _, c := range []struct {
		name               string
		x, y, wantX, wantY float64
	}{
		{"below", 20, 176, 20, 90}, {"above", 20, -116, 20, -30},
		{"right", 196, 30, 110, 30}, {"left", -156, 30, -70, 30},
	} {
		t.Run(c.name, func(t *testing.T) {
			labels := []screenLabelPlacement{compactTestPlacement(0, c.x, c.y)}
			compactScreenLabels(labels, bodies, bodies, screenLabelProtection{})
			if labels[0].x != c.wantX || labels[0].y != c.wantY {
				t.Fatalf("wrong side/alignment: %+v", labels[0].rect())
			}
		})
	}
	// A clean preferred spot can happen to align with a cardinal ray for a
	// very thin part. Identity of the preferred choice, not an epsilon guess,
	// keeps it fixed and avoids preparing expensive geometry.
	labels := []screenLabelPlacement{compactTestPlacement(0, 20, 94)}
	labels[0].preferred = true
	compactScreenLabels(labels, bodies, bodies, screenLabelProtection{prepare: func(*screenLabelProtection) { t.Fatal("prepared a clean preferred label") }})
	if labels[0].y != 94 {
		t.Fatal("clean preferred label moved")
	}
	for _, vertical := range []bool{true, false} {
		p := compactTestPlacement(0, 20.006, 176.006)
		p.block.width, p.block.height = 60.01, 20.01
		p.block.lines = append([]screenTextLine(nil), p.block.lines...)
		p.block.lines[0].baseline = 8.006
		if !vertical {
			p.x, p.y = 196.006, 30.006
		}
		before := observeScreenLabel(0, partInfo{}, p.block, p.x+p.block.width/2, p.y, "Source Sans Pro", "600")
		labels := []screenLabelPlacement{p}
		fractionalBodies := []screenRect{{.015, .015, 100.015, 80.015}}
		compactScreenLabels(labels, fractionalBodies, fractionalBodies, screenLabelProtection{})
		a := labels[0]
		if a.x == p.x && a.y == p.y {
			t.Fatal("fractional fixture did not exercise a move")
		}
		after := observeScreenLabel(0, partInfo{}, a.block, a.x+a.block.width/2, a.y, "Source Sans Pro", "600")
		if vertical && before.Lines[0].X != after.Lines[0].X || !vertical && before.Lines[0].Baseline != after.Lines[0].Baseline {
			t.Fatal("unchanged axis shifted after rectangle rounding")
		}
	}
}

func TestScreenLabelCompactionProtectsLateLabelsAndUnknowns(t *testing.T) {
	bodies := []screenRect{{0, 0, 100, 80}, {200, 0, 300, 80}}
	base := []screenLabelPlacement{compactTestPlacement(0, 20, 176), compactTestPlacement(1, 20, 110)}
	labels := append([]screenLabelPlacement(nil), base...)
	compactScreenLabels(labels, bodies, bodies, screenLabelProtection{})
	if labels[0].y != 136.02 || !reflect.DeepEqual(labels[1], base[1]) {
		t.Fatal("later label's reserved space was stolen")
	}
	for _, unknown := range []bool{false, true} {
		labels = append([]screenLabelPlacement(nil), base...)
		if unknown {
			labels[1].block.font = nil
		}
		before := append([]screenLabelPlacement(nil), labels...)
		compactScreenLabels(labels, bodies, bodies, screenLabelProtection{uncertain: !unknown})
		if !reflect.DeepEqual(labels, before) {
			t.Fatal("unmeasured envelope was treated as safe")
		}
	}
	labels = append([]screenLabelPlacement(nil), base[:1]...)
	guard := screenRect{0, 85, 100, 150}
	compactScreenLabels(labels, bodies, bodies, screenLabelProtection{rects: []screenRect{guard}})
	if labels[0].y != 156.02 {
		t.Fatal("producer guard ignored")
	}
}
func TestScreenLabelCompactionIndependentMatrix(t *testing.T) {
	rng := rand.New(rand.NewSource(261012))
	improved := 0
	for trial := 0; trial < 500; trial++ {
		var bodies []screenRect
		var labels []screenLabelPlacement
		for j := 0; j < 8; j++ {
			x, y := rng.Float64()*900-450, rng.Float64()*600-300
			bodies = append(bodies, screenRect{x, y, x + 60 + rng.Float64()*100, y + 40 + rng.Float64()*70})
		}
		obstacles := append([]screenRect(nil), bodies...)
		for i, b := range bodies {
			p := compactTestPlacement(i, 0, 0)
			p.x, p.y = placeTextBox(p.block.width, p.block.height, b, b.cx()-30, b.y1+10, 0, 0, obstacles)
			labels = append(labels, p)
			obstacles = append(obstacles, p.rect())
		}
		before := append([]screenLabelPlacement(nil), labels...)
		compactScreenLabels(labels, bodies, bodies, screenLabelProtection{})
		repeat := append([]screenLabelPlacement(nil), before...)
		compactScreenLabels(repeat, bodies, bodies, screenLabelProtection{})
		if !reflect.DeepEqual(labels, repeat) {
			t.Fatal("non-deterministic")
		}
		distance := func(a, b screenRect) float64 {
			dx := math.Max(0, math.Max(a.x0-b.x1, b.x0-a.x1))
			dy := math.Max(0, math.Max(a.y0-b.y1, b.y0-a.y1))
			return math.Sqrt(dx*dx + dy*dy)
		}
		for i, p := range labels {
			a, z := p.rect(), before[i].rect()
			owner := bodies[i]
			if a == z {
				continue
			}
			improved++
			if distance(a, owner) >= distance(z, owner)-.99 {
				t.Fatal("moved without meaningful owner improvement")
			}
			for j, b := range bodies {
				if a.x0 < b.x1+6 && a.x1 > b.x0-6 && a.y0 < b.y1+6 && a.y1 > b.y0-6 {
					t.Fatalf("trial %d new body overlap", trial)
				}
				if j != i && distance(a, b)-distance(a, owner) < distance(z, b)-distance(z, owner)-.011 {
					t.Fatal("worse peer association")
				}
			}
			for j, l := range labels {
				if j == i {
					continue
				}
				b := l.rect()
				if a.x0 < b.x1+6 && a.x1 > b.x0-6 && a.y0 < b.y1+6 && a.y1 > b.y0-6 {
					t.Fatal("new label overlap")
				}
			}
		}
	}
	if improved < 100 {
		t.Fatalf("only %d improvements; corpus no longer exercises compaction", improved)
	}
	t.Logf("improved %d labels across 500 scenes", improved)
}
func TestScreenLabelCompactionEmitterAndProtection(t *testing.T) {
	p := screenTestInfo("Label", 16, "600")
	infos := []partInfo{p}
	owner := partScreenRects(infos)[0]
	obstacle := screenRect{owner.x0 - 30, owner.y1 + 4, owner.x1 + 30, owner.y1 + 53}
	svg, _ := injectScreenLabels(screenTestSVG, infos, []screenRect{obstacle})
	rect := screenTestLabels(t, svg)[0].Rects[0]
	if math.Abs(rect.Y-(svgCoord(obstacle.y1+6.02))) > 1e-8 {
		t.Fatal("emitter did not use compact result")
	}
	var records []ScreenLabel
	same, _ := injectScreenLabelsWithInfo(screenTestSVG, infos, []screenRect{obstacle}, &records)
	if svg != same || records[0].Bounds.Y != rect.Y {
		t.Fatal("collected and emitted compact geometry diverged")
	}
	for _, routing := range []string{"straight", "orthogonal", "bezier"} {
		t.Run(routing, func(t *testing.T) {
			infos := []partInfo{{id: "a", w: 100, d: 80, h: 20}, {id: "b", w: 100, d: 80, h: 20, offWX: 300, offWY: 80}}
			width := 28.0
			protection := screenLabelProtection{}
			rc := &routeCollector{}
			injectCompositeConnectors(screenTestSVG, []*Connector{{From: "a.right", To: "b.left", Routing: routing, Arrow: "triangle", Stroke: &Stroke{Width: &width}}}, infos, nil, 0, rc, false, &protection)
			if len(rc.routes) != 1 || len(protection.rects) < 2 {
				t.Fatal("missing connector protection")
			}
			d := rc.routes[0].Drawing
			contains := func(x, y float64) bool {
				for _, b := range protection.rects {
					if x >= b.x0 && x <= b.x1 && y >= b.y0 && y <= b.y1 {
						return true
					}
				}
				return false
			}
			for _, pt := range d.Arrow {
				if !contains(pt[0], pt[1]) {
					t.Fatal("arrow not protected")
				}
			}
			if d.Kind == "quadratic" {
				for k := 0; k <= 100; k++ {
					v := float64(k) / 100
					u := 1 - v
					x := u*u*d.Points[0][0] + 2*u*v*d.Points[1][0] + v*v*d.Points[2][0]
					y := u*u*d.Points[0][1] + 2*u*v*d.Points[1][1] + v*v*d.Points[2][1]
					if !contains(x, y) {
						t.Fatal("curve not protected")
					}
				}
			}
		})
	}
	protection := screenLabelProtection{}
	protectScreenLabelContent([]iso25d.CompositePart{{Shape: "rectangle", Opts: iso25d.ConvertOpts{Width: 100, Depth: 80, Height: 20, Label: "Unknown font"}}}, infos, &protection)
	if !protection.uncertain {
		t.Fatal("unbound top-face text was certified")
	}
}

func BenchmarkScreenLabelCompaction(b *testing.B) {
	for _, n := range []int{100, 1000} {
		name := "100"
		if n == 1000 {
			name = "1000"
		}
		b.Run(name, func(b *testing.B) {
			bodies := make([]screenRect, n)
			base := make([]screenLabelPlacement, n)
			for i := range bodies {
				x, y := float64(i%25)*180, float64(i/25)*240
				bodies[i] = screenRect{x, y, x + 100, y + 80}
				base[i] = compactTestPlacement(i, x+20, y+176)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				labels := append([]screenLabelPlacement(nil), base...)
				compactScreenLabels(labels, bodies, bodies, screenLabelProtection{})
			}
		})
	}
}
