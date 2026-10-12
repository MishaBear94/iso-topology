package isotopo

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLabelOcclusionFollowsRenderedDepthOrder(t *testing.T) {
	for _, sorted := range []bool{false, true} {
		t.Run(fmt.Sprint(sorted), func(t *testing.T) {
			n := &Node{Shape: "composite", Parts: []*CompositePart{
				leafNode("front", "Front", 80, 80, 0, 160, 160, 200),
				leafNode("back", "Back", 0, 0, 0, 120, 120, 26),
			}}
			cv := &Canvas{DepthSort: sorted}
			doc := &Document{Nodes: map[string]*Node{"scene": n}, Canvas: cv}
			issues := LabelOcclusionIssues(doc)
			if !sorted {
				if len(issues) != 0 {
					t.Fatalf("author order paints Back last and leaves both labels visible: %v", issues)
				}
				return
			}
			if len(issues) != 1 || !strings.Contains(issues[0].Message, `"back" is hidden by node "front"`) {
				t.Fatalf("depth order must report Back covered by Front: %v", issues)
			}
			svg := RenderWithCanvas(n, nil, cv, nil)
			back := strings.Index(svg, `data-part-id="back"`)
			front := strings.Index(svg, `data-part-id="front"`)
			if back < 0 || front <= back {
				t.Fatal("the actual SVG must paint Front after Back")
			}
		})
	}
}

// Evaluating before render must use the same connector-driven layout as an
// explicit solve. A partial clone that drops connectors silently evaluates a
// different graph; tall nodes make that discrepancy visible in the reports.
func TestOcclusionUsesCompleteAutoLayoutScene(t *testing.T) {
	for _, seed := range []int{1, 2} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			doc := parseWSH(t, fmt.Sprintf(`
canvas: {depthSort: true}
nodes:
  scene:
    shape: composite
    layout: {mode: auto}
    parts:
      - {id: a, label: A, geom: {w: 100, d: 100, h: %d}, offset: {wx: %d}}
      - {id: b, label: B, geom: {w: 100, d: 100, h: %d}}
      - {id: c, label: C, geom: {w: 100, d: 100, h: %d}}
      - {id: d, label: D, geom: {w: 100, d: 100, h: %d}}
    connectors:
      - {from: a, to: c}
      - {from: c, to: b}
      - {from: b, to: d}
`, 30+seed, seed*90, 101+seed, 172+seed, 243+seed))
			n := doc.Nodes["scene"]
			before, _ := json.Marshal(doc)
			labels := LabelOcclusionIssues(doc)
			bodies := BodyOcclusions(doc)
			layout := layoutIssues(n, doc.Canvas)
			plan := PlanFootprintIssues(doc)
			lint := VisualLint(doc)
			after, _ := json.Marshal(doc)
			if string(before) != string(after) {
				t.Fatal("evaluation mutated the author's layout or offsets")
			}
			solved := cloneSceneForEval(n)
			wantLayout := applyLayout(solved, doc.Canvas)
			solvedDoc := &Document{Nodes: map[string]*Node{"scene": solved}, Canvas: doc.Canvas}
			if seed == 1 && len(BodyOcclusions(solvedDoc)) != 1 {
				t.Fatal("fixture must contain one body occlusion after solving")
			}
			if seed == 2 && len(LabelOcclusionIssues(solvedDoc)) != 1 {
				t.Fatal("fixture must contain one label occlusion after solving")
			}
			if want := LabelOcclusionIssues(solvedDoc); !reflect.DeepEqual(labels, want) {
				t.Errorf("labels before solve = %v; after solve = %v", labels, want)
			}
			if want := BodyOcclusions(solvedDoc); !reflect.DeepEqual(bodies, want) {
				t.Errorf("bodies before solve = %v; after solve = %v", bodies, want)
			}
			if !reflect.DeepEqual(layout, wantLayout) {
				t.Errorf("layout diagnostics = %v; actual solve = %v", layout, wantLayout)
			}
			if want := PlanFootprintIssues(solvedDoc); !reflect.DeepEqual(plan, want) {
				t.Errorf("plan collisions before solve = %v; after solve = %v", plan, want)
			}
			if want := VisualLint(solvedDoc); !reflect.DeepEqual(lint, want) {
				t.Errorf("solid collisions before solve = %v; after solve = %v", lint, want)
			}
		})
	}
}
