package isotopo

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// LabelOcclusionIssues warns when text is covered by a later-painted node in
// the rendered view. It covers three kinds of text:
//
//   - group labels, whose silent auto-lift (resolveGroupLabelOcclusion) would
//     otherwise swallow the defect — a caption floating over a node body still
//     reads as cramped;
//   - iso_text titles;
//   - ordinary node face labels (the default top-face label), which have no
//     auto-lift at all: a node painted in front simply hides them.
//
// Labels with text.orient: screen are drawn on the top screen layer and can
// never be occluded by a node body, so they are skipped — which is why the
// theme is threaded through (orient is often set theme-wide).
func LabelOcclusionIssues(doc *Document) []Issue {
	if doc == nil || (doc.Canvas != nil && doc.Canvas.Projection == "top") {
		return nil
	}
	var issues []Issue
	for nodeID, n := range doc.Nodes {
		if n == nil || n.Shape != "composite" {
			continue
		}
		issues = append(issues, labelOcclusionComposite(n, doc.Theme, doc.Canvas, nodeID)...)
	}
	return issues
}

func labelOcclusionComposite(n *Node, theme *Theme, canvas *Canvas, nodeID string) []Issue {
	// Solve + lower on a clone (Validate must never mutate the doc) to get the
	// same paint-ordered flat parts, solved offsets, and groupLabel/substrate
	// flags the renderer's occlusion resolver sees.
	clone := cloneSceneForEval(n)
	applyLayout(clone, canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0, clone.GridStep)
	// Keep captions at their pre-lift positions so cramped labels are still
	// diagnosed, but test face labels in the same paint order as the SVG.
	flat, _ = orderCompositeParts(flat, canvas, clone.GridStep)
	return labelOcclusionInFlat(flat, theme, nodeID)
}

// labelOcclusionInFlat queries the actual resolved text-line regions against
// filled faces in painter order. Glyph extents are estimates; positions,
// wrapping, icon displacement and font sizes share the renderer's layout.
func labelOcclusionInFlat(flat []*CompositePart, theme *Theme, nodeID string) []Issue {
	var issues []Issue
	regions := occPaintRegions(flat, theme).regions
	for i, lb := range flat {
		if lb == nil || lb.Label == "" {
			continue
		}

		if lb.groupLabel || lb.Shape == "iso_text" {
			issues = append(issues, captionOcclusionIssues(flat, i, lb, regions, theme, nodeID)...)
			continue
		}

		// Ordinary node face label. Containers/substrates carry their caption as
		// a separate group label, and screen-oriented labels paint on top — both
		// are out of scope here.
		if lb.isSubstrate || isContainerShape(lb.Shape) {
			continue
		}
		if merged := ResolveStyleWithRole(theme, lb.Shape, lb.Role, lb.Preset, lb.Style); merged != nil &&
			merged.Text != nil && merged.Text.Orient == "screen" {
			continue
		}
		text := partContentRegions(lb, theme, "text")
		if len(text) == 0 {
			continue
		}
		for j := i + 1; j < len(flat); j++ {
			q := flat[j]
			if !occludingSolid(q) || stackBase(q.ID) == stackBase(lb.ID) {
				continue // not a solid, or a replica of the label's own stack
			}
			if contentCovered(text, regions[q], captionRideThreshold) {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     fmt.Sprintf("nodes.%s", nodeID),
					Message: fmt.Sprintf("label %q on node %q is hidden by node %q in the rendered view — %q is painted in front and covers the label; raise the place gap or reorder so it isn't in front",
						lb.Label, lb.ID, q.ID, q.ID),
					Suggest: "raise the place gap or move the occluding node so it doesn't cover the label",
				})
				break
			}
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Message < issues[j].Message })
	return issues
}

// captionRideThreshold is the covered fraction of an estimated rendered
// text-line region required to report a substantial overlap. Each line is
// checked independently so hiding one line of a long block cannot disappear
// in a whole-block average. This is not a glyph legibility proof.
const captionRideThreshold = 0.25

