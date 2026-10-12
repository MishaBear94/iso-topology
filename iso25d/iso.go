package iso25d

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// IsoBoxOpts captures every customisable parameter the POC exposes.
type IsoBoxOpts struct {
	// Geometry
	Width, Depth, Height float64

	// Face colours
	TopFill, LeftFill, RightFill string

	// Border
	Stroke      string
	StrokeWidth float64

	// Per-face stroke overrides (v1.2). Empty / zero = inherit base.
	TopStroke, LeftStroke, RightStroke struct {
		Color, Dash string
		Width       float64
	}
	// Per-face opacity overrides (v1.2). 0 = inherit base Opacity.
	TopOpacity, LeftOpacity, RightOpacity float64
	// Linear-gradient fills per face (v1.2). Non-nil overrides solid fill.
	TopGradient, LeftGradient, RightGradient *FaceGradient

	// Top-face label
	Label      string
	LabelLines []string // multi-line override
	FontFamily string
	FontSize   float64
	FontColor  string
	FontWeight string

	// Top-face icon
	Icon      string
	IconScale float64
	// v3.3 — per-face surface overrides (style.faces).
	FaceSurfaces map[string]*FaceSurface
	// v3.7 — gaussian blur stdDev applied to the whole part.
	Blur float64
	// v3.8 — silhouette accent ring (effects.outline).
	OutlineColor   string
	OutlineWidth   float64
	OutlineDash    string
	OutlineOpacity float64
	IconAnchor     string  // center | topLeft | topRight | bottomLeft | bottomRight
	IconOffX       float64 // fraction of W
	IconOffY       float64 // fraction of D

	// Padding around the projected shape in viewBox units
	Margin float64
	// Rounded-corner radius for the top face (v1.2).
	CornerRadius float64
	// FaceSplit (opt-in): on a rounded box, split the single continuous side
	// band at the front vertical edge into a left wall (LeftFill) and a right
	// wall (RightFill), so the two iso faces shade independently — true
	// per-face lighting on a rounded silhouette. Default false keeps the
	// classic single wrap-around side gradient.
	FaceSplit bool
	// Drop shadow filter (v1.2). Zero color = disabled.
	ShadowDx, ShadowDy, ShadowBlur float64
	ShadowColor                    string

	// Global presentation knobs — wrap every drawn face in a <g> carrying
	// these attributes.
	Opacity         float64 // 0..1; treated as 1 when <= 0 or >= 1
	StrokeDasharray string  // e.g. "4 2"; empty = solid
	Background      string  // CSS colour for an underlying background <rect>; empty = transparent

	// v1.3 wiring.
	Wireframe       bool
	BackglowColor   string
	BackglowRadius  float64
	BackglowOpacity float64
	GrainIntensity  float64
	GrainScale      float64
	PatternKind     string
	PatternColor    string
	PatternSpacing  float64
	PatternAngle    float64
	Cells           [][]string
	CellColors      [][]string
	Params          map[string]any
}

func DefaultIsoBox() IsoBoxOpts {
	return IsoBoxOpts{
		Width: 140, Depth: 140, Height: 80,
		TopFill:     "#7FB3FF",
		LeftFill:    "#3A6FBA",
		RightFill:   "#5589D6",
		Stroke:      "#1D3A66",
		StrokeWidth: 1.5,
		FontFamily:  "Helvetica Neue, Arial, sans-serif",
		FontSize:    defaultFontSize,
		FontColor:   "#0B1F3A",
		FontWeight:  "600",
		IconScale:   defaultIconScale,
		Margin:      24,
	}
}

// Cos30 / Sin30 are the iso ground-plane projection constants — the single
// source of truth for the whole pipeline (the isotopo package references these
// rather than redefining its own, so they can never drift).
const (
	Cos30 = 0.8660254037844387
	Sin30 = 0.5
	cos30 = Cos30 // internal lowercase alias (used throughout iso25d)
	sin30 = Sin30
)

// defaultIconScale is the top-face icon size as a fraction of the smaller face
// dimension when no explicit scale is set. Raised from the historical 0.4 so
// brand logos (e.g. GitHub) are identifiable at 1:1 without enlarging the whole
// diagram (issue #7). The adaptive fit path still caps it so icons never
// overflow the face.
const defaultIconScale = 0.55

