package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// This oracle intersects a viewing ray with world boxes. It does not use
// production projected faces, plane coefficients or polygon subtraction.
func rayBoxFront(p [2]float64, b [6]float64) (float64, bool) {
	c := math.Sqrt(3) / 2
	origin := [3]float64{p[1] + p[0]/(2*c), p[1] - p[0]/(2*c), 0}
	lo, hi := math.Inf(-1), math.Inf(1)
	for k := 0; k < 3; k++ {
		lo = math.Max(lo, b[k]-origin[k])
		hi = math.Min(hi, b[k]+b[k+3]-origin[k])
	}
	return origin[0] + origin[1] + 3*hi, lo <= hi
}

// Even/odd membership is independent of production convex clipping.
func spatialOracleContains(p [2]float64, polys [][][2]float64) bool {
	for _, poly := range polys {
		inside := false
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < a[0]+(p[1]-a[1])*(b[0]-a[0])/(b[1]-a[1]) {
				inside = !inside
			}
		}
		if inside {
			return true
		}
	}
	return false
}

func spatialTestParts(boxes [][6]float64) []iso25d.CompositePart {
	var out []iso25d.CompositePart
	for i, b := range boxes {
		out = append(out, iso25d.CompositePart{ID: fmt.Sprintf("b%d", i), Shape: "rectangle", OffWX: b[0], OffWY: b[1], OffWZ: b[2], Opts: iso25d.ConvertOpts{Width: b[3], Depth: b[4], Height: b[5], Stroke: "none"}})
	}
	return out
}

func spatialTestLine(a, b [3]float64) spatialLine {
	// Analytic projection, independently written (no projectIso).
	p := func(w [3]float64) [2]float64 {
		return [2]float64{(w[0] - w[1]) * math.Sqrt(3) / 2, (w[0]+w[1])/2 - w[2]}
	}
	return spatialLine{p(a), p(b), a[0] + a[1] + a[2], b[0] + b[1] + b[2], 0, 0, 1}
}

func TestSpatialDepthAgainstWorldRayOracle(t *testing.T) {
	boxes := [][6]float64{{0, -100, 0, 160, 120, 45}, {220, -20, 0, 100, 100, 200}, {360, 40, 130, 160, 100, 12}}
	count, hidden, visible := 0, 0, 0
	for _, shift := range [][3]float64{{}, {37, -123, 19}, {10000, -20000, 3000}} {
		moved := append([][6]float64(nil), boxes...)
		for i := range moved {
			for k := range shift {
				moved[i][k] += shift[k]
			}
		}
		scene := prepareConnectorSpatial(spatialTestParts(moved))
		if scene.reason != "" {
			t.Fatal(scene.reason)
		}
		for _, height := range []float64{10, 80, 130, 220} {
			for _, slope := range []float64{-70, 0, 110} {
				a, b := [3]float64{-120, -50, height}, [3]float64{640, -50, height + slope}
				for k := range shift {
					a[k] += shift[k]
					b[k] += shift[k]
				}
				line := spatialTestLine(a, b)
				region, ok := spatialVisibleRegion([]spatialLine{line}, scene.surfaces, 2)
				if !ok {
					t.Fatal("unexpected fragment budget")
				}
				spans := spatialDepthSpans([]spatialLine{line}, scene.surfaces)
				end := 0.0
				for _, s := range spans {
					if math.Abs(s.T0-end) > 1e-9 || s.T1 <= s.T0 {
						t.Fatal("depth interval gap/overlap", spans)
					}
					end = s.T1
				}
				if math.Abs(end-1) > 1e-9 {
					t.Fatal("incomplete depth intervals", spans)
				}
				dx, dy := line.b[0]-line.a[0], line.b[1]-line.a[1]
				length := math.Hypot(dx, dy)
				for j := 0; j < 151; j++ {
					u := (float64(j) + .317) / 151
					depth := line.da + u*(line.db-line.da)
					for _, across := range []float64{-1.7, 0, 1.7} {
						p := [2]float64{line.a[0] + u*dx - across*dy/length, line.a[1] + u*dy + across*dx/length}
						want := true
						for _, box := range moved {
							front, hit := rayBoxFront(p, box)
							if hit && front > depth+.0001 {
								want = false
							}
						}
						got := spatialOracleContains(p, region)
						if across == 0 {
							for _, s := range spans {
								if u > s.T0 && u < s.T1 && (len(s.Occluders) == 0) != want {
									t.Fatalf("depth metadata disagrees with world ray: %+v at %v", s, p)
								}
							}
						}
						if got != want {
							t.Fatalf("shift=%v z=%v slope=%v t=%v across=%v point=%v got %v want %v", shift, height, slope, u, across, p, got, want)
						}
						count++
						if want {
							visible++
						} else {
							hidden++
						}
					}
				}
			}
		}
	}
	if hidden == 0 || visible == 0 {
		t.Fatal("oracle matrix missed a visibility class")
	}
	t.Logf("%d independent ray samples: visible=%d hidden=%d", count, visible, hidden)
}

