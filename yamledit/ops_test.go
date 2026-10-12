package yamledit

import (
	"strings"
	"testing"
)

// ── Drag: UpsertInlineKey writes an absolute offset, flow + block ──────────

func TestUpsertInlineKeyFlowForm(t *testing.T) {
	// flow part: offset goes INSIDE the braces, comment after them survives.
	out, ok := UpsertInlineKey(editDoc, FindPartIDLine(editDoc, "a"), "offset", 40, 20, 0)
	if !ok {
		t.Fatal("not ok")
	}
	if !strings.Contains(out, "offset: { wx: 40, wy: 20 }") {
		t.Fatalf("offset not written inline:\n%s", firstPartLine(out, "a"))
	}
	if !strings.Contains(out, "# flow part") {
		t.Error("trailing comment lost")
	}
	if d := onlyChangedLines(editDoc, out); len(d) != 1 {
		t.Errorf("flow drag should touch one line, touched %v", d)
	}
}

func TestUpsertInlineKeyBlockForm(t *testing.T) {
	// block part b: offset is inserted as a child line at the right indent.
	out, ok := UpsertInlineKey(editDoc, FindPartIDLine(editDoc, "b"), "offset", 12, 0, 0)
	if !ok {
		t.Fatal("not ok")
	}
	if !strings.Contains(out, "        offset: { wx: 12, wy: 0 }") {
		t.Fatalf("offset child line wrong:\n%s", out)
	}
	if !strings.Contains(out, "# block part") {
		t.Error("block comment lost")
	}
}

func TestUpsertInlineKeyReplacesNotDuplicates(t *testing.T) {
	// dragging twice must leave exactly one offset (re-drag is idempotent in count).
	out, _ := UpsertInlineKey(editDoc, FindPartIDLine(editDoc, "a"), "offset", 40, 20, 0)
	out, _ = UpsertInlineKey(out, FindPartIDLine(out, "a"), "offset", 99, 88, 0)
	if n := strings.Count(out, "offset:"); n != 1 {
		t.Errorf("expected 1 offset after re-drag, got %d:\n%s", n, firstPartLine(out, "a"))
	}
	if !strings.Contains(out, "wx: 99, wy: 88") {
		t.Error("offset not updated to latest drag")
	}
}

func TestUpsertInlineKeyWritesWZOnlyWhenNonzero(t *testing.T) {
	flat, _ := UpsertInlineKey(editDoc, FindPartIDLine(editDoc, "b"), "offset", 5, 5, 0)
	if strings.Contains(flat, "wz:") {
		t.Error("2D drag must not write wz (keeps scenes byte-stable)")
	}
	lifted, _ := UpsertInlineKey(editDoc, FindPartIDLine(editDoc, "b"), "offset", 5, 5, 15)
	if !strings.Contains(lifted, "wz: 15") {
		t.Error("nonzero wz must be written")
	}
}

// ── Edge drag: UpsertInlineList writes waypoints, drops bend ───────────────

func TestUpsertInlineListWaypoints(t *testing.T) {
	pts := [][2]float64{{10, 20}, {30, 40}}
	out, ok := UpsertInlineList(editDoc, FindConnectorLine(editDoc, 0), "waypoints", pts)
	if !ok {
		t.Fatal("not ok")
	}
	if !strings.Contains(out, "waypoints: [{ wx: 10, wy: 20 }, { wx: 30, wy: 40 }]") {
		t.Fatalf("waypoints not written:\n%s", out)
	}
	// empty list reverts to the auto route (removes the key).
	rev, _ := UpsertInlineList(out, FindConnectorLine(out, 0), "waypoints", nil)
	if strings.Contains(rev, "waypoints:") {
		t.Error("empty list should remove waypoints")
	}
}

// ── Freeze: strip root layout/place, keep nested ──────────────────────────

const freezeDoc = `nodes:
  scene:
    shape: composite
    layout: { mode: row, gap: 1 }
    parts:
      - id: a
        shape: rectangle
        place: { rightOf: b, gap: 1 }
      - id: grp
        shape: group
        layout: { mode: column }
        parts:
          - { id: x, shape: rectangle, place: { rightOf: y } }
          - { id: y, shape: rectangle }
`