// defaultIconOnlyScale is the default icon size for a node that carries an icon
// but NO label: with no text sharing the face, a brand logo can fill more of it.
// Used only when the author didn't set an explicit scale, so deliberate scales
// are respected. Makes logo-only chips (e.g. a GitHub/Postgres tile) legible.
const defaultIconOnlyScale = 0.72

// defaultFontSize is the top-face label size when no explicit size is set.
// Raised from the historical 16 so node labels read comfortably at 1:1. The
// adaptive fit path (fitTopContent) still shrinks it when a label would
// otherwise overflow a small face, so bigger-by-default never causes spill.
const defaultFontSize = 18

// projQuantum quantizes projected coordinates so sub-ULP arithmetic
// differences between Go toolchains (FMA contraction of a*b+c is
// compiler-dependent) can never flip a downstream threshold decision —
// the source of "same code, same input, different golden" drift across
// Go patch releases. 1e-6 is ~7 orders of magnitude above 1-ULP noise at
// scene scale and ~4 below the 0.01 the SVG emitter prints.
const projQuantum = 1e6

func quant(v float64) float64 { return math.Round(v*projQuantum) / projQuantum }

func project(x, y, z float64) (float64, float64) {
	return quant(x*cos30 - y*cos30), quant(x*sin30 + y*sin30 - z)
}

// boxGeom holds projected, viewbox-shifted coordinates for the 8 corners of
// an iso box of given dimensions plus the resulting viewBox size.
//
//	  E───────F          z
//	 /|      /|          |
//	H───────G |          o───── x
//	| A─────|─B         /
//	|/      |/         y
//	D───────C
type boxGeom struct {
	A, B, C, D, E, F, G, H [2]float64
	ViewW, ViewH           float64
	Width, Depth, Height   float64
	Tx, Ty                 float64 // translation applied to projected coords
}

func computeBoxGeom(width, depth, height, margin float64) boxGeom {
	w, d, h, m := width, depth, height, margin

	raw := [8][2]float64{}
	raw[0] = [2]float64{0, 0}
	raw[0][0], raw[0][1] = project(0, 0, 0)
	raw[1][0], raw[1][1] = project(w, 0, 0)
	raw[2][0], raw[2][1] = project(w, d, 0)
	raw[3][0], raw[3][1] = project(0, d, 0)
	raw[4][0], raw[4][1] = project(0, 0, h)
	raw[5][0], raw[5][1] = project(w, 0, h)
	raw[6][0], raw[6][1] = project(w, d, h)
	raw[7][0], raw[7][1] = project(0, d, h)

	minX, maxX := raw[0][0], raw[0][0]
	minY, maxY := raw[0][1], raw[0][1]
	for _, p := range raw {
		minX = math.Min(minX, p[0])
		maxX = math.Max(maxX, p[0])
		minY = math.Min(minY, p[1])
		maxY = math.Max(maxY, p[1])
	}

	tx, ty := -minX+m, -minY+m
	shift := func(p [2]float64) [2]float64 { return [2]float64{p[0] + tx, p[1] + ty} }

	return boxGeom{
		A: shift(raw[0]), B: shift(raw[1]), C: shift(raw[2]), D: shift(raw[3]),
		E: shift(raw[4]), F: shift(raw[5]), G: shift(raw[6]), H: shift(raw[7]),
		ViewW: (maxX - minX) + 2*m,
		ViewH: (maxY - minY) + 2*m,
		Width: w, Depth: d, Height: h,
		Tx: tx, Ty: ty,
	}
}

// svgHeader returns the opening <svg> tag for a given viewBox.
func svgHeader(w, h float64) string {
	return fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 %.2f %.2f" width="%.2f" height="%.2f">`,
		w, h, w, h,
	)
}

