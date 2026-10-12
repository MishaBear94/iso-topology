package iso25d

import (
	"fmt"
	"math"
	"strings"
)

// RenderIsoBoxRounded draws a true iso-extruded rounded rectangle prism:
//   - the TOP FACE is a complete rounded rectangle (sampled outline)
//     projected to the iso top plane via the project() function — visually a
//     rounded rhombus
//   - the SIDE WALL is a thin band between the top face's *front-visible*
//     outline portion and the same portion shifted down by Height
//   - one continuous silhouette stroke traces the visible outer boundary
//
// This matches the Vite/JS cushion-badge baseline: distinct top + side, sharp
// outline, single colour on each face. CornerRadius is in local (W, D) units.
func RenderIsoBoxRounded(o IsoBoxOpts) string {
	return renderIsoBoxRounded(o, nil)
}

func renderIsoBoxRounded(o IsoBoxOpts, fonts fontResources) string {
	w, d, h, m := o.Width, o.Depth, o.Height, o.Margin
	if m <= 0 {
		m = 24
	}
	p, frontIdx := roundedBodyGeometry(w, d, h, o.CornerRadius)
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, q := range p {
		minX, maxX = math.Min(minX, q.topX), math.Max(maxX, q.topX)
		minY, maxY = math.Min(minY, q.topY), math.Max(maxY, q.botY)
	}
	tx, ty := -minX+m, -minY+m
	viewW, viewH := maxX-minX+2*m, maxY-minY+2*m
	for i := range p {
		p[i].topX += tx
		p[i].topY += ty
		p[i].botX += tx
		p[i].botY += ty
	}

	var sb strings.Builder
	sb.WriteString(svgHeader(viewW, viewH))
	blurOn := emitBlurOpen(&sb, "rounded-blur", o.Blur)

	// Drop shadow filter def.
	shadowID := ""
	hasShadow := strings.TrimSpace(o.ShadowColor) != "" &&
		(o.ShadowDx != 0 || o.ShadowDy != 0 || o.ShadowBlur > 0)
	if hasShadow {
		shadowID = "rounded-shadow"
	}

	// Side gradient def — vertical, RightFill (lighter) at top → LeftFill
	// at bottom. v2.2 — author Left/RightGradient win over solid fills:
	// the rounded silhouette has a single continuous side band, so the
	// two face gradients collapse onto it as (right.from → left.to),
	// mirroring the solid right-top/left-bottom convention.
	topStop := o.RightFill
	botStop := o.LeftFill
	if o.RightGradient != nil && strings.TrimSpace(o.RightGradient.From) != "" {
		topStop = o.RightGradient.From
	}
	if o.LeftGradient != nil && strings.TrimSpace(o.LeftGradient.To) != "" {
		botStop = o.LeftGradient.To
	}
	if topStop == "" {
		topStop = botStop
	}
	if botStop == "" {
		botStop = topStop
	}
	// v3.6.1 — palette "none" (ghosts, boundary containers) is a FILL
	// keyword, not a color: feeding it to a gradient stop renders an
	// invalid-color BLACK band along the side wall (the boundary
	// container's phantom "solid second outline").
	if strings.EqualFold(topStop, "none") || strings.EqualFold(botStop, "none") {
		topStop, botStop = "", ""
	}
	wantGradient := !o.Wireframe && botStop != ""
	var defs strings.Builder
	if hasShadow {
		emitDropShadowFilter(&defs, shadowID, o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor)
	}
	gradID := ""
	if wantGradient {
		gradID = "rounded-side"
		fmt.Fprintf(&defs,
			`<linearGradient id="%s" x1="50%%" y1="0%%" x2="50%%" y2="100%%"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></linearGradient>`,
			gradID, escapeAttr(topStop), escapeAttr(botStop),
		)
	}
	// v2.1 — author-specified top-face linear gradient. When set, it
	// overrides the solid TopFill and is referenced via url(#grad-top).
	topGradID := ""
	if o.TopGradient != nil && strings.TrimSpace(o.TopGradient.From) != "" {
		topGradID = "rounded-top"
		emitLinearGradient(&defs, topGradID, o.TopGradient)
	}
	// v2.1 — backglow filter: an oversized blur that gets painted as a
	// halo behind the silhouette. We use feGaussianBlur on a flood so the
	// halo respects the part's footprint without requiring an extra path.
	backglowID := ""
	if strings.TrimSpace(o.BackglowColor) != "" && o.BackglowRadius > 0 {
		backglowID = "rounded-backglow"
		op := o.BackglowOpacity
		if op <= 0 {
			op = 0.6
		}
		fmt.Fprintf(&defs,
			`<filter id="%s" x="-50%%" y="-50%%" width="200%%" height="200%%">`+
				`<feGaussianBlur in="SourceGraphic" stdDeviation="%.2f"/>`+
				`<feComponentTransfer><feFuncA type="linear" slope="%.2f"/></feComponentTransfer>`+
				`</filter>`,
			backglowID, o.BackglowRadius, op,
		)
	}
	// v2.3 — hatch/dots texture overlaid on the top face.
	patID := emitPatternDef(&defs, "rounded-pattern", o.PatternKind, o.PatternColor, o.PatternSpacing, o.PatternAngle)
	// v2.6 — film-grain noise over the faces.
	grainID := emitGrainFilter(&defs, "rounded-grain", o.GrainIntensity, o.GrainScale)
	if defs.Len() > 0 {
		sb.WriteString(`<defs>`)
		sb.WriteString(defs.String())
		sb.WriteString(`</defs>`)
	}

	// Wrapper.
	wrapAttrs := ""
	if o.Opacity > 0 && o.Opacity < 1 {
		wrapAttrs += fmt.Sprintf(` opacity="%.3f"`, o.Opacity)
	}
	// v3.0 — stroke.dash was honoured by every box renderer except this
	// one, so any rounded part (incl. every group substrate) silently
	// rendered solid outlines.
	if strings.TrimSpace(o.StrokeDasharray) != "" {
		wrapAttrs += fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(o.StrokeDasharray))
	}
	if shadowID != "" {
		wrapAttrs += fmt.Sprintf(` filter="url(#%s)"`, shadowID)
	}
	// v2.1 — paint the backglow halo first, so it sits behind every
	// visible face. We re-use the top-face perimeter as the halo shape
	// and rely on the SVG filter chain to blur + fade it.
	if backglowID != "" {
		fmt.Fprintf(&sb,
			`<path data-face="backglow" d="%s" fill="%s" stroke="none" filter="url(#%s)"/>`,
			perimeterPath(p, false), escapeAttr(o.BackglowColor), backglowID,
		)
	}
	fmt.Fprintf(&sb, `<g data-face="wrap"%s>`, wrapAttrs)
	if grainID != "" {
		fmt.Fprintf(&sb, `<g filter="url(#%s)">`, grainID)
	}

	stroke := o.Stroke
	if stroke == "" {
		stroke = "#1F2433"
	}
	sw := o.StrokeWidth
	if sw <= 0 {
		sw = 1.4
	}
	topFill := o.TopFill
	if topFill == "" {
		topFill = "#FFFFFF"
	}
	if topGradID != "" {
		topFill = "url(#" + topGradID + ")"
	}
	sideFill := "none"
	if wantGradient && gradID != "" {
		sideFill = "url(#" + gradID + ")"
	} else if o.LeftFill != "" {
		sideFill = o.LeftFill
	}
	// v3.3.1 — style.faces was silently discarded on the rounded path
	// (the M3 acceptance round's one HIGH finding: cornerRadius + faces
	// were mutually exclusive with zero warning). The rounded silhouette
	// has two surfaces: "top" and the continuous "side" band (left/right
	// accepted as aliases for the band).
	var faceDefs strings.Builder
	if fs := surfaceFor(o.FaceSurfaces, "top"); fs != nil && fs.Fill != nil {
		if ref := emitFaceFill(&faceDefs, "", "top", fs.Fill); ref != "" {
			topFill = ref
		}
	}
	for _, alias := range []string{"side", "left", "right"} {
		if fs := surfaceFor(o.FaceSurfaces, alias); fs != nil && fs.Fill != nil {
			if ref := emitFaceFill(&faceDefs, "", "side", fs.Fill); ref != "" {
				sideFill = ref
			}
			break
		}
	}
	if faceDefs.Len() > 0 {
		fmt.Fprintf(&sb, `<defs>%s</defs>`, faceDefs.String())
	}

	if o.Wireframe {
		// Top face stroked outline.
		fmt.Fprintf(&sb, `<path data-face="top-wire" d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round"/>`,
			perimeterPath(p, false), escapeAttr(stroke), sw)
		if len(frontIdx) >= 2 {
			// Vertical edges at the two transitions.
			ts, te := frontIdx[0], frontIdx[len(frontIdx)-1]
			fmt.Fprintf(&sb, `<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`,
				p[ts].topX, p[ts].topY, p[ts].botX, p[ts].botY, escapeAttr(stroke), sw)
			fmt.Fprintf(&sb, `<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`,
				p[te].topX, p[te].topY, p[te].botX, p[te].botY, escapeAttr(stroke), sw)
			// Bottom front-arc.
			fmt.Fprintf(&sb, `<path data-face="bot-front-wire" d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round"/>`,
				botFrontPath(p, frontIdx), escapeAttr(stroke), sw)
			// faceSplit: the front vertical edge is a real corner in line-art too.
			if k := frontCornerSplit(p, frontIdx); o.FaceSplit && k > 0 && k < len(frontIdx)-1 {
				fc := frontIdx[k]
				fmt.Fprintf(&sb, `<line data-face="front-edge-wire" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`,
					p[fc].topX, p[fc].topY, p[fc].botX, p[fc].botY, escapeAttr(stroke), sw)
			}
		}
	} else {
		// 1. Side wall fill (top-front-arc + right vertical + reverse bot-front-arc + left vertical).
		if len(frontIdx) >= 2 {
			k := frontCornerSplit(p, frontIdx)
			if o.FaceSplit && k > 0 && k < len(frontIdx)-1 {
				// Opt-in per-face lighting: split the continuous band at the
				// front vertical edge into two walls, each filled independently
				// (LeftFill / RightFill) so the rounded cube shades like a sharp
				// one. The wall sitting further screen-left is the left face.
				lFill, rFill := o.LeftFill, o.RightFill
				if lFill == "" {
					lFill = sideFill
				}
				if rFill == "" {
					rFill = sideFill
				}
				// Per-face GRADIENTS win over solids, mirroring the sharp box —
				// so a gradient-lit cube (e.g. light-top→shadow-bottom walls)
				// still splits correctly instead of falling back to a flat fill.
				var gdefs strings.Builder
				if g := o.LeftGradient; g != nil && strings.TrimSpace(g.From) != "" {
					emitLinearGradient(&gdefs, "grad-left-split", g)
					lFill = "url(#grad-left-split)"
				}
				if g := o.RightGradient; g != nil && strings.TrimSpace(g.From) != "" {
					emitLinearGradient(&gdefs, "grad-right-split", g)
					rFill = "url(#grad-right-split)"
				}
				if gdefs.Len() > 0 {
					fmt.Fprintf(&sb, `<defs>%s</defs>`, gdefs.String())
				}
				segL := sideSubPath(p, frontIdx, 0, k)
				segR := sideSubPath(p, frontIdx, k, len(frontIdx)-1)
				if avgTopX(p, frontIdx, 0, k) > avgTopX(p, frontIdx, k, len(frontIdx)-1) {
					segL, segR = segR, segL // segL is now the screen-left wall
				}
				fmt.Fprintf(&sb, `<path data-face="left" d="%s" fill="%s" stroke="none"/>`, segL, escapeAttr(lFill))
				fmt.Fprintf(&sb, `<path data-face="right" d="%s" fill="%s" stroke="none"/>`, segR, escapeAttr(rFill))
				// Front vertical edge — the seam where the two walls meet gives
				// the cube a real corner instead of a smooth colour blend.
				fc := frontIdx[k]
				fmt.Fprintf(&sb, `<line data-face="front-edge" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.2f" stroke-linecap="round"/>`,
					p[fc].topX, p[fc].topY, p[fc].botX, p[fc].botY, escapeAttr(stroke), sw)
			} else {
				fmt.Fprintf(&sb, `<path data-face="side" d="%s" fill="%s" stroke="none"/>`,
					sideWallPath(p, frontIdx), escapeAttr(sideFill),
				)
			}
		}
		// 2. Top face filled + stroked (full rounded perimeter at z=h).
		fmt.Fprintf(&sb, `<path data-face="top" d="%s" fill="%s" stroke="%s" stroke-width="%.2f" stroke-linejoin="round"/>`,
			perimeterPath(p, false), escapeAttr(topFill), escapeAttr(stroke), sw)
		if fs := surfaceFor(o.FaceSurfaces, "top"); fs != nil {
			for i, l := range fs.Strokes {
				if l.Width <= 0 || l.Color == "" {
					continue
				}
				extra := ""
				if l.Dash != "" {
					extra += fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(l.Dash))
				}
				if l.Opacity > 0 && l.Opacity < 1 {
					extra += fmt.Sprintf(` stroke-opacity="%.3f"`, l.Opacity)
				}
				fmt.Fprintf(&sb, `<path data-face="top-stroke%d" d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round"%s/>`,
					i, perimeterPath(p, false), escapeAttr(l.Color), l.Width, extra)
			}
		}
		if patID != "" {
			fmt.Fprintf(&sb, `<path data-face="top-pattern" d="%s" fill="url(#%s)" stroke="none"/>`,
				perimeterPath(p, false), patID)
		}
		// 3. Silhouette stroke for the parts NOT on the top-face path: two
		//    vertical edges + the bot-front-arc.
		if len(frontIdx) >= 2 {
			fmt.Fprintf(&sb, `<path data-face="silhouette" d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"/>`,
				silhouetteSkirtPath(p, frontIdx), escapeAttr(stroke), sw,
			)
		}
	}

	// Top-face content: 2D cell grid OR label/icon (cells take precedence,
	// same rule as the regular box renderer — v1.3 patch that closes the
	// cornerRadius/cells mutual-exclusion gap surfaced by the Vite repro).
	topOriginX, topOriginY := project(0, 0, h)
	topOriginX += tx
	topOriginY += ty
	if grainID != "" {
		sb.WriteString(`</g>`)
	}
	writeTopLabelAndIconV12(
		&sb,
		topOriginX, topOriginY, w, d,
		o.Label, o.LabelLines, o.Icon, o.IconScale, o.IconAnchor, o.IconOffX, o.IconOffY,
		o.FontFamily, o.FontSize, o.FontWeight, o.FontColor,
		fonts,
	)

	sb.WriteString(`</g>`)
	if blurOn {
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// sampleRoundedRectCW returns a screen-CW polyline of the rounded rectangle
// outline starting at (r, 0). Interior is on the RIGHT of motion.
func sampleRoundedRectCW(w, d, r float64) [][2]float64 {
	nEdge := 18
	nCorner := 12
	out := make([][2]float64, 0, 4*nEdge+4*nCorner+4)

	add := func(x, y float64) { out = append(out, [2]float64{x, y}) }

	// Top edge (r, 0) → (W-r, 0).
	for i := 0; i <= nEdge; i++ {
		t := float64(i) / float64(nEdge)
		add(r+t*(w-2*r), 0)
	}
	// Top-right corner: centre (W-r, r), θ from -π/2 to 0.
	for i := 1; i <= nCorner; i++ {
		t := float64(i) / float64(nCorner)
		theta := -math.Pi/2 + t*math.Pi/2
		add((w-r)+r*math.Cos(theta), r+r*math.Sin(theta))
	}
	// Right edge (W, r) → (W, D-r).
	for i := 1; i <= nEdge; i++ {
		t := float64(i) / float64(nEdge)
		add(w, r+t*(d-2*r))
	}
	// Bottom-right corner: centre (W-r, D-r), θ from 0 to π/2.
	for i := 1; i <= nCorner; i++ {
		t := float64(i) / float64(nCorner)
		theta := t * math.Pi / 2
		add((w-r)+r*math.Cos(theta), (d-r)+r*math.Sin(theta))
	}
	// Bottom edge (W-r, D) → (r, D).
	for i := 1; i <= nEdge; i++ {
		t := float64(i) / float64(nEdge)
		add((w-r)+t*(2*r-w), d)
	}
	// Bottom-left corner: centre (r, D-r), θ from π/2 to π.
	for i := 1; i <= nCorner; i++ {
		t := float64(i) / float64(nCorner)
		theta := math.Pi/2 + t*math.Pi/2
		add(r+r*math.Cos(theta), (d-r)+r*math.Sin(theta))
	}
	// Left edge (0, D-r) → (0, r).
	for i := 1; i <= nEdge; i++ {
		t := float64(i) / float64(nEdge)
		add(0, (d-r)+t*(2*r-d))
	}
	// Top-left corner: centre (r, r), θ from π to 3π/2.
	for i := 1; i <= nCorner; i++ {
		t := float64(i) / float64(nCorner)
		theta := math.Pi + t*math.Pi/2
		add(r+r*math.Cos(theta), r+r*math.Sin(theta))
	}
	// At the radius clamp, straight spans collapse to a point. Duplicate
	// vertices create false invisible edges and split the otherwise single
	// front run, which used to drop most of a capsule's side wall.
	clean := out[:0]
	for _, p := range out {
		if len(clean) == 0 || math.Hypot(p[0]-clean[len(clean)-1][0], p[1]-clean[len(clean)-1][1]) > 1e-9 {
			clean = append(clean, p)
		}
	}
	if len(clean) > 1 && math.Hypot(clean[0][0]-clean[len(clean)-1][0], clean[0][1]-clean[len(clean)-1][1]) <= 1e-9 {
		clean = clean[:len(clean)-1]
	}
	return clean
}

type roundedPr struct {
	topX, topY, botX, botY float64
}

// Helpers operate on the projected sample array (named pr for brevity).
func perimeterPath(p []roundedPr, bot bool) string {
	var sb strings.Builder
	if bot {
		fmt.Fprintf(&sb, "M %.2f,%.2f", p[0].botX, p[0].botY)
		for i := 1; i < len(p); i++ {
			fmt.Fprintf(&sb, " L %.2f,%.2f", p[i].botX, p[i].botY)
		}
	} else {
		fmt.Fprintf(&sb, "M %.2f,%.2f", p[0].topX, p[0].topY)
		for i := 1; i < len(p); i++ {
			fmt.Fprintf(&sb, " L %.2f,%.2f", p[i].topX, p[i].topY)
		}
	}
	sb.WriteString(" Z")
	return sb.String()
}

func sideWallPath(p []roundedPr, frontIdx []int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "M %.2f,%.2f", p[frontIdx[0]].topX, p[frontIdx[0]].topY)
	for _, idx := range frontIdx[1:] {
		fmt.Fprintf(&sb, " L %.2f,%.2f", p[idx].topX, p[idx].topY)
	}
	last := frontIdx[len(frontIdx)-1]
	fmt.Fprintf(&sb, " L %.2f,%.2f", p[last].botX, p[last].botY)
	for j := len(frontIdx) - 2; j >= 0; j-- {
		idx := frontIdx[j]
		fmt.Fprintf(&sb, " L %.2f,%.2f", p[idx].botX, p[idx].botY)
	}
	sb.WriteString(" Z")
	return sb.String()
}

