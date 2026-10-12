package isotopo

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// ConnectorTypography is available only when the complete label binds to an
// embedded font. Placement describes clearance against the declared envelopes,
// not visibility of every scene pixel or global placement optimality.
type ConnectorTypography struct {
	FontFamily string                   `json:"font_family"`
	FontWeight string                   `json:"font_weight"`
	FontSize   float64                  `json:"font_size"`
	FontHash   string                   `json:"font_hash"`
	Lines      []ScreenLabelLine        `json:"lines"`
	Placement  string                   `json:"placement"` // clear-within-scope | blocked
	Scope      string                   `json:"scope"`
	Conflicts  []ConnectorLabelConflict `json:"conflicts,omitempty"`
}

// ConnectorLabelRecord identifies a measured label in either projection. The
// index is the scene's connector index; it is not a persistent authored ID.
type ConnectorLabelRecord struct {
	ConnectorIndex int            `json:"connector_index"`
	Label          ConnectorLabel `json:"label"`
}

type connectorTextRequest struct {
	index  int
	c      *Connector
	block  screenTextBlock
	points [][2]float64
	curved bool
	drawn  *ConnectorDrawing
}

type connectorTextObstacle struct {
	owner int // -1: always avoid, including this connector's arrow
	rect  screenRect
}

func connectorFont(c *Connector) (family, weight string) {
	family, weight = c.LabelFontFamily, c.LabelFontWeight
	if family == "" {
		family = "Inter, sans-serif"
	}
	if weight == "" {
		weight = "600"
	}
	return
}

func layoutConnectorText(c *Connector, defaultSize float64) screenTextBlock {
	family, weight := connectorFont(c)
	size := c.LabelFontSize
	if size <= 0 {
		size = defaultSize
	}
	b := layoutScreenText(c.Label, family, weight, size)
	if b.font != nil {
		// Connector pills use 6 horizontal / 4 vertical units of padding.
		b.width -= 4
		b.height -= 2
		for i := range b.lines {
			b.lines[i].baseline--
		}
	}
	return b
}

// connectorTextPosition searches all continuous route intervals. Each segment
// is split at roots against expanded AABBs and convex stroke envelopes. A tiny
// expansion accounts for final SVG rounding; the submitted box is checked
// again. For a quadratic, t=.5 is the preference (not an arc-length claim).
// A polyline prefers its length midpoint. There is no arbitrary candidate cap.
func connectorTextPosition(points [][2]float64, curved bool, w, h float64, obstacles []screenRect, strokes ...connectorTextStroke) (float64, float64, bool) {
	return connectorTextPositionWithInteriors(points, curved, w, h, obstacles, strokes, nil)
}

