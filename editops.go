package isotopo

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/MarkovWangRR/iso-topology/yamledit"
	"gopkg.in/yaml.v3"
)

// This file lifts Studio's "direct-manipulation → DSL rewrite" loop out of the
// HTTP handlers and the (formerly internal) yamledit package into a public,
// STATELESS contract any embedder — a cloud service, a WASM module — can call.
//
// The whole loop was always stateless: the editor POSTs the full DSL text and
// the server transforms that text (the single source of truth), never holding
// state. ApplyOpText is that transform; ApplyOp bundles a re-render.

// EditOp describes one direct-manipulation edit. It mirrors the operations the
// Studio canvas produces; Kind selects which fields are read.
//
//	move       — Target node|edge + DWX/DWY world drag delta (+ Snap grid step,
//	             or Waypoints for a drawio-style per-segment edge edit)
//	move-segment — CI + Segment (pre-clip route index) + DWX/DWY: move an
//	             orthogonal leg, preserving world Z and docked endpoints.
//	move-multi — IDs + DWX/DWY: apply the SAME world delta to every listed node
//	             in ONE text surgery (Studio group-move). Atomic: any id that
//	             fails aborts the whole op and the source is returned unchanged.
//	set-field  — Target node|edge|canvas + Fields: dotted YAML path → value
//	add        — append a default rectangle part to the scene
//	delete     — Target node|edge (a node also drops its connectors)
//	delete-multi — IDs: delete every listed node in one atomic surgery
//	duplicate  — Target node: clone it with a fresh id, offset down-right
type EditOp struct {
	Kind      string            `json:"kind"`
	Target    string            `json:"target,omitempty"`  // node | edge | canvas
	ID        string            `json:"id,omitempty"`      // part id
	IDs       []string          `json:"ids,omitempty"`     // move-multi / delete-multi: batch part ids
	CI        int               `json:"ci,omitempty"`      // connector index
	Segment   int               `json:"segment,omitempty"` // move-segment: pre-clip route segment
	Fields    map[string]string `json:"fields,omitempty"`  // set-field: path → value
	DWX       float64           `json:"dwx,omitempty"`     // move: world drag delta
	DWY       float64           `json:"dwy,omitempty"`
	Snap      float64           `json:"snap,omitempty"`      // move: grid step (0 = none)
	Waypoints [][2]float64      `json:"waypoints,omitempty"` // edge move: interior corners
}

// RenderSource validates and renders a DSL document to one topology SVG. Parse
// and validation errors are returned as Issues (with an error-severity entry),
// not as err — err is reserved for future I/O paths and is currently always nil.
// The layout engine is the default (dagre); d2 auto-layout honours it, YAML/JSON
// have no layout step.
func RenderSource(format string, src []byte) (svg string, issues []Issue, err error) {
	doc, derr := LoadInput(context.Background(), format, src, LayoutDagre)
	if derr != nil {
		return "", []Issue{{Severity: SeverityError, Path: "$", Message: derr.Error()}}, nil
	}
	issues = Validate(doc)
	if format != "d2" {
		issues = append(issues, UnknownKeyIssues(src)...)
	}
	for _, i := range issues {
		if i.Severity == SeverityError {
			return "", issues, nil
		}
	}
	if scene := doc.Scene(); scene != nil {
		svg = RenderWithCanvas(scene, doc.Theme, doc.Canvas, doc.Annotations)
	}
	if svg == "" {
		issues = append(issues, Issue{
			Severity: SeverityError, Path: "$",
			Message: "document renders no scene — it has no nodes (or the scene resolves empty)",
		})
	}
	return svg, issues, nil
}

// ApplyOp applies a direct-manipulation edit to DSL text and re-renders it:
// newSrc is the rewritten document (comments and formatting preserved), svg and
// issues are RenderSource of that result. The operation is stateless — newSrc
// is a pure function of (format, src, op). err is non-nil only when the op
// itself can't be applied (e.g. the target id isn't in the source).
func ApplyOp(format string, src []byte, op EditOp) (newSrc []byte, svg string, issues []Issue, err error) {
	newSrc, err = ApplyOpText(format, src, op)
	if err != nil {
		return src, "", nil, err
	}
	svg, issues, _ = RenderSource(format, newSrc)
	return newSrc, svg, issues, nil
}

// ApplyOpText applies an edit and returns ONLY the rewritten DSL text — the
// comment-preserving transform, without rendering. Embedders that render
// separately (or with a non-default layout engine) use this.
func ApplyOpText(format string, src []byte, op EditOp) ([]byte, error) {
	out, err := applyOpText(format, src, op)
	if err != nil {
		return src, err
	}
	// Universal safety net: a text-surgery op must NEVER return a document it
	// made unparseable (the result is fed to the next edit in the chain, so a
	// silent corruption poisons the whole session). If the original parsed and
	// the result doesn't, surface it as an error on the ORIGINAL source instead.
	// Skips d2, whose source isn't YAML/JSON.
	if format != "d2" && string(out) != string(src) {
		if _, perr := Parse(out); perr != nil {
			if _, ok := Parse(src); ok == nil {
				return src, fmt.Errorf("%s: edit would make the document unparseable: %w", op.Kind, perr)
			}
		}
	}
	return out, nil
}

