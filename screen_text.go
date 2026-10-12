package isotopo

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// screenTextBlock owns the geometry used by the chip, its hit target, text,
// obstacle placement and export bounds. Unknown fonts retain the legacy model.
type screenTextBlock struct {
	width, height, size float64
	font                *textmetrics.BuiltinFont
	lines               []screenTextLine
}

type screenTextLine struct {
	text     string
	baseline float64 // relative to the chip's top
}

func layoutScreenText(text, family, weight string, size float64) screenTextBlock {
	legacy := screenTextBlock{
		width:  textmetrics.TextPxWideBytes(text, size, textmetrics.NarrowScreenLabel) + 16,
		height: size + 10, size: size,
	}
	font := textmetrics.ResolveBuiltin(family, weight)
	if font == nil || math.IsNaN(size) || math.IsInf(size, 0) || size < .01 {
		return legacy
	}
	// Geometry uses the exact size that the SVG will submit.
	size = math.Round(size*100) / 100
	lines := strings.Split(text, "\n")
	metrics := make([]textmetrics.GlyphMetrics, len(lines))
	for i, line := range lines {
		metrics[i] = font.Measure(line)
		if !metrics[i].Known {
			return legacy // no partially certified fallback block
		}
	}
	out := screenTextBlock{font: font, size: size}
	minY, maxY, width := math.Inf(1), math.Inf(-1), 0.0
	baseline, previousBottom := 0.0, 0.0
	for i, line := range lines {
		m := metrics[i]
		top, bottom := m.Top*size, m.Bottom*size
		if !m.HasInk {
			top, bottom = -.8*size, .2*size // preserve authored blank lines
		}
		if i > 0 {
			baseline += math.Max(1.2*size, previousBottom-top+.12*size+.04)
		}
		out.lines = append(out.lines, screenTextLine{text: line, baseline: baseline})
		minY, maxY = math.Min(minY, baseline+top), math.Max(maxY, baseline+bottom)
		previousBottom = bottom
		width = math.Max(width, m.CenteredWidth()*size)
	}
	// Preserve nominal line height and padding, but expand for tall ink. Round
	// dimensions outward so emitted rects cannot become smaller than the budget.
	out.width = math.Ceil((width+16)*100) / 100
	out.height = math.Ceil((math.Max(size, maxY-minY)+10)*100) / 100
	shift := (out.height-(maxY-minY))/2 - minY
	for i := range out.lines {
		out.lines[i].baseline += shift
	}
	return out
}
