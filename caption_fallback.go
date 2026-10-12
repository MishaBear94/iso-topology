package isotopo

import (
	"math"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// captionLiftFallback preserves the pre-region lift protection only when a
// later body's filled coverage is unresolved. This legacy box heuristic must
// not feed measured occlusion, scores or repair acceptance. Empty known fills
// never enter it. Remove it as the remaining shapes/materials gain coverage.
func captionLiftFallback(label, body *CompositePart) bool {
	if body.Offset == nil {
		return false
	}
	size := 11.0
	if label.Style != nil && label.Style.Text != nil && label.Style.Text.Size != nil && *label.Style.Text.Size > 0 {
		size = *label.Style.Text.Size
	}
	tw := textWidthPx(label.Label, size, textmetrics.NarrowPartLabel)
	var ox, oy, oz float64
	if label.Offset != nil {
		ox, oy, oz = label.Offset.WX, label.Offset.WY, label.Offset.WZ
	}
	if occOverlap(occRect{ox, oy, ox + tw, oy + size}, occTopBox(body)) {
		return true
	}
	labelBox := occRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range [4][3]float64{{ox, oy, oz}, {ox + tw, oy, oz}, {ox, oy, oz + size}, {ox + tw, oy, oz + size}} {
		x, y := projectIso(c[0], c[1], c[2])
		labelBox.x0, labelBox.x1 = math.Min(labelBox.x0, x), math.Max(labelBox.x1, x)
		labelBox.y0, labelBox.y1 = math.Min(labelBox.y0, y), math.Max(labelBox.y1, y)
	}
	w, d, h := occDims(body)
	ox, oy, oz = body.Offset.WX, body.Offset.WY, body.Offset.WZ
	x0, _ := projectIso(ox, oy+d, oz)
	x1, _ := projectIso(ox+w, oy, oz)
	_, y0 := projectIso(ox, oy, oz+h)
	_, y1 := projectIso(ox+w, oy+d, oz)
	return occOverlap(labelBox, occRect{x0, y0, x1, y1})
}
