package isotopo

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

func isoCrowdingTestRoute(index int, points ...[2]float64) ConnectorRoute {
	return ConnectorRoute{Index: index, Drawing: &ConnectorDrawing{Kind: "polyline", Points: points, StrokeWidth: 2,
		Spatial: &ConnectorSpatialVisibility{Mode: "depth", ClipRegion: screenRegion{planCrowdingRect(screenRect{-1000, -1000, 1000, 1000})}}}}
}

func TestIsoRouteCrowdingVisibleGeometry(t *testing.T) {
	a := isoCrowdingTestRoute(2, [2]float64{0, 0}, [2]float64{100, 0})
	for _, tc := range []struct {
		name   string
		points [][2]float64
		want   bool
	}{
		{"coincident", [][2]float64{{20, 0}, {80, 0}}, true},
		{"parallel-close", [][2]float64{{20, 7}, {80, 7}}, true},
		{"parallel-clear", [][2]float64{{20, 9}, {80, 9}}, false},
		{"transverse-crossing", [][2]float64{{50, -30}, {50, 30}}, false},
		{"diagonal-empty-corner", [][2]float64{{20, 20}, {80, 80}}, false},
		{"short-contact", [][2]float64{{98, 0}, {130, 0}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := observeIsoRouteCrowding([]ConnectorRoute{a, isoCrowdingTestRoute(7, tc.points...)}, nil)
			if r.Status != "complete" || (len(r.Conflicts) > 0) != tc.want {
				t.Fatalf("unexpected observation: %+v", r)
			}
			for _, c := range r.Conflicts {
				if c.ConnectorIndex != 2 || c.PeerIndex != 7 || c.Kind != "parallel-stroke" || c.Area <= 0 {
					t.Fatalf("lost identity: %+v", c)
				}
			}
		})
	}
	b := isoCrowdingTestRoute(7, [2]float64{20, 0}, [2]float64{80, 0})
	b.Drawing.Spatial.ClipRegion = nil
	if r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil); r.Status != "complete" || len(r.Conflicts) != 0 {
		t.Fatal("hidden stroke became a visible clearance obstacle", r)
	}
	// A clip passing through the clearance halo but not through ink is hidden.
	b.Drawing.Spatial.ClipRegion = screenRegion{planCrowdingRect(screenRect{0, 2, 100, 4})}
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) != 0 {
		t.Fatal("expanded before clipping ink")
	}
	b.Drawing.Spatial.ClipRegion = screenRegion{planCrowdingRect(screenRect{30, -10, 70, 10})}
	r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil)
	// Analytic clipped rectangle [30,70] x [-1,1], expanded by three on
	// each side, is contained inside a's expanded stroke: 46 * 8 units².
	if len(r.Conflicts) != 1 || math.Abs(r.Conflicts[0].Area-368) > 1e-7 {
		t.Fatalf("wrong independently known clipped area: %+v", r)
	}
}

func TestIsoRouteCrowdingSubdivisionTranslationAndDocks(t *testing.T) {
	a := isoCrowdingTestRoute(0, [2]float64{0, 0}, [2]float64{100, 0})
	b := isoCrowdingTestRoute(1, [2]float64{20, 5}, [2]float64{80, 5})
	r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil)
	a.Drawing.Points = [][2]float64{{0, 0}, {30, 0}, {30, 0}, {70, 0}, {100, 0}}
	if q := observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil); !reflect.DeepEqual(q, r) {
		t.Fatal("subdivision changed severity", q, r)
	}
	for _, d := range []*ConnectorDrawing{a.Drawing, b.Drawing} {
		d.ProjectionOrigin = [2]float64{1e6, -1e6}
		for i := range d.Points {
			d.Points[i][0] += 1e6
			d.Points[i][1] -= 1e6
		}
	}
	if q := observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil); len(q.Conflicts) != 1 || math.Abs(q.Conflicts[0].Area-r.Conflicts[0].Area) > 1e-5 {
		t.Fatal("root frame changed clearance", q)
	}
	attach := func(r *ConnectorRoute, from, to string, z float64) {
		w := [3]float64{z, z, z}
		r.Drawing.Attachments = &ConnectorAttachments{Source: ConnectorEndpointAttachment{InstanceID: from, Point: r.Drawing.Points[0], World: ConnectorAttachmentWorld{Status: "known", Point: &w}}, Target: ConnectorEndpointAttachment{InstanceID: to}}
	}
	a = isoCrowdingTestRoute(0, [2]float64{0, 0}, [2]float64{100, 0})
	b = isoCrowdingTestRoute(1, [2]float64{0, 0}, [2]float64{12, 0}, [2]float64{12, 100})
	attach(&a, "a", "b", 0)
	attach(&b, "a", "c", 0)
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) != 0 {
		t.Fatal("normal local shared dock reported")
	}
	b.Drawing.Points[1][0] = 70
	b.Drawing.Points[2][0] = 70
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) == 0 {
		t.Fatal("shared node exempted a long overlap")
	}
	b.Drawing.Points[1][0] = 12
	b.Drawing.Points[2][0] = 12
	attach(&b, "a", "c", 10)
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) == 0 {
		t.Fatal("same projection at different depth received shared-world exemption")
	}
	attach(&b, "a", "b", 0)
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) == 0 {
		t.Fatal("parallel semantic edge exempted")
	}
}

