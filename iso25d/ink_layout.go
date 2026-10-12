package iso25d

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

const minInkLineGapEm = .12
const inkLayoutGuard = .02 // room for two-decimal SVG placement rounding

// layoutBoundTopContent searches actual ink layouts only when every authored
// line has a bound font. It never moves manual icon placements, drops text,
// increases the requested font/icon, or invents metrics for fallback glyphs.
// The legacy result remains available if the content constraints are infeasible.
func layoutBoundTopContent(w, d float64, lines []string, hasIcon bool, scale, requested float64, font *textmetrics.BuiltinFont, legacy []contentPlacement) []contentPlacement {
	if font == nil || len(lines) == 0 || !finitePositive(w) || !finitePositive(d) || !finitePositive(requested) || requested < .01 {
		return legacy
	}
	hasInk := false
	for _, line := range lines {
		m := font.Measure(line)
		if !m.Known {
			return legacy
		}
		hasInk = hasInk || m.HasInk
	}
	if !hasInk {
		return legacy
	}
	pad := math.Max(6, .07*math.Min(w, d)) + inkLayoutGuard
	aw, ah := w-2*pad, d-2*pad
	if aw <= 0 || ah <= 0 {
		return legacy
	}
	icon := 0.0
	if hasIcon {
		icon = math.Min(math.Min(w, d)*scale, math.Min(aw, ah))
	}
	if !finiteNonnegative(icon) {
		return legacy
	}
	best := legacy
	bestValid := boundPlacementFits(w, d, best)
	// A full-size feasible layout wins ties, preserving an already-good result.
	if bestValid && placementFont(best) >= requested-1e-9 && placementIcon(best) >= icon-1e-9 {
		return best
	}
	consider := func(candidate []contentPlacement) {
		if candidate == nil || !boundPlacementFits(w, d, candidate) {
			return
		}
		if !bestValid || betterInkLayout(candidate, best) {
			best, bestValid = candidate, true
		}
	}
	sizes := []float64{icon}
	// The icon may regain size, but cannot be reduced below what the existing
	// fitting policy already selected just to buy a larger type size.
	if hasIcon {
		if old := placementIcon(legacy); old > 0 && old < icon {
			sizes = append(sizes, old)
		}
	}
	for _, size := range sizes {
		// A row contains one unbroken authored line. Solve both branches of
		// max(4, .5*fontSize) exactly; no trial glyph/rune sums are used.
		if hasIcon && len(lines) == 1 {
			m := font.Measure(lines[0])
			mw := m.CenteredWidth()
			mh := inkLineHeight(m)
			maxFont := requested
			if mw > 0 {
				maxFont = math.Min(maxFont, (aw-size-iconLabelGap)/mw)
			}
			maxFont = math.Min(maxFont, (aw-size)/(mw+.5))
			if mh > 0 {
				maxFont = math.Min(maxFont, ah/mh)
			}
			if maxFont >= math.Min(requested, minAutoFontSize) {
				fs := inkFontSize(maxFont)
				gap := math.Max(iconLabelGap, .5*fs)
				rw := size + gap + mw*fs
				x := (w - rw) / 2
				top, bottom := inkLineExtents(m)
				consider(quantizeInkPlacements([]contentPlacement{
					{X: x, Y: (d - size) / 2, W: size, H: size, Icon: true},
					{Text: lines[0], X: x + size + gap + mw*fs/2, Y: d/2 - (.35+(top+bottom)/2)*fs, FontSize: fs, Font: font},
				}))
			}
		}
		if bestValid && placementFont(best) >= requested-.009 && placementIcon(best) >= icon-.009 {
			return best
		}
		width := func(s string, fs float64) float64 { return font.Measure(s).CenteredWidth() * fs }
		lower := math.Min(requested, minAutoFontSize)
		upper := requested
		if unit := longestUnbreakable(lines, 1, width); unit > 0 {
			upper = math.Min(upper, math.Max(lower, aw/unit))
		}
		makeColumn := func(fs float64) []contentPlacement {
			fs = inkFontSize(fs)
			wrapped := wrapAll(lines, fs, aw, width)
			return inkColumn(w, d, wrapped, hasIcon, size, fs, font)
		}
		// Search descending sizes with a bounded budget. Reflow is discrete, so
		// do not use a global binary-search monotonicity assumption. Keep the best
		// tested result and refine the first feasible interval to SVG precision.
		step := math.Max(.25, (upper-lower)/64)
		prev := upper
		for i := 0; i <= 64; i++ {
			fs := math.Max(lower, upper-float64(i)*step)
			candidate := makeColumn(fs)
			if boundPlacementFits(w, d, candidate) {
				consider(candidate)
				if i > 0 {
					lo, hi := fs, prev
					for j := 0; j < 10 && hi-lo > .009; j++ {
						mid := (lo + hi) / 2
						trial := makeColumn(mid)
						if boundPlacementFits(w, d, trial) {
							consider(trial)
							lo = mid
						} else {
							hi = mid
						}
					}
				}
				break
			}
			if fs == lower {
				break
			}
			prev = fs
		}
	}
	return best
}