// openWrapper writes (a) an optional background <rect> covering the viewBox
// and (b) the opening <g> tag that carries Opacity / StrokeDasharray. The
// shadowID arg is the id of a previously emitted drop-shadow <filter>; if
// non-empty, it is applied to the wrapper g via filter="url(#...)".
// The inverse is closeWrapper(). Both must be paired around the actual face
// drawing code in every Render* function.
func openWrapper(sb *strings.Builder, w, h float64, bg string, opacity float64, dasharray, shadowID string) {
	if strings.TrimSpace(bg) != "" {
		fmt.Fprintf(sb,
			`<rect data-face="bg" x="0" y="0" width="%.2f" height="%.2f" fill="%s"/>`,
			w, h, escapeAttr(bg),
		)
	}
	attrs := ""
	if opacity > 0 && opacity < 1 {
		attrs += fmt.Sprintf(` opacity="%.3f"`, opacity)
	}
	if strings.TrimSpace(dasharray) != "" {
		attrs += fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(dasharray))
	}
	if shadowID != "" {
		attrs += fmt.Sprintf(` filter="url(#%s)"`, shadowID)
	}
	fmt.Fprintf(sb, `<g data-face="wrap"%s>`, attrs)
}

func closeWrapper(sb *strings.Builder) {
	sb.WriteString(`</g>`)
}

// emitLinearGradient writes a <linearGradient> def keyed to a fixed id.
// Direction strings: "down" (default), "up", "right", "left", "diag".
func emitLinearGradient(sb *strings.Builder, id string, g *FaceGradient) {
	x1, y1, x2, y2 := "0%", "0%", "0%", "100%"
	switch g.Dir {
	case "up":
		x1, y1, x2, y2 = "0%", "100%", "0%", "0%"
	case "right":
		x1, y1, x2, y2 = "0%", "0%", "100%", "0%"
	case "left":
		x1, y1, x2, y2 = "100%", "0%", "0%", "0%"
	case "diag":
		x1, y1, x2, y2 = "0%", "0%", "100%", "100%"
	}
	fmt.Fprintf(sb,
		`<linearGradient id="%s" x1="%s" y1="%s" x2="%s" y2="%s"><stop offset="0%%" stop-color="%s"/><stop offset="100%%" stop-color="%s"/></linearGradient>`,
		id, x1, y1, x2, y2, escapeAttr(g.From), escapeAttr(g.To),
	)
}

// emitDropShadowFilter writes a Gaussian-blurred drop shadow filter def.
func emitDropShadowFilter(sb *strings.Builder, id string, dx, dy, blur float64, color string) {
	fmt.Fprintf(sb,
		`<filter id="%s" x="-50%%" y="-50%%" width="200%%" height="200%%">`+
			`<feGaussianBlur in="SourceAlpha" stdDeviation="%.2f"/>`+
			`<feOffset dx="%.2f" dy="%.2f" result="offsetblur"/>`+
			`<feFlood flood-color="%s"/>`+
			`<feComposite in2="offsetblur" operator="in"/>`+
			`<feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge>`+
			`</filter>`,
		id, blur, dx, dy, escapeAttr(color),
	)
}

// pickFaceStroke chooses per-face stroke values, falling back to base when
// the face override is empty / zero.
func pickFaceStroke(baseColor string, baseWidth float64, baseDash string, face struct {
	Color, Dash string
	Width       float64
}) (string, float64, string) {
	color := baseColor
	if face.Color != "" {
		color = face.Color
	}
	width := baseWidth
	if face.Width > 0 {
		width = face.Width
	}
	dash := baseDash
	if face.Dash != "" {
		dash = face.Dash
	}
	return color, width, dash
}

// writeFace emits a face polygon supporting per-face fill/stroke/opacity
// /dasharray. Use for box-family shapes that want v1.2 overrides; the
// legacy faceTag stays for shapes that don't need them.
func writeFace(sb *strings.Builder, name, fill, stroke string, strokeWidth float64, dasharray string, opacity float64, pts ...[2]float64) {
	parts := make([]string, 0, len(pts))
	for _, p := range pts {
		parts = append(parts, fmt.Sprintf("%.2f,%.2f", p[0], p[1]))
	}
	attrs := ""
	if strokeWidth > 0 && stroke != "" {
		attrs += fmt.Sprintf(` stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"`, escapeAttr(stroke), strokeWidth)
		if dasharray != "" {
			attrs += fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(dasharray))
		}
	} else {
		attrs += ` stroke="none"`
	}
	if opacity > 0 && opacity < 1 {
		attrs += fmt.Sprintf(` opacity="%.3f"`, opacity)
	}
	fmt.Fprintf(sb,
		`<polygon data-face="%s" points="%s" fill="%s"%s/>`,
		name, strings.Join(parts, " "), escapeAttr(fill), attrs,
	)
}

