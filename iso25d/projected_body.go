package iso25d

import (
	"math"
	"sort"
	"strings"
)

// ProjectedBody is the union of closed polygons occupied by a shape's body,
// in projected world-local coordinates (no SVG margin or canvas translation).
// It is docking geometry, not an opacity/visibility verdict: text, strokes,
// glow and shadow do not expand it. Concave faces must not be convexified.
type ProjectedBody struct {
	Polygons [][][2]float64
	// TopAnchor is a world-local point in the rendered content face. Concave
	// shapes may not contain their bounding-box centre.
	TopAnchor *[3]float64
	// MaxError bounds ellipse-to-chord approximation in screen units, before
	// SVG decimal rounding. Polygonal faces have zero approximation error.
	MaxError float64
	// Envelope marks deliberate semantic envelopes, such as an array's box;
	// consumers must not mistake these for exact painted coverage.
	Envelope bool
}

// ShapeBody follows Convert2DTo25D dispatch and its fully resolved options.
// In particular, a registered alias alone does not select a different renderer.
func ShapeBody(shape string, o ConvertOpts) ProjectedBody {
	shape = strings.ToLower(strings.TrimSpace(shape))
	w, d, h := ShapeDimensions(shape, o)
	body := ProjectedBody{}
	fromProvider := func(p ShapeProvider, params map[string]any) {
		for _, f := range p.Faces(w, d, h, params) {
			if !f.Visible || len(f.Points) < 3 {
				continue
			}
			for i := range f.Points {
				f.Points[i][0] -= d * cos30
				f.Points[i][1] -= h
			}
			body.Polygons = append(body.Polygons, f.Points)
		}
	}
	arc := body.arc
	switch shape {
	case "iso_text", "title", "composite":
		return body
	case "circle":
		// World (r,r,r) projects to (0,0), the centre of the rendered disc.
		body.Polygons = append(body.Polygons, arc(0, 0, w/2, w/2, 0, 2*math.Pi))
	case "cylinder", "stored_data", "queue":
		cx, cy := (w-d)/2*cos30, (w+d)/2*sin30-h
		rx, ry := w/2, w/2*sin30/cos30
		pts := arc(cx, cy, rx, ry, math.Pi, math.Pi)
		pts = append(pts, arc(cx, cy+h, rx, ry, 0, math.Pi)...)
		body.Polygons = append(body.Polygons, pts)
	case "person":
		p := DefaultIsoPerson()
		applyPerson(o, &p) // preserve explicit-vs-default head radius
		cx, apex := (w-d)/2*cos30, (w+d)/2*sin30-h
		rx, ry := w/2, w/2*sin30/cos30
		pts := arc(cx, apex+h, rx, h, math.Pi, math.Pi)
		pts = append(pts, arc(cx, apex+h, rx, ry, 0, math.Pi)...)
		body.Polygons = append(body.Polygons, pts,
			arc(cx, apex-.8*p.HeadRadius, p.HeadRadius, p.HeadRadius, 0, 2*math.Pi))
	case "triprism", "hexprism", "octprism":
		n := map[string]int{"triprism": 3, "hexprism": 6, "octprism": 8}[shape]
		fromProvider(PrismShapeProvider{}, map[string]any{"sides": n})
	case "wedge":
		fromProvider(WedgeShapeProvider{}, o.Params)
	case "custom_path":
		// Keep the actual visible faces. The old convex hull filled notches
		// and the old caller discarded params.path before asking the provider.
		fromProvider(CustomPathShapeProvider{}, o.Params)
		if x, y, ok := interiorTopPoint(customPathBase(w, d, o.Params), w/2, d/2); ok {
			body.TopAnchor = &[3]float64{x, y, h}
		}
	case "cloud":
		outline := sampleCloudOutline(w, d)
		top := make([][2]float64, len(outline))
		for i, q := range outline {
			x, y := project(q[0], q[1], h)
			top[i] = [2]float64{x, y}
		}
		body.Polygons = append(body.Polygons, top)
		for i, visible := range cloudFrontEdges(top) {
			if visible {
				a, b := top[i], top[(i+1)%len(top)]
				body.Polygons = append(body.Polygons, [][2]float64{a, b, {b[0], b[1] + h}, {a[0], a[1] + h}})
			}
		}
	case "array1d", "array2d", "array3d", "rack":
		fromProvider(BoxShapeProvider{}, nil)
		body.Envelope = true
	default:
		if o.CornerRadius <= 0 {
			fromProvider(BoxShapeProvider{}, nil)
			break
		}
		// Rounded boxes are convex extrusions of the same sampled outline
		// the renderer emits. Its projected hull is exact for those polygons.
		var pts [][2]float64
		for _, q := range sampleRoundedRectCW(w, d, roundedRadius(w, d, o.CornerRadius)) {
			x, y := project(q[0], q[1], 0)
			pts = append(pts, [2]float64{x, y}, [2]float64{x, y - h})
		}
		body.Polygons = append(body.Polygons, convexHull(pts))
	}
	return body
}

