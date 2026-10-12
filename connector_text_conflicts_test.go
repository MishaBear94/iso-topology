package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

func TestConnectorTextConflictShortfallIndependentOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 67867967))
	hits := 0
	for trial := 0; trial < 1200; trial++ {
		scale := math.Pow(10, float64(trial%5)-2)
		x, y := rng.Float64()*100*scale, rng.Float64()*100*scale
		box := screenRect{x, y, x + (5+rng.Float64()*40)*scale, y + (5+rng.Float64()*40)*scale}
		o := screenRect{x + (rng.Float64()-.5)*100*scale, y + (rng.Float64()-.5)*100*scale, 0, 0}
		o.x1 = o.x0 + 30*scale
		o.y1 = o.y0 + 20*scale
		stroke := connectorTextStroke{a: [2]float64{x + (rng.Float64()-.5)*100*scale, y + (rng.Float64()-.5)*100*scale}, b: [2]float64{x + (rng.Float64()-.5)*100*scale, y + (rng.Float64()-.5)*100*scale}, radius: 2 * scale}
		if trial%11 == 0 {
			stroke.b = stroke.a
		}
		for _, kind := range []string{"rect", "stroke"} {
			collidesWith := func(b screenRect) bool { return collides(b, []screenRect{o}) }
			depth := connectorTextRectShortfall(box, o)
			if kind == "stroke" {
				depth = connectorTextStrokeShortfall(box, stroke)
				collidesWith = func(b screenRect) bool { return connectorTextStrokeHits(b, []connectorTextStroke{stroke}) }
			}
			if !collidesWith(box) {
				if depth != 0 {
					t.Fatal("non-collision has positive depth")
				}
				continue
			}
			hits++
			// Independently move the actual rectangle, testing the original
			// slab/AABB predicates. A convex forbidden region contains a square
			// iff all four corners are inside, so bisection over these four
			// translations gives its L-infinity interior depth without a hull.
			escapes := func(d float64) bool {
				for _, dx := range []float64{-d, d} {
					for _, dy := range []float64{-d, d} {
						if !collidesWith(screenRect{box.x0 + dx, box.y0 + dy, box.x1 + dx, box.y1 + dy}) {
							return true
						}
					}
				}
				return false
			}
			lo, hi := 0., 1000*scale+20
			if !escapes(hi) {
				t.Fatal("oracle search bound")
			}
			for i := 0; i < 60; i++ {
				mid := (lo + hi) / 2
				if escapes(mid) {
					hi = mid
				} else {
					lo = mid
				}
			}
			if math.Abs(depth-hi) > 1e-7*math.Max(1, scale) {
				t.Fatalf("trial %d %s depth=%g oracle=%g", trial, kind, depth, hi)
			}
		}
	}
	if hits < 500 {
		t.Fatal("insufficient intersection coverage")
	}
	t.Logf("2400 oracle comparisons, %d penetrating cases", hits)
}

func TestConnectorTextConflictIdentityAndObservation(t *testing.T) {
	record := func(index int, clear bool) ConnectorLabelRecord {
		status := "blocked"
		if clear {
			status = "clear-within-scope"
		}
		return ConnectorLabelRecord{index, ConnectorLabel{Rect: [4]float64{20, 20, 40, 20}, Typography: &ConnectorTypography{Placement: status}}}
	}
	records := []ConnectorLabelRecord{record(7, false), record(8, true)}
	targets := []connectorTextRectTarget{connectorTextBodyTarget("node", 0, screenRect{25, 25, 50, 45}), {kind: "arrow", index: 7, rect: screenRect{30, 20, 40, 30}}, {kind: "label", index: 4, rect: screenRect{30, 20, 40, 30}}}
	strokes := []connectorTextStroke{{owner: 7, a: [2]float64{0, 30}, b: [2]float64{80, 30}, radius: 1}, {owner: 0, a: [2]float64{0, 30}, b: [2]float64{40, 30}, radius: 1}, {owner: 0, a: [2]float64{40, 30}, b: [2]float64{80, 30}, radius: 1}}
	observeConnectorTextConflicts(records, targets, strokes)
	got := records[0].Label.Typography.Conflicts
	if len(got) != 5 || len(records[1].Label.Typography.Conflicts) != 0 {
		t.Fatal("wrong counterpart count or clear certificate changed")
	}
	encoded, _ := json.Marshal(records)
	if !strings.Contains(string(encoded), `"connector_index":0`) || !strings.Contains(string(encoded), `"part_index":0`) {
		t.Fatal("zero counterpart identity omitted")
	}
	for _, c := range got {
		if c.ClearanceShortfall <= 0 {
			t.Fatal("nonpositive conflict")
		}
		if c.Kind == "route" && *c.ConnectorIndex != 0 {
			t.Fatal("own route counted or peer identity lost")
		}
	}
	observeConnectorTextConflicts(records, targets, strokes)
	second, _ := json.Marshal(records)
	if string(encoded) != string(second) {
		t.Fatal("observation not deterministic/idempotent")
	}
	records[0].Label.Typography.Placement = "clear-within-scope"
	observeConnectorTextConflicts(records, targets, strokes)
	if len(records[0].Label.Typography.Conflicts) != 0 {
		t.Fatal("stale conflict retained")
	}
}