func applyOpText(format string, src []byte, op EditOp) ([]byte, error) {
	// Ops that LOCATE a nested child by line can't address children hidden in a
	// single-line inline `parts: [ {…} ]`; expand those to block form first so
	// the edit hits the right node (not the outer group). No-op without inline
	// lists; d2 is left alone.
	if format != "d2" {
		switch op.Kind {
		case "move", "move-multi", "set-field", "delete", "delete-multi", "duplicate", "reparent":
			src = []byte(yamledit.ExpandInlineParts(string(src)))
		}
	}
	switch op.Kind {
	case "move-segment":
		return applyMoveSegment(format, src, op)
	case "move":
		return applyMove(format, src, op)
	case "move-multi":
		return applyMoveMulti(format, src, op)
	case "delete-multi":
		return applyDeleteMulti(format, src, op)
	case "set-field":
		return applySetField(src, op)
	case "add":
		out, ok := yamledit.AddPart(string(src))
		if !ok {
			return src, fmt.Errorf("add: scene parts block not found")
		}
		return []byte(out), nil
	case "add-edge":
		from, to := op.Fields["from"], op.Fields["to"]
		if from == "" || to == "" {
			return src, fmt.Errorf("add-edge: from and to are required")
		}
		// Validate both endpoints exist (mirrors set-field/delete) so we don't
		// silently append a dangling connector that blanks the whole render.
		if doc, derr := Parse(src); derr == nil {
			for _, endpoint := range []struct{ ref, anchor string }{{from, op.Fields["fromAnchor"]}, {to, op.Fields["toAnchor"]}} {
				if err := checkEditEndpoint(doc, endpoint.ref, endpoint.anchor); err != nil {
					return src, err
				}
			}
		}
		out, ok := yamledit.AddConnector(string(src), from, to, op.Fields["fromAnchor"], op.Fields["toAnchor"])
		if !ok {
			return src, fmt.Errorf("add-edge: connectors block not found and no parts block to anchor to")
		}
		return []byte(out), nil
	case "delete":
		return applyDelete(format, src, op)
	case "duplicate":
		return applyDuplicate(format, src, op)
	case "reparent":
		return applyReparent(format, src, op)
	default:
		return src, fmt.Errorf("unknown op kind %q", op.Kind)
	}
}

// rerouteMovedConnectors gives obstacle avoidance to the edges touching a
// just-moved node (the Studio drag → auto-route behaviour). For each connector
// on movedID whose straight line now tunnels another node, it computes an
// avoiding orthogonal route with the scorecard kernel and writes
// `routing: orthogonal` + interior `waypoints` to that connector. Edges that
// are already clear — or that can't be cleared — are left untouched, so a
// default straight edge only becomes routed when a move actually creates a
// collision. Non-yaml formats are returned unchanged.
func rerouteMovedConnectors(format string, src []byte, movedID string) []byte {
	if format == "d2" {
		return src
	}
	doc, err := LoadInput(context.Background(), format, src, LayoutDagre)
	if err != nil {
		return src
	}
	scene := doc.Scene()
	if scene == nil {
		return src
	}
	rects, byID, _ := buildPlanModelOpt(scene, doc.Theme, doc.Canvas, false)
	var leaves []planRect
	for _, r := range rects {
		if !r.container {
			leaves = append(leaves, r)
		}
	}
	out := string(src)
	for ci, c := range scene.Connectors {
		if c == nil || len(c.Waypoints3D) > 0 {
			continue
		}
		fromID, toID := connectorTarget(c.From), connectorTarget(c.To)
		if fromID != movedID && toID != movedID {
			continue
		}
		fr, okF := byID[fromID]
		to, okT := byID[toID]
		if !okF || !okT {
			continue
		}
		ez := edgeZLevel(fr, to)
		// Inflate obstacles by a clearance margin so routes are pushed to keep
		// a visible gap (a route that merely grazes a face is not "avoiding").
		obstacles := inflateRects(leaves, 12)
		straight := [][2]float64{{fr.x + fr.w/2, fr.y + fr.d/2}, {to.x + to.w/2, to.y + to.d/2}}
		if routeTunnels(straight, fromID, toID, ez, obstacles) == 0 {
			continue // already clear — leave the edge as the author drew it
		}
		route := avoidingRoute(fr, to, obstacles)
		if routeTunnels(route, fromID, toID, ez, obstacles) > 0 {
			continue // no candidate clears it — don't write a still-broken detour
		}
		var wps [][2]float64
		for _, p := range route[1 : len(route)-1] {
			if len(wps) > 0 && math.Abs(wps[len(wps)-1][0]-p[0]) < 0.5 && math.Abs(wps[len(wps)-1][1]-p[1]) < 0.5 {
				continue // drop coincident corners
			}
			wps = append(wps, p)
		}
		if len(wps) == 0 {
			continue
		}
		line := yamledit.FindConnectorLine(out, ci)
		if line < 0 {
			continue
		}
		if o2, ok := yamledit.SetField(out, line, []string{"routing"}, "orthogonal"); ok {
			out = o2
		}
		if line = yamledit.FindConnectorLine(out, ci); line < 0 {
			continue
		}
		if o2, ok := yamledit.UpsertInlineList(out, line, "waypoints", wps); ok {
			out = o2
		}
	}
	return []byte(out)
}

