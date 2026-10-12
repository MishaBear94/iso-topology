package isotopo

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"
)

const curveConnectorFixture = `
canvas: {background: "#f8fafc", grid: none, aspectRatio: -1}
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, label: Source, geom: {w: 120, d: 100, h: 40}, offset: {wx: 0, wy: 0}}
      - {id: b, label: Target, geom: {w: 120, d: 100, h: 40}, offset: {wx: 400, wy: 180}}
    connectors:
      - {from: a, to: b, routing: bezier, arrow: triangle, label: flow}
`

// This checks the emitted SVG against the original anchor curve, not against
// the clipping helper. A quadratic's control point is not a route waypoint:
// clipping its control polygon changes both its locus and endpoint tangent.
func TestBezierEndpointClippingPreservesCurve(t *testing.T) {
	doc := parseWSH(t, curveConnectorFixture)
	svg, routes := RenderWithRouteInfo(doc.Scene(), doc.Theme, doc.Canvas, nil)
	m := regexp.MustCompile(`<path data-connector="0"[^>]* d="([^"]+)"`).FindStringSubmatch(svg)
	if len(m) != 2 || len(routes) != 1 {
		t.Fatal("missing rendered curve or logical route")
	}
	var q [3][2]float64
	if n, err := fmt.Sscanf(m[1], "M %f,%f Q %f,%f %f,%f", &q[0][0], &q[0][1], &q[1][0], &q[1][1], &q[2][0], &q[2][1]); n != 6 || err != nil {
		t.Fatalf("clipped curve must remain a quadratic, got %q (%v)", m[1], err)
	}
	r := routes[0].Screen
	p0, mid, p2 := r[0], r[len(r)/2], r[len(r)-1]
	control := [2]float64{2*mid[0] - (p0[0]+p2[0])/2, 2*mid[1] - (p0[1]+p2[1])/2}
	dx, dy := p2[0]-p0[0], p2[1]-p0[1]
	// The original control is on the perpendicular bisector, so projection
	// along the anchor chord recovers its curve parameter independently.
	distanceToOriginal := func(p [2]float64) float64 {
		u := ((p[0]-p0[0])*dx + (p[1]-p0[1])*dy) / (dx*dx + dy*dy)
		v := 1 - u
		x := v*v*p0[0] + 2*u*v*control[0] + u*u*p2[0]
		y := v*v*p0[1] + 2*u*v*control[1] + u*u*p2[1]
		return math.Hypot(x-p[0], y-p[1])
	}
	for i := 0; i <= 20; i++ {
		u := float64(i) / 20
		v := 1 - u
		p := [2]float64{v*v*q[0][0] + 2*u*v*q[1][0] + u*u*q[2][0], v*v*q[0][1] + 2*u*v*q[1][1] + u*u*q[2][1]}
		if d := distanceToOriginal(p); d > .03 {
			t.Errorf("sample %d deviates %.3f px from the anchor curve after clipping", i, d)
		}
	}
	label := regexp.MustCompile(`<text x="([^"]+)" y="([^"]+)"[^>]*>flow</text>`).FindStringSubmatch(svg)
	if len(label) != 3 {
		t.Fatal("missing connector label")
	}
	var lp [2]float64
	fmt.Sscanf(label[1]+" "+label[2], "%f %f", &lp[0], &lp[1])
	if d := distanceToOriginal(lp); d > .25 {
		t.Errorf("label sits %.3f px away from the actual curve", d)
	}
	arrow := regexp.MustCompile(`(?s)data-layer="connector-overlay".*?<polygon points="([^"]+)"`).FindStringSubmatch(svg)
	if len(arrow) != 2 {
		t.Fatal("missing curve arrow")
	}
	var tri [3][2]float64
	fmt.Sscanf(arrow[1], "%f,%f %f,%f %f,%f", &tri[0][0], &tri[0][1], &tri[1][0], &tri[1][1], &tri[2][0], &tri[2][1])
	if d := math.Hypot(tri[0][0]-q[2][0], tri[0][1]-q[2][1]); d > .02 {
		t.Fatalf("arrow detached from curve by %.3f px", d)
	}
	ax, ay := tri[0][0]-(tri[1][0]+tri[2][0])/2, tri[0][1]-(tri[1][1]+tri[2][1])/2
	tx, ty := q[2][0]-q[1][0], q[2][1]-q[1][1]
	if cosine := (ax*tx + ay*ty) / (math.Hypot(ax, ay) * math.Hypot(tx, ty)); cosine < .9999 {
		t.Fatalf("arrow does not follow the curve tangent: cosine %.6f", cosine)
	}
}

