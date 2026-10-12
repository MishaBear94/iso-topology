package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func portFixture(t *testing.T, shape string) *Document {
	t.Helper()
	return parseWSH(t, fmt.Sprintf(`canvas: {padding: 30}
nodes:
  scene:
    shape: composite
    parts:
      - id: source
        shape: %s
        geom: {w: 140, d: 100, h: 40, params: {path: 'M 0 0 L 140 0 L 140 100 L 110 100 L 110 30 L 30 30 L 30 100 L 0 100 Z'}}
        offset: {wx: -80, wy: 35, wz: 17}
      - {id: target, shape: rectangle, geom: {w: 80, d: 70, h: 30}, offset: {wx: 320, wy: 230}}
`, shape))
}
func findPort(t *testing.T, model []PartModel, id, name string) ConnectionPort {
	t.Helper()
	for _, p := range model {
		for _, port := range p.Ports {
			if port.InstanceID == id && port.Name == name {
				return port
			}
		}
	}
	t.Fatalf("missing %s.%s", id, name)
	return ConnectionPort{}
}
func portScreenSVG(t *testing.T, svg string, p ConnectionPort) [2]float64 {
	t.Helper()
	m := regexp.MustCompile(`data-scene-tx="([-\d.]+)" data-scene-ty="([-\d.]+)"`).FindStringSubmatch(svg)
	if m == nil {
		t.Fatal("missing scene origin")
	}
	x, y := projectIso(p.WX, p.WY, p.WZ)
	return [2]float64{x + atof(m[1]), y + atof(m[2])}
}
func actualConnectorEnds(t *testing.T, svg string, index int) ([2]float64, [2]float64) {
	t.Helper()
	m := regexp.MustCompile(fmt.Sprintf(`<path data-connector="%d"[^>]* d="([^"]+)"`, index)).FindStringSubmatch(svg)
	if m == nil {
		t.Fatalf("connector %d not emitted", index)
	}
	nums := regexp.MustCompile(`[-\d.]+`).FindAllString(m[1], -1)
	return [2]float64{atof(nums[0]), atof(nums[1])}, [2]float64{atof(nums[len(nums)-2]), atof(nums[len(nums)-1])}
}
func TestFixedPortsMatchEmittedEndpoints(t *testing.T) {
	for _, shape := range []string{"rectangle", "circle", "person", "cylinder", "custom_path", "cloud", "hexprism", "wedge"} {
		for _, routing := range []string{"orthogonal", "orthogonal2", "straight", "bezier"} {
			for _, name := range fixedPortNames {
				t.Run(shape+"/"+routing+"/"+name, func(t *testing.T) {
					doc := portFixture(t, shape)
					doc.Scene().Parts[0].Stack = &Stack{Count: 3, Gap: 50}
					before, _ := json.Marshal(doc)
					model := BuildInteractionModel(doc)
					after, _ := json.Marshal(doc)
					if string(before) != string(after) {
						t.Fatal("port observation mutated input")
					}
					// Multiple links on the same fixed port must not fan the chosen endpoint.
					for _, other := range []string{"port-left", "port-back", "port-front"} {
						doc.Scene().Connectors = append(doc.Scene().Connectors, &Connector{From: "source~2." + name, To: "target." + other, Routing: routing, Arrow: "triangle"})
					}
					svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
					wantStart := portScreenSVG(t, svg, findPort(t, model, "source~2", name))
					for i, other := range []string{"port-left", "port-back", "port-front"} {
						a, b := actualConnectorEnds(t, svg, i)
						wantEnd := portScreenSVG(t, svg, findPort(t, model, "target", other))
						if math.Hypot(a[0]-wantStart[0], a[1]-wantStart[1]) > .02 || math.Hypot(b[0]-wantEnd[0], b[1]-wantEnd[1]) > .02 {
							t.Fatalf("path %d endpoints %v %v want %v %v", i, a, b, wantStart, wantEnd)
						}
						if routing == "orthogonal2" {
							m := regexp.MustCompile(fmt.Sprintf(`<path data-connector="%d"[^>]* d="([^"]+)"`, i)).FindStringSubmatch(svg)
							n := regexp.MustCompile(`[-\d.]+`).FindAllString(m[1], -1)
							for j, side := range []string{name, other} {
								x, y := atof(n[2])-a[0], atof(n[3])-a[1]
								if j == 1 {
									x, y = atof(n[len(n)-4])-b[0], atof(n[len(n)-3])-b[1]
								}
								dx, dy := portDirection(strings.TrimPrefix(side, "port-"))
								ex, ey := projectIso(dx, dy, 0)
								if x*ex+y*ey <= 0 || math.Abs(x*ey-y*ex)/math.Hypot(x, y) > .015 {
									t.Fatalf("%s terminal points into body or leaves wrong direction: %v", side, m[1])
								}
							}
						}
					}
				})
			}
		}
	}
}

