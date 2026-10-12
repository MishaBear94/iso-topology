package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
	"github.com/rivo/uniseg"
)

func planTextTestDoc(text string, w, d, size float64) *Document {
	return &Document{Canvas: &Canvas{Projection: "top"}, Nodes: map[string]*Node{"scene": {Shape: "composite", Parts: []*CompositePart{{ID: "a", Shape: "rectangle", Label: text, Geom: &Geom{W: w, D: d}, Style: &Style{Text: &Text{Family: "Source Sans Pro", Weight: "400", Size: &size}}}}}}}
}

func TestPlanTextSubmittedTypographyAndWrapping(t *testing.T) {
	doc := planTextTestDoc("Database connection pool", 106, 90, 18)
	before, _ := json.Marshal(doc)
	scene, report := RenderSceneWithReport(doc)
	if len(scene.PlanLabels) != 1 || !reflect.DeepEqual(scene.PlanLabels, report.PlanLabels) {
		t.Fatal("report lost submitted title records")
	}
	p := scene.PlanLabels[0]
	if p.Status != "within-budget" || p.FontSize != 18 || p.FontWeight != "400" || len(p.Lines) < 2 || p.FontHash == "" {
		t.Fatalf("wrong typography: %+v", p)
	}
	if len(scene.ScreenLabels) != 0 || len(ContentFits(doc)) != 0 {
		t.Fatal("plan text mislabeled as iso content")
	}
	// Inspect all text tokens independently of the producer's record helper.
	dec := xml.NewDecoder(strings.NewReader(scene.SVG))
	count := 0
	for {
		token, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "text" {
			continue
		}
		attrs := map[string]string{}
		for _, a := range start.Attr {
			attrs[a.Name.Local] = a.Value
		}
		var text string
		if err := dec.DecodeElement(&text, &start); err != nil {
			t.Fatal(err)
		}
		l := p.Lines[count]
		count++
		if text != l.Text || attrs["x"] != fmt.Sprintf("%.2f", l.X) || attrs["y"] != fmt.Sprintf("%.2f", l.Baseline) || attrs["font-size"] != "18.00" || attrs["font-weight"] != "400" || attrs["text-anchor"] != "middle" || attrs["space"] != "preserve" {
			t.Fatalf("record does not describe text: %v %q", attrs, text)
		}
	}
	if count != len(p.Lines) || strings.Count(scene.SVG, "data-isotopo-font=") != 1 {
		t.Fatal("missing lines or duplicate font")
	}
	after, _ := json.Marshal(doc)
	if string(before) != string(after) {
		t.Fatal("render modified author source")
	}
	if RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil) != scene.SVG {
		t.Fatal("observation changes SVG")
	}
}

func TestPlanTextWrapPreservesGraphemesAndWhitespace(t *testing.T) {
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "400")
	for _, src := range []string{"  AV  office  ", "A\u0301e\u0301n\u0303 office\n\nnext ", strings.Repeat("AV office ", 500), "\n \n", "extraordinarilylongword"} {
		for _, w := range []float64{0, 1, 25, 80, 500} {
			wrapped, source := wrapPlanText(src, f, 18, w, false)
			want := strings.Split(src, "\n")
			got := make([]string, len(want))
			boundaries := make([]map[int]bool, len(want))
			for i, s := range want {
				boundaries[i] = map[int]bool{0: true}
				g := uniseg.NewGraphemes(s)
				for g.Next() {
					_, end := g.Positions()
					boundaries[i][end] = true
				}
			}
			for i, s := range strings.Split(wrapped, "\n") {
				n := source[i]
				got[n] += s
				if !boundaries[n][len(got[n])] {
					t.Fatalf("split grapheme %q", s)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("lost authored content at width %g", w)
			}
		}
	}
}

