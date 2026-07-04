package isotopo

// inject_router2_test.go — acceptance + feature-interaction tests for the
// opt-in v2 orthogonal router (routing: "orthogonal2", inject_router2.go).
//
// The headline test is the evaluation gate: three scenes where v1 orthogonal
// demonstrably produces a tunnel or a crossing (asserted, not assumed), scored
// with the real-route scorecard (EvaluateIso). v2 must improve the weighted
// crossings+tunnels sum by ≥30% overall and must not make ANY single scene
// worse. Weights follow the plan router's routeCost: tunnel 1000, crossing 100.

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

// r2Weighted is the crossings+tunnels weighted sum, routeCost口径:
// an edge tunnelling unrelated nodes costs 1000, an edge-edge crossing 100.
func r2Weighted(rep *PlanReport) float64 {
	return 1000*float64(rep.EdgesThroughNodes) + 100*float64(rep.Crossings)
}

// r2Scenes are the evaluation scenarios. Each template has a ROUTING
// placeholder so the identical geometry can be scored under v1 and v2.
var r2Scenes = []struct {
	name string
	tpl  string
}{
	{
		// Collinear a→c with b squarely in the corridor: BOTH v1 elbow
		// candidates degenerate to the same straight run through b (a
		// tunnel). v2 must detour around b.
		name: "tunnel-collinear",
		tpl: `nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 0,   wy: 0 } }
      - { id: b, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 200, wy: 0 } }
      - { id: c, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 400, wy: 0 } }
    connectors:
      - { from: a, to: c, routing: ROUTING }
`,
	},
	{
		// A long horizontal bus a→b and a vertical drop c→d that cuts
		// straight across it: v1 routes both as single collinear segments
		// that properly cross mid-span. v2 routes the later edge around the
		// bus's endpoint instead of crossing it.
		name: "crossing-bus",
		tpl: `nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 0,   wy: 100 } }
      - { id: b, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 400, wy: 100 } }
      - { id: c, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 200, wy: -100 } }
      - { id: d, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 200, wy: 300 } }
    connectors:
      - { from: a, to: b, routing: ROUTING }
      - { from: c, to: d, routing: ROUTING }
`,
	},
	{
		// An L-shaped a→b with a tall wall spanning BOTH elbow corridors:
		// the v1 xFirst and yFirst staircases each tunnel the wall, so the
		// 2-candidate greedy cannot escape. v2 must insert extra segments
		// and go around the wall's end.
		name: "tunnel-wall-L",
		tpl: `nodes:
  scene:
    shape: composite
    parts:
      - { id: a,    shape: rectangle, geom: { w: 60, d: 60,  h: 40 }, offset: { wx: 0,   wy: 0 } }
      - { id: wall, shape: rectangle, geom: { w: 60, d: 400, h: 40 }, offset: { wx: 150, wy: -150 } }
      - { id: b,    shape: rectangle, geom: { w: 60, d: 60,  h: 40 }, offset: { wx: 400, wy: 120 } }
    connectors:
      - { from: a, to: b, routing: ROUTING }
`,
	},
}

func r2Eval(t *testing.T, tpl, routing string) *PlanReport {
	t.Helper()
	rep, err := EvaluateIsoText("yaml", []byte(strings.ReplaceAll(tpl, "ROUTING", routing)))
	if err != nil {
		t.Fatalf("evaluate (%s): %v", routing, err)
	}
	return rep
}

// TestRouter2_ImprovesCrossingsAndTunnels is the WS-B gate: over scenes where
// v1 orthogonal produces crossings/tunnels, v2 improves the weighted sum by
// ≥30% and no single scene gets worse.
func TestRouter2_ImprovesCrossingsAndTunnels(t *testing.T) {
	var v1Sum, v2Sum float64
	for _, sc := range r2Scenes {
		rep1 := r2Eval(t, sc.tpl, "orthogonal")
		rep2 := r2Eval(t, sc.tpl, "orthogonal2")
		w1, w2 := r2Weighted(rep1), r2Weighted(rep2)
		t.Logf("%s: v1 tunnels=%d crossings=%d (w=%.0f) → v2 tunnels=%d crossings=%d (w=%.0f)",
			sc.name, rep1.EdgesThroughNodes, rep1.Crossings, w1,
			rep2.EdgesThroughNodes, rep2.Crossings, w2)
		if w1 == 0 {
			t.Errorf("%s: scene is supposed to defeat the v1 router but scored clean — test scenario invalid", sc.name)
		}
		if w2 > w1 {
			t.Errorf("%s: v2 REGRESSED the weighted score: v1=%.0f v2=%.0f", sc.name, w1, w2)
		}
		v1Sum += w1
		v2Sum += w2
	}
	if v1Sum <= 0 {
		t.Fatal("no v1 defects measured; gate meaningless")
	}
	if v2Sum > 0.7*v1Sum {
		t.Errorf("v2 improvement below the 30%% gate: v1 weighted sum=%.0f, v2=%.0f (need ≤ %.0f)",
			v1Sum, v2Sum, 0.7*v1Sum)
	}
}

