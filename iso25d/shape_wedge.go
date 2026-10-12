// Wedge shape: a right-triangular prism on its side — bottom is a
// rectangle at z=0, back edge (y=0) rises to z=h, front edge (y=d)
// stays at z=0. Produces a sloped ramp / data-ingestion pipeline look.
//
// Vertices (world space):
//
//	A = (0, 0, 0)   B = (w, 0, 0)   C = (w, d, 0)   D = (0, d, 0)
//	E = (0, 0, h)   F = (w, 0, h)
//
// Five faces:
//  1. Bottom (ABCD): faces down — NOT visible from iso camera.
//  2. Back wall (y=0): AEFB — normal (0,-1,0) — NOT visible (ny<0).
//  3. Left triangle (x=0): DAE — normal (-1,0,0) — NOT visible (nx<0).
//  4. Right triangle (x=w): CBF — normal (+1,0,0) — VISIBLE (nx+ny=1>0).
//  5. Slope: DCFE — outward normal has ny>0, nz>0 — VISIBLE.
package iso25d

import (
	"fmt"
	"math"
	"strings"
)

// WedgeShapeProvider implements ShapeProvider for the wedge / ramp shape.
type WedgeShapeProvider struct{}

func (WedgeShapeProvider) Names() []string { return []string{"wedge"} }

// wedgeProj projects a world point into screen space using the same
// prismLocal convention as the prism family.
func wedgeProj(d, h, x, y, z float64) [2]float64 {
	return prismLocal(d, h, x, y, z)
}

func (WedgeShapeProvider) Faces(w, d, h float64, params map[string]any) []Face {
	proj := func(x, y, z float64) [2]float64 { return wedgeProj(d, h, x, y, z) }

	// Slope face outward normal: edges (w,0,0) and (0,-d,h).
	// Cross product = (0·h − 0·(−d), 0·0 − w·h, w·(−d) − 0·0) = (0, −wh, −wd).
	// Negate for outward (away from interior) → (0, h, d). Normalize.
	slopeLen := math.Hypot(h, d)
	slopeNy := h / slopeLen
	slopeNz := d / slopeLen

	// Slope face: D C F E (CCW viewed from outside)
	slopeFace := Face{
		Name: "slope",
		Points: [][2]float64{
			proj(0, d, 0), // D
			proj(w, d, 0), // C
			proj(w, 0, h), // F
			proj(0, 0, h), // E
		},
		Normal:  [3]float64{0, slopeNy, slopeNz},
		Visible: true,
		ZOrder:  1,
	}

	// Right triangle: C B F (CW → outward +x)
	rightFace := Face{
		Name: "right",
		Points: [][2]float64{
			proj(w, d, 0), // C
			proj(w, 0, 0), // B
			proj(w, 0, h), // F
		},
		Normal:  [3]float64{1, 0, 0},
		Visible: true,
		ZOrder:  0,
	}

	// Hidden bottom — included for Silhouette completeness.
	bottomFace := Face{
		Name: "bottom",
		Points: [][2]float64{
			proj(0, 0, 0), proj(w, 0, 0), proj(w, d, 0), proj(0, d, 0),
		},
		Normal:  [3]float64{0, 0, -1},
		Visible: false,
		ZOrder:  -1,
	}

	return []Face{bottomFace, rightFace, slopeFace}
}

func (WedgeShapeProvider) Silhouette(w, d, h float64, params map[string]any) [][2]float64 {
	proj := func(x, y, z float64) [2]float64 { return wedgeProj(d, h, x, y, z) }
	pts := [][2]float64{
		proj(0, 0, 0), proj(w, 0, 0), proj(w, d, 0), proj(0, d, 0),
		proj(0, 0, h), proj(w, 0, h),
	}
	return convexHull(pts)
}

func (WedgeShapeProvider) ContentAnchor() string { return "slope" }

func (WedgeShapeProvider) ContentRectFor(w, d, h float64, params map[string]any) ContentRect {
	// Centered 60%×60% box on the slope face ground footprint.
	return ContentRect{X: w * 0.2, Y: d * 0.2, W: w * 0.6, H: d * 0.6}
}

func (WedgeShapeProvider) Footprint(w, d, _ float64) (float64, float64) { return w, d }

func init() {
	RegisterShape(WedgeShapeProvider{})
}

// ---------------------------------------------------------------------------
// Renderer
// ---------------------------------------------------------------------------

// RenderIsoWedge draws a wedge (sloped ramp) shape.
// The slope face uses the top palette colour; the right triangle uses the
// right palette colour.
func RenderIsoWedge(o IsoBoxOpts) string {
	return renderIsoWedge(o, nil)
}

