package isotopo

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// anchorNames is the connector/annotation anchor vocabulary the resolver
// understands (see (*anchorResolver).world / .parse). Every side has a bare
// and a "-mid" spelling; "center" is an alias for the top-face centre. This is
// the single source for both validation (reject typos) and the capability
// contract (let the agent discover the set instead of guessing).
var anchorNames = []string{
	"top", "top-mid", "center",
	"bottom", "bottom-mid",
	"left", "left-mid", "right", "right-mid",
	"back", "back-mid", "front", "front-mid",
	"port-left", "port-back", "port-right", "port-front",
}

// anchorResolver resolves a connector endpoint reference ("partID" or
// "partID.anchor") to world / screen coordinates, exit normals and fan-out
// keys. It was lifted verbatim out of injectCompositeConnectors — a family of
// read-only closures over the parts index — so the anchor subsystem is a named,
// independently testable unit instead of ~200 lines inline in an 800-line
// function. All methods are pure reads of byID / tx / ty.
type anchorResolver struct {
	byID     map[string]partInfo
	tx, ty   float64           // parts→screen origin translation (for screen())
	parentOf map[string]string // nested part ID → scene-level container ID (Fix 2)
}

func newAnchorResolver(infos []partInfo, tx, ty float64) *anchorResolver {
	byID := make(map[string]partInfo, len(infos))
	for _, p := range infos {
		if p.id != "" {
			byID[p.id] = p
		}
	}
	return &anchorResolver{byID: byID, tx: tx, ty: ty, parentOf: nil}
}

// parse splits "partID.anchor" into (id, anchor). Bare "partID" defaults to
// "top-mid".
func (ar *anchorResolver) parse(ref string) (id, anchor string) {
	dot := strings.Index(ref, ".")
	if dot < 0 {
		return ref, "top-mid"
	}
	return ref[:dot], ref[dot+1:]
}

// world returns the iso-world anchor coords for ref = "partID" or
// "partID.anchor". Anchors default to the top-face centre.
func (ar *anchorResolver) world(ref string) (wx, wy, wz float64, ok bool) {
	id, anchor := ar.parse(ref)
	p, found := ar.byID[id]
	if !found {
		return 0, 0, 0, false
	}
	if side, fixed := fixedPortSide(anchor); fixed {
		return contourPort(p, side)
	}
	wx, wy, wz = p.offWX+p.w/2, p.offWY+p.d/2, p.offWZ+p.h
	switch anchor {
	// 2.5D ground routing: side anchors sit at the face's BOTTOM edge (z=offWZ)
	// so connectors run along the ground plane and read as physical links rather
	// than bridges flying over the tops. Horizontal position stays the face mid.
	case "left-mid", "left":
		wx, wy, wz = p.offWX, p.offWY+p.d/2, p.offWZ
	case "right-mid", "right":
		wx, wy, wz = p.offWX+p.w, p.offWY+p.d/2, p.offWZ
	case "back-mid", "back":
		wx, wy, wz = p.offWX+p.w/2, p.offWY, p.offWZ
	case "front-mid", "front":
		wx, wy, wz = p.offWX+p.w/2, p.offWY+p.d, p.offWZ
	case "top-mid", "top", "center":
		if p.body != nil && p.body.TopAnchor != nil {
			a := *p.body.TopAnchor
			wx, wy, wz = p.offWX+a[0], p.offWY+a[1], p.offWZ+a[2]
		}
	case "bottom-mid", "bottom":
		wz = p.offWZ
	}
	return wx, wy, wz, true
}

// exit returns the unit outward-normal of an anchor in the iso world's (x, y)
// ground plane. top/bottom/center have no horizontal normal — caller falls
// back to the x-axis.
func (ar *anchorResolver) exit(ref string) (dx, dy float64) {
	_, anchor := ar.parse(ref)
	if side, fixed := fixedPortSide(anchor); fixed {
		return portDirection(side)
	}
	switch anchor {
	case "left-mid", "left":
		return -1, 0
	case "right-mid", "right":
		return 1, 0
	case "back-mid", "back":
		return 0, -1
	case "front-mid", "front":
		return 0, 1
	}
	return 1, 0
}

// baseZ returns the part's ground level (world z of its bottom face) — the
// height the ground-plane router walks so connectors hug the floor and read as
// links lying on the ground, occluded by the bodies they pass behind.
func (ar *anchorResolver) baseZ(ref string) float64 {
	id, _ := ar.parse(ref)
	p, found := ar.byID[id]
	if !found {
		return 0
	}
	return p.offWZ
}

