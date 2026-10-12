package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

func TestInteractionSelectionUsesActualSphereSVG(t *testing.T) {
	doc := parseWSH(t, `nodes:
  scene:
    shape: composite
    parts:
      - {id: orb, shape: circle, geom: {w: 100}, offset: {wx: 70, wy: -40, wz: 30}}
`)
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	group := regexp.MustCompile(`data-part-id="orb" transform="translate\(([-\d.]+) ([-\d.]+)\)"`).FindStringSubmatch(svg)
	circle := regexp.MustCompile(`<circle data-face="sphere" cx="([\d.]+)" cy="([\d.]+)" r="([\d.]+)"`).FindStringSubmatch(svg)
	origin := regexp.MustCompile(`data-scene-tx="([-\d.]+)" data-scene-ty="([-\d.]+)"`).FindStringSubmatch(svg)
	if group == nil || circle == nil || origin == nil {
		t.Fatal("missing actual circle or scene frame")
	}
	cx, cy, r := atof(group[1])+atof(circle[1])-atof(origin[1]), atof(group[2])+atof(circle[2])-atof(origin[2]), atof(circle[3])
	model := BuildInteractionModel(doc)
	if len(model) != 1 {
		t.Fatal(model)
	}
	region := screenRegion(model[0].SelectionRegions)
	for i := -12; i <= 12; i++ {
		for j := -12; j <= 12; j++ {
			x, y := float64(i)*r/10, float64(j)*r/10
			if math.Abs(math.Hypot(x, y)-r) < .15 {
				continue
			}
			if region.contains([2]float64{cx + x, cy + y}) != (x*x+y*y < r*r) {
				t.Fatalf("actual SVG/model mismatch at %v; centre=%v radius=%g bounds=%+v origin=%v group=%v", [2]float64{x, y}, [2]float64{cx, cy}, r, regionBounds(region), origin, group)
			}
		}
	}
}

func TestInteractionSelectionContentAndInstances(t *testing.T) {
	doc := parseWSH(t, `nodes:
  scene:
    shape: composite
    parts:
      - id: cutout
        shape: custom_path
        geom: {w: 200, d: 200, h: 20, params: {path: 'M 0 0 L 200 0 L 200 200 L 160 200 L 160 40 L 40 40 L 40 200 L 0 200 Z'}}
      - {id: user, shape: person, label: User}
      - {id: replicas, shape: rectangle, geom: {w: 60, d: 60, h: 30}, stack: {count: 3, gap: 70}}
      - {id: title, shape: iso_text, label: Heading}
      - {id: cloud, shape: array2d}
`)
	before, _ := json.Marshal(doc)
	model := BuildInteractionModel(doc)
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("interaction observation mutated source")
	}
	byID := map[string]PartModel{}
	for _, p := range model {
		byID[p.ID] = p
	}
	for _, tc := range []struct {
		id   string
		p    [2]float64
		want bool
	}{
		{"cutout", [2]float64{0, 100}, false}, // visible U opening, not a convex envelope
		{"cutout", [2]float64{0, 0}, true},
		{"user", [2]float64{0, -.8*33.6 + 10}, true}, // actual head above body
		{"user", [2]float64{0, 112}, true},           // lower screen-flat label
		{"replicas", [2]float64{0, -120}, true},      // highest replica, missing in old model
		{"title", [2]float64{12, 3}, true},
		{"title", [2]float64{100, 100}, false},
	} {
		if got := screenRegion(byID[tc.id].SelectionRegions).contains(tc.p); got != tc.want {
			t.Errorf("%s %v got %v want %v", tc.id, tc.p, got, tc.want)
		}
	}
	if !byID["cloud"].SelectionApproximate || !byID["title"].SelectionApproximate {
		t.Fatal("envelope and text estimates must retain their capability marker")
	}
}

