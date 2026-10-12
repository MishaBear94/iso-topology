package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Independent Bernstein evaluation: do not use portion/worldLerp or the
// screen sampler to check a transformation's source parameterization.
func provenanceWorldAt(points [][3]float64, t float64) (out [3]float64) {
	weights := []float64{1 - t, t}
	if len(points) == 3 {
		weights = []float64{(1 - t) * (1 - t), 2 * t * (1 - t), t * t}
	}
	for i, p := range points {
		for k := range p {
			out[k] += weights[i] * p[k]
		}
	}
	return
}

func assertProvenanceProjection(t *testing.T, d *ConnectorDrawing) {
	t.Helper()
	p := d.Provenance
	if p == nil {
		t.Fatal("missing produced provenance")
	}
	n := len(d.Points) - 1
	if d.Kind == "quadratic" {
		n = 1
	}
	if len(p.Segments) != n {
		t.Fatalf("%d segments, %d sources", n, len(p.Segments))
	}
	for i, s := range p.Segments {
		if s.Segment != i {
			t.Fatal("lost output segment identity", s)
		}
		if s.Kind == "unresolved" {
			if len(s.World) != 0 || s.Source != nil || s.Reason == "" || len(s.Ancestors) == 0 {
				t.Fatal("unresolved geometry claims an exact mapping", s)
			}
			continue
		}
		if s.Kind == "source" {
			if s.Source == nil || s.Source.T0 < 0 || s.Source.T1 > 1 || s.Source.T0 > s.Source.T1 || len(s.Ancestors) != 0 {
				t.Fatal("invalid exact mapping", s)
			}
		} else if s.Kind != "derived" || s.Source != nil || len(s.Ancestors) < 2 || s.Reason != "terminal-shortcut" {
			t.Fatal("invalid derived geometry", s)
		}
		points := d.Points[i : i+2]
		if d.Kind == "quadratic" {
			points = d.Points
		}
		if len(points) != len(s.World) {
			t.Fatal("control count differs", s)
		}
		screenControls := make([][3]float64, len(points))
		for j, v := range points {
			screenControls[j] = [3]float64{v[0], v[1], 0}
		}
		for j := 0; j <= 100; j++ {
			u := float64(j) / 100
			w := provenanceWorldAt(s.World, u)
			v := provenanceWorldAt(screenControls, u)
			// Use the affine projection directly, without its quantizer.
			x := (w[0]-w[1])*cos30 + d.ProjectionOrigin[0]
			y := (w[0]+w[1])*sin30 - w[2] + d.ProjectionOrigin[1]
			if err := math.Hypot(x-v[0], y-v[1]); math.IsNaN(err) || err > s.ProjectionError+1e-9 {
				t.Fatalf("segment %d t %g projection error %g exceeds %g", i, u, err, s.ProjectionError)
			}
		}
	}
}

