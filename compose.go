package isotopo

import (
	"fmt"
	"math"
	"sort"
)

// Composition repair — the acting half of the M2 composition metrics. Where
// RepairScene fixes DEFECTS (occlusion, overlap, contrast), ComposeScene
// improves the positive qualities, starting with the highest-value bounded
// move: snapping off-grid parts onto their neighbours' alignment tracks.
//
// Guarantees (the reason this is safe to run unattended):
//   - only parts that carry an EXPLICIT offset are moved — solver-laid parts
//     are already composed and authors' `place:` relations are never touched;
//   - every snap is bounded (≤ composeMaxSnap world units) and axis-minimal;
//   - each snap is measured against named visual defects; a new crossing,
//     tunnel, overlap or occlusion is rejected, even if total counts improve;
//   - deterministic (declared order, fixed passes) and idempotent — a snapped
//     part shares a track, so a second run finds nothing to do.
//
// Opt-in via `isotopo repair --compose [--write]`; persistence rides the same
// diff→set-field machinery as every other repair.

const (
	composeMaxSnap = 150.0 // freehand misalignment is typically 60-150u
	// (measured on scattered-scene fixtures); the collision guard, not a tight
	// radius, is what keeps snapping safe — 48u caught nothing real
	composeTol    = 6.0 // "on a track" tolerance, matches compAlignment
	composePasses = 4
)

// composeCand is a movable leaf: a part with its own explicit offset, plus its
// solved absolute footprint.
type composeCand struct {
	part *CompositePart
	rect compRect
}

// ComposeScene aligns off-track parts in every composite node of the doc,
// mutating offsets in place. Returns a human/agent-readable fix list in the
// RepairAndReport style.
func ComposeScene(doc *Document) []string {
	if doc == nil {
		return nil
	}
	var fixes []string
	ids := make([]string, 0, len(doc.Nodes))
	for id := range doc.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := doc.Nodes[id]
		if n == nil || n.Shape != "composite" {
			continue
		}
		fixes = append(fixes, composeNode(n, doc.Theme, doc.Canvas)...)
	}
	return fixes
}

func composeNode(n *Node, theme *Theme, canvas *Canvas) []string {
	var fixes []string
	var accepted visualDefects
	for pass := 0; pass < composePasses; pass++ {
		leaves, cands := composeGeometry(n, canvas)
		if len(leaves) < 3 {
			return fixes
		}
		changed := false
		for ci := range cands {
			c := &cands[ci]
			// Re-read the candidate's CURRENT rect: earlier snaps in this same
			// pass may have moved neighbours (or this part's tracks). Working
			// against stale geometry made pairs swap onto each other's old
			// tracks forever — netting zero movement across passes.
			c.rect = leaves[c.rect.ord]
			if composeOnTrack(c.rect, leaves) {
				continue
			}
			dx, dy := composeNearestTrack(c.rect, leaves)
			// Try the cheaper axis first, fall back to the other; a snap that
			// would collide with any other leaf footprint is reverted.
			order := [][2]float64{{dx, 0}, {0, dy}}
			if math.Abs(dy) < math.Abs(dx) {
				order = [][2]float64{{0, dy}, {dx, 0}}
			}
			for _, mv := range order {
				if mv[0] == 0 && mv[1] == 0 || math.Abs(mv[0])+math.Abs(mv[1]) > composeMaxSnap {
					continue
				}
				moved := c.rect
				moved.x += mv[0]
				moved.y += mv[1]
				if composeCollides(moved, leaves) {
					continue
				}
				if accepted == nil {
					accepted = sceneVisualDefects(n, theme, canvas)
				}
				saved := *c.part.Offset
				c.part.Offset.WX += mv[0]
				c.part.Offset.WY += mv[1]
				candidate := sceneVisualDefects(n, theme, canvas)
				if !candidate.noWorseThan(accepted) {
					*c.part.Offset = saved
					continue
				}
				accepted = candidate
				leaves[c.rect.ord].x += mv[0] // keep the live geometry current
				leaves[c.rect.ord].y += mv[1] // so later snaps see the truth
				axis := "x"
				if mv[0] == 0 {
					axis = "y"
				}
				fixes = append(fixes, fmt.Sprintf("aligned %q onto a neighbour's %s-track (moved %.0f)", c.rect.id, axis, mv[0]+mv[1]))
				changed = true
				break
			}
		}
		if !changed {
			break
		}
	}
	return fixes
}

