package isotopo

// WS-H — body-vs-body occlusion: detection kernel, paint-order consistency
// (author painter order vs canvas.depthSort), validate / readability /
// EvaluateIsoReport wiring, the conservative repair executor, and the corpus
// guard that pins the whole shipped sample set at zero (so existing goldens,
// R values and repair fixpoints provably cannot move).

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// bodyOccFullYAML — the small cache is painted first, then a much bigger
// tower in front: the cache's whole silhouette is covered (frac = 1.00).
// Mirrors samples/topology/bodyocc-hidden-node.
const bodyOccFullYAML = `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: cache, label: Cache, shape: rectangle, geom: {w: 120, d: 120, h: 70}, offset: {wx: 0, wy: 0}}
      - {id: gateway, label: Gateway, shape: rectangle, geom: {w: 260, d: 260, h: 260}, offset: {wx: 130, wy: 130}}
`

// bodyOccPartialYAML — the tower in front covers ≈57% of the worker.
// Mirrors samples/topology/bodyocc-partial-shadow.
const bodyOccPartialYAML = `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: worker, label: Worker, shape: rectangle, geom: {w: 140, d: 140, h: 80}, offset: {wx: 0, wy: 0}}
      - {id: tower, label: Tower, shape: rectangle, geom: {w: 150, d: 150, h: 160}, offset: {wx: 150, wy: 40}}
`

func parseWSH(t *testing.T, yaml string) *Document {
	t.Helper()
	doc, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

// ── detection kernel ──────────────────────────────────────────────────────────

func TestBodyOcclusion_FullBurialDetected(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	occs := BodyOcclusions(doc)
	if len(occs) != 1 {
		t.Fatalf("want exactly 1 body occlusion, got %d: %+v", len(occs), occs)
	}
	oc := occs[0]
	if oc.Hidden != "cache" || oc.Occluder != "gateway" {
		t.Fatalf("wrong pair: %q hidden by %q", oc.Hidden, oc.Occluder)
	}
	if oc.Frac < 0.99 {
		t.Fatalf("cache should be ~fully covered, frac=%.3f", oc.Frac)
	}
	if oc.NodeID != "scene" {
		t.Fatalf("NodeID = %q, want scene", oc.NodeID)
	}
}

func TestBodyOcclusion_PartialBurialDetected(t *testing.T) {
	doc := parseWSH(t, bodyOccPartialYAML)
	occs := BodyOcclusions(doc)
	if len(occs) != 1 {
		t.Fatalf("want exactly 1 body occlusion, got %d: %+v", len(occs), occs)
	}
	oc := occs[0]
	if oc.Hidden != "worker" || oc.Occluder != "tower" {
		t.Fatalf("wrong pair: %q hidden by %q", oc.Hidden, oc.Occluder)
	}
	if oc.Frac < bodyOcclusionThreshold || oc.Frac > 0.75 {
		t.Fatalf("partial burial should sit between the threshold and ~0.75, frac=%.3f", oc.Frac)
	}
}

// A pair that merely grazes (well under the threshold) is not a defect.
func TestBodyOcclusion_GrazeNotFlagged(t *testing.T) {
	doc := parseWSH(t, `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, label: A, shape: rectangle, geom: {w: 140, d: 140, h: 80}, offset: {wx: 0, wy: 0}}
      - {id: b, label: B, shape: rectangle, geom: {w: 140, d: 140, h: 80}, offset: {wx: 150, wy: 150}}
`)
	if occs := BodyOcclusions(doc); len(occs) != 0 {
		t.Fatalf("graze must not be flagged: %+v", occs)
	}
	// …but the kernel still measures the sub-threshold residue for repair.
	occs := bodyOcclusionsInNode(doc.Scene(), doc.Theme, doc.Canvas, 0.01)
	if len(occs) != 1 || occs[0].Frac >= bodyOcclusionThreshold {
		t.Fatalf("expected one sub-threshold residue, got %+v", occs)
	}
}

// Footprint contact is out of scope: same-band footprint collisions are the
// overlap metric's defect, and place:above Z-stacks are deliberate.
func TestBodyOcclusion_FootprintContactExcluded(t *testing.T) {
	for name, yaml := range map[string]string{
		"same-band overlap": `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, label: A, shape: rectangle, geom: {w: 200, d: 200, h: 80}, offset: {wx: 0, wy: 0}}
      - {id: b, label: B, shape: rectangle, geom: {w: 200, d: 200, h: 220}, offset: {wx: 60, wy: 60}}
`,
		"deliberate z-stack": `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: plate, label: Plate, shape: rectangle, geom: {w: 240, d: 240, h: 30}}
      - {id: chip, label: Chip, shape: rectangle, geom: {w: 100, d: 100, h: 60}, place: {above: plate}}
`,
	} {
		doc := parseWSH(t, yaml)
		if occs := BodyOcclusions(doc); len(occs) != 0 {
			t.Errorf("%s: footprint-contact pair must be excluded, got %+v", name, occs)
		}
	}
}

// A see-through occluder (wireframe / low opacity) paints nothing over the
// node behind it, so it must not be reported.
func TestBodyOcclusion_GhostOccluderExempt(t *testing.T) {
	doc := parseWSH(t, `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: cache, label: Cache, shape: rectangle, geom: {w: 120, d: 120, h: 70}, offset: {wx: 0, wy: 0}}
      - {id: ceiling, label: Ceiling, shape: rectangle, geom: {w: 260, d: 260, h: 260}, offset: {wx: 130, wy: 130},
         style: {effects: {wireframe: true}}}
`)
	if occs := BodyOcclusions(doc); len(occs) != 0 {
		t.Fatalf("a wireframe ghost cannot bury a node: %+v", occs)
	}
}

// Top projection has no height-induced occlusion — the silhouette IS the
// footprint, which the overlap metric already owns.
func TestBodyOcclusion_TopProjectionSkipped(t *testing.T) {
	doc := parseWSH(t, strings.Replace(bodyOccFullYAML, "{grid: none}", "{grid: none, projection: top}", 1))
	if occs := BodyOcclusions(doc); len(occs) != 0 {
		t.Fatalf("projection: top must report none, got %+v", occs)
	}
}

func TestBodyOcclusion_Deterministic(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	a := BodyOcclusions(doc)
	b := BodyOcclusions(doc)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("detection is not deterministic:\n%+v\n%+v", a, b)
	}
}

