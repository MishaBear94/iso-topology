package isotopo

import (
	"github.com/MarkovWangRR/iso-topology/iso25d"
	"strings"
	"testing"
)

func TestContentInkGapDiagnosticsAndGuard(t *testing.T) {
	fit := iso25d.ShapeContent("rectangle", iso25d.ConvertOpts{Width: 80, Depth: 80, Height: 20, Label: "Service", Icon: "icon", IconAnchor: "center", IconScale: .4, FontFamily: "Source Sans Pro", FontSize: 18}).Fit
	observations := []ContentFitObservation{{NodeID: "scene", PartID: "service", Path: "nodes.scene.parts[0]", Fit: fit}}
	if len(fit.Gaps) != 1 || !fit.Manual {
		t.Fatalf("manual overlap not observed: %+v", fit)
	}
	issue, ok := contentFitIssue(observations[0])
	if !ok {
		t.Fatal("missing gap warning")
	}
	issues := []Issue{issue}
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "ink separation") || strings.Contains(issues[0].Message, "padding budget by up to 0.00") {
		t.Fatalf("misleading gap warning: %+v", issues)
	}
	if !strings.Contains(issues[0].Message, "authored icon placement has been preserved") {
		t.Fatal("manual constraint not reported")
	}
	a := visualDefects{}
	addContentFitDefects(a, observations)
	if len(a) != 1 {
		t.Fatalf("gap absent from candidate protection: %+v", a)
	}
	// Another instance's better spacing cannot compensate for this one.
	other := observations[0]
	other.PartID = "other"
	other.Fit.Gaps = append([]iso25d.ContentFitGap(nil), other.Fit.Gaps...)
	other.Fit.Gaps[0].Shortfall /= 2
	b := visualDefects{}
	addContentFitDefects(b, []ContentFitObservation{other})
	for key := range a {
		if _, exists := b[key]; exists {
			t.Fatal("instance identities aliased")
		}
	}
}

func TestContentInkGapReachesPublicDiagnostics(t *testing.T) {
	doc, err := Parse([]byte(`nodes:
  scene:
    shape: composite
    parts:
      - id: cramped
        shape: rectangle
        label: "gyp\nǗgj"
        icon: "iso://glyph/database"
        geom: {w: 70, d: 24, h: 10}
        style:
          text: {family: "Source Sans Pro", size: 18}
`))
	if err != nil {
		t.Fatal(err)
	}
	fit := ContentFits(doc)
	if len(fit) != 1 || len(fit[0].Fit.Gaps) == 0 {
		t.Fatalf("unmet ink gap disappeared: %+v", fit)
	}
	issues := ContentFitIssues(doc)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "ink separation") {
		t.Fatalf("missing public warning: %+v", issues)
	}
}