func TestConnectorProvenanceCorpus(t *testing.T) {
	counts := map[string]int{}
	maxError := 0.0
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		if doc.Scene() == nil {
			return
		}
		before, _ := json.Marshal(doc)
		svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
		if plain := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations); plain != svg {
			t.Fatal("provenance changes emitted SVG")
		}
		for _, r := range routes {
			assertProvenanceProjection(t, r.Drawing)
			counts["routes"]++
			for _, s := range r.Drawing.Provenance.Segments {
				counts[s.Kind]++
				maxError = math.Max(maxError, s.ProjectionError)
				if s.Kind != "source" {
					counts[s.Reason]++
					continue
				}
				var controls [][3]float64
				if r.Drawing.Provenance.SourceSpace == "preclip-polyline" {
					j := s.Source.Segment
					if j < 0 || j+1 >= len(r.World3D) {
						t.Fatal("source outside collected track", s.Source)
					}
					controls = r.World3D[j : j+2]
				} else {
					// Recover the original quadratic using its independent
					// collected values at 0, 1/2 and 1, not its traced controls.
					a, b, mid := r.World3D[0], r.World3D[len(r.World3D)-1], r.World3D[(len(r.World3D)-1)/2]
					c := [3]float64{}
					for k := range c {
						c[k] = 2*mid[k] - .5*(a[k]+b[k])
					}
					controls = [][3]float64{a, c, b}
					counts["quadratic"]++
				}
				for j := 0; j <= 10; j++ {
					u := float64(j) / 10
					actual := provenanceWorldAt(s.World, u)
					want := provenanceWorldAt(controls, s.Source.T0+u*(s.Source.T1-s.Source.T0))
					for k := range actual {
						if math.Abs(actual[k]-want[k]) > 1e-7 {
							t.Fatalf("source parameter mismatch: %v != %v", actual, want)
						}
					}
				}
			}
		}
		after, _ := json.Marshal(doc)
		if string(before) != string(after) {
			t.Fatal("source changed during tracing")
		}
	})
	t.Logf("provenance corpus: %v max SVG error bound %.9g", counts, maxError)
	if dir := os.Getenv("ISOTOPO_PROVENANCE_EVIDENCE"); dir != "" {
		data, _ := json.MarshalIndent(map[string]any{"counts": counts, "max_projection_error": maxError}, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "corpus.json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func provenanceFixture(pts [][2]float64, curved bool) (*connectorPathTrace, [][3]float64) {
	world := make([][3]float64, len(pts))
	for i, p := range pts {
		x, y := isoUnproject(p[0], p[1], 30)
		world[i] = [3]float64{x, y, 30}
	}
	return newConnectorPathTrace(pts, world, curved, [2]float64{}), world
}

func TestConnectorProvenanceMultiLegClip(t *testing.T) {
	pts := [][2]float64{{0, 0}, {20, 0}, {50, 0}, {80, 0}, {100, 0}}
	trace, _ := provenanceFixture(pts, false)
	pts = clipRouteEndRegionTracked(pts, screenRegion{{{75, -5}, {110, -5}, {110, 5}, {75, 5}}}, 2, trace)
	pts = clipRouteStartRegionTracked(pts, screenRegion{{{-5, -5}, {25, -5}, {25, 5}, {-5, 5}}}, 1, trace)
	d := &ConnectorDrawing{Kind: "polyline", Points: svgPoints(pts)}
	d.Provenance = trace.finish(d)
	assertProvenanceProjection(t, d)
	if !reflect.DeepEqual(d.Points, [][2]float64{{26, 0}, {50, 0}, {73, 0}}) {
		t.Fatal("wrong clipped geometry", d.Points)
	}
	a, b := d.Provenance.Segments[0].Source, d.Provenance.Segments[1].Source
	if a.Segment != 1 || math.Abs(a.T0-.2) > 1e-12 || a.T1 != 1 || b.Segment != 2 || b.T0 != 0 || math.Abs(b.T1-23.0/30) > 1e-12 {
		t.Fatal("removed source legs or reversed clip lost parameters", a, b)
	}
}

func TestConnectorProvenanceShortcutAndReuse(t *testing.T) {
	for _, endpoint := range [][2]float64{{98, 0}, {99, 1}} {
		pts := [][2]float64{{0, 0}, {100, 0}, endpoint}
		trace, _ := provenanceFixture(pts, false)
		trace.shortcutTail()
		pts = [][2]float64{pts[0], pts[2]}
		trace.reverse()
		trace.reverse()
		d := &ConnectorDrawing{Kind: "polyline", Points: svgPoints(pts)}
		d.Provenance = trace.finish(d)
		assertProvenanceProjection(t, d)
		s := d.Provenance.Segments[0]
		if endpoint[1] == 0 {
			if s.Kind != "source" || math.Abs(s.Source.T1-.98) > 1e-12 {
				t.Fatal("collinear shortcut lost known source", s)
			}
		} else if s.Kind != "derived" || len(s.Ancestors) != 2 {
			t.Fatal("new chord claims an original segment", s)
		}
		trace.reuse(pts, [][2]float64{{0, 0}, {endpoint[0] + .001, endpoint[1]}})
		if trace.legs[0].Kind == "unresolved" {
			t.Fatal("identical rounded path lost its own height")
		}
		trace.reuse(pts, [][2]float64{{0, 1}, {endpoint[0], endpoint[1] + 1}})
		if s := trace.legs[0]; s.Kind != "unresolved" || s.Source != nil || s.World != nil || s.Reason != "coincident-path-displacement" {
			t.Fatal("displaced coincidence invented a height", s)
		}
	}
}

func TestConnectorProvenanceCurveParameters(t *testing.T) {
	pts := [][2]float64{{0, 0}, {60, 80}, {120, 0}}
	trace, _ := provenanceFixture(pts, true)
	original := append([][3]float64(nil), trace.legs[0].World...)
	got := clipQuadraticRegionsTracked(pts, screenRegion{{{-10, -10}, {25, -10}, {25, 100}, {-10, 100}}}, screenRegion{{{95, -10}, {130, -10}, {130, 100}, {95, 100}}}, 1, 2, trace)
	d := &ConnectorDrawing{Kind: "quadratic", Points: svgPoints(got)}
	d.Provenance = trace.finish(d)
	assertProvenanceProjection(t, d)
	s := d.Provenance.Segments[0]
	if s.Source.T0 <= 25.0/120 || s.Source.T1 >= 95.0/120 {
		t.Fatal("curve lost boundary and arc-length inset parameters", s.Source)
	}
	for j := 0; j <= 100; j++ {
		u := float64(j) / 100
		a := provenanceWorldAt(s.World, u)
		b := provenanceWorldAt(original, s.Source.T0+u*(s.Source.T1-s.Source.T0))
		for k := range a {
			if math.Abs(a[k]-b[k]) > 1e-10 {
				t.Fatal("quadratic clipping lost analytic parameter", a, b)
			}
		}
	}
}

func TestConnectorProvenanceSeparatesCoincidentHeights(t *testing.T) {
	world := [][3]float64{{0, 0, 0}, {100, 0, 0}, {20, 20, 20}, {120, 20, 20}}
	points := make([][2]float64, len(world))
	for i, w := range world {
		points[i][0], points[i][1] = projectIso(w[0], w[1], w[2])
	}
	trace := newConnectorPathTrace(points, world, false, [2]float64{})
	d := &ConnectorDrawing{Kind: "polyline", Points: svgPoints(points)}
	d.Provenance = trace.finish(d)
	r := &ConnectorRoute{World3D: world, Screen: points, Drawing: d}
	lines, reason := spatialLines(r)
	if reason != "" || len(lines) != 3 {
		t.Fatal("known source was confused by projected overlap", reason, lines)
	}
	if lines[0].da != 0 || lines[0].db != 100 || lines[2].da != 60 || lines[2].db != 160 {
		t.Fatal("coincident legs acquired the wrong height", lines)
	}
	d.Provenance = nil
	if _, why := spatialLines(r); why == "" {
		t.Fatal("missing provenance must not guess by projection")
	}
}

func TestConnectorProvenanceProducedCoincidence(t *testing.T) {
	for _, shift := range [][3]float64{{1, 1, 0}, {20, 20, 20}} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			doc := parseWSH(t, fmt.Sprintf(`nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: 0, wy: 0}}
      - {id: b, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: 300, wy: 0}}
      - {id: c, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: %g, wy: %g, wz: %g}}
      - {id: d, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: %g, wy: %g, wz: %g}}
    connectors:
      - {from: a.right, to: b.left, routing: straight}
      - {from: c.right, to: d.left, routing: straight}
`, shift[0], shift[1], shift[2], 300+shift[0], shift[1], shift[2]))
			svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
			if len(routes) != 2 || !reflect.DeepEqual(routes[0].Drawing.Points, routes[1].Drawing.Points) {
				t.Fatal("fixture did not exercise producer coincidence")
			}
			if svg != RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil) {
				t.Fatal("tracing changed reused SVG")
			}
			for _, r := range routes {
				assertProvenanceProjection(t, r.Drawing)
			}
			s := routes[1].Drawing.Provenance.Segments[0]
			if shift[2] == 0 {
				if s.Kind != "unresolved" || s.Reason != "coincident-path-displacement" {
					t.Fatal("producer reused unrelated world coordinates", s)
				}
				if _, reason := spatialLines(&routes[1]); reason == "" {
					t.Fatal("depth compositor accepted displaced source")
				}
			} else {
				old := routes[0].Drawing.Provenance.Segments[0]
				if s.Kind != "source" || math.Abs(s.World[0][2]-old.World[0][2]-20) > 1e-9 {
					t.Fatal("same projection adopted another connector's height", old, s)
				}
			}
		})
	}
}

