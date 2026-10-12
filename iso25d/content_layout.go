package iso25d

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// contentPlacement is the shared local layout consumed by both SVG emission
// and projected content observation. Text Y is the SVG y before dy=.35em.
type contentPlacement struct {
	Text                 string
	X, Y, W, H, FontSize float64
	Font                 *textmetrics.BuiltinFont
	Icon                 bool
}

func layoutTopContent(faceW, faceD float64, label string, labelLines []string, icon string, scale float64, anchor string, offX, offY, fontSize float64) []contentPlacement {
	return layoutTopContentFont(faceW, faceD, label, labelLines, icon, scale, anchor, offX, offY, fontSize, nil)
}

func layoutTopContentFont(faceW, faceD float64, label string, labelLines []string, icon string, scale float64, anchor string, offX, offY, fontSize float64, font *textmetrics.BuiltinFont) (placements []contentPlacement) {
	legacy := layoutTopContentLegacy(faceW, faceD, label, labelLines, icon, scale, anchor, offX, offY, fontSize, font)
	if font == nil || (strings.TrimSpace(icon) != "" && (anchor != "" || offX != 0 || offY != 0)) {
		return legacy
	}
	if scale <= 0 {
		scale = defaultIconScale
	}
	return layoutBoundTopContent(faceW, faceD, authoredContentLines(label, labelLines), strings.TrimSpace(icon) != "", scale, fontSize, font, legacy)
}

func layoutTopContentLegacy(faceW, faceD float64, label string, labelLines []string, icon string, scale float64, anchor string, offX, offY, fontSize float64, font *textmetrics.BuiltinFont) (placements []contentPlacement) {
	defer func() {
		for i := range placements {
			if !placements[i].Icon {
				placements[i].Font = font
			}
		}
	}()
	var measure textWidth
	if font != nil {
		measure = func(text string, fs float64) float64 {
			m := font.Measure(text)
			if m.Known {
				return m.CenteredWidth() * fs
			}
			return lineWidth(text, fs)
		}
	}
	lines := labelLines
	if len(lines) == 0 && strings.Contains(label, "\n") {
		lines = strings.Split(label, "\n")
	}
	hasIcon := strings.TrimSpace(icon) != ""
	hasLabel := strings.TrimSpace(label) != "" || len(lines) > 0
	if !hasLabel && !hasIcon {
		return nil
	}
	if scale <= 0 {
		scale = defaultIconScale
	}
	if !hasLabel && scale <= defaultIconScale {
		scale = defaultIconOnlyScale
	}
	if hasLabel && len(lines) == 0 {
		lines = []string{label}
	}
	var out []contentPlacement
	if !(hasIcon && (anchor != "" || offX != 0 || offY != 0)) {
		fit := fitTopContent(faceW, faceD, lines, hasIcon, scale, fontSize, measure)
		if fit.Adjusted {
			// On a shallow, wide face a row can preserve the requested font
			// and icon size when the legacy column would shrink the text.
			// Keep author positioning, fitting columns and multi-line content
			// unchanged. The candidate must fit with the same padding budget.
			if hasIcon && len(lines) == 1 && fit.FontSize < fontSize {
				pad := math.Max(6, .07*math.Min(faceW, faceD))
				size := math.Min(faceW, faceD) * scale
				width := lineWidth(lines[0], fontSize)
				if measure != nil {
					width = measure(lines[0], fontSize)
				}
				gap := math.Max(iconLabelGap, fontSize*.5)
				rowWidth := size + gap + width
				if rowWidth <= faceW-2*pad && math.Max(size, fontSize*lineHeightK) <= faceD-2*pad {
					x := (faceW - rowWidth) / 2
					return []contentPlacement{
						{X: x, Y: (faceD - size) / 2, W: size, H: size, Icon: true},
						{Text: lines[0], X: x + size + gap + width/2, Y: faceD/2 - fontSize*.05, FontSize: fontSize},
					}
				}
			}
			if hasIcon {
				out = append(out, contentPlacement{X: (faceW - fit.IconSize) / 2, Y: fit.IconTop, W: fit.IconSize, H: fit.IconSize, Icon: true})
			}
			for i, ln := range fit.Lines {
				out = append(out, contentPlacement{Text: ln, X: faceW / 2, Y: fit.FirstLineY + float64(i)*fit.FontSize*lineHeightK, FontSize: fit.FontSize})
			}
			return out
		}
	}
	lift := 0.
	if hasIcon && hasLabel && anchor == "" {
		lift = (fontSize*1.2*float64(len(lines)) + 4) / 2
	}
	if hasIcon {
		size := math.Min(faceW, faceD) * scale
		x, y := iconAnchorXY(anchor, faceW, faceD, size)
		out = append(out, contentPlacement{X: x + offX*faceW, Y: y + offY*faceD - lift, W: size, H: size, Icon: true})
	}
	if hasLabel {
		y := faceD * .5
		if hasIcon && anchor == "" {
			y = faceD*.5 + math.Min(faceW, faceD)*scale*.5 + fontSize*.7 + 3 - lift
		}
		lineH := fontSize * 1.2
		start := y - float64(len(lines)-1)*lineH/2
		for i, ln := range lines {
			out = append(out, contentPlacement{Text: ln, X: faceW / 2, Y: start + float64(i)*lineH, FontSize: fontSize})
		}
	}
	return out
}

