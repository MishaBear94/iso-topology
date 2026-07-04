// Layout solving — turn declarative `layout` (container arrangement)
// and `place` (sibling relations) into concrete part Offsets, so
// authors and agents never hand-compute world coordinates.
//
// The pass runs BEFORE lowering (lower.go) and writes its results into
// the same Offset field the lowering pass already consumes, so the
// rest of the pipeline is untouched. It is deterministic (topological
// solve, no iteration, no randomness) and idempotent: solved parts get
// their Place/Layout cleared, so a second application is a no-op.
//
// All lengths in layout/place are in CELLS. One cell = node.gridStep,
// else canvas.gridStep, else 40 world units — the same lattice as the
// canvas grid texture and the orthogonal connector channels, so solved
// arrangements stay in register with both.
package isotopo

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

const defaultCellSize = 40.0

// Default footprints, mirroring render.go's fallback geometry.
const (
	defaultPartW  = 140.0
	defaultPartD  = 140.0
	defaultPartH  = 80.0
	defaultGroupW = 360.0
	defaultGroupD = 240.0
)

func layoutCell(n *Node, canvas *Canvas) float64 {
	if n != nil && n.GridStep > 0 {
		return n.GridStep
	}
	if canvas != nil && canvas.GridStep > 0 {
		return canvas.GridStep
	}
	return defaultCellSize
}

// applyLayout solves every layout/place declaration in a composite
// node, mutating part Offsets in place. Returned issues are advisory
// for render callers (Validate surfaces the same issues to agents).
func applyLayout(n *Node, canvas *Canvas) []Issue {
	if n == nil || n.Shape != "composite" {
		return nil
	}
	cell := layoutCell(n, canvas)
	var issues []Issue
	// v4.2 (M6) — connector-driven auto-layout at the root: the engine
	// positions every part from the connector graph (no place/coords),
	// while per-part style (faces / prisms / effects) stays authored.
	// Child containers are solved depth-first first so their footprints
	// are final before the auto pass arranges them.
	if n.Layout != nil && n.Layout.Mode == "auto" {
		for i, p := range n.Parts {
			if p != nil && isContainerShape(p.Shape) && len(p.Parts) > 0 {
				solveContainer(p.Parts, p.Layout, p, cell, fmt.Sprintf("nodes.scene.parts[%d]", i), &issues)
				p.Layout = nil
			}
		}
		// v5 — opt-in engine dispatch: layout: {mode: auto, engine:
		// elk|dagre} delegates the whole arrangement to d2's layered
		// engines (layout_elk.go). On any failure it returns false
		// (with a Warning already appended) and the built-in tiers
		// below run exactly as before; engine == "" never enters here,
		// so the default path stays byte-identical.
		if n.Layout.Engine != "" {
			if arrangeEngineAuto(n.Parts, n.Connectors, n.Layout, cell, "nodes.scene", &issues) {
				checkSiblingOverlaps(n.Parts, "nodes.scene", &issues)
				n.Layout = nil
				return issues
			}
		}
		// Graph-class adaptive, in three tiers:
		//   - pure DAG                    → layered (Sugiyama-lite) as-is;
		//   - mostly-DAG (back edges are  → break the cycles: reverse just the
		//     a minority, fraction ≤ 1/3)   feedback arcs FOR RANKING and keep
		//                                   the layered left-to-right narrative
		//                                   (a pipeline with one retry edge is
		//                                   a flow, not a mesh);
		//   - dense/cyclic (fraction>1/3) → force-directed placer, which spreads
		//                                   meshes to near-uniform distance so
		//                                   edges stop tunnelling.
		back := feedbackEdges(n.Parts, n.Connectors)
		switch {
		case len(back) == 0:
			arrangeAuto(n.Parts, n.Connectors, n.Layout, cell, "nodes.scene", &issues)
		case hasReciprocalBackEdge(n.Connectors, back):
			// A<->B pairs are bidirectional traffic (hub-and-spoke), not
			// pipeline feedback — radial force spread reads best.
			arrangeForce(n.Parts, n.Connectors, n.Layout, cell)
		default:
			// Long-cycle feedback in a flow: break the cycles (reverse the
			// feedback arcs for RANKING only) and keep the layered
			// left-to-right narrative. Judge the trial with the router's own
			// capability — straight line-of-sight for flow edges, straight-or-
			// elbow for the feedback arcs; if any edge has no clear route the
			// graph is too dense for ranks, so roll back and spread instead.
			saved := snapshotOffsets(n.Parts)
			arrangeAuto(n.Parts, reversedForRanking(n.Connectors, back), n.Layout, cell, "nodes.scene", &issues)
			if trialTunnels(n.Parts, n.Connectors, back) > 0 {
				restoreOffsets(n.Parts, saved)
				arrangeForce(n.Parts, n.Connectors, n.Layout, cell)
			}
		}
		checkSiblingOverlaps(n.Parts, "nodes.scene", &issues)
		n.Layout = nil
		return issues
	}
	solveContainer(n.Parts, n.Layout, nil, cell, "nodes.scene", &issues)
	n.Layout = nil
	return issues
}

