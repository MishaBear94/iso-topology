package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func segmentFixture(t *testing.T, routing, from, to string, z float64) []byte {
	t.Helper()
	return []byte(fmt.Sprintf(`canvas: {padding: 30}
nodes:
  scene:
    shape: composite
    parts:
      - {id: source, shape: rectangle, geom: {w: 140, d: 100, h: 40}, offset: {wx: -80, wy: 35, wz: 17}}
      - {id: target, shape: rectangle, geom: {w: 80, d: 70, h: 30}, offset: {wx: 320, wy: 230, wz: %g}}
    connectors:
      - {from: source.%s, to: target.%s, routing: %s, arrow: triangle}
`, z, from, to, routing))
}
func readSegmentRoute(t *testing.T, src []byte) (*Document, string, ConnectorRoute) {
	t.Helper()
	doc := parseWSH(t, string(src))
	svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
	if len(routes) != 1 {
		t.Fatalf("routes: %d", len(routes))
	}
	return doc, svg, routes[0]
}
func assertSVGOrthogonal(t *testing.T, svg string) {
	t.Helper()
	for _, path := range submittedConnectorPaths(t, svg) {
		for i := 1; i < len(path.pts); i++ {
			a, b := path.pts[i-1], path.pts[i]
			dx, dy := b[0]-a[0], b[1]-a[1]
			if math.Abs(dx) > .03 && math.Abs(math.Abs(dy)-math.Abs(dx)*sin30/cos30) > .03 {
				t.Fatalf("non-axis SVG segment %v -> %v", a, b)
			}
		}
	}
}
func TestMoveSegmentPreserves3DPorts(t *testing.T) {
	count := 0
	for _, routing := range []string{"orthogonal", "orthogonal2"} {
		for _, z := range []float64{-40, 17, 100} {
			for _, from := range fixedPortNames {
				for _, to := range fixedPortNames {
					src := segmentFixture(t, routing, from, to, z)
					_, _, before := readSegmentRoute(t, src)
					for j := 0; j+1 < len(before.World3D); j++ {
						t.Run(fmt.Sprintf("%s/%g/%s/%s/%d", routing, z, from, to, j), func(t *testing.T) {
							op := EditOp{Kind: "move-segment", CI: 0, Segment: j, DWX: 31, DWY: -27}
							out, err := ApplyOpText("yaml", src, op)
							if err != nil {
								t.Fatal(err)
							}
							doc, svg, after := readSegmentRoute(t, out)
							if len(doc.Scene().Connectors[0].Waypoints3D) == 0 {
								t.Fatal("height-aware intent not serialized")
							}
							for _, pair := range [][2][3]float64{{before.World3D[0], after.World3D[0]}, {before.World3D[len(before.World3D)-1], after.World3D[len(after.World3D)-1]}} {
								if pair[0] != pair[1] {
									t.Fatalf("endpoint changed: %v", pair)
								}
							}
							for k := 1; k < len(after.World3D); k++ {
								if routeAxis(after.World3D[k-1], after.World3D[k]) < 0 {
									t.Fatalf("non-axis world leg %v %v", after.World3D[k-1], after.World3D[k])
								}
							}
							model := BuildInteractionModel(doc)
							a, b := actualConnectorEnds(t, svg, 0)
							wantA := portScreenSVG(t, svg, findPort(t, model, "source", from))
							wantB := portScreenSVG(t, svg, findPort(t, model, "target", to))
							if math.Hypot(a[0]-wantA[0], a[1]-wantA[1]) > .02 || math.Hypot(b[0]-wantB[0], b[1]-wantB[1]) > .02 {
								t.Fatalf("actual SVG detached ports: %v %v != %v %v", a, b, wantA, wantB)
							}
							assertSVGOrthogonal(t, svg)
							_, repeat, _ := readSegmentRoute(t, out)
							if svg != repeat {
								t.Fatal("preview/commit render differs")
							}
							noop, err := ApplyOpText("yaml", out, EditOp{Kind: "move-segment", CI: 0, Segment: 0})
							if err != nil || string(noop) != string(out) {
								t.Fatal("no-op must preserve bytes", err)
							}
							count++
						})
					}
				}
			}
		}
	}
	t.Logf("verified %d segment edits", count)
}
func TestMoveSegmentAtomicRejection(t *testing.T) {
	src := segmentFixture(t, "orthogonal", "port-right", "port-left", 100)
	for _, op := range []EditOp{
		{Kind: "move-segment", CI: -1}, {Kind: "move-segment", CI: 2}, {Kind: "move-segment", Segment: -1}, {Kind: "move-segment", Segment: 999}, {Kind: "move-segment", Segment: int(^uint(0) >> 1)},
		{Kind: "move-segment", DWX: math.NaN()}, {Kind: "move-segment", DWY: math.Inf(1)}, {Kind: "move-segment", DWX: maxGeomDim * 2},
	} {
		out, err := ApplyOpText("yaml", src, op)
		if err == nil || string(out) != string(src) {
			t.Fatalf("rejection not atomic: %+v %v", op, err)
		}
	}
	for _, routing := range []string{"bezier", "straight"} {
		s := segmentFixture(t, routing, "port-right", "port-left", 100)
		out, err := ApplyOpText("yaml", s, EditOp{Kind: "move-segment", DWY: 20})
		if err == nil || string(out) != string(s) {
			t.Fatal("unsupported route must be unchanged")
		}
	}
}
func TestWrite3DWaypointsSourceIsolation(t *testing.T) {
	fixtures := []string{
		"      - from: source.port-right # first\n        to: target.port-left\n        routing: orthogonal\n",
		"      - waypoints3d: # coordinates\n          - {wx: 40, wy: 20, wz: 50} # vertex\n        from: source.port-right\n        to: target.port-left\n        routing: orthogonal\n",
		"      - from: source.port-right\n        waypoints3d:\n        - {wx: 40, wy: 20, wz: 50} # vertex\n        routing: orthogonal\n        to: target.port-left\n",
		"      - {from: source.port-right, to: target.port-left, routing: orthogonal, label: 'brace } and ''quote'''} # edge\n",
		"      - {from: source.port-right, # { a comment brace\n         to: target.port-left, routing: orthogonal}\n",
	}
	sibling := "      # second edge\n      - from: target.port-right\n        to: source.port-left\n        routing: orthogonal\n        waypoints:\n          - {wx: 3, wy: 4}\n          - {wx: 10, wy: 30}\n"
	base := strings.Split(string(segmentFixture(t, "orthogonal", "port-right", "port-left", 100)), "    connectors:\n")[0] + "    connectors:\n"
	for i, f := range fixtures {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			src := []byte(base + f + sibling)
			points := []WorldPoint{{WX: 20, WY: 30, WZ: 70}, {WX: 100, WY: 30, WZ: 70}}
			out, err := writeConnectorWaypoints3D("yaml", src, "scene", 0, points)
			if err != nil {
				t.Fatal(err)
			}
			d := parseWSH(t, string(out))
			if !reflect.DeepEqual(d.Scene().Connectors[0].Waypoints3D, points) {
				t.Fatal("wrong field", string(out))
			}
			if !strings.HasSuffix(string(out), sibling) {
				t.Fatal("sibling changed", string(out))
			}
			for _, comment := range []string{"# first", "# coordinates", "# vertex", "# edge", "# { a comment brace"} {
				if strings.Contains(f, comment) && !strings.Contains(string(out), comment) {
					t.Fatalf("lost %s: %s", comment, out)
				}
			}
			// Second connector selection must not count nested waypoint dash lines.
			out2, err := writeConnectorWaypoints3D("yaml", out, "scene", 1, points)
			if err != nil {
				t.Fatal(err)
			}
			d2 := parseWSH(t, string(out2))
			if !reflect.DeepEqual(d2.Scene().Connectors[1].Waypoints3D, points) {
				t.Fatal("wrong sibling selected")
			}
		})
	}
	inline := []byte("nodes: {scene: {shape: composite, connectors: [{from: a, to: b, routing: orthogonal}, {from: b, to: a, routing: orthogonal}]}} # tail\n")
	out, err := writeConnectorWaypoints3D("yaml", inline, "scene", 0, []WorldPoint{{WX: 1, WY: 2, WZ: 3}})
	if err != nil {
		t.Fatal(err)
	}
	var n yaml.Node
	if err := yaml.Unmarshal(out, &n); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), ", {from: b, to: a, routing: orthogonal}]}} # tail") {
		t.Fatal("flow siblings lost", string(out))
	}
}
func TestMoveSegmentJSONAndEvidence(t *testing.T) {
	src := segmentFixture(t, "orthogonal", "port-right", "port-left", 100)
	doc, before, r := readSegmentRoute(t, src)
	j := -1
	for i := 0; i+1 < len(r.World3D); i++ {
		if routeAxis(r.World3D[i], r.World3D[i+1]) == 2 {
			j = i
			break
		}
	}
	if j < 0 {
		t.Fatal("fixture has no riser")
	}
	jsonSrc, _ := json.Marshal(doc)
	jsonOut, err := ApplyOpText("json", jsonSrc, EditOp{Kind: "move-segment", Segment: j, DWX: 45, DWY: -25})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(jsonOut, &raw); err != nil {
		t.Fatal(err)
	}
	out, err := ApplyOpText("yaml", src, EditOp{Kind: "move-segment", Segment: j, DWX: 45, DWY: -25})
	if err != nil {
		t.Fatal(err)
	}
	_, after, route := readSegmentRoute(t, out)
	dir := os.Getenv("ISOTOPO_SEGMENT_QA_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	trace, _ := json.MarshalIndent(map[string]any{"before": r, "after": route, "segment": j}, "", "  ")
	for name, data := range map[string][]byte{"fixture.yaml": src, "edited.yaml": out, "before.svg": []byte(before), "after.svg": []byte(after), "trace.json": trace} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
func TestConnectorEditMappingRejectsAmbiguity(t *testing.T) {
	r := editMappingFixture([][3]float64{{0, 0, 0}, {100, 0, 0}})
	if got := connectorEditSpans(r); len(got) != 1 || got[0].RouteSegment != 0 {
		t.Fatal("known source did not map", got)
	}
	// Different world heights remain distinct sources; the entire submitted
	// path overlaps here, so a pointer still cannot distinguish the legs.
	r = editMappingFixture([][3]float64{{0, 0, 0}, {100, 0, 0}, {20, 20, 20}, {120, 20, 20}})
	if got := connectorEditSpans(r); len(got) != 0 {
		t.Fatal("ambiguous final overlap selected an arbitrary leg", got)
	}
	r = editMappingFixture([][3]float64{{0, 0, 0}, {100, 0, 0}})
	r.Drawing.Points[0][1] += 2
	r.Drawing.Points[1][1] += 2
	if got := connectorEditSpans(r); len(got) != 0 {
		t.Fatal("displaced path mapped with stale provenance", got)
	}
}

func TestExplicit3DIntentAcrossProjectionAndRouting(t *testing.T) {
	src := segmentFixture(t, "orthogonal", "port-right", "port-left", 100)
	out, err := ApplyOpText("yaml", src, EditOp{Kind: "move-segment", Segment: 3, DWX: 45, DWY: 20})
	if err != nil {
		t.Fatal(err)
	}
	doc, iso, _ := readSegmentRoute(t, out)
	before, _ := json.Marshal(doc)
	// Plan flattens only Z. The existing source, render and caller remain intact.
	plan := *doc.Canvas
	plan.Projection = "top"
	rects, _, edges := buildPlanModel(doc.Scene(), doc.Theme, &plan)
	if len(rects) != 2 || len(edges) != 1 {
		t.Fatal("plan lost graph")
	}
	for _, wp := range doc.Scene().Connectors[0].Waypoints3D {
		found := false
		for _, p := range edges[0].pts {
			if math.Hypot(p[0]-wp.WX, p[1]-wp.WY) < 1e-5 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("plan lost authored XY corner: %+v", wp)
		}
	}
	_ = RenderWithCanvas(doc.Scene(), doc.Theme, &plan, nil)
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("projection mutated source")
	}
	if got := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil); got != iso {
		t.Fatal("projection switch altered iso result")
	}
	// Saved manual constraints opt out of automatic routing upgrades.
	if repairRouting(doc, doc.Scene()) {
		t.Fatal("manual 3-D route was auto upgraded")
	}
	for _, routing := range []string{"straight", "bezier"} {
		doc.Scene().Connectors[0].Routing = routing
		for _, issue := range Validate(doc) {
			if issue.Severity == SeverityError {
				t.Fatal("switching route mode blanked saved diagram", issue)
			}
		}
		rendered := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
		saved := doc.Scene().Connectors[0].Waypoints3D
		doc.Scene().Connectors[0].Waypoints3D = nil
		if plain := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil); plain != rendered {
			t.Fatal("inactive corners changed curve/straight render")
		}
		doc.Scene().Connectors[0].Waypoints3D = saved
	}
	doc.Scene().Connectors[0].Routing = "orthogonal"
	if got := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil); got != iso {
		t.Fatal("saved manual intent not restored")
	}
}
func TestMoveSegmentLegacyAndRepeatedEdits(t *testing.T) {
	for _, from := range []string{"right-mid", "left-mid", "front", "back", "top"} {
		src := segmentFixture(t, "orthogonal", from, "left-mid", 100)
		_, _, original := readSegmentRoute(t, src)
		for iteration := 0; iteration < 8; iteration++ {
			_, _, r := readSegmentRoute(t, src)
			j := (iteration + 1) % (len(r.World3D) - 1)
			out, err := ApplyOpText("yaml", src, EditOp{Kind: "move-segment", Segment: j, DWX: 15, DWY: 20})
			if err != nil {
				t.Fatal(err)
			}
			_, svg, edited := readSegmentRoute(t, out)
			assertSVGOrthogonal(t, svg)
			if edited.World3D[0] != original.World3D[0] || edited.World3D[len(edited.World3D)-1] != original.World3D[len(original.World3D)-1] {
				t.Fatal("legacy endpoint identity/height drift")
			}
			src = out
		}
	}
}
func TestWrite3DWaypointsFlowComments(t *testing.T) {
	src := []byte("nodes: {scene: {connectors: [{from: a, to: b, routing: orthogonal, waypoints3d: [\n {wx: 10, wy: 20, wz: 30} # keep coordinate note\n ]}]}} # outer\n")
	out, err := writeConnectorWaypoints3D("yaml", src, "scene", 0, []WorldPoint{{WX: 20, WY: 30, WZ: 40}})
	if err != nil {
		t.Fatal(err)
	}
	var n yaml.Node
	if err := yaml.Unmarshal(out, &n); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.Contains(string(out), "# keep coordinate note") || !strings.HasSuffix(string(out), " # outer\n") {
		t.Fatal("lost comments", string(out))
	}
}

