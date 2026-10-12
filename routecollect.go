// routecollect.go — WS-E: in-memory connector route collection.
//
// The iso scorecard (planeval_iso.go) used to reverse-engineer the engine's
// real WORLD routes by regexing the rendered SVG's data-route attributes.
// That coupling is fragile (an attribute reorder breaks the evaluator) and
// incomplete (straight/bezier connectors emit no data-route, so their score
// silently fell back to the plan view's simplified line).
//
// This file adds a pure-observation collector: while the connector injector
// (inject.go) computes each route it also records the geometry into a
// routeCollector, exposed through RenderWithRouteInfo (render.go). Collection
// NEVER changes a single SVG byte — straight edges still emit no data-route —
// so every golden stays frozen.
package isotopo

import "strconv"

// ConnectorRoute records the editable pre-clip route and the final submitted
// drawing of one composite connector in the same render pass.
//
// World holds the route's ground-plane corners (wx, wy) in the same world
// frame as the plan footprints, source→target, PRE-clip — for orthogonal
// routes these are exactly the corners serialized into the data-route
// attribute. Straight connectors record their two anchor points; bezier
// connectors record a fixed-step polyline sampled along the drawn quadratic
// (inverse-projected back to world), so the evaluator finally scores the
// curve that is actually painted.
//
// Screen holds the matching pre-clip screen-user coordinates, parallel to
// World (same length, same order).
type ConnectorRoute struct {
	Index   int          `json:"index"`   // index into the scene's Connectors (== data-connector)
	From    string       `json:"from"`    // resolved source part id (== data-from)
	To      string       `json:"to"`      // resolved target part id (== data-to)
	Routing string       `json:"routing"` // effective kind: orthogonal | orthogonal2 | bezier | straight
	World   [][2]float64 `json:"world"`   // world (wx, wy) route, source→target, pre-clip
	Screen  [][2]float64 `json:"screen"`  // screen-user route, parallel to World
	// World3D is parallel to the pre-clip tracks above. Quadratics retain the
	// existing linear-height lifting convention; it is not a solved 3-D cable.
	World3D [][3]float64 `json:"world3d"`
	// Drawing is the submitted SVG geometry AFTER trimming, clipping and
	// coincident-path reuse, at the emitter's precision. It must not be used as
	// a replacement for the authored/editable pre-clip World track.
	Drawing *ConnectorDrawing `json:"drawing"`
}

// routeCollector accumulates ConnectorRoutes while injectCompositeConnectors
// runs. A nil collector disables collection entirely (the default render
// path); a non-nil one only observes — it must never influence the SVG.
type routeCollector struct {
	labelConflicts bool // detailed diagnostics requested by report/candidate gate
	routes         []ConnectorRoute
	paint          []connectorPaintPart
	// Optional output owned by SceneResult. Legacy route-only collection does
	// not allocate or measure screen-label records.
	screenLabels *[]ScreenLabel
	// Actual committed SVG body order, indexed by paint rank. GeometryIndex
	// remains the original data-part identity; DOM order drives native hits.
	partPaint []submittedPartPaint
	// Historical planar metrics consume only World/Screen. Avoid calculating
	// body coverage for every search candidate until those consumers migrate.
	preClipOnly bool
	// Producer-owned byte locations within connector-overlay. They distinguish
	// identical arrow polygons belonging to different edges, without a screen
	// geometry search or extra public SVG attributes.
	arrowFragments []connectorArrowFragment
}

type submittedPartPaint struct {
	InstanceID    string `json:"instance_id"`
	GeometryIndex int    `json:"geometry_index"`
	PaintOrder    int    `json:"paint_order"`
}

type connectorArrowFragment struct {
	index, offset int
	markup        string
}

// add records one connector's route. Routes with fewer than two points carry
// no geometry and are skipped — mirroring the regex path, which only ever saw
// connectors whose data-route held at least two corners.
func (rc *routeCollector) add(index int, from, to, routing string, world, screen [][2]float64) *ConnectorRoute {
	if rc == nil || len(world) < 2 || len(world) != len(screen) {
		return nil
	}
	rc.routes = append(rc.routes, ConnectorRoute{
		Index:   index,
		From:    from,
		To:      to,
		Routing: routing,
		World:   append([][2]float64(nil), world...),
		Screen:  append([][2]float64(nil), screen...),
	})
	return &rc.routes[len(rc.routes)-1]
}

// isoUnproject inverts projectIso at a known world z: given a screen-user
// point (sx, sy) WITHOUT the parts-origin translation, it returns the world
// (wx, wy) that projects there. Exact up to projectIso's 1e-6 quantization.
//
//	sx = wx·cos30 − wy·cos30   →  wx − wy = sx / cos30
//	sy = wx·sin30 + wy·sin30 − wz  →  wx + wy = (sy + wz) / sin30
func isoUnproject(sx, sy, wz float64) (wx, wy float64) {
	a := sx / cos30        // wx − wy
	b := (sy + wz) / sin30 // wx + wy
	return (a + b) / 2, (b - a) / 2
}

// bezierRouteSamples preserves the historical pre-clip scorecard's sample
// count. Its error is scale-dependent; it is NOT an exact final curve or a
// visibility oracle. Drawing retains the emitted quadratic's actual controls.
const bezierRouteSamples = 16

// sampleBezierRoute converts a drawn bezier connector into parallel
// screen/world polylines. pts is the emitted screen geometry ([start, ctrl,
// end] for the quadratic, or [start, end] when the chord degenerated); the
// world track is recovered by inverse-projecting each sample at a z that
// interpolates linearly between the two anchors' world heights (the same
// linear-z ruling that makes a straight world segment project to the drawn
// straight screen segment).
func sampleBezierRoute(pts [][2]float64, srcZ, tgtZ, tx, ty float64) (screen, world [][2]float64) {
	if len(pts) < 2 {
		return nil, nil
	}
	if len(pts) != 3 {
		// Degenerate chord: the injector drew a straight polyline.
		pts = [][2]float64{pts[0], pts[len(pts)-1]}
		for _, t := range []float64{0, 1} {
			p := pts[int(t)]
			wz := srcZ + t*(tgtZ-srcZ)
			wx, wy := isoUnproject(p[0]-tx, p[1]-ty, wz)
			screen = append(screen, p)
			world = append(world, [2]float64{wx, wy})
		}
		return screen, world
	}
	p0, c, p2 := pts[0], pts[1], pts[2]
	for k := 0; k <= bezierRouteSamples; k++ {
		t := float64(k) / bezierRouteSamples
		mt := 1 - t
		sx := mt*mt*p0[0] + 2*t*mt*c[0] + t*t*p2[0]
		sy := mt*mt*p0[1] + 2*t*mt*c[1] + t*t*p2[1]
		wz := srcZ + t*(tgtZ-srcZ)
		wx, wy := isoUnproject(sx-tx, sy-ty, wz)
		screen = append(screen, [2]float64{sx, sy})
		world = append(world, [2]float64{wx, wy})
	}
	return screen, world
}

// quantRoute quantizes a collected world coordinate to the exact 3-decimal
// precision the data-route attribute serializes (%.3f): a strconv round-trip,
// so the value is bit-identical to what the legacy regex path parses back out
// of the SVG. This is what lets the parity tests assert that the in-memory
// evaluator reproduces the historical scores field-for-field.
func quantRoute(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 3, 64), 64)
	return f
}
