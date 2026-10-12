package iso25d

import (
	"encoding/xml"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

// Read actual serialized SVG, including its viewport and font resource. The
// observer must agree with it even after a wide caption expands the frame.
func TestBoundCaptionSVGContract(t *testing.T) {
	for _, shape := range []string{"person", "circle", "cloud"} {
		for _, weight := range []string{"400", "600", "700"} {
			for li, label := range []string{"AV office", "j" + strings.Repeat("W", 35), "Ágj\n\nǗff e\u0301\nМосква", "Αθήνα  A  B"} {
				for _, icon := range []string{"", ResolveBrandIcon("iso://glyph/database")} {
					t.Run(fmt.Sprintf("%s-%s-%d-icon%t", shape, weight, li, icon != ""), func(t *testing.T) {
						o := ConvertOpts{Width: 80, Depth: 115, Height: 27, FontFamily: "Source Sans Pro", FontWeight: weight, FontSize: 13.236, Label: label, Icon: icon}
						original := o
						content := ShapeContent(shape, o)
						svg := Convert2DTo25D(shape, o)
						if !reflect.DeepEqual(o, original) || content.EstimatedText || strings.Count(svg, `<style data-isotopo-font=`) != 1 {
							t.Fatal("unbound, duplicate font resource, or mutated input")
						}
						actual := svgContent(t, svg)
						if len(actual) != len(content.Elements) {
							t.Fatalf("actual %d / observed %d elements", len(actual), len(content.Elements))
						}
						var root struct {
							ViewBox string `xml:"viewBox,attr"`
						}
						if err := xml.Unmarshal([]byte(svg), &root); err != nil {
							t.Fatal(err)
						}
						var vx, vy, vw, vh float64
						if _, err := fmt.Sscan(root.ViewBox, &vx, &vy, &vw, &vh); err != nil {
							t.Fatal(err)
						}
						ox, oy := PartOriginOffset(shape, o)
						var emittedLines []string
						lastBottom := math.Inf(-1)
						for i, e := range content.Elements {
							a := actual[i]
							if e.Kind != a.Kind || e.Text != a.Text || math.Abs(e.FontSize-a.FontSize) > .006 || e.DyEm != a.DyEm || math.Hypot(e.Anchor[0]+ox-a.Anchor[0], e.Anchor[1]+oy-a.Anchor[1]) > .05 {
								t.Fatalf("actual SVG disagrees with observer: %+v / %+v", a, e)
							}
							for _, p := range e.Quad {
								if p[0]+ox < vx+23.9 || p[0]+ox > vx+vw-23.9 || p[1]+oy < vy+23.9 || p[1]+oy > vy+vh-23.9 {
									t.Fatalf("content clipped or lost export margin: %v in %s", p, root.ViewBox)
								}
							}
							if e.Kind == "text" {
								emittedLines = append(emittedLines, a.Text)
								if (shape != "cloud" || icon == "") && a.FontSize != 13.24 {
									t.Fatal("free caption shrank below the authored size")
								}
								if shape != "cloud" {
									if a.FontSize != 13.24 || e.Quad[0][1] < lastBottom+1.5 {
										t.Fatal("free caption shrank or lines collide")
									}
									lastBottom = e.Quad[2][1]
								}
							}
							if e.Kind == "icon" && shape == "circle" {
								lastBottom = e.Quad[2][1] + 2.5
							}
						}
						if (shape != "cloud" || icon == "") && !reflect.DeepEqual(emittedLines, strings.Split(label, "\n")) {
							t.Fatal("authored lines changed")
						}
						if (shape != "cloud" || icon == "") && content.Fit.Status != "unsupported" {
							t.Fatal("free caption falsely certified as fitting a bounded face")
						}
					})
				}
			}
		}
	}
}

func TestBoundCaptionWorldBodyDoesNotMove(t *testing.T) {
	for _, shape := range []string{"person", "circle", "cloud"} {
		o := ConvertOpts{Width: 80, Depth: 115, Height: 27, FontFamily: "Source Sans Pro", FontSize: 24}
		before := svgBodyPolygons(t, Convert2DTo25D(shape, o))
		bx, by := PartOriginOffset(shape, o)
		o.Label = "j" + strings.Repeat("W", 40) + "\nÁgj\nǗff"
		after := svgBodyPolygons(t, Convert2DTo25D(shape, o))
		ax, ay := PartOriginOffset(shape, o)
		if len(before) != len(after) {
			t.Fatal("body faces changed")
		}
		for i, poly := range before {
			if len(poly) != len(after[i]) {
				t.Fatal("body topology changed")
			}
			for j, p := range poly {
				q := after[i][j]
				if math.Hypot(p[0]-bx-q[0]+ax, p[1]-by-q[1]+ay) > .025 {
					t.Fatalf("%s body drifted when the caption widened", shape)
				}
			}
		}
	}
}

func TestBoundCaptionFallbackAndFontDedup(t *testing.T) {
	var parts []CompositePart
	for _, shape := range []string{"person", "circle", "cloud"} {
		for _, tc := range []struct{ family, weight, label string }{
			{"unknown", "600", "AV office"}, {"Source Sans Pro", "500", "AV office"},
			{"Source Sans Pro", "600", "AV\n中文"}, {"Source Sans Pro", "600", "AV\n🙂"},
		} {
			o := ConvertOpts{Label: tc.label, FontFamily: tc.family, FontWeight: tc.weight}
			if strings.Contains(Convert2DTo25D(shape, o), `data-isotopo-font=`) || !ShapeContent(shape, o).EstimatedText {
				t.Fatalf("%s partially bound text was certified", shape)
			}
		}
		parts = append(parts, CompositePart{Shape: shape, Opts: ConvertOpts{Label: "AV\nÁgj", FontFamily: "Source Sans Pro"}, OffWX: float64(len(parts)) * 250})
	}
	if strings.Count(RenderComposite(parts), `<style data-isotopo-font=`) != 1 {
		t.Fatal("font resource duplicated across shapes")
	}
}
