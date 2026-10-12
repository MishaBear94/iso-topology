package isotopo

import (
	"encoding/json"
	"testing"
)

// A lower aggregate score used to buy two fresh crossings by removing two
// tunnels. The body repair must evaluate defect identities, not their sum.
func TestBodyRepairRejectsNewCrossings(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	n := doc.Scene()
	for i, p := range []struct {
		id   string
		x, y float64
	}{{"n0", 160, -120}, {"n1", -280, 200}, {"n2", 120, 0}} {
		n.Parts = append(n.Parts, &CompositePart{ID: p.id, Shape: "rectangle", Label: []string{"N0", "N1", "N2"}[i], Geom: &Geom{W: 80, D: 80, H: 40}, Offset: &WorldPoint{WX: p.x, WY: p.y}})
	}
	n.Connectors = []*Connector{{From: "cache", To: "n0", Routing: "orthogonal"}, {From: "n1", To: "n2", Routing: "orthogonal"}}
	before := sceneVisualDefects(n, doc.Theme, doc.Canvas)
	saved, _ := json.Marshal(doc)
	changed := repairBodyOcclusion(doc, n)
	after := sceneVisualDefects(n, doc.Theme, doc.Canvas)
	if !after.noWorseThan(before) {
		t.Fatalf("body repair introduced a defect: before=%v; after=%v", before, after)
	}
	if !changed {
		now, _ := json.Marshal(doc)
		if string(now) != string(saved) {
			t.Fatal("rejected candidate mutated author scene")
		}
	}
}

func TestBodyRepairKeepsUncertainScenes(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	n := doc.Scene()
	n.Parts = append(n.Parts, &CompositePart{ID: "unknown", Shape: "rack", Offset: &WorldPoint{WX: -900, WY: -900}})
	saved, _ := json.Marshal(doc)
	if repairBodyOcclusion(doc, n) {
		t.Fatal("automatic motion accepted despite unmodelled rack coverage")
	}
	now, _ := json.Marshal(doc)
	if string(now) != string(saved) {
		t.Fatal("uncertain scene mutated")
	}
}
