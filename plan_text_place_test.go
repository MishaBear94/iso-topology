package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
)

func planPlacementTestDoc() *Document {
	doc := planTextTestDoc("Warehouse orchestration", 270, 160, 20)
	p := doc.Scene().Parts[0]
	p.ID = "header"
	p.Shape = "group"
	p.Parts = []*CompositePart{{ID: "worker", Shape: "rectangle", Geom: &Geom{W: 180, D: 50, H: 20}, Offset: &WorldPoint{WX: 30, WY: 85}}}
	doc.Scene().Parts = append(doc.Scene().Parts,
		&CompositePart{ID: "from", Shape: "rectangle", Geom: &Geom{W: 30, D: 20, H: 20}, Offset: &WorldPoint{WX: -60, WY: 10}},
		&CompositePart{ID: "to", Shape: "rectangle", Geom: &Geom{W: 30, D: 20, H: 20}, Offset: &WorldPoint{WX: 300, WY: 10}})
	doc.Scene().Connectors = []*Connector{{From: "from", To: "to", Routing: "orthogonal", Arrow: "none"}}
	return doc
}

func TestPlanTitlePlacementClearsRouteWithoutChangingGeometry(t *testing.T) {
	doc := planPlacementTestDoc()
	source, _ := json.Marshal(doc)
	rects, _, edges := buildPlanModel(doc.Scene(), doc.Theme, doc.Canvas)
	ox, oy, _, _ := planFrame(rects, edges, doc.Canvas)
	initial := layoutPlanLabels(rects, ox, oy)[0].record
	scene, report := RenderSceneWithReport(doc)
	p := scene.PlanLabels[0]
	if p.Placement == nil || p.Placement.Status != "clear-within-scope" || p.Placement.Offset == [2]float64{} {
		t.Fatalf("crossed title was not repaired: %+v", p.Placement)
	}
	if p.Text != initial.Text || p.FontSize != initial.FontSize || p.Budget != initial.Budget || len(p.Lines) != len(initial.Lines) || p.Status != "within-budget" {
		t.Fatal("movement changed typography or owner budget")
	}
	for i, l := range p.Lines {
		if l.Text != initial.Lines[i].Text || math.Abs(l.X-initial.Lines[i].X-p.Placement.Offset[0]) > .000001 || math.Abs(l.Baseline-initial.Lines[i].Baseline-p.Placement.Offset[1]) > .000001 {
			t.Fatal("record not a rigid translation")
		}
	}
	for _, edge := range edges {
		path := planPathStr(edge.pts, func(x float64) float64 { return x + ox }, func(y float64) float64 { return y + oy })
		if !strings.Contains(scene.SVG, `d="`+path+`"`) {
			t.Fatal("title repair rerouted an edge")
		}
		// This fixture's entire route is horizontal. Check actual submitted
		// glyph boxes against its full casing and a six-unit reading gap.
		y := edge.pts[0][1] + oy
		for _, l := range p.Lines {
			if b := l.InkBounds; b != nil && b.Y < y+2.7+6 && b.Y+b.Height > y-2.7-6 {
				t.Fatal("moved title still intersects route casing")
			}
		}
	}
	if !reflect.DeepEqual(scene.PlanLabels, report.PlanLabels) {
		t.Fatal("report differs from submission")
	}
	model := BuildInteractionModel(doc)
	found := false
	for _, m := range model {
		if m.ID != "header" {
			continue
		}
		found = true
		for i, l := range p.Lines {
			b := l.InkBounds
			if b == nil {
				continue
			}
			r := m.SelectionRegions[i+1]
			if math.Abs(r[0][0]-(b.X-ox-.01)) > 1e-8 || math.Abs(r[0][1]-(b.Y-oy-.01)) > 1e-8 {
				t.Fatal("selection still uses pre-repair title")
			}
		}
	}
	if !found {
		t.Fatal("missing interaction owner")
	}
	after, _ := json.Marshal(doc)
	if string(source) != string(after) {
		t.Fatal("repair mutated source")
	}
	if RenderScene(doc).SVG != scene.SVG {
		t.Fatal("diagnostic collection changed output")
	}
}