func TestSpatialPenetrationIsNotOcclusion(t *testing.T) {
	box := spatialBox{"solid", [3]float64{0, 0, 0}, [3]float64{100, 100, 100}}
	for _, tc := range []struct {
		name string
		a, b [3]float64
		hit  bool
	}{
		{"inside", [3]float64{-20, 50, 50}, [3]float64{120, 50, 50}, true},
		{"front boundary", [3]float64{-20, 0, 50}, [3]float64{120, 0, 50}, false},
		{"roof touch", [3]float64{-20, 50, 100}, [3]float64{120, 50, 100}, false},
		{"behind without penetration", [3]float64{-20, -20, 20}, [3]float64{120, -20, 20}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := spatialPenetrations([][3]float64{tc.a, tc.b}, []spatialBox{box})
			if (len(got) > 0) != tc.hit {
				t.Fatal(got)
			}
			if tc.hit && (math.Abs(got[0].T0-1./7) > 1e-12 || math.Abs(got[0].T1-6./7) > 1e-12) {
				t.Fatal(got)
			}
		})
	}
}

const spatialLongLineFixture = `canvas: {depthSort: true, background: "#ffffff"}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: -300, wy: -100, wz: 80}}
      - {id: b, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: 600, wy: -100, wz: 80}}
      - {id: low, shape: rectangle, geom: {w: 160, d: 120, h: 45}, offset: {wx: 0, wy: -180}}
      - {id: high, shape: rectangle, geom: {w: 100, d: 100, h: 200}, offset: {wx: 220, wy: -20}}
    connectors:
      - {from: a.port-right, to: b.port-left, routing: straight, stroke: {color: "#c02020", width: 4, dash: "8 4", gradient: {to: "#2030c0"}}}
`

func spatialFixture(t *testing.T) (*Document, string, *routeCollector) {
	t.Helper()
	doc := parseWSH(t, spatialLongLineFixture)
	before, _ := json.Marshal(doc)
	rc := &routeCollector{}
	svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("experimental render mutated its caller")
	}
	if len(rc.routes) != 1 || rc.routes[0].Drawing.Spatial == nil || rc.routes[0].Drawing.Spatial.Mode != "depth" {
		t.Fatalf("no spatial result: %+v", rc.routes)
	}
	return doc, svg, rc
}