func TestExplicit3DProtectsIntentFromLegacyMove(t *testing.T) {
	src := segmentFixture(t, "orthogonal", "port-right", "port-left", 100)
	out, err := ApplyOpText("yaml", src, EditOp{Kind: "move-segment", Segment: 3, DWX: 30, DWY: 20})
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := ApplyOpText("yaml", out, EditOp{Kind: "move", Target: "edge", CI: 0, DWY: 20})
	if err == nil || string(rejected) != string(out) {
		t.Fatal("legacy XY operation silently edited inactive fields")
	}
	doc, svg, _ := readSegmentRoute(t, out)
	c := doc.Scene().Connectors[0]
	c.Bend = &WorldPoint{WX: 777, WY: 888}
	c.Waypoints = []WorldPoint{{WX: 1111, WY: 2222}}
	if got := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil); got != svg {
		t.Fatal("legacy override changed active 3-D route")
	}
	saved := append([]WorldPoint(nil), c.Waypoints3D...)
	moved, err := ApplyOpText("yaml", out, EditOp{Kind: "move", Target: "node", ID: "target", DWX: 30, DWY: 10})
	if err != nil {
		t.Fatal(err)
	}
	movedDoc := parseWSH(t, string(moved))
	if !reflect.DeepEqual(movedDoc.Scene().Connectors[0].Waypoints3D, saved) {
		t.Fatal("node move discarded manual corners")
	}
}

func TestMoveSegmentNullConnectorRejected(t *testing.T) {
	doc := parseWSH(t, string(segmentFixture(t, "orthogonal", "port-right", "port-left", 100)))
	doc.Scene().Connectors[0] = nil
	src, _ := json.Marshal(doc)
	out, err := ApplyOpText("json", src, EditOp{Kind: "move-segment", Segment: 0, DWX: 10})
	if err == nil || string(out) != string(src) {
		t.Fatal("null connector not rejected atomically")
	}
}