func TestQuadraticEndpointBoundaries(t *testing.T) {
	box := func(x0, y0, x1, y1 float64) [][2]float64 {
		return [][2]float64{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
	}
	tests := []struct {
		name  string
		q     quadratic
		poly  [][2]float64
		start bool
		want  float64
		clip  bool
	}{
		{"roof exit", quadratic{{0, 0}, {100, 160}, {200, 0}}, box(-5, -5, 20, 80), true, .1, true},
		{"roof entry", quadratic{{0, 0}, {100, 160}, {200, 0}}, box(180, -5, 205, 80), false, .9, true},
		{"last entry after re-exit", quadratic{{-2, 0}, {2, 0}, {0, 0}}, box(-.5, -1, .5, 1), false, 5.0 / 6, true},
		{"interior tangent is not an exit", quadratic{{-2, 1}, {0, -1}, {2, 1}}, box(-3, 0, 3, 2), false, 1, false},
		{"outside endpoint", quadratic{{0, 0}, {50, 50}, {100, 0}}, box(110, -5, 120, 20), false, 1, false},
		{"endpoint on edge", quadratic{{0, 0}, {50, 50}, {100, 0}}, box(100, -5, 120, 20), false, 1, true},
		{"coincident chord", quadratic{{0, 0}, {50, 0}, {100, 0}}, box(80, 0, 120, 20), false, .8, true},
		{"zero-length curve", quadratic{{0, 0}, {0, 0}, {0, 0}}, box(-5, -5, 5, 5), false, 1, false},
		{"tiny exposed span", quadratic{{0, 0}, {50, 30}, {100, 0}}, box(.000001, -100, 120, 100), false, .00000001, true},
		{"concave outline last entry", quadratic{{0, 0}, {5, 0}, {9, 0}}, [][2]float64{{2, -2}, {10, -2}, {10, 2}, {8, 2}, {8, -1}, {4, -1}, {4, 2}, {2, 2}}, false, 5 - math.Sqrt(17), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, clip := tc.q.endpointBoundary(tc.poly, tc.start)
			if clip != tc.clip || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("boundary = %.12f, clip = %v; want %.12f, %v", got, clip, tc.want, tc.clip)
			}
			// Reversing polygon winding must not change an attachment.
			reversed := make([][2]float64, len(tc.poly))
			for i, p := range tc.poly {
				reversed[len(reversed)-1-i] = p
			}
			if again, ok := tc.q.endpointBoundary(reversed, tc.start); ok != clip || math.Abs(again-got) > 1e-10 {
				t.Fatal("polygon winding changed the attachment")
			}
		})
	}
}

func TestBezierClippingRetainsGradientAndDash(t *testing.T) {
	src := strings.Replace(curveConnectorFixture, "label: flow", `label: flow, stroke: {width: 3, dash: "8 5", gradient: {from: "#2563eb", to: "#9333ea"}}`, 1)
	doc := parseWSH(t, src)
	svg := RenderWithCanvas(doc.Scene(), doc.Theme, doc.Canvas, nil)
	path := regexp.MustCompile(`<path data-connector="0"[^>]*>`).FindString(svg)
	if !strings.Contains(path, `stroke="url(#conn-grad-0)"`) || !strings.Contains(path, `stroke-dasharray="12 7.5"`) {
		t.Fatalf("curve lost its gradient or width-scaled dash: %s", path)
	}
	m := regexp.MustCompile(` d="([^"]+)"`).FindStringSubmatch(path)
	var q [3][2]float64
	fmt.Sscanf(m[1], "M %f,%f Q %f,%f %f,%f", &q[0][0], &q[0][1], &q[1][0], &q[1][1], &q[2][0], &q[2][1])
	gradient := regexp.MustCompile(`<linearGradient id="conn-grad-0"[^>]*>`).FindString(svg)
	for name, want := range map[string]float64{"x1": q[0][0], "y1": q[0][1], "x2": q[2][0], "y2": q[2][1]} {
		if !strings.Contains(gradient, fmt.Sprintf(`%s="%.2f"`, name, want)) {
			t.Errorf("gradient's %s is detached from the clipped curve: %s", name, gradient)
		}
	}
	if !regexp.MustCompile(`<polygon points="[^"]+" fill="url\(#conn-grad-0\)"`).MatchString(svg) {
		t.Fatal("arrow lost the connector's gradient")
	}
}

