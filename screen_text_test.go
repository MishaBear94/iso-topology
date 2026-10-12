package isotopo

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

type screenTestElement struct {
	X    float64 `xml:"x,attr"`
	Y    float64 `xml:"y,attr"`
	W    float64 `xml:"width,attr"`
	H    float64 `xml:"height,attr"`
	Size float64 `xml:"font-size,attr"`
	Dy   string  `xml:"dy,attr"`
	Text string  `xml:",chardata"`
}
type screenTestLabel struct {
	ID    string              `xml:"data-part-id,attr"`
	Rects []screenTestElement `xml:"rect"`
	Texts []screenTestElement `xml:"text"`
}

func screenTestLabels(t *testing.T, svg string) []screenTestLabel {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(svg))
	var labels []screenTestLabel
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "g" {
			continue
		}
		for _, a := range start.Attr {
			if a.Name.Local == "data-screen-label" {
				var label screenTestLabel
				if err := dec.DecodeElement(&label, &start); err != nil {
					t.Fatal(err)
				}
				labels = append(labels, label)
				break
			}
		}
	}
	return labels
}

const screenTestSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 300" width="400" height="300"></svg>`

func screenTestInfo(label string, size float64, weight string) partInfo {
	return partInfo{id: "label", w: 100, d: 60, h: 20, screenLabel: label, labelFamily: "Source Sans Pro", labelWeight: weight, labelFontSize: size, labelBg: "white", labelColor: "black"}
}
func screenTestInk(e screenTestElement, f *textmetrics.BuiltinFont) screenRect {
	m := f.Measure(e.Text)
	dy, _ := strconv.ParseFloat(strings.TrimSuffix(e.Dy, "em"), 64)
	baseline := e.Y + dy*e.Size
	return screenRect{e.X + (m.Left-m.Advance/2)*e.Size, baseline + m.Top*e.Size, e.X + (m.Right-m.Advance/2)*e.Size, baseline + m.Bottom*e.Size}
}
func TestBoundFontScreenLabelGeometry(t *testing.T) {
	labels := []string{"WWWWWWWW", "iiiiiiiiiiii", "Ǘgj", "gyp\nÁgj", "Á\n\ngj", "AV office", "  A   B  ", "Ελληνικά", "Привет"}
	for _, weight := range []string{"400", "600", "700"} {
		for _, size := range []float64{9.75, 13.237, 20, 40} {
			for index, label := range labels {
				t.Run(fmt.Sprintf("%s/%.3f/%d", weight, size, index), func(t *testing.T) {
					p := screenTestInfo(label, size, weight)
					svg, placed := injectScreenLabels(screenTestSVG, []partInfo{p}, nil)
					fonts := textmetrics.FontsInSVG(svg)
					if len(fonts) != 1 || fonts[0].Weight() != weight {
						t.Fatalf("screen-only label lost its bound font: %d resources", len(fonts))
					}
					got := screenTestLabels(t, svg)
					if len(got) != 1 || len(placed) != 1 || len(got[0].Rects) != 2 {
						t.Fatal("chip, hit target or label missing")
					}
					g := got[0]
					rect := g.Rects[0]
					if collides(placed[0], partScreenRects([]partInfo{p})) {
						t.Fatal("measured chip overlaps its owning body")
					}
					if rect != g.Rects[1] {
						t.Fatal("visible and interactive boxes differ")
					}
					if g.ID != p.id {
						t.Fatal("source identity lost")
					}
					var text []string
					last := -1
					for i, line := range g.Texts {
						text = append(text, line.Text)
						if math.Abs(line.Size-math.Round(size*100)/100) > 1e-9 {
							t.Fatal("font geometry differs from serialized size")
						}
						m := fonts[0].Measure(line.Text)
						if !m.HasInk {
							continue
						}
						b := screenTestInk(line, fonts[0])
						if b.x0 < rect.X+7.99 || b.x1 > rect.X+rect.W-7.99 || b.y0 < rect.Y+4.99 || b.y1 > rect.Y+rect.H-4.99 {
							t.Fatalf("ink escapes chip padding: %q ink=%+v rect=%+v", line.Text, b, rect)
						}
						if last >= 0 {
							prev := screenTestInk(g.Texts[last], fonts[0])
							if b.y0-prev.y1 < .12*line.Size-.01 {
								t.Fatal("actual ink line gap too small")
							}
						}
						last = i
					}
					if strings.Join(text, "\n") != label {
						t.Fatalf("authored text changed: %q", strings.Join(text, "\n"))
					}
				})
			}
		}
	}
}
func TestBoundFontScreenTextObstacleClearance(t *testing.T) {
	p := screenTestInfo("WWWWWWWW", 20, "600")
	tx, ty := partsScreenOrigin([]partInfo{p})
	cx, base := projectIso(p.w/2, p.d, 0)
	cx += tx
	base += ty + 14
	oldWidth := textmetrics.TextPxWideBytes(p.screenLabel, 20, textmetrics.NarrowScreenLabel) + 16
	obstacle := screenRect{cx + oldWidth/2 + 8, base, cx + oldWidth/2 + 20, base + 30}
	svg, placed := injectScreenLabels(screenTestSVG, []partInfo{p}, []screenRect{obstacle})
	label := screenTestLabels(t, svg)[0]
	f := textmetrics.ResolveBuiltin(p.labelFamily, p.labelWeight)
	for _, line := range label.Texts {
		if screenTestInk(line, f).intersects(obstacle.inflate(textClearance)) {
			t.Fatal("measured text collides outside the estimated label box")
		}
	}
	if collides(placed[0], []screenRect{obstacle}) {
		t.Fatal("placement ignored its chip")
	}
}
func TestBoundFontScreenResourceAndFallback(t *testing.T) {
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "600")
	infos := []partInfo{screenTestInfo("AV office", 18, "600"), screenTestInfo("Ǘgj", 18, "600")}
	infos[1].offWX = 200
	svg, _ := injectScreenLabels(strings.Replace(screenTestSVG, "</svg>", f.CSS()+"</svg>", 1), infos, nil)
	if strings.Count(svg, `data-isotopo-font=`) != 1 {
		t.Fatal("screen labels duplicated existing font resource")
	}
	for _, s := range []string{"中文", "👩🏽‍💻", "A\tB", "עברית"} {
		b := layoutScreenText(s, "Source Sans Pro", "600", 18)
		if b.font != nil || b.width != textmetrics.TextPxWideBytes(s, 18, textmetrics.NarrowScreenLabel)+16 || b.height != 28 {
			t.Fatalf("fallback incorrectly certified: %q", s)
		}
	}
	if b := layoutScreenText("WWWWWWWW", "Inter, sans-serif", "600", 18); b.font != nil {
		t.Fatal("system font treated as bound")
	}
}
