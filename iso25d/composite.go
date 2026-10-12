package iso25d

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// CompositePart describes one sub-element inside a composite node.
type CompositePart struct {
	ID    string // v2.9 — emitted as data-part-id for SVG↔source tooling
	Shape string
	Opts  ConvertOpts
	OffWX float64
	OffWY float64
	OffWZ float64
}

// RenderComposite stacks N parts into one SVG. Each part is rendered
// independently and placed at its iso-projected offset. Painter order
// follows the slice order — back-most first.
//
// v1.6.4 — coordinate-system unification:
// The composite's (tx, ty) and the routing layer's (tx, ty) are now
// computed from the SAME formula: the union of every part's projected
// 3D world bbox (8 corners projected at z=offWZ and z=offWZ+h). And
// each part's standalone SVG is OFFSET so its internal centering is
// undone, making the part's world (offWX, offWY, offWZ) corner land at
// the standalone's (0, 0). Together these guarantee that any external
// ProjectedBounds is the SINGLE SOURCE OF TRUTH for a composite's projected
// extent. Each box is {offWX, offWY, offWZ, w, d, h}; it projects all 8 world
// corners with the iso transform and returns the screen min/max. RenderComposite
// AND every overlay layer (connectors, screen labels, annotations via
// partsScreenOrigin) MUST derive their (tx,ty) from this one function, so their
// origins can never drift — the cause of the v… edge-detachment bug, where two
// hand-copied bbox loops sourced their dimensions differently.
func ProjectedBounds(boxes [][6]float64) (minX, minY, maxX, maxY float64) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, b := range boxes {
		ox, oy, oz, w, d, h := b[0], b[1], b[2], b[3], b[4], b[5]
		corners := [8][3]float64{
			{ox, oy, oz}, {ox + w, oy, oz}, {ox + w, oy + d, oz}, {ox, oy + d, oz},
			{ox, oy, oz + h}, {ox + w, oy, oz + h}, {ox + w, oy + d, oz + h}, {ox, oy + d, oz + h},
		}
		for _, c := range corners {
			sx := c[0]*cos30 - c[1]*cos30
			sy := c[0]*sin30 + c[1]*sin30 - c[2]
			minX, maxX = math.Min(minX, sx), math.Max(maxX, sx)
			minY, maxY = math.Min(minY, sy), math.Max(maxY, sy)
		}
	}
	return
}

// layer (connectors, screen labels) using world-projected coords
// renders pixel-aligned with the part's visible silhouette.
func RenderComposite(parts []CompositePart) string {
	if len(parts) == 0 {
		return ""
	}

	type rendered struct {
		inner        string
		sx, sy       float64 // screen projection of (OffWX, OffWY, OffWZ)
		intTx, intTy float64 // standalone's internal centering offset
		stdW, stdH   float64 // standalone SVG width / height
	}
	rs := make([]rendered, 0, len(parts))
	fonts := make(fontResources)
	for _, p := range parts {
		full := convert2DTo25D(p.Shape, p.Opts, fonts)
		inner, stdW, stdH := stripOuterSVG(full)
		sx := p.OffWX*cos30 - p.OffWY*cos30
		sy := p.OffWX*sin30 + p.OffWY*sin30 - p.OffWZ
		intTx, intTy := PartOriginOffset(p.Shape, p.Opts)
		rs = append(rs, rendered{
			inner: inner, sx: sx, sy: sy,
			intTx: intTx, intTy: intTy,
			stdW: stdW, stdH: stdH,
		})
	}

	// Composite bbox = union of every part's projected 3D world bbox via the
	// shared ProjectedBounds, so the overlay layers' (tx,ty) can't drift.
	boxes := make([][6]float64, len(parts))
	for i, p := range parts {
		w, d, h := ShapeDimensions(p.Shape, p.Opts)
		boxes[i] = [6]float64{p.OffWX, p.OffWY, p.OffWZ, w, d, h}
	}
	minX, minY, maxX, maxY := ProjectedBounds(boxes)
	pad := 12.0
	tx, ty := -minX+pad, -minY+pad

	// v1.6.4b — expand viewBox if any part's standalone SVG extends
	// past the world-corner bbox (notably iso_text whose text length
	// isn't captured by world geom). tx/ty stay aligned with the world
	// bbox so connectors / screen-labels keep coord parity; we only
	// grow the SVG canvas, not the origin.
	maxOuterX := maxX
	maxOuterY := maxY
	// v1.6.5 — also track overflow at the TOP and LEFT. The previous code
	// only expanded the viewBox rightward/downward (maxOuter*), so shapes
	// whose standalone SVG extends above or left of the 3D-corner bbox
	// (e.g. the person's head, callout tails) were silently clipped.
	// Fix: record how far each part's standalone SVG bleeds into negative
	// composite coordinates; widen the viewBox by shifting its origin
	// (vx, vy) without moving tx/ty — connector coordinates stay intact.
	var bleedLeft, bleedTop float64
	for _, r := range rs {
		// Where the standalone (0, 0) lands in composite coords:
		px := r.sx + tx - r.intTx
		py := r.sy + ty - r.intTy
		// Composite x at which this part's right edge sits.
		if rx := px + r.stdW - tx; rx > maxOuterX {
			maxOuterX = rx
		}
		if ry := py + r.stdH - ty; ry > maxOuterY {
			maxOuterY = ry
		}
		// Left / top overflow: standalone SVG bleeds into negative coords.
		if px < 0 && -px > bleedLeft {
			bleedLeft = -px
		}
		if py < 0 && -py > bleedTop {
			bleedTop = -py
		}
	}
	W := (maxOuterX - minX) + 2*pad + bleedLeft
	H := (maxOuterY - minY) + 2*pad + bleedTop
	vx, vy := -bleedLeft, -bleedTop

	var sb strings.Builder
	// data-scene-tx / data-scene-ty: SVG user coordinates of world (0,0,0).
	// Keep the same cent precision as part transforms; integer rounding caused
	// up to half a pixel of model/hit-region drift. Canonicalise at a micro-unit
	// before rounding to cents so multiply-add noise cannot flip a half-cent
	// boundary between CPUs. Metadata never changes the actual painted origin.
	frameCoord := func(v float64) float64 { return math.Round(math.Round(v*1e6)/1e4) / 100 }
	fmt.Fprintf(&sb,
		`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="%.2f %.2f %.2f %.2f" width="%.2f" height="%.2f" data-scene-tx="%.4f" data-scene-ty="%.4f">`,
		vx, vy, W, H, W, H, frameCoord(tx), frameCoord(ty),
	)
	for i, r := range rs {
		// Outer translate places the part's WORLD (offWX, offWY, offWZ)
		// at composite position (sx + tx, sy + ty). The −intTx / −intTy
		// pre-shift cancels the part's internal centering so the part's
		// local (0, 0, 0) inside the standalone SVG aligns with that
		// translate position — no more per-shape coord mismatch.
		idAttr := ""
		if parts[i].ID != "" {
			idAttr = fmt.Sprintf(` data-part-id="%s"`, escapeAttr(parts[i].ID))
		}
		fmt.Fprintf(&sb, `<g data-part="%d"%s transform="translate(%.2f %.2f)">%s</g>`,
			i, idAttr, r.sx+tx-r.intTx, r.sy+ty-r.intTy, namespaceSVGIDs(r.inner, fmt.Sprintf("p%d-", i)),
		)
	}
	sb.WriteString(`</svg>`)
	return textmetrics.DeduplicateFonts(sb.String())
}

