package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// The oracle reads the emitted path commands independently of the collector;
// a pre-clip track, a quadratic control polygon or a pre-snap path all fail.
func submittedConnectorPaths(t *testing.T, svg string) map[int]struct {
	kind   string
	pts    [][2]float64
	before int
} {
	t.Helper()
	out := map[int]struct {
		kind   string
		pts    [][2]float64
		before int
	}{}
	dec := xml.NewDecoder(strings.NewReader(svg))
	parts := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		e, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		a := map[string]string{}
		for _, v := range e.Attr {
			a[v.Name.Local] = v.Value
		}
		if _, ok := a["data-part"]; ok {
			parts++
		}
		id, ok := a["data-connector"]
		if e.Name.Local != "path" || !ok {
			continue
		}
		idx, _ := strconv.Atoi(id)
		kind := "polyline"
		if strings.Contains(a["d"], "Q") {
			kind = "quadratic"
		}
		var pts [][2]float64
		for _, v := range strings.Fields(a["d"]) {
			if v == "M" || v == "L" || v == "Q" {
				continue
			}
			var p [2]float64
			if n, err := fmt.Sscanf(v, "%f,%f", &p[0], &p[1]); n != 2 || err != nil {
				t.Fatalf("path token %q", v)
			}
			pts = append(pts, p)
		}
		out[idx] = struct {
			kind   string
			pts    [][2]float64
			before int
		}{kind, pts, parts}
	}
	return out
}

func TestConnectorDrawingMatchesSubmittedSVG(t *testing.T) {
	walkSampleDocs(t, func(t *testing.T, name string, doc *Document) {
		if doc.Scene() == nil || (doc.Canvas != nil && doc.Canvas.Projection == "top") {
			return
		}
		before, _ := json.Marshal(doc)
		svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, doc.Annotations)
		paths := submittedConnectorPaths(t, svg)
		if len(paths) != len(routes) {
			t.Fatalf("%d paths, %d records", len(paths), len(routes))
		}
		for _, r := range routes {
			d := r.Drawing
			p := paths[r.Index]
			if d == nil || d.Kind != p.kind || !reflect.DeepEqual(d.Points, p.pts) || d.PaintBeforePart != p.before {
				t.Fatalf("edge %d record != actual SVG: %+v %+v", r.Index, d, p)
			}
			if len(r.World3D) != len(r.Screen) {
				t.Fatalf("edge %d lost height track", r.Index)
			}
			for i, w := range r.World3D {
				x, y := projectIso(w[0], w[1], w[2])
				x += d.ProjectionOrigin[0]
				y += d.ProjectionOrigin[1]
				if math.Hypot(x-r.Screen[i][0], y-r.Screen[i][1]) > 2e-6 {
					t.Fatalf("edge %d point %d inconsistent 3D projection", r.Index, i)
				}
			}
			assertCoveragePartition(t, d)
		}
		after, _ := json.Marshal(doc)
		if string(before) != string(after) {
			t.Fatal("observation changed input")
		}
	})
}

func assertCoveragePartition(t *testing.T, d *ConnectorDrawing) {
	t.Helper()
	n := len(d.Points) - 1
	if d.Kind == "quadratic" {
		n = 1
	}
	for i := 0; i < n; i++ {
		end := 0.0
		for _, s := range d.Coverage.Spans {
			if s.Segment != i {
				continue
			}
			if math.Abs(s.T0-end) > 1e-11 || s.T1 <= s.T0 || s.T1 > 1 {
				t.Fatalf("gap/overlap in segment %d: %+v", i, d.Coverage.Spans)
			}
			end = s.T1
		}
		if end != 1 {
			t.Fatalf("incomplete segment %d coverage: %v", i, end)
		}
	}
}

