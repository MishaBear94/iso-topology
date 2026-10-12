package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func connectorBodyTestInfos(parts []iso25d.CompositePart) []partInfo {
	infos := make([]partInfo, len(parts))
	for i, p := range parts {
		w, d, h := iso25d.ShapeDimensions(p.Shape, p.Opts)
		infos[i] = partInfo{w: w, d: d, h: h, offWX: p.OffWX, offWY: p.OffWY, offWZ: p.OffWZ}
	}
	return infos
}

func connectorBodyTestRegion(poly [][2]float64) connectorTextBodies {
	out := connectorTextBodies{interiors: screenRegion{poly}}
	for i, a := range poly {
		out.borders = append(out.borders, connectorTextStroke{owner: -1, a: a, b: poly[(i+1)%len(poly)], radius: 1})
	}
	return out
}

func TestConnectorTextBodyEmptyCornersAndFilledInteriors(t *testing.T) {
	// This large concave face has an empty right-hand notch. Keep that notch,
	// but do not confuse a pill completely enclosed by its left leg with air.
	body := connectorBodyTestRegion([][2]float64{{0, 0}, {300, 0}, {300, 50}, {100, 50}, {100, 250}, {300, 250}, {300, 300}, {0, 300}})
	for _, test := range []struct {
		name   string
		points [][2]float64
		clear  bool
	}{
		{"notch", [][2]float64{{150, 100}, {250, 200}}, true},
		{"inside", [][2]float64{{40, 100}, {60, 200}}, false},
		{"crossing", [][2]float64{{80, 100}, {110, 200}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, clear := connectorTextPositionWithInteriors(test.points, false, 30, 20, nil, body.borders, body.interiors)
			if clear != test.clear {
				t.Fatalf("clear=%v, want %v", clear, test.clear)
			}
		})
	}
	// SVG defaults to nonzero winding. Even/odd incorrectly empties this face.
	twice := [][2]float64{{0, 0}, {300, 0}, {300, 300}, {0, 300}, {0, 0}, {300, 0}, {300, 300}, {0, 300}}
	body = connectorBodyTestRegion(twice)
	if _, _, clear := connectorTextPositionWithInteriors([][2]float64{{100, 150}, {200, 150}}, false, 30, 20, nil, body.borders, body.interiors); clear {
		t.Fatal("multiply-wound filled face certified as empty")
	}
}

func TestConnectorTextBodyCapabilityFallback(t *testing.T) {
	base := iso25d.CompositePart{Shape: "rectangle", Opts: iso25d.ConvertOpts{Width: 120, Depth: 90, Height: 30, FontFamily: "Source Sans Pro", Label: "AV office"}}
	for _, test := range []struct {
		name   string
		change func(*iso25d.CompositePart, *partInfo)
	}{
		{"unknown-font", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.FontFamily = "unknown" }},
		{"unknown-script", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.Label = "中文" }},
		{"screen-label", func(_ *iso25d.CompositePart, p *partInfo) { p.screenLabel = "external" }},
		{"rows", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.Rows = []string{"row"} }},
		{"cells", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.Cells = [][]string{{"cell"}} }},
		{"header", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.Header = "header" }},
		{"blur", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.Blur = 2 }},
		{"shadow", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.ShadowColor = "black" }},
		{"glow", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.BackglowColor = "blue" }},
		{"background", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.Background = "white" }},
		{"grain", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.GrainIntensity = .1 }},
		{"pattern", func(p *iso25d.CompositePart, _ *partInfo) { p.Opts.PatternKind = "hatch" }},
		{"surface", func(p *iso25d.CompositePart, _ *partInfo) {
			p.Opts.FaceSurfaces = map[string]*iso25d.FaceSurface{"top": {}}
		}},
		{"array", func(p *iso25d.CompositePart, _ *partInfo) { p.Shape = "array2d" }},
		{"unknown-shape", func(p *iso25d.CompositePart, _ *partInfo) { p.Shape = "future-shape" }},
		{"unbound-person-label", func(p *iso25d.CompositePart, _ *partInfo) { p.Shape, p.Opts.FontFamily = "person", "unknown" }},
		{"unbound-circle-label", func(p *iso25d.CompositePart, _ *partInfo) { p.Shape, p.Opts.FontFamily = "circle", "unknown" }},
		{"unbound-cloud-label", func(p *iso25d.CompositePart, _ *partInfo) { p.Shape, p.Opts.FontFamily = "cloud", "unknown" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			parts := []iso25d.CompositePart{base, base}
			infos := connectorBodyTestInfos(parts)
			test.change(&parts[1], &infos[1])
			if _, ok := connectorTextBodyRegions(parts, infos); ok {
				t.Fatal("unknown ink accepted")
			}
		})
	}
	parts := []iso25d.CompositePart{base}
	if _, ok := connectorTextBodyRegions(parts, connectorBodyTestInfos(parts)); !ok {
		t.Fatal("bound text rejected")
	}
	if _, ok := connectorTextBodyRegions(parts, nil); ok {
		t.Fatal("missing part metadata accepted")
	}
	for _, shape := range []string{"person", "circle", "cloud"} {
		parts[0].Shape = shape
		parts[0].Opts.Label = "Ágj AV office\n\nǗff"
		if _, ok := connectorTextBodyRegions(parts, connectorBodyTestInfos(parts)); !ok {
			t.Fatalf("bound %s caption rejected", shape)
		}
		parts[0].Opts.Label += "中文"
		if _, ok := connectorTextBodyRegions(parts, connectorBodyTestInfos(parts)); ok {
			t.Fatalf("partially bound %s caption accepted", shape)
		}
	}
	// A legacy pill or an already clear arrangement must not even request new
	// geometry. This also keeps the lazy producer off the common clear path.
	called := false
	source := func() (connectorTextBodies, bool) { called = true; return connectorTextBodies{}, true }
	refineConnectorTextBodies([]connectorTextPlacement{{clear: true}}, nil, nil, nil, nil, source)
	refineConnectorTextBodies([]connectorTextPlacement{{}}, nil, []screenRect{{}}, nil, nil, source)
	if called {
		t.Fatal("fallback/clear path evaluated new geometry")
	}
}