func connectorConflictRegressionDoc(severity bool) *Document {
	points := [][2]float64{{160, 320}, {0, 160}, {640, 80}, {400, 0}, {80, 640}, {400, 80}, {480, 0}, {400, 640}}
	if severity {
		points = [][2]float64{{160, 160}, {400, 560}, {240, 160}, {480, 160}, {160, 320}, {80, 640}, {560, 480}, {320, 480}}
	}
	n := &Node{Shape: "composite"}
	for i, p := range points {
		n.Parts = append(n.Parts, &CompositePart{ID: fmt.Sprintf("n%d", i), Shape: "rectangle", Geom: &Geom{W: 80, D: 60, H: 20}, Offset: &WorldPoint{WX: p[0], WY: p[1]}})
	}
	n.Parts[0].Label = "Unmeasured"
	for i := 0; i < 4; i++ {
		n.Connectors = append(n.Connectors, &Connector{From: fmt.Sprintf("n%d", i*2), To: fmt.Sprintf("n%d", i*2+1), Routing: "orthogonal", Arrow: "triangle", Label: "long request", LabelFontSize: 16, LabelFontFamily: "Source Sans Pro"})
	}
	return &Document{Nodes: map[string]*Node{"scene": n}}
}

func TestRoutingRepairPreservesBlockedLabelCounterpartsAndSeverity(t *testing.T) {
	for _, severity := range []bool{false, true} {
		t.Run(fmt.Sprint(severity), func(t *testing.T) {
			doc := connectorConflictRegressionDoc(severity)
			n := doc.Scene()
			before := visualDefects{}
			addConnectorTextConflictDefects(before, renderSceneObserved(doc, true).ConnectorLabels)
			key := visualDefect{kind: "connector-label-body", a: "n2", edge: 1}
			if severity {
				key = visualDefect{kind: "connector-label-body", a: "n1", edge: 3}
			}
			if before[key] <= 0 {
				t.Fatalf("regression fixture lost: %v", before)
			}
			saved, _ := json.Marshal(doc)
			cost := Readability(doc).Cost
			changed := repairRouting(doc, n)
			after := visualDefects{}
			addConnectorTextConflictDefects(after, renderSceneObserved(doc, true).ConnectorLabels)
			if !after.noWorseThan(before) {
				t.Fatalf("already-blocked label worsened: before=%v after=%v", before, after)
			}
			if changed && Readability(doc).Cost >= cost {
				t.Fatal("no strict routing improvement")
			}
			if !changed {
				now, _ := json.Marshal(doc)
				if string(saved) != string(now) {
					t.Fatal("rejected routing candidate modified input")
				}
			}
		})
	}
}

func TestConnectorTextConflictSceneAndReport(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		doc := connectorTextTestDoc(t, projection, "straight")
		doc.Scene().Connectors[0].Label = strings.Repeat(strings.Repeat("W", 80)+"\n", 8)
		doc.Scene().Connectors[0].LabelFontSize = 40
		saved, _ := json.Marshal(doc)
		preview := RenderScene(doc)
		scene, report := RenderSceneWithReport(doc)
		if preview.SVG != scene.SVG || len(preview.ConnectorLabels) != len(scene.ConnectorLabels) {
			t.Fatal("diagnostics changed submitted scene")
		}
		for i, record := range scene.ConnectorLabels {
			if len(preview.ConnectorLabels[i].Label.Typography.Conflicts) != 0 {
				t.Fatal("preview eagerly collected detailed conflicts")
			}
			without := record
			typography := *record.Label.Typography
			typography.Conflicts = nil
			without.Label.Typography = &typography
			if !reflect.DeepEqual(without, preview.ConnectorLabels[i]) {
				t.Fatal("diagnostic request changed label geometry/status")
			}
		}
		if scene.SVG != RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations) {
			t.Fatal("collection changed SVG")
		}
		if !reflect.DeepEqual(scene.ConnectorLabels, report.ConnectorLabels) {
			t.Fatal("report rebuilt conflict geometry")
		}
		after, _ := json.Marshal(doc)
		if string(saved) != string(after) {
			t.Fatal("report changed input")
		}
		found := false
		for _, r := range report.ConnectorLabels {
			if r.Label.Typography.Placement == "blocked" {
				if len(r.Label.Typography.Conflicts) == 0 {
					t.Fatal("blocked fixture has no counterparts")
				}
				found = true
			}
		}
		if !found {
			t.Fatal("fixture not blocked")
		}
	}
}

