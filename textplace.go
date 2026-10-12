// Screen-space text placement — the layout contract for everything
// that lives on the canvas plane (screen labels, annotation boxes):
//
//  1. after projection, text must neither cross NOR touch any part's
//     silhouette (a clearance margin is enforced);
//  2. text prefers the position nearest the OUTSIDE of the picture —
//     candidates are tried periphery-first, so copy gravitates to the
//     scene's edge instead of threading between cubes.
//
// placeTextBox is a pure function with its own unit suite.
package isotopo

import (
	"math"
	"sort"
)

// screenRect is an axis-aligned box in composite screen coords.
type screenRect struct{ x0, y0, x1, y1 float64 }

func (r screenRect) intersects(o screenRect) bool {
	return r.x0 < o.x1 && o.x0 < r.x1 && r.y0 < o.y1 && o.y0 < r.y1
}

func (r screenRect) inflate(m float64) screenRect {
	return screenRect{r.x0 - m, r.y0 - m, r.x1 + m, r.y1 + m}
}

func (r screenRect) cx() float64 { return (r.x0 + r.x1) / 2 }
func (r screenRect) cy() float64 { return (r.y0 + r.y1) / 2 }

// textClearance is the no-touch margin between text and any other
// element, in screen units.
const textClearance = 6.0

// partScreenRects projects every part's world bbox into composite
// screen coords (same tx/ty as every injector) and returns one
// axis-aligned obstacle rect per part.
func partScreenRects(infos []partInfo) []screenRect {
	tx, ty := partsScreenOrigin(infos)
	out := make([]screenRect, 0, len(infos))
	for _, p := range infos {
		r := screenRect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		corners := [8][3]float64{
			{p.offWX, p.offWY, p.offWZ},
			{p.offWX + p.w, p.offWY, p.offWZ},
			{p.offWX + p.w, p.offWY + p.d, p.offWZ},
			{p.offWX, p.offWY + p.d, p.offWZ},
			{p.offWX, p.offWY, p.offWZ + p.h},
			{p.offWX + p.w, p.offWY, p.offWZ + p.h},
			{p.offWX + p.w, p.offWY + p.d, p.offWZ + p.h},
			{p.offWX, p.offWY + p.d, p.offWZ + p.h},
		}
		for _, c := range corners {
			sx, sy := projectIso(c[0], c[1], c[2])
			r.x0 = math.Min(r.x0, sx+tx)
			r.y0 = math.Min(r.y0, sy+ty)
			r.x1 = math.Max(r.x1, sx+tx)
			r.y1 = math.Max(r.y1, sy+ty)
		}
		out = append(out, r)
	}
	return out
}

func sceneCenter(rects []screenRect) (float64, float64) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, r := range rects {
		minX, minY = math.Min(minX, r.x0), math.Min(minY, r.y0)
		maxX, maxY = math.Max(maxX, r.x1), math.Max(maxY, r.y1)
	}
	return (minX + maxX) / 2, (minY + maxY) / 2
}

func collides(c screenRect, obstacles []screenRect) bool {
	for _, o := range obstacles {
		if c.intersects(o.inflate(textClearance)) {
			return true
		}
	}
	return false
}

