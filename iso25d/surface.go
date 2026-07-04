package iso25d

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// M3 — per-face surface descriptions, mirroring the DSL's style.faces.
// Renderers look a face up by name, then "*", then fall back to the
// legacy palette fills.
type FaceSurface struct {
	Fill    *FaceFill
	Strokes []FaceStrokeLayer
}

type FaceFill struct {
	Kind      string // solid | linearGradient | radialGradient | pattern
	Color     string
	Stops     []FaceStop
	Angle     float64
	Projected bool    // v3.4 — gradient axis follows the face iso plane
	Cx, Cy    float64 // radial centre 0..1 (defaults 0.5)
	HasC      bool
	Pattern   *FacePatternSpec
}

type FaceStop struct {
	Offset float64
	Color  string
}

type FacePatternSpec struct {
	Kind      string
	Color     string
	Spacing   float64
	Angle     float64
	Projected bool
}

type FaceStrokeLayer struct {
	Color   string
	Width   float64
	Dash    string
	Opacity float64
}

// surfaceFor resolves the face surface by exact name, then wildcard.
func surfaceFor(m map[string]*FaceSurface, name string) *FaceSurface {
	if m == nil {
		return nil
	}
	if s, ok := m[name]; ok {
		return s
	}
	return m["*"]
}

// isoTopMatrix is the patternTransform that pins a tile to the iso
// ground plane (the projection is affine, so the mapping is exact).
const isoTopMatrix = "matrix(0.8660 0.5000 -0.8660 0.5000 0 0)"

// sideMatrix returns the shear that pins a tile to a box's left/right
// wall (x along the world axis, y along world z).
func sideMatrix(name string) string {
	switch name {
	case "left":
		return "matrix(0.8660 0.5000 0 1 0 0)"
	case "right":
		return "matrix(0.8660 -0.5000 0 1 0 0)"
	}
	return ""
}

// SideMatrixForEdge is sideMatrix generalized to ANY vertical wall: the
// patternTransform that pins a tile to the wall extruded from a ground
// edge running along world direction (ex, ey) (need not be normalized).
// The tile's u axis follows the projected edge, v follows world z — the
// box left/right matrices are exactly the (1,0) / (0,1) special cases.
// Used by the prism family, whose side0..sideN-1 wall orientations are
// not derivable from the face name alone.
func SideMatrixForEdge(ex, ey float64) string {
	l := math.Hypot(ex, ey)
	if l == 0 {
		return ""
	}
	ex, ey = ex/l, ey/l
	ux := (ex - ey) * cos30
	uy := (ex + ey) * sin30
	if ux < 0 || (ux == 0 && uy < 0) {
		// Keep u pointing screen-right (the box convention), so the same
		// wall gets the same tiling regardless of base winding. 0-x (not
		// -x) so a flipped 0 never prints as "-0.0000".
		ux, uy = 0-ux, 0-uy
	}
	return fmt.Sprintf("matrix(%.4f %.4f 0 1 0 0)", ux, uy)
}

// emitFaceFill writes any defs the fill needs and returns the fill ref.
// Def ids are deterministic per (prefix, face) — callers namespace per
// part via the usual composite id prefixing.
func emitFaceFill(defs *strings.Builder, prefix, face string, f *FaceFill) string {
	return emitFaceFillProjected(defs, prefix, face, f, "")
}

// emitFaceFillProjected is emitFaceFill with an explicit pin-to-plane
// matrix for Projected fills. Shapes whose wall orientation isn't fixed
// by the face NAME (prism sideK) pass the SideMatrixForEdge result;
// empty keeps the name-based box lookup (left/right) → top-plane
// fallback, byte-identical to the historical behavior.
func emitFaceFillProjected(defs *strings.Builder, prefix, face string, f *FaceFill, wallTF string) string {
	switch f.Kind {
	case "", "solid":
		if f.Color != "" {
			return f.Color
		}
		return ""
	case "linearGradient", "radialGradient":
		stops := append([]FaceStop(nil), f.Stops...)
		sort.SliceStable(stops, func(i, j int) bool { return stops[i].Offset < stops[j].Offset })
		id := fmt.Sprintf("%sface-%s", prefix, face)
		if f.Kind == "linearGradient" {
			rad := f.Angle * math.Pi / 180
			x2, y2 := math.Cos(rad), math.Sin(rad)
			x1, y1 := 0.0, 0.0
			if x2 < 0 {
				x1, x2 = -x2, 0
			}
			if y2 < 0 {
				y1, y2 = -y2, 0
			}
			fmt.Fprintf(defs, `<linearGradient id="%s" x1="%.4f" y1="%.4f" x2="%.4f" y2="%.4f">`, id, x1, y1, x2, y2)
		} else {
			cx, cy := 0.5, 0.5
			if f.HasC {
				cx, cy = f.Cx, f.Cy
			}
			fmt.Fprintf(defs, `<radialGradient id="%s" cx="%.4f" cy="%.4f" r="0.75">`, id, cx, cy)
		}
		for _, s := range stops {
			fmt.Fprintf(defs, `<stop offset="%.4f" stop-color="%s"/>`, s.Offset, escapeAttr(s.Color))
		}
		if f.Kind == "linearGradient" {
			defs.WriteString(`</linearGradient>`)
		} else {
			defs.WriteString(`</radialGradient>`)
		}
		if f.Projected {
			tf := isoTopMatrix
			if wallTF != "" {
				tf = wallTF
			} else if m := sideMatrix(face); m != "" {
				tf = m
			}
			tag := `<linearGradient id="` + id + `"`
			if f.Kind == "radialGradient" {
				tag = `<radialGradient id="` + id + `"`
			}
			out := strings.Replace(defs.String(), tag, tag+` gradientTransform="`+tf+`"`, 1)
			defs.Reset()
			defs.WriteString(out)
		}
		return "url(#" + id + ")"
	case "pattern":
		if f.Pattern == nil {
			return ""
		}
		id := fmt.Sprintf("%sfacepat-%s", prefix, face)
		var inner strings.Builder
		pid := emitPatternDef(&inner, id, f.Pattern.Kind, f.Pattern.Color, f.Pattern.Spacing, f.Pattern.Angle)
		if pid == "" {
			return ""
		}
		body := inner.String()
		if f.Pattern.Projected {
			tf := isoTopMatrix
			if wallTF != "" {
				tf = wallTF
			} else if m := sideMatrix(face); m != "" {
				tf = m
			}
			if strings.Contains(body, `patternTransform="`) {
				// compose with the tile's own rotation
				body = strings.Replace(body, `patternTransform="`, `patternTransform="`+tf+` `, 1)
			} else {
				body = strings.Replace(body, "<pattern ", `<pattern patternTransform="`+tf+`" `, 1)
			}
		}
		defs.WriteString(body)
		return "url(#" + pid + ")"
	}
	return ""
}