// composeGeometry solves the node's layout on a clone (never mutating layout
// state on the real doc) and returns the solved absolute leaf rects PLUS the
// movable candidates mapped back to the REAL parts by identity path.
func composeGeometry(n *Node, canvas *Canvas) (leaves []compRect, cands []composeCand) {
	clone := cloneSceneForEval(n)
	applyLayout(clone, canvas)

	// Walk clone (solved geometry) and the real tree in lockstep — the shapes
	// are identical because cloneSceneForEval copies the parts tree.
	var walk func(cp, rp []*CompositePart, bx, by float64)
	walk = func(cp, rp []*CompositePart, bx, by float64) {
		for i := range cp {
			c := cp[i]
			if c == nil || i >= len(rp) || rp[i] == nil {
				continue
			}
			x, y := bx, by
			if c.Offset != nil {
				x += c.Offset.WX
				y += c.Offset.WY
			}
			w, d := partFootprint(c)
			if !isContainerShape(c.Shape) {
				r := compRect{ord: len(leaves), id: c.ID, x: x, y: y, w: w, d: d}
				leaves = append(leaves, r)
				// Movable = the REAL part carries its own explicit offset.
				if rp[i].Offset != nil && rp[i].Place == nil {
					cands = append(cands, composeCand{part: rp[i], rect: r})
				}
			}
			walk(c.Parts, rp[i].Parts, x, y)
		}
	}
	walk(clone.Parts, n.Parts, 0, 0)
	return leaves, cands
}

func composeOnTrack(r compRect, leaves []compRect) bool {
	near := func(a, b float64) bool { return math.Abs(a-b) <= composeTol }
	for _, o := range leaves {
		if o.ord == r.ord {
			continue
		}
		if near(r.x, o.x) || near(r.cx(), o.cx()) || near(r.x+r.w, o.x+o.w) ||
			near(r.y, o.y) || near(r.cy(), o.cy()) || near(r.y+r.d, o.y+o.d) {
			return true
		}
	}
	return false
}

// composeNearestTrack returns the minimal x and y deltas that would put r on
// some other leaf's track (edge-to-edge or centre-to-centre).
func composeNearestTrack(r compRect, leaves []compRect) (dx, dy float64) {
	dx, dy = math.Inf(1), math.Inf(1)
	upd := func(cur *float64, delta float64) {
		if math.Abs(delta) < math.Abs(*cur) {
			*cur = delta
		}
	}
	for _, o := range leaves {
		if o.ord == r.ord {
			continue
		}
		upd(&dx, o.x-r.x)
		upd(&dx, o.cx()-r.cx())
		upd(&dx, (o.x+o.w)-(r.x+r.w))
		upd(&dy, o.y-r.y)
		upd(&dy, o.cy()-r.cy())
		upd(&dy, (o.y+o.d)-(r.y+r.d))
	}
	if math.IsInf(dx, 1) {
		dx = 0
	}
	if math.IsInf(dy, 1) {
		dy = 0
	}
	return dx, dy
}

// composeCollides reports whether the moved rect would intersect any OTHER
// leaf footprint (small negative margin so kissing edges are allowed).
func composeCollides(moved compRect, leaves []compRect) bool {
	const m = 1.0
	for _, o := range leaves {
		if o.ord == moved.ord {
			continue
		}
		if moved.x+m < o.x+o.w && o.x+m < moved.x+moved.w &&
			moved.y+m < o.y+o.d && o.y+m < moved.y+moved.d {
			return true
		}
	}
	return false
}