// Read actual emitted polygons/circles independently of ShapeBody. These
// shapes do not need an SVG arc approximation to validate their boundary.
func TestFixedPortsOnActualSVGBody(t *testing.T) {
	for _, shape := range []string{"rectangle", "circle", "custom_path", "hexprism"} {
		t.Run(shape, func(t *testing.T) {
			doc := portFixture(t, shape)
			doc.Scene().Parts = doc.Scene().Parts[:1]
			svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
			model := BuildInteractionModel(doc)
			tr := regexp.MustCompile(`data-part-id="source" transform="translate\(([-\d.]+) ([-\d.]+)\)"`).FindStringSubmatch(svg)
			if tr == nil {
				t.Fatal("missing actual transform")
			}
			var segments [][2][2]float64
			var circles [][3]float64
			dec := xml.NewDecoder(strings.NewReader(svg))
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
				if a["data-face"] == "" {
					continue
				}
				if e.Name.Local == "circle" {
					circles = append(circles, [3]float64{atof(a["cx"]), atof(a["cy"]), atof(a["r"])})
				}
				if e.Name.Local == "polygon" {
					var pts [][2]float64
					for _, pair := range strings.Fields(a["points"]) {
						xy := strings.Split(pair, ",")
						pts = append(pts, [2]float64{atof(xy[0]), atof(xy[1])})
					}
					for i, p := range pts {
						segments = append(segments, [2][2]float64{p, pts[(i+1)%len(pts)]})
					}
				}
			}
			if len(segments)+len(circles) == 0 {
				t.Fatal("oracle found no body")
			}
			for _, port := range model[0].Ports {
				p := portScreenSVG(t, svg, port)
				p[0] -= atof(tr[1])
				p[1] -= atof(tr[2])
				dist := math.Inf(1)
				for _, s := range segments {
					dx, dy := s[1][0]-s[0][0], s[1][1]-s[0][1]
					den := dx*dx + dy*dy
					if den == 0 {
						continue
					}
					u := math.Max(0, math.Min(1, ((p[0]-s[0][0])*dx+(p[1]-s[0][1])*dy)/den))
					dist = math.Min(dist, math.Hypot(p[0]-s[0][0]-u*dx, p[1]-s[0][1]-u*dy))
				}
				for _, c := range circles {
					dist = math.Min(dist, math.Abs(math.Hypot(p[0]-c[0], p[1]-c[1])-c[2]))
				}
				if dist > .08 {
					t.Errorf("%s %v is %g off actual SVG", port.Name, p, dist)
				}
			}
		})
	}
}
func TestPlanFixedPortsPreserveAttachmentsAndReplicaEdges(t *testing.T) {
	for _, from := range fixedPortNames {
		for _, to := range fixedPortNames {
			t.Run(from+"/"+to, func(t *testing.T) {
				doc := portFixture(t, "rectangle")
				doc.Canvas.Projection = "top"
				doc.Scene().Parts[0].Stack = &Stack{Count: 3, Gap: 50}
				doc.Scene().Connectors = []*Connector{{From: "source~2." + from, To: "target." + to, Routing: "orthogonal2"}}
				_, _, edges := buildPlanModel(doc.Scene(), doc.Theme, doc.Canvas)
				if len(edges) != 1 {
					t.Fatal("replica edge disappeared")
				}
				e := edges[0]
				a, _ := planPort(e.from, from)
				b, _ := planPort(e.to, to)
				if e.pts[0] != [2]float64{a.WX, a.WY} || e.pts[len(e.pts)-1] != [2]float64{b.WX, b.WY} || !planAttachmentsValid(e.pts, e.from, e.to) {
					t.Fatalf("invalid pinned plan route %v", e.pts)
				}
				svg := RenderPlan(doc.Scene(), doc.Theme, doc.Canvas, nil)
				v := regexp.MustCompile(`<svg[^>]*viewBox="([-\d.]+) ([-\d.]+) ([\d.]+) ([\d.]+)"`).FindStringSubmatch(svg)
				o := regexp.MustCompile(`data-scene-tx="([-\d.]+)" data-scene-ty="([-\d.]+)"`).FindStringSubmatch(svg)
				for _, p := range e.pts {
					if p[0]+atof(o[1]) < atof(v[1]) || p[1]+atof(o[2]) < atof(v[2]) || p[0]+atof(o[1]) > atof(v[1])+atof(v[3]) || p[1]+atof(o[2]) > atof(v[2])+atof(v[4]) {
						t.Fatalf("fixed port route %v point %v clipped by plan canvas %v origin %v", e.pts, p, v, o)
					}
				}
			})
		}
	}
}
func TestStudioPortsKeepInstanceIdentity(t *testing.T) {
	vm := goja.New()
	script := `var window={interactionModel:[{id:'replicas',anchors:[{name:'legacy'}],ports:[{name:'port-right',instanceId:'replicas',wx:1,wy:2,wz:0},{name:'port-left',instanceId:'replicas~2',wx:10,wy:20,wz:0}]}]};function worldToSvgUser(x,y,z){return [x,y-z];}function clientToSvgUser(x,y){return [x,y];}` + studioFunctions(t, "connectionPorts", "nearestAnchor")
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
	r, err := vm.RunString(`JSON.stringify([connectionPorts('replicas~2').length,nearestAnchor('replicas~2',10,20),connectionPorts('replicas~99').length])`)
	if err != nil || r.String() != `[1,{"name":"port-left","cx":10,"cy":20},0]` {
		t.Fatal(r, err)
	}
}
func TestAddEdgeValidatesInstanceAndPort(t *testing.T) {
	src := []byte("nodes:\n  scene:\n    shape: composite\n    parts:\n      - {id: a, shape: rectangle, stack: {count: 3, gap: 20}}\n      - {id: b, shape: rectangle}\n")
	for _, tc := range []struct {
		from, anchor string
		ok           bool
	}{{"a~2", "port-right", true}, {"a~3", "port-right", false}, {"a~0", "port-right", false}, {"a~02", "port-right", false}, {"a", "typo", false}, {"a.left", "port-right", false}} {
		out, err := ApplyOpText("yaml", src, EditOp{Kind: "add-edge", Fields: map[string]string{"from": tc.from, "to": "b", "fromAnchor": tc.anchor, "toAnchor": "port-left"}})
		if (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, err)
		}
		if !tc.ok && string(out) != string(src) {
			t.Fatal("rejected edit changed source")
		}
		if tc.ok && !strings.Contains(string(out), "a~2.port-right") {
			t.Fatal("instance/port identity lost")
		}
	}
}

