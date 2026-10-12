package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

func connectorJointEmptyBody() (connectorTextBodies, bool) { return connectorTextBodies{}, true }

func TestConnectorTextJointReleasesSharedCorridor(t *testing.T) {
	var requests []connectorTextRequest
	var obstacles []connectorTextObstacle
	var strokes []connectorTextStroke
	for i, y := range []float64{0, 20, 40, 200} {
		points := [][2]float64{{-100, y}, {100, y}}
		requests = append(requests, connectorTextRequest{index: i, points: points, block: screenTextBlock{width: 160, height: 24}})
		obstacles = append(obstacles, connectorTextStrokeObstacles(i, points, false, 2)...)
		strokes = append(strokes, connectorTextStrokes(i, points, false, 2)...)
	}
	before := resolveConnectorText(requests, nil, nil, obstacles, strokes)
	if before[0].clear || before[1].clear || before[2].clear || !before[3].clear {
		t.Fatalf("fixture lost its three-way dependency: %v", before)
	}
	for repeat := 0; repeat < 2; repeat++ {
		after := append([]connectorTextPlacement(nil), before...)
		refineConnectorTextJoint(after, requests, nil, nil, obstacles, strokes, connectorJointEmptyBody)
		if after[3] != before[3] {
			t.Fatal("moved a clear label")
		}
		for i, p := range after {
			if !p.clear {
				t.Fatal("cooperative packing still blocked")
			}
			if p.x < -100 || p.x > 100 || p.y != requests[i].points[0][1] {
				t.Fatal("label left its authored route")
			}
			for j, q := range after {
				if i != j && collides(p.box, []screenRect{q.box}) {
					t.Fatal("joint solution overlaps a peer")
				}
			}
		}
		settled := append([]connectorTextPlacement(nil), after...)
		refineConnectorTextJoint(after, requests, nil, nil, obstacles, strokes, connectorJointEmptyBody)
		if !reflect.DeepEqual(settled, after) {
			t.Fatal("accepted solution was not stable")
		}
	}
}

func TestConnectorTextJointFiniteDomainOracle(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 1579))
	// Enumerate every combination independently, including retained blocked
	// pills. Two untouched pills may retain an old conflict; a moved one must
	// clear all peers. Check both maximum clear count and minimum displacement.
	for trial := 0; trial < 180; trial++ {
		n := 2 + trial%4
		old := make([]connectorTextPlacement, n)
		domains := make([][]connectorTextJointChoice, n)
		component := make([]int, n)
		for i := range old {
			component[i] = i
			x, y := rng.Float64()*100, rng.Float64()*70
			old[i] = connectorTextPlacement{x: x, y: y, box: screenRect{x - 20, y - 10, x + 20, y + 10}}
			for j := 0; j < 3; j++ {
				u, v := rng.Float64()*200, rng.Float64()*120
				p := connectorTextPlacement{u, v, true, screenRect{u - 20, v - 10, u + 20, v + 10}}
				domains[i] = append(domains[i], connectorTextJointChoice{p, math.Hypot(u-x, v-y)})
			}
		}
		bestCount, bestDistance := 0, 0.
		choices := make([]int, n)
		combinations := 1 << (2 * n)
		for bits := 0; bits < combinations; bits++ {
			count, distance, valid := 0, 0., true
			for i := range choices {
				choices[i] = (bits >> (2 * i)) & 3
				if choices[i] < 3 {
					count++
					distance += domains[i][choices[i]].distance
				}
			}
			for i := 0; i < n && valid; i++ {
				a := old[i].box
				if choices[i] < 3 {
					a = domains[i][choices[i]].placement.box
				}
				for j := 0; j < i; j++ {
					b := old[j].box
					if choices[j] < 3 {
						b = domains[j][choices[j]].placement.box
					}
					if (choices[i] < 3 || choices[j] < 3) && a.x0 < b.x1+6 && a.x1 > b.x0-6 && a.y0 < b.y1+6 && a.y1 > b.y0-6 {
						valid = false
					}
				}
			}
			if valid && (count > bestCount || count == bestCount && distance < bestDistance) {
				bestCount, bestDistance = count, distance
			}
		}
		next := append([]connectorTextPlacement(nil), old...)
		budget := 1000000
		solveConnectorTextJointComponent(next, domains, component, &budget)
		count, distance := 0, 0.
		for i, p := range next {
			if p.clear {
				count++
				distance += math.Hypot(p.x-old[i].x, p.y-old[i].y)
			} else if p != old[i] {
				t.Fatal("moved an unresolved label")
			}
		}
		if budget <= 0 || count != bestCount || math.Abs(distance-bestDistance) > 1e-8 {
			t.Fatalf("case %d got %d/%g want %d/%g budget %d", trial, count, distance, bestCount, bestDistance, budget)
		}
	}
}

