package isotopo

import (
	"fmt"
	"math"
	"sort"
)

// PlanFootprintIssues flags parts whose TOP-DOWN (plan) projection footprints
// touch, cross, or cover one another. Where VisualLint (lint.go) reports 3-D
// world interpenetration — solids occupying the same physical space across
// containers — this looks at the flat plan view the `evaluate`/Plan renderer
// draws: height is dropped, every part is its (x, y, w, d) footprint rectangle,
// and any two rectangles that meet are a defect.
//
// In the plan view two boxes sharing screen space read as broken regardless of
// their z, so the rule covers BOTH leaf nodes and group/container slabs. The one
// legitimate "overlap" — a child sitting inside its own group (or any ancestor)
// — is excluded by ancestry; stack replicas that share space by design are
// excluded too. boundary zones (dashed trust regions meant to wrap nodes) and
// iso_text labels carry no solid footprint and are skipped.
//
// Everything is a Warning: an intentional isometric stair/stack legitimately
// overlaps in plan, so a hit is a strong "look at this" signal, not a hard stop.
func PlanFootprintIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue
	for nodeID, n := range doc.Nodes {
		if n == nil || n.Shape != "composite" {
			continue
		}
		issues = append(issues, planFootprintComposite(n, doc.Canvas, nodeID)...)
	}
	return issues
}

type planBox struct {
	id        string
	path      string
	w, d      float64
	x0, y0    float64
	x1, y1    float64
	container bool
	ancestors map[string]bool // ids of every container enclosing this part
}

func planFootprintComposite(n *Node, canvas *Canvas, nodeID string) []Issue {
	// Solve on a clone — Validate must never mutate the doc — then walk the
	// solved tree accumulating each part's ABSOLUTE plan corner, exactly as
	// planCollect does for the renderer, so the check matches what is drawn.
	clone := cloneSceneForEval(n)
	applyLayout(clone, canvas)

	var boxes []planBox
	var walk func(parts []*CompositePart, ox, oy float64, anc map[string]bool, ancPath string)
	walk = func(parts []*CompositePart, ox, oy float64, anc map[string]bool, ancPath string) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			x, y, _ := partLocalOffset(p, clone.GridStep)
			x, y = x+ox, y+oy
			w, d, _ := planDims(p.Shape, p.Geom)
			container := isContainerShape(p.Shape) || len(p.Parts) > 0

			path := ancPath + ".parts[]"
			if p.ID != "" {
				path = fmt.Sprintf("%s.parts[%s]", ancPath, p.ID)
			}
			// boundary = intentional wrapping zone; iso_text = a bare label.
			// Neither is a solid box, so neither collides.
			if p.ID != "" && p.Shape != "iso_text" && p.Shape != "boundary" {
				boxes = append(boxes, planBox{
					id: p.ID, path: path,
					w: w, d: d,
					x0: x, y0: y, x1: x + w, y1: y + d,
					container: container,
					ancestors: anc,
				})
			}
			if len(p.Parts) > 0 {
				childAnc := make(map[string]bool, len(anc)+1)
				for k := range anc {
					childAnc[k] = true
				}
				if p.ID != "" {
					childAnc[p.ID] = true
				}
				walk(p.Parts, x, y, childAnc, path)
			}
		}
	}
	walk(clone.Parts, 0, 0, map[string]bool{}, fmt.Sprintf("nodes.%s", nodeID))

	const eps = 0.5 // sub-pixel meeting still counts as a touch
	var issues []Issue
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			if a.ancestors[b.id] || b.ancestors[a.id] {
				continue // legitimate containment (child inside its group)
			}
			if stackBase(a.id) == stackBase(b.id) {
				continue // stack replicas share plan space by design
			}
			ox := math.Min(a.x1, b.x1) - math.Max(a.x0, b.x0)
			oy := math.Min(a.y1, b.y1) - math.Max(a.y0, b.y0)
			if ox < -eps || oy < -eps {
				continue // a real gap on at least one axis — cleanly separated
			}

			lo, hi := a, b
			if lo.id > hi.id {
				lo, hi = hi, lo
			}
			kind := "footprints"
			if lo.container || hi.container {
				kind = "footprints (group)"
			}
			suggest := "increase the place gap or adjust the offset so the plan-view footprints don't meet"

			if ox > eps && oy > eps {
				verb := "overlap"
				if rectCovers(a, b) || rectCovers(b, a) {
					verb = "cover one another"
				}
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     lo.path,
					Message: fmt.Sprintf("%s %q and %q %s in the plan (top-down) view by %.0f×%.0f units — they will visually collide; %s",
						kind, lo.id, hi.id, verb, ox, oy, suggest),
					Suggest: suggest,
				})
			} else {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     lo.path,
					Message: fmt.Sprintf("%s %q and %q touch (edges meet) in the plan (top-down) view — %s",
						kind, lo.id, hi.id, suggest),
					Suggest: suggest,
				})
			}
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Message < issues[j].Message })
	return issues
}

// rectCovers reports whether outer fully encloses inner in the plan projection.
func rectCovers(outer, inner planBox) bool {
	const eps = 0.5
	return outer.x0 <= inner.x0+eps && outer.y0 <= inner.y0+eps &&
		outer.x1+eps >= inner.x1 && outer.y1+eps >= inner.y1
}