func TestFreezeLayoutText(t *testing.T) {
	out := FreezeLayoutText(freezeDoc)
	// root scene layout + root-part place are detached...
	if strings.Contains(out, "mode: row") {
		t.Error("root scene layout not stripped")
	}
	if strings.Contains(out, "rightOf: b") {
		t.Error("root part place not stripped")
	}
	// ...but nested-group layout and nested-child place survive untouched.
	if !strings.Contains(out, "mode: column") {
		t.Error("nested group layout was wrongly stripped")
	}
	if !strings.Contains(out, "rightOf: y") {
		t.Error("nested child place was wrongly stripped")
	}
}

// ── Structural ops: AddPart, DuplicatePart ────────────────────────────────

func TestAddPart(t *testing.T) {
	out, ok := AddPart(editDoc)
	if !ok {
		t.Fatal("not ok")
	}
	if !strings.Contains(out, "id: node") {
		t.Errorf("default node not appended:\n%s", out)
	}
	// original parts untouched
	for _, keep := range []string{"id: a", "id: b", "# flow part"} {
		if !strings.Contains(out, keep) {
			t.Errorf("AddPart clobbered %q", keep)
		}
	}
}

func TestDuplicatePart(t *testing.T) {
	out, ok := DuplicatePart(editDoc, "b", 40, 40)
	if !ok {
		t.Fatal("not ok")
	}
	if !strings.Contains(out, "id: b_copy") {
		t.Errorf("clone id not minted:\n%s", out)
	}
	if !strings.Contains(out, "offset: { wx: 40, wy: 40 }") {
		t.Error("clone offset not written")
	}
	// the original b survives and the clone is a distinct second id.
	if n := strings.Count(out, "shape: cylinder"); n != 2 {
		t.Errorf("expected original + clone (2 cylinders), got %d", n)
	}
}

// DeletePart's full cleanup: an annotation anchored to the removed node is
// dropped (findListItemLine), and a sibling that places RELATIVE to it loses
// its place: so it falls back to layout instead of dangling (removeKeyInRange
// via stripPlaceReferencing).
func TestDeleteCleansAnnotationAndPlaceRefs(t *testing.T) {
	src := `nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, label: "A" }
      - id: b
        shape: cylinder
        place: { rightOf: a, gap: 1 }
    connectors:
      - { from: a, to: b }
annotations:
  - { anchor: a, text: "note on a" }
  - { anchor: b, text: "note on b" }
`
	out, ok := DeletePart(src, "a")
	if !ok {
		t.Fatal("DeletePart(a) not ok")
	}
	if strings.Contains(out, "id: a") {
		t.Error("a not removed")
	}
	if strings.Contains(out, "anchor: a") {
		t.Error("annotation anchored to a should be dropped")
	}
	if strings.Contains(out, "rightOf: a") {
		t.Error("sibling b's place referencing a should be stripped")
	}
	// b itself and the annotation on b survive.
	if !strings.Contains(out, "id: b") || !strings.Contains(out, "anchor: b") {
		t.Errorf("delete over-reached:\n%s", out)
	}
}

// ── Traversal helpers used by the schema bridge ───────────────────────────

func TestReadPathAndFindNodeMap(t *testing.T) {
	root := mustParse(editDoc)
	b := FindNodeMap(root, "b")
	if b == nil {
		t.Fatal("FindNodeMap(b) nil")
	}
	if got := ReadPath(b, "label"); got != "B" {
		t.Errorf("ReadPath label = %q, want B", got)
	}
	if got := ReadPath(b, "geom.w"); got != "84" {
		t.Errorf("ReadPath geom.w = %q, want 84", got)
	}
	if got := ReadPath(b, "nope.missing"); got != "" {
		t.Errorf("absent path should be empty, got %q", got)
	}
	if c := FindConnectors(root); len(c) != 1 {
		t.Errorf("FindConnectors len = %d, want 1", len(c))
	}
}

func TestFindCanvasLine(t *testing.T) {
	if FindCanvasLine(editDoc) != 0 {
		t.Errorf("canvas is line 0 in editDoc, got %d", FindCanvasLine(editDoc))
	}
	if FindCanvasLine("nodes:\n  scene: {}\n") != -1 {
		t.Error("absent canvas should be -1")
	}
}

// ── AddConnector ─────────────────────────────────────────────────────────────

