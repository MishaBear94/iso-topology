package iso25d

import (
	"math"
	"strings"
)

// Convert2DTo25D — central iso shape dispatcher.
//
// ConvertOpts is the canonical intermediate representation: every
// rendered shape's options are projected from it. Shape-specific
// renderers live in shape_*.go files; this file owns the dispatch.
type ConvertOpts struct {
	Width, Depth, Height float64
	Sides                int                     // polygon sides (3+); only used by shape=polygon
	TopScale             float64                 // tapered prism: 0=apex, 0..1=frustum, 1=prism
	Params               map[string]any          // shape-specific parameters (e.g. custom_path.path)
	FaceSurfaces         map[string]*FaceSurface // v3.3 — style.faces overrides
	Blur                 float64                 // v3.7 — gaussian blur stdDev over the whole part
	OutlineColor         string                  // v3.8 — silhouette accent ring
	OutlineWidth         float64
	OutlineDash          string
	OutlineOpacity       float64

	TopFill, LeftFill, RightFill string
	Stroke                       string
	StrokeWidth                  float64

	// Per-face stroke overrides (v1.2). Empty string / 0 width = inherit.
	TopStroke, LeftStroke, RightStroke struct {
		Color, Dash string
		Width       float64
	}
	// Per-face opacity overrides (v1.2). 0 = inherit base Opacity.
	TopOpacity, LeftOpacity, RightOpacity float64
	// Linear-gradient fills per face (v1.2). Non-nil overrides the matching
	// solid fill.
	TopGradient, LeftGradient, RightGradient *FaceGradient

	Label      string
	LabelLines []string // multi-line override; falls back to splitting Label by \n
	FontFamily string
	FontSize   float64
	FontColor  string
	FontWeight string

	Icon       string
	IconScale  float64
	IconAnchor string  // center | topLeft | topRight | bottomLeft | bottomRight
	IconOffX   float64 // fraction of W (0..1)
	IconOffY   float64 // fraction of D (0..1)

	Margin       float64
	CornerRadius float64 // box family: clipped corner radius in local units
	FaceSplit    bool    // rounded box: split the side band into independent left/right faces

	// Drop shadow filter (v1.2). Zero color = disabled.
	ShadowDx, ShadowDy, ShadowBlur float64
	ShadowColor                    string

	// Structural payload for class / sql_table / sequence_diagram.
	Rows      []string
	RowColors []string
	Header    string

	// Direction overrides for callout / step.
	TailDir  string // callout: down | up | left | right
	ArrowDir string // step: right | left

	Opacity         float64
	StrokeDasharray string
	Background      string

	// v1.3 — material + variant knobs.
	Wireframe       bool
	BackglowColor   string
	BackglowRadius  float64
	BackglowOpacity float64
	GrainIntensity  float64
	GrainScale      float64
	PatternKind     string // hatch | dots | grid
	PatternColor    string
	PatternSpacing  float64
	PatternAngle    float64
	Cells           [][]string
	CellColors      [][]string
}

// FaceGradient is the low-level twin of nodedsl.FaceGradient. Kept here so
// iso25d stays standalone (no cyclic import with the parent package).
type FaceGradient struct {
	From, To, Dir string
}

// Convert2DTo25D maps a 2D D2 node-type name to an iso SVG string. Every
// constant from d2target.go's shape list is handled; for shapes that don't
// have a natural 3D analogue, we use a sensible fallback (flat slab, low
// box, etc.).
func Convert2DTo25D(shapeType string, o ConvertOpts) string {
	return convert2DTo25D(shapeType, o, nil)
}

func convert2DTo25D(shapeType string, o ConvertOpts, fonts fontResources) string {
	shapeType = strings.ToLower(strings.TrimSpace(shapeType))
	switch shapeType {

	case "cylinder", "stored_data", "queue":
		c := DefaultIsoCylinder()
		applyCylinder(o, &c)
		return renderIsoCylinder(c, fonts)

	case "circle":
		s := DefaultIsoSphere()
		applySphere(o, &s)
		return renderIsoSphere(s, fonts)

	case "composite":
		// Composite parts are dispatched at the isotopo layer via
		// RenderComposite. Returning empty here is a safety net for
		// callers that bypass that layer.
		return ""

	case "person":
		p := DefaultIsoPerson()
		applyPerson(o, &p)
		p.HeadStyle = "sphere"
		return renderIsoPerson(p, fonts)

	case "cloud":
		b := DefaultIsoBox()
		applyBox(o, &b)
		mw, md, mh := ShapeMinDims("cloud")
		if b.Width < mw {
			b.Width = mw
		}
		if b.Depth < md {
			b.Depth = md
		}
		if b.Height < mh {
			b.Height = mh
		}
		return renderIsoCloud(b, fonts)

	case "iso_text", "title":
		b := DefaultIsoBox()
		applyBox(o, &b)
		return RenderIsoText(b)

	// v3.10 — wedge: sloped ramp (back edge at h, front at 0).
	case "wedge":
		b := DefaultIsoBox()
		applyBox(o, &b)
		return renderIsoWedge(b, fonts)

	// v3.10 — custom_path: arbitrary polygon base extruded vertically.
	case "custom_path":
		b := DefaultIsoBox()
		applyBox(o, &b)
		pathStr := ""
		if o.Params != nil {
			if p, ok := o.Params["path"].(string); ok {
				pathStr = p
			}
		}
		return renderIsoCustomPath(b, pathStr, fonts)

	// v3.2 (M2) — prism family: regular n-gon base × vertical extrude.
	case "triprism", "hexprism", "octprism":
		b := DefaultIsoBox()
		applyBox(o, &b)
		sides := o.Sides
		switch shapeType {
		case "triprism":
			sides = 3
		case "hexprism":
			sides = 6
		case "octprism":
			sides = 8
		}
		if sides < 3 {
			sides = 6
		}
		return renderIsoPrism(b, sides, fonts)

	// v3.11 — array shapes: tiled cell grids.
	case "array1d", "array2d", "array3d":
		b := DefaultIsoBox()
		applyBox(o, &b)
		return renderIsoArray(b, shapeType, fonts)

	// v3.11 — rack: server rack with slot shelves.
	case "rack":
		b := DefaultIsoBox()
		applyBox(o, &b)
		slots := 4
		if o.Params != nil {
			if v, ok := o.Params["slots"].(int); ok && v > 0 {
				slots = v
			}
			if v, ok := o.Params["slots"].(float64); ok && v > 0 {
				slots = int(v)
			}
		}
		return renderIsoRack(b, slots, fonts)

	// rectangle / square / empty / unknown → plain iso box.
	case "rectangle", "square", "":
		b := DefaultIsoBox()
		applyBox(o, &b)
		if strings.ToLower(strings.TrimSpace(shapeType)) == "square" {
			s := math.Max(b.Width, b.Depth)
			b.Width, b.Depth = s, s
		}
		return renderIsoBox(b, fonts)

	default:
		b := DefaultIsoBox()
		applyBox(o, &b)
		return renderIsoBox(b, fonts)
	}
}

