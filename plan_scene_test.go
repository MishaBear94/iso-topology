package isotopo

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestSceneWithInteractionMatchesSeparateConsumers(t *testing.T) {
	check := func(t *testing.T, doc *Document) {
		t.Helper()
		input, _ := json.Marshal(doc)
		wantScene, wantModel := RenderScene(doc), BuildInteractionModel(doc)
		got := RenderSceneWithInteraction(doc)
		if !reflect.DeepEqual(got.Model, wantModel) {
			t.Fatal("combined preview has a different interaction model")
		}
		model := got.Model
		got.Model = nil
		if !reflect.DeepEqual(got, wantScene) {
			t.Fatal("requesting interaction changed submitted geometry or diagnostics")
		}
		after, _ := json.Marshal(doc)
		if string(input) != string(after) {
			t.Fatal("combined preview mutated the author document")
		}
		if len(model) > 0 {
			model[0].X++
			if len(model[0].Ports) > 0 {
				model[0].Ports[0].WX++
			}
			if len(model[0].SelectionRegions) > 0 {
				model[0].SelectionRegions[0][0][0]++
			}
			if !reflect.DeepEqual(RenderSceneWithInteraction(doc).Model, wantModel) {
				t.Fatal("result model aliases a later render")
			}
		}
	}
	walkSampleDocs(t, func(t *testing.T, _ string, doc *Document) { check(t, doc) })
	for _, kind := range []string{"tight", "vertical"} {
		for _, mode := range []string{"measured", "legacy", "unsupported", "fixed", "pill", "empty-pill"} {
			t.Run(kind+mode, func(t *testing.T) {
				doc := planRouteContentTestDoc(kind)
				switch mode {
				case "legacy":
					doc.Scene().Parts[0].Style.Text.Family = ""
				case "unsupported":
					doc.Scene().Parts[0].Label = "中文标题🙂"
				case "fixed":
					doc.Scene().Connectors[0].From = "from.port-right"
				case "pill", "empty-pill":
					c := doc.Scene().Connectors[0]
					c.LabelFontFamily, c.Label = "Source Sans Pro", "request"
					if mode == "empty-pill" {
						c.Label = "  "
					}
				}
				check(t, doc)
			})
		}
	}
	for _, doc := range []*Document{nil, {}, {Nodes: map[string]*Node{"scene": {Shape: "composite"}}, Canvas: &Canvas{Projection: "top"}}, {Nodes: map[string]*Node{"scene": {Shape: "rectangle", Label: "single"}}, Canvas: &Canvas{Projection: "top"}}} {
		check(t, doc)
	}
}

func TestSceneWithInteractionFreshInputsAndConcurrentOwnership(t *testing.T) {
	doc := planRouteContentTestDoc("vertical")
	var last string
	for _, weight := range []string{"400", "600", "700"} {
		doc.Scene().Parts[0].Style.Text.Weight = weight
		input, _ := json.Marshal(doc)
		var fresh Document
		if err := json.Unmarshal(input, &fresh); err != nil {
			t.Fatal(err)
		}
		got, want := RenderSceneWithInteraction(doc), RenderSceneWithInteraction(&fresh)
		if !reflect.DeepEqual(got, want) || got.SVG == last {
			t.Fatal("edited document reused stale content")
		}
		last = got.SVG
	}
	want := RenderSceneWithInteraction(doc)
	results := make(chan SceneResult, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- RenderSceneWithInteraction(doc)
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if !reflect.DeepEqual(got, want) {
			t.Fatal("concurrent requests changed submitted result")
		}
	}
}

func TestPlanSceneReusesAcceptedContentAcrossConsumers(t *testing.T) {
	doc := planRouteContentTestDoc("tight")
	p := resolvePlanScene(doc.Scene(), doc.Theme, doc.Canvas, true, true)
	if p.content == nil || !p.edges[0].refined {
		t.Fatal("fixture must retain a content-certified detour")
	}
	content := p.content
	before, _ := json.Marshal(content.texts[0].record)
	var labels []PlanLabel
	svg := renderResolvedPlan(p, doc.Canvas, nil, nil, &labels)
	model := buildInteractionModel(doc, p)
	_ = p.evaluate(nil)
	if p.content != content {
		t.Fatal("consumer discarded the accepted content arrangement")
	}
	after, _ := json.Marshal(content.texts[0].record)
	if string(before) != string(after) || renderResolvedPlan(p, doc.Canvas, nil, nil, nil) != svg {
		t.Fatal("consuming the resolved scene changed its output")
	}
	if !reflect.DeepEqual(model, BuildInteractionModel(doc)) || labels[0].Placement.Status != "clear-within-scope" {
		t.Fatal("selection or submitted title differs from accepted content")
	}
}

func TestPlanScenePreviewKeepsReportOnlyConflictsPrivate(t *testing.T) {
	doc := planRouteContentTestDoc("tight")
	doc.Scene().Parts[1].Label = "Unmeasured"
	preview := RenderSceneWithInteraction(doc)
	scene, report := RenderSceneWithReport(doc)
	if preview.SVG != scene.SVG || !reflect.DeepEqual(report.PlanLabels, scene.PlanLabels) {
		t.Fatal("diagnostic detail changed the picture or report")
	}
	a, b := preview.PlanLabels[0].Placement, report.PlanLabels[0].Placement
	if a.Status != "blocked" || b.Status != "blocked" || len(a.Conflicts) != 0 || len(b.Conflicts) == 0 {
		t.Fatal("preview lost status or exposed report-only detail")
	}
	encoded, _ := json.Marshal(RenderScene(doc))
	if strings.Contains(string(encoded), `"model":`) {
		t.Fatal("ordinary render unexpectedly publishes an interaction layer")
	}
}