func TestFixedPortsDoNotResnapNearbyLegacyRoute(t *testing.T) {
	doc := parseWSH(t, `nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 100, d: 80, h: 1}}
      - {id: b, shape: rectangle, geom: {w: 100, d: 80, h: 1}, offset: {wx: 300}}
    connectors:
      - {from: a.right, to: b.left, routing: straight}
`)
	path := func(svg string, index int) string {
		m := regexp.MustCompile(fmt.Sprintf(`<path data-connector="%d"[^>]* d="([^"]+)"`, index)).FindStringSubmatch(svg)
		if m == nil {
			t.Fatal("missing route")
		}
		return m[1]
	}
	before := path(RenderWithCanvas(doc.Scene(), nil, nil, nil), 0)
	doc.Scene().Connectors = append([]*Connector{{From: "a.port-right", To: "b.port-left", Routing: "straight"}}, doc.Scene().Connectors...)
	after := path(RenderWithCanvas(doc.Scene(), nil, nil, nil), 1)
	if after != before {
		t.Fatalf("new fixed edge moved a legacy path: before %s after %s", before, after)
	}
}

func TestFixedPortRejectsTextOnlyBody(t *testing.T) {
	src := []byte("nodes:\n  scene:\n    shape: composite\n    parts:\n      - {id: a, shape: iso_text, label: Note}\n      - {id: b, shape: rectangle}\n")
	out, err := ApplyOpText("yaml", src, EditOp{Kind: "add-edge", Fields: map[string]string{"from": "a", "to": "b", "fromAnchor": "port-right"}})
	if err == nil || string(out) != string(src) {
		t.Fatal("text-only fixed port must be rejected without source changes")
	}
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	doc.Scene().Connectors = []*Connector{{From: "a.port-right", To: "b.port-left", Routing: "orthogonal2"}}
	found := false
	for _, issue := range Validate(doc) {
		if issue.Severity == SeverityError && strings.Contains(issue.Message, "fixed contour ports require") {
			found = true
		}
	}
	if !found {
		t.Fatal("unsupported fixed port must report an error, not silently lose its edge")
	}
	doc.Canvas = &Canvas{Projection: "top"}
	for _, p := range BuildInteractionModel(doc) {
		if p.ID == "a" && len(p.Ports) != 0 {
			t.Fatal("plan must not offer a fixed port that becomes invalid in iso")
		}
	}
}
