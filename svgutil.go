// SVG string-manipulation utilities used by the injectors. These
// don't care about iso semantics — they're tiny parse/edit helpers
// that work on the SVG envelope as text.
//
// Structure: every public-facing helper (growViewBox, growViewBoxAround,
// padViewBox, enforceAspectRatio, ceilOuterDims, resizeCanvasBg) keeps its
// historical never-fails signature, but is now a thin wrapper over a
// *Checked variant that returns an explicit error instead of silently
// mis-cutting the document. The wrappers return the input UNCHANGED on any
// parse failure — the one safe fallback — while tests can call the Checked
// variants to assert the exact failure mode. Attribute access goes through
// a real (single-tag) attribute scanner, so the helpers are robust to
// attribute reordering, quoted '>' / '=' characters, and decoy attribute
// names that merely end with the wanted name (e.g. data-viewBox).
package isotopo

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Sentinel errors for the Checked helpers. All of them mean "the input
// was left untouched"; they exist so callers/tests can tell WHY.
var (
	errNoSVGTag       = errors.New("svgutil: no root <svg> open tag found")
	errUnclosedSVGTag = errors.New("svgutil: root <svg> open tag is unterminated")
	errNoViewBox      = errors.New("svgutil: root <svg> has no viewBox attribute")
	errBadViewBox     = errors.New("svgutil: malformed viewBox attribute")
	errBadDims        = errors.New("svgutil: viewBox is non-finite or overflows integer snapping")
	errBadCanvasBg    = errors.New("svgutil: canvas-bg rect is not a self-closing tag")
)

// ---------------------------------------------------------------------------
// Parsing core
// ---------------------------------------------------------------------------

// rootSVG is the located root <svg ...> open tag of a document.
type rootSVG struct {
	doc   string
	start int // index of '<'
	end   int // index just past '>'
}

func (r rootSVG) tag() string  { return r.doc[r.start:r.end] }
func (r rootSVG) body() string { return r.doc[r.end:] }

// rebuild re-assembles the document around a modified tag/body.
func (r rootSVG) rebuild(newTag, newBody string) string {
	return r.doc[:r.start] + newTag + newBody
}

// findRootSVG locates the root <svg ...> open tag. Unlike a bare
// strings.Index(svg, ">"), the closing '>' search honours quoted attribute
// values, so a '>' inside e.g. a font-family or aria-label attribute can't
// truncate the header.
func findRootSVG(svg string) (rootSVG, error) {
	from := 0
	for {
		i := strings.Index(svg[from:], "<svg")
		if i < 0 {
			return rootSVG{}, errNoSVGTag
		}
		i += from
		// Require a real tag boundary after "<svg" so "<svgfoo" (or an
		// element like <svgdef>) is not mistaken for the root element.
		after := i + len("<svg")
		if after >= len(svg) {
			return rootSVG{}, errUnclosedSVGTag
		}
		if c := svg[after]; c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '>' || c == '/' {
			end := tagCloseIndex(svg, i)
			if end < 0 {
				return rootSVG{}, errUnclosedSVGTag
			}
			return rootSVG{doc: svg, start: i, end: end + 1}, nil
		}
		from = after
	}
}

// tagCloseIndex returns the index of the '>' that closes the tag opened at
// s[lt] == '<', skipping any '>' inside quoted attribute values.
// Returns -1 when the tag never closes.
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

func isXMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// attrValueSpan scans ONE element open tag (`<name a="v" b="w" ...>` or the
// self-closing form) and returns the byte range [valStart, valEnd) of the
// value of the attribute whose FULL name is name. Order-independent and
// immune to prefix/suffix collisions (searching "width" will not hit
// "stroke-width", searching "viewBox" will not hit "data-viewBox").
// ok=false when the attribute is absent or the tag is malformed.
func attrValueSpan(tag, name string) (valStart, valEnd int, ok bool) {
	i := 0
	// Skip '<' and the element name.
	if i < len(tag) && tag[i] == '<' {
		i++
	}
	for i < len(tag) && !isXMLSpace(tag[i]) && tag[i] != '>' {
		i++
	}
	for i < len(tag) {
		for i < len(tag) && isXMLSpace(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] == '>' || tag[i] == '/' {
			return 0, 0, false // end of attribute list
		}
		// Attribute name.
		ns := i
		for i < len(tag) && tag[i] != '=' && !isXMLSpace(tag[i]) && tag[i] != '>' {
			i++
		}
		aname := tag[ns:i]
		for i < len(tag) && isXMLSpace(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] != '=' {
			continue // valueless attribute — keep scanning
		}
		i++
		for i < len(tag) && isXMLSpace(tag[i]) {
			i++
		}
		if i >= len(tag) {
			return 0, 0, false
		}
		q := tag[i]
		if q != '"' && q != '\'' {
			return 0, 0, false // unquoted value — refuse to guess
		}
		i++
		vs := i
		for i < len(tag) && tag[i] != q {
			i++
		}
		if i >= len(tag) {
			return 0, 0, false // unterminated quote
		}
		if aname == name {
			return vs, i, true
		}
		i++ // past the closing quote
	}
	return 0, 0, false
}

