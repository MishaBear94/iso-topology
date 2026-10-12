package isotopo

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
	"github.com/rivo/uniseg"
)

// PlanLabel describes a submitted top-projection node title. PartIndex is the
// preorder footprint index (containers included, stacks collapsed). Budget and
// ink use final SVG user coordinates. Status certifies only budget containment,
// not visibility against other scene objects. Unmeasured legacy titles have no
// invented ink geometry and report the font actually requested by their SVG.
type PlanLabel struct {
	PartID      string              `json:"part_id,omitempty"`
	PartIndex   int                 `json:"part_index"`
	Text        string              `json:"text"`
	FontFamily  string              `json:"font_family"`
	FontWeight  string              `json:"font_weight"`
	FontSize    float64             `json:"font_size"`
	TextMetrics string              `json:"text_metrics"`
	FontHash    string              `json:"font_hash,omitempty"`
	Budget      ScreenLabelRect     `json:"budget"`
	Status      string              `json:"status"`   // within-budget | overflow | unmeasured
	Overflow    [4]float64          `json:"overflow"` // left, top, right, bottom ink excess
	Lines       []PlanLabelLine     `json:"lines,omitempty"`
	Placement   *PlanLabelPlacement `json:"placement,omitempty"`
}

// SourceLine is the zero-based authored newline index. Concatenating fragments
// of each source line preserves every character, including whitespace.
type PlanLabelLine struct {
	ScreenLabelLine
	SourceLine int `json:"source_line"`
}

type planTextLayout struct {
	record PlanLabel
	font   *textmetrics.BuiltinFont
	left   bool
	ink    []screenRect
}

func layoutPlanLabels(rects []planRect, tx, ty float64) []planTextLayout {
	out := make([]planTextLayout, len(rects))
	bottoms := make([]float64, len(rects))
	for i, r := range rects {
		bottoms[i] = r.y + r.d - 6
	}
	for _, r := range rects {
		if r.parent >= 0 && r.parent < len(rects) {
			// Reserve the child-free title band, including the child's outline
			// and a reading gap. Nested groups constrain their own title band.
			bottoms[r.parent] = math.Min(bottoms[r.parent], r.y-planLineW(r)/2-6)
		}
	}
	for i, r := range rects {
		if r.label == "" {
			continue
		}
		b := ScreenLabelRect{svgCoord(r.x + tx + 8), svgCoord(r.y + ty + 6), math.Max(0, r.w-16), math.Max(0, r.d-12)}
		if r.container {
			b.Height = math.Max(0, bottoms[i]-r.y-6)
		}
		p := PlanLabel{PartID: r.id, PartIndex: i, Text: r.label, Budget: b,
			FontFamily: "Inter,system-ui,sans-serif", FontWeight: "400", FontSize: planFontLabel,
			TextMetrics: "unmeasured", Status: "unmeasured"}
		if r.container {
			p.FontWeight, p.FontSize = "600", planFontTitle
		}
		out[i] = planTextLayout{record: p, left: r.container}
		block := layoutScreenText(r.label, r.family, r.weight, r.fontSize)
		if block.font == nil {
			continue
		}
		wrapped, sources := wrapPlanText(r.label, block.font, block.size, b.Width, r.container)
		candidate := layoutScreenText(wrapped, r.family, r.weight, block.size)
		// Width wrapping must not manufacture a vertical failure. Preserve
		// authored lines if the wrapped result cannot fit at the requested size.
		if candidate.font != nil && candidate.height-10 <= b.Height {
			block = candidate
		} else {
			sources = make([]int, len(block.lines))
			for j := range sources {
				sources[j] = j
			}
		}
		p.FontFamily, p.FontWeight, p.FontSize = "Source Sans Pro", block.font.Weight(), block.size
		p.TextMetrics, p.FontHash, p.Status = "shaped-ink", block.font.Hash(), "within-budget"
		cx, y := r.x+tx+r.w/2, r.y+ty+r.d/2-block.height/2
		if r.container {
			y = b.Y - 5
		}
		for j, line := range block.lines {
			m := block.font.Measure(line.text)
			x := cx
			anchor := m.Advance / 2
			if r.container {
				x, anchor = b.X-math.Min(0, m.Left)*block.size, 0
			}
			x, baseline := svgCoord(x), svgCoord(y+line.baseline)
			advance := m.Advance * block.size
			l := PlanLabelLine{ScreenLabelLine: ScreenLabelLine{Text: line.text, X: x, Baseline: baseline, Advance: &advance}, SourceLine: sources[j]}
			if m.HasInk {
				ink := ScreenLabelRect{x + (m.Left-anchor)*block.size, baseline + m.Top*block.size, (m.Right - m.Left) * block.size, (m.Bottom - m.Top) * block.size}
				l.InkBounds = &ink
				excess := [4]float64{b.X - ink.X, b.Y - ink.Y, ink.X + ink.Width - b.X - b.Width, ink.Y + ink.Height - b.Y - b.Height}
				for side, v := range excess {
					p.Overflow[side] = math.Max(p.Overflow[side], v)
				}
			}
			p.Lines = append(p.Lines, l)
		}
		for _, v := range p.Overflow {
			if v > 0 {
				p.Status = "overflow"
			}
		}
		out[i] = planTextLayout{record: p, font: block.font, left: r.container}
		out[i].ink = out[i].inkRects()
	}
	return out
}