func TestPlanTitlePlacementRespectsPainterOpacity(t *testing.T) {
	for _, fill := range []string{"#ffffff", "rgba(255,255,255,.4)", "none", "var(--unknown)"} {
		doc := planTextTestDoc("AV office", 160, 100, 18)
		doc.Scene().Parts[0].Style.Palette = &Palette{Top: fill}
		rects, _, _ := buildPlanModel(doc.Scene(), doc.Theme, doc.Canvas)
		texts := layoutPlanLabels(rects, 0, 0)
		before := texts[0].record.Lines[0]
		y := before.InkBounds.Y + before.InkBounds.Height/2
		stroke := connectorTextStroke{owner: 0, a: [2]float64{-100, y}, b: [2]float64{300, y}, radius: 2.7}
		obs := []planTitleObstacle{{kind: "route", index: 0, stroke: &stroke}}
		moved := refinePlanTitles(rects, texts, obs, 0, 0, false, true)
		if fill == "#ffffff" {
			if moved || texts[0].record.Placement.Status != "clear-within-scope" || texts[0].record.Lines[0].Baseline != before.Baseline {
				t.Fatal("hidden route displaced opaque node title")
			}
		} else if !moved || texts[0].record.Placement.Offset == [2]float64{} {
			t.Fatalf("transparent fill incorrectly hid route: %s", fill)
		}
	}
}

func TestPlanTitlePlacementFallbackAndConflictIdentity(t *testing.T) {
	doc := planPlacementTestDoc()
	doc.Scene().Parts[1].Label = "unknown font"
	scene, report := RenderSceneWithReport(doc)
	p := scene.PlanLabels[0]
	if p.Placement.Status != "blocked" || p.Placement.Offset != [2]float64{} || p.Placement.RetainedReason != "unmeasured-text" {
		t.Fatalf("unknown neighbour did not retain layout: %+v", p.Placement)
	}
	found := false
	for _, c := range p.Placement.Conflicts {
		if c.Kind == "route" && c.ConnectorIndex != nil && *c.ConnectorIndex == 0 && c.ClearanceShortfall > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("missing route identity and severity")
	}
	found = false
	for _, d := range report.Defects {
		if d.Kind == "plan-label-placement" && d.Location == "header" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing located report warning")
	}
	rects, _, edges := buildPlanModel(doc.Scene(), doc.Theme, doc.Canvas)
	ox, oy, _, _ := planFrame(rects, edges, doc.Canvas)
	texts := layoutPlanLabels(rects, ox, oy)
	s := connectorTextStroke{owner: 0, a: [2]float64{0, 20 + oy}, b: [2]float64{500, 20 + oy}, radius: 2.7}
	// Isolate the search budget from the unknown-font fallback.
	rects[2].label = ""
	texts[2] = planTextLayout{}
	if refinePlanTitlesWithBudget(rects, texts, []planTitleObstacle{{kind: "route", index: 0, stroke: &s}}, ox, oy, false, true, 0) {
		t.Fatal("zero budget accepted a move")
	}
	if texts[0].record.Placement.RetainedReason != "search-budget" {
		t.Fatalf("wrong exhausted reason: %+v", texts[0].record.Placement)
	}
	d := visualDefects{}
	addPlanTitlePlacementDefects(d, texts)
	worse := visualDefects{}
	for k, v := range d {
		worse[k] = v
		if k.kind == "plan-title-route" {
			worse[k] = v + 1
		}
	}
	if worse.noWorseThan(d) {
		t.Fatal("candidate gate accepts a deeper route conflict")
	}
}

func TestPlanTitlePlacementDoesNotTransferConflicts(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261012, 9973))
	moves := 0
	for trial := 0; trial < 140; trial++ {
		var rects []planRect
		for i := 0; i < 4; i++ {
			size := 14.
			st := &Style{Text: &Text{Family: "Source Sans Pro", Weight: "400", Size: &size}}
			r := planRectFor(fmt.Sprint(i), "Service AV office", float64(i)*80, float64(i%2)*35, 0, 220, 90, 8, "boundary", st, true)
			rects = append(rects, r)
		}
		texts := layoutPlanLabels(rects, 0, 0)
		initial := layoutPlanLabels(rects, 0, 0)
		var static []planTitleObstacle
		for j := 0; j < 3; j++ {
			y := rng.Float64() * 100
			s := connectorTextStroke{owner: j, a: [2]float64{-20, y}, b: [2]float64{520, y}, radius: 1 + rng.Float64()*3}
			static = append(static, planTitleObstacle{kind: "route", index: j, stroke: &s})
		}
		old := make([][]PlanLabelConflict, len(texts))
		wasClear := make([]bool, len(texts))
		for i, p := range initial {
			obs := planTitleObstacles(i, rects, initial, static, 0, 0)
			old[i] = planTitleConflicts(p, obs, rects, screenRect{}, false)
			wasClear[i] = len(old[i]) == 0
		}
		refinePlanTitles(rects, texts, static, 0, 0, false, true)
		for i, p := range texts {
			if wasClear[i] && !reflect.DeepEqual(initial[i].record.Lines, p.record.Lines) {
				t.Fatal("moved a clear title")
			}
			if p.record.Placement.Offset != [2]float64{} {
				moves++
				if p.record.Placement.Status != "clear-within-scope" {
					t.Fatal("accepted move remains obstructed")
				}
			}
			now := planTitleConflicts(p, planTitleObstacles(i, rects, texts, static, 0, 0), rects, screenRect{}, false)
			for _, c := range now {
				matched := false
				for _, o := range old[i] {
					if c.Kind == o.Kind && reflect.DeepEqual(c.PartIndex, o.PartIndex) && reflect.DeepEqual(c.ConnectorIndex, o.ConnectorIndex) && c.ClearanceShortfall <= o.ClearanceShortfall+1e-9 {
						matched = true
					}
				}
				if !matched {
					t.Fatalf("trial %d transferred conflict %+v", trial, c)
				}
			}
		}
	}
	if moves < 10 {
		t.Fatalf("property corpus exercised only %d accepted moves", moves)
	}
}