// placeTextBox positions a boxW×boxH text box for an element whose
// silhouette is anchor. The preferred position (the legacy spot) wins
// when it is already collision-free — scenes that were clean render
// unchanged. The search is DIRECTION-major for consistency: the
// preferred direction (where the preferred spot sits relative to the
// anchor — below, for captions) is exhausted at every clearance
// before any other direction is tried, so sibling labels keep the
// same relative position; fallback directions are ordered
// periphery-first (away from the scene centre).
func placeTextBox(boxW, boxH float64, anchor screenRect, prefX, prefY float64, sceneCx, sceneCy float64, obstacles []screenRect) (float64, float64) {
	pref := screenRect{prefX, prefY, prefX + boxW, prefY + boxH}
	if !collides(pref, obstacles) {
		return prefX, prefY
	}

	outX, outY := anchor.cx()-sceneCx, anchor.cy()-sceneCy
	if n := math.Hypot(outX, outY); n > 0.001 {
		outX, outY = outX/n, outY/n
	} else {
		outX, outY = 0, 1 // dead centre: fall outward = downward
	}

	type dir struct{ dx, dy float64 }
	dirs := []dir{{0, 1}, {1, 0}, {-1, 0}, {0, -1}} // below, right, left, above
	// Stable periphery-first sort: higher dot(dir, outward) first.
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			si := dirs[i].dx*outX + dirs[i].dy*outY
			sj := dirs[j].dx*outX + dirs[j].dy*outY
			if sj > si+1e-9 {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
		}
	}
	// Consistency beats periphery: the direction of the PREFERRED spot
	// goes first regardless of outwardness.
	prefDir := dir{0, 1}
	switch {
	case pref.cy() < anchor.y0:
		prefDir = dir{0, -1}
	case pref.cx() > anchor.x1:
		prefDir = dir{1, 0}
	case pref.cx() < anchor.x0:
		prefDir = dir{-1, 0}
	}
	ordered := []dir{prefDir}
	for _, d := range dirs {
		if d != prefDir {
			ordered = append(ordered, d)
		}
	}

	for _, d := range ordered {
		for _, g := range []float64{10, 34, 62, 96, 136} {
			var x, y float64
			switch {
			case d.dy > 0: // below
				x, y = anchor.cx()-boxW/2, anchor.y1+g
			case d.dy < 0: // above
				x, y = anchor.cx()-boxW/2, anchor.y0-g-boxH
			case d.dx > 0: // right
				x, y = anchor.x1+g, anchor.cy()-boxH/2
			default: // left
				x, y = anchor.x0-g-boxW, anchor.cy()-boxH/2
			}
			c := screenRect{x, y, x + boxW, y + boxH}
			if !collides(c, obstacles) {
				return x, y
			}
		}
	}
	// The finite aesthetic candidates can all be blocked. Do not put the label
	// back into a known collision: solve the forbidden intervals on four outward
	// rays and choose the free result nearest the preferred position. For finite
	// obstacles every ray has a free tail, without an arbitrary distance cap.
	bestX, bestY, distance := prefX, prefY, math.Inf(1)
	for _, d := range ordered {
		x, y := anchor.cx()-boxW/2, anchor.cy()-boxH/2
		switch {
		case d.dy > 0:
			y = anchor.y1 + 10
		case d.dy < 0:
			y = anchor.y0 - 10 - boxH
		case d.dx > 0:
			x = anchor.x1 + 10
		default:
			x = anchor.x0 - 10 - boxW
		}
		x, y = freeTextBoxOnRay(screenRect{x, y, x + boxW, y + boxH}, d.dx, d.dy, obstacles)
		if delta := math.Hypot(x-prefX, y-prefY); delta < distance-1e-9 {
			bestX, bestY, distance = x, y, delta
		}
	}
	return bestX, bestY
}

// Each intersecting obstacle forbids an open interval of translations. Sorting
// and merging intervals finds the first legal point, including narrow gaps
// between distant obstacles. The small guard covers two-decimal SVG rounding.
func freeTextBoxOnRay(box screenRect, dx, dy float64, obstacles []screenRect) (float64, float64) {
	type interval struct{ lo, hi float64 }
	var blocked []interval
	for _, obstacle := range obstacles {
		o := obstacle.inflate(textClearance + .02)
		var lo, hi float64
		if dy != 0 {
			if box.x0 >= o.x1 || o.x0 >= box.x1 {
				continue
			}
			lo, hi = o.y0-box.y1, o.y1-box.y0
		} else {
			if box.y0 >= o.y1 || o.y0 >= box.y1 {
				continue
			}
			lo, hi = o.x0-box.x1, o.x1-box.x0
		}
		if dx < 0 || dy < 0 {
			lo, hi = -hi, -lo
		}
		if hi > 0 {
			blocked = append(blocked, interval{lo, hi})
		}
	}
	sort.Slice(blocked, func(i, j int) bool { return blocked[i].lo < blocked[j].lo })
	t := 0.0
	for _, b := range blocked {
		if t <= b.lo {
			break
		}
		t = math.Max(t, b.hi)
	}
	return box.x0 + dx*t, box.y0 + dy*t
}

// leaderStart returns the point on the anchor silhouette's boundary
// facing (tx, ty) — annotation leader lines start HERE instead of at
// the top-face centre, so a leader can never lie across its own node.
func leaderStart(anchor screenRect, tx, ty float64) (float64, float64) {
	cx, cy := anchor.cx(), anchor.cy()
	dx, dy := tx-cx, ty-cy
	if math.Abs(dx) < 0.001 && math.Abs(dy) < 0.001 {
		return cx, anchor.y1
	}
	halfW, halfH := (anchor.x1-anchor.x0)/2, (anchor.y1-anchor.y0)/2
	t := math.Inf(1)
	if dx != 0 {
		t = math.Min(t, halfW/math.Abs(dx))
	}
	if dy != 0 {
		t = math.Min(t, halfH/math.Abs(dy))
	}
	return cx + dx*t, cy + dy*t
}
