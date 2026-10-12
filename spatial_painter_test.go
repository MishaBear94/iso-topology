package isotopo

import (
	"encoding/xml"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// A slanted foreground surface cuts through both the line and direction
// marker. The independent reference simply paints that surface last, with no
// masks and no excluded edge pixels.
func painterFixture(fill string) (string, string, *routeCollector, connectorSpatialScene) {
	ax, ay := isoUnproject(20, 100, 0)
	bx, by := isoUnproject(180, 100, 0)
	d := testArrowDrawing([3]float64{ax, ay, 0}, [3]float64{bx, by, 0}, 12)
	p := d.Arrow
	arrow := fmt.Sprintf(`<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="%s"/>`, p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1], fill)
	defs := `<linearGradient id="g" gradientUnits="userSpaceOnUse" x1="20" y1="100" x2="180" y2="100"><stop stop-color="#c0202080"/><stop offset="1" stop-color="#2030c0c0"/></linearGradient>`
	path := fmt.Sprintf(`<path data-connector="0" d="M 20,100 L 180,100" fill="none" stroke="%s" stroke-width="12" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="18 6"/>`, fill)
	head := `<svg xmlns="http://www.w3.org/2000/svg" width="220" height="160" viewBox="0 0 220 160"><rect width="220" height="160" fill="white"/>`
	face := [][2]float64{{168.3, 80}, {208.3, 80}, {218.3, 120}, {178.3, 120}}
	body := `<g data-part="0" data-part-id="b"><polygon points="168.3,80 208.3,80 218.3,120 178.3,120" fill="#80b0ee"/></g>`
	svg := head + `<g data-layer="connectors">` + defs + path + `</g>` + body + `<g data-layer="connector-overlay">` + arrow + `</g></svg>`
	want := head + `<defs>` + defs + `</defs>` + path + arrow + body + `</svg>`
	rc := &routeCollector{routes: []ConnectorRoute{{Index: 0, Drawing: d}}, paint: []connectorPaintPart{{id: "b", region: screenRegion{face}}}, arrowFragments: []connectorArrowFragment{{0, 0, arrow}}}
	scene := connectorSpatialScene{surfaces: []spatialPiece{{poly: face, depth: depthPlane{0, 0, 1000}, owner: "b"}}, boxes: []spatialBox{{"b", [3]float64{100, 100, 100}, [3]float64{200, 200, 200}}}}
	return svg, want, rc, scene
}

func checkPainterEdgePixels(t *testing.T, hybrid bool) {
	t.Helper()
	for _, fill := range []string{"#c02020", "#c0202070", "url(#g)"} {
		for _, scale := range []float64{.25, 1, 4} {
			t.Run(fmt.Sprintf("%s/%g", fill, scale), func(t *testing.T) {
				svg, reference, rc, scene := painterFixture(fill)
				mode := "painter"
				if hybrid {
					mode = "hybrid"
					face := [][2]float64{{20, 70}, {80, 70}, {80, 130}, {20, 130}}
					body := `<g data-part="0" data-part-id="mixed"><path d="M20,70H80V130H20Z" fill="#b0bbcc"/></g>`
					svg = strings.Replace(svg, `<g data-part="0"`, body+`<g data-part="1"`, 1)
					scene.surfaces = append([]spatialPiece{{poly: face, depth: depthPlane{1, 0, 150}, owner: "mixed"}}, scene.surfaces...)
					scene.boxes = append([]spatialBox{{id: "mixed", lo: [3]float64{0, 0, 0}, hi: [3]float64{1, 1, 1}}}, scene.boxes...)
					rc.paint = append([]connectorPaintPart{{id: "mixed", region: screenRegion{face}}}, rc.paint...)
					// Independent closed-form reference: line depth is 200,
					// body depth is x+150. Only 50.0001 < x < 80 is hidden.
					clip := `<defs><clipPath id="reference-cut"><path d="M-10,-10H50.0001V170H-10Z M80,-10H230V170H80Z"/></clipPath></defs>`
					reference = strings.Replace(reference, `<defs>`, body+clip+`<defs>`, 1)
					reference = strings.Replace(reference, `data-connector="0"`, `data-connector="0" clip-path="url(#reference-cut)"`, 1)
				}
				gotSVG := applyConnectorSpatial(svg, rc, scene, len(scene.boxes))
				if c := rc.routes[0].Drawing.Spatial.Compositing; c == nil || c.Mode != mode {
					t.Fatalf("not ordered: %+v", c)
				}
				want, got := spatialRaster(t, reference, scale), spatialRaster(t, gotSVG, scale)
				changed, worst := 0, 0
				for y := 0; y < want.Bounds().Dy(); y++ {
					for x := 0; x < want.Bounds().Dx(); x++ {
						d := spatialPixelDelta(want, got, x, y)
						worst = max(worst, d)
						if d > 1 {
							changed++
						}
					}
				}
				if dir := os.Getenv("ISOTOPO_PAINTER_EXPORT"); dir != "" && fill == "#c02020" && scale == 1 && !hybrid {
					for n, s := range map[string]string{"actual.svg": gotSVG, "reference.svg": reference} {
						if err := os.WriteFile(dir+"/"+n, []byte(s), 0644); err != nil {
							t.Fatal(err)
						}
					}
				}
				if changed != 0 {
					t.Fatalf("occluder AA differs at %d pixels, worst channel error %d/255", changed, worst)
				}
				if strings.Count(gotSVG, `data-connector="0"`) != 1 {
					t.Fatal("painted path more than once")
				}
			})
		}
	}
}

