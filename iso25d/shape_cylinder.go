package iso25d

import (
	"fmt"
	"math"
	"strings"
)

// Cylinder (database / queue / stored_data) shape implementation.
// ---------------------------------------------------------------------------
// Cylinder (database / queue / stored_data)
// ---------------------------------------------------------------------------

type IsoCylinderOpts struct {
	Radius float64
	Height float64

	TopFill   string
	LeftFill  string
	RightFill string

	Stroke      string
	StrokeWidth float64

	Label      string
	FontFamily string
	FontSize   float64
	FontColor  string
	FontWeight string

	Icon      string
	IconScale float64
	// v3.1 — vertical side gradients (were silently dropped before).
	LeftGradient  *FaceGradient
	RightGradient *FaceGradient
	// v3.3 — per-face surface overrides (style.faces): top / left / right.
	FaceSurfaces map[string]*FaceSurface
	// v3.7 — gaussian blur stdDev over the whole part.
	Blur float64
	// v3.8 — silhouette accent ring (effects.outline).
	OutlineColor   string
	OutlineWidth   float64
	OutlineDash    string
	OutlineOpacity float64

	Margin float64

	Opacity         float64
	StrokeDasharray string
	Background      string

	// v2.4 — hatch/dots texture overlaid on the top ellipse.
	PatternKind    string
	PatternColor   string
	PatternSpacing float64
	PatternAngle   float64

	// v2.6 — film-grain noise over the body + top.
	GrainIntensity float64
	GrainScale     float64
}

func DefaultIsoCylinder() IsoCylinderOpts {
	return IsoCylinderOpts{
		Radius: 80, Height: 110,
		TopFill:     "#FFD27F",
		LeftFill:    "#B8651F",
		RightFill:   "#E08A47",
		Stroke:      "#5C2A0A",
		StrokeWidth: 1.5,
		FontFamily:  "Helvetica Neue, Arial, sans-serif",
		FontSize:    defaultFontSize,
		FontColor:   "#3A1A06",
		FontWeight:  "600",
		IconScale:   0.45,
		Margin:      24,
	}
}

func RenderIsoCylinder(o IsoCylinderOpts) string {
	return renderIsoCylinder(o, nil)
}

