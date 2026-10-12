package isotopo

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

const arrowDepthFixture = `canvas: {depthSort: true, background: "#ffffff", aspectRatio: -1}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: -200}}
      - {id: b, shape: rectangle, geom: {w: 40, d: 30, h: 20}, offset: {wx: 200}}
    connectors:
      - {from: a.port-right, to: b.port-left, routing: straight, arrow: triangle, stroke: {color: "#c02020", width: 4}}
`

func arrowDepthScene(t *testing.T, front bool) *Document {
	t.Helper()
	doc := parseWSH(t, arrowDepthFixture)
	_, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	p := routes[0].Drawing.Provenance.Segments[0].World[1]
	shift := 100.
	if !front {
		shift = -100
	}
	other := parseWSH(t, fmt.Sprintf(`nodes:
  other:
    shape: composite
    parts:
      - {id: occluder, shape: rectangle, geom: {w: 60, d: 60, h: 60}, offset: {wx: %g, wy: %g, wz: %g}}
`, p[0]+shift-30, p[1]+shift-30, p[2]+shift-30))
	doc.Scene().Parts = append(doc.Scene().Parts, other.Scene().Parts...)
	return doc
}

func testArrowDrawing(a, b [3]float64, width float64) *ConnectorDrawing {
	d := collisionDrawing([][3]float64{a, b})
	d.SourceRef, d.ResolvedSource = "a.port-right", "a.port-right"
	d.TargetRef, d.ResolvedTarget = "b.port-left", "b.port-left"
	d.StrokeWidth = width
	p, q := d.Points[0], d.Points[1]
	dx, dy := q[0]-p[0], q[1]-p[1]
	length := math.Hypot(dx, dy)
	dx, dy = dx/length, dy/length
	size := 6 + width
	d.Arrow = svgPoints([][2]float64{q, {q[0] - size*dx - size*.5*dy, q[1] - size*dy + size*.5*dx}, {q[0] - size*dx + size*.5*dy, q[1] - size*dy - size*.5*dx}})
	d.Attachments = connectorAttachments(&anchorResolver{}, d)
	return d
}

func TestSpatialArrowAgainstWorldRayOracle(t *testing.T) {
	visible, hidden := 0, 0
	for _, shift := range [][3]float64{{}, {10000, -20000, 3000}, {-80000, 20000, -6000}} {
		for _, height := range []float64{20, 80, 220} {
			for _, rise := range []float64{-70, 0, 110} {
				for _, width := range []float64{4, 12} {
					a, b := [3]float64{-200, -50, height}, [3]float64{640, -50, height + rise}
					boxes := [][6]float64{{500, -100, 0, 180, 120, 130}, {620, -30, 40, 80, 90, 180}}
					for k := range a {
						a[k] += shift[k]
						b[k] += shift[k]
						for i := range boxes {
							boxes[i][k] += shift[k]
						}
					}
					d := testArrowDrawing(a, b, width)
					lines, why := spatialLines(&ConnectorRoute{Drawing: d})
					if why != "" {
						t.Fatal(why)
					}
					scene := prepareConnectorSpatial(spatialTestParts(boxes))
					if scene.reason != "" {
						t.Fatal(scene.reason)
					}
					result, why := spatialArrowVisibility(d, lines, scene.surfaces)
					if why != "" {
						t.Fatal(why)
					}
					// Independent barycentric points and world ray/box intersections.
					// Endpoint depth interpolates along the projected terminal tangent;
					// displacement perpendicular to it leaves that depth unchanged.
					start, end := d.Points[0], d.Points[1]
					vx, vy := end[0]-start[0], end[1]-start[1]
					for i := 0; i < 30; i++ {
						for j := 0; j < 30-i; j++ {
							u, v := (float64(i)+.317)/31, (float64(j)+.271)/31
							p := [2]float64{(1-u-v)*d.Arrow[0][0] + u*d.Arrow[1][0] + v*d.Arrow[2][0], (1-u-v)*d.Arrow[0][1] + u*d.Arrow[1][1] + v*d.Arrow[2][1]}
							s := ((p[0]-start[0])*vx + (p[1]-start[1])*vy) / (vx*vx + vy*vy)
							depth := (1-s)*(a[0]+a[1]+a[2]) + s*(b[0]+b[1]+b[2])
							want := true
							for _, box := range boxes {
								if front, hit := rayBoxFront(p, box); hit && front > depth+.0001 {
									want = false
								}
							}
							got := spatialOracleContains(p, result.ClipRegion)
							if got != want {
								t.Fatalf("arrow depth at %v: got %v want %v", p, got, want)
							}
							if got {
								visible++
							} else {
								hidden++
							}
						}
					}
				}
			}
		}
	}
	if visible == 0 || hidden == 0 {
		t.Fatal("oracle must exercise both states", visible, hidden)
	}
	t.Logf("arrow world-ray samples: %d visible, %d hidden, %d total", visible, hidden, visible+hidden)
}

