// canvas.depthSort (v5.2) — feature tests.
//
// Contract under test:
//  1. default OFF: painter order stays the author's source order,
//     byte-for-byte (existing goldens must never move);
//  2. ON: lowered parts paint back-to-front by world depth (corner sum
//     x+y+z), merged ACROSS groups, with a STABLE sort (equal depth
//     keeps author order — determinism);
//  3. the substrate/platform partition survives: slabs paint first,
//     group captions still paint above their children;
//  4. connector routes layer by endpoint depth: a part strictly behind
//     both endpoints paints under the route, the endpoints and anything
//     in front still paint over it;
//  5. interactions: stack replication, capabilities index, validator.
package isotopo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// reversedScene lists three diagonal boxes FRONT-most first — the
// author order that mispaints under the legacy painter.
const depthReversedScene = `
nodes:
  scene:
    shape: composite
    parts:
      - id: edge
        shape: rectangle
        geom: {w: 120, d: 100, h: 60}
        offset: {wx: 240, wy: 240}
      - id: mid
        shape: rectangle
        geom: {w: 120, d: 100, h: 90}
        offset: {wx: 130, wy: 130}
      - id: core
        shape: rectangle
        geom: {w: 120, d: 100, h: 150}
        offset: {wx: 20, wy: 20}
`

func renderYAML(t *testing.T, src string) string {
	t.Helper()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return renderTopology(doc)
}

// partOrder returns the first-occurrence index of each id's
// data-part-id attribute, failing if any id is missing.
func partOrder(t *testing.T, svg string, ids ...string) []int {
	t.Helper()
	out := make([]int, len(ids))
	for i, id := range ids {
		needle := fmt.Sprintf(`data-part-id="%s"`, id)
		at := strings.Index(svg, needle)
		if at < 0 {
			t.Fatalf("part %q not found in svg", id)
		}
		out[i] = at
	}
	return out
}

func assertAscending(t *testing.T, svg string, ids ...string) {
	t.Helper()
	pos := partOrder(t, svg, ids...)
	for i := 1; i < len(pos); i++ {
		if pos[i] <= pos[i-1] {
			t.Fatalf("paint order wrong: %q (at %d) should come after %q (at %d)",
				ids[i], pos[i], ids[i-1], pos[i-1])
		}
	}
}

// TestDepthSortDefaultOff — without the flag the author order is the
// painter order (legacy behaviour), and depthSort: false is
// byte-identical to omitting the field entirely.
func TestDepthSortDefaultOff(t *testing.T) {
	absent := renderYAML(t, "canvas:\n  grid: none\n"+depthReversedScene)
	explicitOff := renderYAML(t, "canvas:\n  grid: none\n  depthSort: false\n"+depthReversedScene)
	if absent != explicitOff {
		t.Fatalf("depthSort: false must be byte-identical to the field being absent")
	}
	// Author order = paint order when off (edge written first paints first).
	assertAscending(t, absent, "edge", "mid", "core")
}

// TestDepthSortReordersAuthorOrder — the same scene with the flag on
// paints back-to-front regardless of source order.
func TestDepthSortReordersAuthorOrder(t *testing.T) {
	svg := renderYAML(t, "canvas:\n  grid: none\n  depthSort: true\n"+depthReversedScene)
	assertAscending(t, svg, "core", "mid", "edge")
}

// TestDepthSortMergesAcrossGroups — children of DIFFERENT groups sort
// into one global depth order (the legacy painter kept each group's
// children contiguous, so a back group written last painted over a
// front group written first), and the substrate block itself sorts too
// while staying in front of (i.e. under) every body.
func TestDepthSortMergesAcrossGroups(t *testing.T) {
	src := `
canvas: {grid: none, depthSort: true}
nodes:
  scene:
    shape: composite
    parts:
      - id: serving          # FRONT group written first
        shape: group
        geom: {w: 320, d: 200, h: 24}
        offset: {wx: 260, wy: 190}
        parts:
          - id: api
            shape: rectangle
            geom: {w: 120, d: 100, h: 60}
            offset: {wx: 30, wy: 50}
      - id: data             # BACK group written last
        shape: group
        geom: {w: 320, d: 180, h: 24}
        offset: {wx: 120, wy: 0}
        parts:
          - id: warehouse
            shape: rectangle
            geom: {w: 140, d: 120, h: 190}
            offset: {wx: 40, wy: 30}
`
	svg := renderYAML(t, src)
	// Substrates first (both slabs before both bodies), back slab first.
	assertAscending(t, svg, "data", "serving", "warehouse", "api")
}