// faceTag writes a <polygon> for an iso face.
func faceTag(sb *strings.Builder, name string, fill, stroke string, strokeWidth float64, pts ...[2]float64) {
	parts := make([]string, 0, len(pts))
	for _, p := range pts {
		parts = append(parts, fmt.Sprintf("%.2f,%.2f", p[0], p[1]))
	}
	if stroke == "" || strokeWidth <= 0 {
		fmt.Fprintf(sb,
			`<polygon data-face="%s" points="%s" fill="%s" stroke="none"/>`,
			name, strings.Join(parts, " "), escapeAttr(fill),
		)
		return
	}
	fmt.Fprintf(sb,
		`<polygon data-face="%s" points="%s" fill="%s" stroke="%s" stroke-width="%.2f" stroke-linejoin="round" stroke-linecap="round"/>`,
		name, strings.Join(parts, " "), escapeAttr(fill), escapeAttr(stroke), strokeWidth,
	)
}

// topContentTransform returns the SVG matrix(...) string that maps a local
// rectangle of size (width, depth) onto the iso top face at the supplied origin.
func topContentTransform(originX, originY float64) string {
	return fmt.Sprintf(
		`matrix(%.4f %.4f %.4f %.4f %.2f %.2f)`,
		cos30, sin30, -cos30, sin30, originX, originY,
	)
}

// writeTopLabelAndIcon emits a <g transform=...> with the icon and label
// positioned on a face whose local frame has dimensions (faceW, faceD)
// and whose origin (local 0,0) sits at the projected point (originX, originY).
func writeTopLabelAndIcon(
	sb *strings.Builder,
	originX, originY, faceW, faceD float64,
	label, icon string, iconScale float64,
	fontFamily string, fontSize float64, fontWeight, fontColor string,
) {
	hasLabel := strings.TrimSpace(label) != ""
	hasIcon := strings.TrimSpace(icon) != ""
	if !hasLabel && !hasIcon {
		return
	}
	if iconScale <= 0 {
		iconScale = defaultIconScale
	}
	if !hasLabel && iconScale <= defaultIconScale {
		iconScale = defaultIconOnlyScale // no text sharing the face → larger logo
	}

	fmt.Fprintf(sb, `<g data-face="top-content" transform="%s">`, topContentTransform(originX, originY))

	if hasIcon {
		iconSize := math.Min(faceW, faceD) * iconScale
		ix := (faceW - iconSize) / 2
		iy := faceD*0.5 - iconSize*0.5
		if hasLabel {
			iy = faceD*0.5 - iconSize - 4
		}
		fmt.Fprintf(sb,
			`<image href="%s" xlink:href="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f" preserveAspectRatio="xMidYMid meet"/>`,
			escapeAttr(icon), escapeAttr(icon), ix, iy, iconSize, iconSize,
		)
	}
	if hasLabel {
		lx := faceW / 2
		ly := faceD * 0.5
		if hasIcon {
			ly = faceD*0.5 + math.Min(faceW, faceD)*iconScale*0.5 + fontSize*0.5 + 2
		}
		fmt.Fprintf(sb,
			`<text x="%.2f" y="%.2f" dy=".35em" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle">%s</text>`,
			lx, ly,
			escapeAttr(fontFamily), fontSize, escapeAttr(fontWeight),
			escapeAttr(fontColor),
			escapeXML(label),
		)
	}

	sb.WriteString(`</g>`)
}

// RenderIsoBox draws a 3-face iso prism with optional top-face label/icon.
// Supports v1.2 knobs (per-face stroke/opacity/gradient, drop shadow,
// multi-line labels) and v1.3 (wireframe, backglow, pattern overlay, cells).
// When CornerRadius > 0 the box becomes a single rounded silhouette via
// RenderIsoBoxRounded — see that fn for the cushion-badge look.
func RenderIsoBox(o IsoBoxOpts) string {
	return renderIsoBox(o, nil)
}

