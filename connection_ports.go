package isotopo

import (
	"math"
	"strings"
)

// Fixed contour ports are distinct from legacy world-face anchors. A port is
// an explicit visible attachment: fan-out and endpoint clipping must not move
// it. Its world position is the inverse projection at the instance's base Z;
// this does not assert a physical point on a 3D face.
var fixedPortNames = []string{"port-left", "port-back", "port-right", "port-front"}

func hasPortBody(shape string) bool {
	switch strings.ToLower(strings.TrimSpace(shape)) {
	case "iso_text", "title", "composite":
		return false
	}
	return true
}

func fixedPortSide(name string) (string, bool) {
	if !strings.HasPrefix(name, "port-") {
		return "", false
	}
	side := strings.TrimPrefix(name, "port-")
	switch side {
	case "left", "right", "front", "back":
		return side, true
	}
	return "", false
}
func isFixedPortRef(ref string) bool {
	i := strings.IndexByte(ref, '.')
	if i < 0 {
		return false
	}
	_, ok := fixedPortSide(ref[i+1:])
	return ok
}
func portDirection(side string) (float64, float64) {
	switch side {
	case "left":
		return -1, 0
	case "back":
		return 0, -1
	case "front":
		return 0, 1
	}
	return 1, 0
}

// contourPort intersects a line through the body reference centre with the
// polygon union, taking the furthest boundary in the requested exit direction.
// The centre may be in a concave gap. When the line misses every lobe, the
// furthest support vertex is still on the body and has a clear outward ray.
func contourPort(p partInfo, side string) (wx, wy, wz float64, ok bool) {
	r := partBodyRegion(p, 0, 0)
	if len(r) == 0 {
		return 0, 0, 0, false
	}
	cx, cy := projectIso(p.offWX+p.w/2, p.offWY+p.d/2, p.offWZ+p.h/2)
	dx, dy := portDirection(side)
	sx, sy := projectIso(dx, dy, 0)
	best := math.Inf(-1)
	var x, y float64
	for _, poly := range r {
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			ex, ey := b[0]-a[0], b[1]-a[1]
			den := sx*ey - sy*ex
			if math.Abs(den) < 1e-12 {
				continue
			}
			u := ((a[0]-cx)*sy - (a[1]-cy)*sx) / den
			t := ((a[0]-cx)*ey - (a[1]-cy)*ex) / den
			if u >= -1e-9 && u <= 1+1e-9 && t > best {
				best = t
				x, y = cx+t*sx, cy+t*sy
			}
		}
	}
	if math.IsInf(best, -1) {
		for _, poly := range r {
			for _, a := range poly {
				if t := a[0]*sx + a[1]*sy; t > best {
					best = t
					x, y = a[0], a[1]
				}
			}
		}
	}
	if math.IsInf(best, -1) {
		return 0, 0, 0, false
	}
	wz = p.offWZ
	wx, wy = isoUnproject(x, y, wz)
	return wx, wy, wz, true
}

// ConnectionPort identifies the displayed instance, separately from its
// source part. Approximate marks a semantic envelope rather than a true body.
type ConnectionPort struct {
	Name        string  `json:"name"`
	InstanceID  string  `json:"instanceId"`
	WX          float64 `json:"wx"`
	WY          float64 `json:"wy"`
	WZ          float64 `json:"wz"`
	Approximate bool    `json:"approximate,omitempty"`
}

func instancePorts(p partInfo) []ConnectionPort {
	out := make([]ConnectionPort, 0, 4)
	for _, name := range fixedPortNames {
		side, _ := fixedPortSide(name)
		if x, y, z, ok := contourPort(p, side); ok {
			out = append(out, ConnectionPort{Name: name, InstanceID: p.id, WX: x, WY: y, WZ: z, Approximate: p.body != nil && p.body.Envelope})
		}
	}
	return out
}
func planPort(r planRect, name string) (ConnectionPort, bool) {
	side, ok := fixedPortSide(name)
	if !ok {
		return ConnectionPort{}, false
	}
	x, y := r.x+r.w/2, r.y+r.d/2
	switch side {
	case "left":
		x = r.x
	case "right":
		x = r.x + r.w
	case "back":
		y = r.y
	case "front":
		y = r.y + r.d
	}
	return ConnectionPort{Name: name, InstanceID: r.id, WX: x, WY: y}, true
}

func planConnectionPort(r, other planRect, ref string) ConnectionPort {
	if i := strings.IndexByte(ref, '.'); i >= 0 {
		if p, ok := planPort(r, ref[i+1:]); ok {
			return p
		}
	}
	dx, dy := other.x+other.w/2-r.x-r.w/2, other.y+other.d/2-r.y-r.d/2
	name := "port-right"
	if math.Abs(dx) >= math.Abs(dy) {
		if dx < 0 {
			name = "port-left"
		}
	} else {
		name = "port-front"
		if dy < 0 {
			name = "port-back"
		}
	}
	p, _ := planPort(r, name)
	return p
}

// Plan ports are footprint-side midpoints. Route only the interior between
// outward leads, keeping the selected endpoints stable. Reuse the bounded
// router with the endpoint footprints also acting as interior obstacles.
func planFixedPortRoute(fr, to planRect, c *Connector, nodes []planRect, placed [][][2]float64) [][2]float64 {
	a, b := planConnectionPort(fr, to, c.From), planConnectionPort(to, fr, c.To)
	start, end := [2]float64{a.WX, a.WY}, [2]float64{b.WX, b.WY}
	if c.Routing == "straight" {
		return [][2]float64{start, end}
	}
	as, _ := fixedPortSide(a.Name)
	bs, _ := fixedPortSide(b.Name)
	ax, ay := portDirection(as)
	bx, by := portDirection(bs)
	s, t := [2]float64{a.WX + 24*ax, a.WY + 24*ay}, [2]float64{b.WX + 24*bx, b.WY + 24*by}
	var obstacles []planRect
	for _, r := range nodes {
		r.z = 0
		obstacles = append(obstacles, r)
	}
	for i, r := range []planRect{fr, to} {
		r.id = []string{"\x00port-source", "\x00port-target"}[i]
		r.z = 0
		r.h = 100
		obstacles = append(obstacles, r)
	}
	middle := route(routerAnchor{ID: fr.id, X: s[0], Y: s[1], DX: ax, DY: ay}, routerAnchor{ID: to.id, X: t[0], Y: t[1], DX: bx, DY: by}, obstacles, placed)
	if len(middle) < 2 {
		// Degenerate/budget fallback retains both leads; it may still report a
		// conflict, but must not change an explicit endpoint to improve a score.
		candidates := [][][2]float64{{s, {t[0], s[1]}, t}, {s, {s[0], t[1]}, t}}
		best := math.Inf(1)
		for _, m := range candidates {
			p := append([][2]float64{start}, m...)
			p = append(p, end)
			cost := routeCost(p, fr, to, nodes, placed)
			if cost < best {
				best = cost
				middle = m
			}
		}
	}
	out := append([][2]float64{start}, middle...)
	return append(out, end)
}