// writeFaceStrokeLayers re-traces a face polygon once per stroke layer
// (fill none), widest first so narrow highlight lines read on top.
func writeFaceStrokeLayers(sb *strings.Builder, name string, layers []FaceStrokeLayer, pts ...[2]float64) {
	for i, l := range layers {
		if l.Width <= 0 || l.Color == "" {
			continue
		}
		dashAttr := ""
		if l.Dash != "" {
			dashAttr = fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(l.Dash))
		}
		opAttr := ""
		if l.Opacity > 0 && l.Opacity < 1 {
			opAttr = fmt.Sprintf(` stroke-opacity="%.3f"`, l.Opacity)
		}
		var d strings.Builder
		for j, p := range pts {
			if j == 0 {
				fmt.Fprintf(&d, "M %.2f %.2f", p[0], p[1])
			} else {
				fmt.Fprintf(&d, " L %.2f %.2f", p[0], p[1])
			}
		}
		d.WriteString(" Z")
		fmt.Fprintf(sb,
			`<path data-face="%s-stroke%d" d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round"%s%s/>`,
			name, i, d.String(), escapeAttr(l.Color), l.Width, dashAttr, opAttr,
		)
	}
}

// emitBackglowHalo writes the blur filter def + a silhouette-shaped
// halo path (painted before the faces). Shared by the sharp box and
// prism paths — the rounded path has had its own since v2.1.
func emitBackglowHalo(sb, defs *strings.Builder, id, color string, radius, opacity float64, pts [][2]float64) {
	if strings.TrimSpace(color) == "" || radius <= 0 || len(pts) < 3 {
		return
	}
	if opacity <= 0 {
		opacity = 0.6
	}
	fmt.Fprintf(defs,
		`<filter id="%s" x="-50%%" y="-50%%" width="200%%" height="200%%">`+
			`<feGaussianBlur in="SourceGraphic" stdDeviation="%.2f"/>`+
			`<feComponentTransfer><feFuncA type="linear" slope="%.2f"/></feComponentTransfer>`+
			`</filter>`,
		id, radius, opacity,
	)
	var d strings.Builder
	for i, p := range pts {
		if i == 0 {
			fmt.Fprintf(&d, "M %.2f %.2f", p[0], p[1])
		} else {
			fmt.Fprintf(&d, " L %.2f %.2f", p[0], p[1])
		}
	}
	d.WriteString(" Z")
	fmt.Fprintf(sb, `<path data-face="backglow" d="%s" fill="%s" stroke="none" filter="url(#%s)"/>`,
		d.String(), escapeAttr(color), id)
}

// emitBlurOpen writes a blur filter def + opens a <g filter=…> around
// the whole part. Returns true when the caller must close the group.
func emitBlurOpen(sb *strings.Builder, id string, std float64) bool {
	if std <= 0 {
		return false
	}
	fmt.Fprintf(sb, `<defs><filter id="%s" x="-30%%" y="-30%%" width="160%%" height="160%%"><feGaussianBlur stdDeviation="%.2f"/></filter></defs><g filter="url(#%s)">`,
		id, std, id)
	return true
}

// emitOutlineRing strokes a closed accent ring along the given
// silhouette (already in standalone-SVG local coords). Painted by each
// renderer AFTER its faces so the ring inherits the part's authoritative
// transform — no post-hoc frame matching (the inject-layer version
// mis-registered against substrate-shifted composites).
func emitOutlineRing(sb *strings.Builder, sil [][2]float64, color string, width float64, dash string, opacity float64) {
	if color == "" || width <= 0 || len(sil) < 3 {
		return
	}
	var d strings.Builder
	for i, q := range sil {
		if i == 0 {
			fmt.Fprintf(&d, "M %.2f %.2f", q[0], q[1])
		} else {
			fmt.Fprintf(&d, " L %.2f %.2f", q[0], q[1])
		}
	}
	d.WriteString(" Z")
	extra := ""
	if dash != "" {
		extra += fmt.Sprintf(` stroke-dasharray="%s"`, escapeAttr(dash))
	}
	if opacity > 0 && opacity < 1 {
		extra += fmt.Sprintf(` stroke-opacity="%.3f"`, opacity)
	}
	fmt.Fprintf(sb, `<path data-face="outline" d="%s" fill="none" stroke="%s" stroke-width="%.2f" stroke-linejoin="round"%s/>`,
		d.String(), escapeAttr(color), width, extra)
}