func renderIsoWedge(o IsoBoxOpts, fonts fontResources) string {
	g := computeBoxGeom(o.Width, o.Depth, o.Height, o.Margin)
	params := map[string]any{}
	var prov WedgeShapeProvider
	m := o.Margin

	stroke := o.Stroke
	if stroke == "" {
		stroke = "#1F2433"
	}
	sw := o.StrokeWidth
	if sw <= 0 {
		sw = 1.4
	}

	var sb strings.Builder
	sb.WriteString(svgHeader(g.ViewW, g.ViewH))
	blurOn := emitBlurOpen(&sb, "wedge-blur", o.Blur)

	shadowID, grainID := "", ""
	{
		var defs strings.Builder
		if strings.TrimSpace(o.ShadowColor) != "" && (o.ShadowDx != 0 || o.ShadowDy != 0 || o.ShadowBlur > 0) {
			shadowID = "wedge-shadow"
			emitDropShadowFilter(&defs, shadowID, o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor)
		}
		grainID = emitGrainFilter(&defs, "wedge-grain", o.GrainIntensity, o.GrainScale)
		if defs.Len() > 0 {
			fmt.Fprintf(&sb, `<defs>%s</defs>`, defs.String())
		}
	}

	if strings.TrimSpace(o.BackglowColor) != "" && o.BackglowRadius > 0 {
		var gdefs, halo strings.Builder
		sil := prov.Silhouette(o.Width, o.Depth, o.Height, params)
		hpts := make([][2]float64, len(sil))
		for k, q := range sil {
			hpts[k] = [2]float64{q[0] + m, q[1] + m}
		}
		emitBackglowHalo(&halo, &gdefs, "wedge-backglow", o.BackglowColor, o.BackglowRadius, o.BackglowOpacity, hpts)
		fmt.Fprintf(&sb, `<defs>%s</defs>%s`, gdefs.String(), halo.String())
	}

	openWrapper(&sb, g.ViewW, g.ViewH, o.Background, o.Opacity, o.StrokeDasharray, shadowID)
	if grainID != "" {
		fmt.Fprintf(&sb, `<g filter="url(#%s)">`, grainID)
	}

	// Gradient defs.
	topFill, rightFill := o.TopFill, o.RightFill
	{
		var defs strings.Builder
		if o.TopGradient != nil {
			emitLinearGradient(&defs, "wedge-grad-top", o.TopGradient)
			topFill = "url(#wedge-grad-top)"
		}
		if o.RightGradient != nil {
			emitLinearGradient(&defs, "wedge-grad-right", o.RightGradient)
			rightFill = "url(#wedge-grad-right)"
		}
		if defs.Len() > 0 {
			fmt.Fprintf(&sb, `<defs>%s</defs>`, defs.String())
		}
	}

	type pend struct {
		name string
		pts  [][2]float64
	}
	var stroked []pend
	var faceDefs strings.Builder

	for _, f := range prov.Faces(o.Width, o.Depth, o.Height, params) {
		if !f.Visible {
			continue
		}
		fill := topFill
		if f.Name == "right" {
			fill = rightFill
		}
		if fs := surfaceFor(o.FaceSurfaces, f.Name); fs != nil && fs.Fill != nil {
			if ref := emitFaceFill(&faceDefs, "", f.Name, fs.Fill); ref != "" {
				fill = ref
			}
		}
		pts := make([][2]float64, len(f.Points))
		for i, p := range f.Points {
			pts[i] = [2]float64{p[0] + m, p[1] + m}
		}
		writeFace(&sb, f.Name, fill, stroke, sw, "", 0, pts...)
		if fs := surfaceFor(o.FaceSurfaces, f.Name); fs != nil && len(fs.Strokes) > 0 {
			stroked = append(stroked, pend{f.Name, pts})
		}
	}
	if faceDefs.Len() > 0 {
		fmt.Fprintf(&sb, `<defs>%s</defs>`, faceDefs.String())
	}
	for _, sd := range stroked {
		fs := surfaceFor(o.FaceSurfaces, sd.name)
		writeFaceStrokeLayers(&sb, sd.name, fs.Strokes, sd.pts...)
	}

	if grainID != "" {
		sb.WriteString(`</g>`)
	}

	// Label/icon: project onto the slope face at the midpoint of the
	// content rect at mid-height.
	cr := prov.ContentRectFor(o.Width, o.Depth, o.Height, params)
	midZ := o.Height / 2
	ctr := wedgeProj(o.Depth, o.Height, cr.X+cr.W/2, cr.Y+cr.H/2, midZ)
	ox := ctr[0] + m - cr.W/2*cos30
	oy := ctr[1] + m - cr.H/2*sin30
	writeTopLabelAndIconV12(
		&sb,
		ox, oy, cr.W, cr.H,
		o.Label, o.LabelLines, o.Icon, o.IconScale, o.IconAnchor, o.IconOffX, o.IconOffY,
		o.FontFamily, o.FontSize, o.FontWeight, o.FontColor,
		fonts,
	)

	if o.OutlineColor != "" && o.OutlineWidth > 0 {
		sil := prov.Silhouette(o.Width, o.Depth, o.Height, params)
		for k := range sil {
			sil[k][0] += m
			sil[k][1] += m
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
