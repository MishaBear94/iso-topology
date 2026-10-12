package iso25d

import (
	"math"
	"strings"
)

// SpatialFace couples a projected face to its physical world plane. World
// coordinates use the part's bottom origin, before its composite translation.
// The screen convention is ShapeBody's, without standalone SVG centering.
// Stroke describes the actual solid round border painted around this face.
type SpatialFace struct {
	Name        string
	World       [][3]float64
	StrokeWidth float64
}

// SpatialBody is deliberately capability-bearing. A semantic envelope is not
// a depth surface. Unsupported material/effect combinations retain their old
// compositor as a whole scene; callers must expose the reason rather than
// silently reporting physical visibility for that fallback.
type SpatialBody struct {
	Faces  []SpatialFace
	Size   [3]float64
	Reason string
}

func ShapeSpatialBody(shape string, o ConvertOpts) SpatialBody {
	out := SpatialBody{}
	shape = strings.ToLower(strings.TrimSpace(shape))
	switch shape {
	case "rectangle", "square", "box", "":
	default:
		out.Reason = "unsupported shape: " + shape
		return out
	}
	if o.CornerRadius > 0 {
		out.Reason = "rounded surface"
		return out
	}
	if o.Wireframe || (o.Opacity > 0 && o.Opacity < 1) {
		out.Reason = "non-opaque body"
		return out
	}
	if o.Blur > 0 || o.ShadowColor != "" || o.BackglowColor != "" || o.OutlineColor != "" || o.Background != "" || o.GrainIntensity > 0 {
		out.Reason = "off-surface effect"
		return out
	}
	b := DefaultIsoBox()
	applyBox(o, &b)
	w, d, h := ShapeDimensions(shape, o)
	out.Size = [3]float64{w, d, h}
	if w <= 0 || d <= 0 || h <= 0 || math.IsNaN(w+d+h) || math.IsInf(w+d+h, 0) {
		out.Reason = "degenerate body"
		return out
	}
	candidates := []struct {
		name   string
		world  [][3]float64
		alpha  alphaRange
		stroke struct {
			Color, Dash string
			Width       float64
		}
	}{
		{"left", [][3]float64{{0, d, 0}, {0, d, h}, {w, d, h}, {w, d, 0}}, gradientAlpha(b.LeftFill, b.LeftGradient), b.LeftStroke},
		{"right", [][3]float64{{w, 0, 0}, {w, 0, h}, {w, d, h}, {w, d, 0}}, gradientAlpha(b.RightFill, b.RightGradient), b.RightStroke},
		{"top", [][3]float64{{0, 0, h}, {w, 0, h}, {w, d, h}, {0, d, h}}, gradientAlpha(b.TopFill, b.TopGradient), b.TopStroke},
	}
	for _, v := range candidates {
		a := overrideAlpha(v.alpha, b.FaceSurfaces, v.name)
		if a.lo != 1 {
			out.Reason = "non-opaque face: " + v.name
			return out
		}
		if fs := surfaceFor(b.FaceSurfaces, v.name); fs != nil && len(fs.Strokes) > 0 {
			out.Reason = "layered face stroke"
			return out
		}
		color, width, dash := pickFaceStroke(b.Stroke, b.StrokeWidth, b.StrokeDasharray, v.stroke)
		if color == "" || strings.EqualFold(color, "none") || width <= 0 {
			width = 0
		} else {
			if dash != "" || fillAlpha(color).lo != 1 {
				out.Reason = "non-solid face stroke"
				return out
			}
		}
		out.Faces = append(out.Faces, SpatialFace{Name: v.name, World: v.world, StrokeWidth: width})
	}
	return out
}