func TestConnectorDrawingCoverageIntervals(t *testing.T) {
	box := func(x0, y0, x1, y1 float64) screenRegion {
		return screenRegion{{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}}
	}
	for _, tc := range []struct {
		name    string
		pts     [][2]float64
		kind    string
		regions []screenRegion
		want    []ConnectorCoverSpan
	}{
		{"overlapping blockers", [][2]float64{{0, 0}, {10, 0}}, "polyline", []screenRegion{box(2, -1, 6, 1), box(4, -1, 8, 1)}, []ConnectorCoverSpan{{0, 0, .2, []string{}}, {0, .2, .4, []string{"a"}}, {0, .4, .6, []string{"a", "b"}}, {0, .6, .8, []string{"b"}}, {0, .8, 1, []string{}}}},
		{"quadratic cuts", [][2]float64{{0, 0}, {5, 10}, {10, 0}}, "quadratic", []screenRegion{box(2, -1, 8, 10)}, []ConnectorCoverSpan{{0, 0, .2, []string{}}, {0, .2, .8, []string{"a"}}, {0, .8, 1, []string{}}}},
		{"tangent only", [][2]float64{{0, 0}, {5, 10}, {10, 0}}, "quadratic", []screenRegion{box(2, 5, 8, 10)}, []ConnectorCoverSpan{{0, 0, 1, []string{}}}},
		{"collinear edge", [][2]float64{{0, 0}, {10, 0}}, "polyline", []screenRegion{box(2, 0, 8, 10)}, []ConnectorCoverSpan{{0, 0, .2, []string{}}, {0, .2, .8, []string{"a"}}, {0, .8, 1, []string{}}}},
		{"concave gap", [][2]float64{{0, 3}, {10, 3}}, "polyline", []screenRegion{{{{2, 0}, {8, 0}, {8, 5}, {6, 5}, {6, 1}, {4, 1}, {4, 5}, {2, 5}}}}, []ConnectorCoverSpan{{0, 0, .2, []string{}}, {0, .2, .4, []string{"a"}}, {0, .4, .6, []string{}}, {0, .6, .8, []string{"a"}}, {0, .8, 1, []string{}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &ConnectorDrawing{Kind: tc.kind, Points: tc.pts}
			var parts []connectorPaintPart
			for i, r := range tc.regions {
				parts = append(parts, connectorPaintPart{id: string(rune('a' + i)), region: r})
			}
			d.Coverage = observeConnectorCoverage(d, parts)
			assertCoveragePartition(t, d)
			if len(d.Coverage.Spans) != len(tc.want) {
				t.Fatalf("%+v want %+v", d.Coverage.Spans, tc.want)
			}
			for i, w := range tc.want {
				g := d.Coverage.Spans[i]
				if math.Abs(g.T0-w.T0) > 1e-10 || math.Abs(g.T1-w.T1) > 1e-10 || !slices.Equal(g.OpaqueParts, w.OpaqueParts) {
					t.Fatalf("%+v want %+v", g, w)
				}
			}
			// Move the same bodies behind the route; they must cease covering it.
			d.PaintBeforePart = len(parts)
			c := observeConnectorCoverage(d, parts)
			for _, s := range c.Spans {
				if len(s.OpaqueParts) > 0 {
					t.Fatal("body behind route counted as cover")
				}
			}
		})
	}
}

func TestConnectorDrawingOverlayAndClipping(t *testing.T) {
	doc := parseWSH(t, curveConnectorFixture)
	svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	d := routes[0].Drawing
	if d.Points[0] == routes[0].Screen[0] || d.Points[len(d.Points)-1] == routes[0].Screen[len(routes[0].Screen)-1] {
		t.Fatal("drawing retained unclipped anchors")
	}
	dec := xml.NewDecoder(strings.NewReader(svg))
	inOverlay := false
	arrows, labels := 0, 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		e, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		a := map[string]string{}
		for _, v := range e.Attr {
			a[v.Name.Local] = v.Value
		}
		if a["data-layer"] == "connector-overlay" {
			inOverlay = true
		}
		if !inOverlay {
			continue
		}
		switch e.Name.Local {
		case "polygon":
			var pts [][2]float64
			for _, s := range strings.Fields(a["points"]) {
				var p [2]float64
				fmt.Sscanf(s, "%f,%f", &p[0], &p[1])
				pts = append(pts, p)
			}
			if !reflect.DeepEqual(pts, d.Arrow) {
				t.Fatalf("arrow %+v != drawing %+v", pts, d.Arrow)
			}
			arrows++
		case "rect":
			r := [4]float64{atof(a["x"]), atof(a["y"]), atof(a["width"]), atof(a["height"])}
			if d.Label == nil || r != d.Label.Rect {
				t.Fatalf("pill %+v != drawing %+v", r, d.Label)
			}
			labels++
		case "text":
			if [2]float64{atof(a["x"]), atof(a["y"])} != d.Label.Anchor {
				t.Fatal("text anchor != submitted SVG")
			}
		}
	}
	if arrows != 1 || labels != 1 {
		t.Fatalf("overlay not exercised: %d %d", arrows, labels)
	}
	// A consumer can edit its returned record without affecting subsequent
	// renders or the authored document.
	d.Points[0][0] += 1000
	d.Arrow[0][0] += 1000
	again, _ := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	if again != svg {
		t.Fatal("returned data aliases render state")
	}
}

