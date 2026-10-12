package isotopo

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func TestRegionClippingRetainsConcaveGaps(t *testing.T) {
	u := [][2]float64{{0, 0}, {10, 0}, {10, 10}, {7, 10}, {7, 3}, {3, 3}, {3, 10}, {0, 10}}
	for _, tc := range []struct {
		name   string
		region screenRegion
		pts    [][2]float64
		want   float64
	}{
		{"both vertices inside", screenRegion{u}, [][2]float64{{1, 8}, {9, 8}}, 7},
		{"overlapping faces", screenRegion{{{0, 0}, {8, 0}, {8, 10}, {0, 10}}, {{4, 0}, {12, 0}, {12, 10}, {4, 10}}}, [][2]float64{{-5, 8}, {10, 8}}, 0},
		{"last lobe", screenRegion{u, {{6, 4}, {12, 4}, {12, 10}, {6, 10}}}, [][2]float64{{-5, 8}, {10, 8}}, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clipRouteEndRegion(tc.pts, tc.region, 0)
			if math.Abs(got[len(got)-1][0]-tc.want) > 1e-8 {
				t.Fatalf("got %v, want final x=%g", got, tc.want)
			}
			a, b := tc.pts[0], tc.pts[1]
			q := [][2]float64{a, {(a[0] + b[0]) / 2, (a[1] + b[1]) / 2}, b}
			curve := clipQuadraticRegions(q, nil, tc.region, 0, 0)
			if math.Abs(curve[2][0]-tc.want) > 1e-8 {
				t.Fatal("quadratic union differs", curve)
			}
			rev := [][2]float64{b, a}
			start := clipRouteStartRegion(rev, tc.region, 0)
			if math.Abs(start[0][0]-tc.want) > 1e-8 {
				t.Fatal("reverse endpoint differs", start)
			}
		})
	}
}

func TestRegionQuadraticBoundaryIndependentOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(731))
	type rect struct{ x0, y0, x1, y1 float64 }
	for k := 0; k < 80; k++ {
		rects := []rect{{-20, -15, 20, 15}, {12, -25, 42, 25}, {-8, 22, 10, 40}}
		region := screenRegion{}
		for _, r := range rects {
			region = append(region, [][2]float64{{r.x0, r.y0}, {r.x1, r.y0}, {r.x1, r.y1}, {r.x0, r.y1}})
		}
		inside := func(p [2]float64) bool {
			for _, r := range rects {
				if p[0] >= r.x0 && p[0] <= r.x1 && p[1] >= r.y0 && p[1] <= r.y1 {
					return true
				}
			}
			return false
		}
		q := quadratic{{rng.Float64()*140 - 70, rng.Float64()*140 - 70}, {rng.Float64()*180 - 90, rng.Float64()*180 - 90}, {rng.Float64()*30 - 15, rng.Float64()*20 - 10}}
		// Dense independent occupancy oracle with binary refinement of the last
		// outside interval; axis-aligned comparisons do not use polygonContains.
		want, found := 1.0, false
		const n = 16384
		for i := n - 1; i >= 0; i-- {
			lo, hi := float64(i)/n, float64(i+1)/n
			if !inside(q.at(lo)) {
				for j := 0; j < 40; j++ {
					mid := (lo + hi) / 2
					if inside(q.at(mid)) {
						hi = mid
					} else {
						lo = mid
					}
				}
				want, found = hi, true
				break
			}
		}
		got, ok := q.endpointBoundaryRegion(region, false)
		if ok != found || (ok && math.Abs(got-want) > 1e-7) {
			t.Fatalf("case %d: (%g,%v), oracle (%g,%v)", k, got, ok, want, found)
		}
	}
}

func TestCardinalAnchorsDockToResolvedBody(t *testing.T) {
	for _, shape := range []string{"circle", "cylinder", "person", "cloud", "custom_path"} {
		for _, height := range []float64{20, 110} {
			o := iso25d.ConvertOpts{Width: 180, Depth: 120, Height: height, Params: map[string]any{"path": "M 0 0 L 180 0 L 180 40 L 40 40 L 40 120 L 0 120 Z"}}
			body := iso25d.ShapeBody(shape, o)
			w, d, h := iso25d.ShapeDimensions(shape, o)
			p := partInfo{id: "p", shape: shape, w: w, d: d, h: h, offWX: 200, offWY: -90, offWZ: 37, body: &body}
			ar := newAnchorResolver([]partInfo{p}, 0, 0)
			region := partBodyRegion(p, 0, 0)
			for _, side := range []string{"left", "right", "front", "back"} {
				ref := "p." + side
				wx, wy, z, _ := ar.world(ref)
				wx, wy = ar.refineSilhouette(ref, wx, wy, z)
				x, y := projectIso(wx, wy, z)
				pt := [2]float64{x, y}
				dist := math.Inf(1)
				for _, poly := range region {
					for i, a := range poly {
						b := poly[(i+1)%len(poly)]
						dx, dy := b[0]-a[0], b[1]-a[1]
						den := dx*dx + dy*dy
						if den == 0 {
							continue
						}
						u := math.Max(0, math.Min(1, ((x-a[0])*dx+(y-a[1])*dy)/den))
						dist = math.Min(dist, math.Hypot(x-a[0]-u*dx, y-a[1]-u*dy))
					}
				}
				if dist > 1e-6 || !region.containsEndpoint(pt) {
					t.Errorf("%s h=%g %s anchor %v is %g from body", shape, height, side, pt, dist)
				}
			}
		}
	}
}