func TestConnectorTextBodyRefinementProtectsWholeBaseline(t *testing.T) {
	rng := rand.New(rand.NewPCG(179, 20261012))
	improved := 0
	for n := 0; n < 250; n++ {
		poly := [][2]float64{{30, 150}, {150, 30}, {270, 150}, {150, 270}}
		body := connectorBodyTestRegion(poly)
		var requests []connectorTextRequest
		var obs []connectorTextObstacle
		var strokes []connectorTextStroke
		for i := 0; i < 6; i++ {
			points := [][2]float64{{rng.Float64()*400 - 50, rng.Float64()*400 - 50}, {rng.Float64()*400 - 50, rng.Float64()*400 - 50}}
			requests = append(requests, connectorTextRequest{index: i, points: points, block: screenTextBlock{width: 20 + rng.Float64()*25, height: 12 + rng.Float64()*8}})
			obs = append(obs, connectorTextStrokeObstacles(i, points, false, 2)...)
			strokes = append(strokes, connectorTextStrokes(i, points, false, 2)...)
		}
		old := resolveConnectorText(requests, []screenRect{{30, 30, 270, 270}}, nil, obs, strokes)
		next := append([]connectorTextPlacement(nil), old...)
		changed := refineConnectorTextBodies(next, requests, nil, obs, strokes, func() (connectorTextBodies, bool) { return body, true })
		for i, p := range next {
			if old[i].clear && p != old[i] {
				t.Fatal("old clear anchor/certificate changed")
			}
			if !changed[i] && p != old[i] {
				t.Fatal("failed refinement changed baseline")
			}
			if changed[i] {
				improved++
				if !p.clear || connectorTextInteriorHits(p.box, body.interiors) || connectorTextStrokeHits(p.box, body.borders) {
					t.Fatal("new placement covers body")
				}
				for j, other := range next {
					if i != j && collides(p.box, []screenRect{other.box}) {
						t.Fatal("new placement covers peer pill")
					}
				}
				for _, stroke := range strokes {
					if stroke.owner != i && connectorTextStrokeHits(p.box, []connectorTextStroke{stroke}) {
						t.Fatal("new placement covers foreign route")
					}
				}
			}
		}
	}
	if improved == 0 {
		t.Fatal("no refinement exercised")
	}
	t.Logf("body refinement improved %d labels in 250 layouts", improved)
}

func connectorTextBodyRegressionDoc() *Document {
	n := &Node{Shape: "composite"}
	for i, p := range [][2]float64{{480, 480}, {0, 160}, {400, 320}, {160, 560}, {320, 560}, {0, 240}} {
		n.Parts = append(n.Parts, &CompositePart{ID: fmt.Sprintf("n%d", i), Shape: "rectangle", Geom: &Geom{W: 80, D: 60, H: 20}, Offset: &WorldPoint{WX: p[0], WY: p[1]}})
	}
	for i := 0; i < 3; i++ {
		n.Connectors = append(n.Connectors, &Connector{From: fmt.Sprintf("n%d", i*2), To: fmt.Sprintf("n%d", i*2+1), Routing: "orthogonal", Arrow: "triangle", Label: "request", LabelFontSize: 13, LabelFontFamily: "Source Sans Pro"})
	}
	return &Document{Nodes: map[string]*Node{"scene": n}}
}

