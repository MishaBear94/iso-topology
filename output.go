package isotopo

import (
	_ "embed"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PartFragments returns one round-trip-able mini-Document per atomic
// part — in the same id-space as RenderParts. Each fragment is itself a
// valid isotopo Document with a single top-level node, so the user can
// drop it in a file and re-render it standalone via `isotopo render`.
// For composite scenes we lift each CompositePart up into its own Node
// (recursing into groups); for non-composite docs each top-level Node
// is its own fragment.
func PartFragments(doc *Document) map[string]*Document {
	out := make(map[string]*Document)
	for id, n := range doc.Nodes {
		if n.Shape != "composite" {
			out[id] = &Document{Theme: doc.Theme, Nodes: map[string]*Node{id: n}}
			continue
		}
		walkAtomicParts(n.Parts, func(p *CompositePart) {
			sub := &Node{
				Shape:   p.Shape,
				Geom:    p.Geom,
				Style:   p.Style,
				Label:   p.Label,
				Icon:    p.Icon,
				Content: p.Content,
			}
			out[p.ID] = &Document{
				Theme: doc.Theme,
				Nodes: map[string]*Node{p.ID: sub},
			}
		})
	}
	return out
}

// MarshalFragmentYAML serializes a per-part fragment Document to YAML
// bytes suitable for writing to disk and re-rendering standalone.
func MarshalFragmentYAML(d *Document) ([]byte, error) {
	return yaml.Marshal(d)
}

// PartIDs returns the sorted id list of every atomic part in the
// document, matching the keys produced by RenderParts / PartFragments.
// v2 — recurses into groups so children show up in the gallery.
func PartIDs(doc *Document) []string {
	seen := map[string]struct{}{}
	for id, n := range doc.Nodes {
		if n.Shape != "composite" {
			seen[id] = struct{}{}
			continue
		}
		walkAtomicParts(n.Parts, func(p *CompositePart) {
			seen[p.ID] = struct{}{}
		})
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// TopologyHTML wraps the topology SVG together with embed-ready snippets
// and the editable source DSL. The HTML is self-contained: a single
// file the user can open, copy embed code from, or hand to a designer.
//
// sourceLang is "yaml", "json", or "d2" — used only to label the source
// block. sourceText is the original DSL bytes; we drop them into a
// <textarea> with copy / download buttons so the user can iterate on
// the topology without losing fidelity to what produced this SVG.
//
//go:embed studio/studio.html
var studioShell string

//go:embed studio/studio.css
var studioCSS string

//go:embed studio/studio.js
var studioJS string

//go:embed studio/selection.js
var studioSelectionJS string

func TopologyHTML(svg, sourceText, sourceLang, sourceFilename string) string {
	// Assemble the Studio page from three embedded assets (studio/*.{html,css,js}):
	// first inline the CSS/JS into the shell, then fill in the per-render data.
	page := strings.NewReplacer("{{CSS}}", studioCSS, "{{JS}}", studioSelectionJS+"\n"+studioJS).Replace(studioShell)
	base := filepath.Base(sourceFilename)
	return strings.NewReplacer(
		"{{LANG}}", html.EscapeString(sourceLang),
		"{{SVG}}", svg,
		"{{SRC}}", html.EscapeString(sourceText),
		"{{LANGQ}}", fmt.Sprintf("%q", sourceLang),
		"{{PATHQ}}", fmt.Sprintf("%q", sourceFilename),
		"{{FILE}}", html.EscapeString(base),
	).Replace(page)
}

// NodeHTML wraps a single per-part SVG with its embed snippet and DSL
// fragment. The fragment is editable in the same way as the topology
// page — drop it into a YAML doc and render it standalone.
//
//go:embed studio/node.html
var nodeShell string

func NodeHTML(id, svg, yamlFragment string) string {
	return strings.NewReplacer(
		"{{BG}}", "#FAFAFB",
		"{{SVG}}", svg,
		"{{FRAG}}", html.EscapeString(yamlFragment),
		"{{ID}}", html.EscapeString(id),
	).Replace(nodeShell)
}

// NodesIndexHTML is a tiny gallery linking to every per-part page so
// users can browse the parts of a topology like a sticker sheet.
//
//go:embed studio/nodes-index.html
var nodesIndexShell string

func NodesIndexHTML(ids []string) string {
	var cards strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&cards,
			`<a class="card" href="./%s.html"><h3>%s</h3><div class="stage"><img src="./%s.svg" alt="%s"/></div></a>`+"\n",
			id, html.EscapeString(id), id, html.EscapeString(id))
	}
	return strings.NewReplacer(
		"{{COUNT}}", fmt.Sprintf("%d", len(ids)),
		"{{CARDS}}", cards.String(),
	).Replace(nodesIndexShell)
}