func TestInteractionParentMembershipAndDropCycle(t *testing.T) {
	doc := parseWSH(t, `nodes:
  scene:
    shape: composite
    parts:
      - id: outer
        shape: group
        geom: {w: 200, d: 200}
        parts:
          - id: inner
            shape: group
            geom: {w: 100, d: 100}
            parts:
              - {id: leaf, shape: rectangle, geom: {w: 30, d: 30}}
`)
	model := BuildInteractionModel(doc)
	parents := map[string]string{}
	for _, pm := range model {
		parents[pm.ID] = pm.ParentID
	}
	if parents["inner"] != "outer" || parents["leaf"] != "inner" {
		t.Fatal(parents)
	}
	if target := WorldDropTarget(model, "outer", 0); target != "" {
		t.Fatalf("container cannot target descendant %q", target)
	}
}

func TestStudioSelectionGeometry(t *testing.T) {
	vm := goja.New()
	if _, err := vm.RunString(studioSelectionJS); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		poly [][2]float64
		rect [4]float64
		want bool
	}{
		{"inside", [][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}, [4]float64{5, 5, 6, 6}, true},
		{"outside", [][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}, [4]float64{21, 5, 22, 6}, false},
		{"edge contact", [][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}, [4]float64{20, 5, 22, 6}, true},
		{"crossing without vertices inside", [][2]float64{{0, 8}, {20, 8}, {20, 9}, {0, 9}}, [4]float64{5, 5, 6, 12}, true},
		{"C notch", [][2]float64{{0, 0}, {20, 0}, {20, 4}, {4, 4}, {4, 16}, {20, 16}, {20, 20}, {0, 20}}, [4]float64{10, 8, 12, 10}, false},
		{"contains whole polygon", [][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}, [4]float64{-1, -1, 21, 21}, true},
	}
	fn, ok := goja.AssertFunction(vm.Get("polygonTouchesRect"))
	if !ok {
		t.Fatal("missing geometry function")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for reverse := 0; reverse < 2; reverse++ {
				poly := append([][2]float64(nil), tc.poly...)
				if reverse == 1 {
					for a, b := 0, len(poly)-1; a < b; a, b = a+1, b-1 {
						poly[a], poly[b] = poly[b], poly[a]
					}
				}
				data, _ := json.Marshal(poly)
				v, err := vm.RunString(string(data))
				if err != nil {
					t.Fatal(err)
				}
				r, err := fn(goja.Undefined(), v, vm.ToValue(map[string]float64{"left": tc.rect[0], "top": tc.rect[1], "right": tc.rect[2], "bottom": tc.rect[3]}))
				if err != nil {
					t.Fatal(err)
				}
				if r.ToBoolean() != tc.want {
					t.Fatal("incorrect rectangle/polygon intersection")
				}
			}
		})
	}
	result, err := vm.RunString(`JSON.stringify([...dragMemberIds([{id:'leaf',parentId:'inner'},{id:'outer'},{id:'inner',parentId:'outer'},{id:'other'}],['outer'])].sort())`)
	if err != nil || result.String() != `["inner","leaf","outer"]` {
		t.Fatalf("descendant closure: %v %v", result, err)
	}
}

func TestPlanInteractionFrameMatchesEmittedRect(t *testing.T) {
	doc := parseWSH(t, `canvas: {projection: top, padding: 43}
nodes:
  scene:
    shape: composite
    parts:
      - {id: n, shape: rectangle, geom: {w: 90, d: 70, h: 150}, offset: {wx: -200, wy: 80, wz: 90}}
`)
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	origin := regexp.MustCompile(`data-projection="top" data-scene-tx="([-\d.]+)" data-scene-ty="([-\d.]+)"`).FindStringSubmatch(svg)
	node := regexp.MustCompile(`<g data-part-id="n"><rect x="([-\d.]+)" y="([-\d.]+)" width="([-\d.]+)" height="([-\d.]+)"`).FindStringSubmatch(svg)
	if origin == nil || node == nil {
		t.Fatal("plan output lacks interaction frame or identity")
	}
	pm := BuildInteractionModel(doc)[0]
	x, y := pm.SelectionRegions[0][0][0]+atof(origin[1]), pm.SelectionRegions[0][0][1]+atof(origin[2])
	if math.Abs(x-atof(node[1])) > .01 || math.Abs(y-atof(node[2])) > .01 {
		t.Fatal(fmt.Sprintf("model (%g,%g) does not match actual rect", x, y))
	}
	if !strings.Contains(svg, `height="70.00"`) {
		t.Fatal("height must not affect plan footprint")
	}
}
