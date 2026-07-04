package isotopo

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// RepairAndReport runs the projection-repair loop and returns the iteration
// count plus a human/agent-readable list of what it cleared (occlusions that
// vanished, overlap pairs that were separated). Side-effect-free measurement:
// the defect snapshots are taken via the clone-based detectors. This is the L1
// "what I fixed" report of the agent-loop harness plan.
func RepairAndReport(doc *Document) (iters int, fixed []string) {
	if doc == nil {
		return 0, nil
	}
	beforeOcc := occlusionMessages(doc)
	beforeRead := Readability(doc)
	beforeContrast := contrastIssueCount(doc)
	_, iters = RepairScene(doc)
	afterOcc := occlusionMessages(doc)
	afterRead := Readability(doc)
	afterContrast := contrastIssueCount(doc)
	for msg := range beforeOcc {
		if !afterOcc[msg] {
			fixed = append(fixed, "cleared occlusion: "+msg)
		}
	}
	sort.Strings(fixed)
	if d := beforeRead.Overlaps - afterRead.Overlaps; d > 0 {
		fixed = append(fixed, fmt.Sprintf("separated %d overlapping node pair(s)", d))
	}
	if d := beforeContrast - afterContrast; d > 0 {
		fixed = append(fixed, fmt.Sprintf("raised label contrast on %d node(s)", d))
	}
	if d := beforeRead.BodyOcclusions - afterRead.BodyOcclusions; d > 0 {
		fixed = append(fixed, fmt.Sprintf("uncovered %d node(s) hidden behind a nearer body", d))
	}
	if d := beforeRead.Tunnels - afterRead.Tunnels; d > 0 {
		fixed = append(fixed, fmt.Sprintf("rerouted: cleared %d edge(s) tunnelling through nodes", d))
	}
	if d := beforeRead.Crossings - afterRead.Crossings; d > 0 {
		fixed = append(fixed, fmt.Sprintf("rerouted: removed %d edge crossing(s)", d))
	}
	return iters, fixed
}

// contrastIssueCount counts labels flagged below the text/fill contrast
// threshold — the before/after metric for the contrast-repair pass.
func contrastIssueCount(doc *Document) int {
	n := 0
	for _, is := range VisualContrastIssues(doc) {
		if strings.Contains(is.Message, "low contrast between top fill") {
			n++
		}
	}
	return n
}

// occlusionMessages snapshots the current label/caption occlusions as a set,
// trimmed to the defect phrase (before the em-dash rationale) so before/after
// comparison is stable.
func occlusionMessages(doc *Document) map[string]bool {
	out := map[string]bool{}
	for _, is := range LabelOcclusionIssues(doc) {
		msg := is.Message
		if i := strings.Index(msg, " — "); i >= 0 {
			msg = msg[:i]
		}
		out[msg] = true
	}
	return out
}

var captionOcclRe = regexp.MustCompile(`group label "([^"]+)" is covered`)
var neighbourOcclRe = regexp.MustCompile(`label "([^"]+)" is covered by node.*adjacent`)

// RepairScene is the Phase-1 projection-repair loop (docs/design/layout-engine-
// master-plan.md): it measures the rendered scene and locally repairs the
// defects that only exist in the projected output, re-checking each round until
// the scene is clean or a budget is hit — "optimize in the space you render".
// Repaired defect classes:
//   - caption clearance: a group label ridden by its front-most child → widen
//     the group's front padding;
//   - node overlap: two sibling footprints collide — at the top level AND
//     (WS-G) inside each group — → push them apart;
//   - routing (WS-G): an orthogonal connector crossing another edge or
//     tunnelling a node → escalate it to routing:"orthogonal2", kept only on
//     strict measured improvement (repair_routing.go).
//
// It is a strict no-op on an already-clean scene (0 iterations, byte-identical
// output), so it is safe to run unconditionally. Returns the doc and iteration
// count.
func RepairScene(doc *Document) (*Document, int) {
	if doc == nil {
		return doc, 0
	}
	// Headroom: separating one genuine overlap can cascade into neighbours, so a
	// dense scene may need several passes to reach the no-change fixpoint. The
	// loop breaks early the instant nothing changes, so this only costs
	// iterations on scenes that genuinely need them.
	const maxIters = 16
	iters := 0
	for ; iters < maxIters; iters++ {
		changed := repairCaptions(doc)
		if repairNeighbourLabels(doc) {
			changed = true
		}
		if repairContrast(doc) {
			changed = true
		}
		if scene := doc.Scene(); scene != nil {
			if repairOverlaps(doc, scene) {
				changed = true
			}
			// WS-H: uncover bodies buried behind a nearer node — measured,
			// kept only on strict improvement (repair_bodyocc.go).
			if repairBodyOcclusion(doc, scene) {
				changed = true
			}
			// Routing last: it measures the routes AROUND the positions the
			// passes above just settled.
			if repairRouting(doc, scene) {
				changed = true
			}
		}
		if !changed {
			break // converged (or nothing left we can fix)
		}
	}
	return doc, iters
}