// extractAttr returns the value of the named attribute inside one element
// open tag, or "" when absent/malformed.
func extractAttr(tag, name string) string {
	vs, ve, ok := attrValueSpan(tag, name)
	if !ok {
		return ""
	}
	return tag[vs:ve]
}

// replaceAttr rewrites the named attribute's value in place, leaving every
// other byte of the tag untouched. No-op when the attribute is absent —
// this mirrors the historical behaviour the callers rely on.
func replaceAttr(tag, name, val string) string {
	vs, ve, ok := attrValueSpan(tag, name)
	if !ok {
		return tag
	}
	return tag[:vs] + val + tag[ve:]
}

// parseViewBox parses "minX minY width height". Rejects anything that is
// not exactly four finite numbers — better to refuse than to operate on a
// half-parsed frame.
func parseViewBox(vb string) (x, y, w, h float64, err error) {
	fields := strings.Fields(vb)
	if len(fields) != 4 {
		return 0, 0, 0, 0, errBadViewBox
	}
	var vals [4]float64
	for i, f := range fields {
		v, perr := strconv.ParseFloat(f, 64)
		if perr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, 0, 0, 0, errBadViewBox
		}
		vals[i] = v
	}
	return vals[0], vals[1], vals[2], vals[3], nil
}

// ---------------------------------------------------------------------------
// ViewBox editing helpers
// ---------------------------------------------------------------------------

// growViewBox expands the root viewBox rightward/downward so that
// (needW, needH) fits. Never fails: on any parse error the document is
// returned unchanged (see growViewBoxChecked for the error detail).
func growViewBox(svg string, needW, needH float64) string {
	out, err := growViewBoxChecked(svg, needW, needH)
	if err != nil {
		return svg
	}
	return out
}

func growViewBoxChecked(svg string, needW, needH float64) (string, error) {
	root, err := findRootSVG(svg)
	if err != nil {
		return svg, err
	}
	tag := root.tag()
	vb := extractAttr(tag, "viewBox")
	if vb == "" {
		return svg, errNoViewBox
	}
	x, y, w, h, err := parseViewBox(vb)
	if err != nil {
		return svg, err
	}
	grew := false
	if needW > x+w {
		w = needW - x
		grew = true
	}
	if needH > y+h {
		h = needH - y
		grew = true
	}
	if !grew {
		return svg, nil
	}
	newVB := fmt.Sprintf("%.2f %.2f %.2f %.2f", x, y, w, h)
	tag = replaceAttr(tag, "viewBox", newVB)
	tag = replaceAttr(tag, "width", fmt.Sprintf("%.2f", w))
	tag = replaceAttr(tag, "height", fmt.Sprintf("%.2f", h))
	return root.rebuild(tag, root.body()), nil
}

// stripXMLCtrl drops characters XML 1.0 forbids (control chars except tab, LF,
// CR), which would otherwise make labels/attributes non-well-formed.
func stripXMLCtrl(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, s)
}

