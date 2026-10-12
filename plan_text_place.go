package isotopo

import (
	"math"
	"sort"

	"github.com/mazznoer/csscolorparser"
)

// PlanLabelPlacement describes clearance within a declared geometric scope.
// It is separate from fitting inside the owner budget. Conflicts are populated
// by detailed reports; absent conflicts in an ordinary preview are not proof.
type PlanLabelPlacement struct {
	Status         string              `json:"status"`
	Scope          string              `json:"scope"`
	Offset         [2]float64          `json:"offset"`
	RetainedReason string              `json:"retained_reason,omitempty"`
	Conflicts      []PlanLabelConflict `json:"conflicts,omitempty"`
}

type PlanLabelConflict struct {
	Kind               string  `json:"kind"` // body | title | route | arrow | label
	PartID             string  `json:"part_id,omitempty"`
	PartIndex          *int    `json:"part_index,omitempty"`
	ConnectorIndex     *int    `json:"connector_index,omitempty"`
	ClearanceShortfall float64 `json:"clearance_shortfall"`
}

type planTitleObstacle struct {
	kind        string
	index       int
	box         screenRect
	stroke      *connectorTextStroke
	maskByOwner bool
}

const planTitlePlacementScope = "later-body-aabb,paint-order-masked-title-ink-aabb,own-opaque-core-masked-route-envelope,arrow-aabb,pill-aabb"

// The rounded footprint certainly fills this core. Only fully opaque paint
// can suppress route ink; transparent and unknown CSS fills never prove it.
func planOpaqueCore(r planRect, ox, oy float64) (screenRect, bool) {
	c, err := csscolorparser.Parse(r.fill)
	if r.boundary || err != nil || c.A < 1 || r.w < 12 || r.d < 12 {
		return screenRect{}, false
	}
	x, y := svgCoord(r.x+ox), svgCoord(r.y+oy)
	return screenRect{x + 6, y + 6, x + svgCoord(r.w) - 6, y + svgCoord(r.d) - 6}, true
}

func planLater(a, b planRect, ai, bi int) bool {
	if a.container != b.container {
		return a.container
	}
	return bi > ai
}

func planSubtractCore(b, core screenRect, opaque bool) []screenRect {
	if !opaque {
		return []screenRect{b}
	}
	x0, y0 := math.Max(b.x0, core.x0), math.Max(b.y0, core.y0)
	x1, y1 := math.Min(b.x1, core.x1), math.Min(b.y1, core.y1)
	if x0 >= x1 || y0 >= y1 {
		return []screenRect{b}
	}
	var out []screenRect
	for _, r := range []screenRect{{b.x0, b.y0, b.x1, y0}, {b.x0, y1, b.x1, b.y1}, {b.x0, y0, x0, y1}, {x1, y0, b.x1, y1}} {
		if r.x0 < r.x1 && r.y0 < r.y1 {
			out = append(out, r)
		}
	}
	return out
}

func planTitleObstacles(index int, rects []planRect, texts []planTextLayout, static []planTitleObstacle, ox, oy float64) []planTitleObstacle {
	out := make([]planTitleObstacle, len(static), len(static)+len(rects)*3)
	copy(out, static)
	for i := range out {
		if out[i].kind == "route" || out[i].kind == "arrow" {
			out[i].maskByOwner = !rects[index].container
		}
	}
	for j, r := range rects {
		if j == index {
			continue
		}
		if planLater(rects[index], r, index, j) {
			x, y, w, d := svgCoord(r.x+ox), svgCoord(r.y+oy), svgCoord(r.w), svgCoord(r.d)
			box := screenRect{x, y, x + w, y + d}
			if r.boundary {
				// Dashed outline gaps are conservatively filled; its interior is
				// still empty, so a boundary cannot become a solid occluder.
				for _, b := range []screenRect{{x, y, x + w, y}, {x, y + d, x + w, y + d}, {x, y, x, y + d}, {x + w, y, x + w, y + d}} {
					out = append(out, planTitleObstacle{kind: "body", index: j, box: b.inflate(planLineW(r) / 2)})
				}
			} else {
				out = append(out, planTitleObstacle{kind: "body", index: j, box: box.inflate(planLineW(r) / 2)})
			}
		}
		for _, b := range texts[j].inkRects() {
			out = append(out, planTitleObstacle{kind: "title", index: j, box: b, maskByOwner: planLater(r, rects[index], j, index)})
		}
	}
	return out
}

