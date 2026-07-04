package iso25d

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// PrismShapeProvider — M2 of the flexible-geometry plan, and the first
// genuinely NEW geometry to flow through the ShapeProvider interface
// (category A of the design doc: any regular base × vertical extrude).
//
// The base is a regular n-gon inscribed in the part's w×d footprint,
// first vertex pointing back (-y). diamond / triprism / hexprism /
// octprism are fixed-side aliases; bare "prism" reads geom.sides.
type PrismShapeProvider struct{}

func (PrismShapeProvider) Names() []string {
	return []string{"triprism", "hexprism", "octprism"}
}

// prismSides resolves the side count from params (set by the renderer
// from shape name or geom.sides). Default hexagon.
// maxPrismSides caps polygon sides so a hostile geom.sides can't exhaust
// resources building a useless near-circle (512 already looks like a circle).
const maxPrismSides = 512

func prismSides(params map[string]any) int {
	if params != nil {
		if n, ok := params["sides"].(int); ok && n >= 3 {
			if n > maxPrismSides {
				return maxPrismSides
			}
			return n
		}
	}
	return 6
}

// prismBase returns the ground-plane vertices of the regular n-gon.
// n == 4 is rotated 22.5°: a square whose vertices sit ON the world
// axes projects to a screen-aligned rectangle (its diagonals coincide
// with the iso axes), collapsing both visible walls into one band —
// the M2 acceptance round's top finding. The offset restores a true
// lozenge silhouette with two distinctly-shaded walls.
func prismBase(w, d float64, n int) [][2]float64 {
	cx, cy := w/2, d/2
	rx, ry := w/2, d/2
	th0 := -math.Pi / 2
	if n == 4 {
		th0 += math.Pi / 8
	}
	out := make([][2]float64, n)
	for k := 0; k < n; k++ {
		th := th0 + 2*math.Pi*float64(k)/float64(n)
		out[k] = [2]float64{cx + rx*math.Cos(th), cy + ry*math.Sin(th)}
	}
	return out
}

// prismLocal projects a world-relative point into the provider's local
// frame — same convention as BoxShapeProvider (origin at the projected
// (0,d,0)/(0,0,h) extremes of the BOUNDING box, so PartOriginOffset's
// default branch stays correct without changes).
func prismLocal(d, h, x, y, z float64) [2]float64 {
	return [2]float64{(x-y)*cos30 + d*cos30, (x+y)*sin30 - z + h}
}

