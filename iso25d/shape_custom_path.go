// CustomPath shape: an arbitrary polygon base extruded vertically.
// The path is provided as an SVG-subset string (M/L/Z commands only)
// via geom.params.path. The polygon is normalized to fit the w×d
// footprint and then extruded to height h.
//
// Face names: "top" (the top polygon), "side0" … "sideN-1" (walls).
// Visibility: same criterion as prisms — outward normal nx+ny > 0.
package iso25d

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CustomPathShapeProvider implements ShapeProvider for arbitrary polygon extrusion.
type CustomPathShapeProvider struct{}

func (CustomPathShapeProvider) Names() []string { return []string{"custom_path"} }

// parseMLZPath parses an SVG path string containing only M, L, and Z
// commands into an ordered list of (x,y) points. Z closes the path
// (ignored for the point list). Returns nil on error or empty path.
func parseMLZPath(s string) [][2]float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Normalise separators.
	s = strings.ReplaceAll(s, ",", " ")
	tokens := strings.Fields(s)
	var pts [][2]float64
	i := 0
	for i < len(tokens) {
		cmd := tokens[i]
		i++
		switch strings.ToUpper(cmd) {
		case "M", "L":
			if i+1 >= len(tokens) {
				return nil
			}
			x, err1 := strconv.ParseFloat(tokens[i], 64)
			y, err2 := strconv.ParseFloat(tokens[i+1], 64)
			if err1 != nil || err2 != nil {
				return nil
			}
			pts = append(pts, [2]float64{x, y})
			i += 2
		case "Z":
			// close — nothing to do
		default:
			// Unknown command; try to parse as number (implicit lineto).
			x, err1 := strconv.ParseFloat(cmd, 64)
			if err1 != nil || i >= len(tokens) {
				return nil
			}
			y, err2 := strconv.ParseFloat(tokens[i], 64)
			if err2 != nil {
				return nil
			}
			pts = append(pts, [2]float64{x, y})
			i++
		}
	}
	if len(pts) < 3 {
		return nil
	}
	// Remove duplicate closing point if present.
	if len(pts) > 1 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	return pts
}

// normalizePathToBox scales the path points so they fit within [0,W]×[0,D].
func normalizePathToBox(pts [][2]float64, w, d float64) [][2]float64 {
	if len(pts) == 0 {
		return pts
	}
	minX, minY := pts[0][0], pts[0][1]
	maxX, maxY := pts[0][0], pts[0][1]
	for _, p := range pts[1:] {
		if p[0] < minX {
			minX = p[0]
		}
		if p[0] > maxX {
			maxX = p[0]
		}
		if p[1] < minY {
			minY = p[1]
		}
		if p[1] > maxY {
			maxY = p[1]
		}
	}
	rx, ry := maxX-minX, maxY-minY
	if rx < 1e-9 {
		rx = 1
	}
	if ry < 1e-9 {
		ry = 1
	}
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{(p[0] - minX) / rx * w, (p[1] - minY) / ry * d}
	}
	return out
}

// customPathBase returns the ground polygon. Falls back to a box when
// no valid path is available.
func customPathBase(w, d float64, params map[string]any) [][2]float64 {
	var pathStr string
	if params != nil {
		if p, ok := params["path"].(string); ok {
			pathStr = p
		}
	}
	pts := parseMLZPath(pathStr)
	if pts == nil {
		// Fallback: rectangle.
		return [][2]float64{{0, 0}, {w, 0}, {w, d}, {0, d}}
	}
	return normalizePathToBox(pts, w, d)
}

// customPathSideNormal returns the outward XY normal of the i-th side.
func customPathSideNormal(base [][2]float64, cx, cy float64, i int) (nx, ny float64, visible bool) {
	n := len(base)
	a, b := base[i], base[(i+1)%n]
	ex, ey := b[0]-a[0], b[1]-a[1]
	nx, ny = ey, -ex
	mx := (a[0]+b[0])/2 - cx
	my := (a[1]+b[1])/2 - cy
	if nx*mx+ny*my < 0 {
		nx, ny = -nx, -ny
	}
	l := math.Hypot(nx, ny)
	if l == 0 {
		return 0, 0, false
	}
	nx, ny = nx/l, ny/l
	visible = nx+ny > 1e-9
	return
}

