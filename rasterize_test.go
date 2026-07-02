package isotopo

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSVGIntrinsicSize: the chrome fallback screenshots at the SVG's declared
// size — reading it wrong would crop or letterbox the capture.
func TestSVGIntrinsicSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.svg")
	if err := os.WriteFile(p, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="-41 -12 582 364" width="582" height="364"></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	w, h, err := svgIntrinsicSize(p)
	if err != nil || w != 582 || h != 364 {
		t.Fatalf("got %dx%d err=%v; want 582x364", w, h, err)
	}
	if err := os.WriteFile(p, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svgIntrinsicSize(p); err == nil {
		t.Error("dimensionless svg must error, not guess")
	}
}

// TestRasterizeEndToEnd renders a real scene and rasterizes it with whatever
// backend this machine has (resvg / magick / chrome); skipped when none exists
// — but the error must then enumerate every install option, because a dead-end
// error is the exact friction Rasterize exists to remove.
func TestRasterizeEndToEnd(t *testing.T) {
	doc, err := Parse([]byte(`
nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, label: "A" }
`))
	if err != nil {
		t.Fatal(err)
	}
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
	dir := t.TempDir()
	svgPath := filepath.Join(dir, "t.svg")
	pngPath := filepath.Join(dir, "t.png")
	if err := os.WriteFile(svgPath, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Rasterize(svgPath, pngPath); err != nil {
		for _, opt := range []string{"resvg", "ImageMagick", "Chrome", "ISOTOPO_RASTERIZER"} {
			if !strings.Contains(err.Error(), opt) {
				t.Errorf("failure message must mention %s; got: %v", opt, err)
			}
		}
		t.Skipf("no rasterizer on this machine: %v", err)
	}
	info, err := os.Stat(pngPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("rasterize produced no png: %v", err)
	}
	// PNG magic.
	b, _ := os.ReadFile(pngPath)
	if len(b) < 8 || string(b[1:4]) != "PNG" {
		t.Error("output is not a png")
	}
}

// TestChromeScreenshotNoWhiteStrip pins the regression that shipped white-
// bottomed README images: new headless Chrome reserves a fixed UI band from
// --window-size, so screenshotting at the SVG's intrinsic size clipped the
// bottom of the scene and padded it back with white. The fix (slack window +
// crop) must yield a PNG that is exactly the SVG's size with canvas-coloured
// corners on a dark-canvas scene. Skipped when no Chrome is on this machine.
func TestChromeScreenshotNoWhiteStrip(t *testing.T) {
	chrome := chromeBinary()
	if chrome == "" {
		t.Skip("no chrome/chromium on this machine")
	}
	doc, err := Parse([]byte(`
canvas: { background: "#0b0b0e" }
nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, label: "A" }
`))
	if err != nil {
		t.Fatal(err)
	}
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
	dir := t.TempDir()
	svgPath := filepath.Join(dir, "t.svg")
	pngPath := filepath.Join(dir, "t.png")
	if err := os.WriteFile(svgPath, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := chromeScreenshot(chrome, svgPath, pngPath); err != nil {
		t.Fatal(err)
	}
	w, h, err := svgIntrinsicSize(svgPath)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
		t.Fatalf("png is %dx%d, svg is %dx%d — crop contract broken", img.Bounds().Dx(), img.Bounds().Dy(), w, h)
	}
	for _, c := range [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}} {
		r, g, b, _ := img.At(img.Bounds().Min.X+c[0], img.Bounds().Min.Y+c[1]).RGBA()
		if r>>8 > 0x40 || g>>8 > 0x40 || b>>8 > 0x40 {
			t.Errorf("corner (%d,%d) = #%02x%02x%02x — not the dark canvas; white padding leaked into the capture",
				c[0], c[1], r>>8, g>>8, b>>8)
		}
	}
}