// Independent oracle: scan explicit x/y box membership, then bisect the last
// crossing. It uses neither polynomial root solving nor polygon membership.
func TestQuadraticBoundaryAgainstSampledBoxOracle(t *testing.T) {
	for i := 0; i < 120; i++ {
		q := quadratic{{-250 + float64(i%5)*10, float64(i%9)*30 - 100}, {float64(i%7)*70 - 160, float64(i%11)*45 - 230}, {10, 15}}
		inside := func(t float64) bool {
			u := 1 - t
			x := u*u*q[0][0] + 2*u*t*q[1][0] + t*t*q[2][0]
			y := u*u*q[0][1] + 2*u*t*q[1][1] + t*t*q[2][1]
			return x >= -40 && x <= 60 && y >= -35 && y <= 65
		}
		hi := 1.0
		lo := 0.0
		for k := 4095; k >= 0; k-- {
			lo = float64(k) / 4096
			if !inside(lo) {
				break
			}
			hi = lo
		}
		for k := 0; k < 50; k++ {
			mid := (lo + hi) / 2
			if inside(mid) {
				hi = mid
			} else {
				lo = mid
			}
		}
		poly := [][2]float64{{-40, -35}, {60, -35}, {60, 65}, {-40, 65}}
		got, ok := q.endpointBoundary(poly, false)
		if !ok || math.Abs(got-hi) > 1e-9 {
			t.Fatalf("case %d: analytic boundary %.12f disagrees with sampled oracle %.12f", i, got, hi)
		}
	}
}

func TestQuadraticClipPreservesDirectionAndShortSpans(t *testing.T) {
	q := [][2]float64{{0, 0}, {100, 160}, {200, 0}}
	source := [][2]float64{{-5, -5}, {20, -5}, {20, 80}, {-5, 80}}
	target := [][2]float64{{180, -5}, {205, -5}, {205, 80}, {180, 80}}
	forward := clipQuadraticEndpoints(q, source, target, 1, 2)
	reverse := clipQuadraticEndpoints([][2]float64{q[2], q[1], q[0]}, target, source, 2, 1)
	for i := range forward {
		if math.Hypot(forward[i][0]-reverse[2-i][0], forward[i][1]-reverse[2-i][1]) > 1e-8 {
			t.Fatal("reversing a route changed its curve or asymmetric endpoint gaps")
		}
	}
	for _, p := range [][2]float64{forward[0], forward[2]} {
		if polygonContains(p, source) || polygonContains(p, target) {
			t.Fatal("clipped endpoint did not clear its silhouette")
		}
	}
	// Less than the requested total gap: keep a short correctly oriented Q,
	// rather than crossing the endpoints or silently deleting the connector.
	tiny := [][2]float64{{19, 0}, {20, 0}, {21, 0}}
	out := clipQuadraticEndpoints(tiny, source, nil, 10, 10)
	if len(out) != 3 || out[0][0] >= out[2][0] || math.Abs(out[0][0]-20) > 1e-9 {
		t.Fatalf("short exposed span lost or reversed: %v", out)
	}
}

func TestQuadraticSamplingBoundsActualCurve(t *testing.T) {
	for _, q := range []quadratic{
		{{0, 0}, {100, 160}, {200, 0}},
		{{0, 0}, {100000, 300000}, {-10000, 0}},
		{{0, 0}, {100, 0}, {0, 0}}, // collinear reversal still needs subdivision
	} {
		ss := q.samples(.1)
		for i := 1; i < len(ss); i++ {
			for k := 1; k < 10; k++ {
				u := float64(k) / 10
				p := q.at(ss[i-1].t + u*(ss[i].t-ss[i-1].t))
				a, b := ss[i-1].p, ss[i].p
				dx, dy := b[0]-a[0], b[1]-a[1]
				v := 0.0
				if den := dx*dx + dy*dy; den > 0 {
					v = math.Max(0, math.Min(1, ((p[0]-a[0])*dx+(p[1]-a[1])*dy)/den))
				}
				if distance := math.Hypot(p[0]-a[0]-v*dx, p[1]-a[1]-v*dy); distance > .100001 {
					t.Fatalf("sampled route misses the real curve by %.6f px", distance)
				}
			}
		}
	}
}

func BenchmarkBezierEndpointClipping(b *testing.B) {
	pts := [][2]float64{{0, 0}, {100, 160}, {200, 0}}
	source := [][2]float64{{-5, -5}, {20, -5}, {20, 80}, {-5, 80}}
	target := [][2]float64{{180, -5}, {205, -5}, {205, 80}, {180, 80}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if got := clipQuadraticEndpoints(pts, source, target, 1, 2); len(got) != 3 {
			b.Fatal("lost curve")
		}
	}
}