func TestConnectorTextConflictParallelAndLegacyIdentity(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		for _, legacy := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", projection, legacy), func(t *testing.T) {
				doc := connectorTextTestDoc(t, projection, "straight")
				n := doc.Scene()
				n.Parts = n.Parts[:2]
				peer := *n.Connectors[0]
				peer.Label = ""
				peer.LabelFontFamily = ""
				if legacy {
					peer.Label = "legacy"
				}
				n.Connectors = []*Connector{n.Connectors[0], &peer}
				labels := renderSceneObserved(doc, true).ConnectorLabels
				if len(labels) != 1 || labels[0].ConnectorIndex != 0 || labels[0].Label.Typography.Placement != "blocked" {
					t.Fatal("parallel route fixture changed")
				}
				route, label := false, false
				for _, c := range labels[0].Label.Typography.Conflicts {
					if c.Kind == "route" {
						if *c.ConnectorIndex != 1 {
							t.Fatal("own route counted")
						}
						route = true
					}
					if c.Kind == "label" {
						if *c.ConnectorIndex != 1 {
							t.Fatal("legacy peer identity lost")
						}
						label = true
					}
				}
				if !route || label != legacy {
					t.Fatalf("route=%v legacy=%v", route, label)
				}
			})
		}
	}
}

func TestConnectorTextConflictCollinearSubdivision(t *testing.T) {
	box := screenRect{-50, -10, 50, 10}
	whole := []connectorTextStroke{{owner: 1, a: [2]float64{-100, -100}, b: [2]float64{100, 100}, radius: 2}}
	pieces := []connectorTextStroke{{owner: 1, a: [2]float64{-100, -100}, b: [2]float64{0, 0}, radius: 2}, {owner: 1, a: [2]float64{0, 0}, b: [2]float64{100, 100}, radius: 2}}
	merged := connectorTextConflictStrokes(pieces)
	if len(merged) != 1 || !reflect.DeepEqual(merged, whole) || connectorTextStrokeShortfall(box, merged[0]) != connectorTextStrokeShortfall(box, whole[0]) {
		t.Fatal("straight waypoint changes severity")
	}
	pieces[1].owner = 2
	if len(connectorTextConflictStrokes(pieces)) != 2 {
		t.Fatal("parallel identities merged")
	}
	pieces[1].owner = 1
	pieces[1].radius = 3
	if len(connectorTextConflictStrokes(pieces)) != 2 {
		t.Fatal("different stroke widths merged")
	}
	pieces[1].radius = 2
	pieces[1].a = [2]float64{1, 1}
	if len(connectorTextConflictStrokes(pieces)) != 2 {
		t.Fatal("gap filled")
	}
	pieces[1].a = [2]float64{0, 0}
	pieces[1].b = [2]float64{100, 100.001}
	if len(connectorTextConflictStrokes(pieces)) != 2 {
		t.Fatal("bend erased")
	}
	if pieces[0].b != ([2]float64{0, 0}) {
		t.Fatal("source strokes mutated")
	}
	points := []connectorTextStroke{{owner: 1, a: [2]float64{0, 0}, b: [2]float64{0, 0}, radius: 2}, {owner: 1, a: [2]float64{0, 100}, b: [2]float64{0, 100}, radius: 2}}
	if len(connectorTextConflictStrokes(points)) != 2 {
		t.Fatal("distinct point strokes merged")
	}
}

func BenchmarkConnectorTextConflictDiagnostics(b *testing.B) {
	for _, count := range []int{16, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			doc := connectorTextDenseSearchScene(count)
			renderSceneObserved(doc, true)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				renderSceneObserved(doc, true)
			}
		})
	}
}