func inkColumn(w, d float64, lines []string, hasIcon bool, size, fs float64, font *textmetrics.BuiltinFont) []contentPlacement {
	if fs <= 0 || len(lines) == 0 {
		return nil
	}
	baselines := make([]float64, len(lines))
	tops := make([]float64, len(lines))
	bottoms := make([]float64, len(lines))
	for i, line := range lines {
		m := font.Measure(line)
		if !m.Known {
			return nil
		}
		tops[i], bottoms[i] = inkLineExtents(m)
		if i == 0 {
			baselines[i] = -tops[i] * fs
		} else {
			pitch := math.Max(lineHeightK*fs, (bottoms[i-1]-tops[i]+minInkLineGapEm)*fs+2*inkLayoutGuard)
			baselines[i] = baselines[i-1] + pitch
		}
	}
	height := baselines[len(lines)-1] + bottoms[len(lines)-1]*fs
	// The normal leading remains intact, including authored blank lines.
	// Ink can extend beyond the immediately adjacent line's nominal cell.
	top, bottom := math.Inf(1), math.Inf(-1)
	for i := range lines {
		top = math.Min(top, baselines[i]+tops[i]*fs)
		bottom = math.Max(bottom, baselines[i]+bottoms[i]*fs)
	}
	height = math.Max(height, bottom) - math.Min(0, top)
	totalHeight := height
	if hasIcon {
		totalHeight += size + iconLabelGap + 2*inkLayoutGuard
	}
	if totalHeight > d-2*(math.Max(6, .07*math.Min(w, d))+inkLayoutGuard) {
		return nil
	}
	start := (d - height) / 2
	var out []contentPlacement
	if hasIcon {
		block := height + size + iconLabelGap + 2*inkLayoutGuard
		start = (d - block) / 2
		out = append(out, contentPlacement{X: (w - size) / 2, Y: start, W: size, H: size, Icon: true})
		start += size + iconLabelGap + 2*inkLayoutGuard
	}
	for i, line := range lines {
		out = append(out, contentPlacement{Text: line, X: w / 2, Y: start + baselines[i] - .35*fs - math.Min(0, top), FontSize: fs, Font: font})
	}
	return quantizeInkPlacements(out)
}

func inkLineExtents(m textmetrics.GlyphMetrics) (float64, float64) {
	if !m.HasInk {
		return -.8, .2
	} // explicit empty/space-only lines retain a cell
	return m.Top, m.Bottom
}
func inkLineHeight(m textmetrics.GlyphMetrics) float64 { a, b := inkLineExtents(m); return b - a }
func inkFontSize(fs float64) float64                   { return math.Floor((fs+1e-9)*100) / 100 }
func quantizeInkPlacements(items []contentPlacement) []contentPlacement {
	for i := range items {
		p := &items[i]
		p.X = math.Round(p.X*100) / 100
		p.Y = math.Round(p.Y*100) / 100
		if p.Icon {
			p.W = math.Round(p.W*100) / 100
			p.H = p.W
		}
	}
	return items
}
func placementFont(items []contentPlacement) float64 {
	for _, p := range items {
		if !p.Icon {
			return p.FontSize
		}
	}
	return 0
}
func placementIcon(items []contentPlacement) float64 {
	for _, p := range items {
		if p.Icon {
			return p.W
		}
	}
	return 0
}
func betterInkLayout(a, b []contentPlacement) bool {
	af, bf := placementFont(a), placementFont(b)
	if math.Abs(af-bf) > .009 {
		return af > bf
	}
	ai, bi := placementIcon(a), placementIcon(b)
	if math.Abs(ai-bi) > .009 {
		return ai > bi
	}
	return len(a) < len(b)
}
func boundPlacementFits(w, d float64, items []contentPlacement) bool {
	if len(items) == 0 {
		return false
	}
	fit := measureTopContent(w, d, items, false, true)
	return fit.Status == "estimated-fit"
}
func finitePositive(v float64) bool    { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }
func finiteNonnegative(v float64) bool { return v >= 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }

func authoredContentLines(label string, lines []string) []string {
	if len(lines) > 0 {
		return lines
	}
	if strings.Contains(label, "\n") {
		return strings.Split(label, "\n")
	}
	if strings.TrimSpace(label) != "" {
		return []string{label}
	}
	return nil
}
