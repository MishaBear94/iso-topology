---
title: "Replicas and annotations"
description: "Add stacked replicas, explanatory callouts, and an isometric grid."
---

Two primitives left to introduce:

- **`stack`** — auto-replicate a part N times vertically. Models
  HA pods, server racks, sharded databases.
- **`annotation`** — screen-space text box pinned to a part via a
  thin leader line. For latency callouts, ownership notes, etc.

Both are YAML-only (the `.d2` path doesn't have an equivalent yet).

## Three replicas of one pod

Take a single pod and stamp it N times:

```yaml
nodes:
  scene:
    shape: composite
    parts:
      - id: pod
        shape: rectangle
        geom: {w: 100, d: 100, h: 36}
        label: "API Pod"
        stack: {count: 3, gap: 10}
```

Render:

```bash
isotopo render scene.yaml ./out
open ./out/topology.html
```

You'll see three pods stacked vertically with a 10-unit gap. The
bottom one keeps the declared id `pod`; the upper two get suffixed
ids `pod~1` and `pod~2`. Connectors and annotations can target a
specific layer:

```yaml
connectors:
  - {from: edge, to: pod}      # bottom layer (default)
  - {from: edge, to: pod~2}    # top layer
```

## A callout pinned to the top pod

Add a document-level `annotations:` block:

```yaml
annotations:
  - anchor: pod~2
    side: top
    text: "3 replicas\nrolling update"
    fontSize: 11
```

The renderer draws a rounded white box above the top pod with a
dashed leader line pointing at its silhouette. Multi-line text via
`\n`.

Sides: `top` | `right` | `bottom` | `left`. Default distance is 60
world units; set `distance: 80` for more breathing room.

## Iso ground grid (already shown in step 03 indirectly)

For the floor plan look, add a `canvas:` block:

```yaml
canvas:
  background: "#FAFBFC"
  grid: iso
  gridColor: "#E2E6EE"
  gridStep: 36
```

`grid: iso` paints a dashed diamond rhombus pattern that aligns with
the iso projection — the scene now floats on a ground plane. Other
options: `dots`, `hatch`, `solid`, `none`.

## Putting it all together

```yaml
canvas: {background: "#FAFBFC", grid: iso, gridColor: "#E2E6EE"}

theme:
  palette: {top: "#FFFFFF", left: "#7E94B5", right: "#A6B7D2"}
  stroke:  {color: "#1F2937", width: 1.2}
  text:    {family: Inter, size: 12, weight: "600", color: "#1F2937"}

nodes:
  scene:
    shape: composite
    parts:
      - id: pod
        shape: rectangle
        geom:   {w: 100, d: 100, h: 36}
        label:  "API Pod"
        style:
          palette: {top: "#3B82F6", left: "#1D4ED8", right: "#2563EB"}
          text:    {color: "#FFFFFF"}
        stack:   {count: 3, gap: 10}

annotations:
  - anchor: pod~2
    side: top
    text: "3 replicas\nrolling update"
    fontSize: 11
```

That's all four DSL primitives at once: **canvas grid**, **theme**,
**stack**, **annotation**. Render it.

## Next step

→ [`05-publishing.md`](./05-publishing.md) — drop the SVG into docs,
slides, or LLM context.
