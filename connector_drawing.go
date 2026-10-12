package isotopo

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// ConnectorDrawing records what the SVG emitter submitted, in root SVG user
// coordinates. A quadratic has [start, control, end]; a polyline has vertices.
// These points are not all visible: Coverage describes only later opaque BODY
// FILLS at the centreline, not stroke width, dashes, text, effects, overlay
// labels, or true 3-D visibility. Paint order is observed, not certified correct.
type ConnectorDrawing struct {
	Kind             string                      `json:"kind"`
	Points           [][2]float64                `json:"points"`
	SourceRef        string                      `json:"source_ref"`
	TargetRef        string                      `json:"target_ref"`
	ResolvedSource   string                      `json:"resolved_source"`
	ResolvedTarget   string                      `json:"resolved_target"`
	ProjectionOrigin [2]float64                  `json:"projection_origin"`
	PaintBeforePart  int                         `json:"paint_before_part"`
	StrokeWidth      float64                     `json:"stroke_width"`
	Arrow            [][2]float64                `json:"arrow,omitempty"` // actual overlay polygon
	Label            *ConnectorLabel             `json:"label,omitempty"`
	Coverage         ConnectorBodyCoverage       `json:"body_coverage"`
	Editable         []ConnectorEditSpan         `json:"editable,omitempty"`
	Provenance       *ConnectorProvenance        `json:"provenance,omitempty"`
	Attachments      *ConnectorAttachments       `json:"attachments,omitempty"`
	Spatial          *ConnectorSpatialVisibility `json:"spatial,omitempty"`
}

// ConnectorLabel is the actual rounded pill rectangle and text anchor. Its
// Rect is the pill envelope, not glyph ink. Optional Typography supplies the
// measured lines for a fully bound font. Arrow/label paint in the connector
// overlay, independently of the route's PaintBeforePart.
type ConnectorLabel struct {
	Text       string               `json:"text"`
	Rect       [4]float64           `json:"rect"` // x, y, width, height
	Anchor     [2]float64           `json:"anchor"`
	Typography *ConnectorTypography `json:"typography,omitempty"`
}

// ConnectorBodyCoverage partitions every emitted segment into ordered,
// disjoint [T0,T1] spans. Empty OpaqueParts means no KNOWN opaque body fill,
// not proven pixel visibility. UnresolvedParts identifies potentially relevant
// bodies for which coverage could not be established. BoundaryError is the
// declared polygonal curve approximation plus SVG coordinate rounding.
type ConnectorBodyCoverage struct {
	Scope           string               `json:"scope"`
	BoundaryError   float64              `json:"boundary_error"`
	Spans           []ConnectorCoverSpan `json:"spans"`
	UnresolvedParts []string             `json:"unresolved_parts,omitempty"`
}

type ConnectorCoverSpan struct {
	Segment     int      `json:"segment"`
	T0          float64  `json:"t0"`
	T1          float64  `json:"t1"`
	OpaqueParts []string `json:"opaque_parts"` // actual painter order, no double counting
}

type connectorPaintPart struct {
	id        string
	region    screenRegion
	uncertain bool
	error     float64
}

func svgCoord(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 2, 64), 64)
	return f
}

func svgPoints(pts [][2]float64) [][2]float64 {
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{svgCoord(p[0]), svgCoord(p[1])}
	}
	return out
}

func (rc *routeCollector) addPaint(id, shape string, opts iso25d.ConvertOpts, wx, wy, wz float64) {
	if rc == nil || rc.preClipOnly {
		return
	}
	p := iso25d.ShapePaint(shape, opts, 1) // only complete opacity proves coverage
	if id == "" {
		id = fmt.Sprintf("#part:%d", len(rc.paint))
	}
	sx, sy := projectIso(wx, wy, wz)
	region := screenRegion(p.Polygons)
	for _, poly := range region {
		for i := range poly {
			poly[i][0] += sx
			poly[i][1] += sy
		}
	}
	rc.paint = append(rc.paint, connectorPaintPart{id, region, p.Uncertain, p.MaxError})
}

// connectorPaintLayer is also used by SVG insertion, preventing the observer
// from maintaining a second approximation of the renderer's layer policy.
func connectorPaintLayer(from, to string, indexes map[string]int, substrates int, depthSort bool) int {
	if !depthSort {
		return substrates
	}
	fi, okF := indexes[connectorTarget(from)]
	ti, okT := indexes[connectorTarget(to)]
	if !okF || !okT {
		return substrates
	}
	return max(substrates, min(fi, ti))
}

