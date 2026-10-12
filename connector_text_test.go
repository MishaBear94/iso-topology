package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

func connectorTextTestDoc(t *testing.T, projection, routing string) *Document {
	t.Helper()
	return parseWSH(t, `canvas: {background: '#152235', projection: `+projection+`, padding: 20, aspectRatio: -1}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 100, d: 60, h: 20}}
      - {id: b, shape: rectangle, geom: {w: 100, d: 60, h: 20}, offset: {wx: 750, wy: 0}}
      - {id: c, shape: rectangle, geom: {w: 100, d: 60, h: 20}, offset: {wx: 0, wy: 450}}
      - {id: d, shape: rectangle, geom: {w: 100, d: 60, h: 20}, offset: {wx: 750, wy: 450}}
    connectors:
      - {from: a, to: b, routing: `+routing+`, arrow: triangle, label: 'AV office', labelFontFamily: Source Sans Pro}
      - {from: c, to: d, routing: `+routing+`, arrow: triangle, label: 'WWW', labelFontFamily: Source Sans Pro}
`)
}

func TestConnectorTextBoundGeometry(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		for _, weight := range []string{"400", "600", "700"} {
			for _, size := range []float64{9.75, 13.237, 32} {
				for i, text := range []string{"WWWWWWWW", "iiiiiiiiiiii", "Ǘgj\n\ngyp", "  AV office  ", "Ελληνικά Привет"} {
					t.Run(fmt.Sprintf("%s/%s/%.3f/%d", projection, weight, size, i), func(t *testing.T) {
						doc := connectorTextTestDoc(t, projection, "straight")
						for _, c := range doc.Scene().Connectors {
							c.Label, c.LabelFontWeight, c.LabelFontSize = text, weight, size
						}
						before, _ := json.Marshal(doc)
						scene := RenderScene(doc)
						after, _ := json.Marshal(doc)
						if string(before) != string(after) {
							t.Fatal("render mutated input")
						}
						if scene.SVG != RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations) {
							t.Fatal("observation changes SVG")
						}
						labels := screenTestLabels(t, strings.ReplaceAll(scene.SVG, "data-connector-label=", "data-screen-label="))
						if len(labels) != 2 || len(scene.ConnectorLabels) != 2 {
							t.Fatal("bound labels missing")
						}
						font := textmetrics.ResolveBuiltin("Source Sans Pro", weight)
						if strings.Count(scene.SVG, font.CSS()) != 1 {
							t.Fatal("font resource missing or duplicated")
						}
						for j, record := range scene.ConnectorLabels {
							label, emitted := record.Label, labels[j]
							if record.ConnectorIndex != j || label.Typography.FontHash != font.Hash() {
								t.Fatal("identity mismatch")
							}
							if label.Typography.Placement != "clear-within-scope" {
								t.Fatal("spacious fixture must place label")
							}
							if len(emitted.Rects) != 1 || len(emitted.Texts) != len(label.Typography.Lines) {
								t.Fatal("rect/line mismatch")
							}
							r := emitted.Rects[0]
							if label.Rect != ([4]float64{r.X, r.Y, r.W, r.H}) {
								t.Fatal("serialized rectangle differs")
							}
							var texts []string
							for k, e := range emitted.Texts {
								line := label.Typography.Lines[k]
								texts = append(texts, e.Text)
								if e.X != line.X || e.Y != line.Baseline || e.Size != math.Round(size*100)/100 {
									t.Fatal("baseline/size differs")
								}
								if !font.Measure(e.Text).HasInk {
									if line.InkBounds != nil {
										t.Fatal("blank line has ink")
									}
									continue
								}
								ink := screenTestInk(e, font)
								if ink.x0 < r.X+5.98 || ink.x1 > r.X+r.W-5.98 || ink.y0 < r.Y+3.98 || ink.y1 > r.Y+r.H-3.98 {
									t.Fatalf("ink escapes padding: %+v %+v", ink, r)
								}
							}
							if strings.Join(texts, "\n") != text {
								t.Fatal("authored text lost")
							}
						}
					})
				}
			}
		}
	}
}