func TestSpatialPainterRealSceneCoverage(t *testing.T) {
	cases := map[string]*Document{"long-line": parseWSH(t, spatialLongLineFixture), "front": arrowDepthScene(t, true), "behind": arrowDepthScene(t, false)}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			rc := &routeCollector{}
			renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
			for _, r := range rc.routes {
				c := r.Drawing.Spatial.Compositing
				// The source crosses the line's depth and needs a local clip.
				// The target is uniformly nearer wherever it overlaps; it can
				// paint last without a second clip caused by numerical slivers.
				want := []string{"a"}
				if c.Mode != "hybrid" || !reflect.DeepEqual(c.LineClipOwners, want) || len(c.ArrowClipOwners) != 0 {
					t.Fatalf("unexpected local clips: %+v", c)
				}
				t.Logf("edge %d: %+v", r.Index, c)
			}
		})
	}
}

func TestSpatialPainterAllEdgePixels(t *testing.T)       { checkPainterEdgePixels(t, false) }
func TestSpatialPainterHybridAllEdgePixels(t *testing.T) { checkPainterEdgePixels(t, true) }

// This assertion runs on WASM and machines without resvg. A wholly nearer
// slanted surface cannot acquire a reverse dependency from rounding slivers.
func TestSpatialPainterForegroundOrderWithoutRasterizer(t *testing.T) {
	svg, _, rc, scene := painterFixture("#c02020")
	applyConnectorSpatial(svg, rc, scene, len(scene.boxes))
	c := rc.routes[0].Drawing.Spatial.Compositing
	if c == nil || c.Mode != "painter" || len(c.LineClipOwners) != 0 || len(c.ArrowClipOwners) != 0 {
		t.Fatalf("uniformly nearer foreground must paint last: %+v", c)
	}
}

// An independent one-dimensional arrangement supplies exact classification
// intervals. The implementation uses convex polygon subtraction in 2D.
func TestSpatialPainterNearestSurfaceOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(91))
	counts := [4]int{}
	for trial := 0; trial < 600; trial++ {
		p := spatialPiece{poly: [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, depth: depthPlane{float64(rng.Intn(9) - 4), 0, float64(rng.Intn(21) - 10)}}
		subject := painterNode{kind: "route", pieces: []painterPiece{painterBounded(p)}}
		body := painterNode{kind: "part"}
		cuts := []float64{0, 10}
		for k := 0; k < 1+rng.Intn(6); k++ {
			lo, hi := float64(rng.Intn(15)-3), float64(rng.Intn(15)-3)
			if lo > hi {
				lo, hi = hi, lo
			}
			if lo == hi {
				hi++
			}
			q := spatialPiece{poly: [][2]float64{{lo, -1}, {hi, -1}, {hi, 11}, {lo, 11}}, depth: depthPlane{float64(rng.Intn(9) - 4), 0, float64(rng.Intn(21) - 10)}}
			body.pieces = append(body.pieces, painterBounded(q))
			for _, x := range []float64{lo, hi} {
				if x > 0 && x < 10 {
					cuts = append(cuts, x)
				}
			}
			if a := q.depth[0] - p.depth[0]; a != 0 {
				x := (p.depth[2] - q.depth[2] + .0001) / a
				if x > 0 && x < 10 {
					cuts = append(cuts, x)
				}
			}
		}
		sort.Float64s(cuts)
		want := 0
		for i := 1; i < len(cuts); i++ {
			if cuts[i] == cuts[i-1] {
				continue
			}
			x := (cuts[i] + cuts[i-1]) / 2
			overlap, hidden := false, false
			for _, q := range body.pieces {
				if x > q.bounds.x0 && x < q.bounds.x1 {
					overlap = true
					hidden = hidden || q.depth.at([2]float64{x, 5}) > p.depth.at([2]float64{x, 5})+.0001
				}
			}
			if hidden {
				want |= 2
			} else if overlap {
				want |= 1
			}
		}
		work := spatialPainterWork
		got, ok := painterRelation(subject, body, &work)
		if !ok || got != want {
			t.Fatalf("trial %d got %d/%v want %d", trial, got, ok, want)
		}
		counts[got]++
		work = spatialPainterWork
		reverse, ok := painterRelation(body, subject, &work)
		if !ok || reverse != (want&1)<<1|(want&2)>>1 {
			t.Fatalf("reverse trial %d", trial)
		}
	}
	if counts[1] == 0 || counts[2] == 0 || counts[3] == 0 {
		t.Fatal(counts)
	}
	t.Logf("600 exact overlap classifications, plus reversed order: %v", counts)
}

