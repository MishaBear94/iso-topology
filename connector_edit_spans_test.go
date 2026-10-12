package isotopo

import (
	"math"
	"reflect"
	"testing"
)

func editMappingFixture(world [][3]float64) *ConnectorRoute {
	points := make([][2]float64, len(world))
	for i, w := range world {
		points[i][0], points[i][1] = projectIso(w[0], w[1], w[2])
	}
	r := &ConnectorRoute{Routing: "orthogonal", World3D: world, Screen: points, Drawing: &ConnectorDrawing{Kind: "polyline", Points: svgPoints(points)}}
	r.Drawing.Provenance = newConnectorPathTrace(points, world, false, [2]float64{}).finish(r.Drawing)
	return r
}

func TestConnectorEditSourceSurvivesRemovedOverlap(t *testing.T) {
	r := editMappingFixture([][3]float64{{0, 0, 0}, {100, 0, 0}, {20, 20, 20}, {120, 20, 20}})
	// The endpoint clip removed all later legs. They must not veto editing a
	// remaining, precisely identified source, even with identical projections.
	r.Drawing.Points = r.Drawing.Points[:2]
	r.Drawing.Provenance.Segments = r.Drawing.Provenance.Segments[:1]
	want := []ConnectorEditSpan{{Segment: 0, T0: 0, T1: 1, RouteSegment: 0}}
	if got := connectorEditSpans(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("removed geometry blocked known source: %v want %v", got, want)
	}
	// A source consumer must not need the old inverse-lookup screen track.
	r.Screen = nil
	if got := connectorEditSpans(r); !reflect.DeepEqual(got, want) {
		t.Fatal("edit identity still depends on projected search", got)
	}
}

func TestConnectorEditFinalOverlapIntervals(t *testing.T) {
	r := editMappingFixture([][3]float64{{0, 0, 0}, {100, 0, 0}, {70, 20, 20}, {170, 20, 20}})
	got := connectorEditSpans(r)
	if len(got) != 2 || got[0].RouteSegment != 0 || got[1].RouteSegment != 2 || got[0].T0 != 0 || math.Abs(got[0].T1-.5) > 1e-4 || math.Abs(got[1].T0-.5) > 1e-4 || got[1].T1 != 1 {
		t.Fatal("final projected overlap must exclude only the ambiguous intervals", got)
	}
}

func TestConnectorEditRejectsUnverifiedSource(t *testing.T) {
	for _, kind := range []string{"missing", "derived", "unresolved", "index", "parameter", "nan", "height", "displacement"} {
		t.Run(kind, func(t *testing.T) {
			r := editMappingFixture([][3]float64{{0, 0, 0}, {100, 0, 0}})
			s := &r.Drawing.Provenance.Segments[0]
			switch kind {
			case "missing":
				r.Drawing.Provenance = nil
			case "derived", "unresolved":
				s.Kind = kind
			case "index":
				s.Source.Segment = 4
			case "parameter":
				s.Source.T0 = -1
			case "nan":
				s.ProjectionError = math.NaN()
			case "height":
				s.World[0][2] += 10
			case "displacement":
				r.Drawing.Points[0][1] += 1
			}
			if got := connectorEditSpans(r); len(got) != 0 {
				t.Fatal("invalid source was mapped to an edit", got)
			}
		})
	}
}

func TestConnectorEditCorpusUsesProducedIdentity(t *testing.T) {
	count := 0
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		if doc.Scene() == nil {
			return
		}
		_, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
		for _, r := range routes {
			before := append([]ConnectorEditSpan(nil), r.Drawing.Editable...)
			r.Screen = nil
			if got := connectorEditSpans(&r); !reflect.DeepEqual(got, before) {
				t.Fatal("editable identity relies on old screen search", got, before)
			}
			for _, s := range before {
				p := r.Drawing.Provenance.Segments[s.Segment]
				if p.Kind != "source" || p.Source.Segment != s.RouteSegment || !(s.T0 >= 0 && s.T1 <= 1 && s.T1 > s.T0) {
					t.Fatal("wrong produced edit identity", s, p)
				}
				count++
			}
		}
	})
	if count == 0 {
		t.Fatal("empty edit corpus")
	}
	t.Logf("validated %d explicit edit intervals", count)
}
