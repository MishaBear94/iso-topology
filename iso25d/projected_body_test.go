package iso25d

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
)

// This oracle reads emitted SVG faces rather than calling a shape provider.
// Elliptical SVG arcs are reconstructed from their endpoints/radii/flags.
func svgBodyPolygons(t *testing.T, svg string) [][][2]float64 {
	polys := svgFilteredBodyPolygons(t, svg, nil)
	if len(polys) == 0 {
		t.Fatal("no filled SVG faces")
	}
	return polys
}

func svgFilteredBodyPolygons(t *testing.T, svg string, include func(map[string]string) bool) [][][2]float64 {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(svg))
	var out [][][2]float64
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		e, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		a := map[string]string{}
		for _, v := range e.Attr {
			a[v.Name.Local] = v.Value
		}
		if a["data-face"] == "" || a["fill"] == "" || a["fill"] == "none" || (include != nil && !include(a)) {
			continue
		}
		num := func(k string) float64 {
			v, err := strconv.ParseFloat(a[k], 64)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		var poly [][2]float64
		switch e.Name.Local {
		case "polygon":
			for _, pair := range strings.Fields(a["points"]) {
				var x, y float64
				if _, err := fmt.Sscanf(pair, "%f,%f", &x, &y); err != nil {
					t.Fatal(err)
				}
				poly = append(poly, [2]float64{x, y})
			}
		case "circle", "ellipse":
			cx, cy := num("cx"), num("cy")
			var rx, ry float64
			if e.Name.Local == "circle" {
				rx = num("r")
				ry = rx
			} else {
				rx, ry = num("rx"), num("ry")
			}
			for i := 0; i < 2048; i++ {
				theta := 2 * math.Pi * float64(i) / 2048
				poly = append(poly, [2]float64{cx + rx*math.Cos(theta), cy + ry*math.Sin(theta)})
			}
		case "path":
			f := strings.Fields(strings.ReplaceAll(a["d"], ",", " "))
			i := 0
			take := func() float64 {
				if i >= len(f) {
					t.Fatal("truncated SVG path")
				}
				v, err := strconv.ParseFloat(f[i], 64)
				if err != nil {
					t.Fatal(err)
				}
				i++
				return v
			}
			for i < len(f) {
				cmd := f[i]
				i++
				switch cmd {
				case "M", "L":
					poly = append(poly, [2]float64{take(), take()})
				case "Z":
				case "A":
					rx, ry, rot, large, sweep := take(), take(), take(), take(), take()
					end := [2]float64{take(), take()}
					start := poly[len(poly)-1]
					if rot != 0 {
						t.Fatal("oracle only supports axis-aligned arcs")
					}
					dx, dy := (start[0]-end[0])/2, (start[1]-end[1])/2
					lam := dx*dx/(rx*rx) + dy*dy/(ry*ry)
					if lam > 1 {
						rx *= math.Sqrt(lam)
						ry *= math.Sqrt(lam)
					}
					den := rx*rx*dy*dy + ry*ry*dx*dx
					k := math.Sqrt(math.Max(0, (rx*rx*ry*ry-den)/den))
					if large == sweep {
						k = -k
					}
					cx := (start[0]+end[0])/2 + k*rx*dy/ry
					cy := (start[1]+end[1])/2 - k*ry*dx/rx
					lo := math.Atan2((start[1]-cy)/ry, (start[0]-cx)/rx)
					delta := math.Atan2((end[1]-cy)/ry, (end[0]-cx)/rx) - lo
					if sweep == 1 && delta < 0 {
						delta += 2 * math.Pi
					}
					if sweep == 0 && delta > 0 {
						delta -= 2 * math.Pi
					}
					for j := 1; j <= 512; j++ {
						theta := lo + delta*float64(j)/512
						poly = append(poly, [2]float64{cx + rx*math.Cos(theta), cy + ry*math.Sin(theta)})
					}
				default:
					t.Fatalf("unsupported SVG path token %q", cmd)
				}
			}
		}
		if len(poly) > 0 {
			out = append(out, poly)
		}
	}
	return out
}