func TestAddConnector_NoAnchor(t *testing.T) {
	out, ok := AddConnector(editDoc, "a", "b", "", "")
	if !ok {
		t.Fatal("AddConnector returned !ok")
	}
	if !strings.Contains(out, "from: a, to: b") {
		t.Errorf("connector not written without anchors:\n%s", out)
	}
	// Original connector and all parts must survive.
	for _, keep := range []string{"id: a", "id: b", "from: a, to: b, arrow: triangle"} {
		if !strings.Contains(out, keep) {
			t.Errorf("AddConnector clobbered %q", keep)
		}
	}
}

func TestAddConnector_WithBothAnchors(t *testing.T) {
	out, ok := AddConnector(editDoc, "a", "b", "right", "left")
	if !ok {
		t.Fatal("AddConnector returned !ok")
	}
	if !strings.Contains(out, "from: a.right, to: b.left") {
		t.Errorf("anchor suffixes not written:\n%s", out)
	}
}

func TestAddConnector_FromAnchorOnly(t *testing.T) {
	out, ok := AddConnector(editDoc, "a", "b", "top", "")
	if !ok {
		t.Fatal("AddConnector returned !ok")
	}
	if !strings.Contains(out, "from: a.top, to: b") {
		t.Errorf("only from-anchor should appear:\n%s", out)
	}
}

func TestAddConnector_ToAnchorOnly(t *testing.T) {
	out, ok := AddConnector(editDoc, "a", "b", "", "bottom")
	if !ok {
		t.Fatal("AddConnector returned !ok")
	}
	if !strings.Contains(out, "from: a, to: b.bottom") {
		t.Errorf("only to-anchor should appear:\n%s", out)
	}
}

func TestAddConnector_NoExistingConnectors(t *testing.T) {
	// Document with no connectors: block yet.
	src := `nodes:
  scene:
    shape: composite
    parts:
      - { id: x, shape: rectangle }
      - { id: y, shape: rectangle }
`
	out, ok := AddConnector(src, "x", "y", "right", "left")
	if !ok {
		t.Fatal("AddConnector returned !ok on doc without connectors block")
	}
	if !strings.Contains(out, "connectors:") {
		t.Error("connectors: header not inserted")
	}
	if !strings.Contains(out, "from: x.right, to: y.left") {
		t.Errorf("connector with anchors not written:\n%s", out)
	}
}

func TestAddConnector_PreservesComments(t *testing.T) {
	out, ok := AddConnector(editDoc, "a", "b", "right", "left")
	if !ok {
		t.Fatal("not ok")
	}
	for _, comment := range []string{"# canvas comment", "# keep me", "# flow part", "# block part"} {
		if !strings.Contains(out, comment) {
			t.Errorf("comment %q lost after AddConnector", comment)
		}
	}
}

func TestScalarHelpers(t *testing.T) {
	if got := unquoteYAML(`"hi there"`); got != "hi there" {
		t.Errorf("unquoteYAML = %q", got)
	}
	if got := stringifyYAML(84); got != "84" {
		t.Errorf("stringifyYAML(int) = %q", got)
	}
	if got := stringifyYAML(true); got != "true" {
		t.Errorf("stringifyYAML(bool) = %q", got)
	}
	if got := stringifyYAML(nil); got != "" {
		t.Errorf("stringifyYAML(nil) = %q", got)
	}
}

func TestConnectorLineSkipsNestedWaypointLists(t *testing.T) {
	for _, pad := range []string{"  ", ""} {
		src := "connectors: # routes\n" + pad + "- from: first\n" + pad + "  to: target\n" + pad + "  waypoints3d:\n" + pad + "  - {wx: 1, wy: 2, wz: 30}\n" + pad + "  - {wx: 40, wy: 2, wz: 30}\n# outside-indented comment\n" + pad + "- from: second\n" + pad + "  to: target\nnext: value\n"
		if a, b, c := FindConnectorLine(src, 0), FindConnectorLine(src, 1), FindConnectorLine(src, 2); a != 1 || b != 7 || c != -1 {
			t.Fatalf("connector lines %d %d %d", a, b, c)
		}
		out, ok := SetField(src, FindConnectorLine(src, 1), []string{"label"}, "second only")
		if !ok {
			t.Fatal("edit failed")
		}
		if !strings.Contains(out, pad+"- from: first\n"+pad+"  to: target\n"+pad+"  waypoints3d:\n"+pad+"  - {wx: 1, wy: 2, wz: 30}") {
			t.Fatal("editing second connector changed first waypoint list", out)
		}
	}
}
