package isotopo

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

type planPill struct {
	x, y, w        float64
	bg, ink, label string
	c              *Connector
	index          int
}
type planContentLayout struct {
	texts       []planTextLayout
	pills       []planPill
	requests    []connectorTextRequest
	placements  []connectorTextPlacement
	targets     []connectorTextRectTarget
	strokes     []connectorTextStroke
	bounds      []screenRect
	hasTitleInk bool
}

func planFrame(rects []planRect, edges []planEdge, canvas *Canvas) (float64, float64, float64, float64) {
	// World bbox over every footprint.
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, r := range rects {
		minX, minY = math.Min(minX, r.x), math.Min(minY, r.y)
		maxX, maxY = math.Max(maxX, r.x+r.w), math.Max(maxY, r.y+r.d)
	}
	for _, e := range edges {
		if len(e.c.Waypoints3D) > 0 || isFixedPortRef(e.c.From) || isFixedPortRef(e.c.To) {
			points := e.pts
			if e.refined {
				points = e.framePts
			}
			for _, p := range points {
				minX, minY = math.Min(minX, p[0]), math.Min(minY, p[1])
				maxX, maxY = math.Max(maxX, p[0]), math.Max(maxY, p[1])
			}
		}
	}
	pad := planPad
	if canvas != nil && canvas.Padding > 0 {
		pad = canvas.Padding
	}
	tx := -minX + pad
	ty := -minY + pad
	W := (maxX - minX) + 2*pad
	H := (maxY - minY) + 2*pad

	return tx, ty, W, H
}
func planEdgeStyle(c *Connector) (string, string, float64, string) {
	stroke := "#27364b"
	casing := "#eef2f8"
	if c.Stroke != nil && c.Stroke.Color != "" {
		stroke = c.Stroke.Color
		// keep the halo opposite the ink so a custom colour still pops.
		if planIsDark(stroke) {
			casing = "#f1f5f9"
		} else {
			casing = "#1e293b"
		}
	}
	sw := 2.4 // bolder than iso hairlines so the flow reads at a glance
	if c.Stroke != nil && c.Stroke.Width != nil && *c.Stroke.Width > 0 {
		sw = *c.Stroke.Width
	}
	dash := ""
	if c.Stroke != nil && c.Stroke.Dash == "dashed" {
		dash = ` stroke-dasharray="6 5"`
	} else if c.Stroke != nil && c.Stroke.Dash == "dotted" {
		dash = ` stroke-dasharray="2 4"`
	}
	return stroke, casing, sw, dash
}

