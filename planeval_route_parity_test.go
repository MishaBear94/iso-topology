package isotopo

import (
	"encoding/json"
	"reflect"
	"testing"
)

// planReportFields compares two PlanReports field-by-field and returns the
// names of every differing field, so a parity failure names the exact metric.
func planReportDiff(a, b *PlanReport) []string {
	var diffs []string
	av, bv := reflect.ValueOf(*a), reflect.ValueOf(*b)
	tp := av.Type()
	for i := 0; i < tp.NumField(); i++ {
		if !reflect.DeepEqual(av.Field(i).Interface(), bv.Field(i).Interface()) {
			diffs = append(diffs, tp.Field(i).Name)
		}
	}
	return diffs
}

func reportJSON(t *testing.T, rep any) string {
	t.Helper()
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	return string(b)
}

// TestIsoEvalParity_CollectorMatchesRegex is the WS-E acceptance gate: for
// EVERY sample, scoring through the in-memory collector restricted to the
// legacy coverage (orthogonal routes only — exactly the connectors that
// serialize data-route) must reproduce the historical regex-parsed scorecard
// FIELD-FOR-FIELD. The quantRoute round-trip makes this bit-exact, not
// approximate: any drift here means the collector and the SVG disagree about
// the engine's real routes.
func TestIsoEvalParity_CollectorMatchesRegex(t *testing.T) {
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		scene := doc.Scene()
		if scene == nil {
			t.Skip("no scene")
		}
		regexRep := evaluateIsoWith(scene, doc.Theme, doc.Canvas, routeSourceRegex)
		orthoRep := evaluateIsoWith(scene, doc.Theme, doc.Canvas, routeSourceCollectorOrtho)
		if diffs := planReportDiff(regexRep.PlanReport, orthoRep.PlanReport); len(diffs) > 0 {
			t.Fatalf("collector (orthogonal-only) diverges from the regex reference on %v\n regex:     %s\n collector: %s",
				diffs, reportJSON(t, regexRep.PlanReport), reportJSON(t, orthoRep.PlanReport))
		}
	})
}

// TestIsoEvalParity_FullCoverage separates out the ONE intended behaviour
// change: the production evaluator now also scores straight/bezier routes
// (which emit no data-route, so the regex path never saw them).
//
//   - Samples whose drawn connectors are all orthogonal must score EXACTLY
//     like the regex reference — full coverage adds nothing there.
//   - Samples with straight/bezier connectors may differ ONLY in the
//     route-geometry metrics (crossings, tunnelling, bends, lengths and
//     their per-edge annotations); every route-independent metric — node
//     count, edge count, both overlap counts, flow axis, backward edges —
//     must still match, proving the delta comes from coverage alone.
func TestIsoEvalParity_FullCoverage(t *testing.T) {
	// Metrics derived from the edges' ROUTE GEOMETRY, which straight/bezier
	// coverage legitimately changes. Everything else must stay identical.
	routeDependent := map[string]bool{
		"Crossings": true, "CrossingsAt": true,
		"EdgesThroughNodes": true, "ProblemEdges": true,
		"TotalBends": true, "TotalEdgeLen": true, "MaxEdgeLen": true,
	}
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		scene := doc.Scene()
		if scene == nil {
			t.Skip("no scene")
		}
		regexRep := evaluateIsoWith(scene, doc.Theme, doc.Canvas, routeSourceRegex)
		fullRep := evaluateIsoWith(scene, doc.Theme, doc.Canvas, routeSourceCollector)

		// Does this sample draw any non-orthogonal connector? Compare the
		// collector's full coverage against its orthogonal-only subset.
		full := isoCollectedRoutes(cloneSceneForEval(scene), doc.Theme, doc.Canvas, false)
		ortho := isoCollectedRoutes(cloneSceneForEval(scene), doc.Theme, doc.Canvas, true)
		extraCoverage := len(full) - len(ortho)

		diffs := planReportDiff(regexRep.PlanReport, fullRep.PlanReport)
		if extraCoverage == 0 {
			if len(diffs) > 0 {
				t.Fatalf("all-orthogonal sample must score identically under full coverage; differs on %v\n regex: %s\n full:  %s",
					diffs, reportJSON(t, regexRep.PlanReport), reportJSON(t, fullRep.PlanReport))
			}
			return
		}
		for _, f := range diffs {
			if !routeDependent[f] {
				t.Fatalf("field %s must not change with straight/bezier coverage (%d extra routes)\n regex: %s\n full:  %s",
					f, extraCoverage, reportJSON(t, regexRep.PlanReport), reportJSON(t, fullRep.PlanReport))
			}
		}
		if len(diffs) > 0 {
			t.Logf("%s: %d straight/bezier route(s) widened coverage; route-metric deltas on %v\n regex: %s\n full:  %s",
				name, extraCoverage, diffs, reportJSON(t, regexRep.PlanReport), reportJSON(t, fullRep.PlanReport))
		}
	})
}

// TestIsoReport_LeafOverlapMatchesPlan pins the unified overlap definition:
// IsoReport.LeafNodeOverlaps must equal EvaluatePlan's NodeOverlaps (leaf
// pairs only) on every sample, while the embedded NodeOverlaps keeps the
// iso-specific top-level/slab-aware count untouched.
func TestIsoReport_LeafOverlapMatchesPlan(t *testing.T) {
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		scene := doc.Scene()
		if scene == nil {
			t.Skip("no scene")
		}
		isoRep := EvaluateIsoReport(scene, doc.Theme, doc.Canvas)
		planRep := EvaluatePlan(cloneSceneForEval(scene), doc.Theme, doc.Canvas)
		if isoRep.LeafNodeOverlaps != planRep.NodeOverlaps {
			t.Fatalf("LeafNodeOverlaps (%d) must match EvaluatePlan.NodeOverlaps (%d)",
				isoRep.LeafNodeOverlaps, planRep.NodeOverlaps)
		}
	})
}

// TestIsoReport_JSONShape locks the report wiring: the new leaf-scoped field
// serializes alongside the untouched legacy fields, and EvaluateIso (the old
// API) still returns the same embedded PlanReport.
func TestIsoReport_JSONShape(t *testing.T) {
	doc, err := Parse([]byte(routeCollectFixture))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	scene := doc.Scene()
	full := EvaluateIsoReport(scene, doc.Theme, doc.Canvas)
	legacy := EvaluateIso(scene, doc.Theme, doc.Canvas)
	if !reflect.DeepEqual(full.PlanReport, legacy) {
		t.Fatalf("EvaluateIso must return EvaluateIsoReport's embedded PlanReport\n full:   %s\n legacy: %s",
			reportJSON(t, full.PlanReport), reportJSON(t, legacy))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(reportJSON(t, full)), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"leaf_node_overlaps", "node_overlaps", "nodes", "edges"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("IsoReport JSON missing %q: %v", key, m)
		}
	}
}