func TestRenderedSphereConnectorEndsAtActualDisc(t *testing.T) {
	// This reads the actual SVG circle, group transform and emitted route;
	// it does not use ShapeBody or the anchor resolver as the expected result.
	for _, routing := range []string{"straight", "orthogonal", "bezier"} {
		for _, pos := range [][2]float64{{-350, 0}, {350, 0}, {0, -350}, {0, 350}} {
			src := fmt.Sprintf(`canvas: {aspectRatio: -1}
nodes:
  scene:
    shape: composite
    parts:
      - {id: source, shape: rectangle, geom: {w: 60, d: 60, h: 30}, offset: {wx: %g, wy: %g}}
      - {id: target, shape: circle, geom: {w: 140}, offset: {wx: 0, wy: 0}}
    connectors:
      - {from: source, to: target, routing: %s, arrow: triangle}
`, pos[0], pos[1], routing)
			doc, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
			group := regexp.MustCompile(`<g data-part="\d+" data-part-id="target" transform="translate\(([-\d.]+) ([-\d.]+)\)">`).FindStringSubmatch(svg)
			circle := regexp.MustCompile(`<circle data-face="sphere" cx="([\d.]+)" cy="([\d.]+)" r="([\d.]+)"`).FindStringSubmatch(svg)
			route := regexp.MustCompile(`<path data-connector="0"[^>]* d="([^"]+)"`).FindStringSubmatch(svg)
			if group == nil || circle == nil || route == nil {
				t.Fatal("missing rendered geometry")
			}
			nums := regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)`).FindAllString(route[1], -1)
			f := func(s string) float64 {
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			x, y := f(nums[len(nums)-2]), f(nums[len(nums)-1])
			cx, cy, r := f(group[1])+f(circle[1]), f(group[2])+f(circle[2]), f(circle[3])
			gap := math.Hypot(x-cx, y-cy) - r
			if gap < -.1 || gap > 2.1 {
				t.Errorf("%s from %v: arrow tip gap from actual disc = %.3f px (%s)", routing, pos, gap, route[1])
			}
			if strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
				t.Fatal("non-finite SVG")
			}
		}
	}
}

func TestRenderedConcaveConnectorEndsOnFilledFace(t *testing.T) {
	for _, routing := range []string{"straight", "bezier", "orthogonal", "orthogonal2"} {
		for _, pos := range [][2]float64{{-350, 0}, {350, 0}, {0, -350}, {0, 350}} {
			src := fmt.Sprintf(`nodes:
  scene:
    shape: composite
    parts:
      - {id: source, shape: rectangle, geom: {w: 60, d: 60, h: 30}, offset: {wx: %g, wy: %g}}
      - {id: target, shape: custom_path, geom: {w: 180, d: 180, h: 14, params: {path: "M 0 0 L 180 0 L 180 45 L 45 45 L 45 180 L 0 180 Z"}}, offset: {wx: 0, wy: 0}}
    connectors:
      - {from: source, to: target, routing: %s, arrow: triangle}
`, pos[0], pos[1], routing)
			doc, err := Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			svg := RenderWithCanvas(doc.Scene(), nil, nil, nil)
			group := regexp.MustCompile(`<g data-part="\d+" data-part-id="target" transform="translate\(([-\d.]+) ([-\d.]+)\)">`).FindStringSubmatch(svg)
			route := regexp.MustCompile(`<path data-connector="0"[^>]* d="([^"]+)"`).FindStringSubmatch(svg)
			if group == nil || route == nil {
				t.Fatal("missing target/route")
			}
			start := strings.Index(svg, group[0])
			end, ok := partGroupEnd(svg, start)
			if !ok {
				t.Fatal("missing group end")
			}
			nums := regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)`).FindAllString(route[1], -1)
			pt := [2]float64{atof(nums[len(nums)-2]) - atof(group[1]), atof(nums[len(nums)-1]) - atof(group[2])}
			distance := math.Inf(1)
			for _, match := range regexp.MustCompile(`<polygon data-face="[^"]+" points="([^"]+)"`).FindAllStringSubmatch(svg[start:end], -1) {
				var poly [][2]float64
				for _, pair := range strings.Fields(match[1]) {
					var x, y float64
					fmt.Sscanf(pair, "%f,%f", &x, &y)
					poly = append(poly, [2]float64{x, y})
				}
				for i, a := range poly {
					b := poly[(i+1)%len(poly)]
					dx, dy := b[0]-a[0], b[1]-a[1]
					den := dx*dx + dy*dy
					if den == 0 {
						continue
					}
					u := math.Max(0, math.Min(1, ((pt[0]-a[0])*dx+(pt[1]-a[1])*dy)/den))
					distance = math.Min(distance, math.Hypot(pt[0]-a[0]-u*dx, pt[1]-a[1]-u*dy))
				}
			}
			if distance > 2.1 {
				t.Errorf("%s from %v: endpoint %v floats %g px from all actual faces", routing, pos, pt, distance)
			}
		}
	}
}