// captionOcclusionIssues checks caption text-line regions against all filled
// bodies, including earlier ones: text lifted over a body may be legible but
// still reads as cramped. Own children and neighbours need different repairs.
func captionOcclusionIssues(flat []*CompositePart, i int, lb *CompositePart, regions map[*CompositePart]screenRegion, theme *Theme, nodeID string) []Issue {
	text := partContentRegions(lb, theme, "text")
	if len(text) == 0 {
		return nil
	}
	groupBox, hasGroup := captionGroupFootprint(flat, lb)

	// Test ALL solids, not just later-painted ones: a caption "rides on" a node
	// whenever it shares screen space with that node's painted face — even a node
	// painted BEHIND it (the renderer lifts the caption on top, so it stays
	// legible but reads as cramped). Paint order doesn't change that it's wrong.
	var inGroup, neighbour []string
	seen := map[string]bool{}
	for j := 0; j < len(flat); j++ {
		if j == i {
			continue
		}
		q := flat[j]
		if !occludingSolid(q) || seen[q.ID] {
			continue // labels/slabs/containers don't occlude; need a named solid
		}

		if !contentCovered(text, regions[q], captionRideThreshold) {
			continue
		}

		seen[q.ID] = true
		if hasGroup && qInsideFootprint(q, groupBox) {
			inGroup = append(inGroup, q.ID)
		} else {
			neighbour = append(neighbour, q.ID)
		}
	}

	var issues []Issue
	if len(inGroup) > 0 {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     fmt.Sprintf("nodes.%s", nodeID),
			Message: fmt.Sprintf("group label %q is covered by its own child node(s) %s — the front-most children sit on the caption row; the renderer lifts the caption onto their face so it reads as cramped",
				lb.Label, fmtIDList(inGroup)),
			Suggest: "raise this group's front padding (increase its layout gap) so its children clear the caption, or move the caption off the children's edge",
		})
	}
	if len(neighbour) > 0 {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     fmt.Sprintf("nodes.%s", nodeID),
			Message: fmt.Sprintf("label %q is covered by node(s) %s from an adjacent module in the rendered view — the caption reads as cramped",
				lb.Label, fmtIDList(neighbour)),
			Suggest: "increase the spacing to the adjacent group/node so it doesn't project over the caption",
		})
	}
	return issues
}

// captionGroupFootprint returns the world top-down box of the group a caption
// belongs to (the part with id == lb.labelFor), so the caption's own children
// can be told apart from neighbouring nodes.
func captionGroupFootprint(flat []*CompositePart, lb *CompositePart) (occRect, bool) {
	if lb.labelFor == "" {
		return occRect{}, false
	}
	for _, p := range flat {
		// occTopBox handles a nil offset (group at origin → box at 0,0), so we
		// must NOT require Offset != nil here, or an un-translated group would
		// fall through and mis-classify its own children as neighbours.
		if p != nil && p.ID == lb.labelFor {
			return occTopBox(p), true
		}
	}
	return occRect{}, false
}

// qInsideFootprint reports whether q belongs to the group whose footprint is box.
// It tests footprint OVERLAP, not centre-inside: a child that overflows a
// deliberately-narrow author-fixed group geom still belongs to it (its box
// straddles the slab), so the caption-ride is classified in-group — not
// misread as a neighbour occlusion "from an adjacent module".
func qInsideFootprint(q *CompositePart, box occRect) bool {
	return occOverlap(occTopBox(q), box)
}

// fmtIDList renders up to 3 ids quoted and comma-joined, with a "+N more" tail.
func fmtIDList(ids []string) string {
	show := ids
	extra := 0
	if len(show) > 3 {
		extra = len(show) - 3
		show = show[:3]
	}
	quoted := make([]string, len(show))
	for i, id := range show {
		quoted[i] = fmt.Sprintf("%q", id)
	}
	s := strings.Join(quoted, ", ")
	if extra > 0 {
		s += fmt.Sprintf(" +%d more", extra)
	}
	return s
}

// occludingSolid reports whether q is a named, opaque body that can paint over
// text. Labels, substrate slabs, iso_text and containers never occlude.
func occludingSolid(q *CompositePart) bool {
	return q != nil && !q.groupLabel && !q.isSubstrate &&
		q.Shape != "iso_text" && !isContainerShape(q.Shape) && q.ID != ""
}