// frontCornerSplit returns the index within frontIdx of the front-most vertical
// edge — the nearest corner to the viewer, where the left and right walls meet.
// In the iso projection that's the perimeter vertex with the largest screen Y.
func frontCornerSplit(p []roundedPr, frontIdx []int) int {
	k := 0
	maxY := math.Inf(-1)
	for j, idx := range frontIdx {
		if p[idx].topY > maxY {
			maxY = p[idx].topY
			k = j
		}
	}
	return k
}

// sideSubPath builds a closed side-band quad over frontIdx[a..b] (inclusive):
// top edge a→b, drop to the base at b, back along the base b→a, then close.
func sideSubPath(p []roundedPr, frontIdx []int, a, b int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "M %.2f,%.2f", p[frontIdx[a]].topX, p[frontIdx[a]].topY)
	for j := a + 1; j <= b; j++ {
		fmt.Fprintf(&sb, " L %.2f,%.2f", p[frontIdx[j]].topX, p[frontIdx[j]].topY)
	}
	fmt.Fprintf(&sb, " L %.2f,%.2f", p[frontIdx[b]].botX, p[frontIdx[b]].botY)
	for j := b - 1; j >= a; j-- {
		fmt.Fprintf(&sb, " L %.2f,%.2f", p[frontIdx[j]].botX, p[frontIdx[j]].botY)
	}
	sb.WriteString(" Z")
	return sb.String()
}

