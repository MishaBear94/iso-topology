package isotopo

import "math"

// ConnectorAttachments separates logical references, resolved instance/port
// identity, and submitted endpoint geometry. Neither a silhouette-trimmed
// endpoint nor a fixed contour port is a certified physical surface interface.
type ConnectorAttachments struct {
	Source ConnectorEndpointAttachment `json:"source"`
	Target ConnectorEndpointAttachment `json:"target"`
}

type ConnectorEndpointAttachment struct {
	LogicalRef        string                   `json:"logical_ref"`
	LogicalInstanceID string                   `json:"logical_instance_id"`
	ResolvedRef       string                   `json:"resolved_ref"`
	InstanceID        string                   `json:"instance_id"`
	Anchor            string                   `json:"anchor"`
	VisualRule        string                   `json:"visual_rule"` // fixed-contour | legacy-silhouette-trim
	Point             [2]float64               `json:"point"`
	RouteSegment      int                      `json:"route_segment"`
	RouteT            float64                  `json:"route_t"`
	World             ConnectorAttachmentWorld `json:"world"`
}

type ConnectorAttachmentWorld struct {
	Convention string      `json:"convention"` // producer-route-lift; NOT surface certification
	Status     string      `json:"status"`     // known | unresolved
	Point      *[3]float64 `json:"point,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

func connectorAttachments(ar *anchorResolver, d *ConnectorDrawing) *ConnectorAttachments {
	if d == nil || len(d.Points) < 2 {
		return nil
	}
	endpoint := func(source bool) ConnectorEndpointAttachment {
		a := ConnectorEndpointAttachment{LogicalRef: d.TargetRef, ResolvedRef: d.ResolvedTarget,
			Point: d.Points[len(d.Points)-1], RouteSegment: len(d.Points) - 2, RouteT: 1,
			VisualRule: "legacy-silhouette-trim", World: ConnectorAttachmentWorld{Convention: "producer-route-lift", Status: "unresolved", Reason: "missing endpoint provenance"}}
		if d.Kind == "quadratic" {
			a.RouteSegment = 0
		}
		if source {
			a.LogicalRef, a.ResolvedRef = d.SourceRef, d.ResolvedSource
			a.Point, a.RouteSegment, a.RouteT = d.Points[0], 0, 0
		}
		a.LogicalInstanceID, _ = ar.parse(a.LogicalRef)
		a.InstanceID, a.Anchor = ar.parse(a.ResolvedRef)
		if isFixedPortRef(a.ResolvedRef) {
			a.VisualRule = "fixed-contour"
		}
		if d.Provenance == nil || a.RouteSegment >= len(d.Provenance.Segments) {
			return a
		}
		s := d.Provenance.Segments[a.RouteSegment]
		n := 2
		if d.Kind == "quadratic" {
			n = 3
		}
		if s.Segment != a.RouteSegment || (s.Kind != "source" && s.Kind != "derived") || len(s.World) != n ||
			!connectorFinite(s.ProjectionError) || s.ProjectionError < 0 || s.ProjectionError > .02 {
			a.World.Reason = "unresolved endpoint provenance"
			return a
		}
		w := s.World[len(s.World)-1]
		if source {
			w = s.World[0]
		}
		for _, v := range w {
			if !connectorFinite(v) {
				a.World.Reason = "non-finite endpoint world coordinate"
				return a
			}
		}
		x, y := projectIso(w[0], w[1], w[2])
		err := math.Hypot(x+d.ProjectionOrigin[0]-a.Point[0], y+d.ProjectionOrigin[1]-a.Point[1])
		if !connectorFinite(err) || err > s.ProjectionError+1e-6 {
			a.World.Reason = "endpoint differs from submitted path"
			return a
		}
		a.World.Status, a.World.Point, a.World.Reason = "known", &w, ""
		return a
	}
	return &ConnectorAttachments{Source: endpoint(true), Target: endpoint(false)}
}
