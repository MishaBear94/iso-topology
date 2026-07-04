package isotopo

import (
	"strings"
	"testing"
)

// Fixtures + tests for the batch direct-manipulation ops behind Studio's
// multi-select: move-multi (group move, one atomic text surgery) and
// delete-multi (batch delete). The invariants under test:
//
//	M1 every listed node gets the SAME delta → relative positions preserved
//	M2 unlisted nodes and comments/formatting are untouched
//	M3 atomic rollback: any failing id returns the ORIGINAL source + error
//	M4 zero delta is idempotent (no drift, byte-stable on re-apply)
//	M5 duplicate ids in the list apply the delta ONCE
//	M6 move-multi ≡ the same single moves chained (offset-wise)

const multiFixture = `# multi-select fixture — keep this comment
nodes:
  scene:
    shape: composite
    parts:
      - id: alpha
        shape: rectangle   # alpha shape comment (must survive a batch move)
        geom: { w: 100, d: 80, h: 24 }
        offset: { wx: 0, wy: 0 }
      - id: beta
        shape: rectangle
        geom: { w: 100, d: 80, h: 24 }
        offset: { wx: 200, wy: 0 }
      - id: gamma
        shape: rectangle
        geom: { w: 100, d: 80, h: 24 }
        offset: { wx: 100, wy: 220 }
    connectors:
      - { from: alpha, to: beta }
`

const multiGroupFixture = `# group fixture
nodes:
  scene:
    shape: composite
    parts:
      - id: box
        shape: group
        geom: { w: 300, d: 200, h: 6 }
        offset: { wx: 40, wy: 40 }
        parts:
          - id: inner
            shape: rectangle
            geom: { w: 80, d: 60, h: 20 }
            offset: { wx: 30, wy: 30 }
      - id: solo
        shape: rectangle
        geom: { w: 80, d: 60, h: 20 }
        offset: { wx: 420, wy: 40 }
`

// offsetsOf parses src and returns the RESOLVED world offset of each id via
// the renderer's own resolver, so the assertion can't diverge from the pixels.
func offsetsOf(t *testing.T, src string, ids ...string) map[string][3]float64 {
	t.Helper()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	lowered := resolveLowered(doc)
	out := map[string][3]float64{}
	for _, id := range ids {
		p := lowered[id]
		if p == nil {
			t.Fatalf("part %q not in lowered scene", id)
		}
		x, y, z := worldXYZ(p)
		out[id] = [3]float64{x, y, z}
	}
	return out
}

func TestMoveMulti_SameDeltaAllIDs(t *testing.T) {
	before := offsetsOf(t, multiFixture, "alpha", "beta", "gamma")
	out, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "move-multi", IDs: []string{"alpha", "beta"}, DWX: 30, DWY: -40})
	if err != nil {
		t.Fatalf("move-multi: %v", err)
	}
	after := offsetsOf(t, string(out), "alpha", "beta", "gamma")
	for _, id := range []string{"alpha", "beta"} {
		if dx, dy := after[id][0]-before[id][0], after[id][1]-before[id][1]; dx != 30 || dy != -40 {
			t.Fatalf("%s moved by (%v,%v), want (30,-40)\n%s", id, dx, dy, out)
		}
	}
	// M1: relative position between the two moved nodes is preserved exactly
	relBx := before["beta"][0] - before["alpha"][0]
	relAx := after["beta"][0] - after["alpha"][0]
	if relBx != relAx {
		t.Fatalf("relative x drifted: %v → %v", relBx, relAx)
	}
	// M2: the unselected node did not move, comments survive
	if after["gamma"] != before["gamma"] {
		t.Fatalf("gamma moved: %v → %v", before["gamma"], after["gamma"])
	}
	// UpsertInlineKey rewrites only the offset LINES; every other comment must
	// survive (same contract as the single move — a trailing comment on the
	// offset line itself is dropped by that rewrite in both paths).
	for _, c := range []string{"# multi-select fixture — keep this comment", "# alpha shape comment"} {
		if !strings.Contains(string(out), c) {
			t.Fatalf("comment %q lost:\n%s", c, out)
		}
	}
}

func TestMoveMulti_IncludesGroupChild(t *testing.T) {
	before := offsetsOf(t, multiGroupFixture, "inner", "solo", "box")
	out, err := ApplyOpText("yaml", []byte(multiGroupFixture),
		EditOp{Kind: "move-multi", IDs: []string{"inner", "solo"}, DWX: 25, DWY: 15})
	if err != nil {
		t.Fatalf("move-multi: %v", err)
	}
	after := offsetsOf(t, string(out), "inner", "solo", "box")
	for _, id := range []string{"inner", "solo"} {
		if dx, dy := after[id][0]-before[id][0], after[id][1]-before[id][1]; dx != 25 || dy != 15 {
			t.Fatalf("%s moved by (%v,%v), want (25,15)\n%s", id, dx, dy, out)
		}
	}
	if after["box"] != before["box"] {
		t.Fatalf("container box moved: %v → %v", before["box"], after["box"])
	}
}

