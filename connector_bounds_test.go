package isotopo

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectorStrokeBoundsAnalytic(t *testing.T) {
	// y(t)=-600t+640t², x(t)=400t-380t²: independently solved extrema.
	for _, shift := range []float64{0, 9e6} {
		p := [][2]float64{{shift, shift}, {shift + 200, shift - 300}, {shift + 20, shift + 40}}
		got := connectorStrokeBounds(p, true, 8)
		want := minSvgRect{shift - 5, shift - 145.625, shift + 2000./19 + 5, shift + 45}
		for i, v := range []float64{got.minX, got.minY, got.maxX, got.maxY} {
			w := []float64{want.minX, want.minY, want.maxX, want.maxY}[i]
			if math.Abs(v-w) > 1e-7 {
				t.Fatalf("analytic bound got %+v, want %+v", got, want)
			}
		}
	}
}

// Place each complete export into the same world-aligned viewport. The nested
// SVG retains its real clipping boundary. Enlarging only its viewport supplies
// an independent raster reference without changing any drawing commands.
func connectorRasterFrame(t *testing.T, svg string, expanded bool) string {
	t.Helper()
	root, err := findRootSVG(svg)
	if err != nil {
		t.Fatal(err)
	}
	x, y, w, h, err := parseViewBox(extractAttr(root.tag(), "viewBox"))
	if err != nil {
		t.Fatal(err)
	}
	ox, oy := math.Floor(x)-200, math.Floor(y)-200
	ow, oh := math.Ceil(x+w)-ox+200, math.Ceil(y+h)-oy+200
	if expanded {
		svg = growViewBoxAround(svg, minSvgRect{ox, oy, ox + ow, oy + oh})
		x, y = ox, oy
	}
	root, err = findRootSVG(svg)
	if err != nil {
		t.Fatal(err)
	}
	tag := strings.Replace(root.tag(), "<svg", fmt.Sprintf(`<svg x="%g" y="%g" overflow="hidden"`, x, y), 1)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="%g %g %g %g" width="%g" height="%g"><rect x="%g" y="%g" width="%g" height="%g" fill="white"/>%s</svg>`, ox, oy, ow, oh, ow, oh, ox, oy, ow, oh, root.rebuild(tag, root.body()))
}

func TestConnectorInkRasterNoCrop(t *testing.T) {
	for _, tc := range []struct {
		routing      string
		width, scale float64
	}{
		{"orthogonal", 80, .25}, {"orthogonal", 80, 1}, {"orthogonal", 80, 4},
		{"bezier", 160, 1}, {"straight", 160, 1}, {"orthogonal2", 160, 1},
	} {
		t.Run(fmt.Sprintf("%s_%g_%g", tc.routing, tc.width, tc.scale), func(t *testing.T) {
			doc := parseWSH(t, fmt.Sprintf(wideConnectorFixture, tc.routing, tc.width))
			svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
			got := spatialRaster(t, connectorRasterFrame(t, svg, false), tc.scale)
			want := spatialRaster(t, connectorRasterFrame(t, svg, true), tc.scale)
			changed, worst := 0, 0
			for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
				for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
					d := spatialPixelDelta(got, want, x, y)
					worst = max(worst, d)
					if d > 1 {
						changed++
					}
				}
			}
			if changed > 0 {
				t.Fatalf("viewport cropped %d raster pixels, max channel difference %d", changed, worst)
			}
			if dir := os.Getenv("ISOTOPO_INK_EXPORT"); dir != "" && tc.scale == 1 {
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%g.png", tc.routing, tc.width)))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, got)
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatal(err, closeErr)
				}
			}
		})
	}
}

const wideConnectorFixture = `canvas: {background: "#ffffff"}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 60, d: 40, h: 20}}
      - {id: b, shape: rectangle, geom: {w: 60, d: 40, h: 20}, offset: {wx: 400, wy: 140}}
    connectors:
      - from: a.port-left
        to: b.port-right
        routing: %s
        waypoints: [{wx: -240, wy: -200}, {wx: 660, wy: -200}]
        arrow: triangle
        stroke: {color: "#bd2545", width: %g, dash: "18 8", gradient: {to: "#334bbb"}}
`

func TestConnectorInkInsideViewBox(t *testing.T) {
	for _, routing := range []string{"straight", "orthogonal", "orthogonal2", "bezier"} {
		for _, width := range []float64{4, 80, 160} {
			t.Run(fmt.Sprintf("%s_%g", routing, width), func(t *testing.T) {
				doc := parseWSH(t, fmt.Sprintf(wideConnectorFixture, routing, width))
				svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
				if dir := os.Getenv("ISOTOPO_INK_EXPORT"); dir != "" {
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%g.svg", routing, width)), []byte(svg), 0644); err != nil {
						t.Fatal(err)
					}
				}
				root, err := findRootSVG(svg)
				if err != nil {
					t.Fatal(err)
				}
				x, y, w, h, err := parseViewBox(extractAttr(root.tag(), "viewBox"))
				if err != nil {
					t.Fatal(err)
				}
				inside := func(px, py, r float64) {
					t.Helper()
					if px-r < x-.011 || px+r > x+w+.011 || py-r < y-.011 || py+r > y+h+.011 {
						t.Fatalf("ink at (%g,%g), radius %g exceeds viewBox (%g,%g,%g,%g)", px, py, r, x, y, w, h)
					}
				}
				// Parse actual SVG commands independently of collected points. A
				// dense Bernstein oracle includes the curve rather than its hull.
				for index, path := range submittedConnectorPaths(t, svg) {
					for j := 0; j <= 2000; j++ {
						u := float64(j) / 2000
						if path.kind == "quadratic" {
							a, b, c := path.pts[0], path.pts[1], path.pts[2]
							inside((1-u)*(1-u)*a[0]+2*u*(1-u)*b[0]+u*u*c[0], (1-u)*(1-u)*a[1]+2*u*(1-u)*b[1]+u*u*c[1], width/2)
						} else {
							for k := 1; k < len(path.pts); k++ {
								a, b := path.pts[k-1], path.pts[k]
								inside((1-u)*a[0]+u*b[0], (1-u)*a[1]+u*b[1], width/2)
							}
						}
					}
					for _, p := range routes[index].Drawing.Arrow {
						inside(p[0], p[1], 0)
					}
				}
				// Observing geometry cannot be required for a correct export.
				if svg != RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil) {
					t.Fatal("collector changed viewBox")
				}
				if !strings.Contains(svg, `data-layer="canvas-bg"`) || math.IsNaN(w) || w <= 0 {
					t.Fatal("invalid canvas")
				}
			})
		}
	}
}
