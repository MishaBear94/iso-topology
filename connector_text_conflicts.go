package isotopo

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ConnectorLabelConflict identifies one counterpart in the submitted
// placement scope. Shortfall is measured in root SVG user units: the maximum
// over that counterpart's primitives of the L-infinity penetration into the
// forbidden centre region, including the 6-unit clearance gap. It is not
// painted overlap area or a solution for moving the whole label/route.
type ConnectorLabelConflict struct {
	Kind               string  `json:"kind"` // body | route | arrow | label
	PartID             string  `json:"part_id,omitempty"`
	PartIndex          *int    `json:"part_index,omitempty"`
	ConnectorIndex     *int    `json:"connector_index,omitempty"`
	ClearanceShortfall float64 `json:"clearance_shortfall"`
}

type connectorTextRectTarget struct {
	kind  string
	id    string
	index int
	rect  screenRect
}

func connectorTextRectShortfall(label, obstacle screenRect) float64 {
	return math.Max(0, math.Min(math.Min(label.x1-obstacle.x0+6, obstacle.x1-label.x0+6), math.Min(label.y1-obstacle.y0+6, obstacle.y1-label.y0+6)))
}

func connectorTextStrokeShortfall(box screenRect, stroke connectorTextStroke) float64 {
	if !connectorTextStrokeHits(box, []connectorTextStroke{stroke}) {
		return 0
	}
	// Remove the root search's extra .03 guard: diagnostics use the final
	// serialized box and exactly the same 6-unit gap as the final predicate.
	var storage [8][2]float64
	poly := stroke.appendForbidden(storage[:0], box.x1-box.x0-.06, box.y1-box.y0-.06)
	x, y := box.cx(), box.cy()
	depth := math.Inf(1)
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		denom := math.Abs(dx) + math.Abs(dy)
		if denom > 0 {
			depth = math.Min(depth, (dx*(y-a[1])-dy*(x-a[0]))/denom)
		}
	}
	if math.IsInf(depth, 0) || math.IsNaN(depth) {
		return 0
	}
	return math.Max(0, depth)
}

