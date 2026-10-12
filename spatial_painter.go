package isotopo

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Painter compositing avoids multiplying a foreground body's antialias
// coverage by a second, complementary clip. Each original primitive is drawn
// once. A whole-primitive order must satisfy every supported overlap; cycles
// and budget exhaustion retain the existing clip path. Mixed-depth bodies retain
// local clips and contribute ordering constraints before any SVG is committed.
type ConnectorCompositing struct {
	Mode            string   `json:"mode"` // painter | hybrid | clipped
	Reason          string   `json:"reason,omitempty"`
	Guard           float64  `json:"guard_svg_units"`
	LineClipOwners  []string `json:"line_clip_owners,omitempty"`
	ArrowClipOwners []string `json:"arrow_clip_owners,omitempty"`
}

const spatialPainterGuard = 4.
const spatialPainterWork = 2000000
const spatialPainterNodes = 1024

type painterPiece struct {
	spatialPiece
	bounds occRect
}
type painterNode struct {
	kind        string
	index       int
	start, end  int
	markup      string
	pieces      []painterPiece
	orderLocked bool // emitted text/image ink lacks a certified surface depth
}
type painterResult struct {
	svg                   string
	layers, arrowLayers   map[int]int
	reason                string
	lineClips, arrowClips map[int][]string
	fatal                 bool
	partOrder             []int
}

func painterBounded(p spatialPiece) painterPiece {
	return painterPiece{p, regionBounds(screenRegion{p.poly})}
}
func painterBoundsMeet(a, b occRect) bool {
	return a.x0 <= b.x1 && b.x0 <= a.x1 && a.y0 <= b.y1 && b.y0 <= a.y1
}

func painterIntersection(a, b [][2]float64, work *int) ([][2]float64, bool) {
	q := a
	// Alternate owned buffers; no input or returned polygon shares scratch
	// with another intersection. Keep every clipping step and budget charge.
	var scratch [2][][2]float64
	next := 0
	for i, p := range b {
		*work -= len(q) + 1
		if *work < 0 {
			return nil, false
		}
		n := b[(i+1)%len(b)]
		if cap(scratch[next]) < len(q)+1 {
			scratch[next] = make([][2]float64, 0, len(q)+1)
		}
		q = spatialHalfPlaneInto(q, depthPlane{p[1] - n[1], n[0] - p[0], p[0]*n[1] - p[1]*n[0]}, scratch[next])
		scratch[next] = q
		next = 1 - next
		if len(q) == 0 {
			return nil, true
		}
	}
	return q, true
}

// A conservative convex offset adds the declared antialias guard to arrows.
// Its AABB caps acute miters while still containing the rounded Minkowski offset. No convex hull is substituted for an unsupported body.
func painterExpand(poly [][2]float64, r float64, work *int) ([][2]float64, bool) {
	b := regionBounds(screenRegion{poly})
	q := [][2]float64{{b.x0 - r, b.y0 - r}, {b.x1 + r, b.y0 - r}, {b.x1 + r, b.y1 + r}, {b.x0 - r, b.y1 + r}}
	for i, p := range poly {
		*work -= len(q) + 1
		if *work < 0 {
			return nil, false
		}
		n := poly[(i+1)%len(poly)]
		dx, dy := n[0]-p[0], n[1]-p[1]
		q = spatialHalfPlane(q, depthPlane{-dy, dx, p[0]*n[1] - p[1]*n[0] + r*math.Hypot(dx, dy)})
	}
	return q, true
}

// Classify only the overlap with this subject, against the body's nearest
// surface. A border hidden by a nearer fill cannot impose a reverse order.
// Bits 1/2 mean subject/body must be nearer. Equal depth keeps the subject
// visible, as in spatialVisiblePiece. All operations use whole polygons.
func painterSubjectRelation(subject, body painterNode, work *int) (int, bool) {
	return painterSubjectRelationTolerance(subject, body, work, .0001)
}

