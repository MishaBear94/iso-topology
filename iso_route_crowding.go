package isotopo

import (
	"math"
	"sort"
)

// IsoRouteCrowding observes submitted connector geometry after known opaque fills
// in the actual painter order (or explicit experimental body-depth clips),
// before connector/connector and text compositing. Long near
// parallel strokes and arrow contacts hinder following distinct relationships;
// ordinary transverse stroke crossings are outside this scope. This is a
// conservative geometric clearance observation, not a pixel visibility proof.
type IsoRouteCrowding struct {
	Scope     string              `json:"scope"`
	Status    string              `json:"status"` // complete | unresolved
	Reason    string              `json:"reason,omitempty"`
	Conflicts []PlanRouteConflict `json:"conflicts"`
	ink       map[int]isoCrowdingInk
	contacts  visualDefects
}

const isoRouteCrowdingScope = "submitted-paint-visible-envelope,clearance-6,parallel-cos-0.98,overlap-length-12,triangle-marker,shared-world-dock"

type isoCrowdingStroke struct {
	region screenRegion
	dir    [2]float64
}

type isoCrowdingInk struct {
	strokes    []isoCrowdingStroke
	stroke     screenRegion
	arrow      screenRegion
	box        occRect
	raw, total screenRegion
}

func isoCrowdingRegionFinite(region screenRegion) bool {
	for _, poly := range region {
		if !connectorFinite(polygonSignedArea(poly)) {
			return false
		}
		for _, p := range poly {
			if !connectorFinite(p[0]) || !connectorFinite(p[1]) {
				return false
			}
		}
	}
	return true
}

// Clip the ink FIRST, then expand surviving pieces for the reading gap. A
// completely hidden stroke must not create a clearance obstacle in empty air.
func isoCrowdingVisible(ink, clip screenRegion, work *int) (screenRegion, bool) {
	visible, ok := planCrowdingIntersection(ink, clip, nil, work)
	if !ok {
		return nil, false
	}
	return visible, isoCrowdingRegionFinite(visible)
}

func isoCrowdingGeometry(d *ConnectorDrawing, paint []connectorPaintPart, work *int) (isoCrowdingInk, string) {
	var out isoCrowdingInk
	if d == nil || d.Kind != "polyline" {
		return out, "submitted polyline unavailable"
	}
	if d.Spatial != nil && d.Spatial.Mode != "depth" {
		return out, "experimental body-depth visibility unavailable"
	}
	if len(d.Points) < 2 || len(d.Points) > 512 || !connectorFinite(d.StrokeWidth) || d.StrokeWidth <= 0 ||
		!connectorFinite(d.ProjectionOrigin[0]) || !connectorFinite(d.ProjectionOrigin[1]) || !isoCrowdingRegionFinite(screenRegion{d.Points}) {
		return out, "invalid submitted stroke geometry"
	}
	var clip, masks screenRegion
	points := make([][2]float64, len(d.Points))
	for i, p := range d.Points {
		points[i] = [2]float64{p[0] - d.ProjectionOrigin[0], p[1] - d.ProjectionOrigin[1]}
	}
	if d.Spatial != nil {
		clip = d.Spatial.ClipRegion
		if !isoCrowdingRegionFinite(clip) {
			return out, "invalid body-depth clip geometry"
		}
	} else {
		if d.PaintBeforePart < 0 || d.PaintBeforePart > len(paint) {
			return out, "incomplete paint order"
		}
		for _, p := range paint[d.PaintBeforePart:] {
			if p.uncertain || !connectorFinite(p.error) || p.error < 0 || !isoCrowdingRegionFinite(p.region) {
				return out, "uncertain opaque body coverage"
			}
			for _, poly := range p.region {
				ccw := spatialCCW(poly)
				for i, a := range ccw {
					b, c := ccw[(i+1)%len(ccw)], ccw[(i+2)%len(ccw)]
					if (b[0]-a[0])*(c[1]-b[1])-(b[1]-a[1])*(c[0]-b[0]) < -1e-8 {
						return out, "non-convex opaque body coverage"
					}
				}
				// Only the definitely opaque core hides the stroke. Keep the
				// polygon approximation and emitted-coordinate error visible.
				core, ok := painterExpand(ccw, -p.error-.02, work)
				if !ok {
					return out, "opaque core budget"
				}
				if len(core) > 0 {
					masks = append(masks, core)
				}
			}
		}
	}
	for _, s := range connectorTextConflictStrokes(connectorTextStrokes(0, points, false, svgCoord(d.StrokeWidth))) {
		dx, dy := s.b[0]-s.a[0], s.b[1]-s.a[1]
		length := math.Hypot(dx, dy)
		if length == 0 {
			continue
		}
		pieces, ok := spatialRibbon(s.a, s.b, 0, 0, s.radius)
		if !ok || !connectorFinite(length) {
			return out, "stroke envelope budget"
		}
		var ink screenRegion
		for _, p := range pieces {
			ink = append(ink, p.poly)
		}
		var visible screenRegion
		if d.Spatial != nil {
			visible, ok = isoCrowdingVisible(ink, clip, work)
		} else {
			// Intersect each convex primitive with itself to apply the same
			// bounded subtraction kernel without inventing a scene-sized box.
			for _, p := range ink {
				var pieces screenRegion
				pieces, ok = planCrowdingIntersection(screenRegion{p}, screenRegion{p}, masks, work)
				if !ok {
					break
				}
				for _, poly := range pieces {
					visible = append(visible, poly)
				}
			}
		}
		if !ok || !isoCrowdingRegionFinite(visible) {
			return out, "visible stroke intersection budget"
		}
		out.raw = append(out.raw, visible...)
		out.total = append(out.total, ink...)
		expanded := make(screenRegion, len(visible))
		for i, p := range visible {
			expanded[i] = planCrowdingEnvelope(p, 3)
		}
		visible = expanded
		out.strokes = append(out.strokes, isoCrowdingStroke{visible, [2]float64{dx / length, dy / length}})
		out.stroke = append(out.stroke, visible...)
	}
	if len(d.Arrow) != 0 {
		if len(d.Arrow) != 3 || !isoCrowdingRegionFinite(screenRegion{d.Arrow}) {
			return out, "invalid submitted arrow"
		}
		triangle := make([][2]float64, len(d.Arrow))
		for i, p := range d.Arrow {
			triangle[i] = [2]float64{p[0] - d.ProjectionOrigin[0], p[1] - d.ProjectionOrigin[1]}
		}
		triangle = spatialCCW(triangle)
		if d.Spatial != nil {
			if d.Spatial.Arrow == nil || !isoCrowdingRegionFinite(d.Spatial.Arrow.ClipRegion) {
				return out, "submitted arrow visibility unavailable"
			}
			var ok bool
			out.arrow, ok = isoCrowdingVisible(screenRegion{triangle}, d.Spatial.Arrow.ClipRegion, work)
			if !ok {
				return out, "visible arrow intersection budget"
			}
			for i, p := range out.arrow {
				out.arrow[i] = planCrowdingEnvelope(p, 3)
			}
		} else {
			// Public arrows paint in the overlay after all body fills.
			out.arrow = screenRegion{planCrowdingEnvelope(triangle, 3)}
		}
	}
	out.box = regionBounds(append(append(screenRegion{}, out.stroke...), out.arrow...))
	return out, ""
}

