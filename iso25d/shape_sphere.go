package iso25d

import (
	"fmt"
	"strings"
)

type IsoSphereOpts struct {
	Radius      float64
	Highlight   string // top-lit colour
	Shadow      string // shaded colour
	Stroke      string
	StrokeWidth float64
	Label       string
	Icon        string // resolved href (data URI); drawn flat on the disc
	IconScale   float64
	FontFamily  string
	FontSize    float64
	FontColor   string
	FontWeight  string
	Margin      float64

	Opacity         float64
	StrokeDasharray string
	Background      string

	// v2.4 — hatch/dots texture overlaid on the sphere disc.
	PatternKind    string
	PatternColor   string
	PatternSpacing float64
	PatternAngle   float64
}

func DefaultIsoSphere() IsoSphereOpts {
	return IsoSphereOpts{
		Radius:      80,
		Highlight:   "#C2DAFF",
		Shadow:      "#2A4A7A",
		Stroke:      "#1D3A66",
		StrokeWidth: 1.5,
		FontFamily:  "Helvetica Neue, Arial, sans-serif",
		FontSize:    defaultFontSize,
		FontColor:   "#0B1F3A",
		FontWeight:  "600",
		Margin:      24,
	}
}

func RenderIsoSphere(o IsoSphereOpts) string {
	return renderIsoSphere(o, nil)
}

func renderIsoSphere(o IsoSphereOpts, fonts fontResources) string {
	r := o.Radius
	cx, cy, W, H := sphereFrame(o)

	var sb strings.Builder
	sb.WriteString(svgHeader(W, H))
	openWrapper(&sb, W, H, o.Background, o.Opacity, o.StrokeDasharray, "")
	gradID := "sphere-grad"
	fmt.Fprintf(&sb,
		`<defs><radialGradient id="%s" cx="35%%" cy="30%%" r="75%%"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></radialGradient></defs>`,
		gradID, escapeAttr(o.Highlight), escapeAttr(o.Shadow),
	)
	fmt.Fprintf(&sb,
		`<circle data-face="sphere" cx="%.2f" cy="%.2f" r="%.2f" fill="url(#%s)" stroke="%s" stroke-width="%.2f"/>`,
		cx, cy, r, gradID, escapeAttr(o.Stroke), o.StrokeWidth,
	)
	// v2.4 — texture overlay clipped to the sphere disc.
	{
		var defs strings.Builder
		if patID := emitPatternDef(&defs, "sphere-pattern", o.PatternKind, o.PatternColor, o.PatternSpacing, o.PatternAngle); patID != "" {
			fmt.Fprintf(&sb, `<defs>%s</defs><circle data-face="sphere-pattern" cx="%.2f" cy="%.2f" r="%.2f" fill="url(#%s)" stroke="none"/>`,
				defs.String(), cx, cy, r, patID)
		}
	}
	// v3.0 — icon support: the sphere used to silently drop `icon`.
	// The disc is screen-flat, so the icon is drawn unprojected, centred
	// (or above a label when both are present), sized to stay inside the
	// disc with the same default scale as the box family.
	hasIcon := strings.TrimSpace(o.Icon) != ""
	hasLabel := strings.TrimSpace(o.Label) != ""
	if hasIcon {
		scale := o.IconScale
		if scale <= 0 {
			scale = 0.4
		}
		iconSize := 2 * r * scale
		iy := cy - iconSize/2
		if hasLabel {
			iy = cy - iconSize - 2
		}
		fmt.Fprintf(&sb,
			`<image href="%s" xlink:href="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f" preserveAspectRatio="xMidYMid meet"/>`,
			escapeAttr(o.Icon), escapeAttr(o.Icon), cx-iconSize/2, iy, iconSize, iconSize,
		)
	}
	if caption := sphereCaption(o); len(caption) > 0 {
		writeBoundCaption(&sb, caption, cx, cy, o.FontColor, "", fonts)
	} else if hasLabel {
		ly := cy
		if hasIcon {
			ly = cy + o.FontSize*0.8
		}
		fmt.Fprintf(&sb,
			`<text x="%.2f" y="%.2f" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle" dy=".35em">%s</text>`,
			cx, ly,
			escapeAttr(o.FontFamily), o.FontSize, escapeAttr(o.FontWeight),
			escapeAttr(o.FontColor), escapeXML(o.Label),
		)
	}
	closeWrapper(&sb)
	sb.WriteString(`</svg>`)
	return sb.String()
}

// ---------------------------------------------------------------------------
// Person (sphere head + box body)
// ---------------------------------------------------------------------------

func applySphere(o ConvertOpts, s *IsoSphereOpts) {
	s.PatternKind, s.PatternColor = o.PatternKind, o.PatternColor
	s.PatternSpacing, s.PatternAngle = o.PatternSpacing, o.PatternAngle
	if o.Width > 0 {
		s.Radius = o.Width / 2
	}
	if o.TopFill != "" {
		s.Highlight = o.TopFill
	}
	if o.LeftFill != "" {
		s.Shadow = o.LeftFill
	}
	if o.Stroke != "" {
		s.Stroke = o.Stroke
	}
	if o.StrokeWidth > 0 {
		s.StrokeWidth = o.StrokeWidth
	}
	if o.Label != "" {
		s.Label = o.Label
	}
	if o.Icon != "" {
		s.Icon = o.Icon
	}
	if o.IconScale > 0 {
		s.IconScale = o.IconScale
	}
	if o.FontFamily != "" {
		s.FontFamily = o.FontFamily
	}
	if o.FontSize > 0 {
		s.FontSize = o.FontSize
	}
	if o.FontColor != "" {
		s.FontColor = o.FontColor
	}
	if o.FontWeight != "" {
		s.FontWeight = o.FontWeight
	}
	if o.Margin > 0 {
		s.Margin = o.Margin
	}
	if o.Opacity > 0 {
		s.Opacity = o.Opacity
	}
	if o.StrokeDasharray != "" {
		s.StrokeDasharray = o.StrokeDasharray
	}
	if o.Background != "" {
		s.Background = o.Background
	}
}
