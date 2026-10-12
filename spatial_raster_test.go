package isotopo

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"os/exec"
	"testing"
	"time"
)

// Optional independent compositor check. The analytical tests always run;
// resvg checks the antialiasing/clip semantics that geometry alone cannot.
func spatialRaster(t *testing.T, svg string, scale float64) image.Image {
	t.Helper()
	binary, err := exec.LookPath("resvg")
	if err != nil {
		t.Skip("resvg not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--skip-system-fonts", "--zoom", fmt.Sprint(scale), "-", "-c")
	cmd.Stdin = bytes.NewBufferString(svg)
	var errors bytes.Buffer
	cmd.Stderr = &errors
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("resvg: %v %s", err, errors.String())
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return im
}

func TestSpatialUnoccludedStrokeRasterParity(t *testing.T) {
	for _, width := range []float64{1.4, 4, 12} {
		for _, scale := range []float64{.25, 1, 4} {
			t.Run(fmt.Sprintf("width_%g_scale_%g", width, scale), func(t *testing.T) {
				line := spatialLine{a: [2]float64{20, 20}, b: [2]float64{380, 220}, t1: 1}
				region, ok := spatialStrokeClip([]spatialLine{line}, nil, width)
				if !ok {
					t.Fatal("budget")
				}
				path := fmt.Sprintf(`<path d="M 20 20 L 380 220" fill="none" stroke="#603090" stroke-width="%g" stroke-dasharray="8 4" stroke-linecap="round"/>`, width)
				head := `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="240"><rect width="400" height="240" fill="white"/>`
				// Neutral clipping has the same offscreen compositing step.
				// Independently bound its unavoidable 8-bit rounding against
				// the unclipped stroke before using it as the clip-only oracle.
				neutral := `<defs><clipPath id="neutral"><path d="M -10,-10 L 410,-10 L 410,250 L -10,250 Z"/></clipPath></defs><g clip-path="url(#neutral)">` + path + `</g>`
				original := spatialRaster(t, head+path+`</svg>`, scale)
				want := spatialRaster(t, head+neutral+`</svg>`, scale)
				got := spatialRaster(t, head+`<defs>`+spatialClipMarkup("c", region, [2]float64{})+`</defs><g clip-path="url(#c)">`+path+`</g></svg>`, scale)
				changed := 0
				worst := 0
				for y := want.Bounds().Min.Y; y < want.Bounds().Max.Y; y++ {
					for x := want.Bounds().Min.X; x < want.Bounds().Max.X; x++ {
						if spatialPixelDelta(original, want, x, y) > 2 {
							t.Fatal("neutral compositor exceeded 2/255 rounding bound")
						}
						a, b, c, _ := want.At(x, y).RGBA()
						d, e, f, _ := got.At(x, y).RGBA()
						diff := max(absInt(int(a)-int(d)), absInt(int(b)-int(e)), absInt(int(c)-int(f))) / 257
						if diff > 1 {
							changed++
						}
						worst = max(worst, diff)
					}
				}
				if changed > 0 {
					t.Fatalf("unobstructed clip changed %d pixels, max channel delta=%d", changed, worst)
				}
			})
		}
	}
}

func spatialPixelDelta(a, b image.Image, x, y int) int {
	r, g, bb, aa := a.At(x, y).RGBA()
	rr, gg, bbb, aaa := b.At(x, y).RGBA()
	return max(absInt(int(r)-int(rr)), absInt(int(g)-int(gg)), absInt(int(bb)-int(bbb)), absInt(int(aa)-int(aaa))) / 257
}

func TestSpatialClipHasNoSubdivisionSeams(t *testing.T) {
	// Painter ordering is an independent reference for these simple opaque
	// foreground polygons. Away from their AA boundary, the clipped stroke
	// must be indistinguishable, including shared internal clip edges.
	line := spatialLine{a: [2]float64{20, 20}, b: [2]float64{380, 220}, t1: 1}
	polys := [][][2]float64{{{145, 40}, {190, 40}, {190, 165}, {145, 165}}, {{225, 115}, {270, 125}, {245, 205}}}
	var surfaces []spatialPiece
	var faces string
	for _, poly := range polys {
		surfaces = append(surfaces, spatialPiece{poly: poly, depth: depthPlane{0, 0, 10}})
		d := ""
		for j, p := range poly {
			op := "L"
			if j == 0 {
				op = "M"
			}
			d += fmt.Sprintf("%s %g,%g ", op, p[0], p[1])
		}
		faces += `<path fill="#ddd" d="` + d + `Z"/>`
	}
	for _, scale := range []float64{.25, 1, 4} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			region, ok := spatialStrokeClip([]spatialLine{line}, surfaces, 4)
			if !ok {
				t.Fatal("budget")
			}
			head := `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="240"><rect width="400" height="240" fill="white"/>`
			path := `<path d="M 20,20 L 380,220" fill="none" stroke="#603090" stroke-width="4" stroke-dasharray="8 4" stroke-linecap="round"/>`
			neutral := `<defs><clipPath id="neutral"><path d="M -10,-10 L 410,-10 L 410,250 L -10,250 Z"/></clipPath></defs><g clip-path="url(#neutral)">` + path + `</g>`
			want := spatialRaster(t, head+neutral+faces+`</svg>`, scale)
			got := spatialRaster(t, head+faces+`<defs>`+spatialClipMarkup("clip", region, [2]float64{})+`</defs><g clip-path="url(#clip)">`+path+`</g></svg>`, scale)
			for y := 0; y < want.Bounds().Max.Y; y++ {
				for x := 0; x < want.Bounds().Max.X; x++ {
					p := [2]float64{(float64(x) + .5) / scale, (float64(y) + .5) / scale}
					boundary := false
					for _, poly := range polys {
						for j, a := range poly {
							b := poly[(j+1)%len(poly)]
							dx, dy := b[0]-a[0], b[1]-a[1]
							u := math.Max(0, math.Min(1, ((p[0]-a[0])*dx+(p[1]-a[1])*dy)/(dx*dx+dy*dy)))
							if math.Hypot(p[0]-a[0]-u*dx, p[1]-a[1]-u*dy) < 1.5/scale {
								boundary = true
							}
						}
					}
					if !boundary && spatialPixelDelta(want, got, x, y) > 1 {
						t.Fatalf("clip seam at pixel (%d,%d), scale=%v delta=%d", x, y, scale, spatialPixelDelta(want, got, x, y))
					}
				}
			}
		})
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