// A nil builder observes the same pill arithmetic without formatting SVG.
func planLegacyPill(sb *strings.Builder, p planPill) (screenRect, screenRect, bool) {

	if p.c.LabelFontFamily != "" || p.c.LabelFontWeight != "" || p.c.LabelFontSize > 0 {
		size := p.c.LabelFontSize
		if size <= 0 {
			size = 10
		}
		family, weight := connectorFont(p.c)
		w := textWidthPx(p.label, size, textmetrics.NarrowConnectorLabel) + 12
		h := math.Max(16, size+8)
		x, y := p.x+p.w/2, p.y+8
		if sb != nil {
			fmt.Fprintf(sb, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="4" ry="4" fill="%s" stroke="#e2e8f0" stroke-width="1"/><text x="%.2f" y="%.2f" font-family="%s" font-weight="%s" font-size="%.2f" text-anchor="middle" dominant-baseline="central" fill="%s">%s</text>`, x-w/2, y-h/2, w, h, planEsc(p.bg), x, y, planEsc(family), planEsc(weight), size, planEsc(p.ink), planEsc(p.label))
		}
		r := screenRect{svgCoord(x - w/2), svgCoord(y - h/2), svgCoord(x-w/2) + svgCoord(w), svgCoord(y-h/2) + svgCoord(h)}.inflate(.5)

		return r, r, true
	}
	obstacle := screenRect{p.x, p.y, p.x + p.w, p.y + 16}.inflate(.51)
	x, y, w := svgCoord(p.x), svgCoord(p.y), svgCoord(p.w)
	box := screenRect{x, y, x + w, y + 16}.inflate(.5)

	if sb != nil {
		fmt.Fprintf(sb,
			`<rect x="%.2f" y="%.2f" width="%.2f" height="16" rx="4" ry="4" fill="%s" stroke="#e2e8f0" stroke-width="1"/>`+
				`<text x="%.2f" y="%.2f" font-size="10" text-anchor="middle" dominant-baseline="central" fill="%s">%s</text>`,
			p.x, p.y, p.w, planEsc(p.bg), p.x+p.w/2, p.y+8, planEsc(p.ink), planEsc(p.label))
	}
	return obstacle, box, false

}
func resolvePlanContent(rects []planRect, edges []planEdge, ox, oy float64, collectConflicts bool) planContentLayout {
	tx := func(x float64) float64 { return x + ox }
	ty := func(y float64) float64 { return y + oy }
	texts := layoutPlanLabels(rects, ox, oy)
	// Connectors: orthogonal face-to-face routes between footprints. Collect the
	// label placements to emit after the leaf boxes.

	var pills []planPill
	var measuredLabels []connectorTextRequest
	var labelObstacles []connectorTextObstacle
	var labelStrokes []connectorTextStroke
	var bodyRects []screenRect
	var labelTargets []connectorTextRectTarget
	var titleObstacles []planTitleObstacle
	var measuredBounds []screenRect
	var titleBounds *screenRect
	for i, r := range rects {
		if !r.container {
			box := screenRect{tx(r.x), ty(r.y), tx(r.x + r.w), ty(r.y + r.d)}.inflate(planLineW(r) / 2)
			bodyRects = append(bodyRects, box)
			if collectConflicts {
				labelTargets = append(labelTargets, connectorTextBodyTarget(r.id, i, box))
			}
		}
		for _, box := range texts[i].inkRects() {
			bodyRects = append(bodyRects, box)
			if titleBounds == nil {
				copy := box
				titleBounds = &copy
			} else {
				titleBounds.x0, titleBounds.y0 = math.Min(titleBounds.x0, box.x0), math.Min(titleBounds.y0, box.y0)
				titleBounds.x1, titleBounds.y1 = math.Max(titleBounds.x1, box.x1), math.Max(titleBounds.y1, box.y1)
			}
			if collectConflicts {
				labelTargets = append(labelTargets, connectorTextBodyTarget(r.id, i, box))
			}
		}
	}
	if titleBounds != nil {
		measuredBounds = append(measuredBounds, *titleBounds)
	}
	measureLabels := false
	for _, edge := range edges {
		measureLabels = measureLabels || edge.c.LabelFontFamily != "" && edge.c.Label != ""
	}

	for _, e := range edges {
		c := e.c
		var points [][2]float64
		if measureLabels || titleBounds != nil || e.refined {
			points = planRouteScreenPoints(e, ox, oy)
			obstacleWidth := planRouteWidth(c)
			strokes := connectorTextStrokes(e.ci, points, false, obstacleWidth)
			if e.refined {
				// Grow the final export around detours without changing the
				// coordinate origin used to certify the content arrangement.
				for _, s := range strokes {
					measuredBounds = append(measuredBounds, planStrokeBox(s))
				}
			}
			labelStrokes = append(labelStrokes, strokes...)
			for _, s := range connectorTextConflictStrokes(strokes) {
				copy := s
				titleObstacles = append(titleObstacles, planTitleObstacle{kind: "route", index: e.ci, stroke: &copy})
			}
			labelObstacles = append(labelObstacles, connectorTextStrokeObstacles(e.ci, points, false, obstacleWidth)...)
			if box, ok := planRouteArrow(e, points); ok {
				// markerUnits defaults to strokeWidth. The viewBox/9x9 marker
				// transformation stays within this orientation-independent envelope.
				if e.refined {
					measuredBounds = append(measuredBounds, box)
				}
				labelObstacles = append(labelObstacles, connectorTextObstacle{-1, box})
				titleObstacles = append(titleObstacles, planTitleObstacle{kind: "arrow", index: e.ci, box: box})
				if collectConflicts {
					labelTargets = append(labelTargets, connectorTextRectTarget{kind: "arrow", index: e.ci, rect: box})
				}
			}
		}
		mxw, myw := planMid(e.pts)
		mx, my := tx(mxw), ty(myw)

		if c.Label != "" {
			block := layoutConnectorText(c, 10)
			if block.font != nil {
				measuredLabels = append(measuredLabels, connectorTextRequest{index: e.ci, c: c, block: block, points: points})
				continue
			}
			lblBg, lblInk := "#ffffff", "#334155"
			if c.LabelBg != "" {
				lblBg = c.LabelBg
			}
			if c.LabelColor != "" {
				lblInk = c.LabelColor
			}
			wpx := 7.0*float64(len([]rune(c.Label))) + 10
			pills = append(pills, planPill{x: mx - wpx/2, y: my - 8, w: wpx, bg: lblBg, ink: lblInk, label: c.Label, c: c, index: e.ci})
		}
	}

	for _, p := range pills {
		obstacle, box, expanded := planLegacyPill(nil, p)
		bodyRects = append(bodyRects, obstacle)
		titleObstacles = append(titleObstacles, planTitleObstacle{kind: "label", index: p.index, box: box})
		if collectConflicts {
			labelTargets = append(labelTargets, connectorTextRectTarget{kind: "label", index: p.index, rect: box})
		}
		if expanded {
			measuredBounds = append(measuredBounds, box)
		}
	}
	placements := resolveConnectorText(measuredLabels, bodyRects, nil, labelObstacles, labelStrokes)
	refineConnectorTextJoint(placements, measuredLabels, bodyRects, nil, labelObstacles, labelStrokes, func() (connectorTextBodies, bool) {
		// Bound title ink is already in bodyRects, including overhangs and
		// container titles. Ambient-font titles still have unknown extents.
		for i, r := range rects {
			if r.label != "" && texts[i].font == nil {
				return connectorTextBodies{}, false
			}
		}
		return connectorTextBodies{}, len(pills) == 0
	})
	for i, p := range placements {
		titleObstacles = append(titleObstacles, planTitleObstacle{kind: "label", index: measuredLabels[i].index, box: p.box})
	}
	if refinePlanTitles(rects, texts, titleObstacles, ox, oy, len(pills) > 0, collectConflicts) {
		// Retain the old viewport envelope for stability, and include final
		// ink so small author padding cannot lose the title's export margin.
		for _, p := range texts {
			for _, b := range p.inkRects() {
				measuredBounds[0].x0 = math.Min(measuredBounds[0].x0, b.x0)
				measuredBounds[0].y0 = math.Min(measuredBounds[0].y0, b.y0)
				measuredBounds[0].x1 = math.Max(measuredBounds[0].x1, b.x1)
				measuredBounds[0].y1 = math.Max(measuredBounds[0].y1, b.y1)
			}
		}
		// Rebuild content obstacles after accepted moves. Pill anchors remain
		// fixed; only their clearance status can improve when a title departs.
		bodyRects = nil
		var finalTargets []connectorTextRectTarget
		for i, r := range rects {
			if !r.container {
				box := screenRect{tx(r.x), ty(r.y), tx(r.x + r.w), ty(r.y + r.d)}.inflate(planLineW(r) / 2)
				bodyRects = append(bodyRects, box)
				if collectConflicts {
					finalTargets = append(finalTargets, connectorTextBodyTarget(r.id, i, box))
				}
			}
			for _, box := range texts[i].inkRects() {
				bodyRects = append(bodyRects, box)
				if collectConflicts {
					finalTargets = append(finalTargets, connectorTextBodyTarget(r.id, i, box))
				}
			}
		}
		for _, target := range labelTargets {
			if target.kind != "body" {
				finalTargets = append(finalTargets, target)
			}
		}
		labelTargets = finalTargets
		for _, p := range pills {
			box, _, _ := planLegacyPill(nil, p)
			bodyRects = append(bodyRects, box)
		}
		for i := range placements {
			p := &placements[i]
			routeClear := p.clear
			p.clear = !collides(p.box, bodyRects)
			for j, q := range placements {
				if i != j && collides(p.box, []screenRect{q.box}) {
					p.clear = false
				}
			}
			for _, o := range labelObstacles {
				if o.owner < 0 && collides(p.box, []screenRect{o.rect}) {
					p.clear = false
				}
			}
			if !routeClear {
				for _, s := range labelStrokes {
					if s.owner != measuredLabels[i].index && connectorTextStrokeHits(p.box, []connectorTextStroke{s}) {
						p.clear = false
					}
				}
			}
		}
	}
	return planContentLayout{texts: texts, pills: pills, requests: measuredLabels, placements: placements, targets: labelTargets, strokes: labelStrokes, bounds: measuredBounds, hasTitleInk: titleBounds != nil}
}
