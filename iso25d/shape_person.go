package iso25d

import (
	"fmt"
	"math"
	"strings"
)

type IsoPersonOpts struct {
	HeadRadius float64
	BodyWidth  float64
	BodyDepth  float64
	BodyHeight float64

	HeadHighlight string
	HeadShadow    string
	BodyTop       string
	BodyLeft      string
	BodyRight     string

	Stroke      string
	StrokeWidth float64

	Label      string
	FontFamily string
	FontSize   float64
	FontColor  string
	FontWeight string

	Margin float64

	// HeadStyle: "sphere" (default), "block" (C4-style boxy head).
	HeadStyle string

	Opacity         float64
	StrokeDasharray string
	Background      string

	// Body gradients (style.palette wiring): LeftGradient/RightGradient map
	// onto the matching dome halves; a topGradient is applied to BOTH halves
	// (the dome has no drawn top face, so "top gradient" reads as "body
	// gradient"). Non-nil overrides the matching solid Body* fill.
	BodyLeftGradient, BodyRightGradient *FaceGradient

	// Silhouette accent ring (effects.outline) — same semantics as the box
	// family: an emphasis ring traced along the figure's TRUE outer
	// silhouette (head crown fused onto the dome), painted on top.
	OutlineColor   string
	OutlineWidth   float64
	OutlineDash    string
	OutlineOpacity float64

	// Drop shadow filter (effects.dropShadow). Empty color = disabled,
	// matching emitBoxDefs' gate on the box path.
	ShadowDx, ShadowDy, ShadowBlur float64
	ShadowColor                    string

	// Diffuse halo behind the silhouette (effects.backglow).
	BackglowColor   string
	BackglowRadius  float64
	BackglowOpacity float64
}

func DefaultIsoPerson() IsoPersonOpts {
	return IsoPersonOpts{
		// Dome-shaped body proportions: roughly 2× wider than tall so the
		// torso reads as "shoulders" rather than a column. Head radius is
		// ~0.3× body width, matching the classic user-avatar look.
		HeadRadius: 36,
		BodyWidth:  120, BodyDepth: 120, BodyHeight: 50,
		HeadHighlight: "#FFD9B0", HeadShadow: "#A35F25",
		BodyTop: "#7FB3FF", BodyLeft: "#3A6FBA", BodyRight: "#5589D6",
		Stroke: "#1D3A66", StrokeWidth: 1.5,
		FontFamily: "Helvetica Neue, Arial, sans-serif",
		FontSize:   defaultFontSize, FontColor: "#0B1F3A", FontWeight: "600",
		Margin:    24,
		HeadStyle: "sphere",
	}
}

