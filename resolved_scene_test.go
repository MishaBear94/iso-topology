package isotopo

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestSceneReadersPreserveAuthorDocument(t *testing.T) {
	const src = `nodes:
  scene:
    shape: composite
    layout: {mode: row}
    parts:
      - id: g
        shape: group
        style: {effects: {faceSplit: true}}
        parts:
          - {id: a, shape: rectangle, label: A}
      - id: b
        shape: boundary
        geom: {w: 300, d: 200}
        style: {stroke: {color: "#123456"}}
`
	readers := map[string]func(*Document) any{
		"render":      func(d *Document) any { return RenderWithCanvas(d.Scene(), d.Theme, d.Canvas, nil) },
		"plan":        func(d *Document) any { return RenderPlan(d.Scene(), d.Theme, d.Canvas, nil) },
		"routes":      func(d *Document) any { s, _ := RenderWithRouteInfo(d.Scene(), d.Theme, d.Canvas, nil); return s },
		"interaction": func(d *Document) any { return BuildInteractionModel(d) },
		"offset":      func(d *Document) any { x, y, z, ok := ResolvePartOffset(d, "a"); return []any{x, y, z, ok} },
		"dimensions":  func(d *Document) any { w, h, z, ok := ResolvePartGeom(d, "g"); return []any{w, h, z, ok} },
		"labels":      func(d *Document) any { return LabelOcclusionIssues(d) },
		"bodies":      func(d *Document) any { return BodyOcclusions(d) },
		"lint":        func(d *Document) any { return VisualLint(d) },
		"score":       func(d *Document) any { return EvaluateIso(d.Scene(), d.Theme, d.Canvas) },
	}
	for name, read := range readers {
		t.Run(name, func(t *testing.T) {
			d, err := ParseYAML([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(d)
			first, _ := json.Marshal(read(d))
			second, _ := json.Marshal(read(d))
			after, _ := json.Marshal(d)
			if string(before) != string(after) {
				t.Fatal("reader mutated author geometry, layout or style")
			}
			if string(first) != string(second) {
				t.Fatal("repeated read changed its result")
			}
		})
	}
}

// Verify both the model and the actual SVG against authored coordinates. The
// SVG oracle reads a sharp box's emitted back/top vertex and its group transform,
// without calling the production origin, projection or anchor helpers.
func TestGridAndElevatedGroupCoordinateParity(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      [3]float64
	}{
		{"grid", `nodes:
  scene:
    shape: composite
    gridStep: 40
    parts:
      - {id: a, shape: rectangle, geom: {w: 80, d: 60, h: 20}, position: {i: 2, j: 3}, offset: {wx: 10, wy: 20, wz: 5}}
`, [3]float64{90, 140, 5}},
		{"elevated-group", `nodes:
  scene:
    shape: composite
    gridStep: 40
    parts:
      - id: g
        shape: group
        geom: {w: 360, d: 240, h: 7}
        offset: {wx: 100, wy: 200, wz: 50}
        position: {i: 2, j: 3}
        parts:
          - {id: a, shape: rectangle, geom: {w: 80, d: 60, h: 20}, position: {i: 1, j: -1}, offset: {wx: 10, wy: 20, wz: 3}}
`, [3]float64{230, 300, 60}},
		{"boundary-default", `nodes:
  scene:
    shape: composite
    parts:
      - id: g
        shape: boundary
        geom: {w: 360, d: 240}
        offset: {wx: 100, wy: 200, wz: 50}
        parts:
          - {id: a, shape: rectangle, geom: {w: 80, d: 60, h: 20}, offset: {wx: 10, wy: 20, wz: 3}}
`, [3]float64{110, 220, 55}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := ParseYAML([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range BuildInteractionModel(d) {
				if p.ID == "a" {
					found = true
					if got := ([3]float64{p.X, p.Y, p.Z}); got != tc.want {
						t.Errorf("model position %v, want %v", got, tc.want)
					}
				}
			}
			if !found {
				t.Fatal("missing model node")
			}
			svg := RenderWithCanvas(d.Scene(), d.Theme, d.Canvas, nil)
			dec := xml.NewDecoder(strings.NewReader(svg))
			var tx, ty, gx, gy float64
			inPart := false
			for {
				tok, err := dec.Token()
				if err != nil {
					break
				}
				e, ok := tok.(xml.StartElement)
				if !ok {
					continue
				}
				a := map[string]string{}
				for _, v := range e.Attr {
					a[v.Name.Local] = v.Value
				}
				if e.Name.Local == "svg" {
					tx, _ = strconv.ParseFloat(a["data-scene-tx"], 64)
					ty, _ = strconv.ParseFloat(a["data-scene-ty"], 64)
				}
				if _, exists := a["data-part"]; exists {
					inPart = a["data-part-id"] == "a"
					if inPart {
						fmt.Sscanf(a["transform"], "translate(%f %f)", &gx, &gy)
					}
				}
				if inPart && a["data-face"] == "top" {
					var px, py float64
					fmt.Sscanf(a["points"], "%f,%f", &px, &py)
					wx := (tc.want[0]-tc.want[1])*math.Sqrt(3)/2 + tx
					wy := (tc.want[0]+tc.want[1])/2 - tc.want[2] - 20 + ty
					// Scene origin attributes round to integer, SVG vertices to .01.
					if math.Abs(px+gx-wx) > .52 || math.Abs(py+gy-wy) > .52 {
						t.Errorf("SVG top corner (%g,%g), expected (%g,%g)", px+gx, py+gy, wx, wy)
					}
					return
				}
			}
			t.Fatal("missing SVG top face")
		})
	}
}

func TestActualShapeDimensionsReachLayoutAndInteraction(t *testing.T) {
	for _, tc := range []struct {
		shape string
		geom  *Geom
		want  [3]float64
	}{
		{"rectangle", nil, [3]float64{140, 140, 80}},
		{"cylinder", nil, [3]float64{160, 160, 110}},
		{"circle", &Geom{W: 90, D: 200, H: 5}, [3]float64{90, 90, 90}},
		{"person", nil, [3]float64{120, 120, 50}},
		{"square", &Geom{W: 180, D: 80, H: 30}, [3]float64{180, 180, 30}},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			p := &CompositePart{ID: "a", Shape: tc.shape, Geom: tc.geom}
			w, d := partFootprint(p)
			h := partHeight(p)
			if got := ([3]float64{w, d, h}); got != tc.want {
				t.Errorf("layout dimensions %v, want %v", got, tc.want)
			}
			doc := &Document{Nodes: map[string]*Node{"scene": {Shape: "composite", Parts: []*CompositePart{p}}}}
			model := BuildInteractionModel(doc)
			if len(model) != 1 {
				t.Fatal(model)
			}
			if got := ([3]float64{model[0].W, model[0].D, model[0].H}); got != tc.want {
				t.Errorf("model dimensions %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOcclusionIncludesImplicitOrigin(t *testing.T) {
	const src = `nodes:
  scene:
    shape: composite
    parts:
      - {id: base, shape: rectangle, label: Base, geom: {w: 300, d: 300, h: 26}}
      - {id: mid, shape: rectangle, geom: {w: 300, d: 300, h: 26}, place: {above: base}}
`
	d, err := ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	issues := LabelOcclusionIssues(d)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, `"Base"`) {
		t.Fatalf("origin is a real position: must report covered Base label: %v", issues)
	}
	d.Scene().Parts[0].Offset = &WorldPoint{}
	explicit := LabelOcclusionIssues(d)
	a, _ := json.Marshal(issues)
	b, _ := json.Marshal(explicit)
	if string(a) != string(b) {
		t.Fatal("omitted origin and explicit zero must have identical detection")
	}
}

func TestReparentPreservesElevatedGridWorldPositions(t *testing.T) {
	const src = `nodes:
  scene:
    shape: composite
    gridStep: 40
    parts:
      - id: g
        shape: group
        geom: {w: 360, d: 240, h: 7}
        offset: {wx: 100, wy: 200, wz: 50}
        position: {i: 2, j: 3}
        parts:
          - {id: a, shape: rectangle, geom: {w: 80, d: 60, h: 20}, position: {i: 1, j: -1}, offset: {wx: 10, wy: 20, wz: 3}}
      - id: destination
        shape: group
        geom: {w: 500, d: 500, h: 9}
        offset: {wx: 500, wy: 600, wz: 100}
        parts:
          - {id: sibling, shape: rectangle, geom: {w: 80, d: 60, h: 20}, position: {i: 2, j: 1}, offset: {wx: 10, wy: 20, wz: 3}}
      - {id: empty, shape: group, geom: {w: 400, d: 300, h: 11}, offset: {wx: 400, wy: 400, wz: 80}}
`
	positions := func(source []byte) map[string][3]float64 {
		t.Helper()
		d, err := ParseYAML(source)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string][3]float64{}
		for _, p := range BuildInteractionModel(d) {
			m[p.ID] = [3]float64{p.X, p.Y, p.Z}
		}
		return m
	}
	original := positions([]byte(src))
	if original["a"] != ([3]float64{230, 300, 60}) {
		t.Fatal(original)
	}
	current := []byte(src)
	for _, dest := range []string{"", "destination", "empty", "g"} {
		out, err := ApplyOpText("yaml", current, EditOp{Kind: "reparent", ID: "a", Target: dest})
		if err != nil {
			t.Fatal(err)
		}
		moved, err := ParseYAML(out)
		if err != nil {
			t.Fatal(err)
		}
		if parent := parentOf(moved, "a"); parent != dest {
			t.Fatalf("moved node into %q, requested %q", parent, dest)
		}
		got := positions(out)
		for id, want := range original {
			if got[id] != want {
				t.Fatalf("reparent to %q moved %s: got %v want %v\n%s", dest, id, got[id], want, out)
			}
		}
		current = out
	}
}

func TestGridCoordinatesParticipateInLayout(t *testing.T) {
	for _, mode := range []string{"relative", "auto", "row", "autosize"} {
		t.Run(mode, func(t *testing.T) {
			p := &CompositePart{ID: "a", Shape: "rectangle", Geom: &Geom{W: 80, D: 60, H: 20}, Position: &GridPos{I: 2, J: 3}, Offset: &WorldPoint{WX: 10, WY: 20}}
			b := &CompositePart{ID: "b", Shape: "rectangle", Geom: &Geom{W: 80, D: 60, H: 20}, Place: &Place{RightOf: "a"}}
			n := &Node{Shape: "composite", GridStep: 40, Parts: []*CompositePart{p, b}}
			wantX, wantY := 90.0, 140.0
			switch mode {
			case "auto":
				n.Layout = &Layout{Mode: "auto"}
				p.Offset = nil
				wantX, wantY = 80, 120
				b.Place = nil
			case "row":
				n.Layout = &Layout{Mode: "row"}
				b.Place = nil
				wantX, wantY = 130, 180
			case "autosize":
				n.Parts = []*CompositePart{{ID: "g", Shape: "group", Offset: &WorldPoint{}, Parts: []*CompositePart{p}}}
			}
			doc := &Document{Nodes: map[string]*Node{"scene": n}}
			for _, m := range BuildInteractionModel(doc) {
				if m.ID == "a" && (m.X != wantX || m.Y != wantY) {
					t.Errorf("a position (%g,%g), want (%g,%g)", m.X, m.Y, wantX, wantY)
				}
				if mode == "relative" && m.ID == "b" && (m.X != 210 || m.Y != 140) {
					t.Errorf("rightOf ignored grid position: %+v", m)
				}
			}
			applyLayout(n, nil)
			first, _ := json.Marshal(n)
			applyLayout(n, nil)
			second, _ := json.Marshal(n)
			if string(first) != string(second) {
				t.Fatalf("grid normalization accumulated on repeat\n%s\n%s", first, second)
			}
		})
	}
}

func TestPlanRouteAttachesAcrossFace(t *testing.T) {
	fr := planRect{id: "orders", x: 560, y: 280, w: 140, d: 140, h: 80}
	to := planRect{id: "db", x: 840, y: 120, w: 160, d: 160, h: 110}
	pts := selectRoute(fr, to, []planRect{fr, to}, nil)
	// The tempting one-bend candidate runs along Orders' top and DB's bottom.
	// A valid approach instead has a non-zero final segment crossing a face.
	last := pts[len(pts)-1]
	j := len(pts) - 2
	for j >= 0 && pts[j] == last {
		j--
	}
	if j < 0 {
		t.Fatal("zero-length route")
	}
	dx, dy := last[0]-pts[j][0], last[1]-pts[j][1]
	if last[0] == to.x && !(dx > 0 && dy == 0) || last[1] == to.y+to.d && !(dy < 0 && dx == 0) {
		t.Fatalf("arrow runs along target face: %v", pts)
	}
	if !planAttachmentsValid(pts, fr, to) {
		t.Fatalf("invalid attachment: %v", pts)
	}
}
