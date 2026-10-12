package isotopo

import (
	"github.com/MarkovWangRR/iso-topology/iso25d"
	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// interact.go — server-side interaction model for Studio's direct-manipulation
// layer.
//
// The server emits resolved world boxes, parent identities and projected
// body/content regions alongside every rendered SVG. Studio uses these for
// container targeting, instance-aware drag previews and polygon marquee tests.
// Screen-oriented labels are delivered separately by SceneResult from the
// same SVG emission; Studio combines them with these world regions. Connection ports
// share the renderer's fixed contour-port resolver; legacy Anchors are kept
// only for compatibility with older consumers.

// PartModel describes one source part after layout resolution. Boxes and
// anchors use world units; SelectionRegions use projected world coordinates.
type PartModel struct {
	ID        string        `json:"id"`
	ParentID  string        `json:"parentId,omitempty"`
	Container bool          `json:"container"`
	X         float64       `json:"x"`
	Y         float64       `json:"y"`
	Z         float64       `json:"z,omitempty"`
	W         float64       `json:"w"`
	D         float64       `json:"d"`
	H         float64       `json:"h,omitempty"`
	Anchors   []AnchorPoint `json:"anchors,omitempty"`
	// Ports use the renderer's fixed contour-port contract. Anchors above are
	// retained for older consumers; Studio uses Ports, including instance IDs.
	Ports []ConnectionPort `json:"ports"`
	// SelectionRegions are projected world polygons before canvas translation.
	// They include body and content across stack instances; never convexify the
	// union or substitute its AABB for the narrow-phase marquee hit test.
	SelectionRegions     [][][2]float64 `json:"selectionRegions"`
	SelectionApproximate bool           `json:"selectionApproximate,omitempty"`
}

// AnchorPoint is one named connection point on a part's surface.
// Name matches the anchor suffix used in connector from/to (e.g. "top",
// "left", "right", "bottom"). WX/WY/WZ are world coordinates — the full
// triple is needed so the client can apply the renderer's iso projection,
// including height. The legacy box anchors do not describe every shape's
// actual surface or the renderer's resolved connector ports.
type AnchorPoint struct {
	Name string  `json:"name"`
	WX   float64 `json:"wx"`
	WY   float64 `json:"wy"`
	WZ   float64 `json:"wz"`
}

// BuildInteractionModel returns the world-space geometry for every part in the
// document's composite scene, after layout resolution. Returns nil when the
// document has no composite scene.
//
// The caller should embed the result in the render-response JSON under the
// key "model" so Studio can read it without a separate round-trip.
func BuildInteractionModel(doc *Document) []PartModel {
	return buildInteractionModel(doc, nil)
}

func buildInteractionModel(doc *Document, plan *resolvedPlanScene) []PartModel {
	if doc == nil {
		return nil
	}
	scene := doc.Scene()
	if scene == nil {
		return nil
	}
	// Reuse resolved world geometry without routing every edge merely to
	// display handles. The resolver owns its layout copy and includes grid
	// positions, parent elevation and the slab children stand on.
	var resolved *Node
	var rects []planRect
	if plan != nil {
		resolved, rects = plan.node, plan.rects
	} else {
		resolved, rects = resolvedPlanGeometry(scene, doc.Theme, doc.Canvas)
	}
	parents := map[string]string{}
	portBodies := map[string]bool{}
	var visit func([]*CompositePart, string)
	visit = func(parts []*CompositePart, parent string) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			owner := parent
			if p.ID != "" {
				parents[p.ID] = parent
				portBodies[p.ID] = hasPortBody(p.Shape)
				owner = p.ID
			}
			visit(p.Parts, owner)
		}
	}
	visit(resolved.Parts, "")
	if len(rects) == 0 {
		return nil
	}
	out := make([]PartModel, 0, len(rects))
	byID := make(map[string]int, len(rects))
	top := doc.Canvas != nil && doc.Canvas.Projection == "top"
	for _, r := range rects {
		if r.id == "" {
			continue
		}
		pm := PartModel{
			ID:        r.id,
			ParentID:  parents[r.id],
			Container: r.container,
			X:         r.x, Y: r.y, Z: r.z,
			W: r.w, D: r.d, H: r.h,
			Anchors:          faceAnchors(r),
			SelectionRegions: make([][][2]float64, 0),
			Ports:            make([]ConnectionPort, 0),
		}
		if top {
			// Plan nodes are rectangular footprints. Their small rounded
			// corners remain an explicitly approximate selection envelope.
			pm.SelectionRegions = [][][2]float64{{{r.x, r.y}, {r.x + r.w, r.y}, {r.x + r.w, r.y + r.d}, {r.x, r.y + r.d}}}
			pm.SelectionApproximate = true
			if eligible, known := portBodies[r.id]; eligible || !known && hasPortBody(resolved.Shape) {
				for _, name := range fixedPortNames {
					port, _ := planPort(r, name)
					pm.Ports = append(pm.Ports, port)
				}
			}
		}
		byID[r.id] = len(out)
		out = append(out, pm)
	}
	if top {
		needsTitles := false
		for _, r := range rects {
			needsTitles = needsTitles || r.label != "" && textmetrics.ResolveBuiltin(r.family, r.weight) != nil
		}
		if !needsTitles {
			return out
		}
		if plan == nil {
			plan = resolvePlanSceneGeometry(resolved, rects, doc.Canvas, true, false)
		}
		ox, oy := plan.ox, plan.oy
		content := plan.layoutContent()
		for _, p := range content.texts {
			i, ok := byID[p.record.PartID]
			if !ok {
				continue
			}
			for _, r := range p.inkRects() {
				r.x0 -= ox
				r.x1 -= ox
				r.y0 -= oy
				r.y1 -= oy
				// SVG anchors round after canvas translation. Enclose that
				// subpixel difference in world-space selection geometry.
				r = r.inflate(.01)
				out[i].SelectionRegions = append(out[i].SelectionRegions, [][2]float64{{r.x0, r.y0}, {r.x1, r.y0}, {r.x1, r.y1}, {r.x0, r.y1}})
			}
		}
		return out
	}
	for _, p := range lowerCompositeParts(resolved.Parts, 0, 0, 0, resolved.GridStep) {
		i, ok := byID[stackBase(p.ID)]
		if !ok {
			continue
		}
		shape, opts := Flatten(&Node{Shape: p.Shape, Geom: p.Geom, Style: p.Style, Preset: p.Preset, Role: p.Role, Label: p.Label, Icon: p.Icon, Content: p.Content}, doc.Theme)
		body := iso25d.ShapeBody(shape, opts)
		w, d, h := iso25d.ShapeDimensions(shape, opts)
		info := partInfo{id: p.ID, shape: shape, body: &body, w: w, d: d, h: h}
		if p.Offset != nil {
			info.offWX, info.offWY, info.offWZ = p.Offset.WX, p.Offset.WY, p.Offset.WZ
		}
		out[i].SelectionRegions = append(out[i].SelectionRegions, partBodyRegion(info, 0, 0)...)
		out[i].Ports = append(out[i].Ports, instancePorts(info)...)
		text := partContentRegions(p, doc.Theme, "text")
		out[i].SelectionRegions = append(out[i].SelectionRegions, text...)
		out[i].SelectionRegions = append(out[i].SelectionRegions, partContentRegions(p, doc.Theme, "icon")...)
		out[i].SelectionApproximate = out[i].SelectionApproximate || body.Envelope || len(text) > 0
	}
	return out
}