// arrangeAuto positions parts by layering the connector graph
// (Sugiyama-lite): longest-path rank from sources becomes the world +x
// column; parts within a rank stack along world +y and are centred so
// the flow is balanced. Shared ranks keep connected parts on aligned
// grid tracks, so orthogonal routes stay axis-flush. Parts touched by
// no connector trail into rank 0 with the sources. Deterministic: ties
// break by declared order; positions snap to the cell lattice.
func arrangeAuto(parts []*CompositePart, conns []*Connector, lay *Layout, cell float64, path string, issues *[]Issue) {
	idx := map[string]int{}
	order := []string{}
	for _, p := range parts {
		if p == nil || p.ID == "" {
			continue
		}
		if _, dup := idx[p.ID]; !dup {
			idx[p.ID] = len(order)
			order = append(order, p.ID)
		}
	}
	if len(order) == 0 {
		return
	}

	// v4.4 — pins: a part that already carries an explicit offset (an
	// author coordinate, or one written by a Studio drag) is honoured
	// as-is. It still ranks in the graph so neighbours flow around it,
	// but the auto pass never overwrites its position. This is what
	// makes "drag a node in auto-layout and it stays put" work.
	pinned := map[string]bool{}
	for _, id := range order {
		if parts[idx[id]].Offset != nil {
			pinned[id] = true
		}
	}

	target := func(ref string) string {
		if d := strings.IndexByte(ref, '.'); d >= 0 {
			return ref[:d]
		}
		return ref
	}
	indeg := map[string]int{}
	adj := map[string][]string{}
	for _, c := range conns {
		u, v := target(c.From), target(c.To)
		if _, ok := idx[u]; !ok {
			continue
		}
		if _, ok := idx[v]; !ok {
			continue
		}
		if u == v {
			continue
		}
		adj[u] = append(adj[u], v)
		indeg[v]++
	}

	rank := map[string]int{}
	deg := map[string]int{}
	queue := []string{}
	for _, id := range order {
		rank[id] = 0
		deg[id] = indeg[id]
		if deg[id] == 0 {
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, v := range adj[u] {
			if rank[u]+1 > rank[v] {
				rank[v] = rank[u] + 1
			}
			deg[v]--
			if deg[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	maxRank := 0
	for _, r := range rank {
		if r > maxRank {
			maxRank = r
		}
	}
	ranks := make([][]string, maxRank+1)
	for _, id := range order {
		ranks[rank[id]] = append(ranks[rank[id]], id)
	}

	gap := 2.0
	if lay != nil && lay.Gap != nil && *lay.Gap >= 0 {
		gap = *lay.Gap
	}
	gapW := gap * cell

	fw := map[string]float64{}
	fd := map[string]float64{}
	fh := map[string]float64{}
	for _, id := range order {
		w, d := partFootprint(parts[idx[id]])
		fw[id], fd[id] = w, d
		fh[id] = partHeight(parts[idx[id]])
	}

	// World +x projects DOWN-right on screen and a part's extrusion
	// projects UP, so two parts one rank apart visually collide unless
	// the column advance clears the taller part's height too. Advance =
	// widest part in the rank + gap + a height-clearance term, so big or
	// tall nodes (hexprism gateways, stacked decks) never crowd the next
	// column and connectors keep clear air to run through.
	rankX := make([]float64, len(ranks))
	x := 0.0
	for r := range ranks {
		rankX[r] = x
		maxW, maxH := 0.0, 0.0
		for _, id := range ranks[r] {
			if fw[id] > maxW {
				maxW = fw[id]
			}
			if fh[id] > maxH {
				maxH = fh[id]
			}
		}
		x += maxW + gapW + maxH*0.6
	}

	// Undirected neighbour map for crossing reduction + median alignment.
	nbr := map[string][]string{}
	for u, vs := range adj {
		for _, v := range vs {
			nbr[u] = append(nbr[u], v)
			nbr[v] = append(nbr[v], u)
		}
	}

	// y-CENTRE of each part, packed within its rank in current order.
	cy := map[string]float64{}
	maxExtent := 0.0
	for r := range ranks {
		ext := 0.0
		for i, id := range ranks[r] {
			if i > 0 {
				ext += gapW
			}
			ext += fd[id]
		}
		if ext > maxExtent {
			maxExtent = ext
		}
	}
	packRank := func(r int) {
		y := (maxExtent - rankSpan(ranks[r], fd, gapW)) / 2
		for _, id := range ranks[r] {
			cy[id] = y + fd[id]/2
			y += fd[id] + gapW
		}
	}
	for r := range ranks {
		packRank(r)
	}

	// Crossing reduction: order each rank by the barycentre of its
	// neighbours' y in the adjacent rank, alternating sweep direction.
	for sweep := 0; sweep < 4; sweep++ {
		down := sweep%2 == 0
		seq := make([]int, len(ranks))
		for i := range seq {
			if down {
				seq[i] = i
			} else {
				seq[i] = len(ranks) - 1 - i
			}
		}
		for _, r := range seq {
			bc := map[string]float64{}
			for _, id := range ranks[r] {
				sum, cnt := 0.0, 0
				for _, m := range nbr[id] {
					if _, ok := cy[m]; ok {
						sum += cy[m]
						cnt++
					}
				}
				if cnt > 0 {
					bc[id] = sum / float64(cnt)
				} else {
					bc[id] = cy[id]
				}
			}
			sortByKeyStable(ranks[r], bc)
			packRank(r)
		}
	}

	// Median alignment: pull each node toward the median y of its
	// neighbours, then resolve overlaps within the rank in order. A few
	// down+up passes straighten chains so most edges collapse to a
	// single on-axis segment (the router merges collinear endpoints).
	for pass := 0; pass < 6; pass++ {
		down := pass%2 == 0
		seq := make([]int, len(ranks))
		for i := range seq {
			if down {
				seq[i] = i
			} else {
				seq[i] = len(ranks) - 1 - i
			}
		}
		for _, r := range seq {
			want := make([]float64, len(ranks[r]))
			for i, id := range ranks[r] {
				ys := []float64{}
				for _, m := range nbr[id] {
					if y, ok := cy[m]; ok {
						ys = append(ys, y)
					}
				}
				if len(ys) > 0 {
					want[i] = medianOf(ys)
				} else {
					want[i] = cy[id]
				}
			}
			resolveRank(ranks[r], want, fd, gapW, cy)
		}
	}

	snap := func(v float64) float64 { return math.Round(v/cell) * cell }
	for r := range ranks {
		maxW := 0.0
		for _, id := range ranks[r] {
			if fw[id] > maxW {
				maxW = fw[id]
			}
		}
		for _, id := range ranks[r] {
			if pinned[id] {
				continue // honour the explicit/dragged offset
			}
			p := parts[idx[id]]
			if p.Offset == nil {
				p.Offset = &WorldPoint{}
			}
			p.Offset.WX = snap(rankX[r] + (maxW-fw[id])/2)
			p.Offset.WY = snap(cy[id] - fd[id]/2)
		}
	}
}

func rankSpan(ids []string, fd map[string]float64, gapW float64) float64 {
	ext := 0.0
	for i, id := range ids {
		if i > 0 {
			ext += gapW
		}
		ext += fd[id]
	}
	return ext
}

// sortByKeyStable stable-sorts ids ascending by key[id].
func sortByKeyStable(ids []string, key map[string]float64) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && key[ids[j]] < key[ids[j-1]]-1e-9; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func medianOf(v []float64) float64 {
	s := append([]float64(nil), v...)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// resolveRank sets each node's centre y to its desired value, then
// sweeps to enforce the minimum centre-to-centre spacing (half-depths +
// gap) in BOTH directions so the rank neither overlaps nor drifts.
func resolveRank(ids []string, want []float64, fd map[string]float64, gapW float64, cy map[string]float64) {
	for i, id := range ids {
		cy[id] = want[i]
	}
	// forward: push down to clear overlaps
	for i := 1; i < len(ids); i++ {
		minGap := fd[ids[i-1]]/2 + fd[ids[i]]/2 + gapW
		if cy[ids[i]] < cy[ids[i-1]]+minGap {
			cy[ids[i]] = cy[ids[i-1]] + minGap
		}
	}
	// backward: pull up where the forward pass overshot past desired
	for i := len(ids) - 2; i >= 0; i-- {
		minGap := fd[ids[i]]/2 + fd[ids[i+1]]/2 + gapW
		if cy[ids[i]] > cy[ids[i+1]]-minGap {
			cy[ids[i]] = cy[ids[i+1]] - minGap
		}
	}
}

// solveContainer resolves one sibling set. Child groups are solved
// first (depth-first) so their footprints are final before this level
// arranges or places them. owner is the group that holds these parts
// (nil at the composite root) — it is the auto-size target.
func solveContainer(parts []*CompositePart, lay *Layout, owner *CompositePart, cell float64, path string, issues *[]Issue) {
	for i, p := range parts {
		if p != nil && isContainerShape(p.Shape) && len(p.Parts) > 0 {
			solveContainer(p.Parts, p.Layout, p, cell, fmt.Sprintf("%s.parts[%d]", path, i), issues)
			p.Layout = nil
		}
	}
	if lay != nil {
		arrangeContainer(parts, lay, owner, cell, path, issues)
	} else {
		placed := placeSiblings(parts, cell, path, issues)
		switch {
		case owner == nil:
			// scene root — never auto-sized
		case placed:
			autosizeGroup(owner, parts, cell)
		case owner.Geom == nil || owner.Geom.W <= 0 || owner.Geom.D <= 0:
			// children positioned by explicit offsets and the group has no
			// authored footprint (e.g. a group frozen by a drag) — still wrap
			// the slab around them so added/dragged nodes never stick out.
			autosizeManualGroup(owner, parts, cell)
		}
	}
	checkSiblingOverlaps(parts, path, issues)
}

func partFootprint(p *CompositePart) (w, d float64) {
	w, d = defaultPartW, defaultPartD
	if isContainerShape(p.Shape) {
		w, d = defaultGroupW, defaultGroupD
	}
	if p.Geom != nil {
		if p.Geom.W > 0 {
			w = p.Geom.W
		}
		if p.Geom.D > 0 {
			d = p.Geom.D
		}
	}
	// Reserve the size the renderer will actually DRAW: shapes with a floor
	// (e.g. cloud) are clamped up at render, so the solver must clamp too or
	// the shape overflows its slot / its auto-sized group.
	mw, md, _ := iso25d.ShapeMinDims(p.Shape)
	return math.Max(w, mw), math.Max(d, md)
}

func partHeight(p *CompositePart) float64 {
	h := defaultPartH
	if isContainerShape(p.Shape) {
		h = defaultGroupSubstrateH
	}
	if p.Geom != nil && p.Geom.H > 0 {
		h = p.Geom.H
	}
	_, _, mh := iso25d.ShapeMinDims(p.Shape)
	return math.Max(h, mh)
}

// ── container arrangement (layout: row / column / grid) ─────────────

// arrangeContainer lays kids out on row-major tracks. Row and column
// are degenerate grids (cols = n, cols = 1), which makes track packing
// exact for them too: one child per column track means each column is
// exactly that child's width.
func arrangeContainer(parts []*CompositePart, lay *Layout, owner *CompositePart, cell float64, path string, issues *[]Issue) {
	kids := make([]*CompositePart, 0, len(parts))
	for i, p := range parts {
		if p == nil {
			continue
		}
		if p.Place != nil {
			*issues = append(*issues, Issue{
				Severity: SeverityWarning,
				Path:     fmt.Sprintf("%s.parts[%d].place", path, i),
				Message:  "place is ignored inside a layout container; use offset for fine-tuning",
			})
			p.Place = nil
		}
		kids = append(kids, p)
	}
	if len(kids) == 0 {
		return
	}

	gap := 1.0
	if lay.Gap != nil && *lay.Gap >= 0 {
		gap = *lay.Gap
	}
	pad := gap
	if lay.Padding != nil && *lay.Padding >= 0 {
		pad = *lay.Padding
	}
	gapW, padW := gap*cell, pad*cell
	align := lay.Align
	if align == "" {
		align = "center"
	}

	cols := 0
	switch lay.Mode {
	case "column":
		cols = 1
	case "grid":
		cols = lay.Cols
		if cols <= 0 {
			cols = int(math.Ceil(math.Sqrt(float64(len(kids)))))
		}
	case "ring":
		// v2.4 — hub-and-spoke: kids[0] is the hub at the centre,
		// kids[1..] sit on a world-space circle around it, starting at
		// the back (-y) and proceeding clockwise. The radius keeps one
		// `gap` between the hub's footprint and each satellite's.
		arrangeRing(kids, gapW, padW, owner, path, issues)
		return
	case "row", "", "auto":
		cols = len(kids)
	default:
		*issues = append(*issues, Issue{
			Severity: SeverityError,
			Path:     path + ".layout.mode",
			Message:  fmt.Sprintf("unknown layout mode %q", lay.Mode),
			Suggest:  nearest(lay.Mode, layoutModes),
		})
		cols = len(kids)
	}
	rows := (len(kids) + cols - 1) / cols

	colW := make([]float64, cols)
	rowD := make([]float64, rows)
	for idx, k := range kids {
		c, r := idx%cols, idx/cols
		w, d := partFootprint(k)
		if w > colW[c] {
			colW[c] = w
		}
		if d > rowD[r] {
			rowD[r] = d
		}
	}
	colX := make([]float64, cols)
	x := padW
	for c := range colW {
		colX[c] = x
		x += colW[c] + gapW
	}
	rowY := make([]float64, rows)
	y := padW
	for r := range rowD {
		rowY[r] = y
		y += rowD[r] + gapW
	}

	for idx, k := range kids {
		c, r := idx%cols, idx/cols
		w, d := partFootprint(k)
		kx := colX[c] + alignSlack(align, colW[c]-w)
		ky := rowY[r] + alignSlack(align, rowD[r]-d)
		bakeOffset(k, kx, ky)
	}

	if owner != nil {
		contentW := padW*2 - gapW + sumPlusGaps(colW, gapW)
		contentD := padW*2 - gapW + sumPlusGaps(rowD, gapW)
		ensureGroupFootprint(owner, contentW, contentD, padW)
	}
}

// arrangeRing places kids[0] at the centre and kids[1..] on a circle
// of radius (hubSpan + satSpan)/2 + gap around it, equally spaced,
// first satellite at the back (-y). Positions are normalised so the
// content bbox starts at padding, then the owner auto-sizes.
func arrangeRing(kids []*CompositePart, gapW, padW float64, owner *CompositePart, path string, issues *[]Issue) {
	if len(kids) < 2 {
		*issues = append(*issues, Issue{
			Severity: SeverityWarning,
			Path:     path + ".layout",
			Message:  "ring layout needs a hub plus at least one satellite; falling back to centre placement",
		})
		if len(kids) == 1 {
			bakeOffset(kids[0], padW, padW)
		}
		return
	}
	hw, hd := partFootprint(kids[0])
	maxSat := 0.0
	for _, k := range kids[1:] {
		w, d := partFootprint(k)
		maxSat = math.Max(maxSat, math.Max(w, d))
	}
	radius := (math.Max(hw, hd)+maxSat)/2 + gapW
	n := len(kids) - 1

	// Top-left positions relative to the hub centre at (0, 0).
	xs := make([]float64, len(kids))
	ys := make([]float64, len(kids))
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for i, k := range kids {
		w, d := partFootprint(k)
		cx, cy := 0.0, 0.0
		if i > 0 {
			theta := -math.Pi/2 + 2*math.Pi*float64(i-1)/float64(n)
			cx, cy = radius*math.Cos(theta), radius*math.Sin(theta)
		}
		xs[i], ys[i] = cx-w/2, cy-d/2
		minX, minY = math.Min(minX, xs[i]), math.Min(minY, ys[i])
		maxX, maxY = math.Max(maxX, xs[i]+w), math.Max(maxY, ys[i]+d)
	}
	for i, k := range kids {
		bakeOffset(k, xs[i]-minX+padW, ys[i]-minY+padW)
	}
	if owner != nil {
		ensureGroupFootprint(owner, (maxX-minX)+2*padW, (maxY-minY)+2*padW, padW)
	}
}

func sumPlusGaps(tracks []float64, gapW float64) float64 {
	total := 0.0
	for _, t := range tracks {
		total += t + gapW
	}
	return total
}

func alignSlack(align string, slack float64) float64 {
	if slack <= 0 {
		return 0
	}
	switch align {
	case "start":
		return 0
	case "end":
		return slack
	default: // center
		return slack / 2
	}
}

// bakeOffset writes a solved position into the part. The author's own
// Offset (if any) survives as a fine-tune delta on top.
func bakeOffset(p *CompositePart, x, y float64) {
	if p.Offset == nil {
		p.Offset = &WorldPoint{}
	}
	p.Offset.WX += x
	p.Offset.WY += y
}

// ensureFootprint grows/sets the owner group's Geom so the substrate
// wraps the arranged content. Explicit author dimensions win.
func ensureFootprint(owner *CompositePart, w, d float64) {
	if owner.Geom == nil {
		owner.Geom = &Geom{}
	}
	if owner.Geom.W <= 0 {
		owner.Geom.W = w
	}
	if owner.Geom.D <= 0 {
		owner.Geom.D = d
	}
}

// ensureGroupFootprint is ensureFootprint after reserving room for the group's
// own caption, so an auto-sized slab grows to fit its label instead of letting
// the caption overrun the slab or sit on the front row of children.
func ensureGroupFootprint(owner *CompositePart, w, d, padW float64) {
	w, d = labelReservedFootprint(owner, w, d, padW)
	ensureFootprint(owner, w, d)
}

// labelReservedFootprint grows (w, d) so a populated group's caption fits.
// groupLabelPart draws the label inset 14 from the front-left corner, running
// +x as iso-text at ~size*0.62 per rune and anchored at WY = d-14-size; without
// matching reservation a long caption overruns the right edge or shares the
// front child row. Returns (w, d) unchanged when there is no caption or it
// already fits, so short labels and roomy slabs are byte-identical as before.
func labelReservedFootprint(owner *CompositePart, w, d, padW float64) (float64, float64) {
	if owner == nil || strings.TrimSpace(owner.Label) == "" || len(owner.Parts) == 0 {
		return w, d
	}
	size := 11.0
	if owner.Style != nil && owner.Style.Text != nil &&
		owner.Style.Text.Size != nil && *owner.Style.Text.Size > 0 {
		size = *owner.Style.Text.Size
	}
	runes := float64(len([]rune(strings.TrimSpace(owner.Label))))
	if needW := 14 + runes*size*0.62 + 14; needW > w { // caption must fit L→R
		w = needW
	}
	if band := 14 + size; band > padW { // front strip must clear the caption row
		d += band - padW
	}
	return w, d
}

// ── sibling relations (place: rightOf / leftOf / inFrontOf / behind) ─

// placeSiblings solves the place graph for one sibling set. Returns
// whether any part used place (callers auto-size groups only then, so
// pure-offset legacy documents render byte-identically).
func placeSiblings(parts []*CompositePart, cell float64, path string, issues *[]Issue) bool {
	any := false
	for _, p := range parts {
		if p != nil && p.Place != nil {
			any = true
		}
	}
	if !any {
		return false
	}

	byID := map[string]*CompositePart{}
	idxOf := map[*CompositePart]int{}
	for i, p := range parts {
		if p == nil {
			continue
		}
		idxOf[p] = i
		if p.ID != "" {
			byID[p.ID] = p
		}
	}

	// DFS solve with memo + on-stack cycle detection. pos holds each
	// part's FINAL position (solved constraint + author delta), so
	// dependents follow when the author fine-tunes a reference part.
	pos := map[*CompositePart][3]float64{}
	const (
		stUnseen = 0
		stOnPath = 1
		stDone   = 2
	)
	state := map[*CompositePart]int{}

	var solve func(p *CompositePart) [3]float64
	solve = func(p *CompositePart) [3]float64 {
		if state[p] == stDone {
			return pos[p]
		}
		ppath := fmt.Sprintf("%s.parts[%d].place", path, idxOf[p])
		dx, dy, dz := 0.0, 0.0, 0.0
		if p.Offset != nil {
			dx, dy, dz = p.Offset.WX, p.Offset.WY, p.Offset.WZ
		}
		if p.Place == nil {
			pos[p] = [3]float64{dx, dy, dz}
			state[p] = stDone
			return pos[p]
		}
		if state[p] == stOnPath {
			*issues = append(*issues, Issue{
				Severity: SeverityError,
				Path:     ppath,
				Message:  "place relations form a cycle; falling back to offset for this part",
			})
			pos[p] = [3]float64{dx, dy, dz}
			state[p] = stDone
			return pos[p]
		}
		state[p] = stOnPath

		pl := p.Place
		gap := 1.0
		if pl.Gap != nil && *pl.Gap >= 0 {
			gap = *pl.Gap
		}
		gapX, gapY := gap, gap
		if pl.GapX != nil && *pl.GapX >= 0 {
			gapX = *pl.GapX
		}
		if pl.GapY != nil && *pl.GapY >= 0 {
			gapY = *pl.GapY
		}
		gapXW, gapYW := gapX*cell, gapY*cell
		align := pl.Align
		if align == "" {
			align = "center"
		}

		if pl.RightOf != "" && pl.LeftOf != "" {
			*issues = append(*issues, Issue{
				Severity: SeverityError,
				Path:     ppath,
				Message:  "rightOf and leftOf are mutually exclusive; using rightOf",
			})
			pl.LeftOf = ""
		}
		if pl.InFrontOf != "" && pl.Behind != "" {
			*issues = append(*issues, Issue{
				Severity: SeverityError,
				Path:     ppath,
				Message:  "inFrontOf and behind are mutually exclusive; using inFrontOf",
			})
			pl.Behind = ""
		}

		resolveRef := func(field, ref string) *CompositePart {
			r, ok := byID[ref]
			if !ok {
				*issues = append(*issues, Issue{
					Severity: SeverityError,
					Path:     ppath + "." + field,
					Message:  fmt.Sprintf("place.%s references %q which is not a sibling part id", field, ref),
					Suggest:  nearestSibling(ref, byID),
				})
				return nil
			}
			if r == p {
				*issues = append(*issues, Issue{
					Severity: SeverityError,
					Path:     ppath + "." + field,
					Message:  fmt.Sprintf("place.%s references the part itself", field),
				})
				return nil
			}
			return r
		}

		w, d := partFootprint(p)
		x, y, z := 0.0, 0.0, 0.0
		xConstrained, yConstrained, zConstrained := false, false, false
		var xRef, yRef, zRef *CompositePart

		if pl.RightOf != "" {
			if r := resolveRef("rightOf", pl.RightOf); r != nil {
				rp := solve(r)
				rw, _ := partFootprint(r)
				x = rp[0] + rw + gapXW
				xConstrained, xRef = true, r
			}
		} else if pl.LeftOf != "" {
			if r := resolveRef("leftOf", pl.LeftOf); r != nil {
				rp := solve(r)
				x = rp[0] - gapXW - w
				xConstrained, xRef = true, r
			}
		}
		if pl.InFrontOf != "" {
			if r := resolveRef("inFrontOf", pl.InFrontOf); r != nil {
				rp := solve(r)
				_, rd := partFootprint(r)
				y = rp[1] + rd + gapYW
				yConstrained, yRef = true, r
			}
		} else if pl.Behind != "" {
			if r := resolveRef("behind", pl.Behind); r != nil {
				rp := solve(r)
				y = rp[1] - gapYW - d
				yConstrained, yRef = true, r
			}
		}
		// v2.4 — above: sit flush ON TOP of the sibling (z = its top).
		if pl.Above != "" {
			if r := resolveRef("above", pl.Above); r != nil {
				rp := solve(r)
				z = rp[2] + partHeight(r)
				zConstrained, zRef = true, r
			}
		}

		// Unpinned ground axes align to a reference: prefer the
		// same-plane constraint's ref, else the above-ref (so a bare
		// `above:` centres the part on its base).
		if !xConstrained {
			if ref := firstRef(yRef, zRef); ref != nil {
				rp := solve(ref)
				rw, _ := partFootprint(ref)
				x = rp[0] + alignTrack(align, rw, w)
			}
		}
		if !yConstrained {
			if ref := firstRef(xRef, zRef); ref != nil {
				rp := solve(ref)
				_, rd := partFootprint(ref)
				y = rp[1] + alignTrack(align, rd, d)
			}
		}

		// Author offset degrades to a fine-tune delta on top of the
		// solved (or aligned) position. An axis with no constraint and
		// no alignment falls back to the delta alone, matching the
		// pre-place behavior.
		x += dx
		y += dy
		z += dz
		_ = zConstrained

		pos[p] = [3]float64{x, y, z}
		state[p] = stDone
		p.Offset = &WorldPoint{WX: x, WY: y, WZ: z}
		p.Place = nil
		return pos[p]
	}

	for _, p := range parts {
		if p != nil {
			solve(p)
		}
	}
	return true
}

func firstRef(refs ...*CompositePart) *CompositePart {
	for _, r := range refs {
		if r != nil {
			return r
		}
	}
	return nil
}

func alignTrack(align string, refSpan, span float64) float64 {
	switch align {
	case "start":
		return 0
	case "end":
		return refSpan - span
	default: // center
		return (refSpan - span) / 2
	}
}

func nearestSibling(bad string, byID map[string]*CompositePart) string {
	cand := make([]string, 0, len(byID))
	for id := range byID {
		cand = append(cand, id)
	}
	return nearest(bad, cand)
}

// autosizeGroup wraps a place-arranged group around its children: the
// content bbox is normalised to start at one padding cell from the
// substrate edge, and missing Geom dimensions are derived. Explicit
// author dimensions are respected (no shrink, no shift skip — children
// still normalise so place chains that went negative stay on-board).
func autosizeGroup(owner *CompositePart, parts []*CompositePart, cell float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range parts {
		if p == nil {
			continue
		}
		x, y := 0.0, 0.0
		if p.Offset != nil {
			x, y = p.Offset.WX, p.Offset.WY
		}
		w, d := partFootprint(p)
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x+w), math.Max(maxY, y+d)
	}
	if math.IsInf(minX, 1) {
		return
	}
	padW := cell
	for _, p := range parts {
		if p == nil {
			continue
		}
		bakeOffset(p, padW-minX, padW-minY)
	}
	ensureGroupFootprint(owner, (maxX-minX)+2*padW, (maxY-minY)+2*padW, padW)
}

// autosizeManualGroup wraps a group whose children are positioned by explicit
// offsets (no layout/place) — typically a group frozen by a drag. Like
// autosizeGroup it normalises children to a padding inset and derives the
// footprint, but it ALSO shifts the owner's own offset by the inverse, so the
// children don't move on screen — the frozen group stays put and its slab just
// grows to contain them. Only called when the owner has no authored geom.w/d.
func autosizeManualGroup(owner *CompositePart, parts []*CompositePart, cell float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range parts {
		if p == nil {
			continue
		}
		x, y := 0.0, 0.0
		if p.Offset != nil {
			x, y = p.Offset.WX, p.Offset.WY
		}
		w, d := partFootprint(p)
		minX, minY = math.Min(minX, x), math.Min(minY, y)
		maxX, maxY = math.Max(maxX, x+w), math.Max(maxY, y+d)
	}
	if math.IsInf(minX, 1) {
		return
	}
	pad := cell
	sx, sy := pad-minX, pad-minY
	for _, p := range parts {
		if p != nil {
			bakeOffset(p, sx, sy)
		}
	}
	if owner.Offset != nil { // keep an already-positioned (frozen) group put
		owner.Offset.WX -= sx
		owner.Offset.WY -= sy
	}
	ensureGroupFootprint(owner, (maxX-minX)+2*pad, (maxY-minY)+2*pad, pad)
}

// ── post-solve diagnostics ───────────────────────────────────────────

// checkSiblingOverlaps warns when two siblings' boxes intersect in
// BOTH the ground plane and the z extent (ground overlap alone is fine
// — stacking a part on top of another is a legitimate composition).
func checkSiblingOverlaps(parts []*CompositePart, path string, issues *[]Issue) {
	type box struct {
		idx            int
		id             string
		x0, y0, x1, y1 float64
		z0, z1         float64
	}
	boxes := make([]box, 0, len(parts))
	for i, p := range parts {
		if p == nil || isGhostPart(p) {
			continue
		}
		x, y, z := 0.0, 0.0, 0.0
		if p.Offset != nil {
			x, y, z = p.Offset.WX, p.Offset.WY, p.Offset.WZ
		}
		w, d := partFootprint(p)
		name := p.ID
		if name == "" {
			name = fmt.Sprintf("parts[%d]", i)
		}
		boxes = append(boxes, box{
			idx: i, id: name,
			x0: x, y0: y, x1: x + w, y1: y + d,
			z0: z, z1: z + partHeight(p),
		})
	}
	const eps = 0.01
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			a, b := boxes[i], boxes[j]
			ox := math.Min(a.x1, b.x1) - math.Max(a.x0, b.x0)
			oy := math.Min(a.y1, b.y1) - math.Max(a.y0, b.y0)
			oz := math.Min(a.z1, b.z1) - math.Max(a.z0, b.z0)
			if ox > eps && oy > eps && oz > eps {
				*issues = append(*issues, Issue{
					Severity: SeverityWarning,
					Path:     fmt.Sprintf("%s.parts[%d]", path, b.idx),
					Message: fmt.Sprintf("%q overlaps sibling %q by %.0f×%.0f world units; increase gap or move it",
						b.id, a.id, ox, oy),
				})
			}
		}
	}
}

// isGhostPart reports whether a part is pure decoration that other
// parts may legitimately overlap: every face fill is "none" (dashed
// inset frames, ghost volumes) or it renders as a wireframe.
func isGhostPart(p *CompositePart) bool {
	if p.Style == nil {
		return false
	}
	if e := p.Style.Effects; e != nil && e.Wireframe != nil && *e.Wireframe {
		return true
	}
	if pal := p.Style.Palette; pal != nil &&
		pal.Top == "none" && pal.Left == "none" && pal.Right == "none" {
		return true
	}
	return false
}

// ── validate-time dry run ────────────────────────────────────────────

// layoutIssues runs the solver against a throwaway clone of the node's
// parts tree so Validate can report layout errors/warnings without
// mutating the author's document.
func layoutIssues(n *Node, canvas *Canvas) []Issue {
	if n == nil || n.Shape != "composite" {
		return nil
	}
	clone := &Node{Shape: n.Shape, GridStep: n.GridStep, Parts: cloneParts(n.Parts)}
	if n.Layout != nil {
		l := *n.Layout
		clone.Layout = &l
	}
	return applyLayout(clone, canvas)
}

func cloneParts(parts []*CompositePart) []*CompositePart {
	if parts == nil {
		return nil
	}
	out := make([]*CompositePart, len(parts))
	for i, p := range parts {
		if p == nil {
			continue
		}
		cp := *p
		if p.Offset != nil {
			o := *p.Offset
			cp.Offset = &o
		}
		if p.Geom != nil {
			g := *p.Geom
			cp.Geom = &g
		}
		if p.Place != nil {
			pl := *p.Place
			cp.Place = &pl
		}
		if p.Layout != nil {
			l := *p.Layout
			cp.Layout = &l
		}
		cp.Parts = cloneParts(p.Parts)
		out[i] = &cp
	}
	return out
}

// ResolvePartOffset runs layout on the document's scene and returns the
// world (wx, wy) the part with id resolves to — its auto/place-solved
// position, or its explicit pin. The Studio drag endpoint adds the
// drag delta to this to get a new absolute offset, so dragging a node
// that has no coordinates yet (pure auto-layout) still works. Operates
// on a clone so the caller's document is not mutated.
func ResolvePartOffset(doc *Document, id string) (wx, wy, wz float64, ok bool) {
	if doc == nil {
		return 0, 0, 0, false
	}
	scene := doc.Scene()
	if scene == nil {
		return 0, 0, 0, false
	}
	applyLayout(scene, doc.Canvas)
	var found *CompositePart
	var walk func(ps []*CompositePart)
	walk = func(ps []*CompositePart) {
		for _, p := range ps {
			if p == nil || found != nil {
				continue
			}
			if p.ID == id {
				found = p
				return
			}
			walk(p.Parts)
		}
	}
	walk(scene.Parts)
	if found == nil {
		return 0, 0, 0, false
	}
	if found.Offset == nil {
		return 0, 0, 0, true
	}
	return found.Offset.WX, found.Offset.WY, found.Offset.WZ, true
}

// ResolvePartGeom returns the part's resolved width/depth/height AFTER layout.
// For an auto-sized container (a group with no authored geom.w/d) this is the
// size the solver computed to wrap its children — what the canvas actually
// draws — so Studio can show "auto 380" instead of a bare blank that reads as
// "missing". Returns ok=false if the part isn't found.
func ResolvePartGeom(doc *Document, id string) (w, d, h float64, ok bool) {
	if doc == nil {
		return 0, 0, 0, false
	}
	scene := doc.Scene()
	if scene == nil {
		return 0, 0, 0, false
	}
	applyLayout(scene, doc.Canvas)
	var found *CompositePart
	var walk func(ps []*CompositePart)
	walk = func(ps []*CompositePart) {
		for _, p := range ps {
			if p == nil || found != nil {
				continue
			}
			if p.ID == id {
				found = p
				return
			}
			walk(p.Parts)
		}
	}
	walk(scene.Parts)
	if found == nil {
		return 0, 0, 0, false
	}
	if found.Geom == nil {
		return 0, 0, 0, true
	}
	return found.Geom.W, found.Geom.D, found.Geom.H, true
}

// ConnectorBend returns the current bend of the ci-th connector on the
// document's scene (0,0 if unset). Used to accumulate edge drags.
func ConnectorBend(doc *Document, ci int) (wx, wy float64) {
	if doc == nil {
		return 0, 0
	}
	scene := doc.Scene()
	if scene == nil || ci < 0 || ci >= len(scene.Connectors) {
		return 0, 0
	}
	if b := scene.Connectors[ci].Bend; b != nil {
		return b.WX, b.WY
	}
	return 0, 0
}

// SceneIsAuto reports whether the document's scene is still under
// connector-driven auto-layout (layout: {mode: auto}). The Studio drag
// endpoint uses this to decide whether a manual move must first FREEZE
// the scene into explicit coordinates (drawio model: once the user
// takes manual control, the engine stops deciding positions).
func SceneIsAuto(doc *Document) bool {
	if doc == nil {
		return false
	}
	s := doc.Scene()
	return s != nil && s.Layout != nil && s.Layout.Mode == "auto"
}

// SceneNeedsFreeze reports whether the engine still decides any
// root-part position — a root layout of ANY mode (auto / ring / grid /
// row / column) or any root part positioned by `place`. The Studio
// drag endpoint freezes such a scene into explicit coordinates on the
// first manual move. A scene with no root layout and no root place is
// already manual — nothing to freeze.
func SceneNeedsFreeze(doc *Document) bool {
	if doc == nil {
		return false
	}
	s := doc.Scene()
	if s == nil {
		return false
	}
	if s.Layout != nil {
		return true
	}
	for _, p := range s.Parts {
		if p != nil && p.Place != nil {
			return true
		}
	}
	return false
}

// ResolveChildOffsets runs layout and returns the resolved offsets (relative to
// the group) of a group's DIRECT children, keyed by id — the nested analogue of
// ResolveAllOffsets, used to freeze a layout group when one of its children is
// dragged so the rest keep their spots.
func ResolveChildOffsets(doc *Document, groupID string) map[string][3]float64 {
	out := map[string][3]float64{}
	if doc == nil {
		return out
	}
	scene := doc.Scene()
	if scene == nil {
		return out
	}
	applyLayout(scene, doc.Canvas)
	var grp *CompositePart
	var walk func(ps []*CompositePart)
	walk = func(ps []*CompositePart) {
		for _, p := range ps {
			if p == nil || grp != nil {
				continue
			}
			if p.ID == groupID {
				grp = p
				return
			}
			walk(p.Parts)
		}
	}
	walk(scene.Parts)
	if grp == nil {
		return out
	}
	for _, c := range grp.Parts {
		if c == nil || c.ID == "" {
			continue
		}
		if c.Offset != nil {
			out[c.ID] = [3]float64{c.Offset.WX, c.Offset.WY, c.Offset.WZ}
		} else {
			out[c.ID] = [3]float64{0, 0, 0}
		}
	}
	return out
}

// GroupHasLayout reports whether the named container arranges its children with
// a layout (so a child drag must first freeze that group).
func GroupHasLayout(doc *Document, groupID string) bool {
	if doc == nil {
		return false
	}
	scene := doc.Scene()
	if scene == nil {
		return false
	}
	var found bool
	var walk func(ps []*CompositePart)
	walk = func(ps []*CompositePart) {
		for _, p := range ps {
			if p == nil || found {
				continue
			}
			if p.ID == groupID {
				found = p.Layout != nil
				return
			}
			walk(p.Parts)
		}
	}
	walk(scene.Parts)
	return found
}

// ResolveAllOffsets runs layout on the scene and returns every
// ROOT-level part's resolved world (wx, wy) keyed by id. Used to bake
// an auto-layout scene into explicit per-node coordinates on the first
// manual drag, so subsequent renders never re-flow.
func ResolveAllOffsets(doc *Document) map[string][3]float64 {
	out := map[string][3]float64{}
	if doc == nil {
		return out
	}
	s := doc.Scene()
	if s == nil {
		return out
	}
	applyLayout(s, doc.Canvas)
	for _, p := range s.Parts {
		if p == nil || p.ID == "" {
			continue
		}
		if p.Offset != nil {
			out[p.ID] = [3]float64{p.Offset.WX, p.Offset.WY, p.Offset.WZ}
		} else {
			out[p.ID] = [3]float64{0, 0, 0}
		}
	}
	return out
}