// inflateRects grows each footprint by m on every side (x/y only — z-floor
// stays) so routing keeps a visible clearance instead of grazing a face.
func inflateRects(rs []planRect, m float64) []planRect {
	out := make([]planRect, len(rs))
	for i, r := range rs {
		out[i] = r
		out[i].x, out[i].y = r.x-m, r.y-m
		out[i].w, out[i].d = r.w+2*m, r.d+2*m
	}
	return out
}

// avoidingRoute returns the orthogonal route between two footprints that least
// tunnels the obstacles: the normal staircases PLUS detour candidates whose
// cross-leg runs in a clear lane just beyond the obstacle cluster (so the route
// goes AROUND it, not through it). Cost ranks by tunnelling, then bends/length.
func avoidingRoute(fr, to planRect, obstacles []planRect) [][2]float64 {
	ez := edgeZLevel(fr, to)
	cands := planRouteCandidates(fr, to)

	var obs []planRect
	for _, r := range obstacles {
		if r.id == fr.id || r.id == to.id || r.h <= planThinH || !sameFloor(ez, r) || enclosesBoth(r, fr, to) {
			continue
		}
		obs = append(obs, r)
	}
	if len(obs) > 0 {
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, r := range obs {
			minX, minY = math.Min(minX, r.x), math.Min(minY, r.y)
			maxX, maxY = math.Max(maxX, r.x+r.w), math.Max(maxY, r.y+r.d)
		}
		const mgn = 16.0
		fcx, fcy := fr.x+fr.w/2, fr.y+fr.d/2
		tcx, tcy := to.x+to.w/2, to.y+to.d/2
		// vertical detours: exit along y to a clear lane above/below, cross, return
		for _, laneY := range []float64{minY - mgn, maxY + mgn} {
			sy, ey := fr.y, to.y
			if laneY > fcy {
				sy = fr.y + fr.d
			}
			if laneY > tcy {
				ey = to.y + to.d
			}
			cands = append(cands, [][2]float64{{fcx, sy}, {fcx, laneY}, {tcx, laneY}, {tcx, ey}})
		}
		// horizontal detours: exit along x to a clear lane left/right, cross, return
		for _, laneX := range []float64{minX - mgn, maxX + mgn} {
			sx, ex := fr.x, to.x
			if laneX > fcx {
				sx = fr.x + fr.w
			}
			if laneX > tcx {
				ex = to.x + to.w
			}
			cands = append(cands, [][2]float64{{sx, fcy}, {laneX, fcy}, {laneX, tcy}, {ex, tcy}})
		}
	}

	best := cands[0]
	bestCost := routeCost(best, fr, to, obstacles, nil)
	for _, c := range cands[1:] {
		if cost := routeCost(c, fr, to, obstacles, nil); cost < bestCost {
			best, bestCost = c, cost
		}
	}
	return best
}

// freezeOffsets writes an explicit `offset:` for every id in offs (re-finding
// each line since a write shifts numbers), adding the world delta to nudgeID.
func freezeOffsets(out string, offs map[string][3]float64, nudgeID string, dwx, dwy float64, snap func(float64) float64) string {
	ids := make([]string, 0, len(offs))
	for id := range offs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := offs[id]
		wx, wy, wz := o[0], o[1], o[2]
		if id == nudgeID {
			wx, wy = snap(wx+dwx), snap(wy+dwy)
		}
		if o2, ok := yamledit.UpsertInlineKey(out, yamledit.FindPartIDLine(out, id), "offset", wx, wy, wz); ok {
			out = o2
		}
	}
	return out
}