func (CustomPathShapeProvider) Faces(w, d, h float64, params map[string]any) []Face {
	base := customPathBase(w, d, params)
	n := len(base)
	cx, cy := w/2, d/2

	var faces []Face
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		nx, ny, visible := customPathSideNormal(base, cx, cy, i)
		a, b := base[i], base[j]
		faces = append(faces, Face{
			Name: fmt.Sprintf("side%d", i),
			Points: [][2]float64{
				prismLocal(d, h, a[0], a[1], 0),
				prismLocal(d, h, a[0], a[1], h),
				prismLocal(d, h, b[0], b[1], h),
				prismLocal(d, h, b[0], b[1], 0),
			},
			Normal:  [3]float64{nx, ny, 0},
			Visible: visible,
		})
	}
	// Sort: hidden first, then by painter order.
	for i := range faces {
		faces[i].ZOrder = i
	}

	// Top face.
	top := Face{
		Name:    "top",
		Normal:  [3]float64{0, 0, 1},
		Visible: true,
		ZOrder:  n,
	}
	for _, v := range base {
		top.Points = append(top.Points, prismLocal(d, h, v[0], v[1], h))
	}
	faces = append(faces, top)
	return faces
}

func (CustomPathShapeProvider) Silhouette(w, d, h float64, params map[string]any) [][2]float64 {
	base := customPathBase(w, d, params)
	pts := make([][2]float64, 0, 2*len(base))
	for _, v := range base {
		pts = append(pts, prismLocal(d, h, v[0], v[1], 0))
		pts = append(pts, prismLocal(d, h, v[0], v[1], h))
	}
	return convexHull(pts)
}

func (CustomPathShapeProvider) ContentAnchor() string { return "top" }

func (CustomPathShapeProvider) ContentRectFor(w, d, h float64, params map[string]any) ContentRect {
	poly := customPathBase(w, d, params)
	if simplePaintPolygon(poly) && !convexGround(poly) {
		return concaveContentRect(poly, w, d)
	}
	return largestInscribedRect(poly, w, d)
}

func (CustomPathShapeProvider) Footprint(w, d, _ float64) (float64, float64) { return w, d }

func init() {
	RegisterShape(CustomPathShapeProvider{})
}

// ---------------------------------------------------------------------------
// Renderer
// ---------------------------------------------------------------------------

// RenderIsoCustomPath extrudes an arbitrary polygon path vertically.
func RenderIsoCustomPath(o IsoBoxOpts, pathStr string) string {
	return renderIsoCustomPath(o, pathStr, nil)
}

func renderIsoCustomPath(o IsoBoxOpts, pathStr string, fonts fontResources) string {
	params := map[string]any{}
	if pathStr != "" {
		params["path"] = pathStr
	}
	g := computeBoxGeom(o.Width, o.Depth, o.Height, o.Margin)
	var prov CustomPathShapeProvider
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
	blurOn := emitBlurOpen(&sb, "cpath-blur", o.Blur)

	shadowID, grainID := "", ""
	{
		var defs strings.Builder
		if strings.TrimSpace(o.ShadowColor) != "" && (o.ShadowDx != 0 || o.ShadowDy != 0 || o.ShadowBlur > 0) {
			shadowID = "cpath-shadow"
			emitDropShadowFilter(&defs, shadowID, o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor)
		}
		grainID = emitGrainFilter(&defs, "cpath-grain", o.GrainIntensity, o.GrainScale)
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
		emitBackglowHalo(&halo, &gdefs, "cpath-backglow", o.BackglowColor, o.BackglowRadius, o.BackglowOpacity, hpts)
		fmt.Fprintf(&sb, `<defs>%s</defs>%s`, gdefs.String(), halo.String())
	}

	openWrapper(&sb, g.ViewW, g.ViewH, o.Background, o.Opacity, o.StrokeDasharray, shadowID)
	if grainID != "" {
		fmt.Fprintf(&sb, `<g filter="url(#%s)">`, grainID)
	}

	topFill, leftFill, rightFill := o.TopFill, o.LeftFill, o.RightFill
	{
		var defs strings.Builder
		if o.TopGradient != nil {
			emitLinearGradient(&defs, "cpath-grad-top", o.TopGradient)
			topFill = "url(#cpath-grad-top)"
		}
		if o.LeftGradient != nil {
			emitLinearGradient(&defs, "cpath-grad-left", o.LeftGradient)
			leftFill = "url(#cpath-grad-left)"
		}
		if o.RightGradient != nil {
			emitLinearGradient(&defs, "cpath-grad-right", o.RightGradient)
			rightFill = "url(#cpath-grad-right)"
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
		if f.Name != "top" {
			// Side walls: pick left or right by normal dominance (same as prism).
			if f.Normal[0] >= f.Normal[1] {
				fill = rightFill
			} else {
				fill = leftFill
			}
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

	cr := prov.ContentRectFor(o.Width, o.Depth, o.Height, params)
	ox, oy := project(cr.X, cr.Y, o.Height)
	writeTopLabelAndIconV12(
		&sb,
		ox+g.Tx, oy+g.Ty, cr.W, cr.H,
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