func TestConnectorTextJointWholeArrangement(t *testing.T) {
	rng := rand.New(rand.NewPCG(479, 20261012))
	improved := 0
	for trial := 0; trial < 300; trial++ {
		var requests []connectorTextRequest
		var obstacles []connectorTextObstacle
		var strokes []connectorTextStroke
		bodies := []screenRect{{120, 130, 170, 150}}
		for i := 0; i < 6; i++ {
			points := [][2]float64{{rng.Float64()*400 - 200, rng.Float64() * 200}, {rng.Float64()*400 - 200, rng.Float64() * 200}}
			curved := (trial+i)%3 == 0
			if curved {
				points = append(points[:1], [2]float64{0, 250}, points[1])
			}
			requests = append(requests, connectorTextRequest{index: i, points: points, curved: curved, block: screenTextBlock{width: 60 + rng.Float64()*80, height: 18 + rng.Float64()*20}})
			obstacles = append(obstacles, connectorTextStrokeObstacles(i, points, curved, 2)...)
			strokes = append(strokes, connectorTextStrokes(i, points, curved, 2)...)
		}
		old := resolveConnectorText(requests, bodies, nil, obstacles, strokes)
		next := append([]connectorTextPlacement(nil), old...)
		refineConnectorTextJoint(next, requests, bodies, nil, obstacles, strokes, connectorJointEmptyBody)
		again := append([]connectorTextPlacement(nil), old...)
		refineConnectorTextJoint(again, requests, bodies, nil, obstacles, strokes, connectorJointEmptyBody)
		if !reflect.DeepEqual(next, again) {
			t.Fatal("nondeterministic search")
		}
		for i, p := range next {
			if old[i].clear || !p.clear {
				if p != old[i] {
					t.Fatal("moved a clear or unresolved pill")
				}
				continue
			}
			improved++
			if collides(p.box, bodies) {
				t.Fatal("candidate intersects a fixed body")
			}
			for j, peer := range next {
				if j != i && collides(p.box, []screenRect{peer.box}) {
					t.Fatal("candidate borrows another pill's final slot")
				}
			}
			for _, stroke := range strokes {
				if stroke.owner != i && connectorTextStrokeHits(p.box, []connectorTextStroke{stroke}) {
					t.Fatal("candidate covers another submitted route")
				}
			}
		}
	}
	if improved == 0 {
		t.Fatal("random corpus never exercised joint improvement")
	}
	t.Logf("joint search improved %d labels in 300 arrangements", improved)
}

func TestConnectorTextJointRealScenesAndFallback(t *testing.T) {
	for _, projection := range []string{"iso", "top"} {
		for _, unsupported := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unknown=%v", projection, unsupported), func(t *testing.T) {
				doc := connectorTextJointScene(projection, 25, 400, 14, false, 3)
				if unsupported {
					doc.Scene().Parts[0].Label = "Unmeasured"
				}
				saved, _ := json.Marshal(doc)
				scene, report := RenderSceneWithReport(doc)
				if len(scene.ConnectorLabels) != 3 || !reflect.DeepEqual(scene.ConnectorLabels, report.ConnectorLabels) || scene.SVG != RenderScene(doc).SVG {
					t.Fatal("render/report/preview diverged")
				}
				for _, record := range scene.ConnectorLabels {
					if (record.Label.Typography.Placement == "blocked") != unsupported {
						t.Fatal("joint placement or capability fallback lost")
					}
				}
				now, _ := json.Marshal(doc)
				if string(saved) != string(now) {
					t.Fatal("joint search changed authored data")
				}
			})
		}
	}
}

