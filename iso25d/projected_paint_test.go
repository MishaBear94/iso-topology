package iso25d

import (
	"encoding/xml"
	"io"
	"math"
	"strings"
	"testing"
)

// Independent test oracle: actual SVG faces and paint server stops. Test
// palettes use opaque colors/transparent only, so no production material
// resolver is needed. This catches face precedence as well as geometry.
func opaqueSVGPolygons(t *testing.T, svg string) [][][2]float64 {
	t.Helper()
	gradients := map[string]bool{}
	dec := xml.NewDecoder(strings.NewReader(svg))
	id := ""
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
			if e.Name.Local == "linearGradient" || e.Name.Local == "radialGradient" {
				id = a["id"]
				gradients[id] = true
			}
			if e.Name.Local == "stop" && id != "" && a["stop-color"] == "transparent" {
				gradients[id] = false
			}
		case xml.EndElement:
			if e.Name.Local == "linearGradient" || e.Name.Local == "radialGradient" {
				id = ""
			}
		}
	}
	return svgFilteredBodyPolygons(t, svg, func(a map[string]string) bool {
		f := a["fill"]
		if f == "transparent" {
			return false
		}
		if strings.HasPrefix(f, "url(#") {
			return gradients[strings.TrimSuffix(strings.TrimPrefix(f, "url(#"), ")")]
		}
		return true
	})
}

func TestShapePaintMatchesRenderedFaces(t *testing.T) {
	solid := func(c string) *FaceSurface { return &FaceSurface{Fill: &FaceFill{Color: c}} }
	for _, tc := range []struct {
		name, shape string
		o           ConvertOpts
	}{
		{"sharp-open-top", "rectangle", ConvertOpts{TopFill: "none"}},
		{"sharp-open-left", "rectangle", ConvertOpts{LeftFill: "none"}},
		{"sharp-open-right", "rectangle", ConvertOpts{RightFill: "none"}},
		{"sharp-wireframe-face-override", "rectangle", ConvertOpts{Wireframe: true, FaceSurfaces: map[string]*FaceSurface{"top": solid("red")}}},
		{"sharp-face-overrides-gradient", "rectangle", ConvertOpts{TopGradient: &FaceGradient{From: "red", To: "blue"}, FaceSurfaces: map[string]*FaceSurface{"top": solid("none")}}},
		{"sharp-wildcard-only-side", "rectangle", ConvertOpts{FaceSurfaces: map[string]*FaceSurface{"*": solid("none"), "right": solid("red")}}},
		{"rounded-open-top", "rectangle", ConvertOpts{CornerRadius: 50, TopFill: "none"}},
		{"rounded-open-band", "rectangle", ConvertOpts{CornerRadius: 50, FaceSurfaces: map[string]*FaceSurface{"side": solid("none")}}},
		{"rounded-wireframe-overrides-face", "rectangle", ConvertOpts{CornerRadius: 20, Wireframe: true, FaceSurfaces: map[string]*FaceSurface{"top": solid("red")}}},
		{"rounded-split-left", "rectangle", ConvertOpts{CornerRadius: 40, FaceSplit: true, LeftFill: "none"}},
		{"rounded-split-right", "rectangle", ConvertOpts{CornerRadius: 40, FaceSplit: true, RightFill: "none"}},
		{"rounded-split-own-palette-wins", "rectangle", ConvertOpts{CornerRadius: 40, FaceSplit: true, FaceSurfaces: map[string]*FaceSurface{"side": solid("none")}}},
		{"rounded-clamped-open-top", "rectangle", ConvertOpts{Width: 200, Depth: 80, CornerRadius: 100, TopFill: "none"}},
		{"rounded-deep-open-top", "rectangle", ConvertOpts{Width: 80, Depth: 200, CornerRadius: 100, TopFill: "none"}},
		{"cylinder-open-top", "cylinder", ConvertOpts{TopFill: "none"}},
		{"cylinder-open-left", "cylinder", ConvertOpts{LeftFill: "none"}},
		{"cylinder-open-right", "cylinder", ConvertOpts{RightFill: "none"}},
		{"cylinder-gradient-overrides-face", "cylinder", ConvertOpts{LeftGradient: &FaceGradient{From: "red", To: "blue"}, FaceSurfaces: map[string]*FaceSurface{"left": solid("none")}}},
		{"cylinder-ignores-wireframe", "cylinder", ConvertOpts{Wireframe: true}},
		{"person-open-dome-left", "person", ConvertOpts{LeftFill: "transparent", TopFill: "transparent"}},
		{"person-open-dome-right", "person", ConvertOpts{RightFill: "none"}},
		{"person-both-dome-gradient", "person", ConvertOpts{LeftFill: "transparent", RightFill: "none", TopFill: "transparent", TopGradient: &FaceGradient{From: "red", To: "blue"}}},
		{"sphere-transparent", "circle", ConvertOpts{TopFill: "transparent", LeftFill: "transparent"}},
		{"sphere-ignores-wireframe-faces", "circle", ConvertOpts{Wireframe: true, FaceSurfaces: map[string]*FaceSurface{"*": solid("none")}}},
		{"hex-open-top", "hexprism", ConvertOpts{TopFill: "none"}},
		{"triangle-open-left", "triprism", ConvertOpts{LeftFill: "none"}},
		{"octagon-open-right", "octprism", ConvertOpts{RightFill: "none"}},
		{"wedge-open-slope", "wedge", ConvertOpts{TopFill: "none"}},
		{"concave-open-top", "custom_path", ConvertOpts{TopFill: "none", Width: 200, Depth: 200, Params: map[string]any{"path": "M 0 0 L 200 0 L 200 40 L 40 40 L 40 200 L 0 200 Z"}}},
		{"cloud-open-top", "cloud", ConvertOpts{TopFill: "none"}},
		{"cloud-open-side", "cloud", ConvertOpts{LeftFill: "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ShapePaint(tc.shape, tc.o, .5)
			if got.Uncertain {
				t.Fatal("unexpected uncertain coverage")
			}
			actual := opaqueSVGPolygons(t, Convert2DTo25D(tc.shape, tc.o))
			ox, oy := PartOriginOffset(tc.shape, tc.o)
			for _, poly := range actual {
				for i := range poly {
					poly[i][0] -= ox
					poly[i][1] -= oy
				}
			}
			body := ShapeBody(tc.shape, tc.o)
			minx, miny, maxx, maxy := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, poly := range body.Polygons {
				for _, p := range poly {
					minx = math.Min(minx, p[0])
					maxx = math.Max(maxx, p[0])
					miny = math.Min(miny, p[1])
					maxy = math.Max(maxy, p[1])
				}
			}
			for i := 0; i < 73; i++ {
				for j := 0; j < 71; j++ {
					p := [2]float64{minx - 3 + (maxx-minx+6)*(float64(i)+.37)/73, miny - 3 + (maxy-miny+6)*(float64(j)+.61)/71}
					if oracleInside(p, got.Polygons) != oracleInside(p, actual) && oracleBoundaryDistance(p, actual) > .12 {
						t.Fatalf("paint differs from actual SVG at %v", p)
					}
				}
			}
		})
	}
}

func TestPaintAlphaAndCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name      string
		o         ConvertOpts
		count     int
		uncertain bool
	}{
		{"rgba faint", ConvertOpts{TopFill: "rgba(0,0,0,.3)", LeftFill: "none", RightFill: "none"}, 0, false},
		{"hex alpha", ConvertOpts{TopFill: "#000000cc", LeftFill: "none", RightFill: "none"}, 1, false},
		{"combined opacity", ConvertOpts{TopFill: "#000000cc", LeftFill: "none", RightFill: "none", Opacity: .6}, 0, false},
		{"opaque palette gradient", ConvertOpts{TopGradient: &FaceGradient{From: "rgba(0,0,0,.7)", To: "rgba(0,0,0,.9)"}}, 3, false},
		{"mixed alpha gradient", ConvertOpts{TopGradient: &FaceGradient{From: "transparent", To: "red"}}, 2, true},
		{"stop none unknown", ConvertOpts{TopGradient: &FaceGradient{From: "none", To: "red"}}, 2, true},
		{"valid zero sentinel", ConvertOpts{Opacity: 0}, 3, false},
		{"grain alpha lower bound", ConvertOpts{TopFill: "rgba(0,0,0,.6)", GrainIntensity: .3}, 2, true},
		{"blur uncertain", ConvertOpts{Blur: 2}, 0, true},
		{"pattern not solid", ConvertOpts{FaceSurfaces: map[string]*FaceSurface{"*": {Fill: &FaceFill{Kind: "pattern", Pattern: &FacePatternSpec{Kind: "dots", Color: "red"}}}}}, 0, true},
		{"empty gradient transparent", ConvertOpts{FaceSurfaces: map[string]*FaceSurface{"*": {Fill: &FaceFill{Kind: "linearGradient"}}}}, 0, false},
		{"invalid pattern falls back", ConvertOpts{FaceSurfaces: map[string]*FaceSurface{"*": {Fill: &FaceFill{Kind: "pattern", Pattern: &FacePatternSpec{Kind: "invalid", Color: "red"}}}}}, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := ShapePaint("rectangle", tc.o, .5)
			if len(p.Polygons) != tc.count || p.Uncertain != tc.uncertain {
				t.Fatalf("polys=%d uncertain=%v; want %d, %v", len(p.Polygons), p.Uncertain, tc.count, tc.uncertain)
			}
		})
	}
	for _, shape := range []string{"array1d", "array2d", "array3d", "rack"} {
		p := ShapePaint(shape, ConvertOpts{}, .5)
		if !p.Envelope || !p.Uncertain || len(p.Polygons) != 0 {
			t.Fatal("semantic envelope treated as solid", shape)
		}
	}
}

func TestNonSimplePaintIsUncertain(t *testing.T) {
	for _, path := range []string{"M 0 0 L 100 100 L 0 100 L 100 0 Z", "M 0 0 L 100 0 L 100 100 L 0 0 L 100 0 Z"} {
		p := ShapePaint("custom_path", ConvertOpts{Params: map[string]any{"path": path}}, .5)
		if !p.Uncertain || len(p.Polygons) != 0 {
			t.Fatal("non-simple fill was treated as exact", path)
		}
	}
}
