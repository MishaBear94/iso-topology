package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

func crowdingTestEdge(index int, from, to string, points ...[2]float64) planEdge {
	return planEdge{ci: index, c: &Connector{From: from, To: to, Arrow: "none", Routing: "orthogonal"}, from: planRect{id: from}, to: planRect{id: to}, pts: points}
}

func TestPlanRouteCrowdingMeasuresActualPairs(t *testing.T) {
	a := crowdingTestEdge(2, "a", "b", [2]float64{0, 0}, [2]float64{100, 0})
	for _, tc := range []struct {
		name string
		b    planEdge
		want bool
	}{
		{"collinear", crowdingTestEdge(7, "c", "d", [2]float64{20, 0}, [2]float64{80, 0}), true},
		{"parallel-close", crowdingTestEdge(7, "c", "d", [2]float64{20, 10}, [2]float64{80, 10}), true},
		{"parallel-clear", crowdingTestEdge(7, "c", "d", [2]float64{20, 12}, [2]float64{80, 12}), false},
		{"crossing", crowdingTestEdge(7, "c", "d", [2]float64{50, -30}, [2]float64{50, 30}), true},
		{"shared-local", crowdingTestEdge(7, "a", "c", [2]float64{0, 0}, [2]float64{12, 0}, [2]float64{12, 50}), false},
		{"shared-long", crowdingTestEdge(7, "a", "c", [2]float64{0, 0}, [2]float64{70, 0}, [2]float64{70, 50}), true},
		{"shared-remote-crossing", crowdingTestEdge(7, "a", "c", [2]float64{0, 0}, [2]float64{0, -50}, [2]float64{70, -50}, [2]float64{70, 50}), true},
		{"diagonal-clear", crowdingTestEdge(7, "c", "d", [2]float64{120, 20}, [2]float64{20, 120}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := observePlanRouteCrowding(nil, []planEdge{a, tc.b}, 28.127, 28.234)
			if r.Status != "complete" || (len(r.Conflicts) > 0) != tc.want {
				t.Fatalf("unexpected observation: %+v", r)
			}
			for _, c := range r.Conflicts {
				if c.ConnectorIndex != 2 || c.PeerIndex != 7 || c.Kind != "stroke-stroke" || c.Area <= 0 {
					t.Fatalf("lost connector identity or positive witness: %+v", c)
				}
			}
		})
	}
	// Separate semantic edges are not a junction, even across a short gap.
	b := crowdingTestEdge(7, "a", "b", [2]float64{0, 0}, [2]float64{10, 0})
	a.pts = b.pts
	if len(observePlanRouteCrowding(nil, []planEdge{a, b}, 0, 0).Conflicts) == 0 {
		t.Fatal("duplicate short edges vanished inside two endpoint exemptions")
	}
}

func TestPlanRouteCrowdingIndependentAreaAndSubdivision(t *testing.T) {
	a := crowdingTestEdge(0, "a", "b", [2]float64{0, 0}, [2]float64{100, 0})
	b := crowdingTestEdge(1, "c", "d", [2]float64{20, 5}, [2]float64{80, 5})
	r := observePlanRouteCrowding(nil, []planEdge{a, b}, 0, 0)
	// Default 5.4-wide casing plus half the 6-unit gap per route produces
	// rectangles [-5.7,105.7]×[-5.7,5.7] and [14.3,85.7]×[-.7,10.7].
	if len(r.Conflicts) != 1 || math.Abs(r.Conflicts[0].Area-71.4*6.4) > 1e-8 {
		t.Fatalf("wrong analytic rectangle intersection: %+v", r)
	}
	a.pts = [][2]float64{{0, 0}, {30, 0}, {30, 0}, {70, 0}, {100, 0}}
	q := observePlanRouteCrowding(nil, []planEdge{a, b}, 0, 0)
	if !reflect.DeepEqual(q, r) {
		t.Fatal("collinear subdivisions changed conflict identity or severity")
	}
	q = observePlanRouteCrowding(nil, []planEdge{a, b}, 1e6, -1e6)
	if len(q.Conflicts) != 1 || math.Abs(q.Conflicts[0].Area-r.Conflicts[0].Area) > .0001 {
		t.Fatal("translation changed conflict area")
	}
}

