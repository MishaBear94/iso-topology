package isotopo

// repair_bodyocc.go (WS-H) — the conservative repair executor for body-vs-body
// occlusion (occlusion.go BodyOcclusions): a node whose projected silhouette a
// nearer body substantially covers is pushed along the projected separation
// direction until the pair reads apart.
//
// Each accepted candidate must reduce covered area without increasing cost
// or any supported per-object/per-edge defect. Uncertain filled coverage
// disables this automatic pass; a weighted score cannot prove such a move safe.

const (
	// bodyOccStep is one push in world units — one layout cell, the same
	// stride repairNeighbourLabels uses. RepairScene's outer loop re-measures
	// and pushes again until the fixpoint, so a deep burial clears over a few
	// iterations instead of one violent shove.
	bodyOccStep = 40.0
	// bodyOccMeasureFloor is the fraction floor for the PROGRESS measure: the
	// acceptance sum includes sub-threshold residues (so 0.45 → 0.30 counts as
	// progress) but ignores numeric dust from hulls that merely graze.
	bodyOccMeasureFloor = 0.01
)

// repairBodyOcclusion runs one measured push round against the scene's
// body-occlusion pairs. Returns whether it kept a change.
func repairBodyOcclusion(doc *Document, scene *Node) bool {
	if doc == nil || scene == nil {
		return false
	}
	if doc.Canvas != nil && doc.Canvas.Projection == "top" {
		return false // iso-only defect: a top projection never buries a body
	}
	occs := bodyOcclusionsInNode(scene, doc.Theme, doc.Canvas, bodyOcclusionThreshold)
	if len(occs) == 0 {
		return false
	}
	clone := resolvedLayout(scene, doc.Canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0, clone.GridStep)
	if occPaintRegions(flat, doc.Theme).uncertain {
		return false
	}
	defectsBefore := sceneVisualDefects(scene, doc.Theme, doc.Canvas)
	costBefore := Readability(doc).Cost
	fracBefore := bodyOccTotalFrac(scene, doc.Theme, doc.Canvas)
	saved := snapshotOffsetsDeep(scene.Parts)

	pushed := map[*CompositePart]bool{}
	moved := false
	for _, oc := range occs {
		// The actuator is the direct child of the deepest scope that separates
		// hidden from occluder — nudging it moves the hidden node without
		// dragging its occluder along.
		act := bodyOccActuator(scene.Parts, stackBase(oc.Hidden), stackBase(oc.Occluder))
		if act == nil || pushed[act] {
			continue
		}
		pushed[act] = true
		dx, dy := bodyOccPushDir(oc.sep)
		// Escalating line search: a FULLY-swallowed small node sits on a
		// plateau (one cell of travel can leave it still 100% covered, so a
		// single fixed nudge measures as "no progress" and is thrown away).
		// Try doubling push distances from the part's original offset and
		// keep the SMALLEST one that strictly lowers the total covered
		// fraction; if none does, the part goes back exactly where it was.
		var base *WorldPoint
		if act.Offset != nil {
			o := *act.Offset
			base = &o
		}
		cur := bodyOccTotalFrac(scene, doc.Theme, doc.Canvas)
		improved := false
		for _, k := range [...]float64{1, 2, 4, 8, 16} {
			act.Offset = nil
			if base != nil {
				o := *base
				act.Offset = &o
			}
			nudge(act, dx*bodyOccStep*k, dy*bodyOccStep*k)
			if bodyOccTotalFrac(scene, doc.Theme, doc.Canvas) < cur-1e-9 &&
				Readability(doc).Cost <= costBefore+1e-9 &&
				sceneVisualDefects(scene, doc.Theme, doc.Canvas).noWorseThan(defectsBefore) {
				improved = true
				break
			}
		}
		if !improved {
			act.Offset = base // no distance helped — restore this part exactly
			continue
		}
		moved = true
	}
	if !moved {
		restoreOffsetsDeep(scene.Parts, saved)
		return false
	}

	fracAfter := bodyOccTotalFrac(scene, doc.Theme, doc.Canvas)
	if fracAfter >= fracBefore-1e-9 || Readability(doc).Cost > costBefore+1e-9 ||
		!sceneVisualDefects(scene, doc.Theme, doc.Canvas).noWorseThan(defectsBefore) {
		restoreOffsetsDeep(scene.Parts, saved)
		return false
	}
	return true
}

// bodyOccTotalFrac is the progress measure: the sum of covered-silhouette
// fractions over every pair above the measurement floor. Measuring each
// candidate reveals sub-threshold progress that a bare defect COUNT misses;
// the separate defect gate prevents trading it for a different visible fault.
func bodyOccTotalFrac(scene *Node, theme *Theme, canvas *Canvas) float64 {
	total := 0.0
	for _, oc := range bodyOcclusionsInNode(scene, theme, canvas, bodyOccMeasureFloor) {
		total += oc.Frac
	}
	return total
}

// bodyOccPushDir maps the screen-space separation vector (bounds centre of
// hidden minus bounds centre of occluder) onto the world axis direction whose projection best
// aligns with it — the world push that most directly pulls the buried node out
// from behind its occluder. Ties resolve to the first candidate (+x), so the
// result is deterministic even for a zero separation.
func bodyOccPushDir(sep [2]float64) (float64, float64) {
	ox, oy := projectIso(0, 0, 0)
	xX, xY := projectIso(1, 0, 0)
	yX, yY := projectIso(0, 1, 0)
	xX, xY = xX-ox, xY-oy
	yX, yY = yX-ox, yY-oy
	cands := [4]struct{ dx, dy, sx, sy float64 }{
		{1, 0, xX, xY}, {-1, 0, -xX, -xY}, {0, 1, yX, yY}, {0, -1, -yX, -yY},
	}
	bi, bd := 0, cands[0].sx*sep[0]+cands[0].sy*sep[1]
	for i := 1; i < len(cands); i++ {
		if d := cands[i].sx*sep[0] + cands[i].sy*sep[1]; d > bd {
			bi, bd = i, d
		}
	}
	return cands[bi].dx, cands[bi].dy
}

// bodyOccActuator returns the part to nudge for a (hidden, occluder) pair: the
// direct child, at the deepest scope containing both, that holds hidden but
// not occluder. Its offset is expressed in that scope's frame, so a nudge
// moves the hidden node relative to its occluder exactly.
func bodyOccActuator(parts []*CompositePart, hidden, occluder string) *CompositePart {
	for _, p := range parts {
		if p == nil || !partTreeHasID(p, hidden) {
			continue
		}
		if partTreeHasID(p, occluder) {
			return bodyOccActuator(p.Parts, hidden, occluder)
		}
		return p
	}
	return nil
}

// partTreeHasID reports whether the part or any descendant carries the id.
func partTreeHasID(p *CompositePart, id string) bool {
	if p == nil {
		return false
	}
	if p.ID == id {
		return true
	}
	for _, c := range p.Parts {
		if partTreeHasID(c, id) {
			return true
		}
	}
	return false
}