func oracleInside(p [2]float64, polys [][][2]float64) bool {
	for _, poly := range polys {
		winding := 0
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			cross := (b[0]-a[0])*(p[1]-a[1]) - (p[0]-a[0])*(b[1]-a[1])
			if a[1] <= p[1] && b[1] > p[1] && cross > 0 {
				winding++
			}
			if a[1] > p[1] && b[1] <= p[1] && cross < 0 {
				winding--
			}
		}
		if winding != 0 {
			return true
		}
	}
	return false
}

func oracleBoundaryDistance(p [2]float64, polys [][][2]float64) float64 {
	best := math.Inf(1)
	for _, poly := range polys {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			dx, dy := b[0]-a[0], b[1]-a[1]
			u := 0.0
			if den := dx*dx + dy*dy; den > 0 {
				u = math.Max(0, math.Min(1, ((p[0]-a[0])*dx+(p[1]-a[1])*dy)/den))
			}
			best = math.Min(best, math.Hypot(p[0]-a[0]-u*dx, p[1]-a[1]-u*dy))
		}
	}
	return best
}

func TestShapeBodyMatchesRenderedFaces(t *testing.T) {
	for _, tc := range []struct {
		name, shape string
		o           ConvertOpts
	}{
		{"default-box", "rectangle", ConvertOpts{}},
		{"rounded", "rectangle", ConvertOpts{Width: 180, Depth: 100, Height: 32, CornerRadius: 30}},
		{"rounded-clamped-square", "square", ConvertOpts{Width: 120, Depth: 70, Height: 20, CornerRadius: 100}},
		{"rounded-capsule-wide", "rectangle", ConvertOpts{Width: 200, Depth: 80, Height: 30, CornerRadius: 100}},
		{"rounded-capsule-deep", "rectangle", ConvertOpts{Width: 80, Depth: 200, Height: 30, CornerRadius: 100}},
		{"split-rounded", "rectangle", ConvertOpts{Width: 220, Depth: 140, Height: 12, CornerRadius: 32, FaceSplit: true}},
		{"sphere", "circle", ConvertOpts{Width: 120}},
		{"cylinder", "cylinder", ConvertOpts{Width: 170, Depth: 80, Height: 120}},
		{"default-person", "person", ConvertOpts{}},
		{"explicit-person", "person", ConvertOpts{Width: 120, Depth: 80, Height: 50}},
		{"short-person", "person", ConvertOpts{Width: 240, Depth: 100, Height: 12, Label: "A wide label changes the standalone frame"}},
		{"tall-person", "person", ConvertOpts{Width: 80, Depth: 160, Height: 180}},
		{"triangle", "triprism", ConvertOpts{Width: 180, Depth: 120, Height: 80}},
		{"normalised-triangle", " TRIPRISM ", ConvertOpts{Width: 180, Depth: 120, Height: 80, Sides: 8}},
		{"hexagon", "hexprism", ConvertOpts{}},
		{"octagon", "octprism", ConvertOpts{}},
		{"wedge", "wedge", ConvertOpts{Width: 170, Depth: 100, Height: 60}},
		{"concave-custom", "custom_path", ConvertOpts{Width: 180, Depth: 180, Height: 12, Params: map[string]any{"path": "M 0 0 L 180 0 L 180 40 L 40 40 L 40 180 L 0 180 Z"}}},
		{"custom-reverse", "custom_path", ConvertOpts{Width: 180, Depth: 180, Height: 12, Params: map[string]any{"path": "M 0 180 L 40 180 L 40 40 L 180 40 L 180 0 L 0 0 Z"}}},
		{"cloud-clamped", "cloud", ConvertOpts{Width: 100, Depth: 80, Height: 12}},
		{"cloud-tall", "cloud", ConvertOpts{Width: 260, Depth: 200, Height: 90}},
		{"unknown-alias", "oval", ConvertOpts{Width: 170, Depth: 100, Height: 60}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.o.Margin = 29
			body := ShapeBody(tc.shape, tc.o)
			if body.Envelope {
				t.Fatal("unexpected approximate envelope")
			}
			if body.MaxError > .050001 {
				t.Fatalf("curve error %g", body.MaxError)
			}
			actual := svgBodyPolygons(t, Convert2DTo25D(tc.shape, tc.o))
			ox, oy := PartOriginOffset(tc.shape, tc.o) // separately verified by resolved_geometry_test
			for _, poly := range actual {
				for i := range poly {
					poly[i][0] -= ox
					poly[i][1] -= oy
				}
			}
			minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, poly := range actual {
				for _, p := range poly {
					minx = math.Min(minx, p[0])
					maxx = math.Max(maxx, p[0])
					miny = math.Min(miny, p[1])
					maxy = math.Max(maxy, p[1])
				}
			}
			checked := 0
			for i := 0; i < 73; i++ {
				for j := 0; j < 71; j++ {
					p := [2]float64{minx - 3 + (maxx-minx+6)*(float64(i)+.37)/73, miny - 3 + (maxy-miny+6)*(float64(j)+.61)/71}
					got, want := oracleInside(p, body.Polygons), oracleInside(p, actual)
					if got != want && oracleBoundaryDistance(p, actual) > .12 {
						t.Fatalf("body coverage differs from rendered SVG at %v: got %v, want %v", p, got, want)
					}
					checked++
				}
			}
			if checked < 5000 {
				t.Fatal("insufficient samples")
			}
		})
	}
}

