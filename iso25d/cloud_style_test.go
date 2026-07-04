// Tests for the cloud's style surface: top-face icon (box-family anchor/scale
// semantics on the inscribed content rect) and TopGradient/LeftGradient fills.
// The cloud used to silently drop both (icon was hard-passed as "" and the
// gradient fields were never read).
package iso25d

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func cloudOpts() IsoBoxOpts {
	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = 200, 140, 24
	return o
}

// Icon set → an <image> is emitted; icon empty → none (legacy path).
func TestCloudIconEmitted(t *testing.T) {
	o := cloudOpts()
	o.Icon = "data:image/svg+xml;base64,abc"
	svg := RenderIsoCloud(o)
	if !strings.Contains(svg, `<image href="data:image/svg+xml;base64,abc"`) {
		t.Fatalf("cloud must render its icon, got no <image>:\n%s", svg)
	}

	o.Icon = ""
	if strings.Contains(RenderIsoCloud(o), "<image") {
		t.Fatal("icon-less cloud must not emit an <image>")
	}
}

// Icon + label coexist: both the <image> and the <text> are emitted inside a
// single top-content group, like the box family.
func TestCloudIconAndLabelCoexist(t *testing.T) {
	o := cloudOpts()
	o.Icon = "data:image/svg+xml;base64,abc"
	o.Label = "GCP"
	svg := RenderIsoCloud(o)
	if !strings.Contains(svg, "<image") {
		t.Fatal("icon missing when label is present")
	}
	if !strings.Contains(svg, ">GCP</text>") {
		t.Fatal("label missing when icon is present")
	}
	if !strings.Contains(svg, `data-face="top-content"`) {
		t.Fatal("icon+label must ride the top-content group")
	}
}

// TopGradient paints the crown, LeftGradient the side band; nil gradients keep
// the solid fills (no defs at all).
func TestCloudGradients(t *testing.T) {
	o := cloudOpts()
	o.TopGradient = &FaceGradient{From: "#7DD3FC", To: "#0EA5E9", Dir: "down"}
	o.LeftGradient = &FaceGradient{From: "#0369A1", To: "#0C4A6E"}
	svg := RenderIsoCloud(o)

	if !strings.Contains(svg, `<linearGradient id="cloud-grad-top"`) ||
		!strings.Contains(svg, `<linearGradient id="cloud-grad-side"`) {
		t.Fatalf("gradient defs missing:\n%s", svg)
	}
	if !strings.Contains(svg, `data-face="top" d=`) || !strings.Contains(svg, `fill="url(#cloud-grad-top)"`) {
		t.Fatal("top face must reference cloud-grad-top")
	}
	if !strings.Contains(svg, `data-face="side" points=`) || !strings.Contains(svg, `fill="url(#cloud-grad-side)"`) {
		t.Fatal("side band must reference cloud-grad-side")
	}

	o.TopGradient, o.LeftGradient = nil, nil
	plain := RenderIsoCloud(o)
	if strings.Contains(plain, "<defs>") || strings.Contains(plain, "url(#") {
		t.Fatal("gradient-less cloud must not emit defs or url() fills")
	}
	if !strings.Contains(plain, `fill="`+o.TopFill+`"`) {
		t.Fatal("gradient-less cloud must keep the solid TopFill")
	}
}

// Same input → same output (no hidden nondeterminism in the new paths).
func TestCloudStyleDeterministic(t *testing.T) {
	o := cloudOpts()
	o.Icon = "data:image/svg+xml;base64,abc"
	o.Label = "SaaS"
	o.TopGradient = &FaceGradient{From: "#7DD3FC", To: "#0EA5E9"}
	o.LeftGradient = &FaceGradient{From: "#0369A1", To: "#0C4A6E"}
	a, b := RenderIsoCloud(o), RenderIsoCloud(o)
	if a != b {
		t.Fatal("RenderIsoCloud must be deterministic")
	}
}

// The inscribed content rect (cloudContent*) must lie fully inside the cloud
// outline — corners, edges, and interior — so an icon/label block clamped to
// it can never spill past the silhouette. Checked in the outline's own local
// frame with a NONZERO-winding point-in-polygon test against the sampled
// outline: the SVG renderer fills the top path with the default nonzero rule,
// under which the small self-intersection lenses at the circle seams are
// painted (they wind twice), so nonzero is the rule that matches what is
// actually visible.
func TestCloudContentRectInsideOutline(t *testing.T) {
	const w, d = 200.0, 140.0
	outline := sampleCloudOutline(w, d)

	inside := func(x, y float64) bool {
		wn := 0
		n := len(outline)
		for i := 0; i < n; i++ {
			x1, y1 := outline[i][0], outline[i][1]
			x2, y2 := outline[(i+1)%n][0], outline[(i+1)%n][1]
			cross := (x2-x1)*(y-y1) - (x-x1)*(y2-y1)
			if y1 <= y {
				if y2 > y && cross > 0 {
					wn++
				}
			} else if y2 <= y && cross < 0 {
				wn--
			}
		}
		return wn != 0
	}

	x0, y0 := cloudContentX0*w, cloudContentY0*d
	x1, y1 := x0+cloudContentW*w, y0+cloudContentD*d
	const K = 24
	for i := 0; i <= K; i++ {
		fx := x0 + (x1-x0)*float64(i)/K
		fy := y0 + (y1-y0)*float64(i)/K
		for _, p := range [][2]float64{{fx, y0}, {fx, y1}, {x0, fy}, {x1, fy}} {
			if !inside(p[0], p[1]) {
				t.Fatalf("content rect point (%.2f, %.2f) is outside the cloud outline", p[0], p[1])
			}
		}
	}
}

// The icon lands on the top face: its <image> x/y (local content-rect frame)
// must map, through the top-content iso transform, to a point inside the
// projected top-face bounding box.
func TestCloudIconAnchoredOnTopFace(t *testing.T) {
	o := cloudOpts()
	o.Icon = "data:image/svg+xml;base64,abc"
	svg := RenderIsoCloud(o)

	// The icon-only default scale fills most of the rect's smaller dim.
	faceW, faceD := cloudContentW*o.Width, cloudContentD*o.Depth
	wantSize := math.Min(faceW, faceD) * defaultIconOnlyScale
	if !strings.Contains(svg, fmt.Sprintf(`width="%.2f"`, wantSize)) {
		t.Fatalf("icon size must follow min(faceW,faceD)*defaultIconOnlyScale=%.2f:\n%s", wantSize, svg)
	}
}
