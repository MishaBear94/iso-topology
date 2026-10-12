package iso25d

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// ContentFitResult observes the FINAL selected top-face layout in its local
// face coordinates. It assesses the preferred padding budget. Bound-font items use shaped ink;
// arbitrary fonts use estimates. It does not certify legibility or visibility.
// Status is empty, estimated-fit, estimated-overflow, or unsupported.
type ContentFitResult struct {
	Status    string           `json:"status"`
	Reason    string           `json:"reason,omitempty"`
	FaceWidth float64          `json:"face_width,omitempty"`
	FaceDepth float64          `json:"face_depth,omitempty"`
	Padding   float64          `json:"padding,omitempty"`
	Manual    bool             `json:"manual,omitempty"`
	Gaps      []ContentFitGap  `json:"gaps,omitempty"`
	Items     []ContentFitItem `json:"items,omitempty"`
}

// ContentFitGap records a shortfall in preferred ink separation. Before and
// After refer to ProjectedContent.Elements. Only bound-font text participates;
// arbitrary font estimates cannot certify real interline/icon separation.
type ContentFitGap struct {
	Before    int     `json:"before"`
	After     int     `json:"after"`
	Kind      string  `json:"kind"`
	Required  float64 `json:"required"`
	Actual    float64 `json:"actual"`
	Shortfall float64 `json:"shortfall"`
}

// ContentFitItem refers to an index in ProjectedContent.Elements. Bounds use
// shaped glyph ink, an unbound text advance estimate, or the image viewport,
// in local top-face coordinates. TextMetrics identifies the measurement.
// Overflow gives nonnegative excursions beyond the preferred padded area,
// ordered left, top, right, bottom. Invalid marks a nonpositive visible size.
type ContentFitItem struct {
	Element     int         `json:"element"`
	Kind        string      `json:"kind"`
	Text        string      `json:"text,omitempty"`
	Bounds      ContentRect `json:"bounds"`
	FontSize    float64     `json:"font_size,omitempty"`
	Overflow    [4]float64  `json:"overflow"`
	TextMetrics string      `json:"text_metrics,omitempty"`
	FontHash    string      `json:"font_hash,omitempty"`
	Invalid     bool        `json:"invalid,omitempty"`
}

// contentLocalBounds is shared with projected content observation. These are
// estimates for text, including the emitted baseline/dy convention.
func contentLocalBounds(p contentPlacement, dy, narrow float64, align string) (x, y, w, h float64) {
	x, y, w, h = p.X, p.Y, p.W, p.H
	if !p.Icon {
		if p.Font != nil {
			m := p.Font.Measure(p.Text)
			if m.Known && m.HasInk {
				x += m.Left * p.FontSize
				if align == "middle" {
					x -= m.Advance * p.FontSize / 2
				}
				y += (dy + m.Top) * p.FontSize
				return x, y, (m.Right - m.Left) * p.FontSize, (m.Bottom - m.Top) * p.FontSize
			}
		}
		w, h = textmetrics.TextPx(p.Text, p.FontSize, narrow), p.FontSize
		if align == "middle" {
			x -= w / 2
		}
		y += (dy - .8) * p.FontSize
	}
	return
}

func measureTopContent(w, d float64, items []contentPlacement, manual, visibleText bool) ContentFitResult {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(w) || !finite(d) || w <= 0 || d <= 0 {
		return ContentFitResult{Status: "unsupported", Reason: "invalid content area"}
	}
	pad := math.Max(6, .07*math.Min(w, d))
	out := ContentFitResult{Status: "empty", FaceWidth: w, FaceDepth: d, Padding: pad, Manual: manual}
	for i, p := range items {
		if !p.Icon && (!visibleText || strings.TrimSpace(p.Text) == "") {
			continue
		}
		x, y, width, height := contentLocalBounds(p, .35, textmetrics.NarrowTopFace, "middle")
		if !finite(x) || !finite(y) || !finite(width) || !finite(height) {
			return ContentFitResult{Status: "unsupported", Reason: "non-finite content bounds", Manual: manual}
		}
		e := ContentFitItem{Element: i, Kind: "text", Text: p.Text, Bounds: ContentRect{x, y, width, height}, FontSize: p.FontSize}
		if !p.Icon {
			e.TextMetrics = "estimated-advance"
			if p.Font != nil && p.Font.Measure(p.Text).Known {
				e.TextMetrics = "shaped-ink"
				e.FontHash = p.Font.Hash()
			}
		}
		if p.Icon {
			e.Kind, e.FontSize = "icon", 0
		}
		e.Invalid = width <= 0 || height <= 0
		e.Overflow = [4]float64{math.Max(0, pad-x), math.Max(0, pad-y), math.Max(0, x+width-(w-pad)), math.Max(0, y+height-(d-pad))}
		if out.Status == "empty" {
			out.Status = "estimated-fit"
		}
		if e.Invalid {
			out.Status = "estimated-overflow"
		}
		for _, excess := range e.Overflow {
			if excess > .01 { // two-decimal SVG coordinate resolution
				out.Status = "estimated-overflow"
			}
		}
		out.Items = append(out.Items, e)
	}
	if out.Status == "estimated-overflow" && (w <= 2*pad || d <= 2*pad) {
		out.Reason = "preferred padding leaves no content area"
	}

	out.Gaps = contentInkGaps(out.Items)
	if len(out.Gaps) > 0 {
		out.Status = "estimated-overflow"
		if out.Reason != "" {
			out.Reason += "; "
		}
		out.Reason += "preferred ink separation not met"
	}
	return out
}

// Text lines have authored order, so their clearance is vertical and directed.
// An icon may be beside or above text: either axis can separate their boxes.
func contentInkGaps(items []ContentFitItem) []ContentFitGap {
	var out []ContentFitGap
	add := func(a, b ContentFitItem, kind string, required, actual float64) {
		if required-actual > .01 {
			out = append(out, ContentFitGap{Before: a.Element, After: b.Element, Kind: kind, Required: required, Actual: actual, Shortfall: required - actual})
		}
	}
	last := -1
	for i, a := range items {
		if a.Kind != "text" {
			continue
		}
		if last >= 0 && a.TextMetrics == "shaped-ink" && items[last].TextMetrics == "shaped-ink" {
			b := items[last]
			add(b, a, "text:text", minInkLineGapEm*math.Max(a.FontSize, b.FontSize), a.Bounds.Y-(b.Bounds.Y+b.Bounds.H))
		}
		last = i
		if a.TextMetrics != "shaped-ink" {
			continue
		}
		for _, b := range items {
			if b.Kind == "icon" {
				dx := math.Max(a.Bounds.X-(b.Bounds.X+b.Bounds.W), b.Bounds.X-(a.Bounds.X+a.Bounds.W))
				dy := math.Max(a.Bounds.Y-(b.Bounds.Y+b.Bounds.H), b.Bounds.Y-(a.Bounds.Y+a.Bounds.H))
				add(b, a, "icon:text", iconLabelGap, math.Max(dx, dy))
			}
		}
	}
	return out
}
