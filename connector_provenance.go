package isotopo

import "math"

// ConnectorProvenance is produced alongside path transformations, before SVG
// rounding. SourceSpace identifies the input to those transformations: either
// the collected pre-clip polyline or the analytic pre-clip quadratic (NOT the
// sampled World3D track). It does not refer to authored waypoint indexes.
type ConnectorProvenance struct {
	SourceSpace string                       `json:"source_space"`
	Segments    []ConnectorSegmentProvenance `json:"segments"`
}

// ConnectorSourceInterval maps a final segment's t in [0,1] to
// T0+t*(T1-T0) on the specified pre-clip source segment.
type ConnectorSourceInterval struct {
	Segment int     `json:"segment"`
	T0      float64 `json:"t0"`
	T1      float64 `json:"t1"`
}

// Kind is source, derived, or unresolved. Only source has an exact Source
// parameter mapping. Derived terminal shortcuts have their own world chord;
// Ancestors records inputs, not a parameter mapping to that chord. Unresolved
// paths retain ancestry but omit World. World is a pair of linear endpoints or
// three quadratic controls. ProjectionError bounds the distance between that
// world's projection and the submitted, rounded path in SVG user units.
type ConnectorSegmentProvenance struct {
	Segment         int                       `json:"segment"`
	Kind            string                    `json:"kind"`
	Source          *ConnectorSourceInterval  `json:"source,omitempty"`
	Ancestors       []ConnectorSourceInterval `json:"ancestors,omitempty"`
	World           [][3]float64              `json:"world,omitempty"`
	ProjectionError float64                   `json:"projection_error"`
	Reason          string                    `json:"reason,omitempty"`
}

type connectorPathTrace struct {
	space string
	legs  []ConnectorSegmentProvenance
}

func newConnectorPathTrace(pts [][2]float64, world [][3]float64, curved bool, origin [2]float64) *connectorPathTrace {
	if len(pts) < 2 || len(world) < 2 {
		return nil
	}
	p := &connectorPathTrace{space: "preclip-polyline"}
	if curved {
		if len(pts) != 3 {
			return nil
		}
		// An analytic lift of the existing linear-height convention. Its
		// parameter is independent of the evaluator's fixed sample count.
		w := make([][3]float64, 3)
		for i, q := range pts {
			z := world[0][2] + float64(i)/2*(world[len(world)-1][2]-world[0][2])
			x, y := isoUnproject(q[0]-origin[0], q[1]-origin[1], z)
			w[i] = [3]float64{x, y, z}
		}
		p.space = "preclip-quadratic"
		p.legs = []ConnectorSegmentProvenance{{Kind: "source", Source: &ConnectorSourceInterval{0, 0, 1}, World: w}}
		return p
	}
	if len(pts) != len(world) {
		return nil
	}
	for i := 1; i < len(world); i++ {
		p.legs = append(p.legs, ConnectorSegmentProvenance{Kind: "source", Source: &ConnectorSourceInterval{i - 1, 0, 1}, World: [][3]float64{world[i-1], world[i]}})
	}
	return p
}

func sourceAncestors(s ConnectorSegmentProvenance) []ConnectorSourceInterval {
	if s.Source != nil {
		return []ConnectorSourceInterval{*s.Source}
	}
	return append([]ConnectorSourceInterval(nil), s.Ancestors...)
}

func worldLerp(a, b [3]float64, t float64) (w [3]float64) {
	for i := range w {
		w[i] = a[i] + t*(b[i]-a[i])
	}
	return
}

func (s *ConnectorSegmentProvenance) portion(lo, hi float64) {
	if s.Source != nil {
		a, b := s.Source.T0, s.Source.T1
		s.Source.T0, s.Source.T1 = a+lo*(b-a), a+hi*(b-a)
	}
	switch len(s.World) {
	case 2:
		a, b := s.World[0], s.World[1]
		s.World = [][3]float64{worldLerp(a, b, lo), worldLerp(a, b, hi)}
	case 3:
		// De Casteljau evaluation with the derivative at lo, so the new
		// control retains the exact quadratic parameterization.
		a, b, c := s.World[0], s.World[1], s.World[2]
		at := func(t float64) [3]float64 { return worldLerp(worldLerp(a, b, t), worldLerp(b, c, t), t) }
		start, end, control := at(lo), at(hi), [3]float64{}
		for k := range control {
			control[k] = start[k] + (hi-lo)*((1-lo)*(b[k]-a[k])+lo*(c[k]-b[k]))
		}
		s.World = [][3]float64{start, control, end}
	}
}

