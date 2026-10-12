package isotopo

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// ConnectorSpatialVisibility reports a physical depth decision separately
// from legacy painter coverage. Compositing draws each original primitive
// once, preserving gradient, dash phase, round joins and identity. ClipRegion
// describes geometric visibility; Compositing reports the actual paint policy.
// Scope covers supported opaque surfaces and solid round face borders, with
// a billboard-width stroke convention. It does not certify body/body order,
// labels, filters or alpha compositing. A fallback retains the old SVG bytes.
type ConnectorSpatialVisibility struct {
	Mode               string                    `json:"mode"` // depth | legacy
	Reason             string                    `json:"reason,omitempty"`
	BoundaryError      float64                   `json:"boundary_error,omitempty"`
	Spans              []ConnectorDepthSpan      `json:"spans,omitempty"`
	ClipRegion         screenRegion              `json:"clip_region,omitempty"`
	Penetrations       []ConnectorPenetration    `json:"penetrations,omitempty"`
	SubmittedCollision *ConnectorCollisionReport `json:"submitted_collision,omitempty"`
	Arrow              *ConnectorArrowVisibility `json:"arrow,omitempty"`
	Compositing        *ConnectorCompositing     `json:"compositing,omitempty"`
}
type ConnectorDepthSpan struct {
	Segment   int      `json:"segment"`
	T0        float64  `json:"t0"`
	T1        float64  `json:"t1"`
	Occluders []string `json:"occluders"`
}
type ConnectorPenetration struct {
	// Historical pre-clip diagnostic. Final-path intervals are separate in
	// SubmittedCollision so trimming cannot silently change this index space.
	RouteSegment int     `json:"route_segment"`
	T0           float64 `json:"t0"`
	T1           float64 `json:"t1"`
	Part         string  `json:"part"`
}
type spatialBox struct {
	id     string
	lo, hi [3]float64
}
type connectorSpatialScene struct {
	surfaces []spatialPiece
	boxes    []spatialBox
	reason   string
}

const spatialPieceBudget = 8192

func prepareConnectorSpatial(parts []iso25d.CompositePart) connectorSpatialScene {
	out := connectorSpatialScene{}
	for i, p := range parts {
		id := p.ID
		if id == "" {
			id = fmt.Sprintf("#part:%d", i)
		}
		b := iso25d.ShapeSpatialBody(p.Shape, p.Opts)
		if b.Reason != "" {
			out.reason = id + ": " + b.Reason
			return out
		}
		off := [3]float64{p.OffWX, p.OffWY, p.OffWZ}
		out.boxes = append(out.boxes, spatialBox{id, off, [3]float64{off[0] + b.Size[0], off[1] + b.Size[1], off[2] + b.Size[2]}})
		for _, face := range b.Faces {
			var points [][2]float64
			var depths []float64
			for _, w := range face.World {
				for k := range w {
					w[k] += off[k]
				}
				sx, sy := projectIso(w[0], w[1], w[2])
				points = append(points, [2]float64{sx, sy})
				depths = append(depths, w[0]+w[1]+w[2])
			}
			plane, ok := planeThrough(points[0], points[1], points[2], depths[0], depths[1], depths[2])
			if !ok {
				out.reason = id + ": degenerate projected face"
				return out
			}
			out.surfaces = append(out.surfaces, spatialPiece{spatialCCW(points), plane, id})
			if face.StrokeWidth > 0 {
				for j, a := range points {
					k := (j + 1) % len(points)
					pieces, ok := spatialRibbon(a, points[k], depths[j], depths[k], face.StrokeWidth/2)
					if !ok {
						out.reason = id + ": stroke approximation budget"
						return out
					}
					for _, s := range pieces {
						s.owner = id
						out.surfaces = append(out.surfaces, s)
					}
				}
			}
		}
	}
	return out
}

type spatialLine struct {
	a, b    [2]float64
	da, db  float64
	segment int
	t0, t1  float64
}

// Read the emitter's parameter provenance. Screen overlap cannot identify a
// world leg: distinct heights can have exactly the same projection.
func spatialLines(r *ConnectorRoute) ([]spatialLine, string) {
	d := r.Drawing
	segments, why := connectorWorldLines(d)
	if why != "" {
		return nil, why
	}
	var out []spatialLine
	for si, s := range segments {
		a, b := d.Points[si], d.Points[si+1]
		if math.Hypot(b[0]-a[0], b[1]-a[1]) < .02 {
			continue
		}
		for _, point := range []*[2]float64{&a, &b} {
			point[0] -= d.ProjectionOrigin[0]
			point[1] -= d.ProjectionOrigin[1]
		}
		w, v := s.World[0], s.World[1]
		out = append(out, spatialLine{a, b, w[0] + w[1] + w[2], v[0] + v[1] + v[2], si, 0, 1})
	}
	if len(out) == 0 {
		return nil, "empty projected route"
	}
	return out, ""
}

