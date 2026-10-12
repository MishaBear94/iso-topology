package isotopo

import (
	"math"
	"testing"
)

func TestPersonLabelOcclusionUsesRenderedBottomLabel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset float64
		hidden bool
	}{
		{"body covered but bottom label clear", 0, false},
		{"bottom label covered but top centre clear", 90, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			person := &CompositePart{ID: "person", Shape: "person", Label: "User", Offset: &WorldPoint{}}
			cover := &CompositePart{ID: "cover", Shape: "rectangle", Geom: &Geom{W: 60, D: 60, H: 10}, Offset: &WorldPoint{WX: tc.offset, WY: tc.offset}}
			got := labelOcclusionInFlat([]*CompositePart{person, cover}, nil, "scene")
			if (len(got) > 0) != tc.hidden {
				t.Fatalf("hidden=%v want %v: %v; text=%v paint=%v", len(got) > 0, tc.hidden, got, partContentRegions(person, nil, "text"), occPaintRegions([]*CompositePart{cover}, nil).regions[cover])
			}
		})
	}
}

func TestCaptionLiftIgnoresUnfilledBody(t *testing.T) {
	label := lbl("Lane")
	ghost := tallNodeAt(0, 90)
	ghost.Style = &Style{Palette: &Palette{Top: "none", Left: "none", Right: "none"}}
	out := resolveGroupLabelOcclusion([]*CompositePart{label, ghost})
	if out[0] != label {
		t.Fatal("empty fill must not change caption paint order")
	}
}

func TestCaptionLiftRetainsProtectionForUnknownCoverage(t *testing.T) {
	for _, shape := range []string{"array2d", "rack"} {
		t.Run(shape, func(t *testing.T) {
			label := lbl("Lane")
			cover := tallNodeAt(0, 90)
			cover.Shape = shape
			if !occPaintRegions([]*CompositePart{cover}, nil).unresolved[cover] {
				t.Fatal("fixture must exercise unsupported coverage")
			}
			out := resolveGroupLabelOcclusion([]*CompositePart{label, cover})
			if out[len(out)-1] != label {
				t.Fatal("unknown coverage must not erase existing caption protection")
			}
			cover.Offset = &WorldPoint{WX: 900, WY: 900}
			if resolveGroupLabelOcclusion([]*CompositePart{label, cover})[0] != label {
				t.Fatal("distant unknown body must not move the caption")
			}
		})
	}
}

func TestFaceLabelOcclusionChecksEveryRenderedLine(t *testing.T) {
	for _, tc := range []struct {
		name, label          string
		w, d, fs, x, y, diam float64
	}{
		{"right end of a long label", "ABCDEFGHIJKLMNO", 300, 100, 16, 135, 98, 45},
		{"first line away from face centre", "First\nSecond\nThird\nFourth", 300, 160, 14, 82.45, 72.4, 26},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &CompositePart{ID: "text", Shape: "rectangle", Label: tc.label, Geom: &Geom{W: tc.w, D: tc.d, H: 30}, Offset: &WorldPoint{}, Style: &Style{Text: &Text{Size: ptrFloat(tc.fs)}}}
			q := &CompositePart{ID: "cover", Shape: "circle", Geom: &Geom{W: tc.diam}, Offset: &WorldPoint{WX: tc.y + tc.x/math.Sqrt(3), WY: tc.y - tc.x/math.Sqrt(3)}}
			if got := labelOcclusionInFlat([]*CompositePart{p, q}, nil, "scene"); len(got) == 0 {
				t.Fatal("rendered text overlap was missed")
			}
		})
	}
}

func TestCaptionOcclusionUsesThemeFontSize(t *testing.T) {
	label := &CompositePart{ID: "title", Shape: "iso_text", Label: "Heading", Offset: &WorldPoint{}}
	cover := &CompositePart{ID: "cover", Shape: "circle", Geom: &Geom{W: 60}, Offset: &WorldPoint{WX: 38 + 100/math.Sqrt(3), WY: 38 - 100/math.Sqrt(3)}}
	theme := &Theme{Style: Style{Text: &Text{Size: ptrFloat(36)}}}
	if len(labelOcclusionInFlat([]*CompositePart{label, cover}, theme, "scene")) == 0 {
		t.Fatal("large themed caption overlap was missed")
	}
	if len(labelOcclusionInFlat([]*CompositePart{label, cover}, nil, "scene")) != 0 {
		t.Fatal("small caption should remain clear")
	}
}

func TestLabelOcclusionDoesNotApplyIsoGeometryToTopView(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	doc.Canvas.Projection = "top"
	if got := LabelOcclusionIssues(doc); len(got) != 0 {
		t.Fatalf("iso label detector reported a top-view defect: %v", got)
	}
}