// avgTopX is the mean screen X of the top-edge vertices over frontIdx[a..b],
// used to decide which split wall faces screen-left.
func avgTopX(p []roundedPr, frontIdx []int, a, b int) float64 {
	sum, n := 0.0, 0
	for j := a; j <= b; j++ {
		sum += p[frontIdx[j]].topX
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func silhouetteSkirtPath(p []roundedPr, frontIdx []int) string {
	var sb strings.Builder
	first := frontIdx[0]
	last := frontIdx[len(frontIdx)-1]
	// Left vertical edge: top -> bot
	fmt.Fprintf(&sb, "M %.2f,%.2f L %.2f,%.2f",
		p[first].topX, p[first].topY,
		p[first].botX, p[first].botY,
	)
	// Bottom front-arc
	for _, idx := range frontIdx[1:] {
		fmt.Fprintf(&sb, " L %.2f,%.2f", p[idx].botX, p[idx].botY)
	}
	// Right vertical edge: bot -> top
	fmt.Fprintf(&sb, " L %.2f,%.2f", p[last].topX, p[last].topY)
	return sb.String()
}

func botFrontPath(p []roundedPr, frontIdx []int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "M %.2f,%.2f", p[frontIdx[0]].botX, p[frontIdx[0]].botY)
	for _, idx := range frontIdx[1:] {
		fmt.Fprintf(&sb, " L %.2f,%.2f", p[idx].botX, p[idx].botY)
	}
	return sb.String()
}

var _ = roundedPr{} // keep type comment around for readers

// roundedBodyGeometry is shared by rendering and filled-coverage detection.
// Both consume exactly the same visible run, including degenerate corners.
func roundedBodyGeometry(w, d, h, r float64) ([]roundedPr, []int) {
	outline := sampleRoundedRectCW(w, d, roundedRadius(w, d, r))
	n := len(outline)
	p := make([]roundedPr, n)
	visible := make([]bool, n)
	for i, q := range outline {
		x, y := project(q[0], q[1], 0)
		p[i] = roundedPr{topX: x, topY: y - h, botX: x, botY: y}
		b := outline[(i+1)%n]
		visible[i] = b[1]-q[1] > b[0]-q[0]
	}
	start, end := -1, -1
	for i := range outline {
		prev := (i - 1 + n) % n
		if visible[i] && !visible[prev] {
			start = i
		}
		if !visible[i] && visible[prev] {
			end = i
		}
	}
	var front []int
	if start >= 0 && end >= 0 {
		for i := start; ; i = (i + 1) % n {
			front = append(front, i)
			if i == end || len(front) > n {
				break
			}
		}
	}
	return p, front
}