// RenderIsoPerson renders a friendly "user figure": a hemispherical
// dome torso (no visible top ellipse — silhouette curves smoothly into
// the head) capped with a sphere head that sinks slightly into the
// dome's apex. Matches the iconic 2.5D user-avatar look used in
// fossflow / draw.io stencils.
//
// Geometry (local frame, +y down to match SVG):
//
//	Body apex:       (0, 0)
//	Ground centre:   (0, BodyHeight)  ← bottom ellipse centre
//	Ground front:    (0, BodyHeight + ry)
//	Ground left:     (-rx, BodyHeight)
//	Ground right:    (rx,  BodyHeight)
//
// The silhouette is two elliptical arcs glued at the ground left/right:
// the top arc (rx wide, BodyHeight tall) is the dome; the bottom arc
// (rx wide, ry tall) is the front half of the ground ellipse. The
// left/right halves of the dome are split by the iso "ridge" — a
// vertical line from apex to the ground's front-touch point — so the
// figure still reads as iso (not a flat egg).
//
// The label is rendered in SCREEN SPACE below the figure (not on the
// dome top) because a curved surface can't host a flat label without
// colliding with the head when the body is short.
//
// HeadStyle "block" still works as a C4-style square head if requested.
func RenderIsoPerson(o IsoPersonOpts) string {
	rx := o.BodyWidth / 2
	ry := rx * sin30 / cos30 // iso ellipse y-radius for the ground
	h := o.BodyHeight        // dome height (apex to ground centre)
	headR := o.HeadRadius
	m := o.Margin
	if m <= 0 {
		m = 24
	}

	// Sphere head sits ON TOP of the dome but sinks slightly INTO it so
	// the silhouette reads as one continuous shape (no visible neck
	// seam). 0.20 = the bottom 20% of the head disappears into the
	// dome apex — tuned by eye against fossflow-style references.
	embed := headR * 0.20
	headCx := 0.0
	headCy := -headR + embed

	// Optional bottom label width estimate (screen space).
	hasLabel := strings.TrimSpace(o.Label) != ""
	labelW := 0.0
	labelGap := 6.0
	labelH := 0.0
	if hasLabel {
		// Rough width estimate: 0.6em per character. Lets the bbox
		// expand so the label isn't clipped by the viewBox.
		labelW = float64(len([]rune(o.Label))) * o.FontSize * 0.6
		labelH = o.FontSize + labelGap
	}

	// Local bbox: head sphere on top, ground ellipse + label at bottom.
	minX := math.Min(-math.Max(rx, headR), -labelW/2)
	maxX := math.Max(math.Max(rx, headR), labelW/2)
	minY := headCy - headR
	maxY := h + ry + labelH

	W := (maxX - minX) + 2*m
	H := (maxY - minY) + 2*m
	tx := -minX + m
	ty := -minY + m
	sx := func(x float64) float64 { return x + tx }
	sy := func(y float64) float64 { return y + ty }

	var sb strings.Builder
	sb.WriteString(svgHeader(W, H))

	// Effects silhouette (backglow halo + outline ring both trace it) —
	// sampled lazily so the no-effects render is byte-identical to before.
	var silPts [][2]float64
	silhouette := func() [][2]float64 {
		if silPts == nil {
			silPts = personFigureSilhouette(rx, ry, h, headR, headCy, o.HeadStyle)
			for k := range silPts {
				silPts[k][0] = sx(silPts[k][0])
				silPts[k][1] = sy(silPts[k][1])
			}
		}
		return silPts
	}

	// Pre-wrapper effects, matching the box path's conventions: the drop
	// shadow filter def is referenced by the wrapper <g>; the backglow halo
	// paints beneath everything so part opacity doesn't dim it.
	shadowID := ""
	var preDefs strings.Builder
	if strings.TrimSpace(o.ShadowColor) != "" && (o.ShadowDx != 0 || o.ShadowDy != 0 || o.ShadowBlur > 0) {
		shadowID = "person-shadow"
		emitDropShadowFilter(&preDefs, shadowID, o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor)
	}
	var halo strings.Builder
	if strings.TrimSpace(o.BackglowColor) != "" && o.BackglowRadius > 0 {
		emitBackglowHalo(&halo, &preDefs, "person-backglow", o.BackglowColor, o.BackglowRadius, o.BackglowOpacity, silhouette())
	}
	if preDefs.Len() > 0 {
		fmt.Fprintf(&sb, `<defs>%s</defs>`, preDefs.String())
	}
	sb.WriteString(halo.String())

	openWrapper(&sb, W, H, o.Background, o.Opacity, o.StrokeDasharray, shadowID)

	// Body fills: a per-half linear gradient outranks the solid fill.
	leftFill, rightFill := o.BodyLeft, o.BodyRight
	var bodyGradDefs strings.Builder
	if o.BodyLeftGradient != nil {
		emitLinearGradient(&bodyGradDefs, "person-body-left-grad", o.BodyLeftGradient)
		leftFill = "url(#person-body-left-grad)"
	}
	if o.BodyRightGradient != nil {
		emitLinearGradient(&bodyGradDefs, "person-body-right-grad", o.BodyRightGradient)
		rightFill = "url(#person-body-right-grad)"
	}

	// Defs: radial gradient for the sphere head (+ optional body gradients).
	gradID := "person-head-grad"
	fmt.Fprintf(&sb,
		`<defs><radialGradient id="%s" cx="35%%" cy="30%%" r="75%%"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></radialGradient>%s</defs>`,
		gradID, escapeAttr(o.HeadHighlight), escapeAttr(o.HeadShadow), bodyGradDefs.String(),
	)

	// Key silhouette points in screen space.
	apex := [2]float64{sx(0), sy(0)}
	groundL := [2]float64{sx(-rx), sy(h)}
	groundR := [2]float64{sx(rx), sy(h)}
	groundF := [2]float64{sx(0), sy(h + ry)} // front-touch on ground ellipse
	groundB := [2]float64{sx(0), sy(h - ry)} // back-touch (hidden under dome)

	// ── Dome body ────────────────────────────────────────────────────
	// Two half-domes split by the iso ridge (apex → groundF). Left
	// half takes BodyLeft (shadow side), right half takes BodyRight
	// (lit side), matching the cylinder convention so themes carry over.
	//
	// Left half: apex → ground-back-arc → groundL → dome-arc → apex.
	// The dome arc is half of an ellipse with radii (rx, h).
	// The ground arc here is the BACK half of the ground ellipse,
	// invisible behind the dome itself but needed to close the path
	// for the fill — instead we close apex → groundL directly via the
	// dome arc and use the iso ridge (apex → groundF) on the front side.
	// Sweep-flag convention (SVG +y down): sweep=1 = clockwise = increasing θ
	// when the ellipse is parameterised east(0)→south(π/2)→west(π)→north(3π/2).
	// Front-bottom of the ground ellipse is at θ=π/2, so:
	//   groundL(π)  → groundF(π/2): decreasing θ → counterclockwise → sweep=0
	//   groundR(0)  → groundF(π/2): increasing θ → clockwise         → sweep=1
	//   groundR(0)  → groundL(π) via front: increasing θ → clockwise → sweep=1
	// Getting these wrong makes the renderer pick the back arc (through the
	// hidden top of the ground ellipse), which surfaces as a V-notch at the
	// bottom + an extra horizontal "shelf" inside the body.
	fmt.Fprintf(&sb,
		`<path data-face="body-left" d="M %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f A %.2f %.2f 0 0 0 %.2f %.2f Z" fill="%s" stroke="none"/>`,
		apex[0], apex[1],
		rx, h, groundL[0], groundL[1], // dome arc apex → groundL
		rx, ry, groundF[0], groundF[1], // ground front-arc groundL → groundF (sweep=0)
		escapeAttr(leftFill),
	)
	fmt.Fprintf(&sb,
		`<path data-face="body-right" d="M %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f Z" fill="%s" stroke="none"/>`,
		apex[0], apex[1],
		rx, h, groundR[0], groundR[1], // dome arc apex → groundR
		rx, ry, groundF[0], groundF[1], // ground front-arc groundR → groundF (sweep=1)
		escapeAttr(rightFill),
	)

	// Subtle iso "ridge" — vertical hairline from apex to front-touch,
	// reinforces the iso read without the heavy seam of a top ellipse.
	if strings.TrimSpace(o.Stroke) != "" && o.StrokeWidth > 0 {
		fmt.Fprintf(&sb,
			`<line data-face="body-ridge" x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.2f" stroke-linecap="round" opacity="0.35"/>`,
			apex[0], apex[1], groundF[0], groundF[1],
			escapeAttr(o.Stroke), o.StrokeWidth*0.7,
		)
	}

	// Outer silhouette: dome arc (apex → groundR via top) + ground
	// front arc (groundR → groundL via front) + dome arc back to apex.
	if strings.TrimSpace(o.Stroke) != "" && o.StrokeWidth > 0 {
		fmt.Fprintf(&sb,
			`<path data-face="body-outline" d="M %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f A %.2f %.2f 0 0 1 %.2f %.2f Z" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"/>`,
			apex[0], apex[1],
			rx, h, groundR[0], groundR[1], // dome apex → groundR
			rx, ry, groundL[0], groundL[1], // ground front-arc groundR → groundL (sweep=1, through groundF)
			rx, h, apex[0], apex[1], // dome groundL → apex
			escapeAttr(o.Stroke), o.StrokeWidth,
		)
	}

	_ = groundB // kept named for documentation; not drawn (back of ground is hidden)

	// ── Head ─────────────────────────────────────────────────────────
	if o.HeadStyle == "block" {
		hw := headR * 1.6
		hd := headR * 1.6
		hh := headR * 0.9
		mini := computeBoxGeom(hw, hd, hh, 0)
		anchorX := sx(0) - (mini.A[0]+mini.C[0])/2
		anchorY := sy(0) - mini.ViewH
		off := [2]float64{anchorX, anchorY}
		shift2 := func(p [2]float64) [2]float64 { return [2]float64{p[0] + off[0], p[1] + off[1]} }
		mE, mF, mG, mH := shift2(mini.E), shift2(mini.F), shift2(mini.G), shift2(mini.H)
		faceTag(&sb, "head-left", o.HeadShadow, o.Stroke, o.StrokeWidth, shift2(mini.D), mH, mG, shift2(mini.C))
		faceTag(&sb, "head-right", o.HeadShadow, o.Stroke, o.StrokeWidth, shift2(mini.B), mF, mG, shift2(mini.C))
		faceTag(&sb, "head-top", o.HeadHighlight, o.Stroke, o.StrokeWidth, mE, mF, mG, mH)
	} else {
		fmt.Fprintf(&sb,
			`<circle data-face="head" cx="%.2f" cy="%.2f" r="%.2f" fill="url(#%s)" stroke="%s" stroke-width="%.2f"/>`,
			sx(headCx), sy(headCy), headR, gradID,
			escapeAttr(o.Stroke), o.StrokeWidth,
		)
	}

	// Optional label — screen-space, one line under the figure.
	if hasLabel {
		fmt.Fprintf(&sb,
			`<text data-face="label" x="%.2f" y="%.2f" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle">%s</text>`,
			sx(0), sy(h+ry)+labelGap+o.FontSize*0.85,
			escapeAttr(o.FontFamily), o.FontSize, escapeAttr(o.FontWeight),
			escapeAttr(o.FontColor),
			escapeXML(o.Label),
		)
	}

	// effects.outline — accent ring along the figure's true silhouette,
	// painted on top of the faces like every other shape's outline.
	if o.OutlineColor != "" && o.OutlineWidth > 0 {
		emitOutlineRing(&sb, silhouette(), o.OutlineColor, o.OutlineWidth, o.OutlineDash, o.OutlineOpacity)
	}

	closeWrapper(&sb)
	sb.WriteString(`</svg>`)
	return sb.String()
}