// repairNeighbourLabels raises a standalone label/title that a node from another
// module covers in the projected view — the screen-space neighbour-label class
// (e.g. a wide heading whose tail projects over a node). It moves the label's
// owner back (−wy = up the screen), away from the content covering it, stepping
// until that label clears or a bounded number of steps (so a label that can't be
// cleared this way doesn't drift off-canvas). Returns whether anything moved.
func repairNeighbourLabels(doc *Document) bool {
	const (
		step     = 40.0
		maxSteps = 9
	)
	coveredNow := func() map[string]bool {
		m := map[string]bool{}
		for _, is := range LabelOcclusionIssues(doc) {
			if mm := neighbourOcclRe.FindStringSubmatch(is.Message); mm != nil {
				m[mm[1]] = true
			}
		}
		return m
	}
	covered := coveredNow()
	if len(covered) == 0 {
		return false
	}
	moved := false
	for label := range covered {
		owner := findOwnerByLabel(doc, label)
		// Only relocate a STANDALONE label (e.g. an iso_text title). Never move a
		// group/container by this path: a group caption "covered" by its own
		// child is a caption-ride (fixed by padding, repairCaptions) — sometimes
		// misclassified as a neighbour when the child overflows the group's
		// declared footprint. Moving the whole group can't clear it (the child
		// moves too) and would fling the layout instead.
		if owner == nil || len(owner.Parts) > 0 || isContainerShape(owner.Shape) {
			continue
		}
		for s := 0; s < maxSteps && coveredNow()[label]; s++ {
			if owner.Offset == nil {
				owner.Offset = &WorldPoint{}
			}
			owner.Offset.WY -= step
			moved = true
		}
	}
	return moved
}

// findOwnerByLabel returns the first part carrying the given label.
func findOwnerByLabel(doc *Document, label string) *CompositePart {
	var found *CompositePart
	walkParts(doc, func(p *CompositePart) {
		if found == nil && p.Label == label {
			found = p
		}
	})
	return found
}

// repairCaptions widens the front padding of every group whose caption is ridden
// by its own child. One pass; returns whether anything was bumped.
func repairCaptions(doc *Document) bool {
	const (
		step   = 0.5
		capPad = 8.0
	)
	occluded := map[string]bool{}
	for _, is := range LabelOcclusionIssues(doc) {
		if m := captionOcclRe.FindStringSubmatch(is.Message); m != nil {
			occluded[m[1]] = true
		}
	}
	if len(occluded) == 0 {
		return false
	}
	bumped := false
	walkParts(doc, func(p *CompositePart) {
		if p.Layout == nil || p.Label == "" || len(p.Parts) == 0 || !occluded[p.Label] {
			return
		}
		if cur := groupPadding(p); cur < capPad {
			np := math.Min(cur+step, capPad)
			p.Layout.Padding = &np
			bumped = true
		}
	})
	return bumped
}