// TestDepthSortStableTies — equal depth keys keep the author's relative
// order (sort.SliceStable), so same-depth scenes stay deterministic and
// unchanged.
func TestDepthSortStableTies(t *testing.T) {
	src := `
canvas: {grid: none, depthSort: true}
nodes:
  scene:
    shape: composite
    parts:
      - id: left             # (0, 100) → corner sum 100
        shape: rectangle
        geom: {w: 80, d: 80, h: 60}
        offset: {wx: 0, wy: 100}
      - id: right            # (100, 0) → corner sum 100 (tie)
        shape: rectangle
        geom: {w: 80, d: 80, h: 60}
        offset: {wx: 100, wy: 0}
`
	svg := renderYAML(t, src)
	assertAscending(t, svg, "left", "right")
}

// TestDepthSortGroupCaptionStaysOnTop — the injected group caption
// (iso_text lowered AFTER the children) must keep painting above them:
// its depth key pins to +Inf so the sort cannot bury it.
func TestDepthSortGroupCaptionStaysOnTop(t *testing.T) {
	src := `
canvas: {grid: none, depthSort: true}
nodes:
  scene:
    shape: composite
    parts:
      - id: zone
        shape: group
        label: Zone
        geom: {w: 300, d: 200, h: 24}
        parts:
          - id: box
            shape: rectangle
            geom: {w: 100, d: 100, h: 80}
            offset: {wx: 100, wy: 30}
`
	svg := renderYAML(t, src)
	// The caption's <g> carries the group id (data-part-id="zone") and is
	// the LAST zone-tagged group; it must come after the child box.
	lastZone := strings.LastIndex(svg, `data-part-id="zone"`)
	boxAt := strings.Index(svg, `data-part-id="box"`)
	if boxAt < 0 || lastZone < boxAt {
		t.Fatalf("group caption must paint after its children (caption at %d, child at %d)", lastZone, boxAt)
	}
}

// TestDepthSortConnectorLayering — with depthSort on, a route splices
// just below its back-most ENDPOINT: parts strictly behind both
// endpoints paint under the route (they can no longer swallow it),
// while the endpoints themselves still paint over it.
func TestDepthSortConnectorLayering(t *testing.T) {
	src := `
canvas: {grid: none, depthSort: %s}
nodes:
  scene:
    shape: composite
    parts:
      - id: front
        shape: rectangle
        geom: {w: 100, d: 100, h: 60}
        offset: {wx: 300, wy: 300}
      - id: middle
        shape: rectangle
        geom: {w: 100, d: 100, h: 60}
        offset: {wx: 150, wy: 150}
      - id: back
        shape: rectangle
        geom: {w: 100, d: 100, h: 120}
        offset: {wx: 0, wy: 0}
    connectors:
      - {from: middle, to: front, routing: orthogonal, arrow: triangle}
`
	svg := renderYAML(t, fmt.Sprintf(src, "true"))
	route := strings.Index(svg, `data-connector="0"`)
	if route < 0 {
		t.Fatalf("route not emitted")
	}
	pos := partOrder(t, svg, "back", "middle", "front")
	if !(route > pos[0] && route < pos[1]) {
		t.Fatalf("route (at %d) must splice above the back part (%d) and below its back endpoint middle (%d)",
			route, pos[0], pos[1])
	}

	// Legacy mode for contrast: the route sits under EVERY body,
	// including the unrelated back part.
	legacy := renderYAML(t, fmt.Sprintf(src, "false"))
	route = strings.Index(legacy, `data-connector="0"`)
	first := strings.Index(legacy, `data-part-id=`)
	if route < 0 || first < 0 || route > first {
		t.Fatalf("legacy mode must keep the route under all bodies (route at %d, first part at %d)", route, first)
	}
}