// namespaceSVGIDs prefixes every id definition and reference inside one
// part's standalone markup. Shape renderers use FIXED def ids (e.g.
// "rounded-side"); concatenating N parts verbatim made every part
// resolve url(#...) against the FIRST part's defs — document-wide id
// collision, so e.g. every rounded part inherited part 0's side fill
// regardless of its own palette.
//
// The rewrite is applied ONLY inside element open/close tags (attribute
// context). Text nodes, comments, and CDATA sections are copied verbatim:
// a label whose text happens to contain `id="`, `url(#`, or `href="#`
// (quotes and parens survive escapeXML untouched) must not have the part
// prefix spliced into what the user reads. Within a tag the blanket
// replace stays safe because attribute values cannot contain a raw `"`.
func namespaceSVGIDs(inner, prefix string) string {
	var sb strings.Builder
	sb.Grow(len(inner) + 16*len(prefix))
	i := 0
	for i < len(inner) {
		lt := strings.IndexByte(inner[i:], '<')
		if lt < 0 {
			sb.WriteString(inner[i:]) // trailing text node — verbatim
			break
		}
		lt += i
		sb.WriteString(inner[i:lt]) // text node — verbatim
		// Comments and CDATA are opaque character data: copy verbatim.
		if strings.HasPrefix(inner[lt:], "<!--") {
			end := strings.Index(inner[lt:], "-->")
			if end < 0 {
				sb.WriteString(inner[lt:])
				break
			}
			sb.WriteString(inner[lt : lt+end+len("-->")])
			i = lt + end + len("-->")
			continue
		}
		if strings.HasPrefix(inner[lt:], "<![CDATA[") {
			end := strings.Index(inner[lt:], "]]>")
			if end < 0 {
				sb.WriteString(inner[lt:])
				break
			}
			sb.WriteString(inner[lt : lt+end+len("]]>")])
			i = lt + end + len("]]>")
			continue
		}
		end := tagCloseIndex(inner, lt)
		if end < 0 {
			// Unterminated tag — nothing downstream is safely rewritable.
			sb.WriteString(inner[lt:])
			break
		}
		tag := inner[lt : end+1]
		tag = strings.ReplaceAll(tag, `id="`, `id="`+prefix)
		tag = strings.ReplaceAll(tag, `url(#`, `url(#`+prefix)
		tag = strings.ReplaceAll(tag, `href="#`, `href="#`+prefix)
		sb.WriteString(tag)
		i = end + 1
	}
	return sb.String()
}

// tagCloseIndex returns the index of the '>' closing the tag opened at
// s[lt] == '<', honouring quoted attribute values so a '>' inside an
// attribute cannot end the tag early. Returns -1 when the tag never closes.
func tagCloseIndex(s string, lt int) int {
	var quote byte
	for i := lt + 1; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '>':
			return i
		}
	}
	return -1
}

