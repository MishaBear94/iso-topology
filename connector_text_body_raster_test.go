package isotopo

import (
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// Run the real SVG/font compositor independently of the geometry producer.
// The geometry tests also run on WASM; resvg is optional on that platform.
func connectorBodyTestRaster(t *testing.T, svg string, zoom float64) (image.Image, [4]float64) {
	t.Helper()
	bin, err := exec.LookPath("resvg")
	if err != nil {
		t.Skip("resvg not installed")
	}
	var root struct {
		Width   string `xml:"width,attr"`
		Height  string `xml:"height,attr"`
		ViewBox string `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal([]byte(svg), &root); err != nil {
		t.Fatal(err)
	}
	w, _ := strconv.ParseFloat(root.Width, 64)
	h, _ := strconv.ParseFloat(root.Height, 64)
	var vb [4]float64
	if _, err := fmt.Sscan(root.ViewBox, &vb[0], &vb[1], &vb[2], &vb[3]); err != nil {
		t.Fatal(err)
	}
	svg = strings.Replace(svg, `width="`+root.Width+`"`, fmt.Sprintf(`width="%g"`, w*zoom), 1)
	svg = strings.Replace(svg, `height="`+root.Height+`"`, fmt.Sprintf(`height="%g"`, h*zoom), 1)
	dir := t.TempDir()
	in, out := filepath.Join(dir, "scene.svg"), filepath.Join(dir, "scene.png")
	if err := os.WriteFile(in, []byte(svg), 0600); err != nil {
		t.Fatal(err)
	}
	if err := rasterizeResvg(bin, in, out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return im, vb
}

func TestConnectorTextBodyNativeLabelClearance(t *testing.T) {
	for _, kind := range []string{"plain", "bound", "multiline", "stack", "thick"} {
		t.Run(kind, func(t *testing.T) {
			doc := connectorTextBodyRegressionDoc()
			switch kind {
			case "bound", "multiline":
				for i, p := range doc.Scene().Parts {
					p.Label = fmt.Sprintf("Node %d", i)
					if kind == "multiline" {
						p.Label += "\nAV office"
					}
					p.Style = &Style{Text: &Text{Family: "Source Sans Pro"}}
				}
			case "stack":
				doc.Scene().Parts[4].Stack = &Stack{Count: 3, Gap: 35}
			case "thick":
				w := 12.
				for _, p := range doc.Scene().Parts {
					p.Style = &Style{Stroke: &Stroke{Width: &w}}
				}
			}
			scene := RenderScene(doc)
			bodies := cloneDocParts(doc)
			bodies.Scene().Connectors = nil
			im, vb := connectorBodyTestRaster(t, RenderScene(bodies).SVG, 4)
			sx, sy := vb[2]/float64(im.Bounds().Dx()), vb[3]/float64(im.Bounds().Dy())
			checked := 0
			for _, record := range scene.ConnectorLabels {
				if !strings.Contains(record.Label.Typography.Scope, "body-region") {
					continue
				}
				checked++
				r := record.Label.Rect
				// Test the final pill plus almost the full 6-unit reading gap
				// against real node pixels. Half a unit accommodates raster AA.
				x0, x1 := r[0]-5.5, r[0]+r[2]+5.5
				y0, y1 := r[1]-5.5, r[1]+r[3]+5.5
				pixels := 0
				for y := max(0, int(math.Ceil((y0-vb[1])/sy-.5))); y < im.Bounds().Dy(); y++ {
					if vb[1]+(float64(y)+.5)*sy >= y1 {
						break
					}
					for x := max(0, int(math.Ceil((x0-vb[0])/sx-.5))); x < im.Bounds().Dx(); x++ {
						if vb[0]+(float64(x)+.5)*sx >= x1 {
							break
						}
						pixels++
						_, _, _, a := im.At(x, y).RGBA()
						if a > 257*4 {
							t.Fatalf("new pill/gap covers node pixel (%d,%d), alpha=%d", x, y, a/257)
						}
					}
				}
				if pixels < 100 {
					t.Fatal("pill outside raster oracle")
				}
			}
			if checked != 1 {
				t.Fatalf("checked %d new pills, want 1", checked)
			}
		})
	}
}

func TestConnectorTextBodyNativeCoverage(t *testing.T) {
	icon := iso25d.ResolveBrandIcon("iso://glyph/database")
	var cases []iso25d.CompositePart
	for _, shape := range []string{"rectangle", "cylinder", "circle", "person", "triprism", "hexprism", "octprism", "wedge", "custom_path", "cloud"} {
		for _, width := range []float64{1.5, 14} {
			o := iso25d.ConvertOpts{Width: 120, Depth: 90, Height: 35, StrokeWidth: width, OutlineColor: "#456", OutlineWidth: width + 2}
			if shape == "custom_path" {
				o.Params = map[string]any{"path": "M 0 0 L 120 0 L 120 30 L 40 30 L 40 60 L 120 60 L 120 90 L 0 90 Z"}
			}
			cases = append(cases, iso25d.CompositePart{Shape: shape, Opts: o})
		}
	}
	for _, shape := range []string{"rectangle", "cylinder", "triprism", "hexprism", "octprism", "wedge", "custom_path", "cloud"} {
		o := iso25d.ConvertOpts{Width: 120, Depth: 90, Height: 35, Label: "Ágj AV office\nǗff", FontFamily: "Source Sans Pro", FontWeight: "700", FontSize: 24, Icon: icon, IconAnchor: "topLeft", IconOffX: 1.2, IconOffY: -.7}
		o.TopStroke.Width = 10
		o.LeftStroke.Width = 7
		o.RightStroke.Width = 18
		cases = append(cases, iso25d.CompositePart{Shape: shape, Opts: o})
	}
	cases = append(cases, iso25d.CompositePart{Shape: "rectangle", Opts: iso25d.ConvertOpts{Width: 120, Depth: 90, Height: 35, CornerRadius: 22, FaceSplit: true, StrokeWidth: 12, OutlineColor: "#456", OutlineWidth: 16}})
	cases = append(cases, iso25d.CompositePart{Shape: "rectangle", Opts: iso25d.ConvertOpts{Width: 120, Depth: 90, Height: 35, Label: strings.Repeat("W", 35), FontFamily: "Source Sans Pro", FontSize: 13.236, Icon: icon, IconAnchor: "topLeft"}})
	for _, shape := range []string{"person", "circle", "cloud"} {
		for _, weight := range []string{"400", "600", "700"} {
			for _, label := range []string{"j" + strings.Repeat("W", 35), "Ágj AV office\n\nǗff e\u0301\nМосква"} {
				o := iso25d.ConvertOpts{Width: 80, Depth: 115, Height: 27, Label: label, FontFamily: "Source Sans Pro", FontWeight: weight, FontSize: 13.236}
				cases = append(cases, iso25d.CompositePart{Shape: shape, Opts: o})
				o.Icon = icon
				cases = append(cases, iso25d.CompositePart{Shape: shape, Opts: o})
			}
		}
	}
	for i, p := range cases {
		t.Run(fmt.Sprintf("%02d-%s", i, p.Shape), func(t *testing.T) {
			p.OffWX, p.OffWY, p.OffWZ = -63.237, 170.489, 28.993
			parts := []iso25d.CompositePart{p}
			region, ok := connectorTextBodyRegions(parts, connectorBodyTestInfos(parts))
			if !ok {
				t.Fatal("supported shape/content rejected")
			}
			im, vb := connectorBodyTestRaster(t, iso25d.RenderComposite(parts), 2)
			// Include one pixel of raster AA. Distance-to-segment is separate
			// from the production slab test. sqrt(2) covers square dilation.
			sx, sy := vb[2]/float64(im.Bounds().Dx()), vb[3]/float64(im.Bounds().Dy())
			painted := 0
			for y := 0; y < im.Bounds().Dy(); y++ {
				for x := 0; x < im.Bounds().Dx(); x++ {
					_, _, _, a := im.At(x, y).RGBA()
					if a < 257*4 {
						continue
					}
					painted++
					px, py := vb[0]+(float64(x)+.5)*sx, vb[1]+(float64(y)+.5)*sy
					if connectorTextInteriorHits(screenRect{px, py, px, py}, region.interiors) {
						continue
					}
					inside := false
					for _, s := range region.borders {
						dx, dy := s.b[0]-s.a[0], s.b[1]-s.a[1]
						u := 0.
						if dx*dx+dy*dy > 0 {
							u = math.Max(0, math.Min(1, ((px-s.a[0])*dx+(py-s.a[1])*dy)/(dx*dx+dy*dy)))
						}
						if math.Hypot(px-s.a[0]-u*dx, py-s.a[1]-u*dy) <= s.radius*math.Sqrt2+math.Max(sx, sy) {
							inside = true
							break
						}
					}
					if !inside {
						t.Fatalf("actual SVG pixel escapes protected region at (%g,%g), alpha=%d", px, py, a/257)
					}
				}
			}
			if painted < 100 {
				t.Fatal("empty raster oracle")
			}
		})
	}
}
