package isotopo

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// An upper envelope of submitted bodies and measured content, in root SVG
// coordinates. Faces remain separate; neither notches nor disconnected lobes
// are replaced by a convex hull. Swept borders include round strokes, curve
// approximation error and decimal projection error.
type connectorTextBodies struct {
	interiors screenRegion
	borders   []connectorTextStroke
}

type connectorTextBodySource func() (connectorTextBodies, bool)

// This optional refinement spends at most this many support-line/segment
// root operations per scene. Exhaustion preserves the settled arrangement;
// it is not a proof of infeasibility. Full/global layout belongs off the
// synchronous render path. This is a work estimate, not a wall-clock SLA.
const connectorTextBodySearchBudget = 1024

func connectorTextInteriorHits(box screenRect, interiors screenRegion) bool {
	x, y := box.cx(), box.cy()
	// Submitted polygons use SVG's nonzero fill rule. An even/odd ray test
	// would erase a multiply-wound custom face and accept text inside it.
	for _, poly := range interiors {
		winding := 0
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			cross := (b[0]-a[0])*(y-a[1]) - (b[1]-a[1])*(x-a[0])
			if a[1] <= y && b[1] > y && cross > 0 {
				winding++
			} else if a[1] > y && b[1] <= y && cross < 0 {
				winding--
			}
		}
		if winding != 0 {
			return true
		}
	}
	return false
}

func connectorTextBodyRegions(parts []iso25d.CompositePart, infos []partInfo) (connectorTextBodies, bool) {
	var out connectorTextBodies
	if len(parts) != len(infos) {
		return out, false
	}
	tx, ty := partsScreenOrigin(infos)
	for i, p := range parts {
		o := p.Opts
		// Later screen-label placement is not part of this transaction. Unknown
		// ink/effects have no upper envelope; retain the entire prior layout.
		if infos[i].screenLabel != "" || len(o.Rows) > 0 || o.Header != "" || len(o.Cells) > 0 || o.Blur > 0 || len(o.FaceSurfaces) > 0 || o.ShadowColor != "" || o.BackglowColor != "" || o.Background != "" || o.GrainIntensity > 0 || o.PatternKind != "" {
			return connectorTextBodies{}, false
		}
		switch strings.ToLower(strings.TrimSpace(p.Shape)) {
		case "", "box", "rectangle", "square", "cylinder", "stored_data", "queue", "circle", "person", "triprism", "hexprism", "octprism", "wedge", "custom_path", "cloud":
		default:
			return connectorTextBodies{}, false
		}
		body := iso25d.ShapeBody(p.Shape, o)
		content := iso25d.ShapeContent(p.Shape, o)
		if body.Envelope || content.EstimatedText {
			return connectorTextBodies{}, false
		}
		ox, oy := projectIso(p.OffWX, p.OffWY, p.OffWZ)
		add := func(points [][2]float64, radius float64, filled bool) bool {
			if len(points) < 3 || len(out.borders)+len(points) > 16384 || math.IsNaN(radius) || math.IsInf(radius, 0) {
				return false
			}
			poly := make([][2]float64, len(points))
			for j, point := range points {
				x, y := point[0]+ox+tx, point[1]+oy+ty
				if math.IsNaN(x+y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
					return false
				}
				poly[j] = [2]float64{x, y}
			}
			if filled {
				out.interiors = append(out.interiors, poly)
			}
			for j, a := range poly {
				out.borders = append(out.borders, connectorTextStroke{owner: -1, a: a, b: poly[(j+1)%len(poly)], radius: radius})
			}
			return true
		}
		stroke := math.Max(1.5, math.Max(o.StrokeWidth, math.Max(o.TopStroke.Width, math.Max(o.LeftStroke.Width, o.RightStroke.Width))))
		stroke = math.Max(stroke, o.OutlineWidth)
		if !infos[i].isSubstrate {
			for _, poly := range body.Polygons {
				if !add(poly, stroke/2+body.MaxError+.03, true) {
					return connectorTextBodies{}, false
				}
			}
			// The custom-path renderer accents its convex silhouette, which
			// can bridge concave notches. Protect that actual ring as strokes,
			// without filling the space enclosed by the accent.
			if strings.EqualFold(strings.TrimSpace(p.Shape), "custom_path") && o.OutlineColor != "" && o.OutlineWidth > 0 {
				w, d, h := iso25d.ShapeDimensions(p.Shape, o)
				ring := (iso25d.CustomPathShapeProvider{}).Silhouette(w, d, h, o.Params)
				for j := range ring {
					ring[j][0] -= d * cos30
					ring[j][1] -= h
				}
				if !add(ring, o.OutlineWidth/2+.03, false) {
					return connectorTextBodies{}, false
				}
			}
		}
		// Image viewports bound all their pixels. Bound text quads describe
		// shaped ink, including overhangs and content outside the body itself.
		for _, element := range content.Elements {
			radius := .03
			if element.Kind == "text" {
				if element.FontSize <= 0 || math.IsNaN(element.FontSize) || math.IsInf(element.FontSize, 0) {
					return connectorTextBodies{}, false
				}
				// ShapeContent measures the requested size; SVG emits two
				// decimals. A long line magnifies even a .005 size difference.
				// Bound that scale change about the emitted text anchor instead
				// of assuming a constant coordinate-rounding allowance suffices.
				scaleError := math.Abs(svgCoord(element.FontSize)-element.FontSize) / element.FontSize
				extent := 0.
				for _, p := range element.Quad {
					extent = math.Max(extent, math.Max(math.Abs(p[0]-element.Anchor[0]), math.Abs(p[1]-element.Anchor[1])))
				}
				radius += scaleError * extent
			}
			if !add(element.Quad[:], radius, true) {
				return connectorTextBodies{}, false
			}
		}
	}
	return out, true
}