// TestDepthSortStackInteraction — stack replication produces ascending
// z copies; the depth sort must keep bottom→top order (a copy's key
// grows with its z step) and keep the clones addressable.
func TestDepthSortStackInteraction(t *testing.T) {
	src := `
canvas: {grid: none, depthSort: true}
nodes:
  scene:
    shape: composite
    parts:
      - id: front
        shape: rectangle
        geom: {w: 100, d: 100, h: 40}
        offset: {wx: 260, wy: 260}
      - id: pods
        shape: rectangle
        geom: {w: 100, d: 100, h: 24}
        offset: {wx: 40, wy: 40}
        stack: {count: 3, gap: 40}
`
	svg := renderYAML(t, src)
	assertAscending(t, svg, "pods", "pods~1", "pods~2", "front")
}

// TestCapabilitiesCanvasDepthSort — the new field is discoverable in
// the machine-readable capabilities index (document.canvas primitive).
func TestCapabilitiesCanvasDepthSort(t *testing.T) {
	rep := CapabilityReport()
	for _, p := range rep.Primitives {
		if p.Name != "canvas-grid" {
			continue
		}
		if _, ok := p.Fields["depthSort"]; !ok {
			t.Fatalf("canvas-grid primitive must document the depthSort field")
		}
		return
	}
	t.Fatalf("canvas-grid primitive missing from capabilities")
}

// TestValidateDepthSortTopProjection — depthSort is inert in the
// top-down plan view; the validator must say so (and stay silent for
// the default iso projection).
func TestValidateDepthSortTopProjection(t *testing.T) {
	doc, err := Parse([]byte(`
canvas: {projection: top, depthSort: true}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle}
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	found := false
	for _, iss := range Validate(doc) {
		if iss.Path == "canvas.depthSort" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a canvas.depthSort warning under projection: top")
	}

	doc2, err := Parse([]byte(`
canvas: {depthSort: true}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle}
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, iss := range Validate(doc2) {
		if iss.Path == "canvas.depthSort" {
			t.Fatalf("iso projection must not warn about depthSort: %s", iss.Message)
		}
	}
}

// TestDepthSortSamplesOverlapMeaningfully — guard that the two shipped
// acceptance samples actually exercise the feature: in each, a pair the
// sort reorders overlaps in SCREEN projection (otherwise the golden
// would prove nothing about occlusion).
func TestDepthSortSamplesOverlapMeaningfully(t *testing.T) {
	cases := []struct {
		dir   string
		back  string
		front string
	}{
		{"samples/topology/depthsort-cross-group", "warehouse", "api"},
		{"samples/topology/depthsort-author-order", "core", "mid"},
	}
	for _, tc := range cases {
		data, ext, err := readInput(tc.dir)
		if err != nil {
			t.Fatalf("%s: %v", tc.dir, err)
		}
		doc, err := loadGoldenDoc(ext, data)
		if err != nil {
			t.Fatalf("%s: %v", tc.dir, err)
		}
		svg := renderTopology(doc)
		assertAscending(t, svg, tc.back, tc.front)
		// The depth conflict must be PROJECTION-only: the two parts'
		// projected screen bboxes intersect while their world footprints
		// stay disjoint — exactly the case author order cannot solve.
		scene := doc.Scene()
		applyLayout(scene, doc.Canvas)
		flat := lowerCompositeParts(scene.Parts, 0, 0, 0)
		box := func(id string) [6]float64 {
			for _, p := range flat {
				if p.ID != id {
					continue
				}
				x, y, z := 0.0, 0.0, 0.0
				if p.Offset != nil {
					x, y, z = p.Offset.WX, p.Offset.WY, p.Offset.WZ
				}
				return [6]float64{x, y, z, p.Geom.W, p.Geom.D, p.Geom.H}
			}
			t.Fatalf("%s: part %q not found after lowering", tc.dir, id)
			return [6]float64{}
		}
		b, f := box(tc.back), box(tc.front)
		bx0, by0, bx1, by1 := iso25d.ProjectedBounds([][6]float64{b})
		fx0, fy0, fx1, fy1 := iso25d.ProjectedBounds([][6]float64{f})
		if bx1 <= fx0 || fx1 <= bx0 || by1 <= fy0 || fy1 <= by0 {
			t.Fatalf("%s: %q and %q projections do not overlap — sample proves nothing", tc.dir, tc.back, tc.front)
		}
		if b[0]+b[3] > f[0] && f[0]+f[3] > b[0] && b[1]+b[4] > f[1] && f[1]+f[4] > b[1] {
			t.Fatalf("%s: %q and %q overlap in WORLD footprint — the sample must be projection-only", tc.dir, tc.back, tc.front)
		}
	}
}