func TestConnectorTextContinuousIntervals(t *testing.T) {
	// Segment-midpoint search fails here, while a clear inline interval exists.
	points := [][2]float64{{0, 0}, {300, 0}}
	obstacles := []screenRect{{130, -40, 170, 40}}
	x, y, ok := connectorTextPosition(points, false, 40, 20, obstacles)
	if !ok || y != 0 || x > 104.001 || x < 103.9 {
		t.Fatalf("nearest free interval lost: %.8f %.8f %v", x, y, ok)
	}
	if _, _, ok := connectorTextPosition(points, false, 40, 20, []screenRect{{-100, -100, 400, 100}}); ok {
		t.Fatal("blocked route declared clear")
	}
	// A quadratic's label must lie on its curve, never its control polygon.
	points = append(points[:1], [2]float64{150, 300}, [2]float64{300, 0})
	x, y, ok = connectorTextPosition(points, true, 40, 20, nil)
	if !ok || x != 150 || y != 150 {
		t.Fatalf("quadratic midpoint wrong: %.8f %.8f", x, y)
	}
	x, y, ok = connectorTextPosition(points, true, 40, 20, []screenRect{{125, 130, 175, 170}})
	u := x / 300
	if !ok || math.Abs(y-600*u*(1-u)) > 1e-8 {
		t.Fatal("label detached from actual quadratic")
	}
}

func TestConnectorTextIndependentIntervalOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(1729, 53))
	clearCount := 0
	for n := 0; n < 600; n++ {
		curved := n%2 == 0
		points := [][2]float64{{-200, 0}, {200, 0}}
		if curved {
			points = append(points[:1], [2]float64{rng.Float64()*100 - 50, 200}, [2]float64{200, 0})
		}
		w, h := 20+rng.Float64()*90, 12+rng.Float64()*40
		var obs []screenRect
		for i := 0; i < 12; i++ {
			x, y := rng.Float64()*500-250, rng.Float64()*250-80
			obs = append(obs, screenRect{x, y, x + 20 + rng.Float64()*50, y + 10 + rng.Float64()*50})
		}
		x, y, ok := connectorTextPosition(points, curved, w, h, obs)
		// Independent collision predicate over the final serialized rectangle.
		free := func(x, y float64) bool {
			x0, y0 := math.Round((x-w/2)*100)/100, math.Round((y-h/2)*100)/100
			x1, y1 := x0+math.Round(w*100)/100, y0+math.Round(h*100)/100
			for _, o := range obs {
				if x0 < o.x1+6 && x1 > o.x0-6 && y0 < o.y1+6 && y1 > o.y0-6 {
					return false
				}
			}
			return true
		}
		if ok {
			clearCount++
			if !free(x, y) {
				t.Fatalf("false clearance in case %d", n)
			}
		} else {
			// A dense oracle can disprove a false impossibility claim. It does
			// not certify completeness for every possible narrow interval.
			for i := 0; i <= 1000; i++ {
				u := float64(i) / 1000
				px, py := -200+400*u, 0.0
				if curved {
					px = (1-u)*(1-u)*(-200) + 2*(1-u)*u*points[1][0] + u*u*200
					py = 400 * (1 - u) * u
				}
				if free(px, py) {
					t.Fatalf("missed feasible interval case %d t=%g", n, u)
				}
			}
		}
		xx, yy, again := connectorTextPosition(points, curved, w, h, obs)
		if xx != x || yy != y || again != ok {
			t.Fatal("nondeterministic placement")
		}
	}
	if clearCount < 300 {
		t.Fatalf("weak fixture: only %d clear cases", clearCount)
	}
}

func TestConnectorTextReportAndFallback(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		doc := connectorTextTestDoc(t, projection, "bezier")
		for _, c := range doc.Scene().Connectors {
			c.Label = strings.Repeat(strings.Repeat("W", 120)+"\n", 8)
			c.LabelFontSize = 40
		}
		scene, report := RenderSceneWithReport(doc)
		if !reflect.DeepEqual(scene.ConnectorLabels, report.ConnectorLabels) {
			t.Fatal("report reinterprets label geometry")
		}
		warnings := 0
		for _, d := range report.Defects {
			if d.Kind == "connector-label-placement" {
				warnings++
			}
		}
		if warnings != 2 || !strings.Contains(scene.SVG, `data-label-placement="blocked"`) {
			t.Fatalf("blocked labels not diagnosed: %d", warnings)
		}
		if projection == "top" && !strings.Contains(scene.SVG, `data-layer="canvas-bg"`) {
			t.Fatal("grown plan canvas lost backdrop")
		}
		for _, c := range doc.Scene().Connectors {
			c.Label = "中文 AV"
			c.LabelFontSize = 24
			c.LabelFontFamily = "Unknown Font"
		}
		scene = RenderScene(doc)
		if len(scene.ConnectorLabels) != 0 || strings.Contains(scene.SVG, `data-connector-label=`) {
			t.Fatal("unknown text certified")
		}
		if !strings.Contains(scene.SVG, `font-family="Unknown Font"`) || !strings.Contains(scene.SVG, `font-size="24.`) {
			t.Fatal("explicit fallback style ignored")
		}
	}
}

