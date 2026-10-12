package iso25d

import (
	"fmt"
	"strings"
)

func init() { RegisterShape(RackShapeProvider{}) }

type RackShapeProvider struct{}

func (RackShapeProvider) Names() []string { return []string{"rack"} }

func (RackShapeProvider) Faces(w, d, h float64, params map[string]any) []Face {
	return (BoxShapeProvider{}).Faces(w, d, h, params)
}

func (RackShapeProvider) Silhouette(w, d, h float64, params map[string]any) [][2]float64 {
	return (BoxShapeProvider{}).Silhouette(w, d, h, params)
}

func (RackShapeProvider) ContentAnchor() string { return "top" }

func (RackShapeProvider) ContentRectFor(w, d, h float64, params map[string]any) ContentRect {
	return (BoxShapeProvider{}).ContentRectFor(w, d, h, params)
}

func (RackShapeProvider) Footprint(w, d, h float64) (float64, float64) {
	return (BoxShapeProvider{}).Footprint(w, d, h)
}

func RenderIsoRack(o IsoBoxOpts, slots int) string {
	return renderIsoRack(o, slots, nil)
}

func renderIsoRack(o IsoBoxOpts, slots int, fonts fontResources) string {
	if slots <= 0 {
		slots = 4
	}

	g := computeBoxGeom(o.Width, o.Depth, o.Height, o.Margin)

	var sb strings.Builder
	sb.WriteString(svgHeader(g.ViewW, g.ViewH))
	blurOn := emitBlurOpen(&sb, "rack-blur", o.Blur)

	topFill, leftFill, rightFill, shadowID, _, grainID := emitBoxDefs(&sb, &o)

	openWrapper(&sb, g.ViewW, g.ViewH, o.Background, o.Opacity, o.StrokeDasharray, shadowID)
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
	m := o.Margin
	W, D, H := o.Width, o.Depth, o.Height

	proj := func(x, y, z float64) [2]float64 {
		p := prismLocal(D, H, x, y, z)
		return [2]float64{p[0] + m, p[1] + m}
	}

	// Closed outer shell: the two CAMERA-FACING walls + the top cap.
	// prismLocal projects screenX = (x−y): the front-left wall is y=D (spans x)
	// and the front-right wall is x=W (spans y); x=0/y=0 are hidden back faces.
	// (The previous code drew x=0 — a back face — and never drew x=W, so the
	// right side was open and the slot slabs floated past it.)
	writeFace(&sb, "left", leftFill, stroke, sw, "", 0,
		proj(0, D, 0), proj(W, D, 0), proj(W, D, H), proj(0, D, H))
	writeFace(&sb, "right", rightFill, stroke, sw, "", 0,
		proj(W, 0, 0), proj(W, D, 0), proj(W, D, H), proj(W, 0, H))

	// Slot grooves: a translucent dark band across both visible walls at each
	// shelf, so the rack reads as slotted WITHOUT breaking the closed shell.
	slotH := 8.0
	totalGap := H - float64(slots)*slotH
	if totalGap < 0 {
		slotH = H / float64(slots+1)
		totalGap = H - float64(slots)*slotH
	}
	slotGap := totalGap / float64(slots+1)

	for i := 0; i < slots; i++ {
		z0 := slotGap + float64(i)*(slotH+slotGap)
		z1 := z0 + slotH
		// recessed band on the front-left wall (y=D)
		writeFace(&sb, fmt.Sprintf("slot-left-%d", i), "#000000", "none", 0, "", 0.16,
			proj(0, D, z0), proj(W, D, z0), proj(W, D, z1), proj(0, D, z1))
		// recessed band on the front-right wall (x=W)
		writeFace(&sb, fmt.Sprintf("slot-right-%d", i), "#000000", "none", 0, "", 0.10,
			proj(W, 0, z0), proj(W, D, z0), proj(W, D, z1), proj(W, 0, z1))
	}

	// Top cap
	writeFace(&sb, "top", topFill, stroke, sw, "", 0,
		proj(0, 0, H), proj(W, 0, H), proj(W, D, H), proj(0, D, H))

	if grainID != "" {
		sb.WriteString(`</g>`)
	}

	writeTopLabelAndIconV12(
		&sb,
		g.E[0], g.E[1], o.Width, o.Depth,
		o.Label, o.LabelLines, o.Icon, o.IconScale, o.IconAnchor, o.IconOffX, o.IconOffY,
		o.FontFamily, o.FontSize, o.FontWeight, o.FontColor,
		fonts,
	)

	closeWrapper(&sb)
	if blurOn {
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}