// faceAnchors returns the four cardinal face-centre anchor points for a part.
// These are the same logical positions Studio's showHandles() was computing
// from SVG polygon vertices — now computed once in world space so the browser
// doesn't need to touch the DOM at all for anchor display.
//
// Convention (matches iso projection orientation):
//
//	top    — centre of the top (roof) face: (cx, y,   z+h)   world
//	right  — centre of the right face:       (x+w, cy, z+h/2)
//	bottom — nadir (lowest visible point):   (cx, y+d, z)
//	left   — centre of the left face:         (x, cy,  z+h/2)
//
// The z term is what puts each dot on the right face: the roof handle rides
// the top at z+h, the side handles sit at mid-height, the nadir is on the
// ground at z. projectIso subtracts z, so omitting it (as the old model did)
// dropped every handle by a full node height.
func faceAnchors(r planRect) []AnchorPoint {
	cx := r.x + r.w/2
	cy := r.y + r.d/2
	return []AnchorPoint{
		{Name: "top", WX: cx, WY: r.y, WZ: r.z + r.h},
		{Name: "right", WX: r.x + r.w, WY: cy, WZ: r.z + r.h/2},
		{Name: "bottom", WX: cx, WY: r.y + r.d, WZ: r.z},
		{Name: "left", WX: r.x, WY: cy, WZ: r.z + r.h/2},
	}
}

// WorldDropTarget returns the id of the container that a dragged node (dragID)
// should be reparented into when dropped, based on world-space footprint
// overlap — or "" if no container qualifies (drop to scene root).
//
// Algorithm: among all containers in the model (excluding dragID and its
// subtree), find the one whose AABB overlaps the dragged node's AABB the
// most (by area). A container must overlap by at least minOverlapFrac of the
// dragged node's area to qualify, preventing accidental re-homes when a node
// merely grazes a container's edge.
//
// This replaces the pixel-based elementsFromPoint hit-test in Studio's
// containerUnder(), which failed whenever the container was smaller than the
// node or visually occluded.
func WorldDropTarget(model []PartModel, dragID string, minOverlapFrac float64) string {
	// Find the dragged node's AABB.
	var drag *PartModel
	for i := range model {
		if model[i].ID == dragID {
			drag = &model[i]
			break
		}
	}
	if drag == nil {
		return ""
	}
	dragArea := drag.W * drag.D
	if dragArea <= 0 {
		return ""
	}

	// Build a quick subtree set so we never drop a container into its own child.
	// (The server applyReparent also guards this, but we should not even try.)
	subtree := map[string]bool{dragID: true}
	for changed := true; changed; {
		changed = false
		for _, pm := range model {
			if pm.ParentID != "" && subtree[pm.ParentID] && !subtree[pm.ID] {
				subtree[pm.ID], changed = true, true
			}
		}
	}

	bestID := ""
	bestArea := 0.0
	for _, pm := range model {
		if !pm.Container || subtree[pm.ID] {
			continue
		}
		ov := overlapArea(drag.X, drag.Y, drag.W, drag.D, pm.X, pm.Y, pm.W, pm.D)
		if ov/dragArea >= minOverlapFrac && ov > bestArea {
			bestArea = ov
			bestID = pm.ID
		}
	}
	return bestID
}

// overlapArea returns the area of intersection of two axis-aligned rectangles.
// x,y is the min corner; w,d are the extents (all in world units).
func overlapArea(ax, ay, aw, ad, bx, by, bw, bd float64) float64 {
	ox := min2(ax+aw, bx+bw) - max2(ax, bx)
	oy := min2(ay+ad, by+bd) - max2(ay, by)
	if ox <= 0 || oy <= 0 {
		return 0
	}
	return ox * oy
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
