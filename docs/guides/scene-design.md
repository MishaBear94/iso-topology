---
title: "Design a scene"
description: "Choose layouts, hierarchy, colors, and connector styles for readable architectures."
---

This is the visual style guide behind the
[showcase gallery](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology). The DSL reference tells
you what every field does; this guide tells you which fields to reach
for so the output looks like a launch-post illustration instead of a
clip-art collage. Humans and agents follow the same rules — the
[agent skill](https://github.com/MishaBear94/iso-topology/blob/main/skills/draw-iso-diagram/SKILL.md) is a condensed
version of this page.

## The five rules

**1. One hero.** Every scene has exactly one visually dominant
element: bigger footprint (150–220 vs 90–120), taller or haloed,
gradient flanks. Everything else stays quiet. Two heroes read as
noise; zero heroes read as a parts catalog.

**2. One accent.** Pick a single accent hue per scene and spend it
on: the hero's flanks, the connectors, and icon tints. Neutrals
(white tops, grey or near-black sides) carry everything else.
Reference palettes:

| Scene mood | Canvas | Neutral sides | Accent |
|---|---|---|---|
| light / product | `#F5F6F8` | `#E2E4EA` / `#EEF0F4` | violet `#7C3AED` |
| light / data | `#FFFFFF` | `#D8DAE3` / `#E8EAF1` | indigo `#4F46E5` |
| dark / infra | `#0A0A0B` + iso grid `#161619` | `#0E0E11` / `#141417` | cyan `#22D3EE` |
| dark / green | `#0B0D0C` + iso grid `#151A17` | `#0D0F10` / `#111416` | emerald `#10B981` |

**3. Top faces are canvases.** A glyph plus a 1–3 word caption,
nothing more (`icon: "iso://glyph/gpu"` + `label: "GPU Pool"` — the
renderer centres them as one block, auto-wraps and auto-shrinks so
nothing ever overflows the face). Paragraphs go to screen space:
`style.text.orient: screen` puts the label under the part;
`annotations` add callouts with leader lines.

**4. Effects are a budget, not a default.** `backglow` — once per
scene, on the hero. `dropShadow` — heroes and floating tiles, soft
and colored with the accent at ~20% alpha. `pattern` — one texture
panel or one data surface. `cornerRadius` — everywhere (8–14), it's
what makes tiles read as designed objects.

**5. Never compute coordinates.** Pick the composition pattern, let
the solver do geometry (all gaps in cells):

| Composition | One-liner | Worked example |
|---|---|---|
| hub-and-spoke | `layout: {mode: ring, gap: 2.6}` — first child is the hub | [ai-platform](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/ai-platform/input.yaml) |
| dashboard / board | `group` + `layout: {mode: grid, cols: N}` — substrate auto-sizes | [langchain-app](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/langchain-app/input.yaml) |
| layered flow | anchor part + `place` chains (`rightOf`, `behind`) | [lakehouse-agent](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/lakehouse-agent/input.yaml) |
| dual plane | two `layout: row` groups, the second `place: {inFrontOf: …}` | [rag-pipeline](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/rag-pipeline/input.yaml) |
| metric bars + ghosts | bars in a `place` chain, ghosts `place: {above: <bar>}` | [training-compute](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/training-compute/input.yaml) |
| exploded plate stack (PCB) | plates chained with `above` + a `wz` lift each | [platform-board](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/platform-board/input.yaml) |
| stair | each tile `{rightOf: prev, inFrontOf: prev, gap: 0}` | [RECIPES § stair](../agent/RECIPES.md#i-want-a-staircase-climbing-the-iso-axis) |

## Ship a design system with `theme.presets`

Repeated style blocks are a smell. Name them once in the theme and
reference by name — presets merge between the per-shape defaults and
the part's own `style`, work in JSON as well as YAML (unlike `&`
anchors), and validate with "did you mean" suggestions:

```yaml
theme:
  presets:
    satellite:
      palette: { top: "#FFFFFF", left: "#E2E4EA", right: "#EEF0F4" }
      stroke:  { color: "#D6D9E0", width: 1 }
      effects:
        cornerRadius: 12
        dropShadow: { dx: 0, dy: 7, blur: 10, color: "rgba(63,63,70,0.14)" }

nodes:
  scene:
    shape: composite
    layout: { mode: ring, gap: 2.6 }
    parts:
      - { id: core, ... }                                   # the hero, styled inline
      - { id: llm,  preset: satellite, icon: "iso://glyph/chat",   label: "LLM Gateway" }
      - { id: gpu,  preset: satellite, icon: "iso://glyph/gpu",    label: "GPU Pool" }
      - { id: lake, preset: satellite, icon: "iso://glyph/lake",   label: "Data Lake" }
```

A part can still override any field on top of its preset
(`preset: satellite` + `style: {stroke: {color: "#F00"}}`).
Typical systems need three presets: `hero`, `satellite`/`tile`,
and `ghost` or `pedestal`.

## Connector grammar

- **`routing: orthogonal`, always.** Every segment must ride the iso
  grid (±30° on screen) — a line that cuts across the 2.5D lattice
  instantly breaks the illusion. When two anchors already share an
  iso axis the route collapses to a single on-axis segment, so
  "straight-looking" lines come from ALIGNED LAYOUT, not from
  `routing: straight`.
- Async / replication / background links: same orthogonal routing,
  distinguished by `dash` — never by bezier curves.
- Hairline widths: 1–1.2 for relationship spokes, 1.6–2 for primary
  flow. Solid = synchronous path, dashed = async.
- Arrows (`arrow: triangle`) only where direction carries meaning.
- Pill labels sit at the route's midpoint and paint UNDER parts —
  leave enough gap between connected parts for the label to land in
  open canvas.

## Depth tricks

> These tricks are for **decorative** elements — ghosts, texture
> panels, pedestals, backdrops — that carry **no connectors**.
> Functional nodes that are wired together must share one ground
> plane (tier): give a whole tier a single `wz`, never scatter peer
> nodes at different heights, or their orthogonal connectors will
> sprout vertical drop segments. Elevate by tier, not per node.

- **Texture panel**: a large thin tile with
  `pattern: {kind: dots}` floating `behind` the hero adds depth for
  one part's worth of YAML.
- **Replica stacks**: `stack: {count: 3}` on a cylinder or thin tile
  reads instantly as "replicated".
- **Ghost volumes**: a `place: {above: …}` box with
  `palette: none` + dashed stroke shows capacity/budget headroom.
- **Pedestals**: small flat neutral tiles scattered with `place`
  diagonals fill negative space in marketing shots.
- **Hatch-filled accent tiles**: `pattern: {kind: hatch}` in the
  accent color on a near-white top reads as "circuit / blueprint"
  (see platform-board's chips).
- **Wireframe ghosts**: `effects.wireframe` + dashed stroke for
  floating frames and dashed inset borders — exempt from overlap
  warnings by design.
- **Film grain**: `effects.grain` for the monochrome editorial
  register — one strong texture style per scene, don't mix with
  neon glows.

## Pre-ship checklist

1. `isotopo validate` exits 0 — no overlap warnings you haven't
   consciously accepted.
2. Exactly one hero, one accent. Count your backglows: ≤ 1.
   Adjacent nodes must be tonally separable — never the same fill on
   touching or peer objects (use a tonal ladder in mono scenes).
3. Every top face: glyph + short caption, long text in screen space.
4. Connectors all orthogonal or deliberately bezier — no default
   `straight` cutting across the grid by accident.
5. Render and look once at `out/topology.html`: nothing clipped,
   labels legible at 100% zoom, scene reads left-to-right or
   hub-outward in under three seconds.
