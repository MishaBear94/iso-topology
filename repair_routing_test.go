package isotopo

// repair_routing_test.go — WS-G acceptance for the routing actuator
// (repair_routing.go) and the in-group overlap extension (repair.go):
//   - crossing/tunnel defects on orthogonal connectors are repaired by a
//     measured upgrade to orthogonal2, and roll back when v2 cannot win;
//   - group-internal sibling overlaps are separated under the conservative
//     measured-improvement guard;
//   - the loop is a fixpoint (repair twice == repair once) and R is monotone
//     non-decreasing across the whole sample corpus.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// routingScene parses one of the r2 evaluation scenes (inject_router2_test.go)
// with every ROUTING placeholder set to plain orthogonal — the geometry that
// demonstrably defeats the v1 router.
func routingScene(t *testing.T, tpl string) *Document {
	t.Helper()
	doc, err := Parse([]byte(strings.ReplaceAll(tpl, "ROUTING", "orthogonal")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

// The tunnel-collinear scene: v1 orthogonal runs a→c straight through b.
// Repair must escalate the connector to orthogonal2 and clear the tunnel.
func TestRepairRouting_ClearsTunnel(t *testing.T) {
	doc := routingScene(t, r2Scenes[0].tpl)
	before := Readability(doc)
	if before.Tunnels == 0 {
		t.Fatal("fixture should start with a tunnelling edge")
	}
	_, iters := RepairScene(doc)
	if iters == 0 {
		t.Fatal("repair should have acted")
	}
	if got := doc.Scene().Connectors[0].Routing; got != "orthogonal2" {
		t.Fatalf("connector should be escalated to orthogonal2, got %q", got)
	}
	after := Readability(doc)
	if after.Tunnels != 0 {
		t.Fatalf("repair must clear the tunnel, still %d", after.Tunnels)
	}
	if after.Score <= before.Score {
		t.Fatalf("repair must raise R: %.3f → %.3f", before.Score, after.Score)
	}
}

// The crossing-bus scene: two v1 orthogonal edges properly cross mid-span.
// Repair must reduce the weighted defect sum to zero.
func TestRepairRouting_ClearsCrossing(t *testing.T) {
	doc := routingScene(t, r2Scenes[1].tpl)
	before := Readability(doc)
	if before.Crossings == 0 {
		t.Fatal("fixture should start with an edge crossing")
	}
	_, iters := RepairScene(doc)
	if iters == 0 {
		t.Fatal("repair should have acted")
	}
	after := Readability(doc)
	if s := routingDefectSum(after); s != 0 {
		t.Fatalf("repair must zero the weighted routing-defect sum, still %.1f (tunnels=%d crossings=%d)",
			s, after.Tunnels, after.Crossings)
	}
	if after.Score <= before.Score {
		t.Fatalf("repair must raise R: %.3f → %.3f", before.Score, after.Score)
	}
	upgraded := 0
	for _, c := range doc.Scene().Connectors {
		if c.Routing == "orthogonal2" {
			upgraded++
		}
	}
	if upgraded == 0 {
		t.Fatal("no connector was escalated to orthogonal2")
	}
}

// A hand-authored route (waypoints) is the user's call: even when implicated
// in a defect it must never be rerouted.
func TestRepairRouting_RespectsHandRoutes(t *testing.T) {
	tpl := strings.ReplaceAll(r2Scenes[0].tpl,
		"routing: ROUTING",
		"routing: orthogonal, waypoints: [ { wx: 230, wy: 30 } ]")
	doc, err := Parse([]byte(tpl))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if Readability(doc).Tunnels == 0 {
		t.Fatal("fixture should start with a tunnelling edge")
	}
	RepairScene(doc)
	if got := doc.Scene().Connectors[0].Routing; got != "orthogonal" {
		t.Fatalf("a waypointed connector must keep its routing, got %q", got)
	}
}

// Rollback: an orthogonal edge crossing a STRAIGHT diagonal. The v2 router
// only prices crossings against other routed (orthogonal) edges, so the
// upgrade cannot strictly reduce the defect sum — the actuator must roll the
// connector back and leave the scene byte-identical.
func TestRepairRouting_RollsBackWithoutStrictWin(t *testing.T) {
	src := `nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 0,   wy: 0 } }
      - { id: b, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 400, wy: 200 } }
      - { id: c, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 200, wy: -100 } }
      - { id: d, shape: rectangle, geom: { w: 60, d: 60, h: 40 }, offset: { wx: 200, wy: 300 } }
    connectors:
      - { from: a, to: b, routing: straight }
      - { from: c, to: d, routing: orthogonal }
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	before := Readability(doc)
	if before.Crossings == 0 {
		t.Skip("scenario no longer crosses — geometry drifted, rebuild the fixture")
	}
	preSVG := RenderWithCanvas(cloneSceneForEval(doc.Scene()), doc.Theme, doc.Canvas, nil)
	RepairScene(doc)
	if got := doc.Scene().Connectors[1].Routing; got == "orthogonal2" {
		// Only legitimate if the upgrade genuinely improved the defect sum.
		after := Readability(doc)
		if routingDefectSum(after) >= routingDefectSum(before) {
			t.Fatalf("kept an upgrade with no strict win (defect sum %.1f → %.1f)",
				routingDefectSum(before), routingDefectSum(after))
		}
		return
	}
	postSVG := RenderWithCanvas(cloneSceneForEval(doc.Scene()), doc.Theme, doc.Canvas, nil)
	if preSVG != postSVG {
		t.Fatal("rolled-back repair must leave the render byte-identical")
	}
}

// Fixpoint (no oscillation): running the repair loop a second time on the
// routing scenes and the group-overlap scene must be a strict no-op with a
// byte-identical render.
func TestRepair_FixpointNoOscillation(t *testing.T) {
	scenes := map[string]string{
		"tunnel-collinear": strings.ReplaceAll(r2Scenes[0].tpl, "ROUTING", "orthogonal"),
		"crossing-bus":     strings.ReplaceAll(r2Scenes[1].tpl, "ROUTING", "orthogonal"),
		"tunnel-wall-L":    strings.ReplaceAll(r2Scenes[2].tpl, "ROUTING", "orthogonal"),
		"group-overlap":    groupOverlapSrc,
	}
	for _, name := range []string{"tunnel-collinear", "crossing-bus", "tunnel-wall-L", "group-overlap"} {
		doc, err := Parse([]byte(scenes[name]))
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		RepairScene(doc)
		once := RenderWithCanvas(cloneSceneForEval(doc.Scene()), doc.Theme, doc.Canvas, nil)
		_, iters2 := RepairScene(doc)
		if iters2 != 0 {
			t.Errorf("%s: second repair ran %d iteration(s) — not a fixpoint", name, iters2)
		}
		twice := RenderWithCanvas(cloneSceneForEval(doc.Scene()), doc.Theme, doc.Canvas, nil)
		if once != twice {
			t.Errorf("%s: repair×2 render differs from repair×1 — oscillation", name)
		}
	}
}

const groupOverlapSrc = `nodes:
  scene:
    shape: composite
    parts:
      - id: tray
        shape: group
        label: "Tray"
        parts:
          - { id: a, shape: rectangle, geom: { w: 100, d: 80, h: 30 }, label: "A" }
          - { id: b, shape: rectangle, geom: { w: 100, d: 80, h: 30 }, label: "B", offset: { wx: 40, wy: 10 } }
`

// The WS-G in-group pass: two children of the same group collide; repair must
// separate them (the master plan's "nested overlaps" gap) without lowering R.
func TestRepairGroupOverlaps_SeparatesGroupSiblings(t *testing.T) {
	doc, err := Parse([]byte(groupOverlapSrc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	countInGroup := func() int {
		scene := doc.Scene()
		rects, _, _ := buildPlanModel(cloneSceneForEval(scene), doc.Theme, doc.Canvas)
		n := 0
		walkPartTree(scene.Parts, func(p *CompositePart) {
			if len(p.Parts) > 1 {
				n += len(siblingCollisions(p.Parts, rects))
			}
		})
		return n
	}
	if countInGroup() == 0 {
		t.Fatal("fixture should start with an in-group sibling collision")
	}
	before := Readability(doc)
	_, iters := RepairScene(doc)
	if iters == 0 {
		t.Fatal("repair should have acted on the in-group collision")
	}
	if n := countInGroup(); n != 0 {
		t.Fatalf("in-group collisions must be separated, still %d", n)
	}
	if after := Readability(doc); after.Score < before.Score-1e-9 {
		t.Fatalf("in-group repair lowered R (%.3f → %.3f)", before.Score, after.Score)
	}
}

// R-monotonicity across the WHOLE corpus: the repair loop must never lower
// readability on any shipped sample or bench scene.
func TestRepair_ReadabilityMonotoneAcrossCorpus(t *testing.T) {
	var files []string
	bench, _ := filepath.Glob("samples/bench/*.yaml")
	files = append(files, bench...)
	for _, cat := range []string{"node", "topology"} {
		dirs, _ := filepath.Glob("samples/" + cat + "/*")
		for _, dir := range dirs {
			f := filepath.Join(dir, "input.yaml")
			if _, err := os.Stat(f); err == nil {
				files = append(files, f)
			}
		}
	}
	if len(files) < 20 {
		t.Fatalf("corpus too small (%d files) — glob broken?", len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		before := Readability(doc).Score
		RepairScene(doc)
		if after := Readability(doc).Score; after < before-1e-9 {
			t.Errorf("%s: repair lowered R (%.4f → %.4f)", f, before, after)
		}
	}
}

// TestRepairDemoSamples — the shipped WS-G demo scenes: each starts with its
// advertised routing defect, and the repair loop drives the weighted
// crossing+tunnel sum to ZERO by escalating routing, without lowering R.
func TestRepairDemoSamples(t *testing.T) {
	cases := []struct {
		dir    string
		defect func(r ReadabilityReport) int
		what   string
	}{
		{"samples/topology/repair-route-tunnel", func(r ReadabilityReport) int { return r.Tunnels }, "tunnel"},
		{"samples/topology/repair-route-crossing", func(r ReadabilityReport) int { return r.Crossings }, "crossing"},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join(tc.dir, "input.yaml"))
		if err != nil {
			t.Fatalf("read %s: %v", tc.dir, err)
		}
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("parse %s: %v", tc.dir, err)
		}
		before := Readability(doc)
		if tc.defect(before) == 0 {
			t.Errorf("%s: demo should start with a %s defect", tc.dir, tc.what)
			continue
		}
		RepairScene(doc)
		after := Readability(doc)
		if s := routingDefectSum(after); s != 0 {
			t.Errorf("%s: repair must zero the routing-defect sum, still %.1f (tunnels=%d crossings=%d)",
				tc.dir, s, after.Tunnels, after.Crossings)
		}
		if after.Score <= before.Score {
			t.Errorf("%s: repair must raise R (%.3f → %.3f)", tc.dir, before.Score, after.Score)
		}
		upgraded := false
		for _, c := range doc.Scene().Connectors {
			if c.Routing == "orthogonal2" {
				upgraded = true
			}
		}
		if !upgraded {
			t.Errorf("%s: expected at least one connector escalated to orthogonal2", tc.dir)
		}
		if _, iters2 := RepairScene(doc); iters2 != 0 {
			t.Errorf("%s: second repair ran %d iteration(s) — not a fixpoint", tc.dir, iters2)
		}
	}
}

// The routing repair must persist through `isotopo repair --write`
// (RepairSource): the upgraded routing lands in the YAML text, and a second
// pass finds nothing left to do (idempotent).
func TestRepairRouting_PersistsToSource(t *testing.T) {
	src := []byte(strings.ReplaceAll(r2Scenes[0].tpl, "ROUTING", "orthogonal"))
	out, fixes, err := RepairSource("yaml", src)
	if err != nil {
		t.Fatalf("RepairSource: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("expected the routing fix to be reported")
	}
	if !strings.Contains(string(out), "routing: orthogonal2") {
		t.Fatalf("persisted source must carry the escalated routing:\n%s", out)
	}
	out2, fixes2, err := RepairSource("yaml", out)
	if err != nil {
		t.Fatalf("second RepairSource: %v", err)
	}
	if len(fixes2) != 0 {
		t.Fatalf("second repair should be clean, got fixes: %v", fixes2)
	}
	if string(out2) != string(out) {
		t.Fatal("second RepairSource changed the source — not idempotent")
	}
}

// TestCollectorCoversUpgradedRoutes pins the wave-1 integration fix: an
// orthogonal2 connector (as written by repairRouting) must be recorded by the
// WS-E route collector with its REAL routed corners. Before the fix it fell
// through to the "straight" default with an empty point list and vanished
// from collection, so the evaluator scored the plan router's crossing-free
// fallback instead of the drawn route — repair could "clear" a crossing that
// was still on screen.
func TestCollectorCoversUpgradedRoutes(t *testing.T) {
	data, err := os.ReadFile("samples/topology/repair-route-crossing/input.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	RepairScene(doc)
	upgraded := -1
	for i, c := range doc.Scene().Connectors {
		if c.Routing == "orthogonal2" {
			upgraded = i
		}
	}
	if upgraded < 0 {
		t.Fatal("repair should upgrade one connector to orthogonal2")
	}
	_, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
	if len(routes) != len(doc.Scene().Connectors) {
		t.Fatalf("collector must cover every connector: got %d routes for %d connectors",
			len(routes), len(doc.Scene().Connectors))
	}
	for _, r := range routes {
		if r.Index != upgraded {
			continue
		}
		if r.Routing != "orthogonal2" {
			t.Fatalf("upgraded connector recorded as %q, want orthogonal2", r.Routing)
		}
		if len(r.World) <= 2 {
			t.Fatalf("upgraded connector's collected route has %d points — expected a real detour (>2)", len(r.World))
		}
	}
}