func TestMoveMulti_AtomicRollback(t *testing.T) {
	// The second id doesn't exist: NOTHING may be applied (not even alpha's
	// nudge) and the returned text must be byte-identical to the input.
	out, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "move-multi", IDs: []string{"alpha", "ghost"}, DWX: 30, DWY: 30})
	if err == nil {
		t.Fatal("expected error for missing id in the batch")
	}
	if string(out) != multiFixture {
		t.Fatalf("failed batch mutated the source:\n%s", out)
	}
	// Empty id list is refused, not a silent no-op success.
	if _, err := ApplyOpText("yaml", []byte(multiFixture), EditOp{Kind: "move-multi"}); err == nil {
		t.Fatal("expected error for empty ids")
	}
}

func TestMoveMulti_ZeroDeltaIdempotent(t *testing.T) {
	op := EditOp{Kind: "move-multi", IDs: []string{"alpha", "beta", "gamma"}, DWX: 0, DWY: 0}
	once, err := ApplyOpText("yaml", []byte(multiFixture), op)
	if err != nil {
		t.Fatalf("move-multi(0,0): %v", err)
	}
	before := offsetsOf(t, multiFixture, "alpha", "beta", "gamma")
	after := offsetsOf(t, string(once), "alpha", "beta", "gamma")
	for id, b := range before {
		if after[id] != b {
			t.Fatalf("zero-delta drifted %s: %v → %v", id, b, after[id])
		}
	}
	twice, err := ApplyOpText("yaml", once, op)
	if err != nil {
		t.Fatalf("move-multi(0,0) reapply: %v", err)
	}
	if string(twice) != string(once) {
		t.Fatalf("zero-delta re-apply not byte-stable:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestMoveMulti_DuplicateIDsApplyOnce(t *testing.T) {
	before := offsetsOf(t, multiFixture, "alpha")
	out, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "move-multi", IDs: []string{"alpha", "alpha", "alpha"}, DWX: 10, DWY: 10})
	if err != nil {
		t.Fatalf("move-multi: %v", err)
	}
	after := offsetsOf(t, string(out), "alpha")
	if dx := after["alpha"][0] - before["alpha"][0]; dx != 10 {
		t.Fatalf("duplicate-listed id moved by %v in x, want exactly 10 (applied once)", dx)
	}
}

func TestMoveMulti_MatchesChainedSingleMoves(t *testing.T) {
	multi, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "move-multi", IDs: []string{"alpha", "gamma"}, DWX: 17, DWY: 23})
	if err != nil {
		t.Fatalf("move-multi: %v", err)
	}
	step1, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "move", Target: "node", ID: "alpha", DWX: 17, DWY: 23})
	if err != nil {
		t.Fatalf("move alpha: %v", err)
	}
	step2, err := ApplyOpText("yaml", step1,
		EditOp{Kind: "move", Target: "node", ID: "gamma", DWX: 17, DWY: 23})
	if err != nil {
		t.Fatalf("move gamma: %v", err)
	}
	a := offsetsOf(t, string(multi), "alpha", "beta", "gamma")
	b := offsetsOf(t, string(step2), "alpha", "beta", "gamma")
	for id, av := range a {
		if b[id] != av {
			t.Fatalf("move-multi diverges from chained singles at %s: %v vs %v", id, av, b[id])
		}
	}
}

func TestDeleteMulti_DropsNodesAndConnectors(t *testing.T) {
	out, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "delete-multi", IDs: []string{"alpha", "beta"}})
	if err != nil {
		t.Fatalf("delete-multi: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "id: alpha") || strings.Contains(s, "id: beta") {
		t.Fatalf("nodes survived the batch delete:\n%s", s)
	}
	if strings.Contains(s, "from: alpha") {
		t.Fatalf("connector to a deleted node survived:\n%s", s)
	}
	if !strings.Contains(s, "id: gamma") {
		t.Fatalf("unselected node was deleted:\n%s", s)
	}
	if _, iss, _ := RenderSource("yaml", out); hasErr(iss) {
		t.Fatalf("result does not render clean: %+v", iss)
	}
}

func TestDeleteMulti_AtomicRollback(t *testing.T) {
	// gamma is fine but "ghost" is missing → the whole batch must roll back.
	out, err := ApplyOpText("yaml", []byte(multiFixture),
		EditOp{Kind: "delete-multi", IDs: []string{"gamma", "ghost"}})
	if err == nil {
		t.Fatal("expected error for missing id in the batch")
	}
	if string(out) != multiFixture {
		t.Fatalf("failed batch mutated the source:\n%s", out)
	}
	// A container in the list aborts the batch the same way (leaf-only rule).
	out, err = ApplyOpText("yaml", []byte(multiGroupFixture),
		EditOp{Kind: "delete-multi", IDs: []string{"solo", "box"}})
	if err == nil {
		t.Fatal("expected error deleting a container in the batch")
	}
	if string(out) != multiGroupFixture {
		t.Fatalf("failed batch mutated the source:\n%s", out)
	}
}