func TestSpatialExperimentPreservesPathAndSource(t *testing.T) {
	doc, svg, rc := spatialFixture(t)
	before, _ := json.Marshal(doc)
	plain, old := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	if old[0].Drawing.Spatial != nil || strings.Contains(plain, `conn-visible-`) {
		t.Fatal("experiment leaked into public render")
	}
	if !reflect.DeepEqual(rc.routes[0].Drawing.Points, old[0].Drawing.Points) || !reflect.DeepEqual(rc.routes[0].World3D, old[0].World3D) {
		t.Fatal("visibility rewrote the underlying route")
	}
	if strings.Count(svg, `data-connector="0"`) != 1 {
		t.Fatal("connector paint duplicated")
	}
	lowCovered, highCovered := false, false
	for _, s := range old[0].Drawing.Coverage.Spans {
		for _, id := range s.OpaqueParts {
			lowCovered = lowCovered || id == "low"
		}
	}
	for _, s := range rc.routes[0].Drawing.Spatial.Spans {
		for _, id := range s.Occluders {
			if id == "low" {
				t.Fatal("line above low body was hidden")
			}
			highCovered = highCovered || id == "high"
		}
	}
	if !lowCovered || !highCovered {
		t.Fatal("fixture must expose old low-body error and preserve high-body cover")
	}
	if len(rc.routes[0].Drawing.Spatial.Penetrations) != 0 {
		t.Fatal("legal occlusion fixture penetrates a world body")
	}
	// Reordering and clipping metadata must not change emitted path/style.
	for _, attr := range []string{"d", "stroke", "stroke-width", "stroke-linejoin", "stroke-linecap", "stroke-dasharray"} {
		a, b := spatialPathAttribute(t, plain, attr), spatialPathAttribute(t, svg, attr)
		if a != b {
			t.Fatalf("%s: %q != %q", attr, a, b)
		}
	}
	for _, preclip := range []bool{false, true} {
		c := &routeCollector{preClipOnly: preclip}
		if got := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, c, true); got != svg {
			t.Fatal("observation changed experiment output")
		}
	}
	if got := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, nil, true); got != svg {
		t.Fatal("nil collector changed output")
	}
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("experiment mutated source")
	}
	if dir := os.Getenv("ISOTOPO_SPATIAL_EVIDENCE"); dir != "" {
		for name, data := range map[string][]byte{"long-line.yaml": []byte(spatialLongLineFixture), "legacy.svg": []byte(plain), "experimental.svg": []byte(svg)} {
			if err := os.WriteFile(dir+"/"+name, data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		data, _ := json.MarshalIndent(rc.routes, "", "  ")
		if err := os.WriteFile(dir+"/routes.json", data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSpatialUnsupportedExperimentFallsBackExactly(t *testing.T) {
	for _, mode := range []string{"shape", "curve"} {
		t.Run(mode, func(t *testing.T) {
			doc := parseWSH(t, spatialLongLineFixture)
			if mode == "shape" {
				doc.Scene().Parts[0].Shape = "circle"
			} else {
				doc.Scene().Connectors[0].Routing = "bezier"
			}
			legacy, old := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
			rc := &routeCollector{}
			got := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
			if got != legacy {
				t.Fatal("unsupported scene changed SVG")
			}
			for i, r := range rc.routes {
				if r.Drawing.Spatial == nil || r.Drawing.Spatial.Mode != "legacy" || r.Drawing.Spatial.Reason == "" {
					t.Fatal("missing capability reason")
				}
				rc.routes[i].Drawing.Spatial = nil
			}
			if !reflect.DeepEqual(old, rc.routes) {
				t.Fatal("fallback changed pre-existing route facts")
			}
		})
	}
}

func TestSpatialAmbiguousHeightIsNotGuessed(t *testing.T) {
	// Two different world heights on the same projected segment.
	line := spatialTestLine([3]float64{0, 0, 0}, [3]float64{100, 0, 0})
	r := &ConnectorRoute{Screen: [][2]float64{line.a, line.b, line.a, line.b}, World3D: [][3]float64{{0, 0, 0}, {100, 0, 0}, {20, 20, 20}, {120, 20, 20}}, Drawing: &ConnectorDrawing{Kind: "polyline", Points: [][2]float64{line.a, line.b}}}
	if _, reason := spatialLines(r); reason != "unsupported curve or missing 3-D provenance" {
		t.Fatalf("ambiguous height accepted: %s", reason)
	}
}

func spatialPathAttribute(t *testing.T, svg, key string) string {
	t.Helper()
	start := strings.Index(svg, `<path data-connector="0"`)
	if start < 0 {
		t.Fatal("missing path")
	}
	tag := svg[start:]
	tag = tag[:strings.Index(tag, ">")]
	needle := ` ` + key + `="`
	i := strings.Index(tag, needle)
	if i < 0 {
		return ""
	}
	tag = tag[i+len(needle):]
	return tag[:strings.Index(tag, `"`)]
}

func TestSpatialCommitIsAtomic(t *testing.T) {
	// Feed the already-generated legacy layer to exercise malformed envelope
	// handling. Fallback is required to preserve all pre-existing metadata.
	doc, _, _ := spatialFixture(t)
	_, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	rc := &routeCollector{routes: routes}
	d := rc.routes[0].Drawing
	before, _ := json.Marshal(d)
	svg := `<g data-layer="connectors"><path data-connector="0" d="M 0,0 L 1,1"/></g>`
	got := applyConnectorSpatial(svg, rc, connectorSpatialScene{}, 7)
	if got != svg {
		t.Fatal("failed transaction changed SVG")
	}
	if d.Spatial == nil || d.Spatial.Mode != "legacy" {
		t.Fatal("missing failure diagnostic")
	}
	d.Spatial = nil
	after, _ := json.Marshal(d)
	if string(before) != string(after) {
		t.Fatalf("failed transaction changed drawing metadata\nbefore %s\nafter %s", before, after)
	}
}