// ── paint-order consistency: author order vs canvas.depthSort ────────────────

// bodyOccReversedYAML paints the FRONT node (larger corner sum) first and the
// back node last, so in author painter order the back node wrongly covers the
// front one. canvas.depthSort re-sorts paint back-to-front, flipping who is on
// top — the detector must follow the ACTUAL paint order in both modes, because
// that is what the pixels do.
const bodyOccReversedYAML = `
canvas: {grid: none%s}
nodes:
  scene:
    shape: composite
    parts:
      - {id: front, label: Front, shape: rectangle, geom: {w: 150, d: 150, h: 210}, offset: {wx: 160, wy: 160}}
      - {id: back, label: Back, shape: rectangle, geom: {w: 150, d: 150, h: 210}, offset: {wx: 0, wy: 0}}
`

func bodyOccPairFor(t *testing.T, yaml string) BodyOcclusion {
	t.Helper()
	doc := parseWSH(t, yaml)
	occs := BodyOcclusions(doc)
	if len(occs) != 1 {
		t.Fatalf("want exactly 1 occlusion, got %d: %+v", len(occs), occs)
	}
	return occs[0]
}

func TestBodyOcclusion_PaintOrderConsistency(t *testing.T) {
	author := bodyOccPairFor(t, strings.Replace(bodyOccReversedYAML, "%s", "", 1))
	sorted := bodyOccPairFor(t, strings.Replace(bodyOccReversedYAML, "%s", ", depthSort: true", 1))

	// Author order: "back" is written last, so it paints over "front".
	if author.Hidden != "front" || author.Occluder != "back" {
		t.Fatalf("author order: want front hidden by back, got %q hidden by %q", author.Hidden, author.Occluder)
	}
	// depthSort: paint order is corrected back-to-front, so "front" now
	// covers "back" — the occlusion DIRECTION flips with the paint order.
	if sorted.Hidden != "back" || sorted.Occluder != "front" {
		t.Fatalf("depthSort: want back hidden by front, got %q hidden by %q", sorted.Hidden, sorted.Occluder)
	}

	// Pixel contract: the reported occluder must be the LATER-painted body in
	// the real SVG each mode emits (paint order == document order).
	for mode, tc := range map[string]struct {
		yaml   string
		hidden string
	}{
		"author":    {strings.Replace(bodyOccReversedYAML, "%s", "", 1), "front"},
		"depthSort": {strings.Replace(bodyOccReversedYAML, "%s", ", depthSort: true", 1), "back"},
	} {
		doc := parseWSH(t, tc.yaml)
		svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
		hi := strings.Index(svg, `data-part-id="`+tc.hidden+`"`)
		occluder := "back"
		if tc.hidden == "back" {
			occluder = "front"
		}
		oi := strings.Index(svg, `data-part-id="`+occluder+`"`)
		if hi < 0 || oi < 0 {
			t.Fatalf("%s: could not locate both parts in the SVG", mode)
		}
		if hi > oi {
			t.Errorf("%s: hidden part %q must be painted BEFORE occluder %q in the SVG (hidden@%d, occluder@%d)",
				mode, tc.hidden, occluder, hi, oi)
		}
	}
}

