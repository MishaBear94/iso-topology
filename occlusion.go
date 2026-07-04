package isotopo

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
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
	if doc == nil {
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
	clone := &Node{Shape: n.Shape, GridStep: n.GridStep, Parts: cloneParts(n.Parts)}
	if n.Layout != nil {
		l := *n.Layout
		clone.Layout = &l
	}
	applyLayout(clone, canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0)
	return labelOcclusionInFlat(flat, theme, nodeID)
}

// labelOcclusionInFlat is the paint-order detection over already-lowered parts.
// Captions (group label / iso_text) use the tilted iso-text box and the same
// screen-OR-top cover test the auto-lift resolver uses. Ordinary node face
// labels sit centred on the node's top face, so they are flagged when a later-
// painted solid covers that centre point in the rendered (iso) view; screen-
// oriented labels are painted on top and never occluded, so they are skipped.
func labelOcclusionInFlat(flat []*CompositePart, theme *Theme, nodeID string) []Issue {
	var issues []Issue
	for i, lb := range flat {
		if lb == nil || lb.Label == "" {
			continue
		}

		if lb.groupLabel || lb.Shape == "iso_text" {
			issues = append(issues, captionOcclusionIssues(flat, i, lb, theme, nodeID)...)
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
		cx, cy, ok := occTopFaceLabelCenter(lb)
		if !ok {
			continue
		}
		for j := i + 1; j < len(flat); j++ {
			q := flat[j]
			if !occludingSolid(q) || stackBase(q.ID) == stackBase(lb.ID) {
				continue // not a solid, or a replica of the label's own stack
			}
			if !occluderHidesText(theme, q) {
				continue // wireframe / see-through "ghost" volume paints nothing
			}
			// Conservative: only warn when the occluder actually paints over the
			// spot where the label text sits (the top-face centre). The test is
			// against q's true iso silhouette (convex hull of its projected
			// corners), NOT its bounding box — the box's empty triangular
			// corners would otherwise count a node as covering a label it never
			// paints over (iso layouts overlap bounding boxes routinely while the
			// label stays visible).
			if pointInIsoSilhouette(cx, cy, q) {
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

// captionRideThreshold is the minimum fraction of a caption's glyph-band sample
// points a node must cover before it counts as a "ride". Conservative: a node
// merely grazing the caption's edge (calibrated ≈0.07–0.21 across the sample
// corpus) is not flagged; a substantial ride (≈0.29–0.64) is.
const captionRideThreshold = 0.25

// captionOcclusionIssues detects a group caption / iso_text title being painted
// over by an opaque node in the ISO projection. It samples the caption's glyph
// band and tests each point against every other node's true screen silhouette
// (occNodeHull — which folds the node's height in, so a tall front face rising
// over a flat caption is caught). It checks ALL nodes (not just later-painted
// ones: a node BEHIND the caption still shares its screen space, and the
// renderer lifts the caption onto the node's face — legible but cramped),
// flags only a substantial ride (captionRideThreshold), reports EVERY real
// occluder, and splits them into the caption's own in-group children vs
// neighbouring nodes because the two need different fixes.
func captionOcclusionIssues(flat []*CompositePart, i int, lb *CompositePart, theme *Theme, nodeID string) []Issue {
	pts := occLabelSamplePts(lb, 14, 4)
	if len(pts) == 0 {
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
		if !occluderHidesText(theme, q) {
			continue // see-through ghost / wireframe paints nothing
		}
		hull, ok := occNodeHull(q)
		if !ok {
			continue
		}
		cov := 0
		for _, pt := range pts {
			if pointInConvexPoly(pt[0], pt[1], hull) {
				cov++
			}
		}
		frac := float64(cov) / float64(len(pts))
		if frac < captionRideThreshold {
			continue // only a graze — not a substantial ride
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

// occLabelSamplePts samples a screen-space grid over a caption's whole glyph
// band (anchor → +x for the text width, oz → oz+size in height), matching how
// occLabelBoxes models the iso-text. Sampling the full band — not just the
// baseline — is what catches a ride that touches only the top of the glyphs
// (e.g. a node face rising over the caption), and locates partial rides.
func occLabelSamplePts(p *CompositePart, nx, nz int) [][2]float64 {
	if nx < 2 {
		nx = 2
	}
	if nz < 2 {
		nz = 2
	}
	if p.Label == "" {
		return nil
	}
	size := 11.0
	if p.Style != nil && p.Style.Text != nil && p.Style.Text.Size != nil && *p.Style.Text.Size > 0 {
		size = *p.Style.Text.Size
	}
	tw := textWidthPx(p.Label, size, textmetrics.NarrowPartLabel)
	var ox, oy, oz float64
	if p.Offset != nil {
		ox, oy, oz = p.Offset.WX, p.Offset.WY, p.Offset.WZ
	}
	pts := make([][2]float64, 0, nx*nz)
	for a := 0; a < nx; a++ {
		x := ox + tw*float64(a)/float64(nx-1)
		for b := 0; b < nz; b++ {
			z := oz + size*float64(b)/float64(nz-1)
			sx, sy := projectIso(x, oy, z)
			pts = append(pts, [2]float64{sx, sy})
		}
	}
	return pts
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

// occluderHidesText reports whether q actually paints opaque pixels over text
// beneath it. Wireframe, low-opacity, and no-top-fill "ghost" volumes (e.g. a
// dashed budget-ceiling box) are see-through and hide nothing, so they must not
// be flagged as occluders.
func occluderHidesText(theme *Theme, q *CompositePart) bool {
	merged := ResolveStyleWithRole(theme, q.Shape, q.Role, q.Preset, q.Style)
	if merged == nil {
		return true // default style is an opaque box
	}
	if e := merged.Effects; e != nil {
		if e.Wireframe != nil && *e.Wireframe {
			return false
		}
		if e.Opacity != nil && *e.Opacity < 0.5 {
			return false
		}
	}
	// A part whose top face has no visible fill (and no top gradient or per-face
	// override) is see-through from above and cannot hide a label beneath it.
	if p := merged.Palette; p != nil {
		hasTopFill := isVisibleFill(p.Top) || p.TopGradient != nil
		if _, ok := merged.Faces["top"]; ok {
			hasTopFill = true
		}
		if !hasTopFill {
			return false
		}
	}
	return true
}

func isVisibleFill(c string) bool {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "", "none", "transparent":
		return false
	}
	return true
}

// occTopFaceLabelCenter projects the centre of a part's TOP face — where the
// default top-face label is drawn — to screen space.
func occTopFaceLabelCenter(p *CompositePart) (float64, float64, bool) {
	if p.Offset == nil {
		return 0, 0, false
	}
	w, d, h := occDims(p)
	cx, cy := projectIso(p.Offset.WX+w/2, p.Offset.WY+d/2, p.Offset.WZ+h)
	return cx, cy, true
}

// occNodeHull returns q's painted screen silhouette — the convex hull of its 8
// projected corners. For an opaque box every interior point of that hull is
// painted by a visible face (and the z+h corners fold the node's HEIGHT in, so
// a tall front face that rises over a flat caption is captured), with none of
// the bounding box's empty-corner over-counting.
func occNodeHull(q *CompositePart) ([][2]float64, bool) {
	if q.Offset == nil {
		return nil, false
	}
	w, d, h := occDims(q)
	ox, oy, oz := q.Offset.WX, q.Offset.WY, q.Offset.WZ
	corners := make([][2]float64, 0, 8)
	for _, c := range [8][3]float64{
		{ox, oy, oz}, {ox + w, oy, oz}, {ox + w, oy + d, oz}, {ox, oy + d, oz},
		{ox, oy, oz + h}, {ox + w, oy, oz + h}, {ox + w, oy + d, oz + h}, {ox, oy + d, oz + h},
	} {
		sx, sy := projectIso(c[0], c[1], c[2])
		corners = append(corners, [2]float64{sx, sy})
	}
	return convexHull(corners), true
}

// pointInIsoSilhouette reports whether (x, y) lies within q's painted silhouette.
func pointInIsoSilhouette(x, y float64, q *CompositePart) bool {
	hull, ok := occNodeHull(q)
	if !ok {
		return false
	}
	return pointInConvexPoly(x, y, hull)
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
	w, d, h = 140, 140, 80
	if p.Geom != nil {
		if p.Geom.W > 0 {
			w = p.Geom.W
		}
		if p.Geom.D > 0 {
			d = p.Geom.D
		}
		if p.Geom.H > 0 {
			h = p.Geom.H
		}
	}
	return
}

// occScreenBox projects a part's 8 world corners to its screen bounding box.
func occScreenBox(p *CompositePart) (occRect, bool) {
	if p.Offset == nil {
		return occRect{}, false
	}
	w, d, h := occDims(p)
	ox, oy, oz := p.Offset.WX, p.Offset.WY, p.Offset.WZ
	r := occRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range [8][3]float64{
		{ox, oy, oz}, {ox + w, oy, oz}, {ox + w, oy + d, oz}, {ox, oy + d, oz},
		{ox, oy, oz + h}, {ox + w, oy, oz + h}, {ox + w, oy + d, oz + h}, {ox, oy + d, oz + h},
	} {
		sx, sy := projectIso(c[0], c[1], c[2])
		r.x0, r.x1 = math.Min(r.x0, sx), math.Max(r.x1, sx)
		r.y0, r.y1 = math.Min(r.y0, sy), math.Max(r.y1, sy)
	}
	return r, true
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

// occLabelBoxes returns a group label's screen box and top-down footprint. The
// label is iso-text tilted along world +x from its anchor, so the box spans
// roughly its text width in world x and its font size in z.
func occLabelBoxes(p *CompositePart) (occRect, occRect) {
	size := 11.0
	if p.Style != nil && p.Style.Text != nil && p.Style.Text.Size != nil && *p.Style.Text.Size > 0 {
		size = *p.Style.Text.Size
	}
	tw := textWidthPx(p.Label, size, textmetrics.NarrowPartLabel)
	var ox, oy, oz float64
	if p.Offset != nil {
		ox, oy, oz = p.Offset.WX, p.Offset.WY, p.Offset.WZ
	}
	scr := occRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range [4][3]float64{
		{ox, oy, oz}, {ox + tw, oy, oz}, {ox, oy, oz + size}, {ox + tw, oy, oz + size},
	} {
		sx, sy := projectIso(c[0], c[1], c[2])
		scr.x0, scr.x1 = math.Min(scr.x0, sx), math.Max(scr.x1, sx)
		scr.y0, scr.y1 = math.Min(scr.y0, sy), math.Max(scr.y1, sy)
	}
	return scr, occRect{ox, oy, ox + tw, oy + size}
}

// ── body-vs-body occlusion (WS-H) ────────────────────────────────────────────
//
// The master plan's last Phase-1 open item: a node whose projected silhouette
// is largely painted over by a NEARER node was invisible to every detector.
// "Nearer" is defined by the actual paint order — author painter order by
// default, canvas.depthSort order when that flag is on — because the pixel
// truth is whoever paints later covers whoever painted earlier, regardless of
// what a depth heuristic would prefer. occPaintOrder mirrors renderComposite's
// ordering exactly so the detected pair is the pair the SVG really draws.

// BodyOcclusion is one body-vs-body occlusion defect: Hidden's projected
// silhouette is painted over by Occluder (drawn later, i.e. in front),
// covering Frac of Hidden's silhouette area.
type BodyOcclusion struct {
	NodeID   string  `json:"node"`     // owning composite document node (e.g. "scene")
	Hidden   string  `json:"hidden"`   // the earlier-painted part being covered
	Occluder string  `json:"occluder"` // the later-painted part in front
	Frac     float64 `json:"frac"`     // covered fraction of Hidden's silhouette (0..1]
	// sep is the screen-space centroid(hidden)−centroid(occluder) vector — the
	// direction that separates the pair — kept for the repair actuator.
	sep [2]float64
}

// bodyOcclusionThreshold is the minimum covered-silhouette fraction before a
// pair counts as a defect. Calibrated conservatively: ordinary iso layouts
// graze each other's hulls constantly — the corpus sweep measured every
// shipped bench/node/topology sample at ≤ 0.16 (max: training-compute 0.158)
// — so 0.4 flags only a node that is substantially swallowed while leaving
// the whole existing corpus at zero (no new warnings, R unchanged, repair
// never armed).
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
	clone := &Node{Shape: n.Shape, GridStep: n.GridStep, Parts: cloneParts(n.Parts)}
	if n.Layout != nil {
		l := *n.Layout
		clone.Layout = &l
	}
	applyLayout(clone, canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0)
	flat = occPaintOrder(flat, canvas, clone.GridStep)
	return bodyOcclusionsInFlat(flat, theme, minFrac)
}

// occPaintOrder replays renderComposite's painter ordering on lowered parts:
// substrates first, then thin "platform" parts other parts stand on, then the
// bodies — and, when canvas.depthSort is on, the same depthSortParts re-sort.
// It MUST stay in lockstep with renderComposite (render.go); the paint-order
// consistency tests in occlusion_body_test.go pin the two together. The parts
// come from a clone, so marking platforms isSubstrate here is safe.
func occPaintOrder(flat []*CompositePart, canvas *Canvas, gridStep float64) []*CompositePart {
	dims := func(p *CompositePart) (x, y, z, w, d, h float64) {
		w, d, h = 140, 140, 80
		if p.Geom != nil {
			if p.Geom.W > 0 {
				w = p.Geom.W
			}
			if p.Geom.D > 0 {
				d = p.Geom.D
			}
			if p.Geom.H > 0 {
				h = p.Geom.H
			}
		}
		if p.Offset != nil {
			x, y, z = p.Offset.WX, p.Offset.WY, p.Offset.WZ
		}
		return
	}
	isPlatform := make([]bool, len(flat))
	for i, p := range flat {
		if p.isSubstrate {
			continue
		}
		px, py, pz, pw, pd, ph := dims(p)
		if ph > 24 {
			continue
		}
		for j, q := range flat {
			if i == j || q.isSubstrate {
				continue
			}
			qx, qy, qz, qw, qd, _ := dims(q)
			if math.Abs(qz-(pz+ph)) > 0.5 {
				continue
			}
			cx, cy := qx+qw/2, qy+qd/2
			if cx > px && cx < px+pw && cy > py && cy < py+pd {
				isPlatform[i] = true
				break
			}
		}
	}
	ordered := make([]*CompositePart, 0, len(flat))
	for _, p := range flat {
		if p.isSubstrate {
			ordered = append(ordered, p)
		}
	}
	for i, p := range flat {
		if !p.isSubstrate && isPlatform[i] {
			p.isSubstrate = true // downstream treats it like a slab, exactly as render.go does
			ordered = append(ordered, p)
		}
	}
	nSubstrates := len(ordered)
	for _, p := range flat {
		if !p.isSubstrate {
			ordered = append(ordered, p)
		}
	}
	if canvas != nil && canvas.DepthSort {
		depthSortParts(ordered, nSubstrates, gridStep)
	}
	return ordered
}

// bodyOcclusionsInFlat is the pair kernel over paint-ordered parts. For each
// ordered pair (p earlier, q later = in front) of named opaque bodies whose
// top-down FOOTPRINTS are disjoint — footprint contact is either the overlap
// metric's defect or a deliberate Z-stack, never this signal — it clips p's
// projected silhouette hull by q's and reports the covered area fraction.
func bodyOcclusionsInFlat(flat []*CompositePart, theme *Theme, minFrac float64) []BodyOcclusion {
	var out []BodyOcclusion
	for i := 0; i < len(flat); i++ {
		p := flat[i]
		if !occludingSolid(p) {
			continue // labels / slabs / platforms / containers aren't bodies
		}
		hullP, ok := occNodeHull(p)
		if !ok {
			continue
		}
		areaP := convexPolyArea(hullP)
		if areaP <= 0 {
			continue
		}
		for j := i + 1; j < len(flat); j++ {
			q := flat[j]
			if !occludingSolid(q) || stackBase(q.ID) == stackBase(p.ID) {
				continue // not a body, or a replica of p's own stack
			}
			if !occluderHidesText(theme, q) {
				continue // wireframe / see-through ghost paints nothing over p
			}
			if occOverlap(occTopBox(p), occTopBox(q)) {
				continue // footprint contact: overlap metric / deliberate stack territory
			}
			hullQ, ok := occNodeHull(q)
			if !ok {
				continue
			}
			frac := convexPolyArea(convexClip(hullP, hullQ)) / areaP
			if frac < minFrac {
				continue
			}
			cpx, cpy := polyCentroid(hullP)
			cqx, cqy := polyCentroid(hullQ)
			out = append(out, BodyOcclusion{
				Hidden:   p.ID,
				Occluder: q.ID,
				Frac:     frac,
				sep:      [2]float64{cpx - cqx, cpy - cqy},
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

// convexPolyArea is the (unsigned) shoelace area of a polygon.
func convexPolyArea(poly [][2]float64) float64 {
	return math.Abs(signedPolyArea2(poly)) / 2
}

// signedPolyArea2 is twice the signed shoelace area (positive = CCW in the
// math sense, i.e. positive-cross winding).
func signedPolyArea2(poly [][2]float64) float64 {
	s := 0.0
	for i := range poly {
		a, b := poly[i], poly[(i+1)%len(poly)]
		s += a[0]*b[1] - b[0]*a[1]
	}
	return s
}

// polyCentroid is the vertex centroid — enough precision for a push direction.
func polyCentroid(poly [][2]float64) (float64, float64) {
	if len(poly) == 0 {
		return 0, 0
	}
	var sx, sy float64
	for _, p := range poly {
		sx += p[0]
		sy += p[1]
	}
	return sx / float64(len(poly)), sy / float64(len(poly))
}

// convexClip clips subject to the inside of the CONVEX polygon clip
// (Sutherland–Hodgman). Both polygons come from convexHull, so the convexity
// precondition holds; winding is normalized here. Fully deterministic.
func convexClip(subject, clip [][2]float64) [][2]float64 {
	if len(subject) < 3 || len(clip) < 3 {
		return nil
	}
	if signedPolyArea2(clip) < 0 { // normalize to positive winding: interior = left of each edge
		rev := make([][2]float64, len(clip))
		for i := range clip {
			rev[i] = clip[len(clip)-1-i]
		}
		clip = rev
	}
	out := subject
	for i := range clip {
		a, b := clip[i], clip[(i+1)%len(clip)]
		in := out
		out = nil
		if len(in) == 0 {
			return nil
		}
		side := func(p [2]float64) float64 {
			return (b[0]-a[0])*(p[1]-a[1]) - (b[1]-a[1])*(p[0]-a[0])
		}
		for k := range in {
			cur, nxt := in[k], in[(k+1)%len(in)]
			sc, sn := side(cur), side(nxt)
			if sc >= 0 {
				out = append(out, cur)
			}
			if (sc > 0 && sn < 0) || (sc < 0 && sn > 0) {
				t := sc / (sc - sn)
				out = append(out, [2]float64{cur[0] + t*(nxt[0]-cur[0]), cur[1] + t*(nxt[1]-cur[1])})
			}
		}
	}
	return out
}

// resolveGroupLabelOcclusion is the missing auto-layout constraint: a group's
// label must not be covered by an upper-layer node. It judges occlusion in BOTH
// the 2.5D screen projection (a taller node BEHIND the label in the top-down
// plan can still cover it once projected) and the top-down footprint, then
// repaints any occluded label LAST so it sits on top. Labels that are already
// clear keep their position, so the common case is unchanged.
func resolveGroupLabelOcclusion(flat []*CompositePart) []*CompositePart {
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

	occluded := make([]bool, len(flat))
	for i, lb := range flat {
		if lb == nil || !lb.groupLabel {
			continue
		}
		lbScr, lbTop := occLabelBoxes(lb)
		// Only LATER-painted parts can cover the label; substrates/slabs sit
		// below it and other labels never occlude.
		for j := i + 1; j < len(flat); j++ {
			q := flat[j]
			if q == nil || q.groupLabel || q.isSubstrate || q.Shape == "iso_text" || isContainerShape(q.Shape) {
				continue
			}
			qScr, ok := occScreenBox(q)
			if !ok {
				continue
			}
			// 2.5D cover OR top-down cover (the user asked for both views).
			if occOverlap(lbScr, qScr) || occOverlap(lbTop, occTopBox(q)) {
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