func renderIsoCylinder(o IsoCylinderOpts, fonts fontResources) string {
	rx := o.Radius
	ry := rx * math.Tan(math.Pi/6)
	h := o.Height
	m := o.Margin

	topCx, topCy := 0.0, 0.0
	botCy := h

	minX := topCx - rx
	maxX := topCx + rx
	minY := topCy - ry
	maxY := botCy + ry

	W := (maxX - minX) + 2*m
	H := (maxY - minY) + 2*m
	tx, ty := -minX+m, -minY+m
	sx := func(x float64) float64 { return x + tx }
	sy := func(y float64) float64 { return y + ty }

	var sb strings.Builder
	sb.WriteString(svgHeader(W, H))
	grainID := ""
	leftFill, rightFill := o.LeftFill, o.RightFill
	topFill := o.TopFill
	var faceDefs strings.Builder
	if fs := surfaceFor(o.FaceSurfaces, "top"); fs != nil && fs.Fill != nil {
		if ref := emitFaceFill(&faceDefs, "", "top", fs.Fill); ref != "" {
			topFill = ref
		}
	}
	if fs := surfaceFor(o.FaceSurfaces, "left"); fs != nil && fs.Fill != nil {
		if ref := emitFaceFill(&faceDefs, "", "left", fs.Fill); ref != "" {
			leftFill = ref
		}
	}
	if fs := surfaceFor(o.FaceSurfaces, "right"); fs != nil && fs.Fill != nil {
		if ref := emitFaceFill(&faceDefs, "", "right", fs.Fill); ref != "" {
			rightFill = ref
		}
	}
	{
		var defs strings.Builder
		if id := emitGrainFilter(&defs, "cyl-grain", o.GrainIntensity, o.GrainScale); id != "" {
			grainID = id
		}
		if g := o.LeftGradient; g != nil && strings.TrimSpace(g.From) != "" {
			fmt.Fprintf(&defs, `<linearGradient id="cyl-grad-l" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient>`,
				escapeAttr(g.From), escapeAttr(g.To))
			leftFill = "url(#cyl-grad-l)"
		}
		if g := o.RightGradient; g != nil && strings.TrimSpace(g.From) != "" {
			fmt.Fprintf(&defs, `<linearGradient id="cyl-grad-r" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient>`,
				escapeAttr(g.From), escapeAttr(g.To))
			rightFill = "url(#cyl-grad-r)"
		}
		if defs.Len() > 0 {
			fmt.Fprintf(&sb, `<defs>%s</defs>`, defs.String())
		}
	}
	openWrapper(&sb, W, H, o.Background, o.Opacity, o.StrokeDasharray, "")
	blurOn := emitBlurOpen(&sb, "cyl-blur", o.Blur)
	if faceDefs.Len() > 0 {
		fmt.Fprintf(&sb, `<defs>%s</defs>`, faceDefs.String())
	}
	if grainID != "" {
		fmt.Fprintf(&sb, `<g filter="url(#%s)">`, grainID)
	}

	leftTopX, leftTopY := sx(-rx), sy(0)
	rightTopX, rightTopY := sx(rx), sy(0)
	leftBotX, leftBotY := sx(-rx), sy(h)
	rightBotX, rightBotY := sx(rx), sy(h)
	topFrontX, topFrontY := sx(0), sy(0+ry)
	botFrontX, botFrontY := sx(0), sy(h+ry)

	// Two non-overlapping body halves, each filled with its own pure colour.
	// Bounded by: outer vertical edge, front-bottom quarter arc, front
	// meridian (vertical line at x=0), front-top quarter arc.
	// Both halves carry no stroke — the silhouette is one separate path so
	// no seam appears on the front meridian. The top arc traverses the
	// ellipse in the opposite direction to the bottom arc. Using the same
	// sweep fills part of an open/transparent roof with the side color.
	fmt.Fprintf(&sb,
		`<path data-face="side-left" d="M %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f Z" fill="%s" stroke="none"/>`,
		leftTopX, leftTopY,
		leftBotX, leftBotY,
		rx, ry, botFrontX, botFrontY,
		topFrontX, topFrontY,
		rx, ry, leftTopX, leftTopY,
		escapeAttr(leftFill),
	)
	fmt.Fprintf(&sb,
		`<path data-face="side-right" d="M %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f Z" fill="%s" stroke="none"/>`,
		rightTopX, rightTopY,
		rightBotX, rightBotY,
		rx, ry, botFrontX, botFrontY,
		topFrontX, topFrontY,
		rx, ry, rightTopX, rightTopY,
		escapeAttr(rightFill),
	)

	// Body silhouette: left edge + full front-bottom arc + right edge.
	// Open path (no Z) so no horizontal line is drawn across the top — the
	// top ellipse drawn next provides the top silhouette via its own stroke.
	if strings.TrimSpace(o.Stroke) != "" && o.StrokeWidth > 0 {
		fmt.Fprintf(&sb,
			`<path data-face="outline" d="M %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f L %.2f %.2f" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"/>`,
			leftTopX, leftTopY,
			leftBotX, leftBotY,
			rx, ry, rightBotX, rightBotY,
			rightTopX, rightTopY,
			escapeAttr(o.Stroke), o.StrokeWidth,
		)
	}

	// Top ellipse.
	fmt.Fprintf(&sb,
		`<ellipse data-face="top" cx="%.2f" cy="%.2f" rx="%.2f" ry="%.2f" fill="%s" stroke="%s" stroke-width="%.2f"/>`,
		sx(topCx), sy(topCy), rx, ry, escapeAttr(topFill), escapeAttr(o.Stroke), o.StrokeWidth,
	)
	// v2.4 — texture overlay on the top ellipse.
	{
		var defs strings.Builder
		if patID := emitPatternDef(&defs, "cyl-pattern", o.PatternKind, o.PatternColor, o.PatternSpacing, o.PatternAngle); patID != "" {
			fmt.Fprintf(&sb, `<defs>%s</defs><ellipse data-face="top-pattern" cx="%.2f" cy="%.2f" rx="%.2f" ry="%.2f" fill="url(#%s)" stroke="none"/>`,
				defs.String(), sx(topCx), sy(topCy), rx, ry, patID)
		}
	}

	if grainID != "" {
		sb.WriteString(`</g>`)
	}

	// Label + icon on the top circle, using the inscribed square (in 3D) as
	// the local frame so the icon/text sit cleanly inside the top ellipse.
	s := rx / cos30
	e := sx(topCx)
	f := sy(topCy) - s*sin30
	writeTopLabelAndIconV12(
		&sb,
		e, f, s, s,
		o.Label, nil, o.Icon, o.IconScale, "", 0, 0,
		o.FontFamily, o.FontSize, o.FontWeight, o.FontColor,
		fonts,
	)

	if o.OutlineColor != "" && o.OutlineWidth > 0 {
		// Reuse the renderer's own capsule coords (the provider silhouette
		// lives in a different frame). Ring = body outline + top ellipse.
		extra := ""
		if o.OutlineDash != "" {
			extra += fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(o.OutlineDash))
		}
		if o.OutlineOpacity > 0 && o.OutlineOpacity < 1 {
			extra += fmt.Sprintf(` stroke-opacity="%.3f"`, o.OutlineOpacity)
		}
		fmt.Fprintf(&sb,
			`<path data-face="outline" d="M %.2f %.2f L %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f L %.2f %.2f" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"%s/>`,
			leftTopX, leftTopY, leftBotX, leftBotY, rx, ry, rightBotX, rightBotY, rightTopX, rightTopY,
			escapeAttr(o.OutlineColor), o.OutlineWidth, extra)
		fmt.Fprintf(&sb,
			`<ellipse data-face="outline" cx="%.2f" cy="%.2f" rx="%.2f" ry="%.2f" fill="none" stroke="%s" stroke-width="%.2f"%s/>`,
			sx(topCx), sy(topCy), rx, ry, escapeAttr(o.OutlineColor), o.OutlineWidth, extra)
	}
	closeWrapper(&sb)
	if blurOn {
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// ---------------------------------------------------------------------------
// Sphere (circle, person head)
// ---------------------------------------------------------------------------

// applyCylinder lowers a ConvertOpts into an IsoCylinderOpts.
func applyCylinder(o ConvertOpts, c *IsoCylinderOpts) {
	c.PatternKind, c.PatternColor = o.PatternKind, o.PatternColor
	c.PatternSpacing, c.PatternAngle = o.PatternSpacing, o.PatternAngle
	c.GrainIntensity, c.GrainScale = o.GrainIntensity, o.GrainScale
	if o.Width > 0 {
		c.Radius = o.Width / 2
	}
	if o.Height > 0 {
		c.Height = o.Height
	}
	if o.TopFill != "" {
		c.TopFill = o.TopFill
	}
	if o.LeftFill != "" {
		c.LeftFill = o.LeftFill
	}
	if o.RightFill != "" {
		c.RightFill = o.RightFill
	}
	c.LeftGradient = o.LeftGradient
	c.RightGradient = o.RightGradient
	c.FaceSurfaces = o.FaceSurfaces
	c.Blur = o.Blur
	c.OutlineColor, c.OutlineWidth = o.OutlineColor, o.OutlineWidth
	c.OutlineDash, c.OutlineOpacity = o.OutlineDash, o.OutlineOpacity
	if o.Stroke != "" {
		c.Stroke = o.Stroke
	}
	if o.StrokeWidth > 0 {
		c.StrokeWidth = o.StrokeWidth
	}
	if o.Label != "" {
		c.Label = o.Label
	}
	if o.FontFamily != "" {
		c.FontFamily = o.FontFamily
	}
	if o.FontSize > 0 {
		c.FontSize = o.FontSize
	}
	if o.FontColor != "" {
		c.FontColor = o.FontColor
	}
	if o.FontWeight != "" {
		c.FontWeight = o.FontWeight
	}
	if o.Icon != "" {
		c.Icon = o.Icon
	}
	if o.IconScale > 0 {
		c.IconScale = o.IconScale
	}
	if o.Margin > 0 {
		c.Margin = o.Margin
	}
	if o.Opacity > 0 {
		c.Opacity = o.Opacity
	}
	if o.StrokeDasharray != "" {
		c.StrokeDasharray = o.StrokeDasharray
	}
	if o.Background != "" {
		c.Background = o.Background
	}
}

// CylinderShapeProvider — M2.1: geometry-only provider so connector
// clipping uses the cylinder's true capsule outline instead of the
// bbox hexagon (whose empty corners left arrowheads floating 30-60px
// off the body — the acceptance round's most-reported defect class).
type CylinderShapeProvider struct{}

func (CylinderShapeProvider) Names() []string {
	return []string{"cylinder", "stored_data", "queue", "oval"}
}

// Faces gives a coarse description (top disc + one side band) — enough
// for invariants; the legacy renderer still draws the pixels.
func (CylinderShapeProvider) Faces(w, d, h float64, _ map[string]any) []Face {
	sil := (CylinderShapeProvider{}).Silhouette(w, d, h, nil)
	return []Face{{Name: "body", Points: sil, Normal: [3]float64{0.7, 0.7, 0}, ZOrder: 0, Visible: true}}
}

// Silhouette samples the capsule: top ellipse arc, vertical tangents,
// bottom front arc. Local frame matches PartOriginOffset's cylinder
// case: top-ellipse centre at (rx, ry); world-origin corner offset is
// handled by the generic frame conversion in partSilhouette.
func (CylinderShapeProvider) Silhouette(w, d, h float64, _ map[string]any) [][2]float64 {
	rx := w / 2
	ry := rx * sin30 / cos30
	// Local frame contract (see partSilhouette): local − (d·cos30, h) =
	// projection of the world-relative point, so the top-ellipse centre
	// (world (w/2, d/2, h)) sits at local ((w−d)/2·c30 + d·c30, (w+d)/2·s30).
	cx := (w-d)/2*cos30 + d*cos30
	cyTop := (w + d) / 2 * sin30
	cyBot := cyTop + h
	var pts [][2]float64
	const n = 12
	// upper back arc of the top ellipse, left → right
	for i := 0; i <= n; i++ {
		th := math.Pi - math.Pi*float64(i)/float64(n)
		pts = append(pts, [2]float64{cx + rx*math.Cos(th), cyTop - ry*math.Sin(th)})
	}
	// lower front arc of the bottom ellipse, right → left (the straight
	// vertical tangents fall out of joining the arc endpoints)
	for i := 0; i <= n; i++ {
		th := math.Pi * float64(i) / float64(n)
		pts = append(pts, [2]float64{cx + rx*math.Cos(th), cyBot + ry*math.Sin(th)})
	}
	return pts
}

func (CylinderShapeProvider) ContentAnchor() string { return "top" }

func (CylinderShapeProvider) ContentRectFor(w, d, h float64, _ map[string]any) ContentRect {
	s := w / math.Sqrt2
	return ContentRect{X: (w - s) / 2, Y: (d - s) / 2, W: s, H: s}
}

func (CylinderShapeProvider) Footprint(w, d, _ float64) (float64, float64) { return w, d }

func init() {
	RegisterShape(CylinderShapeProvider{})
}