func TestSpatialArrowUnoccludedRasterParity(t *testing.T) {
	ax, ay := isoUnproject(20, 100, 0)
	bx, by := isoUnproject(180, 100, 0)
	d := testArrowDrawing([3]float64{ax, ay, 0}, [3]float64{bx, by, 0}, 12)
	lines, _ := spatialLines(&ConnectorRoute{Drawing: d})
	r, why := spatialArrowVisibility(d, lines, nil)
	if why != "" {
		t.Fatal(why)
	}
	p := d.Arrow
	for _, fill := range []string{"#c02020", "#c0202070", "url(#g)"} {
		for _, scale := range []float64{.25, 1, 4} {
			t.Run(fmt.Sprintf("%s/%g", fill, scale), func(t *testing.T) {
				head := `<svg xmlns="http://www.w3.org/2000/svg" width="220" height="160"><rect width="220" height="160" fill="white"/><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="120" y1="90" x2="180" y2="120"><stop stop-color="#c0202070"/><stop offset="1" stop-color="#2030c0b0"/></linearGradient><clipPath id="neutral"><rect width="220" height="160"/></clipPath></defs>`
				poly := fmt.Sprintf(`<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="%s"/>`, p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1], fill)
				plain := spatialRaster(t, head+poly+`</svg>`, scale)
				neutral := spatialRaster(t, head+`<g clip-path="url(#neutral)">`+poly+`</g></svg>`, scale)
				got := spatialRaster(t, head+`<defs>`+spatialClipMarkup("c", r.ClipRegion, [2]float64{})+`</defs><g clip-path="url(#c)">`+poly+`</g></svg>`, scale)
				for y := 0; y < got.Bounds().Dy(); y++ {
					for x := 0; x < got.Bounds().Dx(); x++ {
						if spatialPixelDelta(plain, neutral, x, y) > 2 {
							t.Fatal("neutral alpha compositor differs by more than 2/255")
						}
						if spatialPixelDelta(neutral, got, x, y) > 1 {
							t.Fatal("unoccluded arrow darkened or clipped")
						}
					}
				}
			})
		}
	}
}