// convexHull returns the convex hull of pts (Andrew's monotone chain). Order is
// consistent (no mixed winding), which is all pointInConvexPoly needs.
func convexHull(pts [][2]float64) [][2]float64 {
	n := len(pts)
	if n < 3 {
		return pts
	}
	ps := make([][2]float64, n)
	copy(ps, pts)
	sort.Slice(ps, func(i, j int) bool {
		if ps[i][0] != ps[j][0] {
			return ps[i][0] < ps[j][0]
		}
		return ps[i][1] < ps[j][1]
	})
	cross := func(o, a, b [2]float64) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}
	hull := make([][2]float64, 0, 2*n)
	for _, p := range ps { // lower chain
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := n - 2; i >= 0; i-- { // upper chain
		p := ps[i]
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	return hull[:len(hull)-1]
}

// pointInConvexPoly reports whether (x, y) is inside the convex polygon (edges
// count as inside). Works for either winding: the point must lie on the same
// side of every directed edge.
func pointInConvexPoly(x, y float64, poly [][2]float64) bool {
	if len(poly) < 3 {
		return false
	}
	var sign float64
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		cr := (b[0]-a[0])*(y-a[1]) - (b[1]-a[1])*(x-a[0])
		if cr == 0 {
			continue
		}
		if sign == 0 {
			sign = cr
		} else if (cr > 0) != (sign > 0) {
			return false
		}
	}
	return true
}

// occRect is a screen-space (or world top-down) axis-aligned box.
type occRect struct{ x0, y0, x1, y1 float64 }

func occOverlap(a, b occRect) bool {
	return a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1
}

func occDims(p *CompositePart) (w, d, h float64) {
	return partDimensions(p)
}

// occTopBox is the part's top-down (x,y) footprint.
func occTopBox(p *CompositePart) occRect {
	w, d, _ := occDims(p)
	var ox, oy float64
	if p.Offset != nil {
		ox, oy = p.Offset.WX, p.Offset.WY
	}
	return occRect{ox, oy, ox + w, oy + d}
}

// ── body-vs-body occlusion (WS-H) ────────────────────────────────────────────
//
// The master plan's last Phase-1 open item: a node whose projected silhouette
// is largely painted over by a NEARER node was invisible to every detector.
// "Nearer" is defined by the actual paint order — author painter order by
// default, canvas.depthSort order when that flag is on — because the pixel
// truth is whoever paints later covers whoever painted earlier, regardless of
// what a depth heuristic would prefer. orderCompositeParts is shared with renderComposite
// so the detected pair is the pair the SVG really draws.

// BodyOcclusion is one body-vs-body occlusion defect: Hidden's projected
// silhouette is painted over by Occluder (drawn later, i.e. in front),
// covering Frac of Hidden's silhouette area.
type BodyOcclusion struct {
	NodeID   string  `json:"node"`     // owning composite document node (e.g. "scene")
	Hidden   string  `json:"hidden"`   // the earlier-painted part being covered
	Occluder string  `json:"occluder"` // the later-painted part in front
	Frac     float64 `json:"frac"`     // covered fraction of Hidden's silhouette (0..1]
	// sep is the screen-space bounds-centre(hidden)−bounds-centre(occluder) vector — the
	// direction that separates the pair — kept for the repair actuator.
	sep [2]float64
}

// bodyOcclusionThreshold retains the existing 0.4 reporting threshold. The
// new measurement uses filled regions rather than box hulls; thresholds are
// not retuned to hide newly detected defects. Coverage remains a lower bound
// for unsupported materials, not a full alpha-compositing visibility metric.
const bodyOcclusionThreshold = 0.4

// BodyOcclusions detects body-vs-body occlusion across the document. It is an
// iso-only, height-induced signal: under projection: top the silhouette IS the
// footprint, and footprint collisions are already the overlap metric's job, so
// top-projection documents report none.
func BodyOcclusions(doc *Document) []BodyOcclusion {
	if doc == nil {
		return nil
	}
	if doc.Canvas != nil && doc.Canvas.Projection == "top" {
		return nil
	}
	ids := make([]string, 0, len(doc.Nodes))
	for id := range doc.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids) // stable output regardless of map iteration
	var out []BodyOcclusion
	for _, id := range ids {
		n := doc.Nodes[id]
		if n == nil || n.Shape != "composite" {
			continue
		}
		occs := bodyOcclusionsInNode(n, doc.Theme, doc.Canvas, bodyOcclusionThreshold)
		for i := range occs {
			occs[i].NodeID = id
		}
		out = append(out, occs...)
	}
	return out
}