// ── wiring: validate / readability / EvaluateIsoReport ───────────────────────

func TestBodyOcclusion_ValidateWarns(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	var hit *Issue
	issues := Validate(doc)
	for i := range issues {
		if strings.Contains(issues[i].Message, "is hidden behind node") {
			hit = &issues[i]
			break
		}
	}
	if hit == nil {
		t.Fatal("Validate must surface the body occlusion")
	}
	if hit.Severity != SeverityWarning {
		t.Fatalf("severity = %q, want warning", hit.Severity)
	}
	if hit.Path != "nodes.scene.parts[cache]" {
		t.Fatalf("path must point at the HIDDEN node, got %q", hit.Path)
	}
	if !strings.Contains(hit.Message, `"gateway"`) || !strings.Contains(hit.Message, "100%") {
		t.Fatalf("message must name the occluder and the covered fraction: %s", hit.Message)
	}
	if !strings.Contains(hit.Suggest, "place/offset") || !strings.Contains(hit.Suggest, "depthSort") {
		t.Fatalf("suggest must offer place/offset and depthSort: %s", hit.Suggest)
	}
}

func TestBodyOcclusion_ReadabilityComponent(t *testing.T) {
	doc := parseWSH(t, bodyOccPartialYAML)
	r := Readability(doc)
	if r.BodyOcclusions != 1 {
		t.Fatalf("BodyOcclusions = %d, want 1", r.BodyOcclusions)
	}
	// The component must be priced into Cost at wBodyOcclusion.
	base := wOcclusion*float64(r.Occlusions) +
		wBodyOcclusion*float64(r.BodyOcclusions) +
		wOverlap*float64(r.Overlaps) +
		wTunnel*float64(r.Tunnels) +
		wCrossing*float64(r.Crossings) +
		wBend*float64(r.Bends) +
		wLength*r.Length
	if diff := r.Cost - base; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Cost %.6f does not include the body-occlusion term (recomputed %.6f)", r.Cost, base)
	}

	clean := parseWSH(t, `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, label: A, shape: rectangle, geom: {w: 140, d: 140, h: 80}}
`)
	if r := Readability(clean); r.BodyOcclusions != 0 {
		t.Fatalf("clean scene must have 0 body occlusions, got %d", r.BodyOcclusions)
	}
}