func TestIsoRouteCrowdingArrowAndUnknown(t *testing.T) {
	a := isoCrowdingTestRoute(0, [2]float64{0, 0}, [2]float64{100, 0})
	b := isoCrowdingTestRoute(1, [2]float64{70, 12}, [2]float64{100, 12})
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) != 0 {
		t.Fatal("strokes already crowded")
	}
	a.Drawing.Arrow = [][2]float64{{100, 0}, {84, 8}, {84, -8}}
	a.Drawing.Spatial.Arrow = &ConnectorArrowVisibility{ClipRegion: a.Drawing.Spatial.ClipRegion}
	r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil)
	if len(r.Conflicts) != 1 || r.Conflicts[0].Kind != "arrow-stroke" {
		t.Fatal("missed visible arrow", r)
	}
	a.Drawing.Spatial.Arrow.ClipRegion = nil
	if len(observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil).Conflicts) != 0 {
		t.Fatal("hidden arrow reported")
	}
	for _, change := range []func(*ConnectorDrawing){
		func(d *ConnectorDrawing) { d.Spatial.Mode = "legacy" }, func(d *ConnectorDrawing) { d.Kind = "quadratic" },
		func(d *ConnectorDrawing) { d.Points[0][0] = math.NaN() }, func(d *ConnectorDrawing) { d.Spatial.ClipRegion[0][0][0] = math.Inf(1) },
		func(d *ConnectorDrawing) { d.Points = make([][2]float64, 513) },
	} {
		c := isoCrowdingTestRoute(0, [2]float64{0, 0}, [2]float64{100, 0})
		change(c.Drawing)
		q := observeIsoRouteCrowding([]ConnectorRoute{c, b}, nil)
		if q.Status != "unresolved" || q.Reason == "" || isoCrowdingDefects(q).noWorseThan(visualDefects{}) {
			t.Fatal("unknown counted as clean", q)
		}
	}
	raw, _ := json.Marshal([]ConnectorRoute{a, b})
	observeIsoRouteCrowding([]ConnectorRoute{a, b}, nil)
	after, _ := json.Marshal([]ConnectorRoute{a, b})
	if string(raw) != string(after) {
		t.Fatal("observer mutated submitted geometry")
	}
}

const isoCrowdingRepairFixture = `nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 60, d: 60, h: 30}, offset: {wx: 200, wy: 500}}
      - {id: b, shape: rectangle, geom: {w: 60, d: 60, h: 30}, offset: {wx: 400, wy: 200}}
      - {id: c, shape: rectangle, geom: {w: 60, d: 60, h: 30}, offset: {wx: 500}}
    connectors:
      - {from: a, to: b, routing: orthogonal, arrow: triangle}
      - {from: a, to: c, routing: orthogonal, arrow: triangle}
`

func TestIsoRouteCrowdingPublicPaintOrder(t *testing.T) {
	a := isoCrowdingTestRoute(0, [2]float64{0, 0}, [2]float64{100, 0})
	b := isoCrowdingTestRoute(1, [2]float64{20, 0}, [2]float64{80, 0})
	a.Drawing.Spatial = nil
	b.Drawing.Spatial = nil
	paint := []connectorPaintPart{{id: "body", region: screenRegion{planCrowdingRect(screenRect{-20, -20, 120, 20})}}}
	if r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, paint); r.Status != "complete" || len(r.Conflicts) != 0 {
		t.Fatal("later opaque fill did not hide full-width stroke", r)
	}
	a.Drawing.PaintBeforePart = 1
	b.Drawing.PaintBeforePart = 1
	if r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, paint); r.Status != "complete" || len(r.Conflicts) != 1 {
		t.Fatal("earlier body hid later stroke", r)
	}
	a.Drawing.PaintBeforePart = 0
	b.Drawing.PaintBeforePart = 0
	a.Drawing.Arrow = [][2]float64{{100, 0}, {84, 8}, {84, -8}}
	b.Drawing.Arrow = a.Drawing.Arrow
	if r := observeIsoRouteCrowding([]ConnectorRoute{a, b}, paint); r.Status != "complete" || len(r.Conflicts) != 1 || r.Conflicts[0].Kind != "arrow-arrow" {
		t.Fatal("public overlay arrows incorrectly clipped by body", r)
	}
	paint[0].uncertain = true
	if observeIsoRouteCrowding([]ConnectorRoute{a, b}, paint).Status != "unresolved" {
		t.Fatal("uncertain paint counted as complete")
	}
	paint[0].uncertain = false
	paint[0].region = screenRegion{{{0, 0}, {100, 0}, {40, 40}, {100, 100}, {0, 100}}}
	if observeIsoRouteCrowding([]ConnectorRoute{a, b}, paint).Status != "unresolved" {
		t.Fatal("concavity silently replaced by convex hull")
	}
}