func drawingSegments(d *ConnectorDrawing) []quadratic {
	if d.Kind == "quadratic" && len(d.Points) == 3 {
		return []quadratic{{d.Points[0], d.Points[1], d.Points[2]}}
	}
	var out []quadratic
	for i := 1; i < len(d.Points); i++ {
		a, b := d.Points[i-1], d.Points[i]
		out = append(out, quadratic{a, {(a[0] + b[0]) / 2, (a[1] + b[1]) / 2}, b})
	}
	return out
}

func curveBounds(q quadratic) screenRect {
	r := screenRect{q[0][0], q[0][1], q[0][0], q[0][1]}
	for _, p := range q[1:] {
		r.x0, r.y0 = math.Min(r.x0, p[0]), math.Min(r.y0, p[1])
		r.x1, r.y1 = math.Max(r.x1, p[0]), math.Max(r.y1, p[1])
	}
	return r // conservative control hull for broad phase only
}

// coverageParameters intersects the actual quadratic with every polygon
// boundary. Collinear overlaps need the edge endpoints as cuts too; a tangent
// alone contributes no interval of coverage.
func coverageParameters(q quadratic, poly [][2]float64) []float64 {
	var out []float64
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		den := dx*dx + dy*dy
		if den < 1e-20 {
			continue
		}
		cross := func(x, y float64) float64 { return x*dy - y*dx }
		aa := cross(q[0][0]-2*q[1][0]+q[2][0], q[0][1]-2*q[1][1]+q[2][1])
		bb := 2 * cross(q[1][0]-q[0][0], q[1][1]-q[0][1])
		cc := cross(q[0][0]-a[0], q[0][1]-a[1])
		roots := quadraticRoots(aa, bb, cc)
		if aa == 0 && bb == 0 && cc == 0 {
			axis := 0
			if math.Abs(dy) > math.Abs(dx) {
				axis = 1
			}
			for _, p := range [][2]float64{a, b} {
				roots = append(roots, quadraticRoots(q[0][axis]-2*q[1][axis]+q[2][axis], 2*(q[1][axis]-q[0][axis]), q[0][axis]-p[axis])...)
			}
		}
		for _, t := range roots {
			if t <= 0 || t >= 1 {
				continue
			}
			p := q.at(t)
			u := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / den
			if u >= -1e-10 && u <= 1+1e-10 {
				out = append(out, t)
			}
		}
	}
	return out
}

func observeConnectorCoverage(d *ConnectorDrawing, parts []connectorPaintPart) ConnectorBodyCoverage {
	out := ConnectorBodyCoverage{Scope: "opaque-body-fill-centerline", BoundaryError: .03, Spans: []ConnectorCoverSpan{}}
	parts = parts[min(d.PaintBeforePart, len(parts)):]
	for _, p := range parts {
		out.BoundaryError = math.Max(out.BoundaryError, p.error+.03)
	}
	// Move the query into the unshifted projected frame used by paint regions.
	for segment, original := range drawingSegments(d) {
		q := original
		for i := range q {
			q[i][0] -= d.ProjectionOrigin[0]
			q[i][1] -= d.ProjectionOrigin[1]
		}
		ts := []float64{0, 1}
		var relevant []connectorPaintPart
		bounds := curveBounds(q)
		for _, p := range parts {
			// Unresolved geometry currently lacks a trustworthy outer coverage
			// bound. Keep its identity, even if that conservatively overreports.
			if p.uncertain && !slices.Contains(out.UnresolvedParts, p.id) {
				out.UnresolvedParts = append(out.UnresolvedParts, p.id)
			}
			hit := false
			for _, poly := range p.region {
				if len(poly) < 3 {
					continue
				}
				pb := regionBounds(screenRegion{poly})
				if pb.x1 < bounds.x0 || pb.x0 > bounds.x1 || pb.y1 < bounds.y0 || pb.y0 > bounds.y1 {
					continue
				}
				hit = true
				ts = append(ts, coverageParameters(q, poly)...)
			}
			if hit {
				relevant = append(relevant, p)
			}
		}
		sort.Float64s(ts)
		for i := 1; i < len(ts); i++ {
			lo, hi := ts[i-1], ts[i]
			if hi-lo < 1e-12 {
				continue
			}
			ids := []string{}
			for _, p := range relevant {
				if p.region.contains(q.at((lo+hi)/2)) && !slices.Contains(ids, p.id) {
					ids = append(ids, p.id)
				}
			}
			n := len(out.Spans)
			if n > 0 && out.Spans[n-1].Segment == segment && slices.Equal(out.Spans[n-1].OpaqueParts, ids) {
				out.Spans[n-1].T1 = hi
			} else {
				out.Spans = append(out.Spans, ConnectorCoverSpan{segment, lo, hi, ids})
			}
		}
	}
	return out
}