func TestPlanTextOverflowViewportSelectionAndGate(t *testing.T) {
	doc := planTextTestDoc("UnbreakableDatabaseIdentifier", 30, 18, 24)
	doc.Scene().Parts[0].Offset = &WorldPoint{WX: .127, WY: -.234}
	scene, rep := RenderSceneWithReport(doc)
	p := scene.PlanLabels[0]
	if p.Status != "overflow" || p.FontSize != 24 || len(p.Lines) != 1 || p.Lines[0].Text != p.Text {
		t.Fatal("overflow truncated, shrunk or vertically exploded")
	}
	found := false
	for _, d := range rep.Defects {
		found = found || d.Kind == "plan-label-fit" && d.Location == "a"
	}
	if !found {
		t.Fatal("missing located overflow warning")
	}
	var root struct {
		ViewBox string  `xml:"viewBox,attr"`
		TX, TY  float64 `xml:"-"`
	}
	if err := xml.Unmarshal([]byte(scene.SVG), &root); err != nil {
		t.Fatal(err)
	}
	var vb [4]float64
	fmt.Sscan(root.ViewBox, &vb[0], &vb[1], &vb[2], &vb[3])
	b := p.Lines[0].InkBounds
	if b.X < vb[0] || b.Y < vb[1] || b.X+b.Width > vb[0]+vb[2] || b.Y+b.Height > vb[1]+vb[3] {
		t.Fatal("viewport clips retained text")
	}
	model := BuildInteractionModel(doc)
	if len(model) != 1 || len(model[0].SelectionRegions) != 2 {
		t.Fatal("overflow text missing from selection")
	}
	if model[0].W != 30 || model[0].D != 18 || len(model[0].Ports) != 4 {
		t.Fatal("text changed footprint or ports")
	}
	_, rects := resolvedPlanGeometry(doc.Scene(), doc.Theme, doc.Canvas)
	d1, d2 := visualDefects{}, visualDefects{}
	addPlanLabelFitDefects(d1, rects)
	rects[0].fontSize = 30
	addPlanLabelFitDefects(d2, rects)
	if d2.noWorseThan(d1) || !d1.noWorseThan(d2) {
		t.Fatal("candidate gate accepts worse title overflow")
	}
}

func TestPlanTextContainerBudgetAndFallback(t *testing.T) {
	size := 18.
	st := &Style{Text: &Text{Family: "Source Sans Pro", Weight: "600", Size: &size}}
	parent := planRectFor("group", "Warehouse services", 0, 0, 0, 160, 160, 8, "group", st, true)
	child := planRectFor("child", "Child", 12, 50, 8, 130, 70, 20, "rectangle", st, false)
	child.parent = 0
	p := layoutPlanLabels([]planRect{parent, child}, 0, 0)
	if math.Abs(p[0].record.Budget.Height-37.4) > 1e-8 || p[0].record.Status != "within-budget" {
		t.Fatalf("wrong child-free band: %+v", p[0].record)
	}
	for _, l := range p[0].record.Lines {
		if l.InkBounds != nil && l.InkBounds.Y+l.InkBounds.Height > child.y-6 {
			t.Fatal("title intrudes into child")
		}
	}
	for _, c := range []struct{ family, weight, text string }{{"unknown", "400", "title"}, {"Source Sans Pro", "500", "title"}, {"Source Sans Pro", "400", "标题"}, {"Source Sans Pro", "400", "mixed 🙂"}} {
		doc := planTextTestDoc(c.text, 100, 80, 20)
		doc.Scene().Parts[0].Style.Text.Family = c.family
		doc.Scene().Parts[0].Style.Text.Weight = c.weight
		scene := RenderScene(doc)
		rec := scene.PlanLabels[0]
		if rec.Status != "unmeasured" || rec.Lines != nil || rec.FontHash != "" || rec.FontFamily != "Inter,system-ui,sans-serif" || rec.FontSize != 11 || strings.Contains(scene.SVG, "data-plan-label=") {
			t.Fatalf("false bound fallback: %+v", rec)
		}
	}
}

func TestPlanTextProtectsConnectorLabels(t *testing.T) {
	// The title lies in a boundary's header, outside every leaf footprint.
	// The old plan placement model treats this header as empty space.
	doc := planTextTestDoc("Warehouse metadata orchestration", 270, 140, 20)
	p := doc.Scene().Parts[0]
	p.ID = "header"
	p.Shape = "boundary"
	w, d := 30., 20.
	doc.Scene().Parts = append(doc.Scene().Parts,
		&CompositePart{ID: "from", Shape: "rectangle", Geom: &Geom{W: w, D: d}, Offset: &WorldPoint{WX: -60, WY: 0}},
		&CompositePart{ID: "to", Shape: "rectangle", Geom: &Geom{W: w, D: d}, Offset: &WorldPoint{WX: 300, WY: 0}})
	doc.Scene().Connectors = []*Connector{{From: "from", To: "to", Label: "synchronization", LabelFontFamily: "Source Sans Pro", Routing: "orthogonal", Arrow: "none"}}
	scene := RenderScene(doc)
	if len(scene.ConnectorLabels) != 1 {
		t.Fatal("missing measured connector")
	}
	c := scene.ConnectorLabels[0].Label
	if c.Typography.Placement == "blocked" {
		t.Fatal("fixture must have room for a clear label")
	}
	for _, l := range scene.PlanLabels[0].Lines {
		if b := l.InkBounds; b != nil && collides(connectorLabelRect(c), []screenRect{{b.X, b.Y, b.X + b.Width, b.Y + b.Height}}) {
			t.Fatal("clear connector pill covers the container title")
		}
	}
}