func planTitleShortfall(ink screenRect, o planTitleObstacle, core screenRect, opaque bool) float64 {
	pieces := []screenRect{ink}
	if o.maskByOwner {
		pieces = planSubtractCore(ink, core, opaque)
	}
	depth := 0.
	for _, b := range pieces {
		v := 0.
		if o.stroke != nil {
			v = connectorTextStrokeShortfall(b, *o.stroke)
		} else {
			v = connectorTextRectShortfall(b, o.box)
		}
		depth = math.Max(depth, v)
	}
	return depth
}

// A negative work budget means observation, not a bounded candidate query.
func planTitleClear(p planTextLayout, obstacles []planTitleObstacle, core screenRect, opaque bool, work *int) bool {
	for _, b := range p.inkRects() {
		for _, o := range obstacles {
			if *work == 0 {
				return false
			}
			if *work > 0 {
				*work--
			}
			if planTitleShortfall(b, o, core, opaque) > 0 {
				return false
			}
		}
	}
	return true
}

func planShiftTitle(p planTextLayout, dx, dy float64) planTextLayout {
	q := p
	q.ink = nil
	q.record.Lines = append([]PlanLabelLine(nil), p.record.Lines...)
	for i := range q.record.Lines {
		l := &q.record.Lines[i]
		x, y := svgCoord(l.X+dx), svgCoord(l.Baseline+dy)
		if l.InkBounds != nil {
			b := *l.InkBounds
			b.X += x - l.X
			b.Y += y - l.Baseline
			l.InkBounds = &b
		}
		l.X, l.Baseline = x, y
	}
	q.ink = q.inkRects()
	return q
}

