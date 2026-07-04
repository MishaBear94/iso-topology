package isotopo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadBenchELK parses one samples/bench-elk scene. The files are
// authored WITH engine: elk (they demonstrate the DSL); engine=""
// strips it to obtain the built-in-solver baseline of the same graph.
func loadBenchELK(t *testing.T, name, engine string) *Document {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("samples", "bench-elk", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	scene := doc.Scene()
	if scene == nil || scene.Layout == nil {
		t.Fatalf("%s: no scene layout", name)
	}
	scene.Layout.Engine = engine
	return doc
}

// TestLayoutEngineELKImprovesDenseDAGs is the WS-I acceptance gate:
// on dense-DAG benches (15+ nodes, multi-tier fan-in/fan-out) the elk
// path must cut the readability crossing+tunnel weighted cost by ≥30%
// vs the built-in auto solver, and no scene may get worse.
func TestLayoutEngineELKImprovesDenseDAGs(t *testing.T) {
	weighted := func(r ReadabilityReport) float64 {
		return wCrossing*float64(r.Crossings) + wTunnel*float64(r.Tunnels)
	}
	defSum, elkSum := 0.0, 0.0
	for _, name := range []string{"dense-fabric.yaml", "wide-funnel.yaml"} {
		defR := Readability(loadBenchELK(t, name, ""))
		elkR := Readability(loadBenchELK(t, name, "elk"))
		dw, ew := weighted(defR), weighted(elkR)
		t.Logf("%s: default crossings=%d tunnels=%d (w=%.1f, R=%.3f) → elk crossings=%d tunnels=%d (w=%.1f, R=%.3f)",
			name, defR.Crossings, defR.Tunnels, dw, defR.Score,
			elkR.Crossings, elkR.Tunnels, ew, elkR.Score)
		if ew > dw {
			t.Errorf("%s: elk made crossings+tunnels WORSE (%.1f → %.1f)", name, dw, ew)
		}
		if elkR.Overlaps > defR.Overlaps {
			t.Errorf("%s: elk introduced footprint overlaps (%d → %d)", name, defR.Overlaps, elkR.Overlaps)
		}
		defSum += dw
		elkSum += ew
	}
	if defSum <= 0 {
		t.Fatalf("bench scenes no longer stress the default solver (weighted sum %.1f); rebuild them denser", defSum)
	}
	if elkSum > 0.7*defSum {
		t.Errorf("elk improvement below the 30%% gate: weighted crossings+tunnels %.1f → %.1f (%.0f%% better)",
			defSum, elkSum, 100*(1-elkSum/defSum))
	}
}

// TestLayoutEngineDeterministic — same input, two full renders, byte-
// identical SVG (the goja engines must be pure functions of the input).
func TestLayoutEngineDeterministic(t *testing.T) {
	for _, name := range []string{"dense-fabric.yaml", "wide-funnel.yaml"} {
		render := func() string {
			doc := loadBenchELK(t, name, "elk")
			scene := doc.Scene()
			return RenderWithCanvas(scene, doc.Theme, doc.Canvas, doc.Annotations)
		}
		if a, b := render(), render(); a != b {
			t.Errorf("%s: two renders of the same input differ — elk layout is not deterministic", name)
		}
	}
}

// TestLayoutEngineGridSnap — engine coordinates must land on the cell
// lattice, so engine output stays in register with the canvas grid and
// the orthogonal connector channels.
func TestLayoutEngineGridSnap(t *testing.T) {
	doc := loadBenchELK(t, "dense-fabric.yaml", "elk")
	scene := doc.Scene()
	cell := layoutCell(scene, doc.Canvas)
	issues := applyLayout(scene, doc.Canvas)
	for _, iss := range issues {
		if iss.Severity == SeverityError {
			t.Fatalf("unexpected error issue: %+v", iss)
		}
	}
	for _, p := range scene.Parts {
		if p == nil || p.Offset == nil {
			t.Fatalf("part %q not positioned", p.ID)
		}
		for _, v := range []float64{p.Offset.WX, p.Offset.WY} {
			cells := v / cell
			if cells != float64(int(cells)) {
				t.Errorf("part %q offset %.2f is off the %g-cell lattice", p.ID, v, cell)
			}
		}
	}
}

// TestLayoutEngineUnknownFallsBack — a typo'd engine must produce a
// did-you-mean error AND still lay the scene out via the built-in path
// (positions identical to engine "").
func TestLayoutEngineUnknownFallsBack(t *testing.T) {
	bad := loadBenchELK(t, "dense-fabric.yaml", "elkk")
	badScene := bad.Scene()
	issues := applyLayout(badScene, bad.Canvas)
	found := false
	for _, iss := range issues {
		if strings.Contains(iss.Message, `unknown layout engine "elkk"`) {
			found = true
			if iss.Suggest != "elk" {
				t.Errorf("did-you-mean suggested %q, want %q", iss.Suggest, "elk")
			}
		}
	}
	if !found {
		t.Errorf("no unknown-engine issue reported; got %+v", issues)
	}

	ref := loadBenchELK(t, "dense-fabric.yaml", "")
	refScene := ref.Scene()
	applyLayout(refScene, ref.Canvas)
	for i, p := range refScene.Parts {
		q := badScene.Parts[i]
		if p.Offset == nil || q.Offset == nil {
			t.Fatalf("part %d missing offset after layout", i)
		}
		if p.Offset.WX != q.Offset.WX || p.Offset.WY != q.Offset.WY {
			t.Errorf("part %q: fallback position (%g,%g) differs from built-in path (%g,%g)",
				q.ID, q.Offset.WX, q.Offset.WY, p.Offset.WX, p.Offset.WY)
		}
	}
}

// TestLayoutEngineValidate — the enum plumbing: unknown engine values
// get did-you-mean at validate time wherever they appear, and an
// engine on a layout that can never use one warns instead of silently
// dropping the key.
func TestLayoutEngineValidate(t *testing.T) {
	t.Run("root auto unknown engine", func(t *testing.T) {
		doc, err := Parse([]byte(`
nodes:
  scene:
    shape: composite
    layout: { mode: auto, engine: elkk }
    parts:
      - { id: a, shape: rectangle }
      - { id: b, shape: rectangle }
    connectors:
      - { from: a, to: b }
`))
		if err != nil {
			t.Fatal(err)
		}
		assertIssue(t, Validate(doc), SeverityError, `unknown layout engine "elkk"`, "elk")
	})

	t.Run("root non-auto engine warns", func(t *testing.T) {
		doc, err := Parse([]byte(`
nodes:
  scene:
    shape: composite
    layout: { mode: row, engine: elk }
    parts:
      - { id: a, shape: rectangle }
`))
		if err != nil {
			t.Fatal(err)
		}
		assertIssue(t, Validate(doc), SeverityWarning, "only applies to the scene root", "")
	})

	t.Run("group engine warns with hint", func(t *testing.T) {
		doc, err := Parse([]byte(`
nodes:
  scene:
    shape: composite
    parts:
      - id: grp
        shape: group
        layout: { mode: row, engine: dagre }
        parts:
          - { id: a, shape: rectangle }
`))
		if err != nil {
			t.Fatal(err)
		}
		assertIssue(t, Validate(doc), SeverityWarning, "only applies to the scene root", "")
	})

	t.Run("valid engine on root auto is clean", func(t *testing.T) {
		doc, err := Parse([]byte(`
nodes:
  scene:
    shape: composite
    layout: { mode: auto, engine: elk }
    parts:
      - { id: a, shape: rectangle, label: A }
      - { id: b, shape: rectangle, label: B }
    connectors:
      - { from: a, to: b }
`))
		if err != nil {
			t.Fatal(err)
		}
		for _, iss := range Validate(doc) {
			if strings.Contains(iss.Path, "engine") {
				t.Errorf("valid engine flagged: %+v", iss)
			}
		}
	})
}

func assertIssue(t *testing.T, issues []Issue, sev Severity, msgFragment, suggest string) {
	t.Helper()
	for _, iss := range issues {
		if iss.Severity == sev && strings.Contains(iss.Message, msgFragment) {
			if suggest != "" && iss.Suggest != suggest {
				t.Errorf("issue %q: suggest %q, want %q", iss.Message, iss.Suggest, suggest)
			}
			return
		}
	}
	t.Errorf("no %v issue containing %q; got %+v", sev, msgFragment, issues)
}

// TestLayoutEnginePinnedOffsetSurvives — feature interaction with the
// v4.4 pin contract: a part with an explicit offset must keep it under
// engine layout (Studio drags stay put), while its neighbours are
// engine-positioned.
func TestLayoutEnginePinnedOffsetSurvives(t *testing.T) {
	doc := loadBenchELK(t, "dense-fabric.yaml", "elk")
	scene := doc.Scene()
	scene.Parts[0].Offset = &WorldPoint{WX: 4321, WY: 1234}
	applyLayout(scene, doc.Canvas)
	if got := scene.Parts[0].Offset; got.WX != 4321 || got.WY != 1234 {
		t.Errorf("pinned offset overwritten: got (%g,%g), want (4321,1234)", got.WX, got.WY)
	}
	for _, p := range scene.Parts[1:] {
		if p.Offset == nil {
			t.Errorf("unpinned part %q not positioned", p.ID)
		}
	}
}

// TestLayoutEngineDagreRuns — the second engine value also produces a
// complete deterministic layout (dagre and elk share the bridge).
func TestLayoutEngineDagreRuns(t *testing.T) {
	doc := loadBenchELK(t, "wide-funnel.yaml", "dagre")
	scene := doc.Scene()
	issues := applyLayout(scene, doc.Canvas)
	for _, iss := range issues {
		if strings.Contains(iss.Message, "falling back") || strings.Contains(iss.Message, "built-in auto layout") {
			t.Fatalf("dagre fell back: %+v", iss)
		}
	}
	for _, p := range scene.Parts {
		if p.Offset == nil {
			t.Errorf("part %q not positioned by dagre", p.ID)
		}
	}
}