func TestSpatialArrowPartialOcclusionRaster(t *testing.T) {
	ax, ay := isoUnproject(20, 100, 0)
	bx, by := isoUnproject(180, 100, 0)
	d := testArrowDrawing([3]float64{ax, ay, 0}, [3]float64{bx, by, 0}, 12)
	lines, _ := spatialLines(&ConnectorRoute{Drawing: d})
	face := spatialPiece{poly: [][2]float64{{170, 80}, {210, 80}, {210, 120}, {170, 120}}, depth: depthPlane{0, 0, 1000}, owner: "b"}
	r, why := spatialArrowVisibility(d, lines, []spatialPiece{face})
	if why != "" {
		t.Fatal(why)
	}
	p := d.Arrow
	for _, fill := range []string{"#c02020", "#c0202070", "url(#g)"} {
		for _, scale := range []float64{.25, 1, 4} {
			t.Run(fmt.Sprintf("%s/%g", fill, scale), func(t *testing.T) {
				head := `<svg xmlns="http://www.w3.org/2000/svg" width="220" height="160"><rect width="220" height="160" fill="white"/><defs><linearGradient id="g" gradientUnits="userSpaceOnUse" x1="120" y1="90" x2="180" y2="120"><stop stop-color="#c0202070"/><stop offset="1" stop-color="#2030c0b0"/></linearGradient></defs>`
				poly := fmt.Sprintf(`<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="%s"/>`, p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1], fill)
				body := `<rect x="170" y="80" width="40" height="40" fill="#80b0ee"/>`
				// Painter ordering is independent of clipping and depth geometry.
				want := spatialRaster(t, head+poly+body+`</svg>`, scale)
				got := spatialRaster(t, head+body+`<defs>`+spatialClipMarkup("c", r.ClipRegion, [2]float64{})+`</defs><g clip-path="url(#c)">`+poly+`</g></svg>`, scale)
				for y := 0; y < got.Bounds().Dy(); y++ {
					for x := 0; x < got.Bounds().Dx(); x++ {
						// At the occluder's AA fringe, clip alpha and painter alpha are
						// not generally interchangeable. Keep that known compositor
						// limit explicit, while verifying all visible/hidden interiors.
						if math.Abs((float64(x)+.5)/scale-170) < 2 {
							continue
						}
						if spatialPixelDelta(want, got, x, y) > 2 {
							t.Fatal("partially covered marker differs from independent painter interior")
						}
					}
				}
			})
		}
	}
}

func TestSpatialArrowProducerIdentityAndAtomicFallback(t *testing.T) {
	for _, mutation := range []string{"offset", "duplicate", "missing", "polygon", "id", "attachment"} {
		t.Run(mutation, func(t *testing.T) {
			doc := arrowDepthScene(t, true)
			rc := &routeCollector{}
			// Generate the original markup and its producer offsets without
			// first applying the experimental compositor.
			svg := renderComposite(doc.Scene(), doc.Theme, doc.Canvas, nil, rc)
			before, _ := json.Marshal(rc.routes[0].Drawing.Coverage)
			parts := lowerCompositeParts(doc.Scene().Parts, 0, 0, 0, doc.Scene().GridStep)
			parts, _ = orderCompositeParts(parts, doc.Canvas, doc.Scene().GridStep)
			var cp []iso25d.CompositePart
			for _, p := range parts {
				shape, opts := Flatten(&Node{Shape: p.Shape, Geom: p.Geom, Style: p.Style}, doc.Theme)
				x, y, z := partLocalOffset(p, doc.Scene().GridStep)
				cp = append(cp, iso25d.CompositePart{ID: p.ID, Shape: shape, Opts: opts, OffWX: x, OffWY: y, OffWZ: z})
			}
			switch mutation {
			case "offset":
				rc.arrowFragments[0].offset++
			case "duplicate":
				rc.arrowFragments = append(rc.arrowFragments, rc.arrowFragments[0])
			case "missing":
				rc.arrowFragments = nil
			case "polygon":
				rc.routes[0].Drawing.Arrow[1][0]++
			case "id":
				svg = strings.Replace(svg, "</svg>", `<g id="conn-arrow-visible-0"/></svg>`, 1)
			case "attachment":
				rc.routes[0].Drawing.Attachments.Target.World.Point = nil
			}
			got := applyConnectorSpatial(svg, rc, prepareConnectorSpatial(cp), len(cp))
			after, _ := json.Marshal(rc.routes[0].Drawing.Coverage)
			v := rc.routes[0].Drawing.Spatial
			if got != svg || string(before) != string(after) || v == nil || v.Mode != "legacy" || v.Arrow != nil || v.SubmittedCollision != nil {
				t.Fatal("failed arrow leaked partial output", v)
			}
		})
	}
}

