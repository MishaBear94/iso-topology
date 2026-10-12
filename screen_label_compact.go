package isotopo

import (
	"math"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// screenLabelProtection supplements the legacy placement obstacles for the
// optional compaction pass. Unmeasured text prevents compaction; its estimated
// rectangle is not a safe claim about what the browser will paint.
type screenLabelProtection struct {
	rects     []screenRect
	uncertain bool
	prepare   func(*screenLabelProtection)
}

type screenLabelPlacement struct {
	index          int
	block          screenTextBlock
	family, weight string
	x, y           float64
	preferred      bool
}

func (p screenLabelPlacement) rect() screenRect {
	// Match the pointer rectangle actually submitted by the emitter.
	x := svgCoord((p.x + p.block.width/2) - p.block.width/2)
	y := svgCoord(p.y)
	return screenRect{x, y, x + svgCoord(p.block.width), y + svgCoord(p.block.height)}
}

func protectScreenLabelContent(parts []iso25d.CompositePart, infos []partInfo, protection *screenLabelProtection) {
	tx, ty := partsScreenOrigin(infos)
	for _, p := range parts {
		o := p.Opts
		// These surfaces contain unmeasured text or unbounded visual effects.
		// Keep the established layout until those producers supply envelopes.
		if len(o.Rows) > 0 || o.Header != "" || o.Blur > 0 || len(o.FaceSurfaces) > 0 || o.ShadowColor != "" || o.BackglowColor != "" {
			protection.uncertain = true
		}
		content := iso25d.ShapeContent(p.Shape, o)
		protection.uncertain = protection.uncertain || content.EstimatedText
		ox, oy := projectIso(p.OffWX, p.OffWY, p.OffWZ)
		add := func(points [][2]float64, margin float64) {
			if len(points) == 0 {
				return
			}
			r := screenRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
			for _, pt := range points {
				r.x0, r.y0 = math.Min(r.x0, pt[0]+ox+tx), math.Min(r.y0, pt[1]+oy+ty)
				r.x1, r.y1 = math.Max(r.x1, pt[0]+ox+tx), math.Max(r.y1, pt[1]+oy+ty)
			}
			protection.rects = append(protection.rects, r.inflate(margin))
		}
		body := iso25d.ShapeBody(p.Shape, o)
		stroke := math.Max(1.5, math.Max(o.StrokeWidth, math.Max(o.TopStroke.Width, math.Max(o.LeftStroke.Width, o.RightStroke.Width))))
		stroke = math.Max(stroke, o.OutlineWidth)
		for _, poly := range body.Polygons {
			add(poly, stroke/2+body.MaxError+.03)
		}
		for _, element := range content.Elements {
			add(element.Quad[:], .03)
		}
	}
}

// compactScreenLabels starts from the whole feasible placement, so improving
// an early label cannot push a later label away. Each accepted move keeps its
// side/alignment, shortens owner distance, preserves every peer's association
// margin, and clears all current labels and producer-supplied obstacles.
// Four deterministic passes bound work; no label is moved to make room for
// another. Unknown screen blocks retain the entire baseline arrangement.
func compactScreenLabels(labels []screenLabelPlacement, bodies, obstacles []screenRect, protection screenLabelProtection) {
	if protection.uncertain {
		return
	}
	eligible := false
	rects := make([]screenRect, len(labels))
	for i, p := range labels {
		if p.block.font == nil {
			return
		}
		rects[i] = p.rect()
		_, _, _, ok := compactLabelRay(rects[i], bodies[p.index])
		eligible = eligible || ok && !p.preferred
	}
	if !eligible {
		return
	}
	if protection.prepare != nil {
		protection.prepare(&protection)
	}
	if protection.uncertain {
		return
	}
	static := append(append([]screenRect(nil), obstacles...), protection.rects...)
	// Rectangles only change at acceptance. Formatting all peer rectangles for
	// each candidate caused quadratic string allocation on large diagrams.
	all := make([]screenRect, len(static), len(static)+len(labels))
	copy(all, static)
	for pass := 0; pass < 4; pass++ {
		changed := false
		for i := range labels {
			p := &labels[i]
			if p.preferred {
				continue
			}
			old, owner := rects[i], bodies[p.index]
			start, dx, dy, ok := compactLabelRay(old, owner)
			if !ok {
				continue
			}
			all = all[:len(static)]
			for j, other := range rects {
				if j != i {
					all = append(all, other)
				}
			}
			x, y := freeTextBoxOnRay(start, dx, dy, all)
			candidate := *p
			// Keep the untouched axis before SVG rounding. Reconstructing it
			// from a rounded rect can shift a text anchor by one decimal step.
			if dx != 0 {
				candidate.x = x
			}
			if dy != 0 {
				candidate.y = y
			}
			next := candidate.rect()
			oldDistance, nextDistance := screenRectDistance(old, owner), screenRectDistance(next, owner)
			if nextDistance >= oldDistance-1 || collides(next, all) {
				continue
			}
			// Staying inside the old label+owner envelope prevents growth on any
			// side of the canvas and preserves the chosen cardinal direction.
			if next.x0 < math.Min(old.x0, owner.x0)-.01 || next.y0 < math.Min(old.y0, owner.y0)-.01 ||
				next.x1 > math.Max(old.x1, owner.x1)+.01 || next.y1 > math.Max(old.y1, owner.y1)+.01 {
				continue
			}
			safe := true
			for j, peer := range bodies {
				if j != p.index && screenRectDistance(next, peer)-nextDistance < screenRectDistance(old, peer)-oldDistance-.01 {
					safe = false
					break
				}
			}
			if safe {
				*p = candidate
				rects[i] = next
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

func screenRectDistance(a, b screenRect) float64 {
	return math.Hypot(math.Max(0, math.Max(a.x0-b.x1, b.x0-a.x1)), math.Max(0, math.Max(a.y0-b.y1, b.y0-a.y1)))
}

// Compact only the centered cardinal placements made by the fallback search.
// A clean authored/preferred hanging caption retains its exact position.
func compactLabelRay(box, owner screenRect) (screenRect, float64, float64, bool) {
	w, h := box.x1-box.x0, box.y1-box.y0
	x, y, dx, dy := box.x0, box.y0, 0.0, 0.0
	switch {
	case math.Abs(box.cx()-owner.cx()) < .011 && box.y0 > owner.y1+11:
		y, dy = owner.y1+10, 1
	case math.Abs(box.cx()-owner.cx()) < .011 && box.y1 < owner.y0-11:
		y, dy = owner.y0-10-h, -1
	case math.Abs(box.cy()-owner.cy()) < .011 && box.x0 > owner.x1+11:
		x, dx = owner.x1+10, 1
	case math.Abs(box.cy()-owner.cy()) < .011 && box.x1 < owner.x0-11:
		x, dx = owner.x0-10-w, -1
	default:
		return box, 0, 0, false
	}
	return screenRect{x, y, x + w, y + h}, dx, dy, true
}