func planTitleCandidates(p planTextLayout, obs []planTitleObstacle) [][2]float64 {
	ink := p.inkRects()
	if len(ink) == 0 {
		return nil
	}
	b := ink[0]
	for _, r := range ink[1:] {
		b.x0 = math.Min(b.x0, r.x0)
		b.y0 = math.Min(b.y0, r.y0)
		b.x1 = math.Max(b.x1, r.x1)
		b.y1 = math.Max(b.y1, r.y1)
	}
	budget := p.record.Budget
	minX, maxX := budget.X-b.x0, budget.X+budget.Width-b.x1
	minY, maxY := budget.Y-b.y0, budget.Y+budget.Height-b.y1
	xs, ys := []float64{0, minX, maxX}, []float64{0, minY, maxY}
	for _, o := range obs {
		r := o.box
		if o.stroke != nil {
			s := o.stroke
			r = screenRect{math.Min(s.a[0], s.b[0]), math.Min(s.a[1], s.b[1]), math.Max(s.a[0], s.b[0]), math.Max(s.a[1], s.b[1])}.inflate(s.radius)
		}
		for _, line := range ink {
			if len(xs) >= 255 {
				break
			}
			xs = append(xs, r.x0-line.x1-6.03, r.x1-line.x0+6.03)
			ys = append(ys, r.y0-line.y1-6.03, r.y1-line.y0+6.03)
		}
		// Candidate construction is bounded independently of predicates.
		if len(xs) >= 255 {
			break
		}
	}
	axis := func(values []float64, lo, hi float64) []float64 {
		set := map[float64]bool{}
		var out []float64
		for _, v := range values {
			v = svgCoord(v)
			if v >= lo && v <= hi && !set[v] {
				set[v] = true
				out = append(out, v)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if math.Abs(out[i]) != math.Abs(out[j]) {
				return math.Abs(out[i]) < math.Abs(out[j])
			}
			return out[i] < out[j]
		})
		if len(out) > 32 {
			out = out[:32]
		}
		return out
	}
	xs, ys = axis(xs, minX, maxX), axis(ys, minY, maxY)
	var out [][2]float64
	for _, x := range xs {
		for _, y := range ys {
			if x != 0 || y != 0 {
				out = append(out, [2]float64{x, y})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		da, db := a[0]*a[0]+a[1]*a[1], b[0]*b[0]+b[1]*b[1]
		if da != db {
			return da < db
		}
		if a[1] != b[1] {
			return a[1] < b[1]
		}
		return a[0] < b[0]
	})
	if len(out) > 512 {
		out = out[:512]
	}
	return out
}

func planTitleConflicts(p planTextLayout, obs []planTitleObstacle, rects []planRect, core screenRect, opaque bool) []PlanLabelConflict {
	type key struct {
		kind  string
		index int
	}
	depths := map[key]float64{}
	for _, b := range p.inkRects() {
		for _, o := range obs {
			v := planTitleShortfall(b, o, core, opaque)
			k := key{o.kind, o.index}
			depths[k] = math.Max(depths[k], v)
		}
	}
	var out []PlanLabelConflict
	for k, v := range depths {
		if v <= 0 {
			continue
		}
		i := k.index
		c := PlanLabelConflict{Kind: k.kind, ClearanceShortfall: v}
		if k.kind == "body" || k.kind == "title" {
			c.PartID = rects[i].id
			c.PartIndex = &i
		} else {
			c.ConnectorIndex = &i
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.PartIndex != nil {
			return *a.PartIndex < *b.PartIndex
		}
		return *a.ConnectorIndex < *b.ConnectorIndex
	})
	return out
}

// Reposition only obstructed titles. Bodies, routes, all pills and previously
// clear titles are fixed. Each move clears every known counterpart and remains
// inside its own budget. Unknown text disables moves, not truthful observation.
func refinePlanTitles(rects []planRect, texts []planTextLayout, static []planTitleObstacle, ox, oy float64, uncertain, details bool) bool {
	return refinePlanTitlesWithBudget(rects, texts, static, ox, oy, uncertain, details, 65536)
}

func refinePlanTitlesWithBudget(rects []planRect, texts []planTextLayout, static []planTitleObstacle, ox, oy float64, uncertain, details bool, work int) bool {
	for i, r := range rects {
		if r.label != "" && texts[i].font == nil {
			uncertain = true
		}
	}
	moved := false
	for i := range texts {
		p := texts[i]
		if p.font == nil {
			continue
		}
		core, opaque := planOpaqueCore(rects[i], ox, oy)
		obs := planTitleObstacles(i, rects, texts, static, ox, oy)
		unlimited := -1
		if planTitleClear(p, obs, core, opaque, &unlimited) {
			texts[i].record.Placement = &PlanLabelPlacement{Status: "clear-within-scope", Scope: planTitlePlacementScope}
			continue
		}
		reason := "no-safe-candidate"
		if uncertain {
			reason = "unmeasured-text"
		} else if p.record.Status != "within-budget" {
			reason = "infeasible-budget"
		} else {
			for _, delta := range planTitleCandidates(p, obs) {
				if work <= 0 {
					reason = "search-budget"
					break
				}
				q := planShiftTitle(p, delta[0], delta[1])
				fits := true
				b := q.record.Budget
				for _, r := range q.inkRects() {
					if r.x0 < b.X || r.y0 < b.Y || r.x1 > b.X+b.Width || r.y1 > b.Y+b.Height {
						fits = false
						break
					}
				}
				if fits && planTitleClear(q, obs, core, opaque, &work) {
					q.record.Placement = &PlanLabelPlacement{Offset: delta, Status: "clear-within-scope", Scope: planTitlePlacementScope}
					texts[i] = q
					moved = true
					reason = ""
					break
				}
			}
		}
		if reason != "" {
			texts[i].record.Placement = &PlanLabelPlacement{RetainedReason: reason}
		}
	}
	// Observe final state: a neighbour's accepted move may also clear a title
	// that was blocked earlier. No stale certificate survives the last move.
	for i := range texts {
		p := &texts[i]
		if p.font == nil {
			continue
		}
		if p.record.Placement == nil {
			p.record.Placement = &PlanLabelPlacement{}
		}
		// Every accepted move clears every peer, preserving existing clear
		// certificates. Only unresolved titles need a final neighbour scan.
		if p.record.Placement.Status == "clear-within-scope" {
			continue
		}
		v := p.record.Placement
		v.Scope = planTitlePlacementScope
		v.Status = "clear-within-scope"
		core, opaque := planOpaqueCore(rects[i], ox, oy)
		obs := planTitleObstacles(i, rects, texts, static, ox, oy)
		unlimited := -1
		if !planTitleClear(*p, obs, core, opaque, &unlimited) {
			v.Status = "blocked"
			if details {
				v.Conflicts = planTitleConflicts(*p, obs, rects, core, opaque)
			}
		} else {
			v.RetainedReason = ""
		}
	}
	return moved
}

func addPlanTitlePlacementDefects(d visualDefects, texts []planTextLayout) {
	for _, p := range texts {
		v := p.record.Placement
		if v == nil || v.Status != "blocked" {
			continue
		}
		d[visualDefect{kind: "plan-title-blocked", a: p.record.PartID, edge: p.record.PartIndex}] = 1
		for _, c := range v.Conflicts {
			peer := 0
			if c.PartIndex != nil {
				peer = *c.PartIndex
			} else if c.ConnectorIndex != nil {
				peer = *c.ConnectorIndex
			}
			d[visualDefect{kind: "plan-title-" + c.Kind, a: p.record.PartID, b: c.PartID, edge: p.record.PartIndex, peer: peer}] = c.ClearanceShortfall
		}
	}
}
