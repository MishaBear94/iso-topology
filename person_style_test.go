package isotopo

import (
	"strings"
	"testing"
)

// ── person style extension: DSL → flatten → renderer feature interaction ────

// TestPersonEffectsThroughPipeline drives the FULL pipeline (YAML DSL →
// style cascade → flatten → composite renderer) for a person that opts into
// the box family's effects vocabulary, and checks the effects coexist with a
// sibling box that carries its own outline (id namespacing keeps the defs
// distinct per part).
func TestPersonEffectsThroughPipeline(t *testing.T) {
	src := `
nodes:
  scene:
    shape: composite
    parts:
      - id: user
        shape: person
        label: "Operator"
        geom: { w: 120, d: 120, h: 56 }
        style:
          palette:
            topGradient: { from: "#8EC5FF", to: "#2C5896", dir: "down" }
          effects:
            outline: { color: "#9ED0FF", width: 2, dash: "5 3", opacity: 0.9 }
            dropShadow: { dx: 4, dy: 6, blur: 5, color: "#05080F" }
            backglow: { color: "#FFAA55", radius: 30, opacity: 0.5 }
      - id: svc
        shape: rectangle
        place: { rightOf: user, gap: 2 }
        geom: { w: 120, d: 120, h: 60 }
        style:
          effects:
            outline: { color: "#FF0080", width: 3 }
`
	doc, err := ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svg := RenderWithCanvas(doc.Nodes["scene"], doc.Theme, doc.Canvas, doc.Annotations)

	for _, want := range []string{
		// topGradient lands on BOTH dome halves (namespaced per part).
		`linearGradient id="p0-person-body-left-grad"`,
		`linearGradient id="p0-person-body-right-grad"`,
		`fill="url(#p0-person-body-left-grad)"`,
		`fill="url(#p0-person-body-right-grad)"`,
		// silhouette effects.
		`filter id="p0-person-shadow"`,
		`filter="url(#p0-person-shadow)"`,
		`filter id="p0-person-backglow"`,
		`data-face="backglow"`,
		`stroke="#9ED0FF"`,
		// the sibling box keeps its own independent outline.
		`stroke="#FF0080"`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("pipeline render missing %q", want)
		}
	}
	if got := strings.Count(svg, `data-face="outline"`); got != 2 {
		t.Errorf("want 2 outline rings (person + box), got %d", got)
	}
}

// TestPersonEffectsValidateClean — the newly supported effects must NOT trip
// any validator complaint on a person part.
func TestPersonEffectsValidateClean(t *testing.T) {
	src := `
nodes:
  scene:
    shape: composite
    parts:
      - id: user
        shape: person
        geom: { w: 120, d: 120, h: 56 }
        style:
          palette:
            topGradient: { from: "#6FA8E8", to: "#2C5896", dir: "down" }
          effects:
            outline: { color: "#1F4C86", width: 2 }
            dropShadow: { dx: 3, dy: 5, blur: 4, color: "#1B2635" }
`
	doc, err := ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, is := range Validate(doc) {
		if strings.Contains(is.Path, "parts[user]") {
			t.Errorf("unexpected issue on styled person: [%s] %s: %s", is.Severity, is.Path, is.Message)
		}
	}
}

// TestPersonLabelContrastAgainstCanvas — a person's label renders screen-
// space on the CANVAS below the figure, so contrast is judged against the
// canvas, not the body fill: a dark label on a dark body but light canvas is
// fine; a light label on a light canvas warns even if the body is dark.
func TestPersonLabelContrastAgainstCanvas(t *testing.T) {
	build := func(text string) *Document {
		src := `
canvas: { background: "#F2F5FA" }
nodes:
  scene:
    shape: composite
    parts:
      - id: user
        shape: person
        label: "Ops"
        geom: { w: 120, d: 120, h: 56 }
        style:
          palette: { top: "#1B2635", left: "#141C28", right: "#0E141D" }
          text: { color: "` + text + `" }
`
		doc, err := ParseYAML([]byte(src))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return doc
	}
	hasLabelIssue := func(doc *Document) bool {
		for _, is := range Validate(doc) {
			if strings.Contains(is.Path, "parts[user]") && strings.Contains(is.Message, "person label") {
				return true
			}
		}
		return false
	}
	// Dark text on a dark BODY is fine — the label sits on the light canvas.
	if hasLabelIssue(build("#12233D")) {
		t.Errorf("dark label on light canvas must not warn, whatever the body fill")
	}
	// Light text melts into the light canvas — warn.
	if !hasLabelIssue(build("#EAF1F8")) {
		t.Errorf("light label on light canvas must warn")
	}
}

// TestPersonUnsupportedStyleWarns — style.faces and effects.pattern are
// silently dropped by the person renderer, so the validator must say so.
func TestPersonUnsupportedStyleWarns(t *testing.T) {
	src := `
nodes:
  scene:
    shape: composite
    parts:
      - id: user
        shape: person
        geom: { w: 120, d: 120, h: 56 }
        style:
          faces:
            top: { fill: { kind: solid, color: "#FFEECC" } }
          effects:
            pattern: { kind: dots, color: "#334455" }
`
	doc, err := ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	issues := Validate(doc)
	var facesHit, patternHit bool
	for _, is := range issues {
		if is.Severity != SeverityWarning {
			continue
		}
		if strings.HasSuffix(is.Path, "parts[user].style.faces") && strings.Contains(is.Message, "ignored") {
			facesHit = true
		}
		if strings.HasSuffix(is.Path, "parts[user].style.effects.pattern") && strings.Contains(is.Message, "not supported") {
			patternHit = true
		}
	}
	if !facesHit {
		t.Errorf("want a warning for style.faces on person; got %+v", issues)
	}
	if !patternHit {
		t.Errorf("want a warning for effects.pattern on person; got %+v", issues)
	}
}
