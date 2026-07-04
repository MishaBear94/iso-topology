package isotopo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPersonEffectsSampleIsRepairFixpoint — the golden for
// samples/node/person-effects is rendered WITHOUT the repair loop
// (golden_test.go mirrors only the scene resolution), while the documented
// CLI (`isotopo render`) runs RepairScene by default. The golden is therefore
// only CLI-reproducible when the sample is a strict repair fixpoint: no pass
// (contrast retint included) may touch it. This pins the exact regression
// where repairContrast judged the operator label against the body-gradient's
// darkest stop and silently rewrote the author's #12233D to #0B0B0B, making
// this the one sample in the tree whose CLI output differed from its golden.
func TestPersonEffectsSampleIsRepairFixpoint(t *testing.T) {
	dir := filepath.Join("samples", "node", "person-effects")
	data, err := os.ReadFile(filepath.Join(dir, "input.yaml"))
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}

	doc, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	before := renderTopology(doc)

	repaired, iters := RepairScene(doc)
	after := renderTopology(repaired)

	if before != after {
		t.Fatalf("RepairScene is not a no-op on person-effects (%d iteration(s)): CLI `isotopo render` output would drift from the golden", iters)
	}

	// And the golden itself must be exactly that fixpoint.
	want, err := os.ReadFile(filepath.Join(dir, "expected.svg"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if after != string(want) {
		t.Fatalf("repaired render (%d bytes) differs from golden (%d bytes)", len(after), len(want))
	}

	// The validator must agree with repair: a sample that repair leaves alone
	// must also validate clean, or the pipeline contradicts itself.
	if issues := Validate(doc); len(issues) != 0 {
		for _, is := range issues {
			t.Errorf("unexpected validator issue: [%s] %s: %s", is.Severity, is.Path, is.Message)
		}
	}
}