func TestBodyOcclusion_EvaluateIsoReportExposesCount(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	rep := EvaluateIsoReport(doc.Scene(), doc.Theme, doc.Canvas)
	if rep.BodyOcclusions != 1 {
		t.Fatalf("IsoReport.BodyOcclusions = %d, want 1", rep.BodyOcclusions)
	}
}

// ── repair executor ───────────────────────────────────────────────────────────

func TestBodyOcclusion_RepairClearsFullBurial(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	before := Readability(doc)
	if before.BodyOcclusions != 1 {
		t.Fatalf("fixture should start buried, got %d", before.BodyOcclusions)
	}
	_, iters := RepairScene(doc)
	after := Readability(doc)
	if iters == 0 {
		t.Fatal("repair should have run")
	}
	if after.BodyOcclusions != 0 {
		t.Fatalf("repair must uncover the node, still %d", after.BodyOcclusions)
	}
	if after.Cost > before.Cost {
		t.Fatalf("repair may never raise cost: %.3f → %.3f", before.Cost, after.Cost)
	}
	// Fixpoint: a second pass finds nothing left to do.
	if _, again := RepairScene(doc); again != 0 {
		t.Fatalf("repaired scene must be a fixpoint, ran %d iters", again)
	}
}

func TestBodyOcclusion_RepairClearsPartialBurial(t *testing.T) {
	doc := parseWSH(t, bodyOccPartialYAML)
	_, iters := RepairScene(doc)
	if iters == 0 {
		t.Fatal("repair should have run")
	}
	if r := Readability(doc); r.BodyOcclusions != 0 || r.Overlaps != 0 || r.Occlusions != 0 {
		t.Fatalf("scene not clean after repair: %+v", r)
	}
}

// One measured round must strictly reduce the covered fraction and never
// raise readability cost — the acceptance contract of the executor.
func TestBodyOcclusion_RepairRoundIsMeasured(t *testing.T) {
	doc := parseWSH(t, bodyOccFullYAML)
	scene := doc.Scene()
	fracBefore := bodyOccTotalFrac(scene, doc.Theme, doc.Canvas)
	costBefore := Readability(doc).Cost
	if !repairBodyOcclusion(doc, scene) {
		t.Fatal("executor should act on a buried scene")
	}
	if fracAfter := bodyOccTotalFrac(scene, doc.Theme, doc.Canvas); fracAfter >= fracBefore {
		t.Fatalf("covered fraction must strictly drop: %.3f → %.3f", fracBefore, fracAfter)
	}
	if costAfter := Readability(doc).Cost; costAfter > costBefore+1e-9 {
		t.Fatalf("cost must not rise: %.3f → %.3f", costBefore, costAfter)
	}
}

// The executor must be a strict no-op (offsets byte-identical) when there is
// nothing above the threshold, and under projection: top.
func TestBodyOcclusion_RepairNoOpWhenClean(t *testing.T) {
	for name, yaml := range map[string]string{
		"sub-threshold graze": `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, label: A, shape: rectangle, geom: {w: 140, d: 140, h: 80}, offset: {wx: 0, wy: 0}}
      - {id: b, label: B, shape: rectangle, geom: {w: 140, d: 140, h: 80}, offset: {wx: 150, wy: 150}}
`,
		"top projection": strings.Replace(bodyOccFullYAML, "{grid: none}", "{grid: none, projection: top}", 1),
	} {
		doc := parseWSH(t, yaml)
		scene := doc.Scene()
		saved := snapshotOffsetsDeep(scene.Parts)
		if repairBodyOcclusion(doc, scene) {
			t.Errorf("%s: executor must not act", name)
		}
		if !reflect.DeepEqual(saved, snapshotOffsetsDeep(scene.Parts)) {
			t.Errorf("%s: offsets were mutated", name)
		}
	}
}

