package isotopo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestConnectorLabelDefectsPreserveEachConnection(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		doc := connectorTextTestDoc(t, projection, "straight")
		doc.Scene().Parts[2].Offset.WY = 1500
		doc.Scene().Parts[3].Offset.WY = 1500
		long := strings.Repeat(strings.Repeat("W", 120)+"\n", 8)
		c := doc.Scene().Connectors
		c[0].Label = long
		c[0].LabelFontSize = 40
		before := connectorLabelPlacementDefects(doc.Scene(), doc.Theme, doc.Canvas)
		if before[visualDefect{kind: "connector-label-blocked", edge: 0}] != 1 || before[visualDefect{kind: "connector-label-blocked", edge: 1}] != 0 {
			t.Fatalf("fixture needs exactly one blocked label: %v", before)
		}
		c[0].Label = "AV office"
		c[0].LabelFontSize = 13
		c[1].Label = long
		c[1].LabelFontSize = 40
		after := connectorLabelPlacementDefects(doc.Scene(), doc.Theme, doc.Canvas)
		if after[visualDefect{kind: "connector-label-blocked", edge: 1}] != 1 || after[visualDefect{kind: "connector-label-blocked", edge: 0}] != 0 || after.noWorseThan(before) {
			t.Fatalf("blockage transferred between semantic edges: %v %v", before, after)
		}
		c[0].To = "missing-node"
		missing := connectorLabelPlacementDefects(doc.Scene(), doc.Theme, doc.Canvas)
		if missing[visualDefect{kind: "connector-label-missing", edge: 0}] != 1 || missing.noWorseThan(after) {
			t.Fatal("disappearing measured label passed candidate gate")
		}
		for _, c := range c {
			c.LabelFontFamily = "unknown"
		}
		if len(connectorLabelPlacementDefects(doc.Scene(), doc.Theme, doc.Canvas)) != 0 {
			t.Fatal("unknown font claimed as measured")
		}
	}
}

// Reproduced by the real routing actuator: lowering aggregate routing cost
// moved the existing label blockage from edge 1 onto the previously clear edge
// 2. Equal blockage counts do not make that substitution safe.
func TestRoutingRepairPreservesMeasuredLabelClearance(t *testing.T) {
	n := &Node{Shape: "composite"}
	doc := &Document{Nodes: map[string]*Node{"scene": n}}
	for i, p := range [][2]float64{{480, 480}, {0, 160}, {400, 320}, {160, 560}, {320, 560}, {0, 240}} {
		n.Parts = append(n.Parts, &CompositePart{ID: fmt.Sprintf("n%d", i), Shape: "rectangle", Geom: &Geom{W: 80, D: 60, H: 20}, Offset: &WorldPoint{WX: p[0], WY: p[1]}})
	}
	// Keep the unmeasured-font path covered: precise body refinement can
	// already resolve this fixture when all six bodies have no text.
	n.Parts[0].Label = "Unmeasured"
	for i := 0; i < 3; i++ {
		n.Connectors = append(n.Connectors, &Connector{From: fmt.Sprintf("n%d", i*2), To: fmt.Sprintf("n%d", i*2+1), Routing: "orthogonal", Arrow: "triangle", Label: "request", LabelFontSize: 13, LabelFontFamily: "Source Sans Pro"})
	}
	before := connectorLabelPlacementDefects(n, nil, nil)
	if before[visualDefect{kind: "connector-label-blocked", edge: 1}] != 1 || before[visualDefect{kind: "connector-label-blocked", edge: 0}] != 0 || before[visualDefect{kind: "connector-label-blocked", edge: 2}] != 0 {
		t.Fatalf("fixture changed: %v", before)
	}
	saved, _ := json.Marshal(doc)
	cost := Readability(doc).Cost
	changed := repairRouting(doc, n)
	after := connectorLabelPlacementDefects(n, nil, nil)
	if !after.noWorseThan(before) {
		t.Fatalf("routing repair damages a clear measured label: before=%v after=%v", before, after)
	}
	if changed && Readability(doc).Cost >= cost {
		t.Fatal("repair without strict routing improvement")
	}
	if !changed {
		now, _ := json.Marshal(doc)
		if string(saved) != string(now) {
			t.Fatal("rejected routing candidate changed source")
		}
	}
}

func TestRoutingRepairPreservesDefectPairs(t *testing.T) {
	for _, family := range []string{"Source Sans Pro", ""} {
		t.Run("family="+family, func(t *testing.T) {
			n := &Node{Shape: "composite"}
			doc := &Document{Nodes: map[string]*Node{"scene": n}}
			for i, p := range [][2]float64{{480, 160}, {480, 480}, {160, 400}, {480, 320}, {640, 640}, {240, 240}} {
				n.Parts = append(n.Parts, &CompositePart{ID: fmt.Sprintf("n%d", i), Shape: "rectangle", Geom: &Geom{W: 80, D: 60, H: 20}, Offset: &WorldPoint{WX: p[0], WY: p[1]}})
			}
			for i := 0; i < 3; i++ {
				n.Connectors = append(n.Connectors, &Connector{From: fmt.Sprintf("n%d", i*2), To: fmt.Sprintf("n%d", i*2+1), Routing: "orthogonal", Arrow: "triangle", Label: "request", LabelFontSize: 13, LabelFontFamily: family})
			}
			before := sceneVisualDefects(n, nil, nil)
			if before[visualDefect{kind: "crossing", edge: 1, peer: 2}] != 1 || before[visualDefect{kind: "tunnel", edge: 0, a: "n3"}] != 1 {
				t.Fatalf("fixture changed: %v", before)
			}
			repairRouting(doc, n)
			after := sceneVisualDefects(n, nil, nil)
			if !after.noWorseThan(before) {
				t.Fatalf("routing repair bought a new defect pair: before=%v after=%v", before, after)
			}
		})
	}
}