func (PrismShapeProvider) Faces(w, d, h float64, params map[string]any) []Face {
	n := prismSides(params)
	base := prismBase(w, d, n)
	cx, cy := w/2, d/2

	var faces []Face
	for k := 0; k < n; k++ {
		a, b := base[k], base[(k+1)%n]
		// outward normal: perpendicular of the edge, oriented away from
		// the centroid (robust to winding).
		nx, ny := b[1]-a[1], a[0]-b[0]
		mx, my := (a[0]+b[0])/2-cx, (a[1]+b[1])/2-cy
		if nx*mx+ny*my < 0 {
			nx, ny = -nx, -ny
		}
		l := math.Hypot(nx, ny)
		if l == 0 {
			continue
		}
		nx, ny = nx/l, ny/l
		visible := nx+ny > 1e-9
		faces = append(faces, Face{
			Name: fmt.Sprintf("side%d", k),
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
	// painter order: farther side walls (smaller x+y at edge midpoint)
	// first; hidden faces first of all; top face always last.
	sort.SliceStable(faces, func(i, j int) bool {
		if faces[i].Visible != faces[j].Visible {
			return !faces[i].Visible
		}
		di := faces[i].Points[0][0] + faces[i].Points[0][1] + faces[i].Points[2][0] + faces[i].Points[2][1]
		dj := faces[j].Points[0][0] + faces[j].Points[0][1] + faces[j].Points[2][0] + faces[j].Points[2][1]
		return di < dj
	})
	top := Face{
		Name:    "top",
		Normal:  [3]float64{0, 0, 1},
		Visible: true,
	}
	for _, v := range base {
		top.Points = append(top.Points, prismLocal(d, h, v[0], v[1], h))
	}
	faces = append(faces, top)
	for i := range faces {
		faces[i].ZOrder = i
	}
	return faces
}

// Silhouette is the convex hull of every projected vertex (top + base
// rings) — exact for convex bases.
func (PrismShapeProvider) Silhouette(w, d, h float64, params map[string]any) [][2]float64 {
	n := prismSides(params)
	base := prismBase(w, d, n)
	pts := make([][2]float64, 0, 2*n)
	for _, v := range base {
		pts = append(pts, prismLocal(d, h, v[0], v[1], 0))
		pts = append(pts, prismLocal(d, h, v[0], v[1], h))
	}
	return convexHull(pts)
}

func (PrismShapeProvider) ContentAnchor() string { return "top" }

// ContentRectFor inscribes an axis-aligned rectangle in the n-gon top
// face: a centered box scaled by cos(π/n) is always inside a regular
// polygon's inscribed ellipse.
func (PrismShapeProvider) ContentRectFor(w, d, h float64, params map[string]any) ContentRect {
	n := prismSides(params)
	return largestInscribedRect(prismBase(w, d, n), w, d)
}

// largestInscribedRect coarse-searches the biggest axis-aligned rect
// inside a convex ground polygon. A centered inscribed-square estimate
// starved asymmetric bases (a triangle's usable area sits toward its
// wide edge, not its centroid) down to unreadable 5px labels.
func largestInscribedRect(poly [][2]float64, w, d float64) ContentRect {
	const steps = 24
	best := ContentRect{X: w * 0.3, Y: d * 0.3, W: w * 0.4, H: d * 0.4}
	bestArea := 0.0
	inside := func(x, y float64) bool {
		return pointInConvexGround([2]float64{x, y}, poly)
	}
	for i0 := 0; i0 < steps; i0++ {
		for j0 := 0; j0 < steps; j0++ {
			x0, y0 := w*float64(i0)/steps, d*float64(j0)/steps
			if !inside(x0, y0) {
				continue
			}
			for i1 := steps; i1 > i0; i1-- {
				x1 := w * float64(i1) / steps
				for j1 := steps; j1 > j0; j1-- {
					y1 := d * float64(j1) / steps
					if (x1-x0)*(y1-y0) <= bestArea {
						break
					}
					if inside(x1, y0) && inside(x0, y1) && inside(x1, y1) &&
						inside((x0+x1)/2, y0) && inside((x0+x1)/2, y1) {
						bestArea = (x1 - x0) * (y1 - y0)
						best = ContentRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
					}
				}
			}
		}
	}
	return best
}

func pointInConvexGround(pt [2]float64, poly [][2]float64) bool {
	n := len(poly)
	sign := 0.0
	for i := 0; i < n; i++ {
		a, b := poly[i], poly[(i+1)%n]
		cross := (b[0]-a[0])*(pt[1]-a[1]) - (b[1]-a[1])*(pt[0]-a[0])
		if math.Abs(cross) < 1e-9 {
			continue
		}
		if sign == 0 {
			sign = cross
		} else if sign*cross < 0 {
			return false
		}
	}
	return true
}

func (PrismShapeProvider) Footprint(w, d, _ float64) (float64, float64) { return w, d }

func init() {
	RegisterShape(PrismShapeProvider{})
}

// convexHull — Andrew's monotone chain, returns CCW hull.
func convexHull(pts [][2]float64) [][2]float64 {
	if len(pts) < 3 {
		return pts
	}
	sorted := append([][2]float64(nil), pts...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i][0] != sorted[j][0] {
			return sorted[i][0] < sorted[j][0]
		}
		return sorted[i][1] < sorted[j][1]
	})
	cross := func(o, a, b [2]float64) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}
	var lower, upper [][2]float64
	for _, p := range sorted {
		for len(lower) >= 2 && cross(lower[len(lower)-2], lower[len(lower)-1], p) <= 0 {
			lower = lower[:len(lower)-1]
		}
		lower = append(lower, p)
	}
	for i := len(sorted) - 1; i >= 0; i-- {
		p := sorted[i]
		for len(upper) >= 2 && cross(upper[len(upper)-2], upper[len(upper)-1], p) <= 0 {
			upper = upper[:len(upper)-1]
		}
		upper = append(upper, p)
	}
	return append(lower[:len(lower)-1], upper[:len(upper)-1]...)
}

// ---------------------------------------------------------------------------
// Renderer
// ---------------------------------------------------------------------------

