package iso25d

import (
	"math"
	"strings"
)

// ShapeDimensions resolves the dimensions actually used by Convert2DTo25D.
// Keep the original options for rendering: materialising default dimensions
// into options can change other defaults (for example a person's head size).
// Text has no solid footprint; its SVG extent is handled separately.
func ShapeDimensions(shape string, o ConvertOpts) (w, d, h float64) {
	shape = strings.ToLower(strings.TrimSpace(shape))
	switch shape {
	case "iso_text", "title":
		return o.Width, o.Depth, o.Height
	case "cylinder", "stored_data", "queue":
		c := DefaultIsoCylinder()
		applyCylinder(o, &c)
		return 2 * c.Radius, pickDim(o.Depth, 2*c.Radius), c.Height
	case "circle":
		s := DefaultIsoSphere()
		applySphere(o, &s)
		return 2 * s.Radius, 2 * s.Radius, 2 * s.Radius
	case "person":
		p := DefaultIsoPerson()
		applyPerson(o, &p)
		return p.BodyWidth, p.BodyDepth, p.BodyHeight
	default:
		b := DefaultIsoBox()
		applyBox(o, &b)
		w, d, h = b.Width, b.Depth, b.Height
		if shape == "square" {
			w = math.Max(w, d)
			d = w
		}
		mw, md, mh := ShapeMinDims(shape)
		return math.Max(w, mw), math.Max(d, md), math.Max(h, mh)
	}
}

func roundedRadius(w, d, r float64) float64 {
	r = math.Min(r, math.Min(w, d)/2)
	if r <= 0 {
		r = math.Min(w, d) * .18
	}
	return r
}

// outlineOrigin is the same projection and extrema scan used by the rounded
// box and cloud renderers. Using a bounding-box corner here shifts rounded
// parts away from their world position because that corner is cut away.
func outlineOrigin(outline [][2]float64, h, margin float64) (float64, float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	for _, p := range outline {
		x, y := project(p[0], p[1], 0)
		minX, minY = math.Min(minX, x), math.Min(minY, y-h)
	}
	return -minX + margin, -minY + margin
}
