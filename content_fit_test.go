package isotopo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func TestContentFitDiagnosticsReachPublicConsumers(t *testing.T) {
	doc := parseWSH(t, `nodes:
  scene:
    shape: composite
    parts:
      - id: group
        shape: group
        parts:
          - {id: tiny, shape: rectangle, geom: {w: 22, d: 120, h: 10}, label: "👨‍👩‍👧‍👦", stack: {count: 2, gap: 30}}
`)
	before, _ := json.Marshal(doc)
	issues := ContentFitIssues(doc)
	if len(issues) != 2 {
		t.Fatalf("each stack instance needs a diagnostic: %+v", issues)
	}
	for _, is := range issues {
		if is.Path != "nodes.scene.parts[0].parts[0]" || is.Severity != SeverityWarning || !strings.Contains(is.Message, "estimated text advances") {
			t.Fatalf("wrong source or capability: %+v", is)
		}
	}
	public := Validate(doc)
	for _, issue := range public {
		if issue.Severity == SeverityError {
			t.Fatalf("fixture must be a structurally valid document: %+v", issue)
		}
	}
	for _, want := range issues {
		found := false
		for _, got := range public {
			found = found || got == want
		}
		if !found {
			t.Fatalf("validate lost content warning: %+v", want)
		}
	}
	report := BuildRenderReport(doc)
	count := 0
	for _, d := range report.Defects {
		if d.Kind == "content-fit" {
			count++
			if d.Patch != nil {
				t.Fatal("estimated overflow must not invent an automatic patch")
			}
		}
	}
	if count != 2 || !reflect.DeepEqual(report.ContentFits, ContentFits(doc)) {
		t.Fatalf("render report lost final layout observations: %+v", report)
	}
	if after, _ := json.Marshal(doc); string(after) != string(before) {
		t.Fatal("diagnostics changed source dimensions, layout or contents")
	}
}

func TestContentFitScreenLabelUsesIconOnlyLayout(t *testing.T) {
	doc := parseWSH(t, `theme: {text: {orient: screen}}
nodes:
  scene:
    shape: composite
    parts:
      - {id: icon, shape: rectangle, geom: {w: 40, d: 40, h: 20}, label: "A very long label", icon: "iso://glyph/database"}
`)
	p := doc.Scene().Parts[0]
	obs := ContentFits(doc)
	if len(obs) != 1 || len(obs[0].Fit.Items) != 1 || obs[0].Fit.Items[0].Kind != "icon" || obs[0].Fit.Status != "estimated-fit" {
		t.Fatalf("suppressed label still occupies top-face layout: %+v", obs)
	}
	clone := *p
	clone.Label = ""
	want := partContentRegions(&clone, doc.Theme, "icon")
	if got := partContentRegions(p, doc.Theme, "icon"); !reflect.DeepEqual(got, want) {
		t.Fatalf("icon observation kept the suppressed label: %v / %v", got, want)
	}
	if got := partContentRegions(p, doc.Theme, "text"); len(got) != 0 {
		t.Fatal("screen caption was observed as a face label")
	}
	doc.Canvas = &Canvas{Projection: "top"}
	if len(ContentFits(doc)) != 0 || len(ContentFitIssues(doc)) != 0 {
		t.Fatal("iso fitting was applied to the separate top renderer")
	}
}

func TestContentFitDefectsProtectInstanceKindAndSide(t *testing.T) {
	observation := func(id, kind string, overflow [4]float64) ContentFitObservation {
		return ContentFitObservation{PartID: id, Fit: iso25d.ContentFitResult{Status: "estimated-overflow", Items: []iso25d.ContentFitItem{{Kind: kind, Overflow: overflow}}}}
	}
	baseline := visualDefects{}
	addContentFitDefects(baseline, []ContentFitObservation{observation("a", "text", [4]float64{10})})
	for _, o := range []ContentFitObservation{
		observation("a~1", "text", [4]float64{1}),
		observation("a", "icon", [4]float64{1}),
		observation("a", "text", [4]float64{0, 1}),
		observation("a", "text", [4]float64{11}),
	} {
		after := visualDefects{}
		addContentFitDefects(after, []ContentFitObservation{o})
		if after.noWorseThan(baseline) {
			t.Fatalf("new/worsened content defect was accepted: %+v", o)
		}
	}
	better := visualDefects{}
	addContentFitDefects(better, []ContentFitObservation{observation("a", "text", [4]float64{5})})
	if !better.noWorseThan(baseline) || !baseline.noWorseThan(baseline) {
		t.Fatal("same-instance improvements should remain admissible")
	}
	named, unnamed := visualDefects{}, visualDefects{}
	addContentFitDefects(named, []ContentFitObservation{observation("#0", "text", [4]float64{10})})
	addContentFitDefects(unnamed, []ContentFitObservation{observation("", "text", [4]float64{1})})
	if unnamed.noWorseThan(named) {
		t.Fatal("anonymous instance collided with an authored string ID")
	}
}

func TestContentFitDefectsObserveActualScene(t *testing.T) {
	n := &Node{Shape: "composite", Parts: []*CompositePart{{ID: "label", Shape: "rectangle", Geom: &Geom{W: 100, D: 120, H: 20}, Label: "👨‍👩‍👧‍👦"}}}
	before := sceneVisualDefects(n, nil, nil)
	n.Parts[0].Geom.W = 22
	after := sceneVisualDefects(n, nil, nil)
	if after.noWorseThan(before) {
		t.Fatalf("shrinking a face accepted a new content overflow: before=%v after=%v", before, after)
	}
}
