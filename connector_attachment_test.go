package isotopo

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestConnectorAttachmentsMatchSubmittedEndpoints(t *testing.T) {
	count := 0
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		if doc.Scene() == nil {
			return
		}
		svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
		actual := submittedConnectorPaths(t, svg)
		for _, r := range routes {
			d := r.Drawing
			if d.Attachments == nil {
				t.Fatal("missing endpoint contract")
			}
			for side, a := range []ConnectorEndpointAttachment{d.Attachments.Source, d.Attachments.Target} {
				p := actual[r.Index].pts[0]
				ref, resolved, rt := d.SourceRef, d.ResolvedSource, 0.
				if side == 1 {
					p = actual[r.Index].pts[len(actual[r.Index].pts)-1]
					ref, resolved, rt = d.TargetRef, d.ResolvedTarget, 1
				}
				if a.Point != p || a.LogicalRef != ref || a.ResolvedRef != resolved || a.RouteT != rt || a.InstanceID == "" || a.Anchor == "" {
					t.Fatal(a)
				}
				if a.World.Status != "known" || a.World.Convention != "producer-route-lift" || a.World.Point == nil {
					t.Fatal(a)
				}
				w := *a.World.Point
				// Independent projection; the body anchor may have been trimmed.
				x, y := (w[0]-w[1])*math.Sqrt(3)/2, (w[0]+w[1])/2-w[2]
				if math.Hypot(x+d.ProjectionOrigin[0]-p[0], y+d.ProjectionOrigin[1]-p[1]) > .02 {
					t.Fatal("endpoint world lift disagrees with SVG", a)
				}
				count++
			}
		}
	})
	if count != 168 {
		t.Fatalf("unexpected endpoint corpus: %d", count)
	}
	t.Logf("submitted endpoint contracts: %d", count)
}

func TestConnectorAttachmentsInstancesAndPorts(t *testing.T) {
	for _, shape := range []string{"rectangle", "circle", "person", "cylinder", "custom_path", "cloud", "hexprism", "wedge"} {
		for _, routing := range []string{"straight", "orthogonal", "orthogonal2", "bezier"} {
			for _, port := range fixedPortNames {
				t.Run(shape+"/"+routing+"/"+port, func(t *testing.T) {
					doc := portFixture(t, shape)
					doc.Scene().Parts[0].Stack = &Stack{Count: 3, Gap: 50}
					doc.Scene().Connectors = []*Connector{{From: "source~2." + port, To: "target.port-left", Routing: routing, Arrow: "triangle"}}
					before, _ := json.Marshal(doc)
					svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
					a := routes[0].Drawing.Attachments.Source
					if a.LogicalInstanceID != "source~2" || a.InstanceID != "source~2" || a.Anchor != port || a.VisualRule != "fixed-contour" || a.World.Status != "known" {
						t.Fatal(a)
					}
					a.World.Point[0] += 1000
					routes[0].Drawing.Attachments.Target.Point[0] += 1000
					svg2, r2 := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
					if svg2 != svg || reflect.DeepEqual(r2[0].Drawing.Attachments, routes[0].Drawing.Attachments) {
						t.Fatal("attachment observation aliases producer")
					}
					after, _ := json.Marshal(doc)
					if string(before) != string(after) {
						t.Fatal("attachment observation mutated source")
					}
				})
			}
		}
	}
}

func TestConnectorAttachmentLogicalAndVisualRemainDistinct(t *testing.T) {
	d := collisionDrawing([][3]float64{{-20, 50, 50}, {120, 50, 50}})
	d.SourceRef, d.ResolvedSource = "inside", "container.front"
	d.TargetRef, d.ResolvedTarget = "target", "target.left"
	a := connectorAttachments(&anchorResolver{}, d)
	if a.Source.LogicalInstanceID != "inside" || a.Source.InstanceID != "container" || a.Source.Anchor != "front" || a.Source.VisualRule != "legacy-silhouette-trim" {
		t.Fatal(a)
	}
	d.Provenance.Segments[0].unresolved("displaced by coincident route reuse")
	a = connectorAttachments(&anchorResolver{}, d)
	if a.Source.World.Status != "unresolved" || a.Source.World.Point != nil || a.Source.Point != d.Points[0] || a.Source.LogicalInstanceID != "inside" {
		t.Fatal(a)
	}
}