// applyMoveNode is the node half of the move op WITHOUT the connector-reroute
// pass: it returns the text with the node's offset rewritten (freezing layout
// scenes/groups exactly as before). applyMove wraps it with a reroute so the
// single-move behaviour is unchanged; applyMoveMulti chains it across every id
// first and reroutes once at the end — mid-batch rerouting would score edges
// against partners that haven't moved yet and bake stale detours in.
func applyMoveNode(format string, src []byte, op EditOp) ([]byte, error) {
	doc, derr := LoadInput(context.Background(), format, src, LayoutDagre)
	if derr != nil {
		return src, derr
	}
	snap := func(v float64) float64 {
		if op.Snap > 0 {
			return math.Round(v/op.Snap) * op.Snap
		}
		return v
	}
	s := string(src)
	// A node nested in a LAYOUT-driven group: the root-only freeze below
	// never moves it (ResolveAllOffsets/SceneNeedsFreeze are root-scoped).
	// Freeze that group instead — drop its layout and pin every child to its
	// resolved offset — so the dragged child can actually move. Freeze the
	// root too if it still needs it, so the group itself stays put.
	if parent := parentOf(doc, op.ID); parent != "" && GroupHasLayout(doc, parent) {
		out := s
		if SceneNeedsFreeze(doc) {
			out = freezeOffsets(yamledit.FreezeLayoutText(out), ResolveAllOffsets(doc), "", 0, 0, snap)
		}
		out = yamledit.FreezeGroupLayoutText(out, parent)
		out = freezeOffsets(out, ResolveChildOffsets(doc, parent), op.ID, op.DWX, op.DWY, snap)
		return []byte(out), nil
	}
	// drawio model: the FIRST manual move freezes the whole scene into
	// explicit coordinates and drops auto-layout, so the engine never
	// re-decides positions; later moves just nudge one node.
	if parentOf(doc, op.ID) == "" && SceneNeedsFreeze(doc) {
		offs := ResolveAllOffsets(doc)
		out := yamledit.FreezeLayoutText(s)
		ids := make([]string, 0, len(offs))
		for id := range offs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			o := offs[id]
			wx, wy, wz := o[0], o[1], o[2]
			if id == op.ID {
				wx, wy = snap(wx+op.DWX), snap(wy+op.DWY)
			}
			var ok bool
			out, ok = yamledit.UpsertInlineKey(out, yamledit.FindPartIDLine(out, id), "offset", wx, wy, wz)
			if !ok {
				return src, fmt.Errorf("move: part %q not found after freeze", id)
			}
		}
		return []byte(out), nil
	}
	// Prefer the AUTHORED offset (read from the source text) when the part has
	// one: ResolvePartOffset returns the resolved render position, which inside
	// an autosize group folds in the slab's padding shift — baking that back
	// made a move non-idempotent (a zero-delta drag jumped the node by the
	// padding). The authored offset is the true base; fall back to resolving
	// only when the part has no offset yet (pure auto-layout).
	cx, cy, cz, authored := yamledit.ReadInlineOffset(s, yamledit.FindPartIDLine(s, op.ID))
	if !authored {
		// No authored offset — derive the part's CURRENT local offset from the
		// renderer's own resolver (world position minus its parent's child
		// origin), not ResolvePartOffset, whose solved baseline diverges from
		// what's drawn and made a move of an offset-less node non-idempotent.
		lowered := resolveLowered(doc)
		if lowered[op.ID] == nil {
			return src, fmt.Errorf("move: part %q not found", op.ID)
		}
		wx, wy, wz := worldXYZ(lowered[op.ID])
		cox, coy, coz := childOrigin(doc, lowered, s, parentOf(doc, op.ID))
		cx, cy, cz = wx-cox, wy-coy, wz-coz
	}
	out, ok := yamledit.UpsertInlineKey(s, yamledit.FindPartIDLine(s, op.ID), "offset", snap(cx+op.DWX), snap(cy+op.DWY), cz)
	if !ok {
		return src, fmt.Errorf("move: part %q line not found", op.ID)
	}
	return []byte(out), nil
}

// dedupIDs returns op ids de-duplicated in first-seen order: a batch must apply
// each node's delta exactly once even if the caller lists an id twice.
func dedupIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// applyMoveMulti applies the SAME world delta to every node in op.IDs as one
// atomic text surgery: the moves chain on a working copy and the ORIGINAL
// source is returned untouched if any id fails (all-or-nothing, so a partial
// group move can never be persisted). Connector rerouting runs once at the
// end, against the final positions, so edges between two moved nodes are
// scored where they actually land.
func applyMoveMulti(format string, src []byte, op EditOp) ([]byte, error) {
	ids := dedupIDs(op.IDs)
	if len(ids) == 0 {
		return src, fmt.Errorf("move-multi: ids required")
	}
	out := src
	for _, id := range ids {
		sub := op
		sub.Kind, sub.Target, sub.ID, sub.IDs = "move", "node", id, nil
		var err error
		out, err = applyMoveNode(format, out, sub)
		if err != nil {
			return src, fmt.Errorf("move-multi %q: %w", id, err)
		}
	}
	for _, id := range ids {
		out = rerouteMovedConnectors(format, out, id)
	}
	return out, nil
}

// applyDeleteMulti deletes every node in op.IDs (each also dropping its
// connectors, exactly like a single delete) in one atomic surgery: any id that
// fails — missing, a container, an anchor donor — aborts the whole batch and
// the original source is returned unchanged.
func applyDeleteMulti(format string, src []byte, op EditOp) ([]byte, error) {
	ids := dedupIDs(op.IDs)
	if len(ids) == 0 {
		return src, fmt.Errorf("delete-multi: ids required")
	}
	out := src
	for _, id := range ids {
		var err error
		out, err = applyDelete(format, out, EditOp{Kind: "delete", Target: "node", ID: id})
		if err != nil {
			return src, fmt.Errorf("delete-multi %q: %w", id, err)
		}
	}
	return out, nil
}

func applyMove(format string, src []byte, op EditOp) ([]byte, error) {
	if op.Target == "node" {
		out, err := applyMoveNode(format, src, op)
		if err != nil {
			return src, err
		}
		return rerouteMovedConnectors(format, out, op.ID), nil
	}
	doc, derr := LoadInput(context.Background(), format, src, LayoutDagre)
	if derr != nil {
		return src, derr
	}
	s := string(src)
	switch op.Target {
	case "edge":
		if scene := doc.Scene(); scene != nil && op.CI >= 0 && op.CI < len(scene.Connectors) && len(scene.Connectors[op.CI].Waypoints3D) > 0 && (scene.Connectors[op.CI].Routing == "orthogonal" || scene.Connectors[op.CI].Routing == "orthogonal2") {
			return src, fmt.Errorf("move: this edge has a 3-D route; use move-segment or remove waypoints3d first")
		}
		// Per-segment waypoint edit (drawio-style) when supplied; else the
		// legacy single-corner bend accumulated from the current value.
		if len(op.Waypoints) > 0 {
			out, ok := yamledit.UpsertInlineList(s, yamledit.FindConnectorLine(s, op.CI), "waypoints", op.Waypoints)
			if !ok {
				return src, fmt.Errorf("move: connector %d not found", op.CI)
			}
			return []byte(out), nil
		}
		bx, by := ConnectorBend(doc, op.CI)
		out, ok := yamledit.UpsertInlineKey(s, yamledit.FindConnectorLine(s, op.CI), "bend", bx+op.DWX, by+op.DWY, 0)
		if !ok {
			return src, fmt.Errorf("move: connector %d not found", op.CI)
		}
		return []byte(out), nil
	}
	return src, fmt.Errorf("move: target must be node|edge")
}

