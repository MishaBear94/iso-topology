// Feature-interaction tests for the cloud's new style surface (icon +
// top/left gradients): the icon must flow DSL → flatten → renderer, the
// gradients must reach the SVG, and the pre-existing validator lints
// (default-ink icon on a dark top fill; dropped right face) must fire for
// cloud parts now that the cloud actually renders those features.
package isotopo

import (
	"strings"
	"testing"
)

// A dark cloud carrying a default-ink iso:// icon must trigger the existing
// icon-vs-fill contrast lint — before this feature the cloud dropped the icon
// so the warning was moot; now the icon really renders and really is invisible.
func TestCloudDarkIconTriggersContrastLint(t *testing.T) {
	mk := func(icon string) *Document {
		return &Document{
			Nodes: map[string]*Node{
				"scene": {Shape: "composite", Parts: []*CompositePart{{
					ID: "saas", Shape: "cloud",
					Icon:  icon,
					Style: &Style{Palette: &Palette{Top: "#0F172A", Left: "#0B1120"}},
				}}},
			},
		}
	}

	issues := VisualContrastIssues(mk("iso://glyph/cloud"))
	if !hasWarning(issues, "uses default ink color on a dark top fill") {
		t.Errorf("dark cloud + default-ink iso:// icon must trigger the contrast lint, got: %v", issues)
	}

	// The /light variant is the suggested fix — it must NOT warn.
	issues = VisualContrastIssues(mk("iso://glyph/cloud/light"))
	if hasWarning(issues, "uses default ink color on a dark top fill") {
		t.Errorf("/light icon on a dark cloud must pass, got: %v", issues)
	}
}

// A dark TOP GRADIENT on a labelled cloud is read by the fill-vs-text check
// (topFillColors walks gradient stops), so the interaction gradient×label is
// linted, not just solid fills.
func TestCloudDarkGradientTriggersTextContrastLint(t *testing.T) {
	doc := &Document{
		Nodes: map[string]*Node{
			"scene": {Shape: "composite", Parts: []*CompositePart{{
				ID: "saas", Shape: "cloud", Label: "SaaS",
				Style: &Style{
					Palette: &Palette{TopGradient: &FaceGradient{From: "#111827", To: "#0B1120"}},
					Text:    &Text{Color: "#1F2937"}, // dark text on a dark gradient
				},
			}}},
		},
	}
	issues := VisualContrastIssues(doc)
	if !hasWarning(issues, "low contrast between top fill") && !hasError(issues, "low contrast between top fill") {
		t.Errorf("dark cloud topGradient + dark label must trigger the text contrast lint, got: %v", issues)
	}
}

// palette.right / rightGradient set on the cloud itself are silently dropped
// by the renderer (no right face) — the new lint surfaces that.
func TestCloudRightPaletteLint(t *testing.T) {
	mk := func(pal *Palette) *Document {
		return &Document{
			Nodes: map[string]*Node{
				"scene": {Shape: "composite", Parts: []*CompositePart{{
					ID: "saas", Shape: "cloud", Style: &Style{Palette: pal},
				}}},
			},
		}
	}

	issues := VisualContrastIssues(mk(&Palette{Top: "#DBEAFE", Right: "#16A34A"}))
	if !hasWarning(issues, "palette.right \"#16A34A\" is ignored on a cloud") {
		t.Errorf("own-style palette.right on a cloud must warn, got: %v", issues)
	}

	issues = VisualContrastIssues(mk(&Palette{RightGradient: &FaceGradient{From: "#16A34A", To: "#052E16"}}))
	if !hasWarning(issues, "palette.rightGradient is ignored on a cloud") {
		t.Errorf("own-style palette.rightGradient on a cloud must warn, got: %v", issues)
	}

	// top/left (+ gradients) are the supported surface — no warning.
	issues = VisualContrastIssues(mk(&Palette{
		Top:          "#DBEAFE",
		Left:         "#93C5FD",
		TopGradient:  &FaceGradient{From: "#DBEAFE", To: "#93C5FD"},
		LeftGradient: &FaceGradient{From: "#93C5FD", To: "#3B82F6"},
	}))
	if hasWarning(issues, "ignored on a cloud") {
		t.Errorf("supported cloud palette keys must not warn, got: %v", issues)
	}
}

// End-to-end: a YAML cloud node with icon + gradients renders an SVG that
// actually carries the <image> and both gradient fills (DSL → flatten →
// RenderIsoCloud → scene assembly, ids namespaced per part).
func TestCloudIconGradientEndToEnd(t *testing.T) {
	src := `
canvas: { background: "#F8FAFC" }
nodes:
  scene:
    shape: composite
    parts:
      - id: saas
        shape: cloud
        label: "SaaS"
        icon: "iso://glyph/cloud"
        geom: { w: 220, d: 150, h: 26 }
        style:
          palette:
            topGradient: { from: "#E0F2FE", to: "#BAE6FD", dir: down }
            leftGradient: { from: "#7DD3FC", to: "#38BDF8" }
`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)

	if !strings.Contains(svg, "<image") || !strings.Contains(svg, "data:image/svg+xml;base64,") {
		t.Fatal("cloud icon must survive DSL → render as an inlined <image>")
	}
	if !strings.Contains(svg, "cloud-grad-top") || !strings.Contains(svg, "cloud-grad-side") {
		t.Fatal("cloud top/left gradients must reach the SVG")
	}

	// Determinism: same DSL, same bytes.
	if again := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations); again != svg {
		t.Fatal("cloud render must be deterministic")
	}
}