func TestIsoRouteCrowdingRepairAndConsumers(t *testing.T) {
	d := parseWSH(t, isoCrowdingRepairFixture)
	before, report := RenderSceneWithReport(d)
	plain := RenderScene(d)
	if before.IsoRouteCrowding.Status != "complete" || len(before.IsoRouteCrowding.Conflicts) == 0 {
		t.Fatal("fixture must have visible crowding")
	}
	if plain.IsoRouteCrowding != nil || before.SVG != plain.SVG || !reflect.DeepEqual(before.IsoRouteCrowding, report.IsoRouteCrowding) {
		t.Fatal("diagnostics changed preview or report disagreed")
	}
	if r := Readability(d); r.Crossings != 0 || r.Tunnels != 0 {
		t.Fatal("fixture must expose old metric's blind spot", r)
	}
	raw, _ := json.Marshal(d)
	RenderSceneWithReport(d)
	unchanged, _ := json.Marshal(d)
	if string(raw) != string(unchanged) {
		t.Fatal("observation mutated author document")
	}
	defects := sceneVisualDefects(d.Scene(), d.Theme, d.Canvas)
	if !repairIsoRouteCrowding(d, d.Scene()) {
		t.Fatal("automatic elbow failed to clear visible overlap")
	}
	after, report := RenderSceneWithReport(d)
	if len(after.IsoRouteCrowding.Conflicts) != 0 || !isoCrowdingRouteQuality(before, after) || !sceneVisualDefects(d.Scene(), d.Theme, d.Canvas).noWorseThan(defects) {
		t.Fatal("repair did not preserve quality")
	}
	for _, v := range report.Defects {
		if v.Kind == "iso-route-clearance" {
			t.Fatal("stale repaired conflict")
		}
	}
	if repairIsoRouteCrowding(d, d.Scene()) {
		t.Fatal("repair was not a fixpoint")
	}
	expanded := after
	root, _ := findRootSVG(expanded.SVG)
	expanded.SVG = strings.Replace(expanded.SVG, root.tag(), replaceAttr(root.tag(), "viewBox", "0 0 2000 2000"), 1)
	if isoCrowdingRouteQuality(before, expanded) {
		t.Fatal("larger export frame counted as unchanged quality")
	}

	fresh := parseWSH(t, isoCrowdingRepairFixture)
	RepairScene(fresh)
	if RenderScene(fresh).SVG != after.SVG {
		t.Fatal("public repair loop missed accepted route")
	}
}

func TestIsoRouteCrowdingPreservesAuthorAndUnknown(t *testing.T) {
	for _, mode := range []string{"elbow", "bend", "waypoints", "waypoints3d", "fixed", "straight", "unknown-pill", "unknown-screen"} {
		t.Run(mode, func(t *testing.T) {
			d := parseWSH(t, isoCrowdingRepairFixture)
			for _, c := range d.Scene().Connectors {
				switch mode {
				case "elbow":
					c.Elbow = "xFirst"
				case "bend":
					c.Bend = &WorldPoint{}
				case "waypoints":
					c.Waypoints = []WorldPoint{{WX: 300, WY: 200}}
				case "waypoints3d":
					c.Waypoints3D = []WorldPoint{{WX: 300, WY: 200, WZ: 20}}
				case "fixed":
					c.From += ".port-right"
				case "straight":
					c.Routing = "straight"
				case "unknown-pill":
					c.Label = "Unknown font"
				case "unknown-screen":
					d.Scene().Parts[0].Label = "Unbound"
					d.Scene().Parts[0].Style = &Style{Text: &Text{Orient: "screen"}}
				}
			}
			before, _ := json.Marshal(d)
			if repairIsoRouteCrowding(d, d.Scene()) {
				t.Fatal("changed constrained or unmeasured content")
			}
			after, _ := json.Marshal(d)
			if string(before) != string(after) {
				t.Fatal("rejected candidate changed source")
			}
		})
	}
}