// PartOriginOffset returns the (tx, ty) within a part's standalone SVG
// where the part's WORLD-origin corner (OffWX, OffWY, OffWZ) — i.e.
// local (0, 0, 0) — renders. Equivalent to inverting the shape
// renderer's internal `-minX+m, -minY+m` centring. Kept in sync with
// the shape renderers in shapes.go / rounded.go.
//
// Composition layers (RenderComposite + downstream injectors) use this
// to subtract the centring so the standalone's (0, 0) coincides with
// project(OffWX, OffWY, OffWZ), making world-projected coords usable
// directly across all shape types.
func PartOriginOffset(shape string, opts ConvertOpts) (tx, ty float64) {
	shape = strings.ToLower(strings.TrimSpace(shape))
	w, d, h := ShapeDimensions(shape, opts)
	m := opts.Margin
	if m <= 0 {
		m = 24 // matches Default* renderers in shapes.go
	}
	switch shape {
	case "iso_text", "title":
		// Pure iso-tilted text. World (0, 0, 0) corresponds to the text's
		// baseline-start (local origin of the iso matrix), which sits at
		// (m, m + sin30·fontSize) inside the standalone. Use the SAME m
		// that RenderIsoText sees post applyBox / DefaultIsoBox (24).
		b := DefaultIsoBox()
		applyBox(opts, &b)
		fontSize := b.FontSize
		return m, m + sin30*fontSize

	case "cylinder", "stored_data", "queue":
		rx := w / 2
		// ry encodes the iso-tilt of the ellipse depth (rx * tan30).
		ry := rx * sin30 / cos30
		// Top ellipse centre is drawn at (rx+m, ry+m) and corresponds to
		// world (w/2, d/2, h). World (0, 0, 0) sits at offset
		// (−w/2, −d/2, −h); project that delta and add to the centre.
		dx := (-w/2 + d/2) * cos30
		dy := -(w/2+d/2)*sin30 + h
		return rx + m + dx, ry + m + dy

	case "person":
		p := DefaultIsoPerson()
		applyPerson(opts, &p)
		return personOrigin(p)
	case "circle":
		// The flat disc's frame may grow around a bound caption. World (0,0,0) and
		// world centre (r,r,r) project to the SAME screen point (iso
		// projection degeneracy along the camera axis), so the offset is
		// the same as the disc centre.
		s := DefaultIsoSphere()
		applySphere(opts, &s)
		cx, cy, _, _ := sphereFrame(s)
		return cx, cy
	case "cloud":
		ox, oy := outlineOrigin(sampleCloudOutline(w, d), h, m)
		b := DefaultIsoBox()
		applyBox(opts, &b)
		b.Width, b.Depth, b.Height = w, d, h
		minX, minY, _, _ := cloudContentBounds(b, -ox+m, -oy+m, 0, 0)
		return -minX + m, -minY + m
	case "wedge", "custom_path", "triprism", "hexprism", "octprism", "array1d", "array2d", "array3d", "rack":
		// These renderers use the box frame even when the outline does not
		// reach its corners; cornerRadius does not select the rounded path.
		return d*cos30 + m, h + m
	}
	if opts.CornerRadius > 0 {
		return outlineOrigin(sampleRoundedRectCW(w, d, roundedRadius(w, d, opts.CornerRadius)), h, m)
	}
	// Default = box-style extrude (rectangle, document, page, callout,
	// class, sql_table, hexagon, polygon, step, diamond, text, code,
	// image, …). Outline = bbox at z = 0 and z = h. minX = (0 − d)·cos30,
	// minY = −h (smallest y at top-back-left corner (0, 0, h)).
	return d*cos30 + m, h + m
}

// stripOuterSVG extracts the inner content + width/height from a full SVG
// string of the form `<svg ... viewBox="0 0 W H" width="W" height="H">…</svg>`.
// Conservative parser tuned to our renderer's output format.
func stripOuterSVG(full string) (inner string, w, h float64) {
	// Find the first '>' after '<svg'.
	startTag := strings.Index(full, "<svg")
	if startTag < 0 {
		return full, 0, 0
	}
	tagEnd := strings.Index(full[startTag:], ">")
	if tagEnd < 0 {
		return full, 0, 0
	}
	header := full[startTag : startTag+tagEnd+1]
	// Parse viewBox dimensions.
	if idx := strings.Index(header, `viewBox="`); idx >= 0 {
		rest := header[idx+len(`viewBox="`):]
		end := strings.Index(rest, `"`)
		if end > 0 {
			parts := strings.Fields(rest[:end])
			if len(parts) == 4 {
				fmt.Sscanf(parts[2], "%f", &w)
				fmt.Sscanf(parts[3], "%f", &h)
			}
		}
	}
	bodyStart := startTag + tagEnd + 1
	closeIdx := strings.LastIndex(full, "</svg>")
	if closeIdx < 0 {
		return full[bodyStart:], w, h
	}
	return full[bodyStart:closeIdx], w, h
}
