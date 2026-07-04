package iso25d

import (
	"strings"
	"testing"
)

// Regression: namespaceSVGIDs used to blanket-ReplaceAll `id="`, `url(#`,
// and `href="#` over the WHOLE part markup, so a label whose visible text
// happened to contain one of those literals (quotes and parens survive
// escapeXML untouched) got the part prefix spliced into what the user
// reads. The rewrite must apply only in tag/attribute context.
func TestNamespaceSVGIDsSkipsTextNodes(t *testing.T) {
	inner := `<defs><linearGradient id="g1"/></defs>` +
		`<rect fill="url(#g1)"/>` +
		`<use href="#g1"/>` +
		`<text font-size="12">label id="g1" plus url(#g1) plus href="#g1"</text>`
	got := namespaceSVGIDs(inner, "p0-")

	// Attribute context: definitions and references are prefixed.
	for _, want := range []string{
		`<linearGradient id="p0-g1"/>`,
		`<rect fill="url(#p0-g1)"/>`,
		`<use href="#p0-g1"/>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("attribute rewrite missing %q in:\n%s", want, got)
		}
	}
	// Text-node context: the label text must survive byte-for-byte.
	wantText := `>label id="g1" plus url(#g1) plus href="#g1"<`
	if !strings.Contains(got, wantText) {
		t.Errorf("text node was corrupted; want %q preserved in:\n%s", wantText, got)
	}
	if strings.Contains(got, `label id="p0-g1"`) ||
		strings.Contains(got, `plus url(#p0-g1)`) ||
		strings.Contains(got, `plus href="#p0-g1"`) {
		t.Errorf("prefix leaked into text node:\n%s", got)
	}
}

// Comments and CDATA are opaque character data — the prefix must not be
// spliced into them either.
func TestNamespaceSVGIDsSkipsCommentsAndCDATA(t *testing.T) {
	inner := `<!-- keep id="raw" url(#raw) --><g id="raw"/>` +
		`<style><![CDATA[.x{fill:url(#raw)}]]></style>`
	got := namespaceSVGIDs(inner, "p3-")
	if !strings.Contains(got, `<!-- keep id="raw" url(#raw) -->`) {
		t.Errorf("comment content was rewritten:\n%s", got)
	}
	if !strings.Contains(got, `<![CDATA[.x{fill:url(#raw)}]]>`) {
		t.Errorf("CDATA content was rewritten:\n%s", got)
	}
	if !strings.Contains(got, `<g id="p3-raw"/>`) {
		t.Errorf("element id not prefixed:\n%s", got)
	}
}

// A '>' inside a quoted attribute value must not terminate the tag early
// (which would demote the rest of the tag to "text" and skip its rewrite).
func TestNamespaceSVGIDsQuotedGT(t *testing.T) {
	inner := `<text aria-label="a &lt; b > c" fill="url(#g2)">body url(#g2)</text>`
	got := namespaceSVGIDs(inner, "p1-")
	if !strings.Contains(got, `fill="url(#p1-g2)"`) {
		t.Errorf("attribute after quoted '>' not rewritten:\n%s", got)
	}
	if !strings.Contains(got, `>body url(#g2)<`) {
		t.Errorf("text node corrupted:\n%s", got)
	}
}

// Degenerate input: an unterminated tag must be copied through verbatim,
// never half-rewritten or dropped.
func TestNamespaceSVGIDsUnterminatedTag(t *testing.T) {
	inner := `<g id="a"/><rect id="b`
	got := namespaceSVGIDs(inner, "p0-")
	if !strings.Contains(got, `<g id="p0-a"/>`) {
		t.Errorf("well-formed prefix half missing:\n%s", got)
	}
	if !strings.HasSuffix(got, `<rect id="b`) {
		t.Errorf("unterminated tail not preserved verbatim:\n%s", got)
	}
}

// End-to-end: a composite part whose LABEL contains id="..." / url(#...)
// literals renders with the label text intact while the part's own defs
// are still namespaced.
func TestRenderCompositeLabelWithIDLiteral(t *testing.T) {
	out := RenderComposite([]CompositePart{{
		ID:    "n1",
		Shape: "rectangle",
		// LabelLines pins the wrap so each literal is one text node.
		Opts: ConvertOpts{Width: 120, Depth: 80, Height: 30,
			Label: `id="x" url(#y)`, LabelLines: []string{`id="x"`, `url(#y)`}},
	}})
	for _, want := range []string{`>id="x"<`, `>url(#y)<`} {
		if !strings.Contains(out, want) {
			t.Fatalf("label text was corrupted by id namespacing; want %q as a text node in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `id="p0-x"`) || strings.Contains(out, `url(#p0-y)`) {
		t.Fatalf("part prefix spliced into label text:\n%s", out)
	}
}
