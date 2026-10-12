package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func planLaneTestDoc(arrow bool) *Document {
	d := planRouteContentTestDoc("tight")
	n := d.Scene()
	for _, p := range n.Parts[1:] {
		p.Geom.D = 30
	}
	y := 100.0
	if arrow {
		y = -120
		n.Connectors[0].Arrow = "triangle"
	}
	n.Parts = append(n.Parts, &CompositePart{ID: "peer", Shape: "rectangle", Geom: &Geom{W: 30, D: 30, H: 20}, Offset: &WorldPoint{WX: 300, WY: y}})
	n.Connectors = append(n.Connectors, &Connector{From: "from", To: "peer", Routing: "orthogonal", Arrow: n.Connectors[0].Arrow})
	return d
}

func TestPlanRouteLanesClearSharedSource(t *testing.T) {
	for _, arrow := range []bool{false, true} {
		for _, weight := range []string{"400", "600", "700"} {
			t.Run(fmt.Sprint(arrow, weight), func(t *testing.T) {
				d := planLaneTestDoc(arrow)
				d.Scene().Parts[0].Style.Text.Weight = weight
				input, _ := json.Marshal(d)
				r, _, old := buildPlanModelBase(d.Scene(), d.Theme, d.Canvas, true)
				ox, oy, _, _ := planFrame(r, old, d.Canvas)
				before := resolvePlanContent(r, old, ox, oy, true)
				if planRouteTitleContacts(before, 0) == 0 || planRouteTitleContacts(before, 1) == 0 {
					t.Fatal("fixture must obstruct the title with both edges")
				}
				_, _, got := buildPlanModel(d.Scene(), d.Theme, d.Canvas)
				after := resolvePlanContent(r, got, ox, oy, true)
				if after.texts[0].record.Placement.Status != "clear-within-scope" {
					t.Fatal("shared-source title remains blocked")
				}
				for i, e := range got {
					if slices.Equal(e.pts, old[i].pts) || !planAttachmentsValid(e.pts, e.from, e.to) || !planRouteSurfaceClear(e, r, got, ox, oy) || !planRouteLaneBordersClear(e, r, ox, oy) {
						t.Fatal("fan-out did not submit two independently clear attachments")
					}
				}
				bd, _ := planRouteContentDefects(before)
				ad, _ := planRouteContentDefects(after)
				if !ad.noWorseThan(bd) {
					t.Fatal("fan-out transferred a text defect")
				}
				scene, report := RenderSceneWithReport(d)
				if !reflect.DeepEqual(scene.PlanLabels, report.PlanLabels) || RenderSceneWithInteraction(d).SVG != scene.SVG {
					t.Fatal("preview/report disagree with the final layout")
				}
				for _, e := range got {
					path := planPathStr(e.pts, func(x float64) float64 { return x + ox }, func(y float64) float64 { return y + oy })
					if !strings.Contains(scene.SVG, `d="`+path+`"`) {
						t.Fatal("route certificate does not describe the emitted path")
					}
				}
				if score := EvaluatePlan(d.Scene(), d.Theme, d.Canvas); score.EdgesThroughNodes != 0 || score.Crossings != 0 {
					t.Fatal("route repair introduced a topology defect")
				}
				output, _ := json.Marshal(d)
				if string(input) != string(output) {
					t.Fatal("fan-out changed author input")
				}
			})
		}
	}
}

func TestPlanRouteLanesRespectConstraints(t *testing.T) {
	for _, kind := range []string{"fixed", "straight", "elbow", "waypoint", "legacy-anchor", "unknown-title", "unknown-pill", "exhausted"} {
		t.Run(kind, func(t *testing.T) {
			d := planLaneTestDoc(false)
			budget := 32
			for _, c := range d.Scene().Connectors {
				switch kind {
				case "fixed":
					c.From += ".port-right"
					c.To += ".port-left"
				case "straight":
					c.Routing = "straight"
				case "elbow":
					c.Elbow = "xFirst"
				case "waypoint":
					c.Waypoints = []WorldPoint{{WX: 10, WY: 20}}
				case "legacy-anchor":
					c.From += ".right"
				case "unknown-title":
					d.Scene().Parts[1].Label = "unmeasured"
				case "unknown-pill":
					c.Label = "request"
				case "exhausted":
					budget = 0
				}
			}
			r, _, old := buildPlanModelBase(d.Scene(), d.Theme, d.Canvas, true)
			got := refinePlanRoutesWithBudget(r, old, d.Canvas, budget)
			if !reflect.DeepEqual(got, old) {
				t.Fatal("changed a protected or uncertifiable hub")
			}
		})
	}
}