// foldIconColor returns op.Fields with the synthetic "@iconColor" key resolved
// into a concrete `icon` write: the tint lives in the icon-ref suffix
// (iso://glyph|si/<name>/RRGGBB), not a YAML key. The base ref is the icon set
// in this same op if present, else the node's current icon. Non-node targets and
// non-glyph icons can't be tinted → the key is just dropped. The caller's map is
// never mutated.
func foldIconColor(src []byte, op EditOp) map[string]string {
	hex, ok := op.Fields["@iconColor"]
	if !ok {
		return op.Fields
	}
	out := make(map[string]string, len(op.Fields))
	for k, v := range op.Fields {
		if k != "@iconColor" {
			out[k] = v
		}
	}
	if op.Target == "node" {
		base := op.Fields["icon"]
		if base == "" {
			var root map[string]interface{}
			if yaml.Unmarshal(src, &root) == nil {
				if nm := yamledit.FindNodeMap(root, op.ID); nm != nil {
					base, _ = nm["icon"].(string)
				}
			}
		}
		if spliced := spliceIconColor(base, hex); spliced != "" {
			out["icon"] = spliced
		}
	}
	return out
}

// edgeEndpointBase reduces a connector endpoint ref to the part id it must
// match: strips a ".anchor" suffix and a "~N" stack-instance suffix.
func edgeEndpointBase(ref string) string {
	id := connectorTarget(ref)
	if i := strings.IndexByte(id, '~'); i > 0 {
		id = id[:i]
	}
	return id
}

func checkEditEndpoint(doc *Document, ref, anchor string) error {
	id := connectorTarget(ref)
	p := findPart(doc, edgeEndpointBase(ref))
	if p == nil {
		return fmt.Errorf("add-edge: endpoint %q not found", ref)
	}
	if id != p.ID {
		suffix := strings.TrimPrefix(id, p.ID+"~")
		n, err := strconv.Atoi(suffix)
		if err != nil || n < 1 || p.Stack == nil || n >= p.Stack.Count || strconv.Itoa(n) != suffix {
			return fmt.Errorf("add-edge: instance %q not found", id)
		}
	}
	if i := strings.IndexByte(ref, '.'); i >= 0 {
		if anchor != "" {
			return fmt.Errorf("add-edge: endpoint %q already specifies an anchor", ref)
		}
		anchor = ref[i+1:]
	}
	if anchor != "" && !contains(anchorNames, anchor) {
		return fmt.Errorf("add-edge: unknown anchor %q", anchor)
	}
	if _, fixed := fixedPortSide(anchor); fixed && !hasPortBody(p.Shape) {
		return fmt.Errorf("add-edge: %q has no body for a fixed contour port; use a legacy anchor", ref)
	}
	return nil
}

// isCleanID reports whether s is a usable part id: a non-empty run of letters,
// digits, '_' or '-'. Excludes '.' (the partID.anchor separator), '~' (stack
// instance), whitespace, and flow punctuation that would dangle refs or corrupt
// the YAML.
func isCleanID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// boolFieldPaths are the detail-editor fields whose YAML value is a typed
// boolean (effects bools). They're written bare via SetFieldRaw so the value
// round-trips as a bool, not the quoted string "true". Keep in sync with the
// "bool"-typed fields declared in fields.go.
var boolFieldPaths = map[string]bool{
	"style.effects.faceSplit": true,
	"style.effects.wireframe": true,
}