func TestConnectorTextFutureRouteAndArrowProtection(t *testing.T) {
	for _, routing := range []string{"straight", "orthogonal", "orthogonal2", "bezier"} {
		t.Run(routing, func(t *testing.T) {
			doc := connectorTextTestDoc(t, "iso", routing)
			doc.Scene().Connectors[1].From = "a"
			doc.Scene().Connectors[1].To = "d"
			scene := RenderScene(doc)
			if len(scene.ConnectorLabels) != 2 {
				t.Fatal("labels missing")
			}
			for _, record := range scene.ConnectorLabels {
				if record.Label.Typography.Placement == "blocked" {
					continue
				}
				r := connectorLabelRect(record.Label)
				for _, route := range scene.Routes {
					d := route.Drawing
					if route.Index != record.ConnectorIndex {
						// Independently sample the actual submitted path, rather than
						// treating a diagonal's empty AABB corners as stroke ink.
						for i := 1; i < len(d.Points); i++ {
							for j := 0; j <= 4000; j++ {
								u := float64(j) / 4000
								x := d.Points[i-1][0]*(1-u) + d.Points[i][0]*u
								y := d.Points[i-1][1]*(1-u) + d.Points[i][1]*u
								if d.Kind == "quadratic" {
									x = (1-u)*(1-u)*d.Points[0][0] + 2*(1-u)*u*d.Points[1][0] + u*u*d.Points[2][0]
									y = (1-u)*(1-u)*d.Points[0][1] + 2*(1-u)*u*d.Points[1][1] + u*u*d.Points[2][1]
								}
								pad := 6 + d.StrokeWidth/2 - .01
								if x > r.x0-pad && x < r.x1+pad && y > r.y0-pad && y < r.y1+pad {
									t.Fatal("label crosses a later route")
								}
							}
							if d.Kind == "quadratic" {
								break
							}
						}
					}
					for _, p := range d.Arrow {
						if p[0] >= r.x0 && p[0] <= r.x1 && p[1] >= r.y0 && p[1] <= r.y1 {
							t.Fatal("label covers arrow")
						}
					}
				}
			}
		})
	}
}

func TestConnectorTextLateBlockedLabelInvalidatesEarlierStatus(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		doc := connectorTextTestDoc(t, projection, "straight")
		doc.Scene().Connectors[1].From = "a"
		doc.Scene().Connectors[1].To = "b"
		doc.Scene().Connectors[1].Label = strings.Repeat(strings.Repeat("W", 120)+"\n", 8)
		doc.Scene().Connectors[1].LabelFontSize = 40
		scene, report := RenderSceneWithReport(doc)
		warnings := 0
		for _, d := range report.Defects {
			if d.Kind == "connector-label-placement" {
				warnings++
			}
		}
		if warnings != 2 || strings.Count(scene.SVG, `data-label-placement="blocked"`) != 2 {
			t.Fatal("final scene retains a stale clearance claim")
		}
	}
	// Direct producer fixture ensures the early record really began as clear;
	// coincident routes in the integration fixture can already block it.
	a := ConnectorLabelRecord{0, ConnectorLabel{Rect: [4]float64{0, 0, 40, 20}, Typography: &ConnectorTypography{Placement: "clear-within-scope"}}}
	b := ConnectorLabelRecord{1, ConnectorLabel{Rect: [4]float64{-10, -10, 100, 100}, Typography: &ConnectorTypography{Placement: "blocked"}}}
	markup := finishConnectorText(`<g data-connector-label="0" data-label-placement="clear-within-scope"></g>`, []ConnectorLabelRecord{a, b})
	if a.Label.Typography.Placement != "blocked" || !strings.Contains(markup, `data-label-placement="blocked"`) {
		t.Fatal("late blocked label did not invalidate early clear record")
	}
}

func TestConnectorTextFieldsAndRoundTrip(t *testing.T) {
	doc := connectorTextTestDoc(t, "iso", "straight")
	src, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := Fields("json", src, "edge", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range fields {
		seen[f.Path] = true
	}
	for _, name := range []string{"labelFontFamily", "labelFontWeight", "labelFontSize"} {
		if !seen[name] {
			t.Fatal("missing field", name)
		}
	}
	for _, issue := range UnknownKeyIssues(src) {
		if strings.Contains(issue.Message, "labelFont") {
			t.Fatal(issue)
		}
	}
	var roundTrip Document
	if err := json.Unmarshal(src, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if RenderScene(doc).SVG != RenderScene(&roundTrip).SVG {
		t.Fatal("font selection lost in round trip")
	}
}

func BenchmarkConnectorTextIntervals(b *testing.B) {
	for _, count := range []int{100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			obs := make([]screenRect, count)
			for i := range obs {
				x := float64(i) * 4
				obs[i] = screenRect{x, -10, x + 1, 10}
			}
			points := [][2]float64{{-100, 0}, {float64(count)*4 + 100, 0}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				connectorTextPosition(points, false, 40, 20, obs)
			}
		})
	}
}
