package isotopo

import (
	"math"
	"sort"
)

// ConnectorEditSpan identifies an authored-route edit through the emitter's
// source mapping. T0/T1 are final-segment parameters. Only intervals without
// another coincident final segment are offered. Coverage and client-space hit
// testing further restrict these intervals; this is not a visibility claim.
type ConnectorEditSpan struct {
	Segment      int     `json:"segment"`
	T0           float64 `json:"t0"`
	T1           float64 `json:"t1"`
	RouteSegment int     `json:"route_segment"`
}

// Validate the producer contract, never choose identity by projected proximity.
// Consumers of missing, stale or unsupported provenance must fail closed.
func connectorEditSource(r *ConnectorRoute, si int) (int, bool) {
	d := r.Drawing
	if d == nil || d.Kind != "polyline" || d.Provenance == nil || d.Provenance.SourceSpace != "preclip-polyline" ||
		len(d.Provenance.Segments) != len(d.Points)-1 || si < 0 || si >= len(d.Provenance.Segments) {
		return 0, false
	}
	s := d.Provenance.Segments[si]
	if s.Segment != si || s.Kind != "source" || s.Source == nil || len(s.World) != 2 || !(s.ProjectionError >= 0 && s.ProjectionError <= .02) {
		return 0, false
	}
	v := s.Source
	if v.Segment < 0 || v.Segment+1 >= len(r.World3D) || !(v.T0 >= 0 && v.T1 <= 1 && v.T1 > v.T0) || routeAxis(r.World3D[v.Segment], r.World3D[v.Segment+1]) < 0 {
		return 0, false
	}
	for k, t := range []float64{v.T0, v.T1} {
		expected := worldLerp(r.World3D[v.Segment], r.World3D[v.Segment+1], t)
		for axis := range expected {
			if !(math.Abs(expected[axis]-s.World[k][axis]) <= 1e-7) {
				return 0, false
			}
		}
		x, y := projectIso(expected[0], expected[1], expected[2])
		err := math.Hypot(x+d.ProjectionOrigin[0]-d.Points[si+k][0], y+d.ProjectionOrigin[1]-d.Points[si+k][1])
		if !(err <= s.ProjectionError+1e-6) {
			return 0, false
		}
	}
	return v.Segment, true
}

func connectorEditSpans(r *ConnectorRoute) []ConnectorEditSpan {
	if r.Drawing == nil || (r.Routing != "orthogonal" && r.Routing != "orthogonal2") {
		return nil
	}
	d := r.Drawing
	var out []ConnectorEditSpan
	for si := 0; si+1 < len(d.Points); si++ {
		source, ok := connectorEditSource(r, si)
		if !ok {
			continue
		}
		a, b := d.Points[si], d.Points[si+1]
		dx, dy := b[0]-a[0], b[1]-a[1]
		length := math.Hypot(dx, dy)
		if length < .05 {
			continue
		}
		var blocked [][2]float64
		// Only final geometry competes for the pointer. Pre-clip legs that were
		// removed cannot create a hit ambiguity on the submitted path.
		for j := 0; j+1 < len(d.Points); j++ {
			if j == si {
				continue
			}
			p, q := d.Points[j], d.Points[j+1]
			if math.Hypot(q[0]-p[0], q[1]-p[1]) < .02 {
				continue
			}
			if math.Abs((p[0]-a[0])*dy-(p[1]-a[1])*dx)/length > .02 || math.Abs((q[0]-a[0])*dy-(q[1]-a[1])*dx)/length > .02 {
				continue
			}
			u := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / (length * length)
			v := ((q[0]-a[0])*dx + (q[1]-a[1])*dy) / (length * length)
			lo, hi := math.Max(0, math.Min(u, v)), math.Min(1, math.Max(u, v))
			if hi > lo {
				blocked = append(blocked, [2]float64{lo, hi})
			}
		}
		sort.Slice(blocked, func(i, j int) bool { return blocked[i][0] < blocked[j][0] })
		start := 0.0
		for _, span := range blocked {
			if (span[0]-start)*length >= .02 {
				out = append(out, ConnectorEditSpan{si, start, span[0], source})
			}
			start = math.Max(start, span[1])
		}
		if (1-start)*length >= .02 {
			out = append(out, ConnectorEditSpan{si, start, 1, source})
		}
	}
	return out
}