// repairOverlaps pushes genuinely-colliding SIBLING parts apart along their
// axis of least penetration by nudging each side's offset. One pass; returns
// whether anything moved. It runs in two scopes:
//   - the scene's top level (the historical pass, unchanged behaviour);
//   - (WS-G) each group's direct children, in the group's own frame, under a
//     conservative measured-improvement guard (repairGroupOverlaps).
//
// It uses the evaluator's OWN overlap test (rectsOverlap on the plan model) —
// an (x,y) collision that ALSO shares a vertical band — instead of a naive
// footprint test. Parts stacked on different floors (place: above — a chip on a
// plate, a stacked board) deliberately share a footprint and must NOT be pushed
// apart; a naive test shreds such a clean Z-stack and never converges. Positions
// come from a clone so the source declarations are untouched; the push lands as
// an offset delta on the real parts (offset is a fine-tune on top of place).
func repairOverlaps(doc *Document, scene *Node) bool {
	rects, _, _ := buildPlanModel(cloneSceneForEval(scene), doc.Theme, doc.Canvas)
	moved := pushApart(siblingCollisions(scene.Parts, rects))
	if repairGroupOverlaps(doc, scene, rects) {
		moved = true
	}
	return moved
}

// sibCollision is one genuinely-colliding pair within a sibling scope: the two
// colliding footprints plus, for each, the ancestor that is a DIRECT child of
// the scope — the part the actuator can actually relocate by nudging its
// offset. (A collision between a loose node and another group's *child* is
// resolved by pushing the two siblings apart.)
type sibCollision struct {
	ra, rb planRect
	pa, pb *CompositePart
}

// siblingCollisions detects the colliding pairs among one scope's direct
// children (the scene's top level, or one group's parts). The collision set is
// every leaf under the scope, PLUS each sibling group's own slab — so two
// category trays whose slabs collide (even when no leaf inside them does) are
// paired, not just leaf-vs-leaf. Pairs sharing the same direct-child owner
// (intra-sibling) are skipped; rects from outside the scope have no owner here
// and are skipped too.
func siblingCollisions(siblings []*CompositePart, rects []planRect) []sibCollision {
	descOwner := map[string]*CompositePart{}
	var mark func(owner, p *CompositePart)
	mark = func(owner, p *CompositePart) {
		if p.ID != "" {
			descOwner[p.ID] = owner
		}
		for _, c := range p.Parts {
			if c != nil {
				mark(owner, c)
			}
		}
	}
	sibIDs := map[string]bool{}
	for _, p := range siblings {
		if p != nil {
			mark(p, p)
			if p.ID != "" {
				sibIDs[p.ID] = true
			}
		}
	}
	var boxes []planRect
	for _, r := range rects {
		if !r.container || sibIDs[r.id] {
			boxes = append(boxes, r)
		}
	}
	var out []sibCollision
	for a := 0; a < len(boxes); a++ {
		for b := a + 1; b < len(boxes); b++ {
			ra, rb := boxes[a], boxes[b]
			pa, pb := descOwner[ra.id], descOwner[rb.id]
			if pa == nil || pb == nil || pa == pb {
				continue // same sibling owner or outside this scope — skip
			}
			if !rectsOverlap(ra, rb) {
				continue // no real collision (e.g. Z-stacked) — leave it alone
			}
			out = append(out, sibCollision{ra: ra, rb: rb, pa: pa, pb: pb})
		}
	}
	return out
}

// pushApart separates each colliding pair: half the penetration (+margin) to
// each side, along the axis of least penetration. Returns whether anything
// moved.
func pushApart(cols []sibCollision) bool {
	const margin = 10.0
	for _, c := range cols {
		ra, rb := c.ra, c.rb
		ox := math.Min(ra.x+ra.w, rb.x+rb.w) - math.Max(ra.x, rb.x)
		oy := math.Min(ra.y+ra.d, rb.y+rb.d) - math.Max(ra.y, rb.y)
		if ox <= oy {
			push := (ox + margin) / 2
			if ra.x <= rb.x {
				nudge(c.pa, -push, 0)
				nudge(c.pb, +push, 0)
			} else {
				nudge(c.pa, +push, 0)
				nudge(c.pb, -push, 0)
			}
		} else {
			push := (oy + margin) / 2
			if ra.y <= rb.y {
				nudge(c.pa, 0, -push)
				nudge(c.pb, 0, +push)
			} else {
				nudge(c.pa, 0, +push)
				nudge(c.pb, 0, -push)
			}
		}
	}
	return len(cols) > 0
}

