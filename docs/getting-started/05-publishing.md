---
title: "Publish a diagram"
description: "Embed SVG in documentation and export images for presentations."
---

You have a `topology.svg` you like. Time to put it in front of
people.

## Markdown (GitHub / Notion / Obsidian)

```markdown
![architecture](./out/topology.svg)
```

GitHub renders SVG inline. Keep the file in the same commit as the
DSL so reviewers can `git diff` the source.

## HTML

```html
<img src="out/topology.svg" alt="architecture"/>
```

Or inline the entire SVG (lets you style layers via CSS — every
group has a `data-layer="…"` attribute):

```html
<style>
  [data-layer="connectors"] path { stroke-width: 2; }
  [data-layer="annotations"] { display: none; } /* hide callouts */
</style>
<!-- paste the contents of out/topology.svg here -->
```

## Slides

Most slide tools accept SVG drag-and-drop:

- Keynote, PowerPoint, Google Slides: drag the `.svg` from a
  Finder/Explorer window onto the slide
- Figma: same — paste from clipboard works too

Native vector means infinite zoom without pixelation.

## Docs sites (MkDocs / Docusaurus / Mintlify)

Drop `topology.svg` next to your `.md` and reference relatively. No
special config; the SVG is self-contained.

## LLM context

For text-mode LLMs:

```bash
# Send the SVG as text
cat out/topology.svg | <pipe-to-llm>

# Or send just the source DSL — model will reason about structure
cat services.d2 | <pipe-to-llm>
```

For multimodal LLMs that want a bitmap, rasterize via headless
Chrome or `rsvg-convert`:

```bash
rsvg-convert -w 1600 out/topology.svg -o topology.png
```

## Per-element embeds

Each element in `nodes/` is a self-contained iso sticker. Drop one
inline:

```markdown
The ![queue](./out/nodes/queue.svg) holds pending jobs.
```

Useful for blog posts where you want a small icon, not the whole
scene.

## Re-rendering after editing

`out/topology.<ext>` is a byte-exact copy of your source DSL. If you
or a reviewer edit it directly:

```bash
isotopo render out/topology.d2 ./out
```

The same output structure is regenerated. You can wire this into a
file watcher for live preview.

## Next

You've covered the full loop — install, write, render, publish.

Common follow-ups:

- [`reference/dsl-yaml.md`](../reference/dsl-yaml.md) — every YAML
  field, every option
- [`reference/dsl-d2.md`](../reference/dsl-d2.md) — every d2 shape
  and how it lowers
- [`reference/dsl-theme.md`](../reference/dsl-theme.md) — palette /
  stroke / text / effects + theme cascade
- [`agent/RECIPES.md`](../agent/RECIPES.md) — task → DSL primitive
  speed-dial (useful for both humans and agents)
- [`guides/troubleshooting.md`](../guides/troubleshooting.md) —
  when something doesn't render the way you expect
