package iso25d

import (
	"fmt"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

func RenderIsoText(o IsoBoxOpts) string {
	text := strings.TrimSpace(o.Label)
	if text == "" {
		var sb strings.Builder
		sb.WriteString(svgHeader(0, 0))
		sb.WriteString(`</svg>`)
		return sb.String()
	}
	fontSize := o.FontSize
	if fontSize <= 0 {
		fontSize = 16
	}
	fontFamily := o.FontFamily
	if fontFamily == "" {
		fontFamily = "Inter, sans-serif"
	}
	fontWeight := o.FontWeight
	if fontWeight == "" {
		fontWeight = "700"
	}
	fontColor := o.FontColor
	if fontColor == "" {
		fontColor = "#1F2E50"
	}
	m := o.Margin
	if m <= 0 {
		m = 12
	}

	// Approximate text width: rune-aware — CJK a full em, latin the
	// iso-text fudge factor (Inter-ish). Add half-em of padding so the
	// trailing glyph isn't flush against the bbox edge after the
	// matrix tilt.
	textW := textmetrics.TextPx(text, fontSize, textmetrics.NarrowIsoText) + fontSize*0.5

	// Local text bbox is (0..textW, −fontSize..0). After the iso
	// matrix(cos30 sin30 -cos30 sin30 originX originY), each local
	// corner (lx, ly) maps to (lx·cos30 − ly·cos30, lx·sin30 + ly·sin30)
	// relative to (originX, originY). Pick origin so all 4 projected
	// corners fall inside the standalone SVG with margin m.
	W := cos30*(textW+fontSize) + 2*m
	H := sin30*(textW+fontSize) + 2*m
	originX := m
	originY := m + sin30*fontSize

	var sb strings.Builder
	sb.WriteString(svgHeader(W, H))
	fmt.Fprintf(&sb,
		`<g data-face="iso-text" transform="matrix(%.4f %.4f %.4f %.4f %.2f %.2f)">`,
		cos30, sin30, -cos30, sin30, originX, originY,
	)
	// v2.10 — transparent hit-rect over the text's local bbox (0..textW,
	// −fontSize..0) so the whole label area is clickable/draggable in Studio,
	// not just the thin glyph strokes. It tilts with the text via the matrix
	// above; pointer-events:all captures clicks despite the transparent fill.
	fmt.Fprintf(&sb,
		`<rect x="0" y="%.2f" width="%.2f" height="%.2f" fill="transparent" pointer-events="all"/>`,
		-fontSize, textW, fontSize,
	)
	fmt.Fprintf(&sb,
		`<text x="0" y="0" font-family="%s" font-size="%.2f" font-weight="%s" fill="%s">%s</text>`,
		escapeAttr(fontFamily), fontSize, escapeAttr(fontWeight), escapeAttr(fontColor), escapeXML(text),
	)
	sb.WriteString(`</g></svg>`)
	return sb.String()
}

// RenderIsoCloud extrudes a cloud silhouette into the iso top plane.
//
// Geometry: the cloud is sampled as a closed polyline in local (x, y) coords,
// projected at z=0 (bottom face) and z=h (top face). Front-facing perimeter
// segments are detected with the rule (dy > dx) in local coords (the iso
// view direction is (1,1,1); a segment whose outward normal has positive
// dot product with the view direction is front-facing). Each maximal run
// of consecutive front-facing segments is drawn as one side-wall polygon.
// Top face is drawn last as a polyline path so it cleanly covers all
// hidden-side walls and back-perimeter seams.