// Refine only still-blocked labels after the complete old arrangement has
// settled. Every peer retains its slot. Existing clear anchors and their
// certificates are never reinterpreted using this richer obstacle model.
func refineConnectorTextBodies(positions []connectorTextPlacement, requests []connectorTextRequest, legacyPills []screenRect, obstacles []connectorTextObstacle, strokes []connectorTextStroke, source connectorTextBodySource) []bool {
	changed := make([]bool, len(positions))
	if source == nil || len(legacyPills) != 0 {
		return changed
	}
	needed := false
	for _, p := range positions {
		needed = needed || !p.clear
	}
	if !needed {
		return changed
	}
	var bodies connectorTextBodies
	loaded := false
	remaining := connectorTextBodySearchBudget
	spend := func(req connectorTextRequest, boxes, strokes int) bool {
		segments := len(req.points) - 1
		if req.curved {
			segments = 1
		}
		// Each rectangle has four supports and each swept border at most
		// eight. Division avoids overflow for very long authored paths.
		work := 4*boxes + 8*strokes
		if segments < 1 || work > 0 && segments > remaining/work {
			return false
		}
		remaining -= work * segments
		return true
	}
	var arrows []screenRect
	for _, o := range obstacles {
		if o.owner < 0 {
			arrows = append(arrows, o.rect)
		}
	}
	for i, req := range requests {
		if positions[i].clear {
			continue
		}
		boxes := append([]screenRect(nil), arrows...)
		for j, p := range positions {
			if j != i {
				boxes = append(boxes, p.box)
			}
		}
		var other []connectorTextStroke
		for _, stroke := range strokes {
			if stroke.owner != req.index {
				other = append(other, stroke)
			}
		}
		// Body refinement cannot resolve a route/pill-only blockage. Screen
		// that cheaper problem before constructing and searching all faces.
		if !spend(req, len(boxes), len(other)) {
			return changed
		}
		x, y, clear := connectorTextPosition(req.points, req.curved, req.block.width, req.block.height, boxes, other...)
		if !clear {
			continue
		}
		if !loaded {
			var ok bool
			bodies, ok = source()
			if !ok {
				return changed
			}
			loaded = true
		}
		candidate := connectorTextBox(req, x, y)
		if connectorTextStrokeHits(candidate, bodies.borders) || connectorTextInteriorHits(candidate, bodies.interiors) {
			if !spend(req, len(boxes), len(other)+len(bodies.borders)) {
				return changed
			}
			other = append(other, bodies.borders...)
			x, y, clear = connectorTextPositionWithInteriors(req.points, req.curved, req.block.width, req.block.height, boxes, other, bodies.interiors)
		}
		if clear {
			positions[i] = connectorTextPlacement{x, y, true, connectorTextBox(req, x, y)}
			changed[i] = true
		}
	}
	return changed
}
