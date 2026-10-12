package isotopo

import (
	"math"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// screenRegion is a UNION of polygonal faces, not a polygon with holes. Keep
// the faces separate so concave cutouts and overlapping head/body are retained.
type screenRegion [][][2]float64

func (r screenRegion) contains(p [2]float64) bool {
	for _, poly := range r {
		if len(poly) >= 3 && polygonContains(p, poly) {
			return true
		}
	}
	return false
}

func (r screenRegion) containsEndpoint(p [2]float64) bool {
	if r.contains(p) {
		return true
	}
	// Projection rounds by 1e-6. This tolerance applies ONLY to an authored
	// endpoint, never to interval occupancy (which would erase tiny gaps).
	for _, poly := range r {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			dx, dy := b[0]-a[0], b[1]-a[1]
			den := dx*dx + dy*dy
			if den == 0 {
				continue
			}
			u := math.Max(0, math.Min(1, ((p[0]-a[0])*dx+(p[1]-a[1])*dy)/den))
			if math.Hypot(p[0]-a[0]-u*dx, p[1]-a[1]-u*dy) <= 1e-6 {
				return true
			}
		}
	}
	return false
}

func partBodyRegion(p partInfo, tx, ty float64) screenRegion {
	body := p.body
	if body == nil {
		// Compatibility for callers constructing partInfo directly. Production
		// renderers carry the resolved body, including original default intent.
		b := iso25d.ShapeBody(p.shape, iso25d.ConvertOpts{Width: p.w, Depth: p.d, Height: p.h, Sides: p.sides})
		body = &b
	}
	x, y := projectIso(p.offWX, p.offWY, p.offWZ)
	r := make(screenRegion, len(body.Polygons))
	for i, poly := range body.Polygons {
		r[i] = make([][2]float64, len(poly))
		for j, q := range poly {
			r[i][j] = [2]float64{q[0] + x + tx, q[1] + y + ty}
		}
	}
	return r
}

// projectedBodyAnchor projects a nominal cardinal port onto the outermost
// body boundary along its screen direction from the body's reference centre.
// It works even if that centre is in a concave cutout. No convex hull is used.
// This is a screen docking contract; it does not assert a physical 3D face port.
func projectedBodyAnchor(p partInfo, wx, wy, z float64) (float64, float64, bool) {
	region := partBodyRegion(p, 0, 0)
	cx, cy := projectIso(p.offWX+p.w/2, p.offWY+p.d/2, p.offWZ+p.h/2)
	sx, sy := projectIso(wx, wy, z)
	dx, dy := sx-cx, sy-cy
	if math.Hypot(dx, dy) < 1e-8 {
		return wx, wy, false
	}
	best := math.Inf(-1)
	for _, poly := range region {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			ex, ey := b[0]-a[0], b[1]-a[1]
			den := dx*ey - dy*ex
			if math.Abs(den) < 1e-10 {
				continue
			}
			t := ((a[0]-cx)*ey - (a[1]-cy)*ex) / den
			u := ((a[0]-cx)*dy - (a[1]-cy)*dx) / den
			if t >= 0 && u >= -1e-9 && u <= 1+1e-9 {
				best = math.Max(best, t)
			}
		}
	}
	if math.IsInf(best, -1) {
		// A ray can miss every lobe of a concave body. In that case use the
		// closest actual boundary to the nominal port, never the empty bbox.
		distance := math.Inf(1)
		for _, poly := range region {
			for i, a := range poly {
				b := poly[(i+1)%len(poly)]
				ex, ey := b[0]-a[0], b[1]-a[1]
				den := ex*ex + ey*ey
				if den == 0 {
					continue
				}
				u := math.Max(0, math.Min(1, ((sx-a[0])*ex+(sy-a[1])*ey)/den))
				x, y := a[0]+u*ex, a[1]+u*ey
				if dist := math.Hypot(x-sx, y-sy); dist < distance {
					distance, cx, cy = dist, x, y
				}
			}
		}
		if math.IsInf(distance, 1) {
			return wx, wy, false
		}
		sx, sy = cx, cy
	} else {
		sx, sy = cx+best*dx, cy+best*dy
	}
	// projectIso(x,y,z) = (cos30*(x-y), .5*(x+y)-z).
	xMinusY, xPlusY := sx/iso25d.Cos30, 2*(sy+z)
	return (xPlusY + xMinusY) / 2, (xPlusY - xMinusY) / 2, true
}

// Clip only the connected interior portion at the endpoint. Checking all
// interval boundaries also handles a segment that exits and re-enters a
// concavity even though both its vertices are inside.
func clipRouteEndRegion(pts [][2]float64, region screenRegion, inset float64) [][2]float64 {
	return clipRouteEndRegionTracked(pts, region, inset, nil)
}

func clipRouteEndRegionTracked(pts [][2]float64, region screenRegion, inset float64, trace *connectorPathTrace) [][2]float64 {
	if len(pts) < 2 || !region.containsEndpoint(pts[len(pts)-1]) {
		return pts
	}
	for i := len(pts) - 1; i >= 1; i-- {
		a, b := pts[i-1], pts[i]
		q := quadratic{a, {(a[0] + b[0]) / 2, (a[1] + b[1]) / 2}, b}
		t, found := q.endpointBoundaryRegion(region, false)
		if !found {
			continue
		}
		entry := q.at(t)
		dx, dy := entry[0]-a[0], entry[1]-a[1]
		if length := math.Hypot(dx, dy); length > inset {
			entry[0] -= dx / length * inset
			entry[1] -= dy / length * inset
			t *= (length - inset) / length
		}
		trace.clipEnd(i-1, t)
		return append(append([][2]float64{}, pts[:i]...), entry)
	}
	return pts
}

func clipRouteStartRegion(pts [][2]float64, region screenRegion, inset float64) [][2]float64 {
	return clipRouteStartRegionTracked(pts, region, inset, nil)
}

func clipRouteStartRegionTracked(pts [][2]float64, region screenRegion, inset float64, trace *connectorPathTrace) [][2]float64 {
	rev := make([][2]float64, len(pts))
	for i, p := range pts {
		rev[len(pts)-1-i] = p
	}
	trace.reverse()
	rev = clipRouteEndRegionTracked(rev, region, inset, trace)
	trace.reverse()
	out := make([][2]float64, len(rev))
	for i, p := range rev {
		out[len(rev)-1-i] = p
	}
	return out
}
