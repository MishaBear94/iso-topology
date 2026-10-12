package iso25d

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestTinyContentKeepsVisibleIcon(t *testing.T) {
	for _, size := range []float64{1, 4, 8, 12} {
		items := layoutTopContent(size, size, "", nil, "icon", .9, "", 0, 0, 18)
		if len(items) != 1 || !items[0].Icon {
			t.Fatalf("missing icon for size %g: %+v", size, items)
		}
		p := items[0]
		if p.W <= 0 || p.W > size || p.H != p.W || p.X < 0 || p.Y < 0 || p.X+p.W > size+1e-9 || p.Y+p.H > size+1e-9 {
			t.Fatalf("size %g generated invalid or out-of-face icon: %+v", size, p)
		}
	}
}

func TestShallowTinyContentHasPositiveViewport(t *testing.T) {
	o := ConvertOpts{Width: 20, Depth: 8, Height: 4, Icon: "icon", IconScale: .4}
	for _, item := range svgContent(t, Convert2DTo25D("rectangle", o)) {
		if item.Kind == "icon" {
			// A negative image width reverses the projected top edge.
			if item.Quad[1][0] <= item.Quad[0][0] || item.Quad[1][1] <= item.Quad[0][1] {
				t.Fatalf("actual SVG has nonpositive image viewport: %+v", item)
			}
		}
	}
}

func TestContentFitUsesFinalPlacement(t *testing.T) {
	for _, tc := range []struct {
		name, shape, status string
		o                   ConvertOpts
		manual              bool
	}{
		{"fits", "rectangle", "estimated-fit", ConvertOpts{Label: "Service"}, false},
		{"row", "rectangle", "estimated-fit", ConvertOpts{Width: 200, Depth: 40, Label: "Service", Icon: "icon", IconScale: .4, FontSize: 18}, false},
		{"tiny", "rectangle", "estimated-overflow", ConvertOpts{Width: 8, Depth: 8, Icon: "icon"}, false},
		{"atomic-overflow", "rectangle", "estimated-overflow", ConvertOpts{Width: 22, Depth: 120, Label: "👨‍👩‍👧‍👦"}, false},
		{"manual-overflow", "rectangle", "estimated-overflow", ConvertOpts{Width: 100, Depth: 100, Icon: "icon", IconAnchor: "topLeft", IconOffX: 1}, true},
		{"manual-fit", "rectangle", "estimated-fit", ConvertOpts{Width: 140, Depth: 140, Icon: "icon", IconAnchor: "center"}, true},
		{"concave", "custom_path", "estimated-fit", ConvertOpts{Width: 200, Depth: 200, Label: "Service", Icon: "icon", Params: map[string]any{"path": "M 0 0 L 200 0 L 200 40 L 40 40 L 40 200 L 0 200 Z"}}, false},
		{"free-label", "iso_text", "unsupported", ConvertOpts{Label: "Title"}, false},
		{"sphere", "circle", "unsupported", ConvertOpts{Label: "Sphere"}, false},
		{"empty", "rectangle", "empty", ConvertOpts{}, false},
		{"hidden-text", "rectangle", "empty", ConvertOpts{Width: 20, Depth: 20, Label: "Long text", FontColor: "none"}, false},
		{"hidden-cylinder-text", "cylinder", "empty", ConvertOpts{Width: 20, Label: "Long text", FontColor: "transparent"}, false},
		{"zero-alpha-text", "rectangle", "empty", ConvertOpts{Width: 20, Depth: 20, Label: "Long text", FontColor: "rgba(20,30,40,0)"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := ShapeContent(tc.shape, tc.o)
			fit := content.Fit
			if fit.Status != tc.status || fit.Manual != tc.manual {
				t.Fatalf("wrong final fit observation: %+v", fit)
			}
			actual := svgContent(t, Convert2DTo25D(tc.shape, tc.o))
			for _, item := range fit.Items {
				if item.Element < 0 || item.Element >= len(actual) || item.Kind != actual[item.Element].Kind || item.Text != actual[item.Element].Text || math.Abs(item.FontSize-actual[item.Element].FontSize) > .006 {
					t.Fatalf("fit does not describe emitted item: %+v / %+v", item, actual)
				}
			}
			if tc.name == "row" && (len(fit.Items) != 2 || fit.Items[1].FontSize != 18 || fit.Items[0].Bounds.W != 16) {
				t.Fatalf("observed rejected column instead of final row: %+v", fit)
			}
		})
	}
}

func TestContentFitReportsUnsupportedNonfiniteBounds(t *testing.T) {
	fit := measureTopContent(80, 80, []contentPlacement{{X: math.NaN(), W: 10, H: 10, Icon: true}}, false, true)
	if fit.Status != "unsupported" {
		t.Fatal(fit)
	}
	if _, err := json.Marshal(fit); err != nil {
		t.Fatalf("unsupported observation must remain serializable: %v", err)
	}
}

func TestContentIconNeverGrowsWhenYielding(t *testing.T) {
	for _, size := range []float64{8, 14, 20, 40, 80} {
		for _, scale := range []float64{.02, .1, .4, 1, 2} {
			fit := fitTopContent(size, size, []string{strings.Repeat("Long ", 80)}, true, scale, 18)
			if fit.IconSize <= 0 || fit.IconSize > math.Min(size, size*scale)+1e-9 {
				t.Fatalf("size=%g scale=%g icon grew or vanished: %+v", size, scale, fit)
			}
		}
	}
}