func painterSubjectRelationTolerance(subject, body painterNode, work *int, tolerance float64) (int, bool) {
	relation := 0
	for _, p := range subject.pieces {
		visible := screenRegion{p.poly}
		var overlaps screenRegion
		for _, q := range body.pieces {
			*work--
			if *work < 0 {
				return 0, false
			}
			if !painterBoundsMeet(p.bounds, q.bounds) {
				continue
			}
			overlap, ok := painterIntersection(p.poly, q.poly, work)
			if !ok {
				return 0, false
			}
			if len(overlap) == 0 {
				continue
			}
			h := depthPlane{q.depth[0] - p.depth[0], q.depth[1] - p.depth[1], q.depth[2] - p.depth[2] - tolerance}
			*work -= len(q.poly) + len(overlap)
			if *work < 0 {
				return 0, false
			}
			// An affine depth difference positive at every overlap vertex
			// proves the whole convex overlap hidden by this surface. Do not
			// let floating-point subtraction slivers later demand the reverse
			// paint order (and create a platform-dependent dependency cycle).
			fullyHidden := true
			for _, v := range overlap {
				if !(h.at(v) > 0) {
					fullyHidden = false
					break
				}
			}
			if !fullyHidden {
				overlaps = append(overlaps, overlap)
			}
			if len(spatialHalfPlane(overlap, h)) == 0 {
				continue
			}
			cut := spatialHalfPlane(q.poly, h)
			if len(cut) == 0 {
				continue
			}
			relation |= 2
			var next screenRegion
			for _, poly := range visible {
				*work--
				if *work < 0 {
					return 0, false
				}
				if !spatialBoundsOverlap(poly, cut) {
					next = append(next, poly)
					continue
				}
				*work -= 2 * len(cut) * (len(poly) + len(cut))
				if *work < 0 {
					return 0, false
				}
				next = append(next, spatialSubtract(poly, cut)...)
				if len(next) > spatialPieceBudget {
					return 0, false
				}
			}
			visible = next
			if len(visible) == 0 {
				break
			}
		}
		// Remaining area outside the body's projection supplies no ordering
		// constraint. Only a visible intersection can demand subject on top.
		for _, poly := range visible {
			for _, overlap := range overlaps {
				*work--
				if *work < 0 {
					return 0, false
				}
				if !spatialBoundsOverlap(poly, overlap) {
					continue
				}
				v, ok := painterIntersection(poly, overlap, work)
				if !ok {
					return 0, false
				}
				if len(v) > 0 {
					relation |= 1
					break
				}
			}
			if relation&1 != 0 {
				break
			}
		}
		if relation == 3 {
			return relation, true
		}
	}
	return relation, true
}

// Connector stacking stays compatible. Body/body pairs are resolved separately
// by painterBodyRelation, including explicit content and depth constraints.
func painterRelation(a, b painterNode, work *int) (int, bool) {
	if a.kind == "part" && b.kind == "part" {
		return 0, true
	}
	if b.kind == "part" {
		return painterSubjectRelation(a, b, work)
	}
	if a.kind == "part" {
		r, ok := painterSubjectRelation(b, a, work)
		return (r&1)<<1 | (r&2)>>1, ok
	}
	for _, p := range a.pieces {
		for _, q := range b.pieces {
			*work--
			if *work < 0 {
				return 0, false
			}
			if !painterBoundsMeet(p.bounds, q.bounds) {
				continue
			}
			poly, ok := painterIntersection(p.poly, q.poly, work)
			if !ok {
				return 0, false
			}
			if len(poly) > 0 {
				return 2, true
			}
		}
	}
	return 0, true
}

// Read only structural identities generated by the renderer. Original markup
// bytes and nested transforms remain intact, including gradients and content.
func painterReadNodes(svg string, rc *routeCollector, scene connectorSpatialScene, parts int) ([]painterNode, string) {
	if len(scene.boxes) != parts || len(rc.paint) != parts {
		return nil, "incomplete body paint identities"
	}
	owners := map[string]bool{}
	for _, b := range scene.boxes {
		owners[b.id] = true
	}
	for _, p := range scene.surfaces {
		if !owners[p.owner] {
			return nil, "incomplete body paint identities"
		}
	}
	routes := map[int]*ConnectorDrawing{}
	for i := range rc.routes {
		routes[rc.routes[i].Index] = rc.routes[i].Drawing
	}
	byArrow := map[string]int{}
	for _, r := range rc.routes {
		if len(r.Drawing.Arrow) > 0 {
			byArrow[fmt.Sprintf("url(#conn-arrow-visible-%d)", r.Index)] = r.Index
		}
	}
	type frame struct {
		start    int
		layer    string
		selected *painterNode
	}
	var stack []frame
	var nodes []painterNode
	selected := false
	dec := xml.NewDecoder(strings.NewReader(svg))
	seen := map[string]bool{}
	for {
		start := int(dec.InputOffset())
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "malformed painter SVG"
		}
		switch e := token.(type) {
		case xml.StartElement:
			if selected && (e.Name.Local == "text" || e.Name.Local == "image" || e.Name.Local == "foreignObject") {
				for i := range stack {
					if n := stack[i].selected; n != nil && n.kind == "part" {
						n.orderLocked = true
						break
					}
				}
			}
			a := map[string]string{}
			for _, v := range e.Attr {
				a[v.Name.Local] = v.Value
			}
			f := frame{start: start, layer: a["data-layer"]}
			if !selected {
				kind, index := "", 0
				if v, ok := a["data-part"]; e.Name.Local == "g" && ok {
					if _, exists := a["data-paint-order"]; exists {
						return nil, "painter order attribute collision"
					}
					n, err := strconv.Atoi(v)
					if err != nil || n < 0 || n >= parts || len(stack) != 1 {
						return nil, "invalid body paint identity"
					}
					if a["data-part-id"] != "" && a["data-part-id"] != scene.boxes[n].id {
						return nil, "body identity differs from geometry"
					}
					kind, index = "part", n
				} else if v, ok := a["data-connector"]; e.Name.Local == "path" && ok {
					n, err := strconv.Atoi(v)
					if err != nil || routes[n] == nil || len(stack) == 0 || stack[len(stack)-1].layer != "connectors" {
						return nil, "invalid connector paint identity"
					}
					kind, index = "route", n
				} else if n, ok := byArrow[a["clip-path"]]; e.Name.Local == "polygon" && ok {
					kind, index = "arrow", n
				}
				if kind != "" {
					key := fmt.Sprintf("%s:%d", kind, index)
					if seen[key] {
						return nil, "duplicate painter identity"
					}
					seen[key] = true
					f.selected = &painterNode{kind: kind, index: index, start: start}
					selected = true
				}
			}
			stack = append(stack, f)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, "unbalanced painter SVG"
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.selected != nil {
				n := *f.selected
				n.end = int(dec.InputOffset())
				n.markup = svg[n.start:n.end]
				if n.kind == "route" {
					n.markup = strings.Replace(n.markup, fmt.Sprintf(` clip-path="url(#conn-visible-%d)"`, n.index), "", 1)
				}
				if n.kind == "arrow" {
					n.markup = strings.Replace(n.markup, fmt.Sprintf(` clip-path="url(#conn-arrow-visible-%d)"`, n.index), "", 1)
				}
				nodes = append(nodes, n)
				selected = false
			}
		}
		if len(nodes) > spatialPainterNodes {
			return nil, "painter node budget"
		}
	}
	if len(nodes) != parts+len(routes)+len(byArrow) {
		return nil, "incomplete painter nodes"
	}
	return nodes, ""
}