// TestRouter2_Deterministic — same input, same output, byte for byte.
func TestRouter2_Deterministic(t *testing.T) {
	src := []byte(strings.ReplaceAll(r2Scenes[2].tpl, "ROUTING", "orthogonal2"))
	render := func() string {
		doc, err := LoadInput(context.Background(), "yaml", src, LayoutDagre)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		return RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	}
	if a, b := render(), render(); a != b {
		t.Error("orthogonal2 render is not deterministic across runs")
	}
}

// TestRouter2_ElbowIgnored — feature interaction: elbow biases the v1
// 2-candidate pick; orthogonal2 auto-routes, so xFirst and yFirst must yield
// the identical document.
func TestRouter2_ElbowIgnored(t *testing.T) {
	render := func(elbow string) string {
		tpl := strings.ReplaceAll(r2Scenes[2].tpl, "routing: ROUTING", "routing: orthogonal2, elbow: "+elbow)
		doc, err := LoadInput(context.Background(), "yaml", []byte(tpl), LayoutDagre)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		return RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	}
	if render("xFirst") != render("yFirst") {
		t.Error("orthogonal2 must ignore elbow (v2 supersedes the 2-candidate pick)")
	}
}

// TestRouter2_WaypointsOverride — feature interaction: explicit waypoints
// keep their drawio-style override semantics under orthogonal2 (the searched
// route steps aside; the user's corner wins).
func TestRouter2_WaypointsOverride(t *testing.T) {
	tpl := strings.ReplaceAll(r2Scenes[0].tpl,
		"routing: ROUTING",
		"routing: orthogonal2, waypoints: [ { wx: 230, wy: -80 } ] ")
	doc, err := LoadInput(context.Background(), "yaml", []byte(tpl), LayoutDagre)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	// data-route world coords carry the user corner verbatim (%.3f).
	if !strings.Contains(svg, ",230.000,-80.000") {
		t.Error("orthogonal2 route did not thread the explicit waypoint (waypoints must supersede the auto route)")
	}
}

// TestRouter2_CrossTierRiser — feature interaction: endpoints at different wz
// still route (ground plane = min of the two, riser at the elevated stub),
// and the evaluator can score the result.
func TestRouter2_CrossTierRiser(t *testing.T) {
	src := `nodes:
  scene:
    shape: composite
    parts:
      - { id: lo, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 0,   wy: 0, wz: 0 } }
      - { id: hi, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 300, wy: 0, wz: 80 } }
    connectors:
      - { from: hi, to: lo, routing: orthogonal2, arrow: triangle }
`
	doc, err := LoadInput(context.Background(), "yaml", []byte(src), LayoutDagre)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	if !strings.Contains(svg, "data-route=") {
		t.Fatal("orthogonal2 cross-tier connector emitted no data-route")
	}
	if _, err := EvaluateIsoText("yaml", []byte(src)); err != nil {
		t.Fatalf("evaluate cross-tier: %v", err)
	}
}