func spatialVisibleRegion(lines []spatialLine, surfaces []spatialPiece, radius float64) (screenRegion, bool) {
	var out screenRegion
	for _, line := range lines {
		pieces, ok := spatialRibbon(line.a, line.b, line.da, line.db, radius)
		if !ok {
			return nil, false
		}
		for _, piece := range pieces {
			remaining, ok := spatialVisiblePiece(piece, surfaces, spatialPieceBudget-len(out))
			if !ok {
				return nil, false
			}
			out = append(out, remaining...)
			if len(out) > spatialPieceBudget {
				return nil, false
			}
		}
	}
	return out, true
}

// The same surface comparison clips round line pieces and direction markers.
// Each consumer supplies its declared billboard depth field and paint guard.
func spatialVisiblePiece(piece spatialPiece, surfaces []spatialPiece, budget int) (screenRegion, bool) {
	remaining := screenRegion{piece.poly}
	for _, face := range surfaces {
		if !spatialBoundsOverlap(piece.poly, face.poly) {
			continue
		}
		// Equal depth remains visible, including deliberate surface contact.
		delta := depthPlane{face.depth[0] - piece.depth[0], face.depth[1] - piece.depth[1], face.depth[2] - piece.depth[2] - .0001}
		cut := spatialHalfPlane(face.poly, delta)
		if len(cut) == 0 {
			continue
		}
		var next screenRegion
		for _, poly := range remaining {
			if !spatialBoundsOverlap(poly, cut) {
				next = append(next, poly)
			} else {
				next = append(next, spatialSubtract(poly, cut)...)
			}
			if len(next) > budget {
				return nil, false
			}
		}
		remaining = next
		if len(remaining) == 0 {
			break
		}
	}
	return remaining, len(remaining) <= budget
}
func spatialDepthSpans(lines []spatialLine, surfaces []spatialPiece) []ConnectorDepthSpan {
	var out []ConnectorDepthSpan
	for _, line := range lines {
		q := quadratic{line.a, {(line.a[0] + line.b[0]) / 2, (line.a[1] + line.b[1]) / 2}, line.b}
		cuts := []float64{0, 1}
		var faces []spatialPiece
		for _, f := range surfaces {
			if !spatialBoundsOverlap([][2]float64{line.a, line.b}, f.poly) {
				continue
			}
			faces = append(faces, f)
			cuts = append(cuts, coverageParameters(q, f.poly)...)
			da, db := f.depth.at(line.a)-line.da-.0001, f.depth.at(line.b)-line.db-.0001
			if da != db {
				t := da / (da - db)
				if t > 0 && t < 1 {
					cuts = append(cuts, t)
				}
			}
		}
		sort.Float64s(cuts)
		for j := 1; j < len(cuts); j++ {
			lo, hi := cuts[j-1], cuts[j]
			if hi-lo < 1e-9 {
				continue
			}
			t := (lo + hi) / 2
			p := q.at(t)
			dep := line.da + t*(line.db-line.da)
			var ids []string
			for _, f := range faces {
				if f.depth.at(p) > dep+.0001 && polygonContains(p, f.poly) {
					found := false
					for _, id := range ids {
						found = found || id == f.owner
					}
					if !found {
						ids = append(ids, f.owner)
					}
				}
			}
			sort.Strings(ids)
			s := ConnectorDepthSpan{line.segment, line.t0 + lo*(line.t1-line.t0), line.t0 + hi*(line.t1-line.t0), ids}
			if len(out) > 0 {
				prev := &out[len(out)-1]
				if prev.Segment == s.Segment && math.Abs(prev.T1-s.T0) < 1e-8 && strings.Join(prev.Occluders, "\x00") == strings.Join(ids, "\x00") {
					prev.T1 = s.T1
					continue
				}
			}
			out = append(out, s)
		}
	}
	return out
}
func spatialPenetrations(route [][3]float64, boxes []spatialBox) []ConnectorPenetration {
	var out []ConnectorPenetration
	for j := 0; j+1 < len(route); j++ {
		a, b := route[j], route[j+1]
		for _, box := range boxes {
			lo, hi := 0., 1.
			hit := true
			for k := 0; k < 3; k++ {
				d := b[k] - a[k]
				if math.Abs(d) < 1e-10 {
					if a[k] <= box.lo[k]+1e-6 || a[k] >= box.hi[k]-1e-6 {
						hit = false
						break
					}
					continue
				}
				u, v := (box.lo[k]-a[k])/d, (box.hi[k]-a[k])/d
				if u > v {
					u, v = v, u
				}
				lo = math.Max(lo, u)
				hi = math.Min(hi, v)
			}
			if hit && hi-lo > 1e-8 {
				out = append(out, ConnectorPenetration{j, lo, hi, box.id})
			}
		}
	}
	return out
}
func spatialClipMarkup(id string, polys screenRegion, origin [2]float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<clipPath id="%s" clipPathUnits="userSpaceOnUse">`, id)
	// One nonzero path computes the union before antialiasing. Separate
	// polygon clip children leave seams along shared subdivision boundaries.
	b.WriteString(`<path clip-rule="nonzero" d="`)
	for _, poly := range polys {
		for i, p := range poly {
			if i == 0 {
				b.WriteByte('M')
			} else {
				b.WriteByte('L')
			}
			fmt.Fprintf(&b, "%.5f,%.5f", p[0]+origin[0], p[1]+origin[1])
		}
		b.WriteByte('Z')
	}
	b.WriteString(`"/>`)
	b.WriteString(`</clipPath>`)
	return b.String()
}

