package iso25d

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// Bind only a complete authored block. Emission and observation must agree
// on whitespace, supported glyphs, the immutable font, and serialized size.
func captionFont(label string, lines []string, family, weight string, size float64) *textmetrics.BuiltinFont {
	font := textmetrics.ResolveBuiltin(family, weight)
	if font == nil || !finitePositive(size) || size < .01 {
		return nil
	}
	text := authoredContentLines(label, lines)
	visible := false
	for _, line := range text {
		m := font.Measure(line)
		if !m.Known {
			return nil
		}
		visible = visible || m.HasInk
	}
	if !visible {
		return nil
	}
	return font
}

// Free captions have no rectangular face budget. Retain the requested size
// and every authored line, use real ink separation, and let the viewport grow.
// base + em*size is the old single-line baseline; minTop protects the figure
// or icon above the caption. Sphere captions stay centered when possible.
func layoutBoundCaption(label, family, weight string, size, base, em, minTop float64, centered bool) []contentPlacement {
	font := captionFont(label, nil, family, weight, size)
	if font == nil {
		return nil
	}
	size = math.Round(size*100) / 100
	lines := strings.Split(label, "\n")
	items := make([]contentPlacement, len(lines))
	baseline, bottom := base+em*size, 0.
	for i, line := range lines {
		m := font.Measure(line)
		top, nextBottom := inkLineExtents(m)
		if i > 0 {
			baseline += math.Max(1.2*size, (bottom-top+.12)*size+.04)
		}
		items[i] = contentPlacement{Text: line, Y: baseline, FontSize: size, Font: font}
		bottom = nextBottom
	}
	shift := 0.
	if centered {
		shift = -(items[len(items)-1].Y - items[0].Y) / 2
	}
	top := math.Inf(1)
	for _, p := range items {
		_, y, _, _ := contentLocalBounds(p, 0, textmetrics.NarrowTopFace, "middle")
		top = math.Min(top, y+shift)
	}
	shift += math.Max(0, minTop-top)
	for i := range items {
		items[i].Y = math.Round((items[i].Y+shift)*100) / 100
	}
	return items
}

func captionBounds(items []contentPlacement) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range items {
		x, y, w, h := contentLocalBounds(p, 0, textmetrics.NarrowTopFace, "middle")
		x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x+w), math.Max(y1, y+h)
	}
	return
}

func writeBoundCaption(sb *strings.Builder, items []contentPlacement, ox, oy float64, color, face string, fonts fontResources) {
	if len(items) == 0 {
		return
	}
	font := items[0].Font
	css := fonts.css(font)
	// Leave room for the text after the large font payload. Otherwise a
	// handful of attributes can trigger another full-buffer allocation.
	sb.Grow(len(css) + len(items)*512 + 128)
	sb.WriteString(css)
	for _, p := range items {
		attr := ""
		if face != "" {
			attr = ` data-face="` + escapeAttr(face) + `"`
		}
		fmt.Fprintf(sb, `<text%s x="%.2f" y="%.2f" font-family="Source Sans Pro" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle" xml:space="preserve" xml:lang="en" lang="en" style="white-space:pre;font-kerning:normal;font-variant-ligatures:normal">%s</text>`, attr, ox+p.X, oy+p.Y, p.FontSize, font.Weight(), escapeAttr(color), escapeXML(p.Text))
	}
}

func personCaption(o IsoPersonOpts) []contentPlacement {
	ground := o.BodyHeight + o.BodyWidth/2*sin30/cos30
	return layoutBoundCaption(o.Label, o.FontFamily, o.FontWeight, o.FontSize, ground+6, .85, ground+6+.02, false)
}

func sphereCaption(o IsoSphereOpts) []contentPlacement {
	em, top := .35, math.Inf(-1)
	if strings.TrimSpace(o.Icon) != "" {
		em, top = 1.15, 2.02
	}
	return layoutBoundCaption(o.Label, o.FontFamily, o.FontWeight, o.FontSize, 0, em, top, true)
}

// Grow the standalone frame without changing world docking geometry. The
// same center is returned by PartOriginOffset when assembling a composite.
func sphereFrame(o IsoSphereOpts) (cx, cy, w, h float64) {
	r, m := o.Radius, o.Margin
	x0, y0, x1, y1 := -r, -r, r, r
	if items := sphereCaption(o); len(items) > 0 {
		a, b, c, d := captionBounds(items)
		x0, y0, x1, y1 = math.Min(x0, a), math.Min(y0, b), math.Max(x1, c), math.Max(y1, d)
		if strings.TrimSpace(o.Icon) != "" {
			scale := o.IconScale
			if scale <= 0 {
				scale = .4
			}
			size := 2 * r * scale
			x0, x1, y0 = math.Min(x0, -size/2), math.Max(x1, size/2), math.Min(y0, -size-2)
		}
	}
	return -x0 + m, -y0 + m, x1 - x0 + 2*m, y1 - y0 + 2*m
}

func boundCloudContent(o IsoBoxOpts) bool {
	return captionFont(o.Label, o.LabelLines, o.FontFamily, o.FontWeight, o.FontSize) != nil
}

func cloudCaption(o IsoBoxOpts) []contentPlacement {
	return layoutBoundCaption(strings.Join(authoredContentLines(o.Label, o.LabelLines), "\n"), o.FontFamily, o.FontWeight, o.FontSize, 0, .35, math.Inf(-1), true)
}

func cloudCaptionOrigin(o IsoBoxOpts) (float64, float64) {
	outline := sampleCloudOutline(o.Width, o.Depth)
	x, y := 0., 0.
	for _, p := range outline {
		x, y = x+p[0], y+p[1]
	}
	return project(x/float64(len(outline)), y/float64(len(outline)), o.Height)
}

func cloudContentBounds(o IsoBoxOpts, minX, minY, maxX, maxY float64) (float64, float64, float64, float64) {
	if !boundCloudContent(o) {
		return minX, minY, maxX, maxY
	}
	ox, oy := project(cloudContentX0*o.Width, cloudContentY0*o.Depth, o.Height)
	var items []contentPlacement
	dy := .35
	if strings.TrimSpace(o.Icon) == "" {
		// Preserve the preexisting free caption's centroid and requested size.
		// A smaller inscribed rectangle must not silently shrink readable text.
		items = cloudCaption(o)
		ox, oy = cloudCaptionOrigin(o)
		dy = 0
	} else {
		font := textmetrics.ResolveBuiltin(o.FontFamily, o.FontWeight)
		items = layoutTopContentFont(cloudContentW*o.Width, cloudContentD*o.Depth, o.Label, o.LabelLines, o.Icon, o.IconScale, o.IconAnchor, o.IconOffX, o.IconOffY, o.FontSize, font)
	}
	for _, p := range items {
		x, y, w, h := contentLocalBounds(p, dy, textmetrics.NarrowTopFace, "middle")
		for _, point := range [][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
			x, y := ox+.8660*(point[0]-point[1]), oy+.5*(point[0]+point[1])
			minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
		}
	}
	return minX, minY, maxX, maxY
}
