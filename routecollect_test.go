package isotopo

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// walkSampleDocs loads every samples/{node,topology}/<name> input through the
// same pipeline as the golden test and hands the parsed document to fn.
func walkSampleDocs(t *testing.T, fn func(t *testing.T, name string, doc *Document)) {
	t.Helper()
	for _, category := range []string{"node", "topology"} {
		root := filepath.Join("samples", category)
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := category + "/" + e.Name()
			t.Run(name, func(t *testing.T) {
				dir := filepath.Join(root, e.Name())
				data, ext, err := readInput(dir)
				if err != nil {
					t.Fatal(err)
				}
				doc, err := loadGoldenDoc(ext, data)
				if err != nil {
					t.Fatalf("load: %v", err)
				}
				fn(t, name, doc)
			})
		}
	}
}

// TestRenderWithRouteInfo_ByteIdenticalSVG is the WS-E hard gate: attaching
// the route collector must not change a single output byte on ANY sample —
// in particular no straight connector may grow a data-route attribute. This
// is what keeps every existing golden frozen.
func TestRenderWithRouteInfo_ByteIdenticalSVG(t *testing.T) {
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		scene := doc.Scene()
		if scene == nil {
			t.Skip("no scene")
		}
		// Render mutates the scene (applyLayout), so each call gets its own
		// deep clone of the SAME starting state.
		plain := RenderWithCanvas(cloneSceneForEval(scene), doc.Theme, doc.Canvas, doc.Annotations)
		collected, routes := RenderWithRouteInfo(cloneSceneForEval(scene), doc.Theme, doc.Canvas, doc.Annotations)
		if plain != collected {
			t.Fatalf("route collection changed the SVG output (%d vs %d bytes)", len(plain), len(collected))
		}
		// data-route count must equal the collector's ORTHOGONAL route count:
		// straight/bezier routes exist only in memory, never as attributes.
		ortho := 0
		for _, r := range routes {
			if r.Routing == "orthogonal" {
				ortho++
			}
		}
		if got := strings.Count(collected, "data-route="); got != ortho {
			t.Fatalf("data-route attrs (%d) != collected orthogonal routes (%d)", got, ortho)
		}
	})
}

const routeCollectFixture = `nodes:
  scene:
    shape: composite
    parts:
      - { id: a, shape: rectangle, geom: { w: 80, d: 80, h: 20 }, offset: { wx: 0,   wy: 0 } }
      - { id: b, shape: rectangle, geom: { w: 80, d: 80, h: 20 }, offset: { wx: 300, wy: 200 } }
      - { id: c, shape: rectangle, geom: { w: 80, d: 80, h: 20 }, offset: { wx: 0,   wy: 200 } }
    connectors:
      - { from: a, to: b, routing: orthogonal }
      - { from: a.right, to: c.left, routing: straight }
      - { from: c, to: b, routing: bezier }
`

// TestRouteCollector_CoversAllRoutings locks the collector's contract: every
// drawn connector is recorded with its real pre-clip geometry — orthogonal
// reusing the data-route corners, straight recording its two world anchors,
// bezier recording a sampled world polyline of the drawn quadratic.
func TestRouteCollector_CoversAllRoutings(t *testing.T) {
	doc, err := LoadInput(context.Background(), "yaml", []byte(routeCollectFixture), LayoutDagre)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	scene := doc.Scene()
	svg, routes := RenderWithRouteInfo(scene, doc.Theme, doc.Canvas, nil)
	if len(routes) != 3 {
		t.Fatalf("expected 3 collected routes, got %d: %+v", len(routes), routes)
	}
	byIdx := map[int]ConnectorRoute{}
	for _, r := range routes {
		byIdx[r.Index] = r
	}

	// Connector 0: orthogonal — world route matches the serialized data-route
	// coverage (>= 2 corners) and only THIS connector emits the attribute.
	if r := byIdx[0]; r.Routing != "orthogonal" || len(r.World) < 2 {
		t.Fatalf("connector 0: want orthogonal with >=2 corners, got %+v", r)
	}
	if got := strings.Count(svg, "data-route="); got != 1 {
		t.Fatalf("straight/bezier must not emit data-route: want 1 attr, got %d", got)
	}

	// Connector 1: straight a.right → c.left. The anchors sit on the parts'
	// face midpoints at ground z: a.right = (80, 40), c.left = (0, 240).
	r1 := byIdx[1]
	if r1.Routing != "straight" || len(r1.World) != 2 {
		t.Fatalf("connector 1: want straight with 2 world points, got %+v", r1)
	}
	wantS := [][2]float64{{80, 40}, {0, 240}}
	for i, w := range wantS {
		if math.Abs(r1.World[i][0]-w[0]) > 1e-6 || math.Abs(r1.World[i][1]-w[1]) > 1e-6 {
			t.Fatalf("connector 1 world[%d]: want %v, got %v", i, w, r1.World[i])
		}
	}
	if r1.From != "a" || r1.To != "c" {
		t.Fatalf("connector 1 endpoints: want a→c, got %s→%s", r1.From, r1.To)
	}

	// Connector 2: bezier c → b. Bare refs anchor at the top-face centres —
	// c: (40, 240), b: (340, 240) — and the sampled polyline must run
	// endpoint-to-endpoint with the fixed sample count.
	r2 := byIdx[2]
	if r2.Routing != "bezier" || len(r2.World) != bezierRouteSamples+1 {
		t.Fatalf("connector 2: want bezier with %d samples, got %+v", bezierRouteSamples+1, r2)
	}
	wantB := [][2]float64{{40, 240}, {340, 240}}
	ends := [][2]float64{r2.World[0], r2.World[len(r2.World)-1]}
	for i, w := range wantB {
		if math.Abs(ends[i][0]-w[0]) > 1e-5 || math.Abs(ends[i][1]-w[1]) > 1e-5 {
			t.Fatalf("connector 2 endpoint %d: want %v, got %v", i, w, ends[i])
		}
	}
	// The curve must actually bow off the chord (it IS a quadratic).
	mid := r2.World[bezierRouteSamples/2]
	if math.Abs(mid[1]-240) < 1 {
		t.Fatalf("connector 2 midpoint should bend off the straight chord, got %v", mid)
	}
	// World and Screen tracks stay parallel.
	for _, r := range routes {
		if len(r.World) != len(r.Screen) {
			t.Fatalf("connector %d: world (%d) and screen (%d) tracks must be parallel",
				r.Index, len(r.World), len(r.Screen))
		}
	}
}

// TestIsoUnproject_RoundTrips pins the inverse projection used to recover
// bezier world tracks: unproject(project(w)) must reproduce w to within the
// projector's own 1e-6 quantization.
func TestIsoUnproject_RoundTrips(t *testing.T) {
	for _, w := range [][3]float64{{0, 0, 0}, {40, 240, 20}, {-13.5, 7.25, 64}, {321.125, 200, 0}} {
		sx, sy := projectIso(w[0], w[1], w[2])
		wx, wy := isoUnproject(sx, sy, w[2])
		if math.Abs(wx-w[0]) > 1e-5 || math.Abs(wy-w[1]) > 1e-5 {
			t.Fatalf("round-trip %v → (%v, %v)", w, wx, wy)
		}
	}
}