func isoCrowdingDocks(a, b *ConnectorDrawing) screenRegion {
	if a.Attachments == nil || b.Attachments == nil {
		return nil
	}
	x, y := a.Attachments, b.Attachments
	// Parallel semantic edges never lose their entire short route to two docks.
	if (x.Source.InstanceID == y.Source.InstanceID && x.Target.InstanceID == y.Target.InstanceID) ||
		(x.Source.InstanceID == y.Target.InstanceID && x.Target.InstanceID == y.Source.InstanceID) {
		return nil
	}
	radius := 12 + (svgCoord(a.StrokeWidth)+svgCoord(b.StrokeWidth))/2 + 6
	var masks screenRegion
	for _, p := range []ConnectorEndpointAttachment{x.Source, x.Target} {
		for _, q := range []ConnectorEndpointAttachment{y.Source, y.Target} {
			if p.InstanceID == "" || p.InstanceID != q.InstanceID || p.World.Status != "known" || q.World.Status != "known" || p.World.Point == nil || q.World.Point == nil || *p.World.Point != *q.World.Point || p.Point != q.Point {
				continue
			}
			px, py := p.Point[0]-a.ProjectionOrigin[0], p.Point[1]-a.ProjectionOrigin[1]
			masks = append(masks, planCrowdingRect(screenRect{px - radius, py - radius, px + radius, py + radius}))
		}
	}
	return masks
}