func spatialPainterCompose(svg string, rc *routeCollector, scene connectorSpatialScene, parts int, visibility map[int]ConnectorSpatialVisibility) painterResult {
	nodes, why := painterReadNodes(svg, rc, scene, parts)
	if why != "" {
		return painterResult{reason: why, fatal: why != "incomplete body paint identities" && why != "painter node budget"}
	}
	work := spatialPainterWork
	byRoute := map[int]*ConnectorDrawing{}
	for i := range rc.routes {
		byRoute[rc.routes[i].Index] = rc.routes[i].Drawing
	}
	byOwner := make(map[string][]spatialPiece)
	for _, p := range scene.surfaces {
		byOwner[p.owner] = append(byOwner[p.owner], p)
	}
	for i := range nodes {
		n := &nodes[i]
		var pieces []spatialPiece
		switch n.kind {
		case "part":
			pieces = byOwner[scene.boxes[n.index].id]
			if len(pieces) == 0 {
				return painterResult{reason: "missing body depth pieces"}
			}
		case "route":
			d := byRoute[n.index]
			lines, why := spatialLines(&ConnectorRoute{Drawing: d})
			if why != "" {
				return painterResult{reason: why}
			}
			for _, line := range lines {
				p, ok := spatialRibbon(line.a, line.b, line.da, line.db, d.StrokeWidth/2+spatialPainterGuard)
				if !ok {
					return painterResult{reason: "painter stroke budget"}
				}
				pieces = append(pieces, p...)
			}
		case "arrow":
			d := byRoute[n.index]
			a := visibility[n.index].Arrow
			if a == nil {
				return painterResult{reason: "missing arrow depth plane"}
			}
			var poly [][2]float64
			for _, p := range d.Arrow {
				poly = append(poly, [2]float64{p[0] - d.ProjectionOrigin[0], p[1] - d.ProjectionOrigin[1]})
			}
			poly, ok := painterExpand(spatialCCW(poly), spatialPainterGuard, &work)
			if !ok {
				return painterResult{reason: "painter arrow budget"}
			}
			pieces = []spatialPiece{{poly: poly, depth: a.DepthPlane}}
		}
		for _, p := range pieces {
			n.pieces = append(n.pieces, painterBounded(p))
		}
	}
	adj := make([]map[int]bool, len(nodes))
	degree := make([]int, len(nodes))
	add := func(a, b int) {
		if adj[a] == nil {
			adj[a] = map[int]bool{}
		}
		if !adj[a][b] {
			adj[a][b] = true
			degree[b]++
		}
	}
	clipOwners := make([]map[string]bool, len(nodes))
	for i := range nodes {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[i].kind == "part" && nodes[j].kind == "part" {
				rel, ok := painterBodyRelation(nodes[i], nodes[j], &work)
				if !ok {
					return painterResult{reason: "painter body comparison budget"}
				}
				if rel == 3 {
					return painterResult{reason: "mixed body depth"}
				}
				if rel == 1 {
					add(j, i)
				} else if rel == 2 {
					add(i, j)
				}
				continue
			}
			rel, ok := painterRelation(nodes[i], nodes[j], &work)
			if !ok {
				return painterResult{reason: "painter comparison budget"}
			}
			if rel == 3 {
				body, subject := i, j
				if nodes[j].kind == "part" {
					body, subject = j, i
				}
				if clipOwners[subject] == nil {
					clipOwners[subject] = map[string]bool{}
				}
				clipOwners[subject][scene.boxes[nodes[body].index].id] = true
				add(body, subject)
				continue
			}
			if rel == 1 {
				add(j, i)
			} else if rel == 2 {
				add(i, j)
			}
		}
	}
	var ready, order []int
	for i, d := range degree {
		if d == 0 {
			ready = append(ready, i)
		}
	}
	for len(ready) > 0 {
		i := ready[0]
		ready = ready[1:]
		order = append(order, i)
		for j := range adj[i] {
			degree[j]--
			if degree[j] == 0 {
				ready = append(ready, j)
			}
		}
		sort.Ints(ready) // stable against hash-map iteration and repeat renders
	}
	if len(order) != len(nodes) {
		return painterResult{reason: "painter dependency cycle"}
	}
	// A mixed relation clips only that body's contribution. Uniformly nearer
	// bodies paint over the original primitive once, without a complementary
	// clip multiplying antialias coverage a second time.
	var clipDefs strings.Builder
	lineClips, arrowClips := map[int][]string{}, map[int][]string{}
	for i, owners := range clipOwners {
		if len(owners) == 0 {
			continue
		}
		n := &nodes[i]
		var surfaces []spatialPiece
		var ids []string
		// Preserve deterministic scene order, including surface iteration.
		for _, b := range scene.boxes {
			if owners[b.id] {
				ids = append(ids, b.id)
				surfaces = append(surfaces, byOwner[b.id]...)
			}
		}
		d := byRoute[n.index]
		lines, why := spatialLines(&ConnectorRoute{Drawing: d})
		if why != "" {
			return painterResult{reason: why}
		}
		var region screenRegion
		if n.kind == "route" {
			var ok bool
			region, ok = spatialStrokeClip(lines, surfaces, d.StrokeWidth)
			if !ok {
				return painterResult{reason: "painter local clip budget"}
			}
			lineClips[n.index] = ids
		} else {
			a, why := spatialArrowVisibility(d, lines, surfaces)
			if why != "" || a == nil {
				return painterResult{reason: "painter local arrow clip budget"}
			}
			region = a.ClipRegion
			arrowClips[n.index] = ids
		}
		id := fmt.Sprintf("conn-painter-%s-%d", n.kind, n.index)
		if strings.Contains(svg, `id="`+id+`"`) {
			return painterResult{reason: "painter clip ID collision", fatal: true}
		}
		clipDefs.WriteString(spatialClipMarkup(id, region, d.ProjectionOrigin))
		at := strings.IndexByte(n.markup, '>')
		if at < 0 {
			return painterResult{reason: "missing painter primitive boundary", fatal: true}
		}
		if n.markup[at-1] == '/' {
			at--
		}
		n.markup = n.markup[:at] + fmt.Sprintf(` clip-path="url(#%s)"`, id) + n.markup[at:]
	}
	out := svg
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		out = out[:n.start] + out[n.end:]
	}
	at := strings.Index(out, `<g data-layer="connector-overlay">`)
	if at < 0 {
		at = strings.LastIndex(out, "</svg>")
	}
	if at < 0 {
		return painterResult{reason: "missing painter insertion boundary", fatal: true}
	}
	result := painterResult{layers: map[int]int{}, arrowLayers: map[int]int{}, lineClips: lineClips, arrowClips: arrowClips}
	var markup strings.Builder
	if clipDefs.Len() > 0 {
		markup.WriteString("<defs>" + clipDefs.String() + "</defs>")
	}
	painted := 0
	for _, i := range order {
		n := nodes[i]
		switch n.kind {
		case "part":
			markup.WriteString(strings.Replace(n.markup, "<g ", fmt.Sprintf(`<g data-paint-order="%d" `, painted), 1))
			result.partOrder = append(result.partOrder, n.index)
			painted++
		case "route":
			markup.WriteString(`<g data-layer="connectors">` + n.markup + `</g>`)
			result.layers[n.index] = painted
		case "arrow":
			markup.WriteString(`<g data-layer="connector-overlay">` + n.markup + `</g>`)
			result.arrowLayers[n.index] = painted
		}
	}
	result.svg = out[:at] + markup.String() + out[at:]
	return result
}