func (body *ProjectedBody) arc(cx, cy, rx, ry, start, sweep float64) [][2]float64 {
	// The affine image of a unit-circle chord has deviation bounded by
	// max(rx, ry) * (1-cos(step/2)). Cap work, and report the actual bound.
	r := math.Max(rx, ry)
	step := math.Pi / 8
	if r > .05 {
		step = math.Min(step, 2*math.Acos(1-.05/r))
	}
	n := int(math.Ceil(math.Abs(sweep) / step))
	n = max(1, min(n, 2048))
	body.MaxError = math.Max(body.MaxError, r*(1-math.Cos(sweep/float64(n)/2)))
	pts := make([][2]float64, n+1)
	for i := range pts {
		a := start + sweep*float64(i)/float64(n)
		pts[i] = [2]float64{cx + rx*math.Cos(a), cy + ry*math.Sin(a)}
	}
	return pts
}

// interiorTopPoint retains the bbox centre when it is inside, otherwise picks
// the midpoint of an actual filled scanline interval. Unlike the legacy
// convex-only content-rectangle search, it handles U/L/C shaped simple paths.
func interiorTopPoint(poly [][2]float64, cx, cy float64) (float64, float64, bool) {
	crossings := func(y float64) []float64 {
		var xs []float64
		for i, a := range poly {
			b := poly[(i+1)%len(poly)]
			if (a[1] > y) != (b[1] > y) {
				xs = append(xs, a[0]+(y-a[1])*(b[0]-a[0])/(b[1]-a[1]))
			}
		}
		sort.Float64s(xs)
		return xs
	}
	xs := crossings(cy)
	for i := 1; i < len(xs); i += 2 {
		if cx > xs[i-1]+1e-9 && cx < xs[i]-1e-9 {
			return cx, cy, true
		}
	}
	ys := make([]float64, len(poly))
	for i, p := range poly {
		ys[i] = p[1]
	}
	sort.Float64s(ys)
	best, x, y := 0.0, 0.0, 0.0
	for j := 1; j < len(ys); j++ {
		if ys[j]-ys[j-1] <= 1e-9 {
			continue
		}
		mid := (ys[j] + ys[j-1]) / 2
		xs := crossings(mid)
		for i := 1; i < len(xs); i += 2 {
			if score := (xs[i] - xs[i-1]) * (ys[j] - ys[j-1]); score > best {
				best, x, y = score, (xs[i]+xs[i-1])/2, mid
			}
		}
	}
	return x, y, best > 0
}

// cloudFrontEdges mirrors the renderer's current side selection, including its
// centroid convention. This is not a general concave-solid visibility solver.
func cloudFrontEdges(top [][2]float64) []bool {
	var cx, cy float64
	for _, p := range top {
		cx += p[0]
		cy += p[1]
	}
	cx /= float64(len(top))
	cy /= float64(len(top))
	visible := make([]bool, len(top))
	for i, a := range top {
		b := top[(i+1)%len(top)]
		nx, ny := b[1]-a[1], a[0]-b[0]
		if ((a[0]+b[0])/2-cx)*nx+((a[1]+b[1])/2-cy)*ny < 0 {
			ny = -ny
		}
		visible[i] = ny > 0
	}
	return visible
}