// Projection intervals are unioned before measuring length: inserting collinear
// waypoints or splitting a body clip cannot disguise a long continuous overlap.
func isoCrowdingLong(region screenRegion, dir [2]float64) bool {
	var spans [][2]float64
	for _, poly := range region {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, p := range poly {
			s := p[0]*dir[0] + p[1]*dir[1]
			lo, hi = math.Min(lo, s), math.Max(hi, s)
		}
		if hi > lo {
			spans = append(spans, [2]float64{lo, hi})
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, span := range spans {
		if span[0] > hi+1e-8 {
			lo, hi = span[0], span[1]
		} else {
			hi = math.Max(hi, span[1])
		}
		if hi-lo > 12 {
			return true
		}
	}
	return false
}

func observeIsoRouteCrowding(routes []ConnectorRoute, paint []connectorPaintPart) *IsoRouteCrowding {
	out := &IsoRouteCrowding{Scope: isoRouteCrowdingScope, Status: "complete", Conflicts: []PlanRouteConflict{}, ink: map[int]isoCrowdingInk{}, contacts: visualDefects{}}
	if len(routes) < 2 {
		return out
	}
	fail := func(reason string) *IsoRouteCrowding { out.Status, out.Reason = "unresolved", reason; return out }
	if len(routes) > 512 {
		return fail("route pair budget")
	}
	work, points := 1000000, 0
	for _, p := range paint {
		for _, poly := range p.region {
			points += len(poly)
		}
	}
	origin := [2]float64{}
	if routes[0].Drawing != nil {
		origin = routes[0].Drawing.ProjectionOrigin
	}
	experimental := routes[0].Drawing != nil && routes[0].Drawing.Spatial != nil
	if experimental {
		out.Scope = "experimental-body-depth," + out.Scope
	}
	ink := make([]isoCrowdingInk, len(routes))
	for i, r := range routes {
		if r.Drawing != nil {
			if r.Drawing.ProjectionOrigin != origin || (r.Drawing.Spatial != nil) != experimental {
				return fail("inconsistent submitted coordinate or paint frame")
			}
			points += len(r.Drawing.Points)
			if r.Drawing.Spatial != nil {
				for _, poly := range r.Drawing.Spatial.ClipRegion {
					points += len(poly)
				}
				if r.Drawing.Spatial.Arrow != nil {
					for _, poly := range r.Drawing.Spatial.Arrow.ClipRegion {
						points += len(poly)
					}
				}
			}
		}
		if points > 65536 {
			return fail("scene geometry budget")
		}
		var reason string
		ink[i], reason = isoCrowdingGeometry(r.Drawing, paint, &work)
		if reason != "" {
			return fail(reason)
		}
		out.ink[r.Index] = ink[i]
	}
	for i, a := range routes {
		for j := i + 1; j < len(routes); j++ {
			b := routes[j]
			if !painterBoundsMeet(ink[i].box, ink[j].box) {
				continue
			}
			masks := isoCrowdingDocks(a.Drawing, b.Drawing)
			var parallel, incidental screenRegion
			for _, p := range ink[i].strokes {
				for _, q := range ink[j].strokes {
					work--
					if work < 0 {
						return fail("route intersection budget")
					}
					r, ok := planCrowdingIntersection(p.region, q.region, masks, &work)
					if !ok {
						return fail("route intersection budget")
					}
					if math.Abs(p.dir[0]*q.dir[0]+p.dir[1]*q.dir[1]) >= .98 && isoCrowdingLong(r, p.dir) {
						parallel = append(parallel, r...)
					} else {
						incidental = append(incidental, r...)
					}
				}
			}
			// Ordinary crossings are not public crowding warnings. Preserve
			// their pair identity privately so uncrowding cannot introduce a
			// new remote X crossing between two edges sharing a source.
			if len(incidental) > 256 {
				return fail("incidental contact budget")
			}
			area := regionArea(incidental)
			if !connectorFinite(area) {
				return fail("non-finite incidental contact")
			}
			if area > 1e-7 {
				out.contacts[visualDefect{kind: "iso-route-incidental-contact", edge: a.Index, peer: b.Index}] = area
			}
			for _, pair := range []struct {
				kind string
				a, b screenRegion
			}{
				{"parallel-stroke", nil, nil}, {"stroke-arrow", ink[i].stroke, ink[j].arrow},
				{"arrow-stroke", ink[i].arrow, ink[j].stroke}, {"arrow-arrow", ink[i].arrow, ink[j].arrow},
			} {
				r, ok := parallel, true
				if pair.kind != "parallel-stroke" {
					r, ok = planCrowdingIntersection(pair.a, pair.b, masks, &work)
				}
				if !ok || len(r) > 256 {
					return fail("route intersection budget")
				}
				area := regionArea(r)
				if !connectorFinite(area) {
					return fail("non-finite intersection area")
				}
				if area > 1e-7 {
					box := regionBounds(r)
					out.Conflicts = append(out.Conflicts, PlanRouteConflict{a.Index, b.Index, pair.kind, ScreenLabelRect{box.x0 + origin[0], box.y0 + origin[1], box.x1 - box.x0, box.y1 - box.y0}, area})
				}
			}
		}
	}
	return out
}

func isoCrowdingDefects(c *IsoRouteCrowding) visualDefects {
	d := visualDefects{}
	for k, v := range c.contacts {
		d[k] = v
	}
	if c.Status != "complete" {
		d[visualDefect{kind: "iso-route-clearance-unresolved"}] = 1
	}
	for _, v := range c.Conflicts {
		d[visualDefect{kind: "iso-route-" + v.Kind, edge: v.ConnectorIndex, peer: v.PeerIndex}] = v.Area
	}
	return d
}
