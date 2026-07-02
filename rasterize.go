package isotopo

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

// Rasterize converts an SVG file to a PNG deterministically and WITHOUT
// cropping: the output keeps the SVG's intrinsic width/height 1:1, so what the
// image shows is exactly what the SVG says — the property that makes visual
// self-review trustworthy for agents ("look at the PNG" only works if the PNG
// cannot lie about the geometry).
//
// Rasterizer discovery, in order:
//  1. $ISOTOPO_RASTERIZER — explicit binary; invoked as `<bin> in.svg out.png`
//     (resvg-compatible convention);
//  2. resvg (faithful text + gradients — the recommended install);
//  3. ImageMagick `magick` (no -trim, full viewBox);
//  4. headless Chrome/Chromium — found via $CHROME_PATH, common binary names,
//     or a Playwright browsers dir ($PLAYWRIGHT_BROWSERS_PATH) — so machines
//     with only a browser installed still get pixels.
//
// The error on total failure enumerates every option, because "snapshot needs
// a rasterizer" with no next step is exactly the friction this exists to kill.
func Rasterize(svgPath, pngPath string) error {
	if bin := os.Getenv("ISOTOPO_RASTERIZER"); bin != "" {
		return exec.Command(bin, svgPath, pngPath).Run()
	}
	if p, err := exec.LookPath("resvg"); err == nil {
		return exec.Command(p, svgPath, pngPath).Run()
	}
	if p, err := exec.LookPath("magick"); err == nil {
		return exec.Command(p, "-background", "none", svgPath, pngPath).Run()
	}
	if p := chromeBinary(); p != "" {
		return chromeScreenshot(p, svgPath, pngPath)
	}
	return fmt.Errorf("no rasterizer found — install resvg (recommended) or ImageMagick, " +
		"have Chrome/Chromium on PATH (or set $CHROME_PATH / $PLAYWRIGHT_BROWSERS_PATH), " +
		"or point $ISOTOPO_RASTERIZER at any binary invocable as `<bin> in.svg out.png`")
}

// chromeBinary locates a Chrome/Chromium executable.
func chromeBinary() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	// Playwright-managed browsers (PLAYWRIGHT_BROWSERS_PATH layout).
	if root := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); root != "" {
		if matches, _ := filepath.Glob(filepath.Join(root, "chromium-*", "chrome-linux", "chrome")); len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

// chromeUISlack is extra window height requested from headless Chrome. New
// headless mode treats --window-size as the WINDOW size and reserves a fixed
// band (87px on Chromium 141) for browser UI, so the viewport comes out that
// much shorter — the bottom of the SVG gets clipped and --screenshot pads the
// image back to full window height with white. Requesting generous slack and
// cropping the PNG back to the SVG's intrinsic size sidesteps the reservation
// without depending on its exact, version-specific value.
const chromeUISlack = 240

// chromeScreenshot renders the SVG in headless Chrome at its intrinsic size.
func chromeScreenshot(chrome, svgPath, pngPath string) error {
	w, h, err := svgIntrinsicSize(svgPath)
	if err != nil {
		return fmt.Errorf("rasterize: %w", err)
	}
	abs, err := filepath.Abs(svgPath)
	if err != nil {
		return err
	}
	if err := exec.Command(chrome,
		"--headless", "--disable-gpu", "--no-sandbox", "--hide-scrollbars",
		"--force-device-scale-factor=1",
		fmt.Sprintf("--window-size=%d,%d", w+chromeUISlack, h+chromeUISlack),
		"--screenshot="+pngPath,
		"file://"+abs,
	).Run(); err != nil {
		return err
	}
	return cropPNG(pngPath, w, h)
}

// cropPNG rewrites the PNG at path to its top-left w×h region, discarding the
// white padding a slack-sized Chrome window leaves right of / below the SVG.
func cropPNG(path string, w, h int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("rasterize: decode screenshot: %w", err)
	}
	b := img.Bounds()
	if b.Dx() < w || b.Dy() < h {
		return fmt.Errorf("rasterize: chrome screenshot is %dx%d, smaller than the svg's %dx%d — cannot crop", b.Dx(), b.Dy(), w, h)
	}
	if b.Dx() == w && b.Dy() == h {
		return nil
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return fmt.Errorf("rasterize: screenshot image type %T does not support cropping", img)
	}
	cropped := sub.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Min.X+w, b.Min.Y+h))
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(out, cropped); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var reSVGDims = regexp.MustCompile(`<svg[^>]*\swidth="([0-9.]+)"[^>]*\sheight="([0-9.]+)"`)

// svgIntrinsicSize reads the root width/height attributes (integers by the
// renderer's ceilOuterDims contract; ceil defensively anyway).
func svgIntrinsicSize(path string) (w, h int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	head := data
	if len(head) > 2048 {
		head = head[:2048]
	}
	m := reSVGDims.FindSubmatch(head)
	if m == nil {
		return 0, 0, fmt.Errorf("no width/height on the root <svg> of %s", path)
	}
	fw, _ := strconv.ParseFloat(string(m[1]), 64)
	fh, _ := strconv.ParseFloat(string(m[2]), 64)
	if fw <= 0 || fh <= 0 {
		return 0, 0, fmt.Errorf("degenerate svg dimensions %gx%g in %s", fw, fh, path)
	}
	return int(fw + 0.999), int(fh + 0.999), nil
}