func TestPlanTitleSubmittedAnchorAfterMove(t *testing.T) {
	scene := RenderScene(planPlacementTestDoc())
	p := scene.PlanLabels[0]
	dec := xml.NewDecoder(strings.NewReader(scene.SVG))
	index := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "text" {
			continue
		}
		a := map[string]string{}
		for _, v := range start.Attr {
			a[v.Name.Local] = v.Value
		}
		var text string
		dec.DecodeElement(&text, &start)
		if index < len(p.Lines) {
			l := p.Lines[index]
			if text != l.Text || a["x"] != fmt.Sprintf("%.2f", l.X) || a["y"] != fmt.Sprintf("%.2f", l.Baseline) {
				t.Fatal("SVG emitted stale anchor")
			}
		}
		index++
	}
	if index != len(p.Lines) {
		t.Fatal("title text was lost")
	}
}

func TestPlanTitlePlacementIgnoresTextHiddenBehindOwner(t *testing.T) {
	doc := planTextTestDoc("Warehouse metadata services", 210, 180, 18)
	g := doc.Scene().Parts[0]
	g.Shape = "group"
	size := 18.
	g.Parts = []*CompositePart{{ID: "child", Shape: "rectangle", Label: "Child service", Geom: &Geom{W: 150, D: 55, H: 24}, Offset: &WorldPoint{WX: 20}, Style: &Style{Text: &Text{Family: "Source Sans Pro", Size: &size, Weight: "400"}}}}
	scene := RenderScene(doc)
	if len(scene.PlanLabels) != 2 {
		t.Fatal("missing titles")
	}
	child := scene.PlanLabels[1]
	if child.Placement.Status != "clear-within-scope" || child.Placement.Offset != [2]float64{} {
		t.Fatal("hidden earlier title displaced the child title")
	}
	if scene.PlanLabels[0].Placement.Status != "blocked" {
		t.Fatal("occluded earlier title falsely declared clear")
	}
}

func TestPlanTitlePlacementRetainsExportMargin(t *testing.T) {
	doc := planTextTestDoc("AV office", 180, 100, 23)
	doc.Canvas.Padding = 1
	doc.Scene().Parts[0].Style.Palette = &Palette{Top: "none"}
	doc.Scene().Parts = append(doc.Scene().Parts,
		&CompositePart{ID: "from", Shape: "rectangle", Geom: &Geom{W: 30, D: 20, H: 20}, Offset: &WorldPoint{WX: -60, WY: 40}},
		&CompositePart{ID: "to", Shape: "rectangle", Geom: &Geom{W: 30, D: 20, H: 20}, Offset: &WorldPoint{WX: 240, WY: 40}})
	w := 20.
	doc.Scene().Connectors = []*Connector{{From: "from", To: "to", Routing: "straight", Arrow: "none", Stroke: &Stroke{Width: &w}}}
	scene := RenderScene(doc)
	p := scene.PlanLabels[0]
	if p.Placement.Offset == [2]float64{} {
		t.Fatal("fixture did not move title toward viewport edge")
	}
	var root struct {
		ViewBox string `xml:"viewBox,attr"`
	}
	if err := xml.Unmarshal([]byte(scene.SVG), &root); err != nil {
		t.Fatal(err)
	}
	var vb [4]float64
	fmt.Sscan(root.ViewBox, &vb[0], &vb[1], &vb[2], &vb[3])
	for _, l := range p.Lines {
		if b := l.InkBounds; b != nil {
			if b.X-16 < vb[0]-.011 || b.Y-16 < vb[1]-.011 || b.X+b.Width+16 > vb[0]+vb[2]+.011 || b.Y+b.Height+16 > vb[1]+vb[3]+.011 {
				t.Fatal("viewport used stale title bounds")
			}
		}
	}
}