func TestConnectorTextBodyRealRegression(t *testing.T) {
	doc := connectorTextBodyRegressionDoc()
	before, _ := json.Marshal(doc)
	scene := RenderScene(doc)
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatalf("render changed authored scene: %s -> %s", before, after)
	}
	_, report := RenderSceneWithReport(doc)
	after, _ = json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("report simulation changed authored routes")
	}
	if !reflect.DeepEqual(scene, RenderScene(doc)) {
		t.Fatal("report changed the next render")
	}
	if len(scene.ConnectorLabels) != 3 || !reflect.DeepEqual(scene.ConnectorLabels, report.ConnectorLabels) {
		t.Fatal("label/report identity lost")
	}
	refined := 0
	for _, label := range scene.ConnectorLabels {
		if label.Label.Typography.Placement != "clear-within-scope" {
			t.Fatal("body corner still blocked")
		}
		if strings.Contains(label.Label.Typography.Scope, "body-region") {
			refined++
		}
	}
	if refined != 1 {
		t.Fatalf("refined=%d, want 1", refined)
	}
	// One unmeasured node restores the entire prior placement, including its
	// blocked status. No unavailable-geometry certificate may escape.
	doc.Scene().Parts[0].Label = "Unmeasured"
	fallback := RenderScene(doc)
	for _, label := range fallback.ConnectorLabels {
		if strings.Contains(label.Label.Typography.Scope, "body-region") {
			t.Fatal("unknown node font certified")
		}
	}
	if fallback.ConnectorLabels[1].Label.Typography.Placement != "blocked" {
		t.Fatal("legacy fixture no longer exercises fallback")
	}
}

func TestConnectorTextBodyArcBudgetAndTranslation(t *testing.T) {
	parts := []iso25d.CompositePart{{Shape: "circle", Opts: iso25d.ConvertOpts{Width: 2e8}, OffWX: -123.456, OffWY: 789.123, OffWZ: 456.789}}
	infos := connectorBodyTestInfos(parts)
	region, ok := connectorTextBodyRegions(parts, infos)
	if !ok {
		t.Fatal("circle rejected")
	}
	body := iso25d.ShapeBody(parts[0].Shape, parts[0].Opts)
	if body.MaxError <= .05 {
		t.Fatal("fixture did not reach approximation budget")
	}
	tx, ty := partsScreenOrigin(infos)
	ox, oy := projectIso(parts[0].OffWX, parts[0].OffWY, parts[0].OffWZ)
	for i, p := range body.Polygons[0] {
		if region.interiors[0][i] != ([2]float64{p[0] + ox + tx, p[1] + oy + ty}) {
			t.Fatal("root translation mismatch")
		}
		if math.Abs(region.borders[i].radius-(.75+body.MaxError+.03)) > 1e-9 {
			t.Fatal("actual arc error omitted")
		}
	}
}

func TestConnectorTextBodyWorkBudgetKeepsBaseline(t *testing.T) {
	points := make([][2]float64, 2000)
	for i := range points {
		points[i] = [2]float64{float64(i), 50}
	}
	req := connectorTextRequest{index: 0, points: points, block: screenTextBlock{width: 30, height: 20}}
	before := []connectorTextPlacement{{x: 123, y: 50, box: connectorTextBox(req, 123, 50)}}
	after := append([]connectorTextPlacement(nil), before...)
	called := false
	changed := refineConnectorTextBodies(after, []connectorTextRequest{req}, nil, nil,
		[]connectorTextStroke{{owner: 1, a: [2]float64{500, 0}, b: [2]float64{500, 100}, radius: 1}},
		func() (connectorTextBodies, bool) { called = true; return connectorTextBodies{}, true })
	if called || changed[0] || !reflect.DeepEqual(before, after) {
		t.Fatal("work exhaustion changed/certified baseline")
	}
	// Even capped arc producers need a scene-wide storage guard.
	parts := make([]iso25d.CompositePart, 9)
	for i := range parts {
		parts[i] = iso25d.CompositePart{Shape: "circle", Opts: iso25d.ConvertOpts{Width: 2e8}}
	}
	if _, ok := connectorTextBodyRegions(parts, connectorBodyTestInfos(parts)); ok {
		t.Fatal("region point budget not enforced")
	}
}

func TestConnectorTextBodyContentSizeRounding(t *testing.T) {
	p := iso25d.CompositePart{Shape: "rectangle", Opts: iso25d.ConvertOpts{Width: 120, Depth: 90, Height: 30, Label: strings.Repeat("W", 200), FontFamily: "Source Sans Pro", FontSize: 13.236, Icon: "icon", IconAnchor: "topLeft"}}
	region, ok := connectorTextBodyRegions([]iso25d.CompositePart{p}, connectorBodyTestInfos([]iso25d.CompositePart{p}))
	if !ok {
		t.Fatal("known text rejected")
	}
	// The final four borders belong to the text quad. A size-rounding
	// difference of .004 expands this long line far beyond the .03 guard.
	for _, s := range region.borders[len(region.borders)-4:] {
		if s.radius <= .2 {
			t.Fatal("long-line font-size rounding was not bounded")
		}
	}
}