// repairGroupOverlaps extends the overlap actuator INTO groups (the master
// plan's "nested overlaps use only the top-level pass" gap): every group's
// direct children get the same push-apart, in the group's own frame — a child
// offset delta moves the child within its parent, and any resulting overflow
// is the existing autosize / validate-fit machinery's business.
//
// Conservative by contract: the whole round is measured. The nudges are kept
// only when the in-group collision count strictly drops AND the scene's
// readability cost does not rise; otherwise every nudge is rolled back and
// the pass reports no change, so a scene this pass cannot strictly improve
// renders byte-for-byte as before.
func repairGroupOverlaps(doc *Document, scene *Node, rects []planRect) bool {
	var groups []*CompositePart
	walkPartTree(scene.Parts, func(p *CompositePart) {
		if len(p.Parts) > 1 {
			groups = append(groups, p)
		}
	})
	if len(groups) == 0 {
		return false
	}
	before := 0
	cols := make([][]sibCollision, len(groups))
	for i, g := range groups {
		cols[i] = siblingCollisions(g.Parts, rects)
		before += len(cols[i])
	}
	if before == 0 {
		return false
	}
	costBefore := Readability(doc).Cost
	saved := snapshotOffsetsDeep(scene.Parts)
	moved := false
	for _, c := range cols {
		if pushApart(c) {
			moved = true
		}
	}
	if !moved {
		return false
	}
	rects2, _, _ := buildPlanModel(cloneSceneForEval(scene), doc.Theme, doc.Canvas)
	after := 0
	for _, g := range groups {
		after += len(siblingCollisions(g.Parts, rects2))
	}
	if after >= before || Readability(doc).Cost > costBefore+1e-9 {
		restoreOffsetsDeep(scene.Parts, saved)
		return false
	}
	return true
}

// snapshotOffsetsDeep copies every part's Offset (nil included) across the
// WHOLE tree in DFS order — the deep sibling of force_layout.go's top-level
// snapshotOffsets — so a measured-and-rejected round can be rolled back
// exactly.
func snapshotOffsetsDeep(parts []*CompositePart) []*WorldPoint {
	var out []*WorldPoint
	walkPartTree(parts, func(p *CompositePart) {
		if p.Offset != nil {
			o := *p.Offset
			out = append(out, &o)
		} else {
			out = append(out, nil)
		}
	})
	return out
}

// restoreOffsetsDeep writes a snapshotOffsetsDeep copy back (same DFS order).
func restoreOffsetsDeep(parts []*CompositePart, saved []*WorldPoint) {
	i := 0
	walkPartTree(parts, func(p *CompositePart) {
		if i < len(saved) {
			p.Offset = saved[i]
		}
		i++
	})
}

// walkPartTree visits every part under the given slice, depth-first.
func walkPartTree(parts []*CompositePart, fn func(*CompositePart)) {
	for _, p := range parts {
		if p == nil {
			continue
		}
		fn(p)
		walkPartTree(p.Parts, fn)
	}
}

// nudge adds a world (dx,dy) delta to a part's offset, creating it if absent.
func nudge(p *CompositePart, dx, dy float64) {
	if p == nil {
		return
	}
	if p.Offset == nil {
		p.Offset = &WorldPoint{}
	}
	p.Offset.WX += dx
	p.Offset.WY += dy
}

// groupPadding returns a group's effective front padding in cells (Padding,
// else Gap, else the layout default of 1).
func groupPadding(p *CompositePart) float64 {
	if p.Layout != nil {
		if p.Layout.Padding != nil {
			return *p.Layout.Padding
		}
		if p.Layout.Gap != nil {
			return *p.Layout.Gap
		}
	}
	return 1.0
}

// walkParts visits every CompositePart in the document, depth-first.
func walkParts(doc *Document, fn func(*CompositePart)) {
	var rec func(parts []*CompositePart)
	rec = func(parts []*CompositePart) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			fn(p)
			rec(p.Parts)
		}
	}
	for _, n := range doc.Nodes {
		if n != nil {
			rec(n.Parts)
		}
	}
}
