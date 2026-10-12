package iso25d

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mazznoer/csscolorparser"
)

// ProjectedPaint is a lower bound on filled coverage, not docking geometry.
// Polygons cover faces whose fill alpha is everywhere at least minAlpha after
// the renderer's group opacity. Uncertain means some coverage was omitted:
// variable-alpha fills, patterns, blur, or a semantic rather than exact body.
// Strokes, icons, text, filters and overlapping translucent faces are not
// accumulated into solid coverage. Coordinates/curve error match ShapeBody.
type ProjectedPaint struct {
	ProjectedBody
	Uncertain bool
}

type alphaRange struct{ lo, hi float64 }

func fillAlpha(s string) alphaRange {
	if strings.EqualFold(strings.TrimSpace(s), "none") {
		return alphaRange{}
	}
	c, err := csscolorparser.Parse(s)
	if err != nil {
		return alphaRange{0, 1}
	}
	return alphaRange{c.A, c.A}
}

func stopAlpha(colors ...string) alphaRange {
	if len(colors) == 0 {
		return alphaRange{}
	}
	a := alphaRange{1, 0}
	for _, s := range colors {
		// 'none' is valid as a fill but not as stop-color. SVG falls back to the
		// initial stop color (black); don't misinterpret it as a transparent stop.
		c := fillAlpha(s)
		if strings.EqualFold(strings.TrimSpace(s), "none") {
			c = alphaRange{0, 1}
		}
		a.lo, a.hi = math.Min(a.lo, c.lo), math.Max(a.hi, c.hi)
	}
	return a
}
func gradientAlpha(fill string, g *FaceGradient) alphaRange {
	if g != nil {
		return stopAlpha(g.From, g.To)
	}
	return fillAlpha(fill)
}
func overrideAlpha(base alphaRange, surfaces map[string]*FaceSurface, name string) alphaRange {
	fs := surfaceFor(surfaces, name)
	if fs == nil || fs.Fill == nil {
		return base
	}
	f := fs.Fill
	switch f.Kind {
	case "", "solid":
		if f.Color != "" {
			return fillAlpha(f.Color)
		}
	case "linearGradient", "radialGradient":
		colors := make([]string, len(f.Stops))
		for i, s := range f.Stops {
			colors[i] = s.Color
		}
		return stopAlpha(colors...)
	case "pattern":
		if f.Pattern != nil && strings.TrimSpace(f.Pattern.Color) != "" {
			switch strings.ToLower(strings.TrimSpace(f.Pattern.Kind)) {
			case "hatch", "dots":
				return alphaRange{0, fillAlpha(f.Pattern.Color).hi}
			}
		}
	}
	return base // unsupported/empty overrides emit no replacement fill
}

