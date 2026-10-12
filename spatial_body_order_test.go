package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func bodyOrderNodes(scene connectorSpatialScene) []painterNode {
	var nodes []painterNode
	for i, b := range scene.boxes {
		n := painterNode{kind: "part", index: i}
		for _, p := range scene.surfaces {
			if p.owner == b.id {
				n.pieces = append(n.pieces, painterBounded(p))
			}
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func TestSpatialBodyOrderWorldOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(20261012))
	checked, changed, mixed := 0, 0, 0
	for trial := 0; trial < 160; trial++ {
		boxes := [][6]float64{{0, 200, 0, 50, 50, 60}, {60, 0, 0, 50, 300, 300}}
		if trial > 0 {
			for i := range boxes {
				for k := 0; k < 3; k++ {
					boxes[i][k] = float64(rng.Intn(220) - 110)
					boxes[i][k+3] = float64(20 + rng.Intn(200))
				}
			}
		}
		scene := prepareConnectorSpatial(spatialTestParts(boxes))
		nodes := bodyOrderNodes(scene)
		budget := spatialPainterWork
		relation, ok := painterBodyRelation(nodes[0], nodes[1], &budget)
		if !ok {
			t.Fatal("two-box comparison exceeded budget", trial)
		}
		// Axis-aligned world volumes supply ray intervals independently of the
		// projected polygon/depth-plane implementation and its ordering graph.
		minX, minY, maxX, maxY := iso25d.ProjectedBounds(boxes)
		witnessed := 0
		for i := 0; i < 43; i++ {
			for j := 0; j < 41; j++ {
				p := [2]float64{minX + (maxX-minX)*(float64(i)+.317)/43, minY + (maxY-minY)*(float64(j)+.613)/41}
				a, ha := rayBoxFront(p, boxes[0])
				b, hb := rayBoxFront(p, boxes[1])
				if !ha || !hb || math.Abs(a-b) < .001 {
					continue
				}
				checked++
				if a > b {
					witnessed |= 1
				} else {
					witnessed |= 2
				}
				if relation == 1 && a < b || relation == 2 && b < a {
					t.Fatalf("trial %d chose %d at %v: %g %g", trial, relation, p, a, b)
				}
			}
		}
		if witnessed == 3 {
			mixed++
			if relation != 3 {
				t.Fatal("crossing envelopes were sorted", trial)
			}
		}
		if trial == 0 {
			if relation != 2 {
				t.Fatalf("unequal-sized origin-order failure unresolved: %d", relation)
			}
			changed++
		}
	}
	if checked < 5000 || mixed == 0 || changed == 0 {
		t.Fatal("insufficient oracle coverage", checked, mixed, changed)
	}
	t.Logf("world ray comparisons=%d; mixed-volume pairs=%d", checked, mixed)
}

func TestSpatialBodyOrderStableArea(t *testing.T) {
	for _, o := range []float64{0, 10000, 1e9, -1e9} {
		p := [][2]float64{{o, o}, {o + .5, o}, {o + .5, o + .25}, {o, o + .25}}
		if a := polygonSignedArea(p); a != .125 {
			t.Fatalf("translation %g changed area to %g", o, a)
		}
		q := [][2]float64{{o, o}, {o + .5, o + .25}, {o + 1, o + .5}, {o + .5, o + .25}}
		if a := polygonSignedArea(q); a != 0 {
			t.Fatalf("collinear area %g", a)
		}
	}
}

const bodyOrderFixture = `canvas: {depthSort: true, background: "#ffffff", aspectRatio: -1}
nodes:
  scene:
    shape: composite
    parts:
      - {id: back, shape: rectangle, geom: {w: 50, d: 50, h: 60}, offset: {wy: 200}, style: {palette: {top: "#c02020", left: "#9f1818", right: "#7d1010"}, stroke: {color: none}}}
      - {id: front, shape: rectangle, geom: {w: 50, d: 300, h: 300}, offset: {wx: 60}, style: {palette: {top: "#a3c5ee", left: "#80b0ee", right: "#6291cb"}, stroke: {color: none}}}
      - {id: a, shape: rectangle, geom: {w: 30, d: 30, h: 20}, offset: {wx: 600, wy: -300}}
      - {id: b, shape: rectangle, geom: {w: 30, d: 30, h: 20}, offset: {wx: 900, wy: -300}}
    connectors:
      - {from: a.port-right, to: b.port-left, routing: straight, stroke: {color: "#c02020", width: 4}}
`

func TestSpatialBodyOrderCommittedScene(t *testing.T) {
	for _, fixture := range []string{spatialLongLineFixture, bodyOrderFixture} {
		doc := parseWSH(t, fixture)
		before, _ := json.Marshal(doc)
		rc := &routeCollector{}
		svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
		if rc.routes[0].Drawing.Spatial.Compositing.Mode == "clipped" {
			t.Fatal("failed to order", rc.routes[0].Drawing.Spatial.Compositing)
		}
		if len(rc.partPaint) != len(rc.paint) {
			t.Fatal("incomplete submitted order")
		}
		last := -1
		changed := false
		for rank, p := range rc.partPaint {
			if p.PaintOrder != rank || p.InstanceID != rc.paint[rank].id {
				t.Fatal("paint observers disagree", rc.partPaint)
			}
			token := fmt.Sprintf(`<g data-paint-order="%d" data-part="%d" data-part-id="%s"`, rank, p.GeometryIndex, p.InstanceID)
			at := strings.Index(svg, token)
			if at <= last {
				t.Fatal("SVG order disagrees", token)
			}
			last = at
			changed = changed || p.GeometryIndex != rank
		}
		if fixture == bodyOrderFixture && !changed {
			t.Fatal("fixture did not exercise a reordered body")
		}
		if fixture == bodyOrderFixture && strings.Index(svg, `data-part-id="front"`) < strings.Index(svg, `data-part-id="back"`) {
			t.Fatal("tall front box still behind")
		}
		// Coverage is independently checked against the committed paint sequence,
		// including the high occluder that formerly preceded this connector.
		for _, r := range rc.routes {
			if !reflect.DeepEqual(r.Drawing.Coverage, observeConnectorCoverage(r.Drawing, rc.paint)) {
				t.Fatal("coverage not committed")
			}
		}
		nilSVG := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, nil, true)
		if nilSVG != svg {
			t.Fatal("collector changed SVG")
		}
		after, _ := json.Marshal(doc)
		if string(before) != string(after) {
			t.Fatal("mutated input")
		}
		if dir := os.Getenv("ISOTOPO_BODY_ORDER_EXPORT"); dir != "" {
			name := "long-line"
			if fixture == bodyOrderFixture {
				name = "body-order"
			}
			public, _ := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
			for n, s := range map[string]string{name + "-after.svg": svg, name + "-public.svg": public, name + ".yaml": fixture} {
				if e := os.WriteFile(dir+"/"+n, []byte(s), 0644); e != nil {
					t.Fatal(e)
				}
			}
			data, _ := json.MarshalIndent(struct {
				Routes []ConnectorRoute     `json:"routes"`
				Parts  []submittedPartPaint `json:"parts"`
			}{rc.routes, rc.partPaint}, "", "  ")
			if e := os.WriteFile(dir+"/"+name+"-scene.json", data, 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestSpatialBodyOrderContentAndCrossing(t *testing.T) {
	doc := parseWSH(t, bodyOrderFixture)
	doc.Scene().Parts[0].Label = "Keep this content order"
	rc := &routeCollector{}
	svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
	if strings.Index(svg, `data-part-id="front"`) > strings.Index(svg, `data-part-id="back"`) {
		t.Fatal("uncertified content reordered")
	}
	p := [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	a := painterNode{kind: "part", pieces: []painterPiece{painterBounded(spatialPiece{poly: p, depth: depthPlane{1, 0, -5}})}}
	b := painterNode{kind: "part", pieces: []painterPiece{painterBounded(spatialPiece{poly: p})}}
	work := spatialPainterWork
	if r, ok := painterBodyRelation(a, b, &work); !ok || r != 3 {
		t.Fatal("mixed body depth accepted", r, ok)
	}
	work = 0
	if _, ok := painterBodyRelation(a, b, &work); ok {
		t.Fatal("body budget ignored")
	}
}

// Read original body groups from actual SVG byte ranges. The reference uses
// the public renderer's unchanged groups in a hand-established world order.
func bodyOrderMarkup(t *testing.T, svg string) (string, []string, map[string]string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(svg))
	depth, start, selected := 0, 0, 0
	id := ""
	var ids []string
	groups := map[string]string{}
	for {
		at := int(dec.InputOffset())
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch e := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && e.Name.Local == "g" {
				for _, a := range e.Attr {
					if a.Name.Local == "data-part-id" {
						selected = depth
						start = at
						id = a.Value
					}
				}
			}
		case xml.EndElement:
			if selected == depth {
				groups[id] = svg[start:int(dec.InputOffset())]
				ids = append(ids, id)
				selected = 0
			}
			depth--
		}
	}
	return svg[:strings.IndexByte(svg, '>')+1], ids, groups
}

func TestSpatialBodyOrderRasterReference(t *testing.T) {
	doc := parseWSH(t, bodyOrderFixture)
	public, _ := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	actual := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, &routeCollector{}, true)
	head, ids, gotGroups := bodyOrderMarkup(t, actual)
	_, _, originalGroups := bodyOrderMarkup(t, public)
	reference := head + `<rect x="-2000" y="-2000" width="5000" height="5000" fill="white"/>`
	got := reference
	for _, id := range []string{"back", "front", "a", "b"} {
		reference += originalGroups[id]
	}
	for _, id := range ids {
		got += gotGroups[id]
	}
	reference += "</svg>"
	got += "</svg>"
	for _, scale := range []float64{.25, 1, 4} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			a, b := spatialRaster(t, reference, scale), spatialRaster(t, got, scale)
			for y := 0; y < a.Bounds().Dy(); y++ {
				for x := 0; x < a.Bounds().Dx(); x++ {
					if d := spatialPixelDelta(a, b, x, y); d > 0 {
						t.Fatalf("body raster differs at %d,%d by %d", x, y, d)
					}
				}
			}
		})
	}
	if dir := os.Getenv("ISOTOPO_BODY_ORDER_EXPORT"); dir != "" {
		for n, s := range map[string]string{"body-reference.svg": reference, "body-actual.svg": got} {
			if e := os.WriteFile(dir+"/"+n, []byte(s), 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
}

func TestSpatialBodyOrderAfterMoves(t *testing.T) {
	for x := 60.; x <= 180; x += 10 {
		doc := parseWSH(t, bodyOrderFixture)
		doc.Scene().Parts[1].Offset.WX = x
		rc := &routeCollector{}
		svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
		if c := rc.routes[0].Drawing.Spatial.Compositing; c.Mode == "clipped" {
			t.Fatalf("move to x=%g fell back: %+v", x, c)
		}
		if len(rc.partPaint) != 4 {
			t.Fatal("move lost committed order")
		}
		if _, ok := submittedConnectorPaths(t, svg)[0]; !ok {
			t.Fatal("move lost route identity")
		}
	}
}