// Filled faces supplement their swept boundary strokes. Boundary checks alone
// would incorrectly accept a pill wholly inside a large face.
func connectorTextPositionWithInteriors(points [][2]float64, curved bool, w, h float64, obstacles []screenRect, strokes []connectorTextStroke, interiors screenRegion) (float64, float64, bool) {
	if len(points) < 2 {
		return 0, 0, false
	}
	// Width and height are fixed for this search. Keep SVG's exact decimal
	// rounding, but do not format/parse them again for every candidate.
	width, height := svgCoord(w), svgCoord(h)
	box := func(x, y float64) screenRect {
		rx, ry := svgCoord(x-w/2), svgCoord(y-h/2)
		return screenRect{rx, ry, rx + width, ry + height}
	}
	var forbidden, interior [][][2]float64
	total := 1.0
	if !curved {
		total = planLen(points)
	}
	best := math.Inf(1)
	bx, by := points[0][0], points[0][1]
	if curved {
		bx = .25*points[0][0] + .5*points[1][0] + .25*points[2][0]
		by = .25*points[0][1] + .5*points[1][1] + .25*points[2][1]
	} else {
		bx, by = planMid(points)
	}
	search := func(ax, bx0, cx, ay, by0, cy, walked, length float64) {
		at := func(t float64) (float64, float64) { return (ax*t+bx0)*t + cx, (ay*t+by0)*t + cy }
		preferred := .5
		if length > 0 {
			preferred = (total/2 - walked) / length
		}
		if best == 0 {
			return // no later segment can strictly improve the existing result
		}
		if forbidden == nil && len(strokes) != 0 {
			forbidden = make([][][2]float64, 0, len(strokes))
			polygons := make([][2]float64, 0, 8*len(strokes))
			for _, stroke := range strokes {
				start := len(polygons)
				polygons = stroke.appendForbidden(polygons, w, h)
				forbidden = append(forbidden, polygons[start:len(polygons)])
			}
		}
		cuts := []float64{0, 1}
		root := func(a, b, c float64) { cuts = connectorTextRoots(cuts, a, b, c) }
		for _, o := range obstacles {
			root(ax, bx0, cx-(o.x0-w/2-6.03))
			root(ax, bx0, cx-(o.x1+w/2+6.03))
			root(ay, by0, cy-(o.y0-h/2-6.03))
			root(ay, by0, cy-(o.y1+h/2+6.03))
		}
		for _, polygon := range forbidden {
			cuts = connectorTextPolygonRoots(cuts, polygon, ax, bx0, cx, ay, by0, cy)
		}
		slices.Sort(cuts)
		cuts = slices.Compact(cuts)
		// Small scans are cheaper than building an index, especially the
		// rectangle-only compatibility pass run for every measured label.
		// Both strategies keep the same candidates and strict tie rule.
		// Node-heavy scenes below this size tend to reject on inexpensive
		// rectangles before reaching any stroke. Whole-render benchmarks
		// favor direct scans there, despite the dense stroke-only speedup.
		if len(obstacles)+len(strokes) <= 64 || len(obstacles) > 0 && len(obstacles) <= 512 && len(strokes) <= 128 {
			for i := 1; i < len(cuts); i++ {
				t := math.Max(cuts[i-1], math.Min(cuts[i], preferred))
				distance := math.Abs(walked + length*t - total/2)
				if !(distance < best) {
					continue
				}
				x, y := at(t)
				r := box(x, y)
				if !collides(r, obstacles) && !connectorTextStrokeHits(r, strokes) && !connectorTextInteriorHits(r, interiors) {
					best, bx, by = distance, x, y
				}
			}
			return
		}
		// Transform the sorted cuts in place. Writes stay behind the next
		// unread cut, so this needs no second candidate-sized allocation.
		candidates := cuts[:0]
		for i := 1; i < len(cuts); i++ {
			t := math.Max(cuts[i-1], math.Min(cuts[i], preferred))
			distance := math.Abs(walked + length*t - total/2)
			if distance < best {
				candidates = append(candidates, t)
			}
		}
		if len(candidates) == 0 {
			return
		}
		sortConnectorTextCandidates(candidates, walked, length, total/2)
		first := candidates[0]
		x, y := at(first)
		witness, work := connectorTextCollisionWorkInRegions(box(x, y), obstacles, strokes, interiors)
		if witness < 0 {
			best, bx, by = math.Abs(walked+length*first-total/2), x, y
			return
		}
		var spans []connectorTextBlockedSpan
		indexed := false
		budget := 8 * (len(obstacles) + len(strokes))
		for _, candidate := range candidates[1:] {
			if candidate == first {
				continue // already tested against the complete obstacle set
			}
			if !indexed && work >= budget {
				// Only sustained expensive scans build the index. Easy early
				// collisions retain the low setup cost of direct evaluation.
				spans = make([]connectorTextBlockedSpan, 0, len(obstacles)+len(strokes))
				for i, o := range obstacles {
					left, right := o.x0-w/2-5.97, o.x1+w/2+5.97
					top, bottom := o.y0-h/2-5.97, o.y1+h/2+5.97
					poly := [4][2]float64{{left, top}, {right, top}, {right, bottom}, {left, bottom}}
					spans = connectorTextBlockedIntervals(spans, poly[:], i, ax, bx0, cx, ay, by0, cy)
				}
				if interior == nil && len(strokes) != 0 {
					interior = make([][][2]float64, 0, len(strokes))
					polygons := make([][2]float64, 0, 8*len(strokes))
					for _, stroke := range strokes {
						start := len(polygons)
						polygons = stroke.appendForbidden(polygons, w-.12, h-.12)
						interior = append(interior, polygons[start:len(polygons)])
					}
				}
				for i, polygon := range interior {
					spans = connectorTextBlockedIntervals(spans, polygon, len(obstacles)+i, ax, bx0, cx, ay, by0, cy)
				}
				indexConnectorTextSpans(spans)
				indexed = true
			}
			x, y := at(candidate)
			r := box(x, y)
			if witness, ok := connectorTextSpanWitness(spans, candidate); ok {
				if witness < len(obstacles) {
					if collides(r, obstacles[witness:witness+1]) {
						continue
					}
				} else if i := witness - len(obstacles); connectorTextStrokeHits(r, strokes[i:i+1]) {
					continue
				}
			}
			// Acceptance always uses the original full predicates. An imprecise
			// index can cost time but cannot introduce or conceal a collision.
			witness, checked := connectorTextCollisionWorkInRegions(r, obstacles, strokes, interiors)
			work += checked
			if witness >= 0 {
				continue
			}
			best, bx, by = math.Abs(walked+length*candidate-total/2), x, y
			break // nearest safe candidate, with the historic tie order
		}
	}
	if curved && len(points) == 3 {
		p, q, r := points[0], points[1], points[2]
		search(p[0]-2*q[0]+r[0], 2*(q[0]-p[0]), p[0], p[1]-2*q[1]+r[1], 2*(q[1]-p[1]), p[1], 0, 1)
	} else {
		walked := 0.0
		for i := 1; i < len(points); i++ {
			p, q := points[i-1], points[i]
			length := math.Hypot(q[0]-p[0], q[1]-p[1])
			search(0, q[0]-p[0], p[0], 0, q[1]-p[1], p[1], walked, length)
			walked += length
		}
	}
	return bx, by, !math.IsInf(best, 1)
}

