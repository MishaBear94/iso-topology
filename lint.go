package isotopo

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// VisualLint catches geometry problems pure structural validation can't: after
// solving layout/place into concrete positions, it flags atomic parts in
// DIFFERENT containers whose 3-D solids interpenetrate. The same-container case
// is already covered by the layout solver's sibling-overlap check; this adds
// the cross-container collisions — a `place`/`offset` that lands one group's
// part inside another group's part — that the sibling check structurally
// cannot see.
//
// It reports WORLD-space interpenetration, not iso screen-overlap: two solids
// occupying the same physical space is always a mistake, whereas parts that
// merely overlap on screen are usually intentional depth layering. That keeps
// the lint quiet on well-formed scenes — it stays silent on every bundled
// sample — so an agent can treat a hit as a real "fix this" signal.
func VisualLint(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue
	for nodeID, n := range doc.Nodes {
		if n == nil || n.Shape != "composite" {
			continue
		}
		issues = append(issues, lintComposite(n, doc.Theme, doc.Canvas, nodeID)...)
	}
	return issues
}

type lintBox struct {
	id         string
	parent     string
	x0, y0, z0 float64
	x1, y1, z1 float64
}

func lintComposite(n *Node, theme *Theme, canvas *Canvas, nodeID string) []Issue {
	// parent-of map from the ORIGINAL tree (lowering flattens hierarchy away,
	// so we capture container membership before solving).
	parent := map[string]string{}
	var walk func(parts []*CompositePart, container string)
	walk = func(parts []*CompositePart, container string) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			if p.ID != "" {
				parent[p.ID] = container
			}
			if isContainerShape(p.Shape) {
				walk(p.Parts, p.ID)
			}
		}
	}
	walk(n.Parts, "")

	// Solve positions on a clone — Validate/Lint must never mutate the doc.
	clone := cloneSceneForEval(n)
	applyLayout(clone, canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0, clone.GridStep)

	boxes := make([]lintBox, 0, len(flat))
	for _, p := range flat {
		if p == nil || p.isSubstrate || p.ID == "" || isGhostPart(p) {
			continue
		}
		w, d, h := partDimensions(p)
		if w <= 0 || d <= 0 || h <= 0 {
			continue // label-only parts occupy no solid space
		}
		ox, oy, oz := 0.0, 0.0, 0.0
		if p.Position != nil && clone.GridStep > 0 {
			ox = float64(p.Position.I) * clone.GridStep
			oy = float64(p.Position.J) * clone.GridStep
		}
		if p.Offset != nil {
			ox += p.Offset.WX
			oy += p.Offset.WY
			oz += p.Offset.WZ
		}
		boxes = append(boxes, lintBox{
			id: p.ID, parent: parent[p.ID],
			x0: ox, y0: oy, z0: oz,
			x1: ox + w, y1: oy + d, z1: oz + h,
		})
	}

	const eps = 0.5 // ignore mere touching (a part standing ON another)
	var issues []Issue
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			if a.parent == b.parent {
				continue // same container — the sibling-overlap check owns this
			}
			if stackBase(a.id) == stackBase(b.id) {
				continue // stack replicas legitimately share space in z
			}
			ox := math.Min(a.x1, b.x1) - math.Max(a.x0, b.x0)
			oy := math.Min(a.y1, b.y1) - math.Max(a.y0, b.y0)
			oz := math.Min(a.z1, b.z1) - math.Max(a.z0, b.z0)
			if ox > eps && oy > eps && oz > eps {
				lo, hi := a.id, b.id
				if lo > hi {
					lo, hi = hi, lo
				}
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     fmt.Sprintf("nodes.%s", nodeID),
					Message: fmt.Sprintf("parts %q and %q occupy overlapping space (%.0f×%.0f×%.0f world units) across containers — they will visually collide; move one or add a place gap",
						lo, hi, ox, oy, oz),
				})
			}
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Message < issues[j].Message })
	return issues
}

func stackBase(id string) string {
	if i := strings.Index(id, "~"); i >= 0 {
		return id[:i]
	}
	return id
}