// bodyOcclusionsInNode solves + lowers a CLONE (detection must never mutate
// the doc), replays the renderer's paint order, and runs the pair kernel.
// minFrac lets the repair loop measure sub-threshold progress.
func bodyOcclusionsInNode(n *Node, theme *Theme, canvas *Canvas, minFrac float64) []BodyOcclusion {
	if n == nil {
		return nil
	}
	clone := cloneSceneForEval(n)
	applyLayout(clone, canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0, clone.GridStep)
	flat, _ = orderCompositeParts(flat, canvas, clone.GridStep)
	return bodyOcclusionsInFlat(flat, theme, minFrac)
}

// bodyOcclusionsInFlat is the pair kernel over paint-ordered parts. For each
// ordered pair (p earlier, q later = in front) of named opaque bodies whose
// top-down FOOTPRINTS are disjoint — footprint contact is either the overlap
// metric's defect or a deliberate Z-stack, never this signal — it intersects their
// filled polygon unions and reports the covered area fraction. Unknown paint
// coverage is excluded rather than promoted to an opaque semantic envelope.
func bodyOcclusionsInFlat(flat []*CompositePart, theme *Theme, minFrac float64) []BodyOcclusion {
	var out []BodyOcclusion
	regions := occPaintRegions(flat, theme).regions
	type prepared struct {
		part              *CompositePart
		region            screenRegion
		bounds, footprint occRect
		area              float64
		base              string
	}
	var bodies []prepared
	for _, p := range flat {
		if !occludingSolid(p) || len(regions[p]) == 0 {
			continue
		}
		r := regions[p]
		area := regionArea(r)
		if area > 0 {
			bodies = append(bodies, prepared{p, r, regionBounds(r), occTopBox(p), area, stackBase(p.ID)})
		}
	}
	for i, p := range bodies {
		for _, q := range bodies[i+1:] {
			if p.base == q.base || !occOverlap(p.bounds, q.bounds) {
				continue
			}
			if occOverlap(p.footprint, q.footprint) {
				continue // footprint contact belongs to overlap / deliberate stack logic
			}
			frac := math.Min(1, regionIntersectionArea(p.region, q.region)/p.area)
			if frac < minFrac {
				continue
			}
			bp, bq := p.bounds, q.bounds
			out = append(out, BodyOcclusion{
				Hidden: p.part.ID, Occluder: q.part.ID, Frac: frac,
				sep: [2]float64{(bp.x0 + bp.x1 - bq.x0 - bq.x1) / 2, (bp.y0 + bp.y1 - bq.y0 - bq.y1) / 2},
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Hidden != out[j].Hidden {
			return out[i].Hidden < out[j].Hidden
		}
		return out[i].Occluder < out[j].Occluder
	})
	return out
}

// resolveGroupLabelOcclusion uses later filled faces and resolved text regions;
// unresolved coverage retains the legacy protective fallback. Call after painter-order resolution so
// depth sorting cannot undo the decision. A small overlap warrants protecting
// text even when it is below the stronger cramped-layout warning threshold.
func resolveGroupLabelOcclusion(flat []*CompositePart, themes ...*Theme) []*CompositePart {
	var theme *Theme
	if len(themes) > 0 {
		theme = themes[0]
	}
	hasLabel := false
	for _, p := range flat {
		if p != nil && p.groupLabel {
			hasLabel = true
			break
		}
	}
	if !hasLabel {
		return flat
	}

	paint := occPaintRegions(flat, theme)
	occluded := make([]bool, len(flat))
	for i, lb := range flat {
		if lb == nil || !lb.groupLabel {
			continue
		}
		text := partContentRegions(lb, theme, "text")
		// Only LATER-painted parts can cover the label; substrates/slabs sit
		// below it and other labels never occlude.
		for j := i + 1; j < len(flat); j++ {
			q := flat[j]
			if q == nil || q.groupLabel || q.isSubstrate || q.Shape == "iso_text" || isContainerShape(q.Shape) {
				continue
			}
			if contentCovered(text, paint.regions[q], 1e-6) || (paint.unresolved[q] && len(text) > 0 && captionLiftFallback(lb, q)) {
				occluded[i] = true
				break
			}
		}
	}

	out := make([]*CompositePart, 0, len(flat))
	var lifted []*CompositePart
	for i, p := range flat {
		if occluded[i] {
			lifted = append(lifted, p)
		} else {
			out = append(out, p)
		}
	}
	return append(out, lifted...)
}
