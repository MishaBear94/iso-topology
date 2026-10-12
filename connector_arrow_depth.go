package isotopo

import (
	"fmt"
	"math"
	"strings"
)

// An arrow is a direction marker on the terminal route's tangent billboard:
// depth varies along the terminal line and is constant across its width. Its
// owner identifies the attachment; it does NOT exempt any body from occlusion.
// ClipRegion includes a guard and must clip the original polygon once.
type ConnectorArrowVisibility struct {
	DepthConvention string       `json:"depth_convention"`
	OwnerID         string       `json:"owner_id"`
	TerminalSegment int          `json:"terminal_segment"`
	DepthPlane      depthPlane   `json:"depth_plane"` // relative to ProjectionOrigin
	ClipRegion      screenRegion `json:"clip_region"`
	PaintBeforePart *int         `json:"paint_before_part,omitempty"`
}

func spatialArrowVisibility(d *ConnectorDrawing, lines []spatialLine, surfaces []spatialPiece) (*ConnectorArrowVisibility, string) {
	if len(d.Arrow) == 0 {
		return nil, ""
	}
	if len(d.Arrow) != 3 || d.Attachments == nil || len(lines) == 0 {
		return nil, "missing triangle attachment"
	}
	a := d.Attachments.Target
	line := lines[len(lines)-1]
	if line.segment != len(d.Points)-2 || a.RouteSegment != line.segment || a.RouteT != 1 ||
		a.World.Status != "known" || a.World.Convention != "producer-route-lift" || a.World.Point == nil || a.InstanceID == "" ||
		a.Point != d.Points[len(d.Points)-1] || a.Point != d.Arrow[0] {
		return nil, "unresolved terminal attachment"
	}
	w := *a.World.Point
	if w != d.Provenance.Segments[line.segment].World[1] {
		return nil, "terminal world attachment mismatch"
	}
	dx, dy := line.b[0]-line.a[0], line.b[1]-line.a[1]
	den := dx*dx + dy*dy
	if den < .02*.02 {
		return nil, "degenerate terminal tangent"
	}
	z := depthPlane{(line.db - line.da) * dx / den, (line.db - line.da) * dy / den, 0}
	z[2] = line.db - z[0]*line.b[0] - z[1]*line.b[1]
	for _, v := range z {
		if !connectorFinite(v) {
			return nil, "non-finite arrow depth"
		}
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range d.Arrow {
		if !connectorFinite(p[0]) || !connectorFinite(p[1]) {
			return nil, "non-finite arrow polygon"
		}
		x, y := p[0]-d.ProjectionOrigin[0], p[1]-d.ProjectionOrigin[1]
		x0, y0, x1, y1 = math.Min(x0, x), math.Min(y0, y), math.Max(x1, x), math.Max(y1, y)
	}
	// Do not clip on the triangle's own antialiased edge. The final polygon,
	// gradient and alpha remain exactly as submitted, with a single clip.
	const guard = 4.
	box := [][2]float64{{x0 - guard, y0 - guard}, {x1 + guard, y0 - guard}, {x1 + guard, y1 + guard}, {x0 - guard, y1 + guard}}
	region, ok := spatialVisiblePiece(spatialPiece{poly: box, depth: z}, surfaces, spatialPieceBudget)
	if !ok {
		return nil, "arrow visibility fragment budget"
	}
	return &ConnectorArrowVisibility{DepthConvention: "terminal-tangent-billboard", OwnerID: a.InstanceID, TerminalSegment: line.segment, DepthPlane: z, ClipRegion: region}, ""
}

// Use emitter-owned offsets rather than matching polygons by coordinates.
// Coincident arrows can have different world heights and clip decisions.
func clipSubmittedArrows(svg string, rc *routeCollector, ids map[int]string) (string, string) {
	if len(ids) == 0 && len(rc.arrowFragments) == 0 {
		return svg, ""
	}
	const tag = `<g data-layer="connector-overlay">`
	start := strings.Index(svg, tag)
	if start < 0 || strings.Count(svg, tag) != 1 {
		return svg, "missing or duplicate arrow layer"
	}
	start += len(tag)
	end := strings.Index(svg[start:], `</g>`)
	if end < 0 {
		return svg, "missing arrow layer boundary"
	}
	end += start
	byIndex := make(map[int]*ConnectorDrawing)
	for i := range rc.routes {
		byIndex[rc.routes[i].Index] = rc.routes[i].Drawing
	}
	seen := make(map[int]bool)
	previous := 0
	for _, f := range rc.arrowFragments {
		d := byIndex[f.index]
		if ids[f.index] == "" || seen[f.index] || d == nil || len(d.Arrow) != 3 || f.offset < previous || f.offset > end-start || len(f.markup) > end-start-f.offset ||
			svg[start+f.offset:start+f.offset+len(f.markup)] != f.markup || !strings.HasPrefix(f.markup, "<polygon ") || !strings.HasSuffix(f.markup, "/>") {
			return svg, "arrow identity missing or inconsistent"
		}
		p := d.Arrow
		want := fmt.Sprintf("%.2f,%.2f %.2f,%.2f %.2f,%.2f", p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1])
		if extractAttr(f.markup, "points") != want {
			return svg, "arrow polygon differs from observation"
		}
		seen[f.index] = true
		previous = f.offset + len(f.markup)
	}
	if len(seen) != len(ids) {
		return svg, "incomplete arrow identities"
	}
	for i := len(rc.arrowFragments) - 1; i >= 0; i-- {
		f := rc.arrowFragments[i]
		at := start + f.offset + len(f.markup) - 2
		svg = svg[:at] + fmt.Sprintf(` clip-path="url(#%s)"`, ids[f.index]) + svg[at:]
	}
	return svg, ""
}