func TestIsoRouteCrowdingRejectsMisleadingWin(t *testing.T) {
	d := parseWSH(t, isoCrowdingRepairFixture)
	for i, p := range d.Scene().Parts {
		p.Offset = &WorldPoint{WX: []float64{0, 400, 140}[i], WY: []float64{0, 0, 80}[i]}
	}
	before := renderSceneObserved(d, true)
	original, _ := json.Marshal(d)
	d.Scene().Connectors[1].Elbow = "yFirst"
	after := renderSceneObserved(d, true)
	if len(after.IsoRouteCrowding.Conflicts) >= len(before.IsoRouteCrowding.Conflicts) {
		t.Fatal("fixture must tempt a count-only optimizer")
	}
	if isoCrowdingRouteQuality(before, after) {
		t.Fatal("accepted a much longer route with changed endpoint/arrow")
	}
	d.Scene().Connectors[1].Elbow = ""
	if repairIsoRouteCrowding(d, d.Scene()) {
		t.Fatal("accepted misleading improvement")
	}
	final, _ := json.Marshal(d)
	if string(original) != string(final) {
		t.Fatal("failed candidate leaked")
	}
}

func TestIsoRouteCrowdingPerturbationSafety(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 59217))
	changed := 0
	for i := 0; i < 96; i++ {
		d := parseWSH(t, isoCrowdingRepairFixture)
		if i > 0 {
			for _, p := range d.Scene().Parts {
				p.Offset.WX += (rng.Float64() - .5) * 40
				p.Offset.WY += (rng.Float64() - .5) * 40
				p.Geom.W += rng.Float64() * 10
				p.Geom.D += rng.Float64() * 10
			}
		}
		for _, c := range d.Scene().Connectors {
			if i%3 == 1 {
				width := 2 + float64(i%5)
				c.Stroke = &Stroke{Width: &width, Dash: "dashed"}
			}
			if i%4 == 2 {
				c.Arrow = "none"
			}
		}
		original, _ := json.Marshal(d)
		before := renderSceneObserved(d, true)
		defects := sceneVisualDefects(d.Scene(), d.Theme, d.Canvas)
		acted := repairIsoRouteCrowding(d, d.Scene())
		after := renderSceneObserved(d, true)
		if !sceneVisualDefects(d.Scene(), d.Theme, d.Canvas).noWorseThan(defects) {
			t.Fatalf("case %d worsened a defect", i)
		}
		if acted {
			changed++
			if !isoCrowdingRouteQuality(before, after) || len(after.IsoRouteCrowding.Conflicts) >= len(before.IsoRouteCrowding.Conflicts) {
				t.Fatalf("case %d violated accepted quality", i)
			}
		} else {
			final, _ := json.Marshal(d)
			if string(original) != string(final) {
				t.Fatalf("case %d leaked trial", i)
			}
		}
		fresh := &Document{}
		if err := json.Unmarshal(original, fresh); err != nil {
			t.Fatal(err)
		}
		repairIsoRouteCrowding(fresh, fresh.Scene())
		if RenderScene(fresh).SVG != after.SVG || repairIsoRouteCrowding(d, d.Scene()) {
			t.Fatalf("case %d not deterministic/fixed", i)
		}
	}
	if changed == 0 {
		t.Fatal("no actual improvement exercised")
	}
}

func TestIsoRouteCrowdingCannotTradeOverlapForRemoteCrossing(t *testing.T) {
	d := parseWSH(t, isoCrowdingRepairFixture)
	for i, p := range d.Scene().Parts {
		p.Offset = &WorldPoint{WX: []float64{200, 300, 600}[i], WY: []float64{600, 0, 100}[i]}
	}
	d.Scene().Parts = append(d.Scene().Parts, &CompositePart{ID: "free", Shape: "rectangle", Geom: &Geom{W: 60, D: 60, H: 30}, Offset: &WorldPoint{WX: 100, WY: 200}})
	before := renderSceneObserved(d, true)
	raw, _ := json.Marshal(d)
	d.Scene().Connectors[1].Elbow = "yFirst"
	after := renderSceneObserved(d, true)
	if len(after.IsoRouteCrowding.Conflicts) >= len(before.IsoRouteCrowding.Conflicts) || !isoCrowdingRouteQuality(before, after) {
		t.Fatal("fixture must tempt the previous route-quality gate")
	}
	if isoCrowdingDefects(after.IsoRouteCrowding).noWorseThan(isoCrowdingDefects(before.IsoRouteCrowding)) {
		t.Fatal("long overlap traded for a new remote X crossing")
	}
	d.Scene().Connectors[1].Elbow = ""
	if repairIsoRouteCrowding(d, d.Scene()) {
		t.Fatal("introduced a remote crossing between adjacent edges")
	}
	final, _ := json.Marshal(d)
	if string(raw) != string(final) {
		t.Fatal("rejected crossing trial leaked")
	}
}