// ShapePaint follows the *actual* fill precedence in Convert2DTo25D. In
// particular Wireframe and faces are currently implemented only by selected
// renderers; intent alone must never erase pixels that are still emitted.
func ShapePaint(shape string, o ConvertOpts, minAlpha float64) ProjectedPaint {
	shape = strings.ToLower(strings.TrimSpace(shape))
	out := ProjectedPaint{}
	opacity := 1.0
	// Preserve current renderer semantics: 0 is an unset sentinel, not an
	// explicit transparent group. A presence-aware opacity API is separate work.
	if o.Opacity > 0 && o.Opacity < 1 {
		opacity, _ = strconv.ParseFloat(fmt.Sprintf("%.3f", o.Opacity), 64)
	}
	grain := o.GrainIntensity > 0 && shape != "circle" && shape != "person" && shape != "cloud"
	add := func(poly [][2]float64, a alphaRange) {
		// feBlend followed by source-in can lower partially transparent alpha;
		// a² is a safe lower bound. Opaque source faces remain opaque.
		if grain {
			a.lo *= a.lo
		}

		if a.lo*opacity >= minAlpha && len(poly) >= 3 {
			out.Polygons = append(out.Polygons, poly)
		} else if a.hi*opacity >= minAlpha {
			out.Uncertain = true
		}
	}
	if o.Blur > 0 && shape != "circle" {
		out.Uncertain = true
		return out
	}
	w, d, h := ShapeDimensions(shape, o)
	b := DefaultIsoBox()
	applyBox(o, &b)
	top, left, right := gradientAlpha(b.TopFill, b.TopGradient), gradientAlpha(b.LeftFill, b.LeftGradient), gradientAlpha(b.RightFill, b.RightGradient)
	provider := func(p ShapeProvider, params map[string]any) {
		for _, f := range p.Faces(w, d, h, params) {
			if !f.Visible {
				continue
			}
			a := top
			if f.Name != "top" && f.Name != "slope" {
				if f.Normal[0] >= f.Normal[1] {
					a = right
				} else {
					a = left
				}
			}
			a = overrideAlpha(a, b.FaceSurfaces, f.Name)
			for i := range f.Points {
				f.Points[i][0] -= d * cos30
				f.Points[i][1] -= h
			}
			add(f.Points, a)
		}
	}
	arc := out.ProjectedBody.arc
	switch shape {
	case "iso_text", "title", "composite":
		return out
	case "array1d", "array2d", "array3d", "rack":
		out.Envelope = true
		out.Uncertain = true
		return out
	case "circle":
		s := DefaultIsoSphere()
		applySphere(o, &s)
		add(arc(0, 0, w/2, w/2, 0, 2*math.Pi), stopAlpha(s.Highlight, s.Shadow))
	case "person":
		p := DefaultIsoPerson()
		applyPerson(o, &p)
		cx, cy := (w-d)/2*cos30, (w+d)/2*sin30
		rx, ry := w/2, w/2*sin30/cos30
		l := arc(cx, cy, rx, h, 1.5*math.Pi, -math.Pi/2)
		l = append(l, arc(cx, cy, rx, ry, math.Pi, -math.Pi/2)...)
		r := arc(cx, cy, rx, h, 1.5*math.Pi, math.Pi/2)
		r = append(r, arc(cx, cy, rx, ry, 0, math.Pi/2)...)
		add(l, gradientAlpha(p.BodyLeft, p.BodyLeftGradient))
		add(r, gradientAlpha(p.BodyRight, p.BodyRightGradient))
		add(arc(cx, cy-h-.8*p.HeadRadius, p.HeadRadius, p.HeadRadius, 0, 2*math.Pi), stopAlpha(p.HeadHighlight, p.HeadShadow))
	case "cylinder", "stored_data", "queue":
		c := DefaultIsoCylinder()
		applyCylinder(o, &c)
		cx, cy := (w-d)/2*cos30, (w+d)/2*sin30-h
		rx, ry := w/2, w/2*sin30/cos30
		la := overrideAlpha(fillAlpha(c.LeftFill), c.FaceSurfaces, "left")
		ra := overrideAlpha(fillAlpha(c.RightFill), c.FaceSurfaces, "right")
		if c.LeftGradient != nil && strings.TrimSpace(c.LeftGradient.From) != "" {
			la = gradientAlpha("", c.LeftGradient)
		}
		if c.RightGradient != nil && strings.TrimSpace(c.RightGradient.From) != "" {
			ra = gradientAlpha("", c.RightGradient)
		}
		l := arc(cx, cy, rx, ry, math.Pi/2, math.Pi/2)
		l = append(l, arc(cx, cy+h, rx, ry, math.Pi, -math.Pi/2)...)
		r := arc(cx, cy, rx, ry, math.Pi/2, -math.Pi/2)
		r = append(r, arc(cx, cy+h, rx, ry, 0, math.Pi/2)...)
		add(l, la)
		add(r, ra)
		add(arc(cx, cy, rx, ry, 0, 2*math.Pi), overrideAlpha(fillAlpha(c.TopFill), c.FaceSurfaces, "top"))
	case "triprism", "hexprism", "octprism":
		provider(PrismShapeProvider{}, map[string]any{"sides": map[string]int{"triprism": 3, "hexprism": 6, "octprism": 8}[shape]})
	case "wedge":
		provider(WedgeShapeProvider{}, o.Params)
	case "custom_path":
		if !simplePaintPolygon(customPathBase(w, d, o.Params)) {
			out.Uncertain = true
			return out
		}
		provider(CustomPathShapeProvider{}, o.Params)
	case "cloud":
		body := ShapeBody(shape, o)
		out.MaxError = body.MaxError
		for i, p := range body.Polygons {
			a := left
			if i == 0 {
				a = top
			}
			add(p, a)
		}
	default:
		if b.CornerRadius <= 0 {
			if b.Wireframe {
				top, left, right = alphaRange{}, alphaRange{}, alphaRange{}
			}
			provider(BoxShapeProvider{}, nil)
		} else {
			if b.Wireframe {
				return out
			}
			pts, front := roundedBodyGeometry(w, d, h, b.CornerRadius)
			poly := make([][2]float64, len(pts))
			for i, p := range pts {
				poly[i] = [2]float64{p.topX, p.topY}
			}
			ta := fillAlpha(b.TopFill)
			if b.TopGradient != nil && strings.TrimSpace(b.TopGradient.From) != "" {
				ta = gradientAlpha("", b.TopGradient)
			}
			add(poly, overrideAlpha(ta, b.FaceSurfaces, "top"))
			ts, bs := b.RightFill, b.LeftFill
			if b.RightGradient != nil && strings.TrimSpace(b.RightGradient.From) != "" {
				ts = b.RightGradient.From
			}
			if b.LeftGradient != nil && strings.TrimSpace(b.LeftGradient.To) != "" {
				bs = b.LeftGradient.To
			}
			if ts == "" {
				ts = bs
			}
			if bs == "" {
				bs = ts
			}
			side := fillAlpha(b.LeftFill)
			if bs != "" && !strings.EqualFold(ts, "none") && !strings.EqualFold(bs, "none") {
				side = stopAlpha(ts, bs)
			}
			for _, alias := range []string{"side", "left", "right"} {
				if fs := surfaceFor(b.FaceSurfaces, alias); fs != nil && fs.Fill != nil {
					side = overrideAlpha(side, b.FaceSurfaces, alias)
					break
				}
			}
			band := func(ids []int) [][2]float64 {
				p := make([][2]float64, 0, 2*len(ids))
				for _, i := range ids {
					p = append(p, [2]float64{pts[i].topX, pts[i].topY})
				}
				for i := len(ids) - 1; i >= 0; i-- {
					v := pts[ids[i]]
					p = append(p, [2]float64{v.botX, v.botY})
				}
				return p
			}
			k := frontCornerSplit(pts, front)
			if b.FaceSplit && k > 0 && k < len(front)-1 {
				la, ra := fillAlpha(b.LeftFill), fillAlpha(b.RightFill)
				if b.LeftFill == "" {
					la = side
				}
				if b.RightFill == "" {
					ra = side
				}
				if b.LeftGradient != nil && strings.TrimSpace(b.LeftGradient.From) != "" {
					la = gradientAlpha("", b.LeftGradient)
				}
				if b.RightGradient != nil && strings.TrimSpace(b.RightGradient.From) != "" {
					ra = gradientAlpha("", b.RightGradient)
				}
				lp, rp := band(front[:k+1]), band(front[k:])
				if avgTopX(pts, front, 0, k) > avgTopX(pts, front, k, len(front)-1) {
					lp, rp = rp, lp
				}
				add(lp, la)
				add(rp, ra)
			} else {
				add(band(front), side)
			}
		}
	}
	return out
}