func renderIsoBox(o IsoBoxOpts, fonts fontResources) string {
	if o.CornerRadius > 0 {
		return renderIsoBoxRounded(o, fonts)
	}
	g := computeBoxGeom(o.Width, o.Depth, o.Height, o.Margin)

	var sb strings.Builder
	sb.WriteString(svgHeader(g.ViewW, g.ViewH))
	blurOn := emitBlurOpen(&sb, "box-blur", o.Blur)

	topFill, leftFill, rightFill, shadowID, patID, grainID := emitBoxDefs(&sb, &o)

	// v3.3.1 — backglow on the sharp path (was rounded-only, making
	// faces and backglow mutually exclusive on boxes).
	if strings.TrimSpace(o.BackglowColor) != "" && o.BackglowRadius > 0 {
		var gdefs strings.Builder
		sil := (BoxShapeProvider{}).Silhouette(o.Width, o.Depth, o.Height, nil)
		hpts := make([][2]float64, len(sil))
		for k, q := range sil {
			hpts[k] = [2]float64{q[0] + o.Margin, q[1] + o.Margin}
		}
		var halo strings.Builder
		emitBackglowHalo(&halo, &gdefs, "box-backglow", o.BackglowColor, o.BackglowRadius, o.BackglowOpacity, hpts)
		fmt.Fprintf(&sb, `<defs>%s</defs>%s`, gdefs.String(), halo.String())
	}

	openWrapper(&sb, g.ViewW, g.ViewH, o.Background, o.Opacity, o.StrokeDasharray, shadowID)
	if grainID != "" {
		fmt.Fprintf(&sb, `<g filter="url(#%s)">`, grainID)
	}
	// v2.6 — wireframe line-art: keep the strokes, drop every fill.
	if o.Wireframe {
		topFill, leftFill, rightFill, patID = "none", "none", "none", ""
	}

	leftC, leftW, leftD := pickFaceStroke(o.Stroke, o.StrokeWidth, o.StrokeDasharray, o.LeftStroke)
	rightC, rightW, rightD := pickFaceStroke(o.Stroke, o.StrokeWidth, o.StrokeDasharray, o.RightStroke)
	topC, topW, topD := pickFaceStroke(o.Stroke, o.StrokeWidth, o.StrokeDasharray, o.TopStroke)

	// v3.2 (M1) — face polygons come from the geometry provider; the
	// surface emission below is unchanged. Byte parity with the corner
	// math this replaces is enforced by golden tests + parity tests.
	pf := providerFacePoints(o.Width, o.Depth, o.Height, o.Margin)
	// v3.3 — style.faces outranks the palette fills; gradient/pattern
	// defs are emitted into a local defs block on demand.
	var faceDefs strings.Builder
	faceFill := func(name, fallback string) string {
		fs := surfaceFor(o.FaceSurfaces, name)
		if fs == nil || fs.Fill == nil {
			return fallback
		}
		if ref := emitFaceFill(&faceDefs, "", name, fs.Fill); ref != "" {
			return ref
		}
		return fallback
	}
	leftFill = faceFill("left", leftFill)
	rightFill = faceFill("right", rightFill)
	topFill = faceFill("top", topFill)
	if faceDefs.Len() > 0 {
		fmt.Fprintf(&sb, `<defs>%s</defs>`, faceDefs.String())
	}
	writeFace(&sb, "left", leftFill, leftC, leftW, leftD, 0, pf["left"]...)
	writeFace(&sb, "right", rightFill, rightC, rightW, rightD, 0, pf["right"]...)
	writeFace(&sb, "top", topFill, topC, topW, topD, 0, pf["top"]...)
	for _, name := range []string{"left", "right", "top"} {
		if fs := surfaceFor(o.FaceSurfaces, name); fs != nil && len(fs.Strokes) > 0 {
			writeFaceStrokeLayers(&sb, name, fs.Strokes, pf[name]...)
		}
	}
	if patID != "" {
		writeFace(&sb, "top-pattern", "url(#"+patID+")", "", 0, "", 0, pf["top"]...)
	}
	if grainID != "" {
		sb.WriteString(`</g>`)
	}

	{
		writeTopLabelAndIconV12(
			&sb,
			g.E[0], g.E[1], o.Width, o.Depth,
			o.Label, o.LabelLines, o.Icon, o.IconScale, o.IconAnchor, o.IconOffX, o.IconOffY,
			o.FontFamily, o.FontSize, o.FontWeight, o.FontColor,
			fonts,
		)
	}

	if o.OutlineColor != "" && o.OutlineWidth > 0 {
		sil := (BoxShapeProvider{}).Silhouette(o.Width, o.Depth, o.Height, nil)
		for k := range sil {
			sil[k][0] += o.Margin
			sil[k][1] += o.Margin
		}
		emitOutlineRing(&sb, sil, o.OutlineColor, o.OutlineWidth, o.OutlineDash, o.OutlineOpacity)
	}
	closeWrapper(&sb)
	if blurOn {
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// emitBoxDefs emits any required <defs> (gradients + shadow filter +
// pattern tile) onto sb and returns the fill references plus the shadow
// filter and pattern ids.
func emitBoxDefs(sb *strings.Builder, o *IsoBoxOpts) (topFill, leftFill, rightFill, shadowID, patID, grainID string) {
	topFill, leftFill, rightFill = o.TopFill, o.LeftFill, o.RightFill
	var defs strings.Builder
	if o.TopGradient != nil {
		emitLinearGradient(&defs, "grad-top", o.TopGradient)
		topFill = "url(#grad-top)"
	}
	if o.LeftGradient != nil {
		emitLinearGradient(&defs, "grad-left", o.LeftGradient)
		leftFill = "url(#grad-left)"
	}
	if o.RightGradient != nil {
		emitLinearGradient(&defs, "grad-right", o.RightGradient)
		rightFill = "url(#grad-right)"
	}
	if strings.TrimSpace(o.ShadowColor) != "" && (o.ShadowDx != 0 || o.ShadowDy != 0 || o.ShadowBlur > 0) {
		shadowID = "shadow"
		emitDropShadowFilter(&defs, shadowID, o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor)
	}
	patID = emitPatternDef(&defs, "pat-top", o.PatternKind, o.PatternColor, o.PatternSpacing, o.PatternAngle)
	grainID = emitGrainFilter(&defs, "grain", o.GrainIntensity, o.GrainScale)
	if defs.Len() > 0 {
		sb.WriteString(`<defs>`)
		sb.WriteString(defs.String())
		sb.WriteString(`</defs>`)
	}
	return
}

// emitGrainFilter writes a monochrome film-grain noise filter def and
// returns its id, or "" when intensity is zero. The noise is grayscale
// fractal turbulence soft-light-blended onto the shape, clipped to the
// shape's own alpha — it darkens light fills and lifts dark ones, so
// one filter works on any palette.
func emitGrainFilter(defs *strings.Builder, id string, intensity, scale float64) string {
	if intensity <= 0 {
		return ""
	}
	if intensity > 1 {
		intensity = 1
	}
	if scale <= 0 {
		scale = 0.8
	}
	fmt.Fprintf(defs,
		`<filter id="%s" x="-5%%" y="-5%%" width="110%%" height="110%%">`+
			`<feTurbulence type="fractalNoise" baseFrequency="%.3f" numOctaves="2" stitchTiles="stitch" result="noise"/>`+
			`<feColorMatrix in="noise" type="saturate" values="0" result="mono"/>`+
			`<feComponentTransfer in="mono" result="mono2"><feFuncA type="linear" slope="%.3f" intercept="0"/></feComponentTransfer>`+
			`<feBlend in="SourceGraphic" in2="mono2" mode="soft-light" result="lit"/>`+
			`<feComposite in="lit" in2="SourceGraphic" operator="in"/>`+
			`</filter>`,
		id, scale, intensity)
	return id
}

// emitPatternDef writes a hatch/dots <pattern> tile def and returns its
// id, or "" when the kind is empty/unknown so callers skip the overlay.
func emitPatternDef(defs *strings.Builder, id, kind, color string, spacing, angle float64) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || strings.TrimSpace(color) == "" {
		return ""
	}
	if spacing <= 0 {
		spacing = 8
	}
	switch kind {
	case "hatch":
		fmt.Fprintf(defs,
			`<pattern id="%s" patternUnits="userSpaceOnUse" width="%.2f" height="%.2f" patternTransform="rotate(%.1f)">`+
				`<line x1="0" y1="0" x2="0" y2="%.2f" stroke="%s" stroke-width="1" stroke-opacity="0.55"/></pattern>`,
			id, spacing, spacing, angle, spacing, escapeAttr(color))
	case "dots":
		fmt.Fprintf(defs,
			`<pattern id="%s" patternUnits="userSpaceOnUse" width="%.2f" height="%.2f">`+
				`<circle cx="%.2f" cy="%.2f" r="1.1" fill="%s" fill-opacity="0.8"/></pattern>`,
			id, spacing, spacing, spacing/2, spacing/2, escapeAttr(color))
	default:
		return ""
	}
	return id
}

// writeTopLabelAndIconV12 wraps writeTopLabelAndIcon with support for
// multi-line label (LabelLines or "\n" split) and icon anchor/offset.
func writeTopLabelAndIconV12(
	sb *strings.Builder,
	originX, originY, faceW, faceD float64,
	label string, labelLines []string,
	icon string, iconScale float64, iconAnchor string, iconOffX, iconOffY float64,
	fontFamily string, fontSize float64, fontWeight, fontColor string,
	fonts fontResources,
) {
	font := textmetrics.ResolveBuiltin(fontFamily, fontWeight)
	items := layoutTopContentFont(faceW, faceD, label, labelLines, icon, iconScale, iconAnchor, iconOffX, iconOffY, fontSize, font)
	if len(items) == 0 {
		return
	}
	if font != nil {
		for _, p := range items {
			if !p.Icon && strings.TrimSpace(p.Text) != "" {
				sb.WriteString(fonts.css(font))
				break
			}
		}
	}
	fmt.Fprintf(sb, `<g data-face="top-content" transform="%s">`, topContentTransform(originX, originY))
	for _, p := range items {
		if p.Icon {
			fmt.Fprintf(sb, `<image href="%s" xlink:href="%s" x="%.2f" y="%.2f" width="%.2f" height="%.2f" preserveAspectRatio="xMidYMid meet"/>`, escapeAttr(icon), escapeAttr(icon), p.X, p.Y, p.W, p.H)
		} else {
			if font != nil {
				fmt.Fprintf(sb, `<text x="%.2f" y="%.2f" dy=".35em" font-family="Source Sans Pro" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle" xml:space="preserve" xml:lang="en" lang="en" style="white-space:pre;font-kerning:normal;font-variant-ligatures:normal">%s</text>`, p.X, p.Y, p.FontSize, font.Weight(), escapeAttr(fontColor), escapeXML(p.Text))
				continue
			}
			fmt.Fprintf(sb, `<text x="%.2f" y="%.2f" dy=".35em" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s" text-anchor="middle">%s</text>`, p.X, p.Y, escapeAttr(fontFamily), p.FontSize, escapeAttr(fontWeight), escapeAttr(fontColor), escapeXML(p.Text))
		}
	}
	sb.WriteString(`</g>`)
}

// iconAnchorXY returns the (x, y) for an icon of size iconSize on a face
// of (faceW, faceD), anchored to one of: center | topLeft | topRight |
// bottomLeft | bottomRight. Empty / unknown anchor → center.
func iconAnchorXY(anchor string, faceW, faceD, iconSize float64) (float64, float64) {
	pad := 6.0
	switch anchor {
	case "topLeft":
		return pad, pad
	case "topRight":
		return faceW - iconSize - pad, pad
	case "bottomLeft":
		return pad, faceD - iconSize - pad
	case "bottomRight":
		return faceW - iconSize - pad, faceD - iconSize - pad
	default: // center
		return (faceW - iconSize) / 2, faceD*0.5 - iconSize*0.5
	}
}

// stripXMLCtrl drops characters XML 1.0 forbids (control chars except tab, LF,
// CR). They are otherwise emitted raw into text/attributes and make the document
// non-well-formed — e.g. a U+0001 in a label from a d2 source.
func stripXMLCtrl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, s)
}

func escapeXML(s string) string {
	s = stripXMLCtrl(s)
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeAttr(s string) string {
	s = escapeXML(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// providerFacePoints fetches the box faces from the registered
// geometry provider and shifts them into the standalone SVG frame
// (margin offset). The bridge that makes the provider the live
// path's single source of geometric truth.
func providerFacePoints(w, d, h, m float64) map[string][][2]float64 {
	out := map[string][][2]float64{}
	for _, f := range (BoxShapeProvider{}).Faces(w, d, h, nil) {
		pts := make([][2]float64, len(f.Points))
		for i, p := range f.Points {
			pts[i] = [2]float64{p[0] + m, p[1] + m}
		}
		out[f.Name] = pts
	}
	return out
}
