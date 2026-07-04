package isotopo

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Attribute scanner
// ---------------------------------------------------------------------------

func TestExtractAttrOrderIndependent(t *testing.T) {
	// Same attributes, three different orders — the scanner must not care.
	tags := []string{
		`<svg viewBox="0 0 100 50" width="100" height="50">`,
		`<svg width="100" height="50" viewBox="0 0 100 50">`,
		`<svg height="50" viewBox="0 0 100 50" width="100">`,
	}
	for _, tag := range tags {
		if got := extractAttr(tag, "viewBox"); got != "0 0 100 50" {
			t.Errorf("extractAttr(%q, viewBox) = %q", tag, got)
		}
		if got := extractAttr(tag, "width"); got != "100" {
			t.Errorf("extractAttr(%q, width) = %q", tag, got)
		}
	}
}

func TestExtractAttrRejectsDecoyNames(t *testing.T) {
	// A decoy attribute whose NAME merely ends with the wanted name used to
	// win the old strings.Index race when it appeared first in the tag.
	tag := `<svg data-viewBox="9 9 9 9" stroke-width="3" viewBox="0 0 100 50" width="100">`
	if got := extractAttr(tag, "viewBox"); got != "0 0 100 50" {
		t.Errorf("viewBox matched decoy data-viewBox: got %q", got)
	}
	if got := extractAttr(tag, "width"); got != "100" {
		t.Errorf("width matched decoy stroke-width: got %q", got)
	}
	if got := extractAttr(tag, "missing"); got != "" {
		t.Errorf("absent attr should be empty, got %q", got)
	}
}

func TestExtractAttrValueContainingGT(t *testing.T) {
	tag := `<svg aria-label="a > b" viewBox="0 0 10 10">`
	if got := extractAttr(tag, "viewBox"); got != "0 0 10 10" {
		t.Errorf("attr after quoted '>' not found: got %q", got)
	}
}

func TestExtractAttrMalformed(t *testing.T) {
	// Unterminated quote — refuse to guess, return empty for everything at
	// or past the damage.
	if got := extractAttr(`<svg viewBox="0 0 10`, "viewBox"); got != "" {
		t.Errorf("unterminated quote should yield empty, got %q", got)
	}
}

