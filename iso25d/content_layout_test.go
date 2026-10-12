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

type contentMatrix [6]float64

func (m contentMatrix) at(x, y float64) [2]float64 {
	return [2]float64{m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]}
}
func multiplyContentMatrix(a, b contentMatrix) contentMatrix {
	return contentMatrix{a[0]*b[0] + a[2]*b[1], a[1]*b[0] + a[3]*b[1], a[0]*b[2] + a[2]*b[3], a[1]*b[2] + a[3]*b[3], a[0]*b[4] + a[2]*b[5] + a[4], a[1]*b[4] + a[3]*b[5] + a[5]}
}

// Independent oracle reads actual SVG attributes and composes nested matrices.
// It does not call the production content layout or infer content from bodies.
func svgContent(t *testing.T, svg string) []ContentElement {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(svg))
	stack := []contentMatrix{{1, 0, 0, 1, 0, 0}}
	var out []ContentElement
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			a := map[string]string{}
			for _, v := range e.Attr {
				a[v.Name.Local] = v.Value
			}
			m := stack[len(stack)-1]
			if tf := a["transform"]; tf != "" {
				var q contentMatrix
				if strings.HasPrefix(tf, "matrix(") {
					if n, _ := fmt.Sscanf(strings.NewReplacer("matrix(", "", ")", "", ",", " ").Replace(tf), "%f %f %f %f %f %f", &q[0], &q[1], &q[2], &q[3], &q[4], &q[5]); n != 6 {
						t.Fatal(tf)
					}
				} else {
					t.Fatal("unexpected transform", tf)
				}
				m = multiplyContentMatrix(m, q)
			}
			num := func(key string) float64 {
				v, err := strconv.ParseFloat(a[key], 64)
				if err != nil {
					t.Fatal(key, err)
				}
				return v
			}
			if e.Name.Local == "text" {
				var str string
				if err := dec.DecodeElement(&str, &e); err != nil {
					t.Fatal(err)
				}
				dy := 0.
				if a["dy"] != "" {
					dy, _ = strconv.ParseFloat(strings.TrimSuffix(a["dy"], "em"), 64)
				}
				out = append(out, ContentElement{Kind: "text", Text: str, Anchor: m.at(num("x"), num("y")), FontSize: num("font-size"), DyEm: dy})
				continue
			}
			if e.Name.Local == "image" {
				x, y, w, h := num("x"), num("y"), num("width"), num("height")
				out = append(out, ContentElement{Kind: "icon", Anchor: m.at(x, y), Quad: [4][2]float64{m.at(x, y), m.at(x+w, y), m.at(x+w, y+h), m.at(x, y+h)}})
			}
			stack = append(stack, m)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	return out
}