func emitConnectorText(sb *strings.Builder, req connectorTextRequest, placement connectorTextPlacement, bg, ink string, fonts map[string]bool, bodyRefined ...bool) ConnectorLabel {
	b := req.block
	x, y, clear := placement.x, placement.y, placement.clear
	family, weight := connectorFont(req.c)
	observed := observeScreenLabel(0, partInfo{}, b, x, y-b.height/2, family, weight)
	status := "blocked"
	if clear {
		status = "clear-within-scope"
	}
	label := ConnectorLabel{Text: req.c.Label, Rect: [4]float64{observed.Bounds.X, observed.Bounds.Y, observed.Bounds.Width, observed.Bounds.Height}, Anchor: [2]float64{svgCoord(x), svgCoord(y)},
		Typography: &ConnectorTypography{FontFamily: family, FontWeight: weight, FontSize: b.size, FontHash: b.font.Hash(), Lines: observed.Lines, Placement: status, Scope: "body-aabb,other-route-stroke-envelope,arrow-aabb,pill-aabb"}}
	if len(bodyRefined) > 0 && bodyRefined[0] {
		label.Typography.Scope = "body-region,content-quad,other-route-stroke-envelope,arrow-aabb,pill-aabb"
	}
	if !fonts[b.font.Hash()] {
		sb.WriteString(b.font.CSS())
		fonts[b.font.Hash()] = true
	}
	fmt.Fprintf(sb, `<g data-connector-label="%d" data-label-placement="%s"><rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" rx="4" ry="4" fill="%s"/>`, req.index, status, label.Rect[0], label.Rect[1], label.Rect[2], label.Rect[3], escAttr(bg))
	for _, line := range observed.Lines {
		fmt.Fprintf(sb, `<text x="%.2f" y="%.2f" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle" xml:space="preserve" xml:lang="en" lang="en" style="white-space:pre;font-kerning:normal;font-variant-ligatures:normal">%s</text>`, line.X, line.Baseline, escAttr(family), b.size, escAttr(weight), escAttr(ink), escapeXML(line.Text))
	}
	sb.WriteString(`</g>`)
	return label
}

func connectorLabelRect(l ConnectorLabel) screenRect {
	return screenRect{l.Rect[0], l.Rect[1], l.Rect[0] + l.Rect[2], l.Rect[1] + l.Rect[3]}
}

// A later infeasible label is retained at its midpoint. It can invalidate an
// earlier placement; final status must describe the whole submitted scene.
// Typography pointers are shared by the drawing/result records at emission.
func finishConnectorText(svg string, records []ConnectorLabelRecord) string {
	for i, a := range records {
		for j, b := range records {
			if i != j && collides(connectorLabelRect(a.Label), []screenRect{connectorLabelRect(b.Label)}) {
				a.Label.Typography.Placement = "blocked"
			}
		}
		if a.Label.Typography.Placement == "blocked" {
			old := fmt.Sprintf(`<g data-connector-label="%d" data-label-placement="clear-within-scope">`, a.ConnectorIndex)
			new := fmt.Sprintf(`<g data-connector-label="%d" data-label-placement="blocked">`, a.ConnectorIndex)
			svg = strings.Replace(svg, old, new, 1)
		}
	}
	return svg
}
