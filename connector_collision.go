package isotopo

import "math"

// ConnectorCollisionReport concerns the producer's lift of the SUBMITTED
// centreline into world space. It does not reinterpret contour ports as physical
// surface interfaces, nor measure a cable radius or the screen stroke. Only a
// complete report with no intervals establishes no interior occupancy in the
// supplied supported boxes. Missing/unsupported geometry is unresolved.
type ConnectorCollisionReport struct {
	Scope           string                      `json:"scope"`
	WorldConvention string                      `json:"world_convention"`
	Status          string                      `json:"status"` // complete | unresolved
	Reason          string                      `json:"reason,omitempty"`
	Intervals       []ConnectorInteriorInterval `json:"intervals"`
}

// Segment indexes Drawing.Points, not the pre-clip route. T0/T1 bound the
// positive-length parameter interval in the box's open interior; face contact
// and isolated tangency are excluded. Source mapping, when it exists, remains
// in Drawing.Provenance. A derived chord needs no fictitious source mapping.
type ConnectorInteriorInterval struct {
	Segment int     `json:"segment"`
	T0      float64 `json:"t0"`
	T1      float64 `json:"t1"`
	Part    string  `json:"part"`
}

func connectorFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Shared by collision and depth consumers. A finite world leg alone is not
// sufficient: it must describe the actual submitted segment at its declared
// projection precision. Curves await an analytic collision/depth contract.
func connectorWorldLines(d *ConnectorDrawing) ([]ConnectorSegmentProvenance, string) {
	if d == nil || d.Kind != "polyline" || d.Provenance == nil || d.Provenance.SourceSpace != "preclip-polyline" {
		return nil, "unsupported curve or missing 3-D provenance"
	}
	if len(d.Points) < 2 || len(d.Provenance.Segments) != len(d.Points)-1 {
		return nil, "incomplete 3-D provenance"
	}
	for _, v := range d.ProjectionOrigin {
		if !connectorFinite(v) {
			return nil, "non-finite projection origin"
		}
	}
	for si, s := range d.Provenance.Segments {
		if s.Segment != si || (s.Kind != "source" && s.Kind != "derived") || len(s.World) != 2 ||
			!connectorFinite(s.ProjectionError) || s.ProjectionError < 0 || s.ProjectionError > .02 {
			return nil, "final route has no supported 3-D provenance"
		}
		if s.Kind == "source" {
			if s.Source == nil || s.Source.Segment < 0 || !connectorFinite(s.Source.T0) || !connectorFinite(s.Source.T1) ||
				s.Source.T0 < 0 || s.Source.T0 > 1 || s.Source.T1 < 0 || s.Source.T1 > 1 {
				return nil, "invalid source interval"
			}
		} else if s.Source != nil || len(s.Ancestors) == 0 {
			return nil, "invalid derived ancestry"
		}
		for j, w := range s.World {
			for _, v := range w {
				if !connectorFinite(v) {
					return nil, "non-finite world coordinate"
				}
			}
			if !connectorFinite(w[0] + w[1] + w[2]) {
				return nil, "world depth exceeds numeric range"
			}
			p := d.Points[si+j]
			x, y := projectIso(w[0], w[1], w[2])
			err := math.Hypot(x+d.ProjectionOrigin[0]-p[0], y+d.ProjectionOrigin[1]-p[1])
			if !connectorFinite(err) || err > s.ProjectionError+1e-6 {
				return nil, "submitted path differs from world provenance"
			}
		}
		for k := range s.World[0] {
			if !connectorFinite(s.World[1][k] - s.World[0][k]) {
				return nil, "world segment exceeds numeric range"
			}
		}
	}
	return d.Provenance.Segments, ""
}

func submittedConnectorCollisions(d *ConnectorDrawing, boxes []spatialBox) ConnectorCollisionReport {
	out := ConnectorCollisionReport{Scope: "submitted-world-centerline", WorldConvention: "producer-route-lift", Status: "unresolved"}
	segments, why := connectorWorldLines(d)
	if why != "" {
		out.Reason = why
		return out
	}
	// Validate the whole query before publishing any intervals. A thin but
	// positive box remains a box; a fixed epsilon must not erase its interior.
	for _, box := range boxes {
		for k := range box.lo {
			if !connectorFinite(box.lo[k]) || !connectorFinite(box.hi[k]) || box.hi[k] <= box.lo[k] {
				out.Reason = "invalid or degenerate world box"
				return out
			}
		}
	}
	out.Status = "complete"
	out.Intervals = []ConnectorInteriorInterval{}
	for si, s := range segments {
		a, b := s.World[0], s.World[1]
		for _, box := range boxes {
			lo, hi := 0., 1.
			for k := range a {
				delta := b[k] - a[k]
				if delta == 0 {
					if a[k] <= box.lo[k] || a[k] >= box.hi[k] {
						hi = lo
						break
					}
					continue
				}
				u, v := (box.lo[k]-a[k])/delta, (box.hi[k]-a[k])/delta
				if u > v {
					u, v = v, u
				}
				lo, hi = math.Max(lo, u), math.Min(hi, v)
			}
			if hi > lo {
				out.Intervals = append(out.Intervals, ConnectorInteriorInterval{si, lo, hi, box.id})
			}
		}
	}
	return out
}