// wrapPlanText partitions contiguous grapheme sequences; it never trims spaces,
// splits combining marks, shrinks the font or discards a suffix. Shaping work
// is bounded by measured input bytes. When exhausted, retain remaining source.
func wrapPlanText(text string, font *textmetrics.BuiltinFont, size, width float64, left bool) (string, []int) {
	var lines []string
	var sources []int
	remaining := 65536
	appendLine := func(s string, source int) { lines = append(lines, s); sources = append(sources, source) }
	measure := func(s string) float64 {
		remaining -= len(s)
		m := font.Measure(s)
		if left {
			return (math.Max(m.Advance, m.Right) - math.Min(0, m.Left)) * size
		}
		return m.CenteredWidth() * size
	}
	for source, s := range strings.Split(text, "\n") {
		if width <= 0 || len(s) > remaining || measure(s) <= width {
			appendLine(s, source)
			continue
		}
		start := 0
		for start < len(s) && remaining > 0 {
			g := uniseg.NewGraphemes(s[start:])
			last, space, cut := start, 0, 0
			for g.Next() {
				_, rel := g.Positions()
				end := start + rel
				if end-start > remaining {
					break
				}
				if measure(s[start:end]) > width && last > start {
					cut = last
					if space > start {
						cut = space
					}
					break
				}
				last = end
				if g.Str() == " " {
					space = end
				}
			}
			if cut == 0 {
				break
			}
			appendLine(s[start:cut], source)
			start = cut
		}
		appendLine(s[start:], source)
	}
	return strings.Join(lines, "\n"), sources
}

func (p planTextLayout) inkRects() []screenRect {
	if p.ink != nil {
		return p.ink
	}
	var out []screenRect
	for _, line := range p.record.Lines {
		if b := line.InkBounds; b != nil {
			out = append(out, screenRect{b.X, b.Y, b.X + b.Width, b.Y + b.Height})
		}
	}
	return out
}

func emitPlanLabel(sb *strings.Builder, p planTextLayout, color string, fonts map[string]bool) {
	if !fonts[p.font.Hash()] {
		sb.WriteString(p.font.CSS())
		fonts[p.font.Hash()] = true
	}
	anchor := "middle"
	if p.left {
		anchor = "start"
	}
	fmt.Fprintf(sb, `<g data-plan-label="%d" data-plan-label-fit="%s">`, p.record.PartIndex, p.record.Status)
	for _, line := range p.record.Lines {
		fmt.Fprintf(sb, `<text x="%.2f" y="%.2f" font-family="Source Sans Pro" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="%s" xml:space="preserve" xml:lang="en" lang="en" style="white-space:pre;font-kerning:normal;font-variant-ligatures:normal">%s</text>`, line.X, line.Baseline, p.record.FontSize, p.record.FontWeight, planEsc(color), anchor, escapeXML(line.Text))
	}
	sb.WriteString(`</g>`)
}

func addPlanLabelFitDefects(d visualDefects, rects []planRect) {
	for _, p := range layoutPlanLabels(rects, 0, 0) {
		for side, v := range p.record.Overflow {
			if v > 0 {
				d[visualDefect{kind: "plan-label-fit", a: p.record.PartID, edge: p.record.PartIndex, peer: side}] = v
			}
		}
	}
}
