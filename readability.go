package isotopo

// Readability is the single scene-quality objective of the layout engine
// (docs/design/layout-engine-master-plan.md, Phase 0). It is the contract the
// whole pipeline optimizes toward and the metric every change is gated on, so
// `evaluate` (what it reports) and the renderer (what it optimizes) can no
// longer diverge.
//
// It is computed on the ISO projection, decomposed by affine invariance into:
//   - height-induced, iso-only signals: occlusion (the dominant term);
//   - affine-invariant world-plane signals: crossings, tunnelling, overlaps,
//     bends, length — reused verbatim from the EvaluateIso scorecard, since the
//     iso ground projection preserves them.
//
// Phase 0 wires the existing detectors into one score; later phases complete
// occlusion (bodies/icons) and add aspect/alignment/balance terms.

// ReadabilityReport is the per-scene breakdown plus the scalar score R∈[0,1].
type ReadabilityReport struct {
	Occlusions int `json:"occlusions"` // iso-screen label/caption occlusions
	// BodyOcclusions (WS-H) counts nodes whose projected silhouette is
	// substantially painted over by a nearer body (BodyOcclusions in
	// occlusion.go) — the master plan's last Phase-1 open occlusion class.
	BodyOcclusions int     `json:"body_occlusions"`
	Crossings      int     `json:"crossings"`
	Tunnels        int     `json:"tunnels"`  // edges routed through an unrelated node
	Overlaps       int     `json:"overlaps"` // node footprints that collide
	Bends          int     `json:"bends"`
	Length         float64 `json:"length"`
	Cost           float64 `json:"cost"`  // weighted sum (0 = ideal)
	Score          float64 `json:"score"` // R = 1/(1+cost) ∈ (0,1], 1 = ideal
}

// Readability weights — occlusion dominates (it destroys readability outright),
// then tunnelling/overlap (a node or edge in the wrong place), then crossings,
// then route complexity, with length as a negligible tiebreak. Calibrated
// further against the benchmark corpus in later Phase-0 iterations.
const (
	wOcclusion = 5.0
	// A mostly-hidden node body is as destructive as a hidden caption — the
	// reader cannot see the node at all — and it is the same height-induced,
	// iso-only defect family, so it carries the same weight as wOcclusion.
	// Threshold 0.4 (occlusion.go) keeps the term at zero on the whole
	// shipped corpus, so existing R values only move on genuinely-buried scenes.
	wBodyOcclusion = 5.0
	wOverlap       = 4.0
	wTunnel        = 3.0
	wCrossing      = 1.0
	// Orthogonal edges inherently bend (1–2 per edge) — that is normal and
	// readable, so bends are only a faint tiebreak, never enough to rank a clean
	// flow below a broken scene.
	wBend = 0.02
	// Length scales with scene SIZE, not quality, so it must stay a negligible
	// tiebreak — never enough to rank a larger clean scene below a broken one.
	wLength = 0.0001
)

// Readability scores a parsed document. It is safe on a raw (unsolved) doc: the
// sub-calls each solve layout on a clone, matching what the renderer paints.
func Readability(doc *Document) ReadabilityReport {
	var r ReadabilityReport
	if doc == nil {
		r.Score = 1
		return r
	}
	if scene := doc.Scene(); scene != nil {
		if ev := EvaluateIsoReport(scene, doc.Theme, doc.Canvas); ev != nil {
			r.Crossings = ev.Crossings
			r.Tunnels = ev.EdgesThroughNodes
			r.Overlaps = ev.NodeOverlaps
			r.Bends = ev.TotalBends
			r.Length = ev.TotalEdgeLen
			r.BodyOcclusions = ev.BodyOcclusions
		}
	}
	r.Occlusions = len(LabelOcclusionIssues(doc))

	r.Cost = wOcclusion*float64(r.Occlusions) +
		wBodyOcclusion*float64(r.BodyOcclusions) +
		wOverlap*float64(r.Overlaps) +
		wTunnel*float64(r.Tunnels) +
		wCrossing*float64(r.Crossings) +
		wBend*float64(r.Bends) +
		wLength*r.Length
	r.Score = 1.0 / (1.0 + r.Cost)
	return r
}