// pickDim returns a positive value, falling back to defaultVal when v <= 0.
func pickDim(v, defaultVal float64) float64 {
	if v > 0 {
		return v
	}
	return defaultVal
}

// pickStructural lowers the DSL Header / Rows / RowColors into concrete
// values for RenderIsoBoxWithDividers. Falls back to label / defaults when
// the DSL didn't supply data.
// ShapeMinDims returns the minimum world (w, d, h) a shape will actually be
// DRAWN at, regardless of authored geom. Some silhouettes (the cloud) need a
// floor size to read correctly, so the renderer clamps up to these values. It
// is exported so the layout solver reserves the SAME space it will draw into —
// otherwise an auto-sized group wraps the authored size while the renderer
// paints the larger clamped size, and the shape overflows its slot. Shapes with
// no floor return (0, 0, 0), i.e. "use whatever was authored".
func ShapeMinDims(shape string) (w, d, h float64) {
	switch shape {
	case "cloud":
		return 200, 140, 24
	default:
		return 0, 0, 0
	}
}

func applyBox(o ConvertOpts, b *IsoBoxOpts) {
	if o.Width > 0 {
		b.Width = o.Width
	}
	if o.Depth > 0 {
		b.Depth = o.Depth
	}
	if o.Height > 0 {
		b.Height = o.Height
	}
	if o.TopFill != "" {
		b.TopFill = o.TopFill
	}
	if o.LeftFill != "" {
		b.LeftFill = o.LeftFill
	}
	if o.RightFill != "" {
		b.RightFill = o.RightFill
	}
	if o.Stroke != "" {
		b.Stroke = o.Stroke
	}
	if o.StrokeWidth > 0 {
		b.StrokeWidth = o.StrokeWidth
	}
	if o.Label != "" {
		b.Label = o.Label
	}
	if o.FontFamily != "" {
		b.FontFamily = o.FontFamily
	}
	if o.FontSize > 0 {
		b.FontSize = o.FontSize
	}
	if o.FontColor != "" {
		b.FontColor = o.FontColor
	}
	if o.FontWeight != "" {
		b.FontWeight = o.FontWeight
	}
	if o.Icon != "" {
		b.Icon = o.Icon
	}
	if o.IconScale > 0 {
		b.IconScale = o.IconScale
	}
	if o.Margin > 0 {
		b.Margin = o.Margin
	}
	if o.Opacity > 0 {
		b.Opacity = o.Opacity
	}
	if o.StrokeDasharray != "" {
		b.StrokeDasharray = o.StrokeDasharray
	}
	if o.Background != "" {
		b.Background = o.Background
	}
	// v1.2 wiring
	b.TopStroke = o.TopStroke
	b.LeftStroke = o.LeftStroke
	b.RightStroke = o.RightStroke
	b.TopOpacity = o.TopOpacity
	b.LeftOpacity = o.LeftOpacity
	b.RightOpacity = o.RightOpacity
	b.TopGradient = o.TopGradient
	b.LeftGradient = o.LeftGradient
	b.RightGradient = o.RightGradient
	b.LabelLines = o.LabelLines
	b.IconAnchor = o.IconAnchor
	b.IconOffX = o.IconOffX
	b.IconOffY = o.IconOffY
	b.CornerRadius = o.CornerRadius
	b.FaceSplit = o.FaceSplit
	b.FaceSurfaces = o.FaceSurfaces
	b.Blur = o.Blur
	b.OutlineColor, b.OutlineWidth = o.OutlineColor, o.OutlineWidth
	b.OutlineDash, b.OutlineOpacity = o.OutlineDash, o.OutlineOpacity
	b.ShadowDx, b.ShadowDy, b.ShadowBlur, b.ShadowColor = o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor
	// v1.3
	b.Wireframe = o.Wireframe
	b.BackglowColor = o.BackglowColor
	b.BackglowRadius = o.BackglowRadius
	b.BackglowOpacity = o.BackglowOpacity
	b.GrainIntensity = o.GrainIntensity
	b.GrainScale = o.GrainScale
	b.PatternKind = o.PatternKind
	b.PatternColor = o.PatternColor
	b.PatternSpacing = o.PatternSpacing
	b.PatternAngle = o.PatternAngle
	b.Cells = o.Cells
	b.CellColors = o.CellColors
	b.Params = o.Params
}
