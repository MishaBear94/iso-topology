package isotopo

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// Exercise the actual terminal-shortcut producer, then the compositor. The
// authored two-leg path misses the box; its submitted chord crosses it.
func derivedCollisionFixture() (*routeCollector, connectorSpatialScene, string) {
	w := [][3]float64{{-20, -20, 20}, {120, -20, 20}, {120, 120, 20}}
	var p [][2]float64
	for _, v := range w {
		x, y := projectIso(v[0], v[1], v[2])
		p = append(p, [2]float64{x, y})
	}
	trace := newConnectorPathTrace(p, w, false, [2]float64{})
	trace.shortcutTail()
	d := &ConnectorDrawing{Kind: "polyline", Points: svgPoints([][2]float64{p[0], p[2]}), StrokeWidth: 2}
	d.Provenance = trace.finish(d)
	rc := &routeCollector{routes: []ConnectorRoute{{World3D: w, Drawing: d}}}
	scene := prepareConnectorSpatial(spatialTestParts([][6]float64{{0, 0, 0, 100, 100, 100}}))
	svg := fmt.Sprintf(`<svg><g data-layer="connectors"><path data-connector="0" d="M %.2f,%.2f L %.2f,%.2f"/></g></svg>`, p[0][0], p[0][1], p[2][0], p[2][1])
	return rc, scene, svg
}

func TestSpatialDerivedSubmittedRoute(t *testing.T) {
	rc, scene, svg := derivedCollisionFixture()
	if len(spatialPenetrations(rc.routes[0].World3D, scene.boxes)) != 0 {
		t.Fatal("fixture pre-clip path must miss the box")
	}
	applyConnectorSpatial(svg, rc, scene, 1)
	if got := rc.routes[0].Drawing.Spatial; got == nil || got.Mode != "depth" {
		t.Fatalf("known submitted chord still unsupported: %+v", got)
	}
	got := rc.routes[0].Drawing.Spatial
	if len(got.Penetrations) != 0 || got.SubmittedCollision == nil || got.SubmittedCollision.Status != "complete" || len(got.SubmittedCollision.Intervals) != 1 {
		t.Fatalf("pre-clip and submitted spaces confused: %+v", got)
	}
	s := got.SubmittedCollision.Intervals[0]
	if s.Segment != 0 || math.Abs(s.T0-1./7) > 1e-12 || math.Abs(s.T1-6./7) > 1e-12 || s.Part != "b0" {
		t.Fatal(s)
	}
}

func collisionDrawing(w [][3]float64) *ConnectorDrawing {
	var pts [][2]float64
	for _, p := range w {
		x, y := projectIso(p[0], p[1], p[2])
		pts = append(pts, [2]float64{x, y})
	}
	d := &ConnectorDrawing{Kind: "polyline", Points: svgPoints(pts), StrokeWidth: 2}
	d.Provenance = newConnectorPathTrace(pts, w, false, [2]float64{}).finish(d)
	return d
}

func TestSubmittedCollisionAfterClip(t *testing.T) {
	world := [][3]float64{{-20, 50, 50}, {120, 50, 50}}
	box := spatialBox{"b", [3]float64{}, [3]float64{100, 100, 100}}
	d := collisionDrawing(world)
	trace := &connectorPathTrace{space: d.Provenance.SourceSpace, legs: d.Provenance.Segments}
	trace.clipEnd(0, .1)
	end := trace.legs[0].World[1]
	x, y := projectIso(end[0], end[1], end[2])
	d.Points[1] = [2]float64{svgCoord(x), svgCoord(y)}
	d.Provenance = trace.finish(d)
	got := submittedConnectorCollisions(d, []spatialBox{box})
	if len(spatialPenetrations(world, []spatialBox{box})) != 1 || got.Status != "complete" || len(got.Intervals) != 0 {
		t.Fatal("removed tail must not count as a submitted collision", got)
	}
}

func TestSubmittedCollisionInteriorBoundaries(t *testing.T) {
	box := spatialBox{"owner", [3]float64{}, [3]float64{100, 100, 100}}
	for _, tc := range []struct {
		name  string
		w     [][3]float64
		count int
	}{
		{"face", [][3]float64{{-20, 0, 50}, {120, 0, 50}}, 0},
		{"edge", [][3]float64{{0, 0, -10}, {0, 0, 120}}, 0},
		{"corner touch", [][3]float64{{-10, 10, 0}, {10, -10, 0}}, 0},
		{"ends at face", [][3]float64{{-20, 50, 50}, {0, 50, 50}}, 0},
		{"inside", [][3]float64{{10, 20, 30}, {90, 80, 70}}, 1},
		{"stationary inside", [][3]float64{{50, 50, 50}, {50, 50, 50}}, 1},
		{"owner reentry", [][3]float64{{0, 50, 50}, {-20, 50, 50}, {120, 50, 50}}, 1},
		{"negative direction", [][3]float64{{120, 50, 50}, {-20, 50, 50}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := collisionDrawing(tc.w)
			d.SourceRef, d.TargetRef = "owner.port-left", "owner.port-right"
			got := submittedConnectorCollisions(d, []spatialBox{box})
			if got.Status != "complete" || len(got.Intervals) != tc.count {
				t.Fatal(got)
			}
		})
	}
	// Positive thin interiors and near-parallel movement cannot disappear under
	// a fixed world-coordinate or parameter epsilon. Translate near our domain
	// limit as well as testing a small interval in a long leg.
	for _, shift := range []float64{0, 9e6} {
		b := spatialBox{"thin", [3]float64{shift, 0, 0}, [3]float64{shift + 1e-7, 100, 100}}
		d := collisionDrawing([][3]float64{{shift - 1e6, 50, 50}, {shift + 1e6, 50, 50}})
		got := submittedConnectorCollisions(d, []spatialBox{b})
		if got.Status != "complete" || len(got.Intervals) != 1 || got.Intervals[0].T1 <= got.Intervals[0].T0 {
			t.Fatal(got)
		}
	}
	b := spatialBox{"tiny", [3]float64{}, [3]float64{1e-11, 1, 1}}
	got := submittedConnectorCollisions(collisionDrawing([][3]float64{{-1e-11, .5, .5}, {2e-11, .5, .5}}), []spatialBox{b})
	if got.Status != "complete" || len(got.Intervals) != 1 || math.Abs(got.Intervals[0].T0-1./3) > 1e-12 {
		t.Fatal(got)
	}
}