func TestPreclipCollectionKeepsLegacyRouteContract(t *testing.T) {
	doc := parseWSH(t, routeCollectFixture)
	svg, full := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	legacySVG, legacy := renderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil, true)
	if svg != legacySVG || len(full) != len(legacy) {
		t.Fatal("collection mode changed output")
	}
	for i, r := range legacy {
		if r.Drawing != nil || !reflect.DeepEqual(r.World, full[i].World) || !reflect.DeepEqual(r.Screen, full[i].Screen) {
			t.Fatalf("legacy route %d drifted", i)
		}
	}
}

func TestConnectorDrawingCoverageUnsupportedAndTransparent(t *testing.T) {
	for _, tc := range []struct {
		name, shape       string
		opts              iso25d.ConvertOpts
		opaque, uncertain bool
	}{
		{"opaque", "rectangle", iso25d.ConvertOpts{}, true, false},
		{"translucent", "rectangle", iso25d.ConvertOpts{Opacity: .5}, false, false},
		{"wireframe", "rectangle", iso25d.ConvertOpts{Wireframe: true}, false, false},
		{"blur", "rectangle", iso25d.ConvertOpts{Blur: 3}, false, true},
		{"array envelope", "array2d", iso25d.ConvertOpts{}, false, true},
		{"rack envelope", "rack", iso25d.ConvertOpts{}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := &routeCollector{}
			rc.addPaint("blocker", tc.shape, tc.opts, 0, 0, 0)
			d := &ConnectorDrawing{Kind: "polyline", Points: [][2]float64{{-300, 0}, {300, 0}}}
			c := observeConnectorCoverage(d, rc.paint)
			opaque := false
			for _, s := range c.Spans {
				opaque = opaque || len(s.OpaqueParts) > 0
			}
			if opaque != tc.opaque || slices.Contains(c.UnresolvedParts, "blocker") != tc.uncertain {
				t.Fatalf("unexpected coverage %+v", c)
			}
		})
	}
}

func BenchmarkConnectorDrawing(b *testing.B) {
	for _, n := range []int{10, 40, 100} {
		scene := &Node{Shape: "composite"}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("p%d", i)
			scene.Parts = append(scene.Parts, &CompositePart{ID: id, Shape: "rectangle", Geom: &Geom{W: 60, D: 50, H: 35}, Offset: &WorldPoint{WX: float64(i%10) * 150, WY: float64(i/10) * 140}})
			if i > 0 {
				scene.Connectors = append(scene.Connectors, &Connector{From: fmt.Sprintf("p%d.port-right", i-1), To: id + ".port-left", Routing: "straight"})
			}
		}
		for _, collect := range []bool{false, true} {
			b.Run(fmt.Sprintf("nodes-%d/collect-%v", n, collect), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if collect {
						RenderWithRouteInfo(scene, nil, nil, nil)
					} else {
						Render(scene, nil)
					}
				}
			})
		}
	}
}

