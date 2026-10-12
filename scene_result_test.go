package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

func TestSceneResultScreenLabelsMatchSVG(t *testing.T) {
	for _, canvas := range []string{"{padding: 0, aspectRatio: -1}", "{padding: 53, aspectRatio: 2.4, depthSort: true}"} {
		t.Run(canvas, func(t *testing.T) {
			doc := parseWSH(t, `canvas: `+canvas+`
nodes:
  scene:
    shape: composite
    parts:
      - id: cluster
        shape: group
        offset: {wx: -280, wy: 90, wz: 45}
        parts:
          - id: wide
            shape: rectangle
            label: WWWWWWWW
            geom: {w: 80, d: 45, h: 25}
            stack: {count: 3, gap: 40}
            style:
              text: {orient: screen, family: Source Sans Pro, weight: bold, size: 13.237}
          - id: multi
            shape: rectangle
            label: "gyp\n\nǗgj"
            offset: {wx: 150, wy: 0, wz: 10}
            style:
              text: {orient: screen, family: Source Sans Pro, weight: normal, size: 20}
      - id: fallback
        shape: rectangle
        label: "中文 AV"
        offset: {wx: 200, wy: 0, wz: 0}
        style:
          text: {orient: screen, family: Source Sans Pro, size: 13.237}
      - shape: rectangle
        label: Anonymous
        offset: {wx: 400, wy: 0, wz: 0}
        style:
          text: {orient: screen, size: 17.555}
    connectors:
      - {from: wide.right, to: multi.left}
`)
			before, _ := json.Marshal(doc)
			result := RenderScene(doc)
			after, _ := json.Marshal(doc)
			if string(before) != string(after) {
				t.Fatal("render observation mutated source")
			}
			labels := screenTestLabels(t, result.SVG)
			if len(labels) != 4 || len(result.ScreenLabels) != len(labels) {
				t.Fatalf("wrong emitted labels: %d/%d", len(labels), len(result.ScreenLabels))
			}
			seenIndex := map[int]bool{}
			for i, record := range result.ScreenLabels {
				label := labels[i]
				r := label.Rects[0]
				if seenIndex[record.PartIndex] {
					t.Fatal("lowered identity aliased")
				}
				seenIndex[record.PartIndex] = true
				if record.PartID != label.ID || record.Bounds != (ScreenLabelRect{r.X, r.Y, r.W, r.H}) {
					t.Fatalf("record differs from emitted identity/rect: %+v %+v", record, label)
				}
				if len(record.Lines) != len(label.Texts) {
					t.Fatal("line count mismatch")
				}
				for j, line := range record.Lines {
					e := label.Texts[j]
					dy := 0.0
					if e.Dy != "" {
						dy = .35
					}
					if line.Text != e.Text || line.X != e.X || math.Abs(line.Baseline-e.Y-dy*e.Size) > 1e-10 || record.FontSize != e.Size {
						t.Fatalf("serialized baseline/size drift: %+v %+v", line, e)
					}
					if record.TextMetrics == "shaped-ink" {
						f := textmetrics.ResolveBuiltin(record.FontFamily, record.FontWeight)
						if f == nil || f.Hash() != record.FontHash || !strings.Contains(result.SVG, f.CSS()) {
							t.Fatal("font identity not bound to output")
						}
						m := f.Measure(line.Text)
						if line.Advance == nil || math.Abs(*line.Advance-m.Advance*e.Size) > 1e-10 {
							t.Fatal("advance mismatch")
						}
						if m.HasInk {
							b := screenTestInk(e, f)
							want := ScreenLabelRect{b.x0, b.y0, b.x1 - b.x0, b.y1 - b.y0}
							got := line.InkBounds
							if got == nil || math.Abs(got.X-want.X)+math.Abs(got.Y-want.Y)+math.Abs(got.Width-want.Width)+math.Abs(got.Height-want.Height) > 1e-8 {
								t.Fatal("submitted ink mismatch")
							}
						} else if line.InkBounds != nil {
							t.Fatal("blank line invented ink")
						}
					} else if line.Advance != nil || line.InkBounds != nil || record.FontHash != "" {
						t.Fatal("fallback claimed glyph knowledge")
					}
				}
			}
		})
	}
}