func TestSpatialCoincidentArrowsRetainOwnDepth(t *testing.T) {
	var routes []ConnectorRoute
	for i, z := range []float64{-100, 100} {
		ax, ay := isoUnproject(20, 100, z)
		bx, by := isoUnproject(180, 100, z)
		d := testArrowDrawing([3]float64{ax, ay, z}, [3]float64{bx, by, z}, 4)
		routes = append(routes, ConnectorRoute{Index: i, Drawing: d})
	}
	if !reflect.DeepEqual(routes[0].Drawing.Arrow, routes[1].Drawing.Arrow) {
		t.Fatal("fixture arrows must coincide exactly")
	}
	p := routes[0].Drawing.Arrow
	poly := fmt.Sprintf(`<polygon points="%.2f,%.2f %.2f,%.2f %.2f,%.2f" fill="#c02020"/>`, p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1])
	label := `<rect x="0" y="0" width="1" height="1"/>`
	rc := &routeCollector{routes: routes, arrowFragments: []connectorArrowFragment{{0, 0, poly}, {1, len(poly) + len(label), poly}}}
	svg := `<svg><g data-layer="connectors"><path data-connector="0"/><path data-connector="1"/></g><g data-layer="connector-overlay">` + poly + label + poly + `</g></svg>`
	// Same owner deliberately covers the lower arrow. Ownership is identity,
	// never an exemption for passing through its visible foreground surface.
	scene := connectorSpatialScene{surfaces: []spatialPiece{{poly: [][2]float64{{140, 60}, {200, 60}, {200, 140}, {140, 140}}, depth: depthPlane{}, owner: "b"}}}
	got := applyConnectorSpatial(svg, rc, scene, 0)
	for i, r := range rc.routes {
		v := r.Drawing.Spatial
		if v == nil || v.Mode != "depth" || v.Arrow == nil {
			t.Fatal("missing arrow result", v)
		}
		visible := spatialOracleContains([2]float64{176, 100}, v.Arrow.ClipRegion)
		if visible != (i == 1) {
			t.Fatalf("arrow %d borrowed another depth", i)
		}
		if strings.Count(got, fmt.Sprintf(`clip-path="url(#conn-arrow-visible-%d)"`, i)) != 1 {
			t.Fatal("arrow clip applied to wrong producer identity")
		}
	}
	if strings.Count(got, label) != 1 {
		t.Fatal("arrow mapping changed intervening label")
	}
}