func TestConnectorTextJointBudgetAndContent(t *testing.T) {
	req := connectorTextRequest{points: [][2]float64{{0, 0}, {400, 0}}, block: screenTextBlock{width: 80, height: 20}}
	old := connectorTextPlacement{200, 0, false, connectorTextBox(req, 200, 0)}
	if got := connectorTextJointCandidates(req, old, nil, nil, &connectorTextJointBudget{}); len(got) != 0 {
		t.Fatal("exhausted budget generated candidates")
	}
	for _, supported := range []bool{false, true} {
		positions := []connectorTextPlacement{old, old}
		requests := []connectorTextRequest{req, req}
		body := connectorBodyTestRegion([][2]float64{{-200, -200}, {600, -200}, {600, 200}, {-200, 200}})
		calls := 0
		source := func() (connectorTextBodies, bool) { calls++; return body, supported }
		refineConnectorTextJoint(positions, requests, nil, nil, nil, nil, source)
		if calls != 1 || positions[0] != old || positions[1] != old {
			t.Fatal("unknown or filled content ignored")
		}
	}
	// Exhaustion can retain a verified partial assignment, but never an
	// unverified search prefix. Run every early stop in a dependent component.
	domains := [][]connectorTextJointChoice{{{connectorTextPlacement{0, 0, true, screenRect{-40, -10, 40, 10}}, 200}}, {{connectorTextPlacement{400, 0, true, screenRect{360, -10, 440, 10}}, 200}}}
	for limit := 0; limit < 30; limit++ {
		positions := []connectorTextPlacement{old, old}
		budget := limit
		solveConnectorTextJointComponent(positions, domains, []int{0, 1}, &budget)
		if (positions[0].clear || positions[1].clear) && collides(positions[0].box, []screenRect{positions[1].box}) {
			t.Fatal("exhausted search submitted an incomplete assignment")
		}
	}
}

func TestConnectorTextJointBodyBoundarySlots(t *testing.T) {
	// A body outline extends a little past the older AABB. Simply filtering
	// its interval endpoints loses these feasible cooperative assignments;
	// re-solving with submitted borders recovers the safe nearby boundaries.
	for _, c := range []struct {
		gap, length, size float64
		multiline         bool
	}{{25, 400, 18, false}, {25, 500, 22, false}, {35, 400, 18, true}, {35, 500, 22, false}, {35, 500, 22, true}, {50, 500, 22, true}} {
		t.Run(fmt.Sprintf("%g/%g/%g/%v", c.gap, c.length, c.size, c.multiline), func(t *testing.T) {
			scene := RenderScene(connectorTextJointScene("iso", c.gap, c.length, c.size, c.multiline, 3))
			if len(scene.ConnectorLabels) != 3 {
				t.Fatal("missing cooperative labels")
			}
			for _, record := range scene.ConnectorLabels {
				if record.Label.Typography.Placement != "clear-within-scope" {
					t.Fatal("submitted body boundary slot was lost")
				}
			}
		})
	}
}

func connectorTextJointScene(projection string, gap, length, size float64, multiline bool, count int) *Document {
	n := &Node{Shape: "composite"}
	doc := &Document{Nodes: map[string]*Node{"scene": n}, Canvas: &Canvas{Projection: projection, AspectRatio: -1}}
	for i := 0; i < count; i++ {
		a, b := &WorldPoint{WY: float64(i) * gap}, &WorldPoint{WX: length, WY: float64(i) * gap}
		geom := &Geom{W: 40, D: 8, H: 6}
		if projection == "iso" {
			a = &WorldPoint{WX: float64(i) * gap, WY: float64(i) * gap}
			b = &WorldPoint{WX: length/1.7320508075688772 + float64(i)*gap, WY: -length/1.7320508075688772 + float64(i)*gap}
			geom = &Geom{W: 16, D: 8, H: 6}
		}
		from, to := fmt.Sprintf("source-%d", i), fmt.Sprintf("target-%d", i)
		n.Parts = append(n.Parts, &CompositePart{ID: from, Shape: "rectangle", Geom: geom, Offset: a}, &CompositePart{ID: to, Shape: "rectangle", Geom: geom, Offset: b})
		label := "REQUEST RESPONSE"
		if multiline {
			label = "REQUEST RESPONSE\nAV office"
		}
		n.Connectors = append(n.Connectors, &Connector{From: from, To: to, Routing: "straight", Arrow: "triangle", Label: label, LabelFontFamily: "Source Sans Pro", LabelFontSize: size})
	}
	return doc
}