func applySetField(src []byte, op EditOp) ([]byte, error) {
	out := string(src)
	// Work on a private copy — foldIconColor may return op.Fields itself, and we
	// mutate (delete "id") below.
	fields := map[string]string{}
	for k, v := range foldIconColor(src, op) {
		fields[k] = v
	}

	// Guard: the node target must exist (an id-only rename otherwise no-ops
	// silently, unlike every other op which reports "not found").
	if op.Target == "node" {
		if doc, err := Parse(src); err == nil && findPart(doc, op.ID) == nil {
			return src, fmt.Errorf("set-field: target %q not found", op.ID)
		}
	}
	// Guard: an id must be a clean identifier. Empty, whitespace, a dot (the
	// partID.anchor separator), or flow punctuation would dangle references or
	// produce an unrenderable/unparseable doc. Reserved words like "null" are
	// allowed (RenamePart quotes them).
	if newID, ok := fields["id"]; ok && op.Target == "node" && newID != op.ID {
		if !isCleanID(newID) {
			return src, fmt.Errorf("set-field: invalid id %q — use letters, digits, '_' or '-' (no dots, spaces, or punctuation)", newID)
		}
		// And it must not collide with an existing part (silent duplicate).
		if doc, err := Parse(src); err == nil && findPart(doc, newID) != nil {
			return src, fmt.Errorf("set-field: id %q is already in use", newID)
		}
	}
	// Guard: demoting a populated container (group/boundary) to a non-container
	// shape would silently orphan its children (the renderer drops them). Refuse.
	if newShape, ok := fields["shape"]; ok && op.Target == "node" {
		if doc, err := Parse(src); err == nil {
			if p := findPart(doc, op.ID); p != nil && len(p.Parts) > 0 && !isContainerShape(newShape) {
				return src, fmt.Errorf("set-field: cannot change %q to non-container shape %q while it holds %d nested part(s); reparent or delete them first", op.ID, newShape, len(p.Parts))
			}
		}
	}

	// Editing the canvas when no `canvas:` block exists yet: create an empty
	// one at the top so the writes below have somewhere to land.
	if op.Target == "canvas" && yamledit.FindCanvasLine(out) < 0 {
		out = "canvas: {}\n" + out
	}

	// An id rename is handled LAST and specially — via RenamePart, which also
	// rewrites every connector/place/annotation reference (a plain SetField
	// would change only the key and strand the references). Pull it out so the
	// other field writes still resolve the target by the original op.ID.
	newID, renameID := "", false
	if v, ok := fields["id"]; ok && op.Target == "node" && v != "" && v != op.ID {
		newID, renameID = v, true
		delete(fields, "id")
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic regardless of map order
	for _, key := range keys {
		line, ok := targetLine(out, op)
		if !ok {
			return src, fmt.Errorf("set-field: target must be node|edge|canvas")
		}
		if line < 0 {
			return src, fmt.Errorf("set-field: target %q not found in source", op.ID)
		}
		// Re-find the target each write: a write can shift line numbers.
		// Boolean-typed fields are written BARE (faceSplit: true, not "true"),
		// since a quoted reserved word would fail to parse back into *bool.
		if boolFieldPaths[key] {
			out, _ = yamledit.SetFieldRaw(out, line, strings.Split(key, "."), fields[key])
		} else {
			out, _ = yamledit.SetField(out, line, strings.Split(key, "."), fields[key])
		}
	}
	if renameID {
		out = yamledit.RenamePart(out, op.ID, newID)
	}
	return []byte(out), nil // ApplyOpText applies the universal unparseable-guard
}

// findPart locates a composite part by id anywhere in the document's node
// trees, or nil if no part carries that id (a bare top-level node is not a
// part). Mirrors the walk in ResolvePartStyle.
func findPart(doc *Document, id string) *CompositePart {
	if doc == nil {
		return nil
	}
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
	for _, n := range doc.Nodes {
		if n == nil {
			continue
		}
		walk(n.Parts)
		if found != nil {
			break
		}
	}
	return found
}

// partIsContainer reports whether a part is a container — a group/boundary
// substrate or any part that actually wraps nested parts. Structural edits are
// blocked on these: duplicating one clones its children with COLLIDING ids
// (the rename touches only the container's own id), and deleting one removes a
// whole lane and its contents in a single click. Edit the YAML directly for
// those.
func partIsContainer(p *CompositePart) bool {
	return p != nil && (isContainerShape(p.Shape) || len(p.Parts) > 0)
}

func applyDelete(format string, src []byte, op EditOp) ([]byte, error) {
	s := string(src)
	switch op.Target {
	case "node":
		if doc, derr := LoadInput(context.Background(), format, src, LayoutDagre); derr == nil {
			if partIsContainer(findPart(doc, op.ID)) {
				return src, fmt.Errorf("delete: %q is a container — remove its nested parts first (deleting a whole lane from the canvas is disabled)", op.ID)
			}
		}
		// Refuse to delete a block that defines a YAML anchor still aliased
		// elsewhere — text deletion would strand the alias and corrupt the doc.
		if anchors := yamledit.ReferencedAnchorsInPart(s, op.ID); len(anchors) > 0 {
			return src, fmt.Errorf("delete: %q defines YAML anchor &%s still referenced elsewhere; inline or remove those references first", op.ID, strings.Join(anchors, ", &"))
		}
		out, ok := yamledit.DeletePart(s, op.ID)
		if !ok {
			return src, fmt.Errorf("delete: node %q not found", op.ID)
		}
		return []byte(out), nil
	case "edge":
		if start, end, found := yamledit.ConnectorItemRange(s, op.CI); found {
			return []byte(yamledit.DeleteLineRange(s, start, end)), nil
		}
		return src, fmt.Errorf("delete: connector %d not found", op.CI)
	}
	return src, fmt.Errorf("delete: target must be node|edge")
}

func applyDuplicate(format string, src []byte, op EditOp) ([]byte, error) {
	ox, oy := 40.0, 40.0
	if doc, derr := LoadInput(context.Background(), format, src, LayoutDagre); derr == nil {
		if partIsContainer(findPart(doc, op.ID)) {
			return src, fmt.Errorf("duplicate: %q is a container — duplicating it would clone its nested parts with colliding ids; duplicate the children individually", op.ID)
		}
		if cx, cy, _, found := ResolvePartOffset(doc, op.ID); found {
			ox, oy = cx+40, cy+40
		}
	}
	out, ok := yamledit.DuplicatePart(string(src), op.ID, ox, oy)
	if !ok {
		return src, fmt.Errorf("duplicate: node %q not found", op.ID)
	}
	return []byte(out), nil
}

// resolveLowered runs the renderer's own lowering pass and returns a map from
// part id to its lowered form — whose Offset is the part's absolute WORLD
// position and whose Geom carries the resolved (slab) height. Because this is
// the SAME function the renderer uses (lowerCompositeParts), any coordinate it
// reports is exactly what gets drawn — there is no second, divergent position
// model. (applyLayout first solves place/layout into concrete offsets.)
func resolveLowered(doc *Document) map[string]*CompositePart {
	if doc == nil {
		return nil
	}
	scene := doc.Scene()
	if scene == nil {
		return nil
	}
	scene = resolvedLayout(scene, doc.Canvas)
	m := map[string]*CompositePart{}
	for _, p := range lowerCompositeParts(scene.Parts, 0, 0, 0, scene.GridStep) {
		if p != nil && p.ID != "" {
			if _, exists := m[p.ID]; !exists {
				m[p.ID] = p // first wins: the group substrate, not its label part
			}
		}
	}
	return m
}

func worldXYZ(p *CompositePart) (x, y, z float64) {
	if p != nil && p.Offset != nil {
		return p.Offset.WX, p.Offset.WY, p.Offset.WZ
	}
	return 0, 0, 0
}

// childOrigin returns the absolute world position at which a child of targetID
// has its local (0,0,0). It is measured EMPIRICALLY from an existing child:
// since the renderer composes child_world = childOrigin + child_offset, any
// existing child C gives childOrigin = world(C) − authoredOffset(C). This is
// correct for every container kind — authored, layout-solved, and autosize
// (whose ±sx/sy normalisation cancels out of child world positions) — without
// special-casing any of them. Falls back to the container's lowered position +
// slab height when it has no measurable child yet. Scene root → world origin.
func childOrigin(doc *Document, lowered map[string]*CompositePart, src, targetID string) (x, y, z float64) {
	if targetID == "" {
		return 0, 0, 0
	}
	if tp := findPart(doc, targetID); tp != nil {
		for _, c := range tp.Parts {
			if c == nil || c.ID == "" {
				continue
			}
			cw := lowered[c.ID]
			if cw == nil {
				continue
			}
			if ox, oy, oz, ok := yamledit.ReadInlineOffset(src, yamledit.FindPartIDLine(src, c.ID)); ok {
				wx, wy, wz := worldXYZ(cw)
				gx, gy := partGridOffset(c, doc.Scene().GridStep)
				return wx - ox - gx, wy - oy - gy, wz - oz
			}
		}
	}
	// Fallback: no measurable existing child — derive from the container's own
	// lowered world position plus its slab height (children sit on the slab).
	tp := lowered[targetID]
	if tp == nil {
		return 0, 0, 0
	}
	x, y, z = worldXYZ(tp)
	slabH := defaultGroupSubstrateH
	if tp.Geom != nil && tp.Geom.H > 0 {
		slabH = tp.Geom.H
	}
	return x, y, z + slabH
}

// applyReparent moves a node into another group (op.Target) or to the scene
// root (op.Target == ""). It's the engine half of Studio's drag-into / drag-out
// of a group. It is POSITION-PRESERVING into ANY container: the node keeps its
// exact on-screen position, computed from the renderer's own world-coordinate
// resolver (resolveLowered), so the result can't drift from what's drawn and the
// scene bbox/viewBox — hence every other node — is unaffected.
func applyReparent(format string, src []byte, op EditOp) ([]byte, error) {
	doc, derr := LoadInput(context.Background(), format, src, LayoutDagre)
	// Capture layout-ness of the source/target groups NOW, before resolveLowered
	// runs applyLayout (which solves and clears the Layout field on the doc).
	oldParent := ""
	oldHasLayout, targetHasLayout := false, false
	var referrers []string // nodes whose place: references op.ID
	if derr == nil {
		if findPart(doc, op.ID) == nil {
			return src, fmt.Errorf("reparent: node %q not found", op.ID)
		}
		oldParent = parentOf(doc, op.ID)
		oldHasLayout = oldParent != "" && GroupHasLayout(doc, oldParent)
		targetHasLayout = op.Target != "" && GroupHasLayout(doc, op.Target)
		referrers = placeReferrers(doc, op.ID) // capture before applyLayout clears Place
		// No-op when the parent doesn't actually change, so an in-group drag
		// (which also fires a reparent to the same group) keeps its offset.
		if oldParent == op.Target {
			return src, nil
		}
		if op.Target != "" {
			if !partIsContainer(findPart(doc, op.Target)) {
				return src, fmt.Errorf("reparent: target %q is not a container", op.Target)
			}
			// A container can't be moved into its own subtree (it would orphan
			// itself); refuse op.Target that lives inside op.ID.
			if moving := findPart(doc, op.ID); partContains(moving, op.Target) {
				return src, fmt.Errorf("reparent: cannot move %q into its own descendant %q", op.ID, op.Target)
			}
		}
	}
	out, ok := yamledit.MovePart(string(src), op.ID, op.Target)
	if !ok {
		return src, fmt.Errorf("reparent: could not move %q into %q", op.ID, op.Target)
	}

	// Position-preserving reparent. MovePart strips the node's inline offset;
	// re-home it at the SAME world position the renderer currently draws it, so
	// it stays exactly where it was regardless of the destination's layout/
	// autosize behaviour. New offset = node world − the world origin the new
	// parent gives its children. World position is unchanged ⇒ scene bbox is
	// unchanged ⇒ no other node moves.
	if doc != nil {
		if lowered := resolveLowered(doc); lowered[op.ID] != nil {
			wx, wy, wz := worldXYZ(lowered[op.ID])
			cox, coy, coz := childOrigin(doc, lowered, string(src), op.Target)

			// A layout-mode group (row/col/grid) on EITHER end re-arranges its
			// children when membership changes, which would move every sibling.
			// Freeze each such group's CURRENT solved positions into explicit
			// offsets so the move is local: the siblings stay put and the
			// preserving offset written below is honoured (no solver to override
			// it). Mirrors applyMove's layout-group handling.
			ident := func(v float64) float64 { return v }
			if oldHasLayout {
				offs := ResolveChildOffsets(doc, oldParent)
				delete(offs, op.ID) // it's leaving; its offset is set below
				out = yamledit.FreezeGroupLayoutText(out, oldParent)
				out = freezeOffsets(out, offs, "", 0, 0, ident)
			}
			if targetHasLayout {
				out = yamledit.FreezeGroupLayoutText(out, op.Target)
				out = freezeOffsets(out, ResolveChildOffsets(doc, op.Target), "", 0, 0, ident)
			}

			// Reconcile place: references. The moved node may carry a place ref to
			// a former sibling (now dangling), and OTHER nodes may reference the
			// moved node and no longer be its siblings. For each such node freeze
			// its current position into an explicit offset and drop the place key,
			// so the reference can't error and the node stays put. (The moved
			// node's own offset is written below; just drop its place here.)
			out = yamledit.RemovePartKey(out, op.ID, "place")
			for _, ref := range referrers {
				if ref == op.ID || parentOf(doc, ref) == op.Target {
					continue // still a sibling of the moved node — ref stays valid
				}
				lw := lowered[ref]
				if lw == nil {
					continue
				}
				rx, ry, rz := worldXYZ(lw)
				rcox, rcoy, rcoz := childOrigin(doc, lowered, string(src), parentOf(doc, ref))
				out = yamledit.RemovePartKey(out, ref, "place")
				if line := yamledit.FindPartIDLine(out, ref); line >= 0 {
					gx, gy := partGridOffset(findPart(doc, ref), doc.Scene().GridStep)
					if o2, ok2 := yamledit.UpsertInlineKey(out, line, "offset", rx-rcox-gx, ry-rcoy-gy, rz-rcoz); ok2 {
						out = o2
					}
				}
			}

			// Write the moved node's preserving offset LAST so it wins over any
			// stale value a freeze pass may have written for it.
			if line := yamledit.FindPartIDLine(out, op.ID); line >= 0 {
				gx, gy := partGridOffset(findPart(doc, op.ID), doc.Scene().GridStep)
				if o2, ok2 := yamledit.UpsertInlineKey(out, line, "offset", wx-cox-gx, wy-coy-gy, wz-coz); ok2 {
					out = o2
				}
			}
		}
	}
	return []byte(out), nil
}

// placeReferrers returns the ids of parts whose place: relation references the
// given id (rightOf/leftOf/above/behind/inFrontOf). Captured before applyLayout
// so reparent can reconcile refs that a scope change would dangle.
func placeReferrers(doc *Document, id string) []string {
	if doc == nil || id == "" {
		return nil
	}
	var out []string
	var walk func(ps []*CompositePart)
	walk = func(ps []*CompositePart) {
		for _, p := range ps {
			if p == nil {
				continue
			}
			if pl := p.Place; pl != nil && p.ID != "" &&
				(pl.RightOf == id || pl.LeftOf == id || pl.Above == id || pl.Behind == id || pl.InFrontOf == id) {
				out = append(out, p.ID)
			}
			walk(p.Parts)
		}
	}
	if sc := doc.Scene(); sc != nil {
		walk(sc.Parts)
	}
	return out
}

// parentOf returns the id of the container holding the given part, or "" if it
// sits at the scene root (or isn't found).
func parentOf(doc *Document, id string) string {
	if doc == nil {
		return ""
	}
	var found string
	var walk func(parentID string, ps []*CompositePart)
	walk = func(parentID string, ps []*CompositePart) {
		for _, p := range ps {
			if p == nil || found != "" {
				continue
			}
			if p.ID == id {
				found = parentID
				return
			}
			walk(p.ID, p.Parts)
		}
	}
	for _, n := range doc.Nodes {
		if n != nil {
			walk("", n.Parts)
		}
	}
	return found
}

// partContains reports whether want is p or anywhere in p's subtree.
func partContains(p *CompositePart, want string) bool {
	if p == nil {
		return false
	}
	if p.ID == want {
		return true
	}
	for _, c := range p.Parts {
		if partContains(c, want) {
			return true
		}
	}
	return false
}

// targetLine resolves an op's edit target to the source line its block starts
// on (node id / connector index / canvas block).
func targetLine(src string, op EditOp) (int, bool) {
	switch op.Target {
	case "node":
		return yamledit.FindPartIDLine(src, op.ID), true
	case "edge":
		return yamledit.FindConnectorLine(src, op.CI), true
	case "canvas":
		return yamledit.FindCanvasLine(src), true
	}
	return -1, false
}