// The area kernel assumes simple faces. Self-intersections and collapsed paths
// must not quietly switch the SVG's nonzero fill rule to an even/odd region.
func simplePaintPolygon(p [][2]float64) bool {
	if len(p) < 3 || len(p) > 2048 {
		return false
	}
	side := func(a, b, c [2]float64) float64 { return (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0]) }
	for i, a := range p {
		b := p[(i+1)%len(p)]
		if a == b || math.IsNaN(a[0]) || math.IsNaN(a[1]) || math.IsInf(a[0], 0) || math.IsInf(a[1], 0) {
			return false
		}
		for j := i + 1; j < len(p); j++ {
			if j == i+1 || (i == 0 && j == len(p)-1) {
				continue
			}
			c, d := p[j], p[(j+1)%len(p)]
			if math.Max(a[0], b[0]) < math.Min(c[0], d[0]) || math.Max(c[0], d[0]) < math.Min(a[0], b[0]) || math.Max(a[1], b[1]) < math.Min(c[1], d[1]) || math.Max(c[1], d[1]) < math.Min(a[1], b[1]) {
				continue
			}
			s1, s2, s3, s4 := side(a, b, c), side(a, b, d), side(c, d, a), side(c, d, b)
			if s1*s2 <= 0 && s3*s4 <= 0 {
				return false
			}
		}
	}
	return true
}
