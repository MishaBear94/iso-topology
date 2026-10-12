package iso25d

import (
	"math"
	"testing"
)

func TestSpatialFacesMatchActualSVG(t *testing.T) {
	for _, shape := range []string{"rectangle", "square", "box", ""} {
		for _, o := range []ConvertOpts{{}, {Width: 45, Depth: 90, Height: 17}, {Width: 213, Depth: 52, Height: 118, Margin: 31, StrokeWidth: 4}, {Width: -5, Depth: 1, Height: 1}} {
			sp := ShapeSpatialBody(shape, o)
			if sp.Reason != "" {
				t.Fatal(sp.Reason)
			}
			actual := svgBodyPolygons(t, Convert2DTo25D(shape, o))
			if len(actual) != len(sp.Faces) {
				t.Fatalf("%q: %d emitted faces != %d spatial", shape, len(actual), len(sp.Faces))
			}
			ox, oy := PartOriginOffset(shape, o)
			for i, face := range sp.Faces {
				for j, w := range face.World {
					// Read actual SVG polygon coordinates, and project the world
					// vertices with the analytic camera, not a ShapeProvider.
					x, y := (w[0]-w[1])*math.Sqrt(3)/2, (w[0]+w[1])/2-w[2]
					p := actual[i][j]
					if math.Hypot(x-(p[0]-ox), y-(p[1]-oy)) > .015 {
						t.Fatalf("%q %s vertex %d: %v != %v", shape, face.Name, j, []float64{x, y}, p)
					}
				}
			}
		}
	}
}

func TestSpatialCapabilityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, shape string
		o           ConvertOpts
		supported   bool
	}{
		{"opaque", "rectangle", ConvertOpts{}, true},
		{"none border", "rectangle", ConvertOpts{Stroke: "none"}, true},
		{"opaque gradient", "box", ConvertOpts{TopGradient: &FaceGradient{From: "red", To: "blue"}}, true},
		{"opacity sentinel", "box", ConvertOpts{Opacity: 0}, true},
		{"transparent", "box", ConvertOpts{Opacity: .5}, false},
		{"wireframe", "box", ConvertOpts{Wireframe: true}, false},
		{"rounded", "box", ConvertOpts{CornerRadius: 10}, false},
		{"curve", "cylinder", ConvertOpts{}, false},
		{"open face", "box", ConvertOpts{TopFill: "none"}, false},
		{"gradient alpha", "box", ConvertOpts{TopGradient: &FaceGradient{From: "red", To: "transparent"}}, false},
		{"stroke alpha", "box", ConvertOpts{Stroke: "#0008"}, false},
		{"stroke dash", "box", ConvertOpts{StrokeDasharray: "2 3"}, false},
		{"shadow", "box", ConvertOpts{ShadowColor: "black"}, false},
		{"blur", "box", ConvertOpts{Blur: 1}, false},
		{"glow", "box", ConvertOpts{BackglowColor: "red"}, false},
		{"grain", "box", ConvertOpts{GrainIntensity: .5}, false},
		{"background", "box", ConvertOpts{Background: "white"}, false},
		{"face override", "box", ConvertOpts{FaceSurfaces: map[string]*FaceSurface{"top": {Fill: &FaceFill{Color: "transparent"}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ShapeSpatialBody(tc.shape, tc.o)
			if (got.Reason == "") != tc.supported {
				t.Fatalf("supported=%v reason=%s", tc.supported, got.Reason)
			}
		})
	}
}