func TestPlanRouteCrowdingOpaqueMaskAndMarker(t *testing.T) {
	a := crowdingTestEdge(0, "a", "b", [2]float64{0, 0}, [2]float64{100, 0})
	b := crowdingTestEdge(1, "c", "d", [2]float64{20, 0}, [2]float64{80, 0})
	r := planRect{id: "cover", x: -30, y: -30, w: 160, d: 60, fill: "#ffffff"}
	if len(observePlanRouteCrowding([]planRect{r}, []planEdge{a, b}, 0, 0).Conflicts) != 0 {
		t.Fatal("fully hidden route overlap was reported")
	}
	for _, fill := range []string{"none", "rgba(255,255,255,.5)", "var(--unknown)"} {
		r.fill = fill
		if len(observePlanRouteCrowding([]planRect{r}, []planEdge{a, b}, 0, 0).Conflicts) == 0 {
			t.Fatal("unproven opacity suppressed visible overlap")
		}
	}
	b = crowdingTestEdge(1, "c", "d", [2]float64{70, 15}, [2]float64{100, 15})
	if len(observePlanRouteCrowding(nil, []planEdge{a, b}, 0, 0).Conflicts) != 0 {
		t.Fatal("fixture already has a stroke collision")
	}
	a.c.Arrow = "triangle"
	rpt := observePlanRouteCrowding(nil, []planEdge{a, b}, 0, 0)
	if len(rpt.Conflicts) != 1 || rpt.Conflicts[0].Kind != "arrow-stroke" {
		t.Fatalf("missed marker-only obstruction: %+v", rpt)
	}
}

func TestPlanRouteCrowdingLimitsAreExplicit(t *testing.T) {
	a := crowdingTestEdge(0, "a", "b", [2]float64{0, 0}, [2]float64{100, 0})
	b := crowdingTestEdge(1, "c", "d", [2]float64{20, 0}, [2]float64{80, 0})
	for _, kind := range []string{"segments", "nan", "overflow", "pairs"} {
		x := a
		es := []planEdge{x, b}
		switch kind {
		case "segments":
			es[0].pts = make([][2]float64, 513)
		case "nan":
			es[0].pts = [][2]float64{{math.NaN(), 0}, {100, 0}}
		case "overflow":
			es[0].pts = [][2]float64{{-1e308, 0}, {1e308, 0}}
			es[0].c = &Connector{Arrow: "triangle"}
		case "pairs":
			es = nil
			for i := 0; i < 726; i++ {
				x.ci = i
				x.pts = [][2]float64{{float64(i) * 200, 0}, {float64(i)*200 + 100, 0}}
				es = append(es, x)
			}
		}
		if c := observePlanRouteCrowding(nil, es, 0, 0); c.Status != "unresolved" || c.Reason == "" {
			t.Fatalf("%s claimed a complete observation", kind)
		}
	}
}

func planUnlabelledCrowdingDoc() *Document {
	d := planLaneTestDoc(false)
	d.Scene().Parts = d.Scene().Parts[1:]
	return d
}