// bodyOccActuator must resolve the direct child of the deepest scope that
// separates the pair, so an in-group burial nudges the child, not the group.
func TestBodyOcclusion_ActuatorScope(t *testing.T) {
	doc := parseWSH(t, `
canvas: {grid: none}
nodes:
  scene:
    shape: composite
    parts:
      - id: cluster
        shape: group
        parts:
          - {id: cache, label: Cache, shape: rectangle, geom: {w: 120, d: 120, h: 70}, offset: {wx: 0, wy: 0}}
          - {id: gateway, label: Gateway, shape: rectangle, geom: {w: 260, d: 260, h: 260}, offset: {wx: 130, wy: 130}}
      - {id: loose, label: Loose, shape: rectangle, geom: {w: 100, d: 100, h: 60}, offset: {wx: 700, wy: 700}}
`)
	scene := doc.Scene()
	inGroup := bodyOccActuator(scene.Parts, "cache", "gateway")
	if inGroup == nil || inGroup.ID != "cache" {
		t.Fatalf("in-group pair must actuate the child itself, got %+v", inGroup)
	}
	crossScope := bodyOccActuator(scene.Parts, "cache", "loose")
	if crossScope == nil || crossScope.ID != "cluster" {
		t.Fatalf("cross-scope pair must actuate the top-level sibling, got %+v", crossScope)
	}
}

// ── corpus guard ──────────────────────────────────────────────────────────────

// TestBodyOcclusion_CorpusStaysZero pins every shipped sample (bench + node +
// topology, minus the two bodyocc-* demos built to trigger it) at ZERO body
// occlusions. This is what guarantees the new detector/weight/repair change
// nothing for the existing corpus: no new validate warnings, identical R, and
// a repair executor that never arms — so existing goldens and repair
// fixpoints are provably untouched.
func TestBodyOcclusion_CorpusStaysZero(t *testing.T) {
	var files []string
	bench, _ := filepath.Glob("samples/bench/*.yaml")
	files = append(files, bench...)
	for _, cat := range []string{"node", "topology"} {
		dirs, _ := filepath.Glob("samples/" + cat + "/*")
		for _, dir := range dirs {
			if strings.Contains(dir, "bodyocc-") {
				continue // the WS-H demos exist to trigger the detector
			}
			for _, base := range []string{"input.yaml", "input.json", "input.d2"} {
				f := filepath.Join(dir, base)
				if _, err := os.Stat(f); err == nil {
					files = append(files, f)
				}
			}
		}
	}
	if len(files) < 20 {
		t.Fatalf("corpus glob looks broken: %d files", len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		ext := strings.TrimPrefix(filepath.Ext(f), ".")
		doc, err := loadGoldenDoc(ext, data)
		if err != nil {
			t.Fatalf("load %s: %v", f, err)
		}
		if occs := BodyOcclusions(doc); len(occs) != 0 {
			t.Errorf("%s: existing sample now trips body occlusion — threshold miscalibrated: %+v", f, occs)
		}
		if scene := doc.Scene(); scene != nil {
			saved := snapshotOffsetsDeep(scene.Parts)
			if repairBodyOcclusion(doc, scene) {
				t.Errorf("%s: repair executor armed on an existing sample — fixpoint broken", f)
			}
			if !reflect.DeepEqual(saved, snapshotOffsetsDeep(scene.Parts)) {
				t.Errorf("%s: repair executor mutated offsets on an existing sample", f)
			}
		}
	}
}

// The two shipped demos must keep demonstrating the defect (they carry the
// golden of the UNREPAIRED render — the CLI repairs by default, the golden
// pipeline does not).
func TestBodyOcclusion_DemosTrigger(t *testing.T) {
	for dir, want := range map[string]string{
		"samples/topology/bodyocc-hidden-node":    "cache",
		"samples/topology/bodyocc-partial-shadow": "worker",
	} {
		data, err := os.ReadFile(filepath.Join(dir, "input.yaml"))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		doc, err := Parse(data)
		if err != nil {
			t.Fatalf("parse %s: %v", dir, err)
		}
		occs := BodyOcclusions(doc)
		if len(occs) != 1 || occs[0].Hidden != want {
			t.Errorf("%s: want %q hidden, got %+v", dir, want, occs)
		}
	}
}