func TestBodyCapabilityAndDefaultIntent(t *testing.T) {
	for _, shape := range []string{"iso_text", "title", "composite"} {
		if len(ShapeBody(shape, ConvertOpts{}).Polygons) != 0 {
			t.Fatal("non-solid shape has body", shape)
		}
	}
	for _, shape := range []string{"array1d", "array2d", "array3d", "rack"} {
		if !ShapeBody(shape, ConvertOpts{}).Envelope {
			t.Fatal("semantic envelope unmarked", shape)
		}
	}
	a, b := ShapeBody("person", ConvertOpts{}), ShapeBody("person", ConvertOpts{Width: 120})
	p := [2]float64{0, -53}
	if !oracleInside(p, a.Polygons) || oracleInside(p, b.Polygons) {
		t.Fatal("explicit width must preserve different head default")
	}
}

func TestConcaveTopAnchorIsInsideActualFace(t *testing.T) {
	for _, path := range []string{
		"M 0 0 L 180 0 L 180 40 L 40 40 L 40 180 L 0 180 Z",
		"M 0 0 L 180 0 L 180 180 L 140 180 L 140 40 L 40 40 L 40 180 L 0 180 Z",
		"M 0 0 L 180 0 L 180 40 L 40 40 L 40 140 L 180 140 L 180 180 L 0 180 Z",
	} {
		o := ConvertOpts{Width: 180, Depth: 180, Height: 15, Params: map[string]any{"path": path}}
		body := ShapeBody("custom_path", o)
		if body.TopAnchor == nil {
			t.Fatal("missing interior top anchor")
		}
		a := *body.TopAnchor
		p := [2]float64{(a[0] - a[1]) * math.Sqrt(3) / 2, (a[0]+a[1])/2 - a[2]}
		faces := svgBodyPolygons(t, Convert2DTo25D("custom_path", o))
		ox, oy := PartOriginOffset("custom_path", o)
		top := faces[len(faces)-1]
		for i := range top {
			top[i][0] -= ox
			top[i][1] -= oy
		}
		if !oracleInside(p, [][][2]float64{top}) {
			t.Fatalf("top anchor %v in empty concavity of %s", a, path)
		}
	}
}