func (p *connectorPathTrace) reverse() {
	if p == nil {
		return
	}
	for i, j := 0, len(p.legs)-1; i < j; i, j = i+1, j-1 {
		p.legs[i], p.legs[j] = p.legs[j], p.legs[i]
	}
	for i := range p.legs {
		s := &p.legs[i]
		s.portion(1, 0)
		for j, k := 0, len(s.Ancestors)-1; j < k; j, k = j+1, k-1 {
			s.Ancestors[j], s.Ancestors[k] = s.Ancestors[k], s.Ancestors[j]
		}
		for j := range s.Ancestors {
			s.Ancestors[j].T0, s.Ancestors[j].T1 = s.Ancestors[j].T1, s.Ancestors[j].T0
		}
	}
}

func (p *connectorPathTrace) clipEnd(segment int, t float64) {
	if p == nil {
		return
	}
	p.legs = p.legs[:segment+1]
	p.legs[segment].portion(0, t)
}

// Removing a terminal vertex normally creates a new chord. If the endpoint
// lies on the immediately preceding world leg, retain that leg's exact source
// interval. This uses the transformation's inputs, never a projected search.
func (p *connectorPathTrace) shortcutTail() {
	if p == nil || len(p.legs) < 2 {
		return
	}
	n := len(p.legs)
	a, b := p.legs[n-2], p.legs[n-1]
	wa, wb, end := a.World[0], a.World[len(a.World)-1], b.World[len(b.World)-1]
	den, dot := 0.0, 0.0
	for k := range wa {
		den += (wb[k] - wa[k]) * (wb[k] - wa[k])
		dot += (end[k] - wa[k]) * (wb[k] - wa[k])
	}
	t := dot / den
	q := worldLerp(wa, wb, t)
	exact := den > 0 && t >= 0 && t <= 1 && a.Kind == "source"
	for k := range q {
		exact = exact && math.Abs(q[k]-end[k]) <= 1e-9
	}
	if exact {
		a.portion(0, t)
	} else {
		a = ConnectorSegmentProvenance{Kind: "derived", Ancestors: append(sourceAncestors(a), sourceAncestors(b)...), World: [][3]float64{wa, end}, Reason: "terminal-shortcut"}
	}
	p.legs[n-2] = a
	p.legs = p.legs[:n-1]
}

func (s *ConnectorSegmentProvenance) unresolved(reason string) {
	s.Ancestors = sourceAncestors(*s)
	s.Kind, s.Source, s.World, s.Reason = "unresolved", nil, nil, reason
	s.ProjectionError = 0
}

// Coincidence reuse is safe only when SVG rounding gives the same path. A
// nearby path's screen coordinates cannot determine this edge's new height.
func (p *connectorPathTrace) reuse(before, after [][2]float64) {
	if p == nil {
		return
	}
	for i := range p.legs {
		lo, hi := i, i+1
		if p.space == "preclip-quadratic" {
			lo, hi = 0, 2
		}
		for j := lo; j <= hi; j++ {
			if svgCoord(before[j][0]) != svgCoord(after[j][0]) || svgCoord(before[j][1]) != svgCoord(after[j][1]) {
				p.legs[i].unresolved("coincident-path-displacement")
				break
			}
		}
	}
}

func (p *connectorPathTrace) finish(d *ConnectorDrawing) *ConnectorProvenance {
	if p == nil {
		return nil
	}
	for i := range p.legs {
		s := &p.legs[i]
		s.Segment = i
		for j, w := range s.World {
			x, y := projectIso(w[0], w[1], w[2])
			point := i + j
			if d.Kind == "quadratic" {
				point = j
			}
			err := math.Hypot(x+d.ProjectionOrigin[0]-d.Points[point][0], y+d.ProjectionOrigin[1]-d.Points[point][1])
			if (math.IsNaN(err) || math.IsInf(err, 0)) || err > .02 {
				s.unresolved("projection-mismatch")
				break
			}
			// projectIso rounds to 1e-6; include its vector error. The
			// control hull then bounds the whole linear/quadratic curve.
			s.ProjectionError = math.Max(s.ProjectionError, err+1e-6)
		}
	}
	return &ConnectorProvenance{SourceSpace: p.space, Segments: p.legs}
}
