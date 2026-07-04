package isotopo

import (
	"math/rand"
	"testing"
)

// genMultiOp produces a random BATCH op (move-multi / delete-multi) against
// the current document: 1–4 ids drawn from the live parts, occasionally a
// ghost id (to exercise the atomic-rollback path) or a container (which
// delete-multi must refuse). Deltas span the same range as genOp's move.
func genMultiOp(rng *rand.Rand, src string) EditOp {
	ids := allPartIDs(src)
	var batch []string
	n := 1 + rng.Intn(4)
	for i := 0; i < n; i++ {
		switch {
		case len(ids) == 0 || rng.Intn(8) == 0:
			batch = append(batch, "ghost")
		default:
			batch = append(batch, ids[rng.Intn(len(ids))])
		}
	}
	if rng.Intn(2) == 0 {
		return EditOp{Kind: "move-multi", IDs: batch,
			DWX: float64(rng.Intn(200) - 100), DWY: float64(rng.Intn(200) - 100)}
	}
	return EditOp{Kind: "delete-multi", IDs: batch}
}

// FuzzEditSessionMulti drives random op-chains that MIX the batch ops into the
// existing single-op vocabulary, asserting the same floor invariants as
// FuzzEditSession — in particular F3 (an error must leave the source
// unchanged), which is exactly the batch ops' atomic-rollback contract.
//
//	go test -run x -fuzz FuzzEditSessionMulti -fuzztime 60s
func FuzzEditSessionMulti(f *testing.F) {
	f.Add(int64(3), 12)
	f.Add(int64(11), 25)
	f.Add(int64(99), 40)
	f.Fuzz(func(t *testing.T, seed int64, steps int) {
		if steps < 1 || steps > 80 {
			t.Skip()
		}
		rng := rand.New(rand.NewSource(seed))
		src := genScene(rng)
		if _, iss, _ := RenderSource("yaml", []byte(src)); hasErr(iss) {
			t.Skip() // non-clean base — not under test
		}
		for i := 0; i < steps; i++ {
			var op EditOp
			if rng.Intn(3) == 0 {
				op = genMultiOp(rng, src)
			} else {
				op = genOp(rng, src)
			}
			out, err := ApplyOpText("yaml", []byte(src), op)
			if v := checkFloorInvariants(src, out, err); v != "" {
				t.Fatalf("seed=%d step=%d op=%+v: %s\nsrc=\n%s\nout=\n%s", seed, i, op, v, src, out)
			}
			if err == nil {
				src = string(out)
			}
		}
	})
}

// TestEditSessionMulti_Deterministic runs the fuzz body over fixed seeds so the
// batch-op invariants are exercised on every plain `go test` run (the fuzz
// engine only runs its corpus when invoked with -fuzz).
func TestEditSessionMulti_Deterministic(t *testing.T) {
	for _, seed := range []int64{3, 11, 99, 2024} {
		rng := rand.New(rand.NewSource(seed))
		src := genScene(rng)
		if _, iss, _ := RenderSource("yaml", []byte(src)); hasErr(iss) {
			continue
		}
		for i := 0; i < 30; i++ {
			var op EditOp
			if rng.Intn(3) == 0 {
				op = genMultiOp(rng, src)
			} else {
				op = genOp(rng, src)
			}
			out, err := ApplyOpText("yaml", []byte(src), op)
			if v := checkFloorInvariants(src, out, err); v != "" {
				t.Fatalf("seed=%d step=%d op=%+v: %s", seed, i, op, v)
			}
			if err == nil {
				src = string(out)
			}
		}
	}
}