func TestConnectorProvenanceHeightAndTranslation(t *testing.T) {
	for _, origin := range [][2]float64{{0, 0}, {-300, 700}, {100000, -100000}} {
		for _, height := range []float64{-180, 0, 60, 250} {
			pts := [][2]float64{{origin[0], origin[1]}, {origin[0] + 60, origin[1] + 80}, {origin[0] + 120, origin[1]}}
			trace := newConnectorPathTrace(pts, [][3]float64{{0, 0, -30}, {0, 0, height}}, true, origin)
			source := screenRegion{{{origin[0] - 10, origin[1] - 10}, {origin[0] + 25, origin[1] - 10}, {origin[0] + 25, origin[1] + 100}, {origin[0] - 10, origin[1] + 100}}}
			target := screenRegion{{{origin[0] + 95, origin[1] - 10}, {origin[0] + 130, origin[1] - 10}, {origin[0] + 130, origin[1] + 100}, {origin[0] + 95, origin[1] + 100}}}
			pts = clipQuadraticRegionsTracked(pts, source, target, 1, 2, trace)
			d := &ConnectorDrawing{Kind: "quadratic", Points: svgPoints(pts), ProjectionOrigin: origin}
			d.Provenance = trace.finish(d)
			assertProvenanceProjection(t, d)
			s := d.Provenance.Segments[0]
			if s.Kind != "source" {
				t.Fatal("supported curve lost source", s)
			}
			for i := 0; i <= 100; i++ {
				u := float64(i) / 100
				w := provenanceWorldAt(s.World, u)
				z := -30 + (s.Source.T0+u*(s.Source.T1-s.Source.T0))*(height+30)
				if math.Abs(w[2]-z) > 1e-10 {
					t.Fatal("clip changed linear-height convention", w[2], z)
				}
			}
		}
	}
	for _, routing := range []string{"orthogonal", "orthogonal2"} {
		for _, height := range []float64{-40, 17, 100} {
			src := segmentFixture(t, routing, "port-right", "port-left", height)
			_, _, before := readSegmentRoute(t, src)
			assertProvenanceProjection(t, before.Drawing)
			for j := 0; j+1 < len(before.World3D); j++ {
				out, err := ApplyOpText("yaml", src, EditOp{Kind: "move-segment", Segment: j, DWX: 31, DWY: -27})
				if err != nil {
					t.Fatal(err)
				}
				_, _, after := readSegmentRoute(t, out)
				assertProvenanceProjection(t, after.Drawing)
				for _, s := range after.Drawing.Provenance.Segments {
					if s.Kind != "source" {
						t.Fatal("3-D authored edit lost source", s)
					}
				}
			}
		}
	}
}
