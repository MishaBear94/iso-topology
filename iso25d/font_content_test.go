package iso25d

import (
	"github.com/MarkovWangRR/iso-topology/textmetrics"
	"math"
	"strings"
	"testing"
)

func TestBoundFontTopLayout(t *testing.T) {
	for _, label := range []string{"iiiiiiiiiiii", "WWWWWW", "AV office", "e\u0301e\u0301e\u0301", "Αθήνα", "Москва", "A  B"} {
		t.Run(label, func(t *testing.T) {
			o := ConvertOpts{Width: 95, Depth: 80, Height: 20, Label: label, FontFamily: "Source Sans Pro", FontWeight: "600", FontSize: 18}
			c := ShapeContent("rectangle", o)
			svg := Convert2DTo25D("rectangle", o)
			if c.EstimatedText || len(c.Fit.Items) == 0 || c.Fit.Status != "estimated-fit" {
				t.Fatalf("font not measured: %+v", c)
			}
			if !strings.Contains(svg, `data-isotopo-font="`) || !strings.Contains(svg, `xml:space="preserve"`) {
				t.Fatal("font/whitespace contract missing")
			}
			for _, it := range c.Fit.Items {
				if it.TextMetrics != "shaped-ink" || it.FontHash == "" {
					t.Fatal(it)
				}
			}
			if label == "iiiiiiiiiiii" && c.Elements[0].FontSize != 18 {
				t.Fatalf("narrow glyphs unnecessarily shrunk: %+v", c.Elements)
			}
		})
	}
}

func TestBoundFontFallbackAndComposite(t *testing.T) {
	o := ConvertOpts{Width: 100, Depth: 100, Height: 20, Label: "中文 👨‍👩‍👧‍👦", FontFamily: "Source Sans Pro", FontSize: 18}
	c := ShapeContent("rectangle", o)
	if !c.EstimatedText || c.Fit.Items[0].TextMetrics != "estimated-advance" {
		t.Fatal("fallback font certified")
	}
	svg := RenderComposite([]CompositePart{{ID: "a", Shape: "rectangle", Opts: o}, {ID: "b", Shape: "rectangle", Opts: o, OffWX: 130}})
	if strings.Count(svg, `<style data-isotopo-font=`) != 1 {
		t.Fatal("repeated composite font payload")
	}
	c = ShapeContent("cylinder", ConvertOpts{Width: 120, Depth: 120, Height: 30, Label: "AV office", FontFamily: "Source Sans Pro", FontSize: 18})
	if c.EstimatedText {
		t.Fatal("cylinder font not propagated")
	}
}

func TestBoundFontWrapWholeCandidates(t *testing.T) {
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "600")
	width := func(s string, fs float64) float64 { return f.Measure(s).CenteredWidth() * fs }
	for _, s := range []string{"AVAVAVAVAV", "office office office", "e\u0301e\u0301e\u0301e\u0301", "A  B  C"} {
		for _, maxW := range []float64{15, 25, 40} {
			lines := wrapLine(s, 18, maxW, width)
			for _, line := range lines {
				if width(line, 18) > maxW+.0001 && len(textClusters(line)) > 1 {
					t.Fatalf("candidate overflows %q, width %g, max %g", line, width(line, 18), maxW)
				}
			}
		}
	}
	m := f.Measure("Ágj")
	p := contentPlacement{Text: "Ágj", X: 60, Y: 40, FontSize: 18, Font: f}
	x, y, w, h := contentLocalBounds(p, .35, .58, "middle")
	if math.Abs(x-(60+(m.Left-m.Advance/2)*18)) > 1e-9 || math.Abs(y-(40+(.35+m.Top)*18)) > 1e-9 || w <= 0 || h <= 0 {
		t.Fatal("ink placement mismatch")
	}
}

func TestBoundFontResourceOnlyWhenUsed(t *testing.T) {
	o := ConvertOpts{Width: 80, Depth: 80, Height: 20, FontFamily: "Source Sans Pro", Icon: ResolveBrandIcon("iso://glyph/database")}
	if strings.Contains(Convert2DTo25D("rectangle", o), `data-isotopo-font=`) {
		t.Fatal("icon-only shape embedded an unused font")
	}
	o.Label = "office"
	var parts []CompositePart
	for i := 0; i < 40; i++ {
		parts = append(parts, CompositePart{Shape: "rectangle", Opts: o, OffWX: float64(i) * 100})
	}
	svg := RenderComposite(parts)
	if strings.Count(svg, `<style data-isotopo-font=`) != 1 {
		t.Fatal("repeated scene font")
	}
	if len(svg) > len(textmetrics.ResolveBuiltin("Source Sans Pro", "600").CSS())+40*4000 {
		t.Fatal("font payload grew per part")
	}
}
