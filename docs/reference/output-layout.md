---
title: "Output files"
description: "Understand topology files, per-node assets, and export formats."
---

`isotopo render <input> <out>` produces a two-tier output tree:

1. **Topology level** — the whole scene as one SVG + an editable HTML page
2. **Node level** — each individual element as a standalone SVG + an
   embed-ready HTML page + a self-contained YAML fragment

## Directory layout

```
<out>/
├── topology.svg              ← the full scene SVG (drop into anywhere)
├── topology.html             ← embed snippet + editable DSL source
├── topology.{yaml,d2,json}   ← source copy (rename + edit + re-render)
└── nodes/
    ├── _index.html           ← gallery of every per-element SVG
    ├── <id>.svg              ← standalone SVG of one element
    ├── <id>.html             ← embed snippet + per-element YAML fragment
    └── <id>.yaml             ← per-element DSL fragment (re-renderable)
```

For a scene with N atomic elements, you get `1 + (3 × N) + 2` files.

## Topology-level files

### `topology.svg`

The whole scene as a single self-contained SVG. Drop it into any
markdown / Notion / Confluence / Figma — they all accept SVG.

The SVG carries:
- viewBox sized to fit the scene + a margin
- defs block with any iso patterns (ground grid, etc.)
- one `<g data-layer="canvas-bg">` for the background
- one `<g data-layer="connectors">` for arrows
- one `<g data-part="N">` per iso element
- one `<g data-layer="screen-labels">` for screen-oriented labels
- one `<g data-layer="annotations">` for callouts

You can post-process by selecting those layers in CSS or another tool.

### `topology.html`

A self-contained HTML page that:
- shows `topology.svg` in a stage
- shows the embed snippet (`<img src="./topology.svg"/>`) with a copy button
- shows the original DSL source in an editable `<textarea>` with a
  copy button and a "download" button (so you can edit, save, re-run
  `isotopo render`)

Open it in a browser to inspect or hand off to a designer:

```bash
open out/topology.html        # macOS
xdg-open out/topology.html    # Linux
start out/topology.html       # Windows
```

### `topology.<yaml|d2|json>`

A byte-exact copy of the input file, renamed to `topology` for
discoverability. Lets you re-render later without keeping the
original around.

## Node-level files

For every CompositePart with an `id`, three files are emitted under
`nodes/`. Useful when you want an icon library: each iso element
becomes its own embeddable sticker.

### `nodes/<id>.svg`

The single element as a standalone iso SVG — no scene context, no
neighbors. Perfect for inline embedding in docs:

```markdown
The ![queue](./out/nodes/queue.svg) holds pending jobs.
```

### `nodes/<id>.html`

Same shape as `topology.html` but for one element:
- shows just that element's SVG
- embed snippet `<img src="./<id>.svg"/>`
- the per-element YAML fragment in an editable textarea

### `nodes/<id>.yaml`

A self-contained iso-topology Document with just that one element
plus the original theme. Re-renderable:

```bash
isotopo render out/nodes/queue.yaml /tmp/queue-only
# /tmp/queue-only/topology.svg is just the queue
```

This is how you split a scene into a sticker sheet.

### `nodes/_index.html`

A gallery linking to every per-element page. Open this to browse all
the elements in the scene at once.

## How to embed in different surfaces

### Markdown (GitHub, Notion, Obsidian)

```markdown
![architecture](./out/topology.svg)
```

### HTML

```html
<img src="out/topology.svg" alt="architecture"/>
```

Or inline the entire SVG (e.g. for styling individual layers via CSS):

```html
<!-- copy contents of out/topology.svg straight into the HTML -->
```

### Slides

Most slide tools (Keynote, PowerPoint, Google Slides, Figma) accept
SVG drag-and-drop. Drag `topology.svg` from a Finder/Explorer window.

### Docs sites (MkDocs, Docusaurus, etc.)

Put `topology.svg` next to your `.md` and reference it relatively.
SVG renders at native resolution at any zoom.

### LLM context

Some LLMs accept SVG directly as text:

```bash
cat out/topology.svg | <pipe-to-llm>
```

For multimodal LLMs that want a raster, convert via headless Chrome
or `rsvg-convert`:

```bash
rsvg-convert -w 1600 out/topology.svg -o topology.png
```

## Inspecting outputs from the CLI

```bash
# Quick "did the render produce the expected element ids?"
ls out/nodes/*.svg | sed 's|.*/||;s|\.svg$||'

# Run the entire output through an automated diff with a previous run
diff -r prev-out/ out/

# Sanity-check the SVG renders in a browser
open out/topology.html
```

## When the output looks wrong

A few common gotchas:

| Symptom | Likely cause | Fix |
|---|---|---|
| no backdrop / no grid | input has no `canvas.background` and no `canvas.grid` | add `canvas: {background: "#FAFBFC", grid: iso}` to the YAML |
| element missing | the element's `id` is empty | give every CompositePart an `id` |
| connector drawn to the wrong corner | default anchor is centroid | use `from: api.right-mid` to pin to a specific face-center |
| group looks the same as a rectangle | `shape: group` but `parts` empty | nest at least one part inside |
| stack drew only one layer | `stack.count <= 1` (no-op) | set `stack: {count: 3, gap: 10}` |
| annotation invisible | `anchor` doesn't match any part id | run `isotopo validate` — it suggests the closest id |

## Output size

For a moderate scene (20–30 elements) typical sizes are:

| File | Bytes |
|---|---|
| topology.svg | 50–250 KB |
| topology.html | ~5 KB on top of the SVG |
| nodes/*.svg | 1–20 KB each |

The HTML pages have no external dependencies — they work offline.