// refineSilhouette docks cardinal ports on the actual projected body. The
// inverse keeps the routing height fixed; this is a screen docking contract,
// not a general physical surface-port solver. Top/centre intent is retained.
func (ar *anchorResolver) refineSilhouette(ref string, wx, wy, z float64) (float64, float64) {
	if isFixedPortRef(ref) {
		return wx, wy
	}
	id, anchor := ar.parse(ref)
	p, found := ar.byID[id]
	if !found {
		return wx, wy
	}
	// The rendered curved bodies are not their world bounding boxes. Resolve
	// cardinal ports against the actual projected union, then invert at the
	// requested routing height. Keep top/centre anchors at the author's point.
	if anchor == "left" || anchor == "left-mid" || anchor == "right" || anchor == "right-mid" ||
		anchor == "front" || anchor == "front-mid" || anchor == "back" || anchor == "back-mid" {
		switch p.shape {
		case "circle", "cylinder", "stored_data", "queue", "person", "custom_path", "cloud":
			if x, y, ok := projectedBodyAnchor(p, wx, wy, z); ok {
				return x, y
			}
		}
	}
	// v3.2.1 — prism family: bbox face-midpoints sit OUTSIDE the inscribed
	// polygon (hexagon's left-mid is in empty canvas) or on a vertex, piling
	// arrowheads and detaching exits. Re-anchor on the base polygon along the
	// bbox-anchor direction.
	if sides := prismSidesFor(p.shape, p.sides); sides >= 3 && (p.shape == "triprism" || p.shape == "hexprism" || p.shape == "octprism") {
		dx, dy := wx-(p.offWX+p.w/2), wy-(p.offWY+p.d/2)
		ax, ay := iso25d.PrismGroundAnchor(p.w, p.d, sides, dx, dy)
		return p.offWX + ax, p.offWY + ay
	}

	return wx, wy
}

func (ar *anchorResolver) screen(ref string) (float64, float64, bool) {
	wx, wy, wz, ok := ar.world(ref)
	if !ok {
		return 0, 0, false
	}
	x, y := projectIso(wx, wy, wz)
	return x + ar.tx, y + ar.ty, true
}

// sideKey normalises "id.left-mid" and "id.left" to one key so multiple
// connectors touching the same side cluster together in the fan-out accounting.
func (ar *anchorResolver) sideKey(ref string) string {
	id, anchor := ar.parse(ref)
	switch anchor {
	case "left", "left-mid":
		anchor = "left"
	case "right", "right-mid":
		anchor = "right"
	case "back", "back-mid":
		anchor = "back"
	case "front", "front-mid":
		anchor = "front"
	case "top", "top-mid", "center":
		anchor = "top"
	case "bottom", "bottom-mid":
		anchor = "bottom"
	}
	return id + "/" + anchor
}

// auto resolves a bare ref (no ".anchor") to the side face FACING the other
// endpoint. v3.0 — bare refs used to pin to top-mid with a hard-coded +x exit
// normal: a target sitting to the LEFT still made the route walk +x for the
// 24-unit stub and double back, and every spoke into a hub converged on one
// point.
func (ar *anchorResolver) auto(ref, otherRef string) string {
	if strings.Contains(ref, ".") {
		return ref
	}
	p, ok := ar.byID[ref]
	if !ok {
		return ref
	}
	oid, _ := ar.parse(otherRef)
	o, ok2 := ar.byID[oid]
	if !ok2 {
		return ref
	}
	dx := (o.offWX + o.w/2) - (p.offWX + p.w/2)
	dy := (o.offWY + o.d/2) - (p.offWY + p.d/2)
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return ref + ".right"
		}
		return ref + ".left"
	}
	if dy >= 0 {
		return ref + ".front"
	}
	return ref + ".back"
}

// promoteToContainer promotes a bare ref to its scene-level container ID when
// the other endpoint (otherID) is outside the same container. This is used for
// the source side of a connector to ensure the route exits the boundary cleanly
// rather than starting at the nested part's interior position and crossing
// sibling parts. Only the source is promoted, not the destination.
func (ar *anchorResolver) promoteToContainer(ref, otherRef string) string {
	if ar.parentOf == nil || strings.Contains(ref, ".") {
		return ref
	}
	id, _ := ar.parse(ref)
	parentID, nested := ar.parentOf[id]
	if !nested {
		return ref
	}
	oid, _ := ar.parse(otherRef)
	otherParent := ar.parentOf[oid]
	// Only promote when the other endpoint is outside the same container.
	if otherParent == parentID || oid == parentID {
		return ref
	}
	// Return parentID with the correct facing anchor.
	p, ok := ar.byID[parentID]
	if !ok {
		return ref
	}
	o, ok2 := ar.byID[oid]
	if !ok2 {
		return parentID
	}
	dx := (o.offWX + o.w/2) - (p.offWX + p.w/2)
	dy := (o.offWY + o.d/2) - (p.offWY + p.d/2)
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return parentID + ".right"
		}
		return parentID + ".left"
	}
	if dy >= 0 {
		return parentID + ".front"
	}
	return parentID + ".back"
}