// personFigureSilhouette samples the figure's outer silhouette — the sphere
// head crown fused onto the dome torso — as a closed polyline in the local
// dome frame (apex at (0, 0), ground-ellipse centre at (0, h), +y down).
// Feeds emitBackglowHalo / emitOutlineRing, mirroring how the box family
// feeds its provider silhouette into the same emitters.
//
// The head↔dome fusion points are the exact circle∩ellipse intersections:
// substituting x² = headR² − (y−hc)² into the dome ellipse
// x²/rx² + (y−h)²/h² = 1 yields a quadratic in y —
//
//	(rx²−h²)·y² + 2(h²·hc − rx²·h)·y + h²(headR² − hc²) = 0
//
// — whose in-range root (smallest y with a real x) is where the crown meets
// the dome, so the ring hugs the neck seam with no notch. Falls back to the
// body-only silhouette when the head doesn't intersect the dome (oversized
// heads) or for the legacy "block" head style.
func personFigureSilhouette(rx, ry, h, headR, headCy float64, headStyle string) [][2]float64 {
	const (
		nCrown  = 24
		nDome   = 12
		nGround = 16
	)
	var pts [][2]float64
	add := func(x, y float64) { pts = append(pts, [2]float64{x, y}) }

	// Head∩dome fusion point (sphere head only).
	xi, yi := 0.0, 0.0
	haveHead := false
	if headStyle != "block" && headR > 0 && rx > 0 && h > 0 {
		hc := headCy
		qa := rx*rx - h*h
		qb := 2 * (h*h*hc - rx*rx*h)
		qc := h * h * (headR*headR - hc*hc)
		var cands []float64
		if math.Abs(qa) < 1e-9 {
			if qb != 0 {
				cands = append(cands, -qc/qb)
			}
		} else if disc := qb*qb - 4*qa*qc; disc >= 0 {
			s := math.Sqrt(disc)
			cands = append(cands, (-qb-s)/(2*qa), (-qb+s)/(2*qa))
		}
		best := math.Inf(1)
		for _, y := range cands {
			x2 := headR*headR - (y-hc)*(y-hc)
			if x2 <= 0 || y < hc || y > h {
				continue
			}
			if y < best {
				best = y
				xi, yi = math.Sqrt(x2), y
				haveHead = true
			}
		}
	}

	if haveHead {
		// Crown: left fusion point, over the top of the head, to the right one.
		aL := math.Atan2(yi-headCy, -xi)
		aR := math.Atan2(yi-headCy, xi) + 2*math.Pi
		for i := 0; i <= nCrown; i++ {
			a := aL + (aR-aL)*float64(i)/float64(nCrown)
			add(headR*math.Cos(a), headCy+headR*math.Sin(a))
		}
	}
	// Dome right flank: fusion point (or apex) down to ground-right. The dome
	// arc is the ellipse centred at (0, h) with radii (rx, h): φ=0 at the
	// apex, φ=π/2 at ground-right.
	phi0 := 0.0
	if haveHead {
		phi0 = math.Atan2(xi/rx, (h-yi)/h)
	}
	for i := 0; i <= nDome; i++ {
		phi := phi0 + (math.Pi/2-phi0)*float64(i)/float64(nDome)
		add(rx*math.Sin(phi), h-h*math.Cos(phi))
	}
	// Ground front arc: ground-right through the front touch to ground-left.
	for i := 1; i <= nGround; i++ {
		t := math.Pi * float64(i) / float64(nGround)
		add(rx*math.Cos(t), h+ry*math.Sin(t))
	}
	// Dome left flank: ground-left back up to the fusion point (or apex).
	for i := 1; i <= nDome; i++ {
		phi := math.Pi/2 - (math.Pi/2-phi0)*float64(i)/float64(nDome)
		add(-rx*math.Sin(phi), h-h*math.Cos(phi))
	}
	return pts
}

