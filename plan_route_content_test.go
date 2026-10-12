package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func planRouteContentTestDoc(kind string) *Document {
	doc := planPlacementTestDoc()
	if kind == "vertical" {
		doc.Scene().Parts[1].Offset = &WorldPoint{WX: 40, WY: -60}
		doc.Scene().Parts[2].Offset = &WorldPoint{WX: 40, WY: 200}
	} else {
		doc.Scene().Parts[0].Parts[0].Offset.WY = 30
	}
	return doc
}

func TestPlanContentRoutingClearsResidualTitles(t *testing.T) {
	for _, kind := range []string{"vertical", "tight"} {
		for _, weight := range []string{"400", "600", "700"} {
			for _, routing := range []string{"", "orthogonal", "orthogonal2"} {
				t.Run(kind+weight+routing, func(t *testing.T) {
					doc := planRouteContentTestDoc(kind)
					doc.Scene().Parts[0].Style.Text.Weight = weight
					doc.Scene().Connectors[0].Routing = routing
					source, _ := json.Marshal(doc)
					r, _, old := buildPlanModelBase(doc.Scene(), doc.Theme, doc.Canvas, true)
					ox, oy, _, _ := planFrame(r, old, doc.Canvas)
					before := resolvePlanContent(r, old, ox, oy, true)
					if before.texts[0].record.Placement.Status != "blocked" {
						t.Fatal("fixture does not exercise an unresolved title")
					}
					_, _, edges := buildPlanModel(doc.Scene(), doc.Theme, doc.Canvas)
					if slices.Equal(old[0].pts, edges[0].pts) {
						t.Fatal("automatic route did not clear the residual")
					}
					scene, report := RenderSceneWithReport(doc)
					p := scene.PlanLabels[0]
					if p.Placement.Status != "clear-within-scope" || len(p.Placement.Conflicts) != 0 {
						t.Fatalf("routed title remains blocked: %+v", p)
					}
					initial := before.texts[0].record
					if p.Text != initial.Text || p.FontHash != initial.FontHash || p.FontSize != initial.FontSize || p.Budget != initial.Budget || p.Status != initial.Status {
						t.Fatal("route repair changed title content, metrics or budget")
					}
					path := planPathStr(edges[0].pts, func(x float64) float64 { return x + ox }, func(y float64) float64 { return y + oy })
					if !strings.Contains(scene.SVG, `d="`+path+`"`) || RenderScene(doc).SVG != scene.SVG || !reflect.DeepEqual(scene.PlanLabels, report.PlanLabels) {
						t.Fatal("renderer and reported resolved route/content disagree")
					}
					if q := EvaluatePlan(doc.Scene(), doc.Theme, doc.Canvas); q.EdgesThroughNodes != 0 || q.Crossings != 0 || q.TotalEdgeLen != planLen(edges[0].pts) {
						t.Fatal("evaluator did not consume submitted detour")
					}
					got, _ := json.Marshal(doc)
					if string(got) != string(source) {
						t.Fatal("renderer changed author input")
					}
				})
			}
		}
	}
}

func TestPlanContentRoutingPreservesAuthorConstraints(t *testing.T) {
	for _, mode := range []string{"straight", "bend", "waypoints", "waypoints3d", "elbow", "legacy-anchor", "unknown-title", "unknown-pill", "clean"} {
		t.Run(mode, func(t *testing.T) {
			doc := planRouteContentTestDoc("tight")
			c := doc.Scene().Connectors[0]
			switch mode {
			case "straight":
				c.Routing = "straight"
			case "bend":
				c.Bend = &WorldPoint{WY: 4}
			case "waypoints":
				c.Waypoints = []WorldPoint{{WX: 10, WY: 20}}
			case "waypoints3d":
				c.Waypoints3D = []WorldPoint{{WX: 10, WY: 20}}
			case "elbow":
				c.Elbow = "xFirst"
			case "legacy-anchor":
				c.From = "from.right"
			case "unknown-title":
				doc.Scene().Parts[1].Label = "Unknown"
			case "unknown-pill":
				c.Label = "request"
			case "clean":
				doc = planPlacementTestDoc()
			}
			r, _, old := buildPlanModelBase(doc.Scene(), doc.Theme, doc.Canvas, true)
			if got := refinePlanRoutes(r, old, doc.Canvas); !reflect.DeepEqual(got, old) {
				t.Fatal("automatic search changed protected or already-clear route")
			}
		})
	}
	doc := planRouteContentTestDoc("tight")
	r, _, old := buildPlanModelBase(doc.Scene(), doc.Theme, doc.Canvas, true)
	if got := refinePlanRoutesWithBudget(r, old, doc.Canvas, 0); !reflect.DeepEqual(got, old) {
		t.Fatal("exhausted budget changed route")
	}
}