func TestPlanRouteCrowdingRepairsWithoutTitles(t *testing.T) {
	d := planUnlabelledCrowdingDoc()
	d.Canvas.Padding = 1
	input, _ := json.Marshal(d)
	r, _, old := buildPlanModelBase(d.Scene(), d.Theme, d.Canvas, true)
	ox, oy, _, _ := planFrame(r, old, d.Canvas)
	if c := observePlanRouteCrowding(r, old, ox, oy); len(c.Conflicts) == 0 {
		t.Fatal("fixture lacks route crowding")
	}
	p := resolvePlanScene(d.Scene(), d.Theme, d.Canvas, true, true)
	if p.crowding.Status != "complete" || len(p.crowding.Conflicts) != 0 {
		t.Fatalf("unlabelled hub remains crowded: %+v", p.crowding)
	}
	moved := false
	for i, e := range p.edges {
		if !slices.Equal(e.pts, old[i].pts) {
			moved = true
			if !planRouteSurfaceClear(e, r, p.edges, ox, oy) || !planRouteLaneBordersClear(e, r, ox, oy) {
				t.Fatal("new route has an unsafe envelope")
			}
		}
	}
	if !moved || len(p.layoutContent().texts) != len(r) {
		t.Fatal("route was not repaired through the final content resolver")
	}
	scene, report := RenderSceneWithReport(d)
	if scene.PlanRouteCrowding != report.PlanRouteCrowding || !reflect.DeepEqual(scene.PlanRouteCrowding, EvaluatePlan(d.Scene(), d.Theme, d.Canvas).RouteCrowding) || RenderScene(d).SVG != scene.SVG || RenderScene(d).PlanRouteCrowding != nil {
		t.Fatal("report/evaluator do not describe the final submitted preview")
	}
	var root struct {
		ViewBox string `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal([]byte(scene.SVG), &root); err != nil {
		t.Fatal(err)
	}
	x, y, w, h, err := parseViewBox(root.ViewBox)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range p.edges {
		if e.refined {
			for _, v := range planRouteScreenPoints(e, ox, oy) {
				margin := 16 + planRouteWidth(e.c)/2
				if v[0]-margin < x-.01 || v[1]-margin < y-.01 || v[0]+margin > x+w+.01 || v[1]+margin > y+h+.01 {
					t.Fatal("unlabelled detour clipped by export")
				}
			}
		}
	}
	after, _ := json.Marshal(d)
	if string(after) != string(input) {
		t.Fatal("automatic separation mutated input")
	}
}

func TestPlanRouteCrowdingPreservesAuthoredPathsAndUnknownText(t *testing.T) {
	for _, mode := range []string{"straight", "fixed", "waypoint", "unknown"} {
		d := planUnlabelledCrowdingDoc()
		for _, c := range d.Scene().Connectors {
			switch mode {
			case "straight":
				c.Routing = "straight"
			case "fixed":
				c.From += ".port-right"
				c.To += ".port-left"
			case "waypoint":
				c.Waypoints = []WorldPoint{{WX: 100, WY: 25}}
			case "unknown":
				d.Scene().Parts[0].Label = "unmeasured"
			}
		}
		r, _, old := buildPlanModelBase(d.Scene(), d.Theme, d.Canvas, true)
		ox, oy, _, _ := planFrame(r, old, d.Canvas)
		got, _, observation := refinePlanRouteCrowding(r, old, nil, ox, oy)
		if !reflect.DeepEqual(got, old) || observation.Status != "complete" {
			t.Fatalf("%s changed protected input or lost observation", mode)
		}
	}
}

func TestPlanRouteCrowdingDoesNotTransferDefects(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 59117))
	moved := 0
	for trial := 0; trial < 120; trial++ {
		d := planUnlabelledCrowdingDoc()
		for _, p := range d.Scene().Parts {
			p.Geom.W, p.Geom.D = 30+rng.Float64()*60, 30+rng.Float64()*60
			p.Offset.WX += rng.Float64()*12 - 6
			p.Offset.WY += rng.Float64()*12 - 6
		}
		for _, c := range d.Scene().Connectors {
			w := 1 + rng.Float64()*7
			c.Stroke = &Stroke{Width: &w}
			if trial%2 == 0 {
				c.Arrow = "triangle"
			}
			if trial%3 == 0 {
				c.Stroke.Dash = "dashed"
			}
			if trial%4 == 0 {
				c.Label, c.LabelFontFamily = "request", "Source Sans Pro"
			}
		}
		r, _, old := buildPlanModelBase(d.Scene(), d.Theme, d.Canvas, true)
		ox, oy, _, _ := planFrame(r, old, d.Canvas)
		before := resolvePlanContent(r, old, ox, oy, true)
		oldCrowding := observePlanRouteCrowding(r, old, ox, oy)
		got, content, crowding := refinePlanRouteCrowding(r, old, &before, ox, oy)
		bd, _ := planRouteContentDefects(before)
		ad, _ := planRouteContentDefects(*content)
		if !ad.noWorseThan(bd) || !planCrowdingDefects(crowding).noWorseThan(planCrowdingDefects(oldCrowding)) {
			t.Fatalf("trial %d transferred a defect", trial)
		}
		for i, e := range got {
			if !slices.Equal(e.pts, old[i].pts) {
				moved++
				if !planRouteSurfaceClear(e, r, got, ox, oy) || !planRouteLaneBordersClear(e, r, ox, oy) {
					t.Fatalf("trial %d introduced an unsafe route", trial)
				}
			}
		}
		// No document-keyed cache: edit and a fresh JSON decode must agree.
		data, _ := json.Marshal(d)
		var fresh Document
		if err := json.Unmarshal(data, &fresh); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(RenderSceneWithInteraction(d), RenderSceneWithInteraction(&fresh)) {
			t.Fatal("stale or non-deterministic scene")
		}
	}
	if moved < 20 {
		t.Fatalf("insufficient real improvements: %d", moved)
	}
}