func TestPlanRouteLanesKeepResidualVisible(t *testing.T) {
	d := planRouteContentTestDoc("vertical")
	n := d.Scene()
	for _, p := range n.Parts[1:] {
		p.Geom.W, p.Geom.D = 50, 50
	}
	n.Parts = append(n.Parts,
		&CompositePart{ID: "peer-source", Shape: "rectangle", Geom: &Geom{W: 30, D: 30, H: 20}, Offset: &WorldPoint{WX: 200, WY: -60}},
		&CompositePart{ID: "peer", Shape: "rectangle", Geom: &Geom{W: 30, D: 30, H: 20}, Offset: &WorldPoint{WX: 200, WY: 200}})
	n.Connectors[0].Arrow = "triangle"
	n.Connectors = append(n.Connectors, &Connector{From: "peer-source", To: "peer", Arrow: "triangle", Routing: "orthogonal"})
	r, _, old := buildPlanModelBase(n, d.Theme, d.Canvas, true)
	ox, oy, _, _ := planFrame(r, old, d.Canvas)
	before := resolvePlanContent(r, old, ox, oy, true)
	got := refinePlanRoutes(r, old, d.Canvas)
	after := resolvePlanContent(r, got, ox, oy, true)
	bd, bc := planRouteContentDefects(before)
	ad, ac := planRouteContentDefects(after)
	if bc != 1 || ac != 1 || !ad.noWorseThan(bd) || planRouteTitleContacts(before, 0) == 0 || planRouteTitleContacts(after, 0) != 0 || planRouteTitleContacts(after, 1) == 0 {
		t.Fatal("did not eliminate one counterpart while retaining the unresolved peer")
	}
	scene, report := RenderSceneWithReport(d)
	if scene.PlanLabels[0].Placement.Status != "blocked" || !reflect.DeepEqual(scene.PlanLabels, report.PlanLabels) {
		t.Fatal("partial progress incorrectly certified the title as clear")
	}
}

func TestPlanRouteLaneBorderClearance(t *testing.T) {
	r := []planRect{{id: "group", container: true, x: 0, y: 0, w: 270, d: 160}}
	for _, tc := range []struct {
		name string
		pts  [][2]float64
		want bool
	}{
		{"parallel-graze", [][2]float64{{-40, 10}, {-40, -2}, {310, -2}, {310, 10}}, false},
		{"parallel-gap", [][2]float64{{-40, 10}, {-40, -12}, {310, -12}, {310, 10}}, true},
		{"crossing", [][2]float64{{-40, 100}, {310, 100}}, true},
	} {
		e := planEdge{c: &Connector{Arrow: "none"}, pts: tc.pts}
		if planRouteLaneBordersClear(e, r, 28.127, 28.234) != tc.want {
			t.Fatalf("%s: enclosure clearance policy incorrect", tc.name)
		}
	}
}

func TestPlanRouteLanesDoNotTransferDefects(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 29107))
	moves := 0
	for trial := 0; trial < 96; trial++ {
		d := planLaneTestDoc(trial%2 == 0)
		for _, p := range d.Scene().Parts[1:] {
			p.Offset.WX += rng.Float64()*6 - 3
			p.Offset.WY += rng.Float64()*6 - 3
			p.Geom.W += rng.Float64() * 20
			p.Geom.D += rng.Float64() * 20
		}
		for i, c := range d.Scene().Connectors {
			w := 1 + rng.Float64()*5
			c.Stroke = &Stroke{Width: &w}
			if trial%3 == 0 {
				c.Label, c.LabelFontFamily = fmt.Sprint("edge ", i), "Source Sans Pro"
			}
			if trial%5 == 0 {
				c.Stroke.Dash = "dashed"
			}
		}
		r, _, old := buildPlanModelBase(d.Scene(), d.Theme, d.Canvas, true)
		ox, oy, _, _ := planFrame(r, old, d.Canvas)
		before := resolvePlanContent(r, old, ox, oy, true)
		got := refinePlanRoutes(r, old, d.Canvas)
		after := resolvePlanContent(r, got, ox, oy, true)
		bd, bc := planRouteContentDefects(before)
		ad, ac := planRouteContentDefects(after)
		var leaves []planRect
		for _, r := range r {
			if !r.container {
				leaves = append(leaves, r)
			}
		}
		evalGeomWithDefects(leaves, old, bd)
		evalGeomWithDefects(leaves, got, ad)
		if ac > bc || !ad.noWorseThan(bd) {
			t.Fatalf("trial %d transferred or deepened a defect", trial)
		}
		for i, e := range got {
			if slices.Equal(e.pts, old[i].pts) {
				continue
			}
			moves++
			if !planRouteSurfaceClear(e, r, got, ox, oy) {
				t.Fatalf("trial %d has unsafe route %d", trial, i)
			}
			// An independent L-infinity segment-distance check includes every
			// pair, including edges with the same source, full casing and gap.
			for j, peer := range got {
				if i == j {
					continue
				}
				for a := 1; a < len(e.pts); a++ {
					for b := 1; b < len(peer.pts); b++ {
						x, y, p, q := e.pts[a-1], e.pts[a], peer.pts[b-1], peer.pts[b]
						dx := math.Max(math.Min(x[0], y[0])-math.Max(p[0], q[0]), math.Min(p[0], q[0])-math.Max(x[0], y[0]))
						dy := math.Max(math.Min(x[1], y[1])-math.Max(p[1], q[1]), math.Min(p[1], q[1])-math.Max(x[1], y[1]))
						gap := (planRouteWidth(e.c)+planRouteWidth(peer.c))/2 + 6
						if math.Max(dx, dy) < gap-.02 {
							t.Fatalf("trial %d route %d/%d violates independent clearance", trial, i, j)
						}
					}
				}
			}
		}
	}
	if moves < 20 {
		t.Fatalf("insufficient accepted arrangements: %d", moves)
	}
}