func TestSpatialPainterBudgetAndIdentity(t *testing.T) {
	svg, _, rc, scene := painterFixture("#c02020")
	original := svg
	// Corrupting a body identity must roll back SVG and observations together.
	svg = strings.Replace(svg, `data-part-id="b"`, `data-part-id="wrong"`, 1)
	got := applyConnectorSpatial(svg, rc, scene, 1)
	if got != svg || rc.routes[0].Drawing.Spatial.Mode != "legacy" || rc.routes[0].Drawing.Spatial.Compositing != nil {
		t.Fatal("non-atomic identity failure")
	}
	_, _, rc, scene = painterFixture("#c02020")
	nodes, why := painterReadNodes(original, rc, scene, 1)
	// Original SVG has not been annotated with the producer's arrow identity.
	if why == "" || len(nodes) > 0 {
		t.Fatal("unsubmitted arrow accepted")
	}
	p := painterBounded(spatialPiece{poly: [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}})
	work := 0
	if _, ok := painterRelation(painterNode{kind: "route", pieces: []painterPiece{p}}, painterNode{kind: "part", pieces: []painterPiece{p}}, &work); ok {
		t.Fatal("comparison ignored exhausted budget")
	}
}

func TestSpatialPainterSubmittedLayers(t *testing.T) {
	for _, front := range []bool{false, true} {
		doc := arrowDepthScene(t, front)
		rc := &routeCollector{}
		svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
		decoder := xml.NewDecoder(strings.NewReader(svg))
		var layers []string
		painted, pathLayer, arrowLayer := 0, -1, -1
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			switch e := token.(type) {
			case xml.StartElement:
				attrs := map[string]string{}
				for _, a := range e.Attr {
					attrs[a.Name.Local] = a.Value
				}
				if _, ok := attrs["data-part"]; ok {
					painted++
				}
				if attrs["data-connector"] == "0" {
					pathLayer = painted
					if attrs["data-hit-clip"] != "url(#conn-hit-visible-0)" {
						t.Fatal("lost hit clip")
					}
				}
				if e.Name.Local == "polygon" && len(layers) > 0 && layers[len(layers)-1] == "connector-overlay" {
					arrowLayer = painted
				}
				layers = append(layers, attrs["data-layer"])
			case xml.EndElement:
				layers = layers[:len(layers)-1]
			}
		}
		d := rc.routes[0].Drawing
		if pathLayer != d.PaintBeforePart || d.Spatial.Arrow.PaintBeforePart == nil || arrowLayer != *d.Spatial.Arrow.PaintBeforePart {
			t.Fatalf("SVG/metadata order mismatch: %d %d %+v", pathLayer, arrowLayer, d)
		}
		again := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, &routeCollector{}, true)
		if svg != again {
			t.Fatal("unstable painter order")
		}
	}
}

func TestSpatialPainterDiagonalAndClipIdentity(t *testing.T) {
	// The centre sees the line in front, but the far corner sees the body in
	// front. A centre-only test would select an invalid whole-primitive order.
	poly := [][2]float64{{0, 0}, {10, 0}, {10, 10}, {0, 10}}
	line := painterNode{kind: "route", pieces: []painterPiece{painterBounded(spatialPiece{poly: poly})}}
	body := painterNode{kind: "part", pieces: []painterPiece{painterBounded(spatialPiece{poly: poly, depth: depthPlane{1, 1, -18.9}})}}
	work := spatialPainterWork
	if got, ok := painterRelation(line, body, &work); !ok || got != 3 {
		t.Fatalf("missed diagonal depth crossing: %d %v", got, ok)
	}
	svg, _, rc, scene := painterFixture("#c02020")
	scene.surfaces[0].depth = depthPlane{1, 0, 25}
	svg = strings.Replace(svg, "</svg>", `<defs><clipPath id="conn-painter-route-0"/></defs></svg>`, 1)
	got := applyConnectorSpatial(svg, rc, scene, 1)
	d := rc.routes[0].Drawing
	if got != svg || d.Spatial.Mode != "legacy" || d.Spatial.Compositing != nil || d.Spatial.Arrow != nil {
		t.Fatal("local clip identity conflict was not atomic")
	}
}