// RenderIsoPrism draws an n-gon prism with the box family's surface
// vocabulary: palette top/left/right (side walls pick left or right by
// normal dominance), stroke, opacity, dash, label + icon on the top
// face's inscribed content rect. Gradients / patterns / shadow join in
// M3/M4 with the Surface and Effect pipelines. v3.12 — palette
// top/left/rightGradient reach the prism walls (dispatched to the two
// visible wall groups by normal dominance, aligning with the box's
// left/right semantics), and Projected face fills pin to each wall's
// true iso plane via per-side matrices.
func RenderIsoPrism(o IsoBoxOpts, sides int) string {
	g := computeBoxGeom(o.Width, o.Depth, o.Height, o.Margin)
	params := map[string]any{"sides": sides}
	var prov PrismShapeProvider

	// v3.12 — per-side pin-to-plane matrices for Projected fills: the
	// wall extruded from base edge k→k+1 shears a pattern/gradient tile
	// along that edge's iso projection (sideMatrix can't serve sideK
	// names — the orientation depends on the base geometry, not the name).
	n := prismSides(params)
	base := prismBase(o.Width, o.Depth, n)
	sideTF := make(map[string]string, n)
	for k := 0; k < n; k++ {
		a, b := base[k], base[(k+1)%n]
		if tf := SideMatrixForEdge(b[0]-a[0], b[1]-a[1]); tf != "" {
			sideTF[fmt.Sprintf("side%d", k)] = tf
		}
	}

	var sb strings.Builder
	sb.WriteString(svgHeader(g.ViewW, g.ViewH))
	blurOn := emitBlurOpen(&sb, "prism-blur", o.Blur)
	// v3.4 (M4) — dropShadow + grain reach the prism family; the box
	// path has had both since v1.2/v2.6.
	shadowID, grainID := "", ""
	// v3.12 — palette gradients: same DSL fields as the box family; the
	// walls facing +x take the right gradient, the walls facing +y the
	// left one (see the normal-dominance dispatch in the face loop).
	topFill, leftFill, rightFill := o.TopFill, o.LeftFill, o.RightFill
	{
		var defs strings.Builder
		if o.TopGradient != nil {
			emitLinearGradient(&defs, "prism-grad-top", o.TopGradient)
			topFill = "url(#prism-grad-top)"
		}
		if o.LeftGradient != nil {
			emitLinearGradient(&defs, "prism-grad-left", o.LeftGradient)
			leftFill = "url(#prism-grad-left)"
		}
		if o.RightGradient != nil {
			emitLinearGradient(&defs, "prism-grad-right", o.RightGradient)
			rightFill = "url(#prism-grad-right)"
		}
		if strings.TrimSpace(o.ShadowColor) != "" && (o.ShadowDx != 0 || o.ShadowDy != 0 || o.ShadowBlur > 0) {
			shadowID = "prism-shadow"
			emitDropShadowFilter(&defs, shadowID, o.ShadowDx, o.ShadowDy, o.ShadowBlur, o.ShadowColor)
		}
		grainID = emitGrainFilter(&defs, "prism-grain", o.GrainIntensity, o.GrainScale)
		if defs.Len() > 0 {
			fmt.Fprintf(&sb, `<defs>%s</defs>`, defs.String())
		}
	}
	if strings.TrimSpace(o.BackglowColor) != "" && o.BackglowRadius > 0 {
		var gdefs, halo strings.Builder
		sil := prov.Silhouette(o.Width, o.Depth, o.Height, params)
		hpts := make([][2]float64, len(sil))
		for k, q := range sil {
			hpts[k] = [2]float64{q[0] + o.Margin, q[1] + o.Margin}
		}
		emitBackglowHalo(&halo, &gdefs, "prism-backglow", o.BackglowColor, o.BackglowRadius, o.BackglowOpacity, hpts)
		fmt.Fprintf(&sb, `<defs>%s</defs>%s`, gdefs.String(), halo.String())
	}
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
	var faceDefs strings.Builder
	type pend struct {
		name string
		pts  [][2]float64
	}
	var stroked []pend
	for _, f := range prov.Faces(o.Width, o.Depth, o.Height, params) {
		if !f.Visible {
			continue
		}
		fill := topFill
		if f.Name != "top" {
			if f.Normal[0] >= f.Normal[1] {
				fill = rightFill
			} else {
				fill = leftFill
			}
		}
		if fs := surfaceFor(o.FaceSurfaces, f.Name); fs != nil && fs.Fill != nil {
			if ref := emitFaceFillProjected(&faceDefs, "", f.Name, fs.Fill, sideTF[f.Name]); ref != "" {
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

// PrismGroundAnchor returns the point where a ray from the base
// polygon's center along (dx, dy) crosses the polygon boundary — the
// correct world-plane anchor for a prism connector. Falls back to the
// bbox edge when the ray math degenerates.
func PrismGroundAnchor(w, d float64, sides int, dx, dy float64) (float64, float64) {
	if sides < 3 {
		sides = 6
	}
	poly := prismBase(w, d, sides)
	cx, cy := w/2, d/2
	l := math.Hypot(dx, dy)
	if l == 0 {
		return cx, cy
	}
	dx, dy = dx/l, dy/l
	bestT := math.Inf(1)
	for i := 0; i < len(poly); i++ {
		a, b := poly[i], poly[(i+1)%len(poly)]
		ex, ey := b[0]-a[0], b[1]-a[1]
		den := dx*ey - dy*ex
		if math.Abs(den) < 1e-12 {
			continue
		}
		t := ((a[0]-cx)*ey - (a[1]-cy)*ex) / den
		u := ((a[0]-cx)*dy - (a[1]-cy)*dx) / den
		if t > 0 && u >= -1e-9 && u <= 1+1e-9 && t < bestT {
			bestT = t
		}
	}
	if math.IsInf(bestT, 1) {
		return cx + dx*w/2, cy + dy*d/2
	}
	return cx + dx*bestT, cy + dy*bestT
}
