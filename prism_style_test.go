package isotopo

// v3.12 — end-to-end DSL coverage for the prism wall features:
// palette left/rightGradient dispatched onto the prism's visible wall
// groups, and Projected face fills pinned to each wall's iso plane via
// per-side matrices. Exercises the full pipeline (Parse → flatten →
// iso25d render → inject), not just the renderer.

import (
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

const prismGradientYAML = `
nodes:
  gw:
    shape: hexprism
    geom: { w: 120, d: 120, h: 40 }
    label: "GW"
    style:
      palette:
        top: "#EDE9FE"
        leftGradient:  { from: "#6D28D9", to: "#3B1D74", dir: "down" }
        rightGradient: { from: "#8B5CF6", to: "#5B3DB0", dir: "down" }
      text: { color: "#3B1D74" }
`

func TestPrismPaletteGradient_DSLPipeline(t *testing.T) {
	doc, err := Parse([]byte(prismGradientYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svg := renderTopology(doc)
	if svg == "" {
		t.Fatal("empty render")
	}
	// Defs present (composite prefixing may add a pN- namespace).
	for _, id := range []string{"prism-grad-left", "prism-grad-right"} {
		if !strings.Contains(svg, id) {
			t.Errorf("gradient def %s missing from pipeline output", id)
		}
	}
	// Walls reference them: hexprism side1 faces +x (right group),
	// side2/side3 face +y (left group).
	if !strings.Contains(svg, "prism-grad-right)\"") {
		t.Errorf("no wall references the right gradient")
	}
	if !strings.Contains(svg, "prism-grad-left)\"") {
		t.Errorf("no wall references the left gradient")
	}
	// The DSL must still validate without errors.
	for _, iss := range Validate(doc) {
		if iss.Severity == SeverityError {
			t.Errorf("unexpected validation error: %s — %s", iss.Path, iss.Message)
		}
	}
}

const prismProjectedYAML = `
canvas: { background: "#0E1016" }
nodes:
  gate:
    shape: hexprism
    geom: { w: 120, d: 120, h: 40 }
    label: "Gate"
    style:
      palette: { top: "#1D2333" }
      faces:
        "side2":
          fill:
            kind: pattern
            pattern: { kind: hatch, color: "#7DD3FC", spacing: 6, angle: 0, projected: true }
      text: { color: "#E2E8F0" }
`

func TestPrismProjectedSidePattern_DSLPipeline(t *testing.T) {
	doc, err := Parse([]byte(prismProjectedYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svg := renderTopology(doc)
	if svg == "" {
		t.Fatal("empty render")
	}
	// side2 of a square-footprint hexprism: base edge direction
	// (-cos30, sin30) → wall matrix (1.1830, 0.1830) — the numeric
	// contract shared with iso25d's unit tests.
	want := iso25d.SideMatrixForEdge(-iso25d.Cos30, iso25d.Sin30)
	if want != "matrix(1.1830 0.1830 0 1 0 0)" {
		t.Fatalf("matrix contract drifted: %q", want)
	}
	if !strings.Contains(svg, `patternTransform="`+want) {
		t.Errorf("side2 projected hatch must carry wall matrix %q; pipeline output lacks it", want)
	}
	for _, iss := range Validate(doc) {
		if iss.Severity == SeverityError {
			t.Errorf("unexpected validation error: %s — %s", iss.Path, iss.Message)
		}
	}
}