func TestElevatedTargetKeepsOrthogonalRiser(t *testing.T) {
	for _, routing := range []string{"orthogonal", "orthogonal2"} {
		for _, heights := range [][2]float64{{0, 80}, {80, 0}, {30, 110}, {110, 30}, {0, 3}, {3, 0}} {
			for _, from := range fixedPortNames {
				for _, to := range fixedPortNames {
					t.Run(fmt.Sprintf("%s/%g-%g/%s/%s", routing, heights[0], heights[1], from, to), func(t *testing.T) {
						doc := portFixture(t, "rectangle")
						doc.Scene().Parts[0].Offset.WZ = heights[0]
						doc.Scene().Parts[1].Offset.WZ = heights[1]
						doc.Scene().Connectors = []*Connector{{From: "source." + from, To: "target." + to, Routing: routing, Arrow: "triangle"}}
						svg, rr := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
						r := rr[0]
						for i := 1; i < len(r.World3D); i++ {
							axes := 0
							for k := 0; k < 3; k++ {
								if math.Abs(r.World3D[i][k]-r.World3D[i-1][k]) > 1e-6 {
									axes++
								}
							}
							if axes > 1 {
								t.Fatalf("3D segment changed %d axes: %v -> %v", axes, r.World3D[i-1], r.World3D[i])
							}
						}
						// Independently inspect submitted SVG: every segment must be on
						// an iso x/y axis or pure vertical, including the raised target.
						p := submittedConnectorPaths(t, svg)[0].pts
						for i := 1; i < len(p); i++ {
							dx, dy := p[i][0]-p[i-1][0], p[i][1]-p[i-1][1]
							if math.Abs(dx) > .02 && math.Abs(math.Abs(dy)-math.Abs(dx)/math.Sqrt(3)) > .03 {
								t.Fatalf("non-isometric segment %v -> %v", p[i-1], p[i])
							}
						}
					})
				}
			}
		}
	}
}

// Optional artifact output keeps the fixture reproducible without shipping
// generated evidence inside the repository.
func TestConnectorDrawingArtifact(t *testing.T) {
	dir := os.Getenv("ISOTOPO_ROUTE_QA_DIR")
	if dir == "" {
		t.Skip("artifact export disabled")
	}
	doc := portFixture(t, "rectangle")
	doc.Canvas = &Canvas{Padding: 35, AspectRatio: -1, Background: "#f8fafc"}
	doc.Scene().Parts[0].Label = "Source"
	doc.Scene().Parts[0].Offset.WZ = 0
	doc.Scene().Parts[1].Label = "Raised target"
	doc.Scene().Parts[1].Offset.WZ = 100
	doc.Scene().Connectors = []*Connector{{From: "source.port-right", To: "target.port-left", Routing: "orthogonal2", Arrow: "triangle", Label: "Cross-height link"}}
	svg, r := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	if err := os.WriteFile(dir+"/raised-target.svg", []byte(svg), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(dir+"/raised-target.json", b, 0600); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, shape := range []string{"rectangle", "circle", "cylinder", "person", "custom_path", "hexprism", "wedge", "cloud"} {
		for _, depth := range []bool{false, true} {
			name := fmt.Sprintf("coverage-%s-%v", shape, depth)
			f := portFixture(t, shape)
			f.Canvas = &Canvas{Padding: 30, AspectRatio: -1, DepthSort: depth}
			// The two sources lie beyond the same blocker on four sides. Forced
			// straight and curved paths exercise observable coverage independently
			// of automatic routing's ability to avoid that body.
			blocker := f.Scene().Parts[0]
			blocker.ID = "blocker"
			blocker.Offset = &WorldPoint{WX: 120, WY: 100, WZ: 20}
			f.Scene().Parts = []*CompositePart{
				{ID: "a", Shape: "rectangle", Geom: &Geom{W: 60, D: 50, H: 30}, Offset: &WorldPoint{WX: -100, WY: 100}},
				blocker,
				{ID: "b", Shape: "rectangle", Geom: &Geom{W: 60, D: 50, H: 30}, Offset: &WorldPoint{WX: 420, WY: 100}},
				{ID: "c", Shape: "rectangle", Geom: &Geom{W: 60, D: 50, H: 30}, Offset: &WorldPoint{WX: 120, WY: -100}},
				{ID: "d", Shape: "rectangle", Geom: &Geom{W: 60, D: 50, H: 30}, Offset: &WorldPoint{WX: 120, WY: 400}},
			}
			f.Scene().Connectors = []*Connector{
				{From: "a.port-right", To: "b.port-left", Routing: "straight"},
				{From: "c.port-front", To: "d.port-back", Routing: "straight"},
				{From: "a.port-right", To: "d.port-back", Routing: "bezier"},
			}
			svg, routes := RenderWithRouteInfo(f.Scene(), f.Theme, f.Canvas, nil)
			if err := os.WriteFile(dir+"/"+name+".svg", []byte(svg), 0600); err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(routes)
			if err := os.WriteFile(dir+"/"+name+".json", b, 0600); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
	}
	b, _ = json.Marshal(names)
	if err := os.WriteFile(dir+"/cases.json", b, 0600); err != nil {
		t.Fatal(err)
	}
}