// Collinear subdivisions do not add geometry. Merge connected pieces with
// the same owner/radius so inserting a straight waypoint cannot manufacture
// an apparent reduction in obstruction severity. No fuzzy merge is used.
func connectorTextConflictStrokes(strokes []connectorTextStroke) []connectorTextStroke {
	out := make([]connectorTextStroke, 0, len(strokes))
	for _, s := range strokes {
		if len(out) > 0 {
			p := &out[len(out)-1]
			if p.owner == s.owner && p.radius == s.radius {
				dx, dy := p.b[0]-p.a[0], p.b[1]-p.a[1]
				if dx == 0 && dy == 0 {
					dx, dy = s.b[0]-s.a[0], s.b[1]-s.a[1]
				}
				collinear := func(v [2]float64) bool { return dx*(v[1]-p.a[1])-dy*(v[0]-p.a[0]) == 0 }
				axis := 0
				if math.Abs(dy) > math.Abs(dx) {
					axis = 1
				}
				if (dx != 0 || dy != 0 || p.a == s.a) && collinear(s.a) && collinear(s.b) && math.Max(math.Min(p.a[axis], p.b[axis]), math.Min(s.a[axis], s.b[axis])) <= math.Min(math.Max(p.a[axis], p.b[axis]), math.Max(s.a[axis], s.b[axis])) {
					lo, hi := p.a, p.a
					for _, v := range [][2]float64{p.b, s.a, s.b} {
						if v[axis] < lo[axis] {
							lo = v
						}
						if v[axis] > hi[axis] {
							hi = v
						}
					}
					p.a, p.b = lo, hi
					continue
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// Observe after final status and all measured pills are known. Existing clear
// certificates (including exact curve AABBs) remain authoritative. A rich
// body-region refinement only returns clear placements; it cannot silently
// turn into an AABB diagnostic. No positions or status are changed here.
func observeConnectorTextConflicts(records []ConnectorLabelRecord, targets []connectorTextRectTarget, strokes []connectorTextStroke) {
	needed := false
	for _, r := range records {
		if r.Label.Typography != nil {
			r.Label.Typography.Conflicts = nil
		}
		needed = needed || r.Label.Typography != nil && r.Label.Typography.Placement == "blocked"
	}
	if !needed {
		return
	}
	strokes = connectorTextConflictStrokes(strokes)
	for _, r := range records {
		targets = append(targets, connectorTextRectTarget{kind: "label", index: r.ConnectorIndex, rect: connectorLabelRect(r.Label)})
	}
	for _, r := range records {
		typography := r.Label.Typography
		if typography == nil || typography.Placement != "blocked" {
			continue
		}
		box := connectorLabelRect(r.Label)
		var conflicts []ConnectorLabelConflict
		for _, target := range targets {
			if target.kind == "body" && strings.Contains(typography.Scope, "body-region") {
				continue
			}
			if target.kind == "label" && target.index == r.ConnectorIndex {
				continue
			}
			depth := connectorTextRectShortfall(box, target.rect)
			if depth <= 0 {
				continue
			}
			c := ConnectorLabelConflict{Kind: target.kind, ClearanceShortfall: depth}
			index := target.index
			if target.kind == "body" {
				c.PartID = target.id
				c.PartIndex = &index
			} else {
				c.ConnectorIndex = &index
			}
			conflicts = append(conflicts, c)
		}
		// A bent or subdivided route is still one semantic counterpart.
		depths := map[int]float64{}
		for _, s := range strokes {
			if s.owner == r.ConnectorIndex {
				continue
			}
			if depth := connectorTextStrokeShortfall(box, s); depth > depths[s.owner] {
				depths[s.owner] = depth
			}
		}
		for owner, depth := range depths {
			index := owner
			conflicts = append(conflicts, ConnectorLabelConflict{Kind: "route", ConnectorIndex: &index, ClearanceShortfall: depth})
		}
		sort.Slice(conflicts, func(i, j int) bool {
			a, b := conflicts[i], conflicts[j]
			if a.Kind != b.Kind {
				return a.Kind < b.Kind
			}
			if a.PartID != b.PartID {
				return a.PartID < b.PartID
			}
			if a.PartIndex != nil && b.PartIndex != nil {
				return *a.PartIndex < *b.PartIndex
			}
			return *a.ConnectorIndex < *b.ConnectorIndex
		})
		typography.Conflicts = conflicts
	}
}

func connectorTextBodyTarget(id string, index int, rect screenRect) connectorTextRectTarget {
	if id == "" {
		id = fmt.Sprintf("#part:%d", index)
	}
	return connectorTextRectTarget{kind: "body", id: id, index: index, rect: rect}
}

func addConnectorTextConflictDefects(out visualDefects, records []ConnectorLabelRecord) {
	for _, record := range records {
		if record.Label.Typography == nil || record.Label.Typography.Placement != "blocked" {
			continue
		}
		for _, conflict := range record.Label.Typography.Conflicts {
			key := visualDefect{kind: "connector-label-" + conflict.Kind, edge: record.ConnectorIndex, a: conflict.PartID}
			if conflict.ConnectorIndex != nil {
				key.peer = *conflict.ConnectorIndex
			}
			out[key] = math.Max(out[key], conflict.ClearanceShortfall)
		}
	}
}

func connectorTextConflictMessage(conflicts []ConnectorLabelConflict) string {
	if len(conflicts) == 0 {
		return "Final inline label clearance is unresolved within the declared body/route/arrow/pill envelopes; full text retained. Increase spacing or adjust the route."
	}
	var names []string
	for _, c := range conflicts {
		if len(names) == 3 {
			break
		}
		if c.Kind == "body" {
			names = append(names, fmt.Sprintf("body %q", c.PartID))
		} else {
			names = append(names, fmt.Sprintf("connector[%d] %s", *c.ConnectorIndex, c.Kind))
		}
	}
	summary := strings.Join(names, ", ")
	if len(conflicts) > len(names) {
		summary += fmt.Sprintf(" and %d other counterparts", len(conflicts)-len(names))
	}
	return "Final inline label violates the declared 6-unit clearance around " + summary + "; full text retained. Increase spacing or adjust the route."
}