// ---------------------------------------------------------------------------
// Hex prism (pointy-top hexagon extruded)
// ---------------------------------------------------------------------------

func applyPerson(o ConvertOpts, p *IsoPersonOpts) {
	if o.Width > 0 {
		p.BodyWidth = o.Width
		p.HeadRadius = o.Width * 0.27
	}
	if o.Depth > 0 {
		p.BodyDepth = o.Depth
	}
	if o.Height > 0 {
		p.BodyHeight = o.Height
	}
	if o.TopFill != "" {
		p.BodyTop = o.TopFill
		// Head highlight reuses the body's top fill so the figure reads as
		// one unified colour family. Sphere gradient already shades it.
		p.HeadHighlight = o.TopFill
	}
	if o.LeftFill != "" {
		p.BodyLeft = o.LeftFill
		p.HeadShadow = o.LeftFill
	}
	if o.RightFill != "" {
		p.BodyRight = o.RightFill
	}
	if o.Stroke != "" {
		p.Stroke = o.Stroke
	}
	if o.StrokeWidth > 0 {
		p.StrokeWidth = o.StrokeWidth
	}
	if o.Label != "" {
		p.Label = o.Label
	}
	if o.FontFamily != "" {
		p.FontFamily = o.FontFamily
	}
	if o.FontSize > 0 {
		p.FontSize = o.FontSize
	}
	if o.FontColor != "" {
		p.FontColor = o.FontColor
	}
	if o.FontWeight != "" {
		p.FontWeight = o.FontWeight
	}
	if o.Margin > 0 {
		p.Margin = o.Margin
	}
	if o.Opacity > 0 {
		p.Opacity = o.Opacity
	}
	if o.StrokeDasharray != "" {
		p.StrokeDasharray = o.StrokeDasharray
	}
	if o.Background != "" {
		p.Background = o.Background
	}
	// Body gradients: left/rightGradient map onto the matching dome halves;
	// topGradient is the whole-body fallback for any half without its own
	// gradient (the dome draws no top face for it to land on).
	if o.LeftGradient != nil {
		p.BodyLeftGradient = o.LeftGradient
	}
	if o.RightGradient != nil {
		p.BodyRightGradient = o.RightGradient
	}
	if o.TopGradient != nil {
		if p.BodyLeftGradient == nil {
			p.BodyLeftGradient = o.TopGradient
		}
		if p.BodyRightGradient == nil {
			p.BodyRightGradient = o.TopGradient
		}
	}
	// Silhouette effects — same field semantics as applyBox.
	p.OutlineColor, p.OutlineWidth = o.OutlineColor, o.OutlineWidth
	p.OutlineDash, p.OutlineOpacity = o.OutlineDash, o.OutlineOpacity
	p.ShadowDx, p.ShadowDy, p.ShadowBlur, p.ShadowColor = o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor
	p.BackglowColor, p.BackglowRadius, p.BackglowOpacity = o.BackglowColor, o.BackglowRadius, o.BackglowOpacity
}