func TestContentPositionsMatchActualSVG(t *testing.T) {
	for _, tc := range []struct {
		name, shape string
		o           ConvertOpts
	}{
		{"large-projected-face", "rectangle", ConvertOpts{Width: 16000, Depth: 9000, Label: "Large", Icon: "icon"}},
		{"box-default", "rectangle", ConvertOpts{Label: "Cache"}},
		{"box-shallow-row", "rectangle", ConvertOpts{Width: 200, Depth: 40, Label: "Service", Icon: "icon"}},
		{"box-icon", "rectangle", ConvertOpts{Label: "Cache", Icon: "data:image/svg+xml;base64,PHN2Zy8+"}},
		{"box-multiline", "rectangle", ConvertOpts{Label: "Cache\nPrimary\n热备"}},
		{"box-explicit-single-line", "rectangle", ConvertOpts{Label: "old", LabelLines: []string{"explicit"}}},
		{"box-lines-only", "rectangle", ConvertOpts{LabelLines: []string{"Header"}}},
		{"box-shrink", "rectangle", ConvertOpts{Width: 65, Depth: 60, Label: "Customer Facing Dashboards", Icon: "icon", FontSize: 22}},
		{"box-icon-offset", "rectangle", ConvertOpts{Label: "Manual", Icon: "icon", IconAnchor: "bottomRight", IconOffX: .1, IconOffY: -.2}},
		{"rounded", "rectangle", ConvertOpts{CornerRadius: 60, Width: 100, Depth: 200, Label: "Cache", Icon: "icon"}},
		{"sphere", "circle", ConvertOpts{Label: "Sphere"}},
		{"sphere-icon", "circle", ConvertOpts{Label: "Sphere", Icon: "icon"}},
		{"person", "person", ConvertOpts{Label: "User"}},
		{"person-wide-label", "person", ConvertOpts{Width: 90, Depth: 180, Label: "A very wide user label"}},
		{"cylinder", "cylinder", ConvertOpts{Width: 180, Depth: 90, Label: "Database", Icon: "icon"}},
		{"cloud-label", "cloud", ConvertOpts{Label: "Cloud"}},
		{"cloud-icon", "cloud", ConvertOpts{Label: "Cloud services", Icon: "icon"}},
		{"triangle", "triprism", ConvertOpts{Label: "Ingestion", Icon: "icon"}},
		{"hexagon", "hexprism", ConvertOpts{Label: "Workers"}},
		{"octagon", "octprism", ConvertOpts{Label: "Security"}},
		{"wedge", "wedge", ConvertOpts{Label: "Ingestion", Icon: "icon"}},
		{"custom-concave", "custom_path", ConvertOpts{Width: 200, Depth: 200, Label: "Service", Icon: "icon", Params: map[string]any{"path": "M 0 0 L 200 0 L 200 40 L 40 40 L 40 200 L 0 200 Z"}}},
		{"array", "array2d", ConvertOpts{Label: "Storage"}},
		{"rack", "rack", ConvertOpts{Label: "Compute"}},
		{"iso-text-default", "iso_text", ConvertOpts{Label: "Default size"}},
		{"iso-text-theme", "iso_text", ConvertOpts{Label: "标题 / Title", FontSize: 31}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ShapeContent(tc.shape, tc.o).Elements
			actual := svgContent(t, Convert2DTo25D(tc.shape, tc.o))
			ox, oy := PartOriginOffset(tc.shape, tc.o)
			if len(got) != len(actual) {
				t.Fatalf("items got %d actual %d", len(got), len(actual))
			}
			for i, g := range got {
				a := actual[i]
				a.Anchor[0] -= ox
				a.Anchor[1] -= oy
				if g.Kind != a.Kind || g.Text != a.Text || math.Abs(g.FontSize-a.FontSize) > .006 || g.DyEm != a.DyEm {
					t.Fatalf("item %d differs: %+v, actual %+v", i, g, a)
				}
				if math.Hypot(g.Anchor[0]-a.Anchor[0], g.Anchor[1]-a.Anchor[1]) > .05 {
					t.Fatalf("anchor %d got %v actual %v", i, g.Anchor, a.Anchor)
				}
				if g.Kind == "icon" {
					for j, p := range a.Quad {
						p[0] -= ox
						p[1] -= oy
						if math.Hypot(g.Quad[j][0]-p[0], g.Quad[j][1]-p[1]) > .05 {
							t.Fatalf("icon viewport got %v actual %v", g.Quad[j], p)
						}
					}
				}
			}
		})
	}
}

func TestConcaveContentStaysOnActualTopFace(t *testing.T) {
	for _, path := range []string{
		"M 0 0 L 200 0 L 200 40 L 40 40 L 40 200 L 0 200 Z",
		"M 0 0 L 200 0 L 200 200 L 160 200 L 160 40 L 40 40 L 40 200 L 0 200 Z",
		"M 0 0 L 200 0 L 200 40 L 40 40 L 40 160 L 200 160 L 200 200 L 0 200 Z",
	} {
		o := ConvertOpts{Width: 200, Depth: 200, Height: 20, Label: "Service", Icon: "icon", Params: map[string]any{"path": path}}
		svg := Convert2DTo25D("custom_path", o)
		top := svgFilteredBodyPolygons(t, svg, func(a map[string]string) bool { return a["data-face"] == "top" })
		ox, oy := PartOriginOffset("custom_path", o)
		for _, e := range ShapeContent("custom_path", o).Elements {
			if e.Kind == "text" && e.FontSize != 18 {
				t.Fatalf("content fitting must retain the default readable font, got %v", e.FontSize)
			}
			for _, p := range e.Quad {
				p[0] += ox
				p[1] += oy
				if !oracleInside(p, top) && oracleBoundaryDistance(p, top) > .12 {
					t.Fatalf("%s content escapes actual top at %v for %s", e.Kind, p, path)
				}
			}
		}
	}
}

func TestShallowContentRowPreservesSizeAndSeparation(t *testing.T) {
	items := layoutTopContent(200, 40, "Service", nil, "icon", .4, "", 0, 0, 18)
	if len(items) != 2 || !items[0].Icon || items[1].Text != "Service" || items[1].FontSize != 18 || items[0].W != 16 {
		t.Fatalf("row lost original content or sizing: %+v", items)
	}
	textLeft := items[1].X - lineWidth(items[1].Text, 18)/2
	if textLeft-items[0].X-items[0].W < 8.99 {
		t.Fatalf("icon and label must retain the half-em gap: %+v", items)
	}
	// An explicit author position must opt out of the automatic row.
	manual := layoutTopContent(200, 40, "Service", nil, "icon", .4, "topLeft", 0, 0, 18)
	if manual[0].X == items[0].X {
		t.Fatal("manual icon placement was overwritten")
	}
}