func TestPlanContentRoutingKeepsFixedPorts(t *testing.T) {
	for _, kind := range []string{"tight", "vertical"} {
		doc := planRouteContentTestDoc(kind)
		c := doc.Scene().Connectors[0]
		c.From, c.To = "from.port-right", "to.port-left"
		if kind == "vertical" {
			c.From, c.To = "from.port-front", "to.port-back"
		}
		r, _, old := buildPlanModelBase(doc.Scene(), doc.Theme, doc.Canvas, true)
		got := refinePlanRoutes(r, old, doc.Canvas)
		if !got[0].refined {
			t.Fatalf("%s fixed-port residual was not exercised", kind)
		}
		a, b := old[0].pts, got[0].pts
		if a[0] != b[0] || a[len(a)-1] != b[len(b)-1] || !planAttachmentsValid(b, r[2], r[3]) {
			t.Fatal("fixed endpoint or outward attachment changed")
		}
	}
}

func TestPlanContentRoutingRetainsFrameAndExportMargin(t *testing.T) {
	doc := planRouteContentTestDoc("tight")
	doc.Canvas.Padding = 1
	c := doc.Scene().Connectors[0]
	c.From, c.To = "from.port-right", "to.port-left"
	for _, p := range doc.Scene().Parts {
		if p.Offset == nil {
			p.Offset = &WorldPoint{}
		}
		p.Offset.WX += .127
		p.Offset.WY -= .234
	}
	r, _, old := buildPlanModelBase(doc.Scene(), doc.Theme, doc.Canvas, true)
	ox, oy, w, h := planFrame(r, old, doc.Canvas)
	got := refinePlanRoutes(r, old, doc.Canvas)
	if !got[0].refined {
		t.Fatal("fixture did not exercise a detour")
	}
	x, y, w2, h2 := planFrame(r, got, doc.Canvas)
	if x != ox || y != oy || w != w2 || h != h2 {
		t.Fatal("fixed-port detour changed the origin used by its clearance proof")
	}
	scene := RenderScene(doc)
	var root struct {
		ViewBox string `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal([]byte(scene.SVG), &root); err != nil {
		t.Fatal(err)
	}
	vx, vy, vw, vh, err := parseViewBox(root.ViewBox)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range planRouteScreenPoints(got[0], ox, oy) {
		margin := 16 + planRouteWidth(c)/2
		if p[0]-margin < vx-.01 || p[1]-margin < vy-.01 || p[0]+margin > vx+vw+.01 || p[1]+margin > vy+vh+.01 {
			t.Fatal("export clips accepted route casing or its margin")
		}
	}
}

func TestPlanContentRoutingArrowFitsTerminalLeg(t *testing.T) {
	for _, kind := range []string{"tight", "vertical"} {
		for _, width := range []float64{2.4, 5} {
			doc := planRouteContentTestDoc(kind)
			c := doc.Scene().Connectors[0]
			c.Arrow = "triangle"
			c.Stroke = &Stroke{Width: &width}
			r, _, old := buildPlanModelBase(doc.Scene(), doc.Theme, doc.Canvas, true)
			got := refinePlanRoutes(r, old, doc.Canvas)
			if !got[0].refined {
				t.Fatalf("%s width %g did not exercise a detour", kind, width)
			}
			ox, oy, _, _ := planFrame(r, got, doc.Canvas)
			p := planRouteScreenPoints(got[0], ox, oy)
			a, b := p[len(p)-2], p[len(p)-1]
			// Independent marker geometry: the marker's rear vertex must
			// remain beyond the last elbow plus casing and a reading gap.
			length := math.Hypot(a[0]-b[0], a[1]-b[1])
			rear := 8.5 * .9 * width
			if length < rear+(width+3)/2+6 {
				t.Fatalf("arrow extends past its last elbow: length %g, rear %g", length, rear)
			}
		}
	}
}

func TestPlanContentRoutingRejectsSurfaceGrazes(t *testing.T) {
	from := planRect{id: "from", x: 0, y: 0, w: 20, d: 20, h: 20}
	to := planRect{id: "to", x: 120, y: 0, w: 20, d: 20, h: 20}
	edge := planEdge{ci: 0, c: &Connector{Arrow: "none", Routing: "orthogonal"}, from: from, to: to, pts: [][2]float64{{20, 10}, {120, 10}}}
	for _, kind := range []string{"body-casing", "other-floor", "route-casing", "route-touch", "arrow", "endpoint-reentry"} {
		t.Run(kind, func(t *testing.T) {
			e := edge
			r := []planRect{from, to}
			others := []planEdge{e}
			switch kind {
			case "body-casing", "other-floor":
				body := planRect{id: "near", x: 40, y: 13, w: 20, d: 20, h: 20}
				if kind == "other-floor" {
					body.z = 1000
				}
				if routeHitsRect(e.pts, body) {
					t.Fatal("fixture is a centerline tunnel instead of a casing graze")
				}
				r = append(r, body)
			case "route-casing":
				others = append(others, planEdge{ci: 1, c: &Connector{Arrow: "none"}, pts: [][2]float64{{40, 15}, {80, 15}}})
			case "route-touch":
				others = append(others, planEdge{ci: 1, c: &Connector{Arrow: "none"}, pts: [][2]float64{{60, -20}, {60, 10}}})
			case "arrow":
				others = append(others, planEdge{ci: 1, c: &Connector{}, pts: [][2]float64{{60, 60}, {60, 30}}})
			case "endpoint-reentry":
				e.pts = [][2]float64{{20, 10}, {30, 10}, {30, -2}, {-10, -2}, {-10, 30}, {110, 30}, {110, 10}, {120, 10}}
			}
			if planRouteSurfaceClear(e, r, others, 28, 28) {
				t.Fatal("accepted a visible overlap or insufficient clearance")
			}
		})
	}
}

func TestPlanContentRoutingDoesNotTransferDefects(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 18821))
	moves := 0
	for trial := 0; trial < 72; trial++ {
		doc := planRouteContentTestDoc([]string{"tight", "vertical"}[trial%2])
		scene := doc.Scene()
		w := 1 + rng.Float64()*10
		scene.Connectors[0].Stroke = &Stroke{Width: &w}
		if trial%3 == 0 {
			scene.Connectors[0].Label, scene.Connectors[0].LabelFontFamily = "request", "Source Sans Pro"
		}
		for i := 0; i < 3; i++ {
			scene.Parts = append(scene.Parts, &CompositePart{ID: fmt.Sprint("obstacle-", i), Shape: "rectangle", Geom: &Geom{W: 8 + rng.Float64()*20, D: 10 + rng.Float64()*20, H: 20}, Offset: &WorldPoint{WX: rng.Float64()*430 - 80, WY: rng.Float64()*280 - 60}})
		}
		if trial%4 == 0 {
			scene.Connectors = append(scene.Connectors, &Connector{From: "obstacle-0", To: "obstacle-1", Routing: "straight", Arrow: "none", Label: "peer", LabelFontFamily: "Source Sans Pro"})
		}
		r, _, old := buildPlanModelBase(scene, doc.Theme, doc.Canvas, true)
		ox, oy, _, _ := planFrame(r, old, doc.Canvas)
		before := resolvePlanContent(r, old, ox, oy, true)
		got := refinePlanRoutes(r, old, doc.Canvas)
		after := resolvePlanContent(r, got, ox, oy, true)
		bd, bc := planRouteContentDefects(before)
		ad, ac := planRouteContentDefects(after)
		var leaves []planRect
		for _, x := range r {
			if !x.container {
				leaves = append(leaves, x)
			}
		}
		evalGeomWithDefects(leaves, old, bd)
		evalGeomWithDefects(leaves, got, ad)
		if !ad.noWorseThan(bd) || ac > bc {
			t.Fatalf("trial %d introduced or deepened another object's defect", trial)
		}
		if got[0].refined {
			moves++
			if ac >= bc && planRouteTitleContacts(after, 0) >= planRouteTitleContacts(before, 0) {
				t.Fatal("changed route without eliminating a title/route conflict")
			}
			// Independent rectangle check of the submitted orthogonal stroke,
			// including its full casing and six-unit reading gap.
			radius := planRouteWidth(got[0].c) / 2
			p := planRouteScreenPoints(got[0], ox, oy)
			for _, body := range r {
				if body.container || body.id == got[0].from.id || body.id == got[0].to.id {
					continue
				}
				bx, by := svgCoord(body.x+ox), svgCoord(body.y+oy)
				pad := radius + 6 + planLineW(body)/2
				for i := 1; i < len(p); i++ {
					a, b := p[i-1], p[i]
					if math.Max(a[0], b[0])+pad > bx && math.Min(a[0], b[0])-pad < bx+svgCoord(body.w) && math.Max(a[1], b[1])+pad > by && math.Min(a[1], b[1])-pad < by+svgCoord(body.d) {
						t.Fatalf("trial %d submitted a route inside another footprint's clearance", trial)
					}
				}
			}
		}
		if !reflect.DeepEqual(got, refinePlanRoutes(r, old, doc.Canvas)) {
			t.Fatal("route selection is nondeterministic")
		}
	}
	if moves < 12 {
		t.Fatalf("only %d trials exercised accepted routes", moves)
	}
	t.Logf("accepted %d of 72 scenes", moves)
}