func TestSpatialArrowRenderObservationParity(t *testing.T) {
	for _, front := range []bool{false, true} {
		t.Run(fmt.Sprint(front), func(t *testing.T) {
			doc := arrowDepthScene(t, front)
			before, _ := json.Marshal(doc)
			rc := &routeCollector{}
			svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
			d := rc.routes[0].Drawing
			if d.Spatial == nil || d.Spatial.Arrow == nil {
				t.Fatal("missing arrow depth")
			}
			p := [2]float64{}
			for _, v := range d.Arrow {
				p[0] += v[0] / 3
				p[1] += v[1] / 3
			}
			p[0] -= d.ProjectionOrigin[0]
			p[1] -= d.ProjectionOrigin[1]
			if spatialOracleContains(p, d.Spatial.Arrow.ClipRegion) == front {
				t.Fatal("near/far arrow visibility reversed")
			}
			for _, c := range []*routeCollector{nil, {preClipOnly: true}, {}} {
				if got := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, c, true); got != svg {
					t.Fatal("observer changed arrow SVG")
				}
			}
			after, _ := json.Marshal(doc)
			if string(before) != string(after) {
				t.Fatal("arrow compositor mutated source")
			}
			if dir := os.Getenv("ISOTOPO_ARROW_EXPORT"); dir != "" {
				name := "behind"
				if front {
					name = "front"
				}
				if err := os.WriteFile(dir+"/"+name+"-after.svg", []byte(svg), 0644); err != nil {
					t.Fatal(err)
				}
				plain, _ := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
				if err := os.WriteFile(dir+"/"+name+"-public.svg", []byte(plain), 0644); err != nil {
					t.Fatal(err)
				}
				b, _ := json.MarshalIndent(rc.routes, "", "  ")
				if err := os.WriteFile(dir+"/"+name+"-routes.json", b, 0644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSpatialRoundCapDepthDoesNotLeakIntoLine(t *testing.T) {
	count := 0
	for _, reverseDepth := range []bool{false, true} {
		line := spatialLine{a: [2]float64{40, 80}, b: [2]float64{200, 120}, da: 0, db: 160, t1: 1}
		if reverseDepth {
			line.da, line.db = line.db, line.da
		}
		for _, surfaceDepth := range []float64{5, 155} {
			face := spatialPiece{poly: [][2]float64{{0, 0}, {250, 0}, {250, 200}, {0, 200}}, depth: depthPlane{0, 0, surfaceDepth}}
			region, ok := spatialVisibleRegion([]spatialLine{line}, []spatialPiece{face}, 12)
			if !ok {
				t.Fatal("unexpected budget")
			}
			dx, dy := line.b[0]-line.a[0], line.b[1]-line.a[1]
			length := math.Hypot(dx, dy)
			for j := -12; j <= 112; j++ {
				for _, across := range []float64{-11.4, -6, 0, 6, 11.4} {
					u := (float64(j) + .317) / 100
					p := [2]float64{line.a[0] + u*dx - across*dy/length, line.a[1] + u*dy + across*dx/length}
					clamped := math.Max(0, math.Min(1, u))
					distance := math.Hypot((u-clamped)*length, across)
					if math.Abs(distance-12) < .05 {
						continue
					}
					depth := (1-clamped)*line.da + clamped*line.db
					want := distance < 12 && depth >= surfaceDepth-.0001
					if got := spatialOracleContains(p, region); got != want {
						t.Fatalf("round cap leaked into line: reverse=%v surface=%g u=%g across=%g got=%v want=%v", reverseDepth, surfaceDepth, u, across, got, want)
					}
					count++
				}
			}
		}
	}
	t.Logf("independent clamped centreline depth samples: %d", count)
}

func TestSpatialArrowHiddenByFrontBody(t *testing.T) {
	doc := arrowDepthScene(t, true)
	rc := &routeCollector{}
	svg := renderCompositeWithVisibility(doc.Scene(), doc.Theme, doc.Canvas, nil, rc, true)
	d := rc.routes[0].Drawing
	if d.Spatial == nil || d.Spatial.Mode != "depth" {
		t.Fatal("fixture must use depth compositor")
	}
	// Inspect the real raster in the middle of the triangle. The known front
	// box covers it; red pixels here expose a floating overlay arrow.
	root, err := findRootSVG(svg)
	if err != nil {
		t.Fatal(err)
	}
	x, y, _, _, err := parseViewBox(extractAttr(root.tag(), "viewBox"))
	if err != nil {
		t.Fatal(err)
	}
	cx, cy := 0., 0.
	for _, p := range d.Arrow {
		cx += p[0] / 3
		cy += p[1] / 3
	}
	im := spatialRaster(t, svg, 4)
	red := 0
	for py := int((cy-y)*4) - 2; py <= int((cy-y)*4)+2; py++ {
		for px := int((cx-x)*4) - 2; px <= int((cx-x)*4)+2; px++ {
			r, g, b, _ := im.At(px, py).RGBA()
			if r > g*2 && r > b*2 {
				red++
			}
		}
	}
	if dir := os.Getenv("ISOTOPO_ARROW_EXPORT"); dir != "" {
		if err := os.WriteFile(dir+"/front.svg", []byte(svg), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if red != 0 {
		t.Fatalf("arrow floats through nearer opaque body: %d red pixels at triangle centre", red)
	}
}