// PersonShapeProvider — geometry-only (M2.2): the true outline so
// connectors stop floating at the bbox corners ~50px from the figure.
// Approximated as the body capsule extended upward over the head.
type PersonShapeProvider struct{}

func (PersonShapeProvider) Names() []string { return []string{"person", "c4-person"} }

func (PersonShapeProvider) Faces(w, d, h float64, _ map[string]any) []Face {
	sil := (PersonShapeProvider{}).Silhouette(w, d, h, nil)
	return []Face{{Name: "body", Points: sil, Normal: [3]float64{0.7, 0.7, 0}, ZOrder: 0, Visible: true}}
}

func (PersonShapeProvider) Silhouette(w, d, h float64, _ map[string]any) [][2]float64 {
	rx := w / 2
	ry := rx * sin30 / cos30
	headR := w * 0.27
	headGap := 4.0
	cx := (w-d)/2*cos30 + d*cos30
	cyTop := (w + d) / 2 * sin30
	top := cyTop - h + rx - (headGap + 2*headR) // crown of the head zone
	_ = top
	var pts [][2]float64
	const n = 10
	// crown arc over the head (narrower than the body)
	hy := cyTop - h + rx - headGap - headR // head centre height in this frame is approximate;
	for i := 0; i <= n; i++ {
		th := math.Pi - math.Pi*float64(i)/float64(n)
		pts = append(pts, [2]float64{cx + headR*math.Cos(th), hy - headR*math.Sin(th)})
	}
	// shoulders out to the body radius at the body top
	pts = append(pts, [2]float64{cx + rx, cyTop})
	// body bottom front arc, right → left
	for i := 0; i <= n; i++ {
		th := math.Pi * float64(i) / float64(n)
		pts = append(pts, [2]float64{cx + rx*math.Cos(th), cyTop + h - rx + ry*math.Sin(th)})
	}
	pts = append(pts, [2]float64{cx - rx, cyTop})
	return pts
}

func (PersonShapeProvider) ContentAnchor() string { return "top" }

func (PersonShapeProvider) ContentRectFor(w, d, h float64, _ map[string]any) ContentRect {
	return ContentRect{X: 0, Y: 0, W: w, H: d}
}

func (PersonShapeProvider) Footprint(w, d, _ float64) (float64, float64) { return w, d }

func init() {
	RegisterShape(PersonShapeProvider{})
}