// ContentElement describes one rendered text line or icon, in projected
// world-local coordinates. Anchor is the emitted SVG text x/y (before DyEm).
// Quad is the shaped ink bounding box for bound-font text, an estimated
// advance box for other text, or the exact image viewport for an icon.
// It is not a glyph outline or a mask of the icon's opaque pixels.
type ContentElement struct {
	Kind, Text     string
	Anchor         [2]float64
	Quad           [4][2]float64
	FontSize, DyEm float64
}

type ProjectedContent struct {
	Elements      []ContentElement
	EstimatedText bool
	Fit           ContentFitResult
}

// ShapeContent observes the same positions, wrapping and font shrink as the
// renderer. Canvas translation, parent transforms and screen-label injection
// belong to the scene layer. Arbitrary fonts still need real glyph metrics.
func ShapeContent(shape string, o ConvertOpts) ProjectedContent {
	shape = strings.ToLower(strings.TrimSpace(shape))
	out := ProjectedContent{Fit: ContentFitResult{Status: "unsupported", Reason: "no bounded top-face layout"}}
	add := func(p contentPlacement, ox, oy float64, tilted bool, dy, narrow float64, align string) {
		projectPoint := func(x, y float64) [2]float64 {
			if tilted {
				return [2]float64{ox + math.Round(cos30*1e4)/1e4*x - math.Round(cos30*1e4)/1e4*y, oy + sin30*x + sin30*y}
			}
			return [2]float64{ox + x, oy + y}
		}
		e := ContentElement{Anchor: projectPoint(p.X, p.Y), FontSize: p.FontSize, DyEm: dy, Text: p.Text}
		x, y, w, h := contentLocalBounds(p, dy, narrow, align)
		if p.Icon {
			e.Kind = "icon"
			e.DyEm = 0
		} else {
			e.Kind = "text"
			out.EstimatedText = out.EstimatedText || p.Font == nil || !p.Font.Measure(p.Text).Known
		}
		e.Quad = [4][2]float64{projectPoint(x, y), projectPoint(x+w, y), projectPoint(x+w, y+h), projectPoint(x, y+h)}
		out.Elements = append(out.Elements, e)
	}
	top := func(b IsoBoxOpts, ox, oy, w, d float64) {
		items := layoutTopContentFont(w, d, b.Label, b.LabelLines, b.Icon, b.IconScale, b.IconAnchor, b.IconOffX, b.IconOffY, b.FontSize, textmetrics.ResolveBuiltin(b.FontFamily, b.FontWeight))
		manual := strings.TrimSpace(b.Icon) != "" && (b.IconAnchor != "" || b.IconOffX != 0 || b.IconOffY != 0)
		visibleText := fillAlpha(b.FontColor).hi > 0
		out.Fit = measureTopContent(w, d, items, manual, visibleText)
		for _, p := range items {
			add(p, ox, oy, true, .35, textmetrics.NarrowTopFace, "middle")
		}
	}
	w, d, h := ShapeDimensions(shape, o)
	switch shape {
	case "composite":
		return out
	case "person":
		p := DefaultIsoPerson()
		applyPerson(o, &p)
		if items := personCaption(p); len(items) > 0 {
			for _, item := range items {
				add(item, (w-d)/2*cos30, (w+d)/2*sin30-h, false, 0, textmetrics.NarrowPersonLabel, "middle")
			}
		} else if strings.TrimSpace(p.Label) != "" {
			cx, cy := (w-d)/2*cos30, (w+d)/2*sin30+w/2*sin30/cos30+6+p.FontSize*.85
			add(contentPlacement{Text: p.Label, FontSize: p.FontSize}, cx, cy, false, 0, textmetrics.NarrowPersonLabel, "middle")
		}
	case "circle":
		s := DefaultIsoSphere()
		applySphere(o, &s)
		hasIcon, hasLabel := strings.TrimSpace(s.Icon) != "", strings.TrimSpace(s.Label) != ""
		if hasIcon {
			scale := s.IconScale
			if scale <= 0 {
				scale = .4
			}
			size := 2 * s.Radius * scale
			y := -size / 2
			if hasLabel {
				y = -size - 2
			}
			add(contentPlacement{X: -size / 2, Y: y, W: size, H: size, Icon: true}, 0, 0, false, 0, 0, "")
		}
		if items := sphereCaption(s); len(items) > 0 {
			for _, item := range items {
				add(item, 0, 0, false, 0, textmetrics.NarrowTopFace, "middle")
			}
		} else if hasLabel {
			y := 0.
			if hasIcon {
				y = s.FontSize * .8
			}
			add(contentPlacement{Text: s.Label, Y: y, FontSize: s.FontSize}, 0, 0, false, .35, textmetrics.NarrowTopFace, "middle")
		}
	case "cylinder", "stored_data", "queue":
		c := DefaultIsoCylinder()
		applyCylinder(o, &c)
		s := c.Radius / cos30
		b := IsoBoxOpts{Label: c.Label, Icon: c.Icon, IconScale: c.IconScale, FontSize: c.FontSize, FontColor: c.FontColor, FontFamily: c.FontFamily, FontWeight: c.FontWeight}
		top(b, (w-d)/2*cos30, (w+d)/2*sin30-h-s*sin30, s, s)
	default:
		b := DefaultIsoBox()
		applyBox(o, &b)
		b.Width, b.Depth, b.Height = w, d, h
		switch shape {
		case "iso_text", "title":
			if text := strings.TrimSpace(b.Label); text != "" {
				add(contentPlacement{Text: text, FontSize: b.FontSize}, 0, 0, true, 0, textmetrics.NarrowIsoText, "start")
			}
		case "triprism", "hexprism", "octprism", "custom_path":
			var cr ContentRect
			if shape == "custom_path" {
				cr = (CustomPathShapeProvider{}).ContentRectFor(w, d, h, o.Params)
			} else {
				cr = (PrismShapeProvider{}).ContentRectFor(w, d, h, map[string]any{"sides": map[string]int{"triprism": 3, "hexprism": 6, "octprism": 8}[shape]})
			}
			x, y := project(cr.X, cr.Y, h)
			top(b, x, y, cr.W, cr.H)
		case "wedge":
			cr := (WedgeShapeProvider{}).ContentRectFor(w, d, h, o.Params)
			c := wedgeProj(d, h, cr.X+cr.W/2, cr.Y+cr.H/2, h/2)
			top(b, c[0]-d*cos30-cr.W/2*cos30, c[1]-h-cr.H/2*sin30, cr.W, cr.H)
		case "cloud":
			if strings.TrimSpace(b.Icon) != "" {
				x, y := project(cloudContentX0*w, cloudContentY0*d, h)
				top(b, x, y, cloudContentW*w, cloudContentD*d)
			} else if items := cloudCaption(b); len(items) > 0 {
				x, y := cloudCaptionOrigin(b)
				for _, item := range items {
					add(item, x, y, true, 0, textmetrics.NarrowTopFace, "middle")
				}
			} else if strings.TrimSpace(b.Label) != "" {
				outline := sampleCloudOutline(w, d)
				x, y := 0., 0.
				for _, p := range outline {
					x += p[0]
					y += p[1]
				}
				x /= float64(len(outline))
				y /= float64(len(outline))
				sx, sy := project(x, y, h)
				add(contentPlacement{Text: b.Label, FontSize: b.FontSize}, sx, sy, true, .35, textmetrics.NarrowTopFace, "middle")
			}
		default:
			top(b, 0, -h, w, d)
		}
	}
	if len(out.Elements) == 0 {
		out.Fit = ContentFitResult{Status: "empty"}
	}
	return out
}
