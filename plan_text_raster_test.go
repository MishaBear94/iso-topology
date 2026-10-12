package isotopo

import (
	"regexp"
	"testing"
)

func TestPlanTextNativeInkBounds(t *testing.T) {
	// Compare actual font rasterization with the same scene minus its titles.
	// The difference mask is independent of our geometry producer and includes
	// every emitted line, accented glyph, kerning pair and descender.
	for _, weight := range []string{"400", "600", "700", "moved"} {
		doc := planTextTestDoc("AV office\nA\u0301gypq  ", 180, 100, 23)
		doc.Scene().Parts[0].Style.Text.Weight = weight
		if weight == "600" {
			doc.Scene().Parts[0].Shape = "group"
		}
		if weight == "moved" {
			doc = planPlacementTestDoc()
		}
		scene := RenderScene(doc)
		without := regexp.MustCompile(`<text\b[^>]*>[\s\S]*?</text>`).ReplaceAllString(scene.SVG, "")
		ink, vb := connectorBodyTestRaster(t, scene.SVG, 3)
		blank, _ := connectorBodyTestRaster(t, without, 3)
		sx, sy := vb[2]/float64(ink.Bounds().Dx()), vb[3]/float64(ink.Bounds().Dy())
		count := 0
		for y := 0; y < ink.Bounds().Dy(); y++ {
			for x := 0; x < ink.Bounds().Dx(); x++ {
				a, b, c, _ := ink.At(x, y).RGBA()
				d, e, f, _ := blank.At(x, y).RGBA()
				if a == d && b == e && c == f {
					continue
				}
				count++
				wx, wy := vb[0]+(float64(x)+.5)*sx, vb[1]+(float64(y)+.5)*sy
				inside := false
				for _, line := range scene.PlanLabels[0].Lines {
					if r := line.InkBounds; r != nil && wx >= r.X-sx && wx <= r.X+r.Width+sx && wy >= r.Y-sy && wy <= r.Y+r.Height+sy {
						inside = true
						break
					}
				}
				if !inside {
					t.Fatalf("weight %s ink pixel outside recorded geometry at %.3f %.3f", weight, wx, wy)
				}
			}
		}
		if count < 100 {
			t.Fatalf("weight %s has no real glyph ink", weight)
		}
	}
}

func TestPlanTitleNativeVisibility(t *testing.T) {
	for _, kind := range []string{"horizontal", "diagonal", "thick", "arrow", "vertical", "tight", "shared", "shared-arrow"} {
		doc := planPlacementTestDoc()
		doc.Scene().Parts[0].Style.Text.Weight = "600"
		c := doc.Scene().Connectors[0]
		switch kind {
		case "shared", "shared-arrow":
			doc = planLaneTestDoc(kind == "shared-arrow")
			doc.Scene().Parts[0].Style.Text.Weight = "600"
		case "vertical":
			doc.Scene().Parts[1].Offset = &WorldPoint{WX: 40, WY: -60}
			doc.Scene().Parts[2].Offset = &WorldPoint{WX: 40, WY: 200}
		case "tight":
			doc.Scene().Parts[0].Parts[0].Offset.WY = 30
		case "diagonal":
			c.Routing = "straight"
			doc.Scene().Parts[2].Offset.WY = 75
		case "thick":
			w := 12.
			c.Stroke = &Stroke{Width: &w}
		case "arrow":
			c.Arrow = "triangle"
			doc.Scene().Parts[2].Offset.WX = 220
		}
		scene := RenderScene(doc)
		if p := scene.PlanLabels[0].Placement; p.Status != "clear-within-scope" || kind != "shared" && kind != "shared-arrow" && p.Offset == [2]float64{} {
			t.Fatalf("%s fixture did not exercise a verified move", kind)
		}
		without := regexp.MustCompile(`<text\b[^>]*>[\s\S]*?</text>`).ReplaceAllString(scene.SVG, "")
		// Remove actual drawing primitives while keeping the emitted text,
		// font resource and viewport. The resulting alpha mask independently
		// identifies glyph pixels that the submitted scene must still show.
		isolated := regexp.MustCompile(`<(rect|path)\b[^>]*/>`).ReplaceAllString(scene.SVG, "")
		full, _ := connectorBodyTestRaster(t, scene.SVG, 3)
		blank, _ := connectorBodyTestRaster(t, without, 3)
		glyph, _ := connectorBodyTestRaster(t, isolated, 3)
		covered, expected := 0, 0
		for y := 0; y < glyph.Bounds().Dy(); y++ {
			for x := 0; x < glyph.Bounds().Dx(); x++ {
				_, _, _, alpha := glyph.At(x, y).RGBA()
				if alpha < 16384 {
					continue
				}
				expected++
				a, b, c, _ := full.At(x, y).RGBA()
				d, e, f, _ := blank.At(x, y).RGBA()
				if a == d && b == e && c == f {
					covered++
				}
			}
		}
		if expected < 100 || covered != 0 {
			t.Fatalf("%s: %d of %d substantial glyph pixels hidden", kind, covered, expected)
		}
	}
}