func TestReplaceAttrSplicesValueOnly(t *testing.T) {
	tag := `<svg width="100" viewBox="0 0 100 50" height="50">`
	got := replaceAttr(tag, "viewBox", "0 0 200 50")
	want := `<svg width="100" viewBox="0 0 200 50" height="50">`
	if got != want {
		t.Errorf("replaceAttr = %q, want %q", got, want)
	}
	// Absent attribute → historical no-op contract.
	if got := replaceAttr(tag, "nope", "x"); got != tag {
		t.Errorf("replaceAttr on absent attr must be a no-op, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// findRootSVG / parseViewBox failure modes
// ---------------------------------------------------------------------------

func TestFindRootSVGErrors(t *testing.T) {
	if _, err := findRootSVG(`<div>no svg</div>`); !errors.Is(err, errNoSVGTag) {
		t.Errorf("want errNoSVGTag, got %v", err)
	}
	if _, err := findRootSVG(`<svg viewBox="0 0 1 1`); !errors.Is(err, errUnclosedSVGTag) {
		t.Errorf("want errUnclosedSVGTag, got %v", err)
	}
	// "<svgfoo" is not the root svg element.
	if _, err := findRootSVG(`<svgfoo x="1">`); !errors.Is(err, errNoSVGTag) {
		t.Errorf("want errNoSVGTag for <svgfoo>, got %v", err)
	}
	// XML prolog before the root tag is fine.
	root, err := findRootSVG(`<?xml version="1.0"?><svg viewBox="0 0 5 5"><g/></svg>`)
	if err != nil {
		t.Fatalf("prolog case: %v", err)
	}
	if root.tag() != `<svg viewBox="0 0 5 5">` {
		t.Errorf("root tag = %q", root.tag())
	}
}

func TestParseViewBox(t *testing.T) {
	x, y, w, h, err := parseViewBox("-4 -10.5 802 502.25")
	if err != nil || x != -4 || y != -10.5 || w != 802 || h != 502.25 {
		t.Errorf("parseViewBox: %v %v %v %v err=%v", x, y, w, h, err)
	}
	for _, bad := range []string{"", "0 0 100", "0 0 100 50 60", "0 0 abc 50", "0 0 NaN 50", "0 0 +Inf 50"} {
		if _, _, _, _, err := parseViewBox(bad); err == nil {
			t.Errorf("parseViewBox(%q) should fail", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// growViewBox / growViewBoxAround
// ---------------------------------------------------------------------------

func TestGrowViewBox(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50" width="100" height="50"><g/></svg>`
	got := growViewBox(svg, 160, 40)
	want := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0.00 0.00 160.00 50.00" width="160.00" height="50.00"><g/></svg>`
	if got != want {
		t.Errorf("growViewBox:\n got %q\nwant %q", got, want)
	}
	// Already big enough → byte-identical input back.
	if got := growViewBox(svg, 90, 40); got != svg {
		t.Errorf("no-growth case must return input unchanged, got %q", got)
	}
}

func TestGrowViewBoxCheckedFailuresPreserveInput(t *testing.T) {
	cases := []struct {
		name, in string
		wantErr  error
	}{
		{"no svg tag", `<rect/>`, errNoSVGTag},
		{"unterminated tag", `<svg viewBox="0 0 1 1`, errUnclosedSVGTag},
		{"no viewBox", `<svg width="10" height="10"></svg>`, errNoViewBox},
		{"malformed viewBox", `<svg viewBox="0 0 oops 1"></svg>`, errBadViewBox},
	}
	for _, c := range cases {
		got, err := growViewBoxChecked(c.in, 999, 999)
		if !errors.Is(err, c.wantErr) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.wantErr)
		}
		if got != c.in {
			t.Errorf("%s: input must be preserved verbatim, got %q", c.name, got)
		}
		// The never-fails wrapper must hand back the input too.
		if got := growViewBox(c.in, 999, 999); got != c.in {
			t.Errorf("%s: wrapper must return input unchanged, got %q", c.name, got)
		}
	}
}

func TestGrowViewBoxAround(t *testing.T) {
	svg := `<svg viewBox="0 0 100 50" width="100" height="50">` +
		`<rect data-layer="canvas-bg" x="0" y="0" width="100" height="50" fill="#FFF8EE"/><g/></svg>`
	got := growViewBoxAround(svg, minSvgRect{minX: -10, minY: -5, maxX: 110, maxY: 50})
	want := `<svg viewBox="-10.00 -5.00 120.00 55.00" width="120.00" height="55.00">` +
		`<rect data-layer="canvas-bg" x="-10.00" y="-5.00" width="120.00" height="55.00" fill="#FFF8EE"/><g/></svg>`
	if got != want {
		t.Errorf("growViewBoxAround:\n got %q\nwant %q", got, want)
	}
	// Idempotent: the rect already inside → unchanged.
	if again := growViewBoxAround(got, minSvgRect{minX: -10, minY: -5, maxX: 110, maxY: 50}); again != got {
		t.Errorf("growViewBoxAround not idempotent:\n%q", again)
	}
}

// ---------------------------------------------------------------------------
// resizeCanvasBg
// ---------------------------------------------------------------------------

func TestResizeCanvasBg(t *testing.T) {
	body := `<rect data-layer="canvas-bg" x="0.00" y="0.00" width="10.00" height="10.00" fill="url(#bg)"/><g/>`
	got := resizeCanvasBg(body, -2, -3, 20, 30)
	want := `<rect data-layer="canvas-bg" x="-2.00" y="-3.00" width="20.00" height="30.00" fill="url(#bg)"/><g/>`
	if got != want {
		t.Errorf("resizeCanvasBg:\n got %q\nwant %q", got, want)
	}
	// No marker → normal no-op without error.
	plain := `<g><rect x="1"/></g>`
	if out, err := resizeCanvasBgChecked(plain, 0, 0, 5, 5); err != nil || out != plain {
		t.Errorf("missing marker must be a nil-error no-op, got %q err=%v", out, err)
	}
}

// Regression for the silent mis-cut: a non-self-closing canvas-bg rect used
// to make the old code grab the first "/>" of a LATER element and splice the
// document apart mid-tag. Now it refuses and leaves the body untouched.
func TestResizeCanvasBgRefusesNonSelfClosing(t *testing.T) {
	body := `<rect data-layer="canvas-bg" x="0" y="0" width="10" height="10" fill="#fff"></rect><circle r="4"/>`
	out, err := resizeCanvasBgChecked(body, 0, 0, 99, 99)
	if !errors.Is(err, errBadCanvasBg) {
		t.Fatalf("want errBadCanvasBg, got %v", err)
	}
	if out != body {
		t.Fatalf("body must be preserved verbatim, got %q", out)
	}
	if got := resizeCanvasBg(body, 0, 0, 99, 99); got != body {
		t.Fatalf("wrapper must return body unchanged, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ceilOuterDims
// ---------------------------------------------------------------------------

func TestCeilOuterDims(t *testing.T) {
	svg := `<svg viewBox="-0.50 -0.25 100.30 50.10" width="100.30" height="50.10">` +
		`<rect data-layer="canvas-bg" x="-0.50" y="-0.25" width="100.30" height="50.10" fill="#fff"/></svg>`
	got := ceilOuterDims(svg)
	want := `<svg viewBox="-1 -1 101 51" width="101" height="51">` +
		`<rect data-layer="canvas-bg" x="-1.00" y="-1.00" width="101.00" height="51.00" fill="#fff"/></svg>`
	if got != want {
		t.Errorf("ceilOuterDims:\n got %q\nwant %q", got, want)
	}
}

func TestCeilOuterDimsNoViewBoxFallback(t *testing.T) {
	svg := `<svg width="100.30" height="50.10"><g/></svg>`
	got := ceilOuterDims(svg)
	want := `<svg width="101" height="51"><g/></svg>`
	if got != want {
		t.Errorf("fallback:\n got %q\nwant %q", got, want)
	}
}

func TestCeilOuterDimsGuardsAstronomicalViewBox(t *testing.T) {
	svg := `<svg viewBox="0 0 1e300 10" width="10" height="10"></svg>`
	out, err := ceilOuterDimsChecked(svg)
	if !errors.Is(err, errBadDims) {
		t.Fatalf("want errBadDims, got %v", err)
	}
	if out != svg {
		t.Fatalf("input must be preserved, got %q", out)
	}
	if got := ceilOuterDims(svg); got != svg {
		t.Fatalf("wrapper must return input unchanged, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// padViewBox / enforceAspectRatio
// ---------------------------------------------------------------------------

func TestPadViewBox(t *testing.T) {
	svg := `<svg viewBox="0 0 100 50" width="100" height="50"><g/></svg>`
	got := padViewBox(svg, 10)
	want := `<svg viewBox="-10.00 -10.00 120.00 70.00" width="120.00" height="70.00"><g/></svg>`
	if got != want {
		t.Errorf("padViewBox:\n got %q\nwant %q", got, want)
	}
	// Malformed input passes through unchanged.
	bad := `<svg viewBox="x y w h">`
	if got := padViewBox(bad, 10); got != bad {
		t.Errorf("malformed input must pass through, got %q", got)
	}
}

func TestEnforceAspectRatio(t *testing.T) {
	svg := `<svg viewBox="0 0 50 100" width="50" height="100"><g/></svg>`
	got := enforceAspectRatio(svg, 1.0) // needs 100 wide, centred: minX -25
	want := `<svg viewBox="-25.00 0.00 100.00 100.00" width="100.00" height="100.00"><g/></svg>`
	if got != want {
		t.Errorf("enforceAspectRatio:\n got %q\nwant %q", got, want)
	}
	// Already wide enough / disabled → identical input back.
	wide := `<svg viewBox="0 0 200 100" width="200" height="100"></svg>`
	if got := enforceAspectRatio(wide, 1.5); got != wide {
		t.Errorf("wide-enough case must be unchanged, got %q", got)
	}
	if got := enforceAspectRatio(svg, 0); got != svg {
		t.Errorf("minRatio<=0 must be a no-op, got %q", got)
	}
}
