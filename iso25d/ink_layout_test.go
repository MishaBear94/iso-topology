package iso25d

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

func TestInkLayoutShallowFaceKeepsReadableFont(t *testing.T) {
	for _, tc := range []struct {
		name, label string
		depth       float64
		icon        bool
	}{
		{"short-row", "Service", 30, true},
		{"short-label", "Service", 30, false},
		{"high-accent-row", "Ǘgj", 34, true},
		{"high-accent-label", "Ǘgj", 34, false},
		{"descending-then-accent", "gyp\nÁgj", 50, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := ConvertOpts{Width: 140, Depth: tc.depth, Height: 18, Label: tc.label, FontFamily: "Source Sans Pro", FontSize: 18, IconScale: .4}
			if tc.icon {
				o.Icon = "icon"
			}
			c := ShapeContent("rectangle", o)
			if c.EstimatedText || c.Fit.Status != "estimated-fit" {
				t.Fatalf("ink did not fit: %+v", c.Fit)
			}
			for _, it := range c.Fit.Items {
				if it.Kind == "text" && it.FontSize != 18 {
					t.Fatalf("readable 18px layout lost: %+v", c.Fit)
				}
			}
			actual := svgContent(t, Convert2DTo25D("rectangle", o))
			if len(actual) != len(c.Elements) {
				t.Fatal("missing emitted content")
			}
			ox, oy := PartOriginOffset("rectangle", o)
			for i, p := range actual {
				p.Anchor[0] -= ox
				p.Anchor[1] -= oy
				want := c.Elements[i]
				if p.Text != want.Text || p.FontSize != want.FontSize || math.Hypot(p.Anchor[0]-want.Anchor[0], p.Anchor[1]-want.Anchor[1]) > .012 {
					t.Fatalf("SVG differs from selected placement: %+v / %+v", p, want)
				}
			}
		})
	}
}

func TestInkLayoutMatrixPreservesFeasibleContent(t *testing.T) {
	labels := []string{"Service", "Ǘgj", "AV office office", "É\ngyp", "gyp\nÁgj", "Á\n\ngj"}
	for _, weight := range []string{"400", "600", "700"} {
		for _, w := range []float64{40, 70, 95, 140} {
			for _, d := range []float64{18, 24, 30, 34, 50, 80} {
				for _, hasIcon := range []bool{false, true} {
					for j, label := range labels {
						t.Run(fmt.Sprintf("%s/w%g/d%g/icon%t/label%d", weight, w, d, hasIcon, j), func(t *testing.T) {
							f := textmetrics.ResolveBuiltin("Source Sans Pro", weight)
							icon := ""
							if hasIcon {
								icon = "icon"
							}
							old := layoutTopContentLegacy(w, d, label, nil, icon, .4, "", 0, 0, 18, f)
							got := layoutTopContentFont(w, d, label, nil, icon, .4, "", 0, 0, 18, f)
							a, b := measureTopContent(w, d, old, false, true), measureTopContent(w, d, got, false, true)
							if a.Status == "estimated-fit" && (b.Status != "estimated-fit" || placementFont(got) < placementFont(old)-.01 || placementIcon(got) < placementIcon(old)-.01) {
								t.Fatalf("feasible layout regressed: old=%+v new=%+v", a, b)
							}
							if placementFont(got) > 18 || placementFont(got) < 5 {
								t.Fatalf("font outside authored/floor bounds: %g", placementFont(got))
							}
							if hasIcon && (placementIcon(got) <= 0 || placementIcon(got) > math.Min(w, d)*.4+.0051) {
								t.Fatal("icon grew past request or vanished")
							}
							for _, p := range got {
								if math.IsNaN(p.X+p.Y+p.FontSize+p.W+p.H) || math.IsInf(p.X+p.Y+p.FontSize+p.W+p.H, 0) {
									t.Fatal("invalid geometry")
								}
							}
							// The labels contain only spaces as legal word separators. Compare
							// characters independently of line/word separator choices.
							var actual strings.Builder
							for _, p := range got {
								if !p.Icon {
									actual.WriteString(p.Text)
								}
							}
							strip := func(s string) string { return strings.NewReplacer(" ", "", "\n", "").Replace(s) }
							if strip(actual.String()) != strip(label) {
								t.Fatalf("text identity lost: %q -> %q", label, actual.String())
							}
							if label == "Á\n\ngj" && (len(got) < 3 || got[len(got)-2].Text != "") {
								t.Fatal("authored blank line lost")
							}
						})
					}
				}
			}
		}
	}
}

func TestInkLayoutPreservesManualAndFallback(t *testing.T) {
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "600")
	for _, tc := range []struct {
		label, icon, anchor string
		x, y                float64
	}{
		{"Service", "icon", "topLeft", .3, -.1}, {"Service", "icon", "center", 0, 0},
		{"中文", "icon", "", 0, 0}, {"👩🏽‍💻", "", "", 0, 0}, {"A\tB", "icon", "", 0, 0},
	} {
		a := layoutTopContentLegacy(100, 40, tc.label, nil, tc.icon, .4, tc.anchor, tc.x, tc.y, 18, f)
		b := layoutTopContentFont(100, 40, tc.label, nil, tc.icon, .4, tc.anchor, tc.x, tc.y, 18, f)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("manual/fallback layout changed: %+v", tc)
		}
	}
}

func TestInkGapObservation(t *testing.T) {
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "600")
	items := []contentPlacement{{Text: "gyp", X: 40, Y: 30, FontSize: 18, Font: f}, {Text: "Ǘgj", X: 40, Y: 51.6, FontSize: 18, Font: f}}
	got := measureTopContent(80, 90, items, false, true)
	if len(got.Gaps) != 1 || got.Gaps[0].Kind != "text:text" || got.Gaps[0].Actual >= 0 || got.Status != "estimated-overflow" {
		t.Fatalf("overlapping ink passed: %+v", got)
	}
	for _, it := range got.Items {
		for _, v := range it.Overflow {
			if v != 0 {
				t.Fatal("fixture must isolate interline overlap")
			}
		}
	}
	items[1].Y = 60
	if got := measureTopContent(80, 90, items, false, true); got.Status != "estimated-fit" || len(got.Gaps) != 0 {
		t.Fatalf("cleared lines still flagged: %+v", got)
	}
	items[0].Font = nil
	items[1].Font = nil
	items[1].Y = 51.6
	if got := measureTopContent(80, 90, items, false, true); len(got.Gaps) > 0 {
		t.Fatal("unbound font gap was certified")
	}
}

func BenchmarkInkLayout(b *testing.B) {
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "600")
	for _, tc := range []struct {
		name, label string
		w, d        float64
	}{
		{"fits", "Service", 140, 100}, {"shallow", "Service", 140, 30}, {"wrap", "Customer Facing Dashboards", 70, 60}, {"dense", strings.Repeat("Service ", 20), 40, 30},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				layoutTopContentFont(tc.w, tc.d, tc.label, nil, "icon", .4, "", 0, 0, 18, f)
			}
		})
	}
}