// A clip must not coincide with the stroke's own antialiased outer edge:
// applying both coverages makes an unobstructed stroke lighter. Leave a guard
// around the subject, then subtract the exact same foreground occluders.
// Four SVG units are verified at scales 0.25, 1 and 4 in this experiment;
// a public compositor needs the display/export pixel footprint in its contract.
func spatialStrokeClip(lines []spatialLine, surfaces []spatialPiece, width float64) (screenRegion, bool) {
	return spatialVisibleRegion(lines, surfaces, width/2+4)
}

// Re-layer complete connector paths only after every route has a supported
// result. No per-piece path emission: dash/gradient phase and antialias alpha
// remain properties of the original, singly drawn SVG path.
func applyConnectorSpatial(svg string, rc *routeCollector, scene connectorSpatialScene, parts int) string {
	if rc == nil {
		return svg
	}
	fallback := func(reason string) string {
		for i := range rc.routes {
			if d := rc.routes[i].Drawing; d != nil {
				d.Spatial = &ConnectorSpatialVisibility{Mode: "legacy", Reason: reason}
			}
		}
		return svg
	}
	if scene.reason != "" {
		return fallback(scene.reason)
	}
	type solved struct {
		visibility ConnectorSpatialVisibility
		hit        screenRegion
	}
	results := make([]solved, len(rc.routes))
	for i := range rc.routes {
		r := &rc.routes[i]
		collision := submittedConnectorCollisions(r.Drawing, scene.boxes)
		if collision.Status != "complete" {
			return fallback(fmt.Sprintf("connector %d: %s", r.Index, collision.Reason))
		}
		lines, why := spatialLines(r)
		if why != "" {
			return fallback(fmt.Sprintf("connector %d: %s", r.Index, why))
		}
		arrow, why := spatialArrowVisibility(r.Drawing, lines, scene.surfaces)
		if why != "" {
			return fallback(fmt.Sprintf("connector %d arrow: %s", r.Index, why))
		}
		region, ok := spatialStrokeClip(lines, scene.surfaces, r.Drawing.StrokeWidth)
		if !ok {
			return fallback("visibility fragment budget")
		}
		hit, ok := spatialStrokeClip(lines, scene.surfaces, math.Max(10, r.Drawing.StrokeWidth))
		if !ok {
			return fallback("hit-region fragment budget")
		}
		results[i] = solved{ConnectorSpatialVisibility{Mode: "depth", BoundaryError: .04, Spans: spatialDepthSpans(lines, scene.surfaces), ClipRegion: region, Penetrations: spatialPenetrations(r.World3D, scene.boxes), SubmittedCollision: &collision, Arrow: arrow}, hit}
	}
	const startTag = `<g data-layer="connectors">`
	out := svg
	var routes strings.Builder
	for {
		start := strings.Index(out, startTag)
		if start < 0 {
			break
		}
		end := strings.Index(out[start+len(startTag):], `</g>`)
		if end < 0 {
			return fallback("missing connector layer boundary")
		}
		end += start + len(startTag)
		routes.WriteString(out[start+len(startTag) : end])
		out = out[:start] + out[end+4:]
	}
	if routes.Len() == 0 {
		return svg
	}
	// Establish the complete output envelope before touching any observation.
	// SVG and its route metadata commit as one transaction.
	at := strings.Index(out, `<g data-layer="connector-overlay">`)
	if at < 0 {
		at = strings.LastIndex(out, `</svg>`)
	}
	if at < 0 {
		return fallback("missing root boundary")
	}
	body := routes.String()
	var defs strings.Builder
	arrowIDs := make(map[int]string)
	for i := range rc.routes {
		r := &rc.routes[i]
		d := r.Drawing
		result := results[i]
		id := fmt.Sprintf("conn-visible-%d", r.Index)
		hitID := fmt.Sprintf("conn-hit-visible-%d", r.Index)
		if strings.Contains(svg, `id="`+id+`"`) || strings.Contains(svg, `id="`+hitID+`"`) {
			return fallback("visibility ID collision")
		}
		defs.WriteString(spatialClipMarkup(id, result.visibility.ClipRegion, d.ProjectionOrigin))
		defs.WriteString(spatialClipMarkup(hitID, result.hit, d.ProjectionOrigin))
		if a := result.visibility.Arrow; a != nil {
			arrowID := fmt.Sprintf("conn-arrow-visible-%d", r.Index)
			if strings.Contains(svg, `id="`+arrowID+`"`) {
				return fallback("arrow visibility ID collision")
			}
			defs.WriteString(spatialClipMarkup(arrowID, a.ClipRegion, d.ProjectionOrigin))
			arrowIDs[r.Index] = arrowID
		}
		token := fmt.Sprintf(`data-connector="%d"`, r.Index)
		if strings.Count(body, token) != 1 {
			return fallback("connector identity missing or duplicated")
		}
		body = strings.Replace(body, token, token+fmt.Sprintf(` clip-path="url(#%s)" data-hit-clip="url(#%s)"`, id, hitID), 1)
	}
	var why string
	out, why = clipSubmittedArrows(out, rc, arrowIDs)
	if why != "" {
		return fallback(why)
	}
	layer := `<defs>` + defs.String() + `</defs><g data-layer="connectors">` + body + `</g>`
	result := out[:at] + layer + out[at:]
	visibility := make(map[int]ConnectorSpatialVisibility, len(rc.routes))
	for i, r := range rc.routes {
		visibility[r.Index] = results[i].visibility
	}
	ordered := spatialPainterCompose(result, rc, scene, parts, visibility)
	if ordered.fatal {
		return fallback(ordered.reason)
	}
	composition := &ConnectorCompositing{Mode: "clipped", Reason: ordered.reason, Guard: spatialPainterGuard}
	if ordered.svg != "" {
		result = ordered.svg
		composition.Mode = "painter"
		composition.Reason = ""
	}
	// Publish the body's actual order with the SVG, before recomputing any
	// consumer that indexes PaintBeforePart. Keep geometry identities stable.
	if len(ordered.partOrder) > 0 {
		paint := make([]connectorPaintPart, len(ordered.partOrder))
		partPaint := make([]submittedPartPaint, len(ordered.partOrder))
		for rank, index := range ordered.partOrder {
			paint[rank] = rc.paint[index]
			partPaint[rank] = submittedPartPaint{paint[rank].id, index, rank}
		}
		rc.paint, rc.partPaint = paint, partPaint
	}
	for i := range rc.routes {
		d := rc.routes[i].Drawing
		d.Spatial = &results[i].visibility
		d.PaintBeforePart = parts
		c := *composition
		c.LineClipOwners, c.ArrowClipOwners = ordered.lineClips[rc.routes[i].Index], ordered.arrowClips[rc.routes[i].Index]
		if c.Mode == "painter" && (len(c.LineClipOwners) > 0 || len(c.ArrowClipOwners) > 0) {
			c.Mode = "hybrid"
		}
		d.Spatial.Compositing = &c
		if composition.Mode == "painter" {
			d.PaintBeforePart = ordered.layers[rc.routes[i].Index]
			if d.Spatial.Arrow != nil {
				v := ordered.arrowLayers[rc.routes[i].Index]
				d.Spatial.Arrow.PaintBeforePart = &v
			}
		}
		d.Coverage = observeConnectorCoverage(d, rc.paint)
	}
	return result
}