func TestSceneResultParityAndOwnership(t *testing.T) {
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		result := RenderScene(doc)
		svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
		if result.SVG != svg || !reflect.DeepEqual(result.Routes, routes) {
			t.Fatalf("collection changed %s", name)
		}
		labels := screenTestLabels(t, svg)
		if len(labels) != len(result.ScreenLabels) {
			t.Fatal("missing existing screen-label observations")
		}
		for i, record := range result.ScreenLabels {
			r := labels[i].Rects[0]
			if record.PartID != labels[i].ID || record.Bounds != (ScreenLabelRect{r.X, r.Y, r.W, r.H}) {
				t.Fatal("corpus emitted bounds differ")
			}
		}
		if len(result.ScreenLabels) > 0 {
			result.ScreenLabels[0].Bounds.X += 12345
			result.ScreenLabels[0].Lines[0].Text = "changed"
			again := RenderScene(doc)
			if again.ScreenLabels[0].Bounds.X == result.ScreenLabels[0].Bounds.X || again.ScreenLabels[0].Lines[0].Text == "changed" {
				t.Fatal("render results share mutable storage")
			}
		}
	})
}

func TestSceneResultReportAndEmptyLayer(t *testing.T) {
	doc := parseWSH(t, `nodes:
  scene:
    shape: composite
    parts:
      - id: n
        shape: rectangle
        label: AV office
        style: {text: {orient: screen, family: Source Sans Pro, size: 18}}
`)
	result, report := RenderSceneWithReport(doc)
	if !reflect.DeepEqual(result.ScreenLabels, report.ScreenLabels) || len(report.ScreenLabels) != 1 {
		t.Fatal("report lost committed labels")
	}
	if standalone := BuildRenderReport(doc); !reflect.DeepEqual(standalone.ScreenLabels, result.ScreenLabels) {
		t.Fatal("report wrapper differs")
	}
	for _, tc := range []struct {
		name string
		doc  *Document
	}{{"nil", nil}, {"empty", &Document{}}, {"top", func() *Document { d := cloneDocParts(doc); d.Canvas = &Canvas{Projection: "top"}; return d }()}} {
		t.Run(tc.name, func(t *testing.T) {
			r := RenderScene(tc.doc)
			if r.ScreenLabels == nil || len(r.ScreenLabels) != 0 {
				t.Fatal("known empty layer must be []")
			}
			b, _ := json.Marshal(r)
			if !strings.Contains(string(b), `"screen_labels":[]`) {
				t.Fatal("empty layer omitted")
			}
		})
	}
	// Failure to splice the layer must not publish geometry absent from SVG.
	records := []ScreenLabel{{PartID: "already-committed"}}
	svg, _ := injectScreenLabelsWithInfo("<svg>", []partInfo{screenTestInfo("AV", 18, "600")}, nil, &records)
	if svg != "<svg>" || len(records) != 1 {
		t.Fatal("partial label observation leaked after failed emission")
	}
}

func TestSceneResultScreenPrecision(t *testing.T) {
	for _, size := range []float64{13.237, 17.555, 20.005} {
		for _, family := range []string{"Source Sans Pro", "Inter, sans-serif"} {
			t.Run(fmt.Sprintf("%s/%g", family, size), func(t *testing.T) {
				p := screenTestInfo("A   B", size, "600")
				p.labelFamily = family
				p.offWX = -123.455
				p.offWY = 89.125
				var got []ScreenLabel
				svg, _ := injectScreenLabelsWithInfo(screenTestSVG, []partInfo{p}, nil, &got)
				g := screenTestLabels(t, svg)[0]
				if len(got) != 1 {
					t.Fatal("missing record")
				}
				e := g.Texts[0]
				base := e.Y
				if e.Dy != "" {
					base += .35 * e.Size
				}
				if got[0].FontSize != e.Size || got[0].Lines[0].Baseline != base {
					t.Fatal("rounding differs from emitted text")
				}
			})
		}
	}
}