func TestSubmittedCollisionAgainstWorldMembership(t *testing.T) {
	rng := rand.New(rand.NewSource(20261011))
	count := 0
	for _, shift := range []float64{0, -10000, 9e6} {
		for i := 0; i < 200; i++ {
			var a, b [3]float64
			box := spatialBox{id: "b"}
			for k := range a {
				a[k], b[k] = shift+rng.Float64()*600-200, shift+rng.Float64()*600-200
				box.lo[k], box.hi[k] = shift, shift+200
			}
			d := collisionDrawing([][3]float64{a, b})
			got := submittedConnectorCollisions(d, []spatialBox{box})
			if got.Status != "complete" {
				t.Fatal(got)
			}
			for j := 0; j < 101; j++ {
				u := (float64(j) + .317) / 101
				hit := false
				for _, s := range got.Intervals {
					hit = hit || (u > s.T0 && u < s.T1)
				}
				if hit != collisionOraclePoint(a, b, u, box) {
					t.Fatalf("case %d, t %g: %+v", i, u, got)
				}
				count++
			}
		}
	}
	t.Logf("independent world membership samples: %d", count)
}

func TestSubmittedCollisionUnresolvedIsNotClear(t *testing.T) {
	for name, mutate := range map[string]func(*ConnectorDrawing){
		"missing":         func(d *ConnectorDrawing) { d.Provenance = nil },
		"curve":           func(d *ConnectorDrawing) { d.Kind = "quadratic" },
		"partial":         func(d *ConnectorDrawing) { d.Provenance.Segments = d.Provenance.Segments[:1] },
		"unresolved":      func(d *ConnectorDrawing) { d.Provenance.Segments[1].Kind = "unresolved" },
		"negative error":  func(d *ConnectorDrawing) { d.Provenance.Segments[1].ProjectionError = -1 },
		"nan world":       func(d *ConnectorDrawing) { d.Provenance.Segments[1].World[0][0] = math.NaN() },
		"infinite origin": func(d *ConnectorDrawing) { d.ProjectionOrigin[0] = math.Inf(1) },
		"wrong index":     func(d *ConnectorDrawing) { d.Provenance.Segments[1].Segment = 0 },
		"displaced":       func(d *ConnectorDrawing) { d.Points[2][0] += 1 },
		"nan source":      func(d *ConnectorDrawing) { d.Provenance.Segments[1].Source.T0 = math.NaN() },
	} {
		t.Run(name, func(t *testing.T) {
			d := collisionDrawing([][3]float64{{-20, 50, 50}, {120, 50, 50}, {120, 200, 50}})
			mutate(d)
			got := submittedConnectorCollisions(d, []spatialBox{{"b", [3]float64{}, [3]float64{100, 100, 100}}})
			if got.Status != "unresolved" || got.Reason == "" || got.Intervals != nil {
				t.Fatal(got)
			}
		})
	}
	rc, scene, svg := derivedCollisionFixture()
	scene.boxes[0].hi[0] = scene.boxes[0].lo[0]
	if got := applyConnectorSpatial(svg, rc, scene, 1); got != svg || rc.routes[0].Drawing.Spatial.Mode != "legacy" || rc.routes[0].Drawing.Spatial.SubmittedCollision != nil {
		t.Fatal("invalid box committed a partial depth result")
	}
	// A failure discovered after solving must retain both the old SVG and
	// observations; knowing a collision does not waive atomic publication.
	rc, scene, svg = derivedCollisionFixture()
	before := rc.routes[0].Drawing.Coverage
	svg = strings.Replace(svg, `data-connector="0"`, `data-connector="2"`, 1)
	if got := applyConnectorSpatial(svg, rc, scene, 1); got != svg || rc.routes[0].Drawing.Spatial.SubmittedCollision != nil || !reflect.DeepEqual(before, rc.routes[0].Drawing.Coverage) {
		t.Fatal("failed splice leaked collision result")
	}
}

// Used by subsequent collision assertions without the production interpolation
// helper: point membership is a separate oracle from slab intersections.
func collisionOraclePoint(a, b [3]float64, u float64, box spatialBox) bool {
	for k := range a {
		v := (1-u)*a[k] + u*b[k]
		if v <= box.lo[k] || v >= box.hi[k] || math.IsNaN(v) {
			return false
		}
	}
	return true
}