func escapeXML(s string) string {
	s = stripXMLCtrl(s)
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// injectCompositeConnectors computes the iso-projected screen coords of
// each part's named anchor, then splices <line> elements for each
// connector before the closing </svg>. Mirrors the (tx, ty) shift that

type minSvgRect struct {
	minX, minY, maxX, maxY float64
}

// growViewBoxAround expands the SVG's viewBox in all four directions to
// contain the requested rect (in SVG user-space coords). It also
// updates any data-layer="canvas-bg" rect so the backdrop continues to
// fill the full viewBox after expansion. Idempotent — no-op if the
// existing viewBox already contains the rect. Never fails: any parse
// error returns the document unchanged (growViewBoxAroundChecked has the
// error detail).
func growViewBoxAround(svg string, need minSvgRect) string {
	out, err := growViewBoxAroundChecked(svg, need)
	if err != nil {
		return svg
	}
	return out
}

func growViewBoxAroundChecked(svg string, need minSvgRect) (string, error) {
	root, err := findRootSVG(svg)
	if err != nil {
		return svg, err
	}
	tag := root.tag()
	vb := extractAttr(tag, "viewBox")
	if vb == "" {
		return svg, errNoViewBox
	}
	x, y, w, h, err := parseViewBox(vb)
	if err != nil {
		return svg, err
	}
	x2, y2 := x+w, y+h
	if need.minX < x {
		x = need.minX
	}
	if need.minY < y {
		y = need.minY
	}
	if need.maxX > x2 {
		x2 = need.maxX
	}
	if need.maxY > y2 {
		y2 = need.maxY
	}
	w, h = x2-x, y2-y
	newVB := fmt.Sprintf("%.2f %.2f %.2f %.2f", x, y, w, h)
	tag = replaceAttr(tag, "viewBox", newVB)
	tag = replaceAttr(tag, "width", fmt.Sprintf("%.2f", w))
	tag = replaceAttr(tag, "height", fmt.Sprintf("%.2f", h))
	// Resize the canvas-bg rect to track the new viewBox.
	body, err := resizeCanvasBgChecked(root.body(), x, y, w, h)
	if err != nil {
		return svg, err
	}
	return root.rebuild(tag, body), nil
}

// resizeCanvasBg finds the data-layer="canvas-bg" rect and rewrites its
// x/y/width/height so it always covers the full viewBox after growth.
// A body without a canvas-bg rect is a normal no-op; a malformed rect
// leaves the body unchanged (resizeCanvasBgChecked reports why).
func resizeCanvasBg(body string, x, y, w, h float64) string {
	out, err := resizeCanvasBgChecked(body, x, y, w, h)
	if err != nil {
		return body
	}
	return out
}

func resizeCanvasBgChecked(body string, x, y, w, h float64) (string, error) {
	marker := `<rect data-layer="canvas-bg"`
	i := strings.Index(body, marker)
	if i < 0 {
		return body, nil // no backdrop rect — nothing to track
	}
	// Locate this rect's own tag close (quote-aware), instead of grabbing
	// the first "/>" anywhere downstream — which could belong to a LATER
	// element if this rect were ever emitted non-self-closing, splicing the
	// document apart mid-tag.
	close := tagCloseIndex(body, i)
	if close < 0 {
		return body, errBadCanvasBg
	}
	if body[close-1] != '/' {
		// Not a self-closing rect — refuse rather than orphan a </rect>.
		return body, errBadCanvasBg
	}
	tag := body[i : close+1]
	rewritten := fmt.Sprintf(
		`<rect data-layer="canvas-bg" x="%.2f" y="%.2f" width="%.2f" height="%.2f"`,
		x, y, w, h,
	)
	// Preserve the original fill attr by appending whatever was after
	// width=... up to "/>". Simplest robust path: extract fill and append.
	if fill := extractAttr(tag, "fill"); fill != "" {
		rewritten += fmt.Sprintf(` fill="%s"`, fill)
	}
	rewritten += `/>`
	return body[:i] + rewritten + body[close+1:], nil
}

// RenderDocument renders every node in a Document and returns a map of
// node-id → SVG string. The document's Theme is applied to every node.
// v2 — the document's top-level "scene" composite picks up Canvas

// ceilOuterDims integer-snaps the root <svg> frame: the viewBox origin floors,
// its far edge ceils, and width/height + the canvas-bg rect follow. The viewBox
// is just the canvas WINDOW — sub-pixel precision buys nothing visible, but it
// puts coordinates on .xx5 rounding boundaries that flip across CPUs (the Go
// compiler may fuse multiply-add differently on arm64 vs amd64), so the SAME
// scene produced byte-different goldens on two machines. Integers are
// deterministic and visually identical. (Also keeps 1:1 raster captures
// scrollbar-free, the original intent.) Never fails: parse errors return the
// document unchanged (ceilOuterDimsChecked has the error detail).
func ceilOuterDims(svg string) string {
	out, err := ceilOuterDimsChecked(svg)
	if err != nil {
		return svg
	}
	return out
}

func ceilOuterDimsChecked(svg string) (string, error) {
	root, err := findRootSVG(svg)
	if err != nil {
		return svg, err
	}
	tag := root.tag()

	vb := extractAttr(tag, "viewBox")
	if vb == "" {
		// No viewBox — just ceil the width/height (legacy fallback).
		out := tag
		for _, attr := range []string{"width", "height"} {
			vs, ve, ok := attrValueSpan(out, attr)
			if !ok {
				continue
			}
			var v float64
			if _, err := fmt.Sscanf(out[vs:ve], "%f", &v); err != nil {
				continue
			}
			out = out[:vs] + fmt.Sprintf("%d", int(math.Ceil(v))) + out[ve:]
		}
		return root.rebuild(out, root.body()), nil
	}

	x, y, w, h, err := parseViewBox(vb)
	if err != nil {
		return svg, err
	}
	// Defence in depth: a non-finite or astronomically large viewBox overflows
	// the int() conversions below into width=0 / int64-max garbage. Validate
	// already errors on such geometry; if it still reaches here, leave the frame
	// as-is rather than emit a corrupt one. (parseViewBox already rejects
	// NaN/Inf; the magnitude guard lives here.)
	for _, v := range []float64{x, y, w, h} {
		if math.Abs(v) > 1e15 {
			return svg, errBadDims
		}
	}
	nx, ny := math.Floor(x), math.Floor(y)
	nw, nh := math.Ceil(x+w)-nx, math.Ceil(y+h)-ny
	out := replaceAttr(tag, "viewBox", fmt.Sprintf("%d %d %d %d", int(nx), int(ny), int(nw), int(nh)))
	out = replaceAttr(out, "width", fmt.Sprintf("%d", int(nw)))
	out = replaceAttr(out, "height", fmt.Sprintf("%d", int(nh)))
	body, err := resizeCanvasBgChecked(root.body(), nx, ny, nw, nh)
	if err != nil {
		return svg, err
	}
	return root.rebuild(out, body), nil
}

// enforceAspectRatio expands the SVG canvas so its width/height ratio is
// at least minRatio (width÷height). When the content is already wider
// than minRatio the SVG is returned unchanged. When it is taller, the
// width is expanded symmetrically (equal whitespace left and right) so
// the content stays centred. The canvas-bg rect is grown to fill the
// new viewBox. minRatio ≤ 0 is a no-op, as is any parse failure.
func enforceAspectRatio(svg string, minRatio float64) string {
	out, err := enforceAspectRatioChecked(svg, minRatio)
	if err != nil {
		return svg
	}
	return out
}

func enforceAspectRatioChecked(svg string, minRatio float64) (string, error) {
	if minRatio <= 0 {
		return svg, nil
	}
	root, err := findRootSVG(svg)
	if err != nil {
		return svg, err
	}
	vb := extractAttr(root.tag(), "viewBox")
	if vb == "" {
		return svg, errNoViewBox
	}
	x, y, w, h, err := parseViewBox(vb)
	if err != nil {
		return svg, err
	}
	if h <= 0 {
		return svg, errBadViewBox
	}
	ratio := w / h
	if ratio >= minRatio {
		return svg, nil // already wide enough
	}
	// Expand width symmetrically around the current content centre.
	newW := h * minRatio
	dx := (newW - w) / 2
	return growViewBoxAroundChecked(svg, minSvgRect{
		minX: x - dx, minY: y,
		maxX: x + w + dx, maxY: y + h,
	})
}

// padViewBox grows the root viewBox (and width/height) uniformly by pad
// on every side. Used for canvas.padding and backglow halo reserve.
// Never fails: parse errors return the document unchanged.
func padViewBox(svg string, pad float64) string {
	out, err := padViewBoxChecked(svg, pad)
	if err != nil {
		return svg
	}
	return out
}

func padViewBoxChecked(svg string, pad float64) (string, error) {
	root, err := findRootSVG(svg)
	if err != nil {
		return svg, err
	}
	vb := extractAttr(root.tag(), "viewBox")
	if vb == "" {
		return svg, errNoViewBox
	}
	x, y, w, h, err := parseViewBox(vb)
	if err != nil {
		return svg, err
	}
	return growViewBoxAroundChecked(svg, minSvgRect{
		minX: x - pad, minY: y - pad,
		maxX: x + w + pad, maxY: y + h + pad,
	})
}