// TestRouter2_AdaptiveChannelW — the v2 fan-out channel adapts to the source
// face span (v1 hard-codes 14 world units, inject.go). A 30-deep face hosting
// 3 exits must spread them span/(n+1)=7.5 apart, all inside the face.
func TestRouter2_AdaptiveChannelW(t *testing.T) {
	src := `nodes:
  scene:
    shape: composite
    parts:
      - { id: hub, shape: rectangle, geom: { w: 60, d: 30, h: 40 }, offset: { wx: 0, wy: 0 } }
      - { id: t1, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 400, wy: -100 } }
      - { id: t2, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 400, wy: 40 } }
      - { id: t3, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 400, wy: 140 } }
    connectors:
      - { from: hub, to: t1, routing: orthogonal2 }
      - { from: hub, to: t2, routing: orthogonal2 }
      - { from: hub, to: t3, routing: orthogonal2 }
`
	doc, err := LoadInput(context.Background(), "yaml", []byte(src), LayoutDagre)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	routes := isoRealRoutes(cloneSceneForEval(doc.Scene()), doc.Theme, doc.Canvas)
	if len(routes) != 3 {
		t.Fatalf("expected 3 real routes, got %d", len(routes))
	}
	// The hub's right face spans y ∈ [0, 30] (anchor mid 15). With 3 exits
	// the v2 channel is 30/(3+1) = 7.5 → staggered starts at y = 7.5, 15,
	// 22.5. v1's fixed 14 would have put them at 1 and 29, hugging the face
	// edges.
	want := []float64{7.5, 15, 22.5}
	for ci := 0; ci < 3; ci++ {
		pts, ok := routes[ci]
		if !ok || len(pts) < 2 {
			t.Fatalf("connector %d: missing world route", ci)
		}
		if math.Abs(pts[0][1]-want[ci]) > 0.01 {
			t.Errorf("connector %d: source exit y=%.3f, want %.3f (adaptive channel = faceSpan/(n+1))",
				ci, pts[0][1], want[ci])
		}
		if pts[0][1] < 0 || pts[0][1] > 30 {
			t.Errorf("connector %d: exit y=%.3f escapes the 30-unit face", ci, pts[0][1])
		}
	}
}

// TestRouter2_EnumWiring — capabilities/validator wiring: orthogonal2 is a
// discoverable enum value and validates clean; the pre-existing enums are
// still intact and a typo still gets a suggestion.
func TestRouter2_EnumWiring(t *testing.T) {
	caps := CapabilityReport()
	found := false
	for _, r := range caps.Enums["connector_routing"] {
		if r == "orthogonal2" {
			found = true
		}
	}
	if !found {
		t.Error("capabilities connector_routing enum is missing orthogonal2")
	}

	src := `nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 0,   wy: 0 } }
      - { id: b, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 200, wy: 0 } }
    connectors:
      - { from: a, to: b, routing: %s }
`
	doc, err := LoadInput(context.Background(), "yaml", []byte(fmt.Sprintf(src, "orthogonal2")), LayoutDagre)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, is := range Validate(doc) {
		if strings.Contains(is.Path, ".routing") {
			t.Errorf("routing orthogonal2 should validate clean, got: %s", is.Message)
		}
	}
	doc2, err := LoadInput(context.Background(), "yaml", []byte(fmt.Sprintf(src, "orthogonal2x")), LayoutDagre)
	if err != nil {
		t.Fatalf("load typo doc: %v", err)
	}
	sawSuggest := false
	for _, is := range Validate(doc2) {
		if strings.Contains(is.Path, ".routing") && is.Suggest == "orthogonal2" {
			sawSuggest = true
		}
	}
	if !sawSuggest {
		t.Error(`typo "orthogonal2x" should suggest orthogonal2`)
	}
}

// TestRouter2_RouteFallsBackNil — unit-level: degenerate anchors return nil so
// the caller keeps the v1 route (never panic, never a 1-point path).
func TestRouter2_RouteFallsBackNil(t *testing.T) {
	a := routerAnchor{ID: "a", X: 10, Y: 10, DX: 1, DY: 0, Z: 0}
	if got := route(a, a, nil, nil); got != nil {
		t.Errorf("coincident anchors must return nil, got %v", got)
	}
}

// TestRouter2_RouteAvoidsObstacleUnit — unit-level: a blocker straddling the
// direct corridor forces a detour whose segments never hit the blocker, while
// with no obstacles the route is the direct segment.
func TestRouter2_RouteAvoidsObstacleUnit(t *testing.T) {
	src := routerAnchor{ID: "s", X: 0, Y: 30, DX: 1, DY: 0, Z: 0}
	dst := routerAnchor{ID: "t", X: 300, Y: 30, DX: -1, DY: 0, Z: 0}

	direct := route(src, dst, nil, nil)
	if len(direct) != 2 {
		t.Fatalf("unobstructed route should be a single segment, got %v", direct)
	}

	blocker := planRect{id: "w", x: 120, y: -60, w: 60, d: 200, h: 40}
	detour := route(src, dst, []planRect{blocker}, nil)
	if len(detour) < 4 {
		t.Fatalf("blocked route should detour with extra corners, got %v", detour)
	}
	for i := 1; i < len(detour); i++ {
		if routeHitsRect([][2]float64{detour[i-1], detour[i]}, blocker) {
			t.Fatalf("detour segment %d still tunnels the blocker: %v", i, detour)
		}
	}
}
