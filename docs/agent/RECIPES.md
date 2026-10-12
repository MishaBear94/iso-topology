---
title: "Recipes"
description: "Map common architecture-diagram tasks to supported DSL primitives."
---

Task-indexed quick reference. "I want to express X — which primitive
do I reach for?" Each recipe is copy-paste ready.

Both humans and agents read this. Agents should treat it as the
authoritative mapping from natural-language intent to DSL primitive.

For what makes a scene look DESIGNED (hero/accent rules, effect
budget, composition patterns) see the
[scene design guide](../guides/scene-design.md). For full field
grammar see [`../reference/dsl-yaml.md`](../reference/dsl-yaml.md)
and [`../reference/dsl-d2.md`](../reference/dsl-d2.md). For the
machine-readable inventory call `isotopo capabilities` or read
[`CAPABILITIES.md`](./CAPABILITIES.md). For complete worked scenes see
the [samples index](./SAMPLES.md).

**The one rule that matters:** never hand-compute coordinates.
Position parts with `layout` (containers) and `place` (sibling
relations); all gaps are in cells (1 cell = `gridStep`, default 40
world units). `offset` exists only as a fine-tune delta on top of a
solved position.

## Positioning

### I want B next to A

```yaml
- {id: a, shape: rectangle, geom: {w: 100, d: 100, h: 40}}
- {id: b, shape: cylinder,  place: {rightOf: a, gap: 2}, geom: {w: 90, d: 90, h: 36}}
```

One constraint per ground axis: `rightOf`/`leftOf` pins world x,
`inFrontOf`/`behind` pins world y (front = toward the viewer). The
unconstrained axis aligns to the referenced sibling — `align:
start|center|end` (default center). References must be SIBLINGS in
the same `parts:` list.

### I want a pipeline / chain of parts

Chain `place` along one axis; the solver resolves the chain
topologically:

```yaml
- {id: ingest,    shape: rectangle, geom: {w: 100, d: 80, h: 30}}
- {id: transform, shape: rectangle, place: {rightOf: ingest},    geom: {w: 100, d: 80, h: 30}}
- {id: sink,      shape: cylinder,  place: {rightOf: transform}, geom: {w: 90, d: 90, h: 36}}
```

### I want a staircase climbing the iso axis

Each tile is right of AND in front of its predecessor (gap 0 =
corner-to-corner):

```yaml
- {id: s1, shape: rectangle, geom: {w: 120, d: 120, h: 70}}
- {id: s2, shape: rectangle, place: {rightOf: s1, inFrontOf: s1, gap: 0}, geom: {w: 120, d: 120, h: 70}}
- {id: s3, shape: rectangle, place: {rightOf: s2, inFrontOf: s2, gap: 0}, geom: {w: 120, d: 120, h: 70}}
```

See [`samples/topology/rag-pipeline`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/rag-pipeline/input.yaml)
for a two-step stair anchoring a full scene (substrates along the
iso diagonal).

### I want a grid / dashboard of cells

One container, `layout: grid` — the substrate auto-sizes:

```yaml
- id: board
  shape: group
  geom: {h: 20}                       # only thickness; w/d derived
  layout: {mode: grid, cols: 3, gap: 0.5}
  parts:
    - {id: c1, shape: rectangle, geom: {w: 90, d: 90, h: 6}, label: dev}
    - {id: c2, shape: rectangle, geom: {w: 90, d: 90, h: 6}, label: build}
    # … 7 more cells
```

`mode: row` marches along world +x, `column` along +y. See
[`samples/topology/langchain-app`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/langchain-app/input.yaml)
for 2×2 grid service boards carrying glyph icons.

### I want a part stacked ON TOP of another (plinth, ghost volume)

`above` pins world z — flush on the sibling's top, centred on its
footprint, no copied heights:

```yaml
- {id: bar,   shape: rectangle, geom: {w: 116, d: 116, h: 36}}
- {id: ghost, shape: rectangle, place: {above: bar}, geom: {w: 116, d: 116, h: 114},
   style: {palette: {top: "none", left: "none", right: "none"},
           stroke: {color: "#A5B4FC", width: 1.2, dash: "4 4"}}}
```

See [`samples/topology/training-compute`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/training-compute/input.yaml).

> **Connectors + elevation:** `above` / `wz` is for decorative parts
> with no wiring. If a stacked part needs a connector, the rule below
> (tiers) applies — keep it coplanar with what it links to.

### I want a multi-tier architecture (gateway → service → data)

Group each tier and give the whole tier ONE `wz`, so connectors
inside a tier stay flat 2D and only tier→tier links cross planes —
that vertical drop then reads as a deliberate cross-layer call:

```yaml
parts:
  - {id: edge,    shape: group, offset: {wz: 160}, parts: [ ... ]}   # all L7 nodes share wz 160
  - {id: service, shape: group, offset: {wz: 80},  parts: [ ... ]}   # all service nodes share wz 80
  - {id: data,    shape: group, offset: {wz: 0},   parts: [ ... ]}   # data tier on the ground
connectors:
  - {from: edge.gw, to: service.api, routing: orthogonal}   # cross-tier: shows a riser, intentional
  - {from: service.api, to: service.cache, routing: orthogonal}  # intra-tier: flat L
```

Never give two peer nodes in the SAME tier different `wz` — that
produces a riser with no semantic meaning (the noir-sandbox bug).

### I want a hub with satellites around it

One rule — `ring`: first child is the hub, the rest distribute
clockwise from the back:

```yaml
nodes:
  scene:
    shape: composite
    layout: { mode: ring, gap: 2.6 }
    parts:
      - { id: core, ... }        # the hub
      - { id: sat1, ... }        # straight behind the hub
      - { id: sat2, ... }        # …and around, clockwise
```

See [`samples/topology/ai-platform`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/ai-platform/input.yaml)
— the eight-tile ring is exactly this.

### I want different gaps per axis

`gapX` / `gapY` override `gap` independently:

```yaml
- {id: b, place: {rightOf: a, inFrontOf: a, gapX: 3, gapY: 0}}
```

### I want to nudge one part a few pixels

Keep the `place`/`layout`, add an `offset` — it's applied as a delta
AFTER solving, and parts placed relative to this one follow along:

```yaml
- {id: b, shape: rectangle, place: {rightOf: a}, offset: {wy: 10}}
```

### I want absolute coordinates anyway

`offset: {wx, wy, wz}` without `place` is still honored (legacy
documents render byte-identically). Reach for this only for
free-form artistic scatter that relations can't express.

## Containers

### I want to wrap N elements in a labeled container

Give the group a `layout`; never position children by hand:

```yaml
- id: vpc
  shape: group
  label: "AWS VPC"
  layout: {mode: row, gap: 1.5}
  parts:
    - {id: api, shape: rectangle, geom: {w: 120, d: 80, h: 30}, label: API}
    - {id: db,  shape: cylinder,  geom: {w: 100, d: 100, h: 36}, label: DB}
```

Or `.d2` (auto-layout does everything):

```d2
vpc: AWS VPC {
  api: API
  db: Database { shape: cylinder }
}
```

Children may also use `place` against each other instead of a
`layout` — the substrate still auto-sizes around the solved bbox:

```yaml
- id: sources
  shape: group
  label: "Sources"
  geom: {h: 70}
  parts:
    - {id: spark,  shape: rectangle, geom: {w: 60, d: 60, h: 8}, icon: "iso://brand/spark"}
    - {id: hadoop, shape: rectangle, place: {rightOf: spark, inFrontOf: spark, gap: 0.5}, geom: {w: 60, d: 60, h: 8}, icon: "iso://brand/hadoop"}
```

### I want a nested container hierarchy (cluster > region > pod)

Nest `parts:` arbitrarily deep; inner groups are solved first so
outer layouts see their final size:

```yaml
- id: cluster
  shape: group
  label: Cluster
  layout: {mode: row, gap: 2}
  parts:
    - id: region_a
      shape: group
      label: Region A
      layout: {mode: grid, cols: 2, gap: 1}
      parts:
        - {id: pod_a, shape: rectangle, geom: {w: 100, d: 100, h: 30}}
        - {id: pod_b, shape: rectangle, geom: {w: 100, d: 100, h: 30}}
```

Or `.d2` — same dot-path semantics:

```d2
cluster: Cluster {
  region_a: Region A {
    pod_a: Pod
  }
}
```

### I want N replicas of one element

```yaml
- id: pod
  shape: rectangle
  geom: {w: 100, d: 100, h: 36}
  stack: {count: 3, gap: 10}
```

`count` = total layers, `gap` = world-Z step. Top layer's id is
`pod~2`, second from top `pod~1`, bottom keeps `pod`. Stacking is
vertical — it composes freely with `place`/`layout` on the ground.

### I want to lay out a graph automatically

Use `.d2` instead of YAML. iso-topology runs `dagre` by default
(polyline edges) or `elk` for orthogonal right-angle routing.

```bash
isotopo render --layout elk scene.d2 ./out
```

## Annotations & connectors

### I want a labeled callout pinned to a part

Document-level `annotations:` block:

```yaml
annotations:
  - anchor: pod~2
    side: top
    text: "p95 < 100ms\nrolling update"
    fontSize: 11
```

Sides: `top` | `right` | `bottom` | `left`. Multi-line via `\n`.
Default leader-line distance is 60 world units; override with
`distance: 90`.

### I want a directed arrow between two parts

```yaml
connectors:
  - {from: api, to: db, arrow: triangle, label: "query"}
```

For specific face-centers: `from: api.right-mid`, `to: db.top-back`.

### I want grid-aligned (Manhattan) routing

```yaml
connectors:
  - {from: api, to: db, routing: orthogonal, arrow: triangle}
```

Orthogonal paths ride the iso ground axes — every segment projects to
exactly ±30°, in register with `canvas.grid: iso`. **Use this for
architecture flows**; the default `straight` cuts across the grid.

**Golden rule for picking endpoints: connect nodes at the SAME height
(wz/tier).** Same-height pairs route as flat, clean 2D L-shapes. When
several nodes could be the source or target, prefer the coplanar one.
Only span heights for a deliberate cross-tier link (e.g. a hovering
control plane projecting down) — that renders a vertical drop segment,
which should be intentional, never an accident of mismatched `wz`.

### I want an async / replication link

Same orthogonal routing as everything else — async is expressed by
`dash`, NOT by a different geometry:

```yaml
connectors:
  - {from: region_a, to: replica, routing: orthogonal, arrow: triangle,
     stroke: {color: "#FFFFFF", width: 1.2, dash: "3 4"}}
```

`routing: bezier` exists for non-iso freeform sketches only — in an
iso scene a curve cuts across the grid and breaks the projection.

### I want a dashed connector

```yaml
connectors:
  - {from: a, to: b, stroke: {color: "#1F2937", width: 1, dash: "4 3"}}
```

`dash` uses SVG `stroke-dasharray` syntax: `"4 3"` = 4 on, 3 off.

## Shapes & styling

### I want a database / queue / stored-data icon

All three lower to the iso cylinder:

```yaml
- {id: db,    shape: cylinder,    label: "Postgres"}
- {id: queue, shape: queue,       label: "Job Queue"}
- {id: data,  shape: stored_data, label: "Blob"}
```

### I want a user / actor icon

```yaml
- {id: user, shape: person, geom: {w: 30, d: 30, h: 50}}
```

`c4-person` is also accepted as an alias.

### I want a cloud / external system icon

```yaml
- {id: gcp, shape: cloud, label: "GCP"}
```

### I want a text-only label (no extruded box)

```yaml
- {id: title, shape: iso_text, label: "Production Topology"}
```

Low-extrusion panel, just text on the top face. Good for titles.

### I want an icon on the top face instead of text

The preferred look for showcase scenes — keep top faces visual,
push words into screen-space labels or annotations.

```yaml
- {id: db,    icon: "iso://si/postgresql/336791", ...}  # REAL logo, brand color
- {id: queue, icon: "iso://si/apachekafka", ...}        # real logo, ink
- {id: fn,    icon: "iso://glyph/bolt", shape: rectangle, geom: {w: 84, d: 84, h: 10}}
- {id: cdn,   icon: "iso://glyph/globe/light", ...}     # white, for dark tops
- {id: waf,   icon: "iso://glyph/shield/22D3EE", ...}   # any hex color
```

Glyph names (generic, stroke style): cloud, database, bolt, chart,
globe, shield, lock, gear, cpu, code, layers, rocket, user, mobile,
browser, search, bell, queue.

Real brand logos (`iso://si/<slug>`): ~150 marks vendored from
Simple Icons (CC0) — mysql, postgresql, redis, apachekafka, docker,
kubernetes, terraform, openai, anthropic, pytorch, grafana, … with
the same `/light` and `/RRGGBB` color variants. Letter badges
(`iso://brand/<name>`) remain as fallbacks for non-vendored marks.
Any other `icon:` value is treated as a URL / data-URI. Full
browsable index with previews: [ICONS.md](./ICONS.md).

### I want flat / low / tall extrusion

Adjust `geom.h`. Conventions:

| Look | h | Use for |
|---|---|---|
| Paper-flat | 4–6 | substrates, ground layers |
| Standard | 30 | typical compute / API box |
| Tall | 60–80 | server rack, prominent element |
| Hero | 150–220 | THE central element of a marketing scene |

For `.d2` input, the height comes from `d2ShapeCatalog`'s
multiplier × base height. Override per-shape via the YAML path if
you need different heights.

### I want brand colors on a specific shape

```yaml
- id: api
  shape: rectangle
  style:
    palette: {top: "#3B82F6", left: "#1D4ED8", right: "#2563EB"}
    text:    {color: "#FFFFFF", weight: "600"}
```

For document-wide branding, put it in `theme:` once and every part
inherits:

```yaml
theme:
  palette: {top: "#FFFFFF", left: "#7E94B5", right: "#A6B7D2"}
  text:    {family: Inter, weight: "600", color: "#1F2937"}
```

See [`../reference/dsl-theme.md`](../reference/dsl-theme.md) for the
full cascade.

### I want to reuse one style across many parts

Define a named preset in the theme, reference it by name — no YAML
anchors, works in JSON, typos get "did you mean" suggestions:

```yaml
theme:
  presets:
    tile:
      palette: { top: "#FFFFFF", left: "#E2E4EA", right: "#EEF0F4" }
      effects: { cornerRadius: 12 }

# parts:
- { id: a, shape: rectangle, preset: tile, icon: "iso://glyph/gpu", label: "GPU" }
- { id: b, shape: rectangle, preset: tile, icon: "iso://glyph/lake", label: "Lake" }
```

Per-part `style` still overrides individual preset fields. For which
presets a good scene needs, read the
[scene design guide](../guides/scene-design.md).

### I want a gradient face / glow / shadow / texture

```yaml
- id: hero
  shape: rectangle
  geom: {w: 220, d: 220, h: 200}
  style:
    palette:
      topGradient:   {from: "#FFE188", to: "#F59E0B", dir: down}
      leftGradient:  {from: "#F4F4F8", to: "#D8DAE3", dir: down}
      rightGradient: {from: "#FFFFFF", to: "#E8E8EE", dir: down}
    effects:
      cornerRadius: 8
      backglow:   {color: "#FFC857", radius: 80, opacity: 0.55}
      dropShadow: {dx: 0, dy: 14, blur: 22, color: "rgba(245,158,11,0.3)"}
```

`pattern: {kind: hatch|dots, color, spacing, angle}` overlays a
texture on box top faces, cylinder top ellipses, and sphere discs.
Use ONE hero treatment per scene — backglow on everything reads as
noise.

### I want line-art / wireframe ghosts

`effects.wireframe: true` keeps strokes and drops every fill — pair
with a dashed stroke for floating "ghost" frames. None-filled and
wireframe parts are exempt from overlap warnings, so they can sit on
or around solid parts freely:

```yaml
- id: ghost_die
  shape: rectangle
  place: { above: cpu }
  offset: { wz: 26 }
  geom: { w: 86, d: 86, h: 30 }
  style:
    palette: { top: "none", left: "none", right: "none" }
    stroke:  { color: "#7C3AED", width: 1.2, dash: "3 4" }
    effects: { wireframe: true, cornerRadius: 10 }
```

### I want a film-grain / print texture

`effects.grain` overlays monochrome noise — the editorial "print ad"
register (see [`duckdb-handdrawn`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/duckdb-handdrawn/input.yaml)):

```yaml
style:
  effects:
    grain: { intensity: 0.45, scale: 0.8 }
```

Works on boxes (plain + rounded) and cylinders. Keep intensity
≤ 0.5 on dark fills or the noise greys them out.

### I want a screen-horizontal row (editorial layout)

In iso projection a plain `rightOf` row slopes down-right. To march
straight across the screen, pair `rightOf` with `behind` and tune
per-axis gaps so Δx = −Δy:

```yaml
- id: b
  place: { rightOf: a, behind: a, gapX: 2.2, gapY: 2.45 }
```

## Canvas / backdrop

### I want an iso ground grid

```yaml
canvas:
  background: "#FAFBFC"
  grid: iso
  gridColor: "#E2E6EE"
  gridStep: 40
```

`gridStep` doubles as the layout/place CELL size — keep them equal
so arranged parts and orthogonal pipes land on the grid texture.

### I want dots / hatch / solid instead

```yaml
canvas: {grid: dots, gridColor: "#CCCCCC"}
canvas: {grid: hatch, gridColor: "#E2E6EE"}
canvas: {grid: solid, background: "#FAFBFC"}
```

### I want a transparent / no backdrop

Omit `canvas:` entirely.

## Common composite patterns

### Stat card (icon + value + caption)

```yaml
nodes:
  vite_stat:
    shape: composite
    parts:
      - {id: icon,    shape: rectangle, geom: {w: 70, d: 70, h: 20}, label: "VITE"}
      - {id: value,   shape: iso_text,  place: {rightOf: icon, gap: 1.5, align: start}, label: "121m+"}
      - {id: caption, shape: iso_text,  place: {inFrontOf: value, gap: 0.5, align: start}, label: "weekly npm"}
```

### Region / zone container

```yaml
- id: region
  shape: group
  label: "us-east-1"
  layout: {mode: row, gap: 1.5}
  style:
    palette: {top: "#EEF1F8", left: "#A6ADCB", right: "#C5CADE"}
  parts: [ ... ]
```

### Stacked replicas inside a worker node

```yaml
- id: worker
  shape: group
  label: "Worker Node"
  layout: {mode: row}
  parts:
    - id: pod
      shape: rectangle
      geom: {w: 100, d: 100, h: 30}
      stack: {count: 3, gap: 10}
```

### Hero-anchored marketing scene

Pick the central element, give it no `place` (it anchors the world
origin), chain every satellite off it. See
[`samples/topology/ai-platform`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/ai-platform/input.yaml) /
[`clickhouse-hub`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/clickhouse-hub/input.yaml) /
[`training-compute`](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/training-compute/input.yaml) —
all coordinate-free.

## Validation

### I want my agent's output checked before render

```bash
isotopo validate scene.yaml
# exit 0 = clean, 2 = warnings only, 3 = errors
```

JSON output is JSONPath-located with "did you mean" suggestions —
apply them programmatically and re-validate. See
[`PROMPT_TEMPLATE.md`](./PROMPT_TEMPLATE.md) for the agent-side
boilerplate.

### What the validator checks

**Errors** (block render):
- Unknown shape names, with "did you mean" suggestions
- Connector `from`/`to` referencing a non-existent part id
- Duplicate part ids; `place` cycles and dangling references
- Negative dimensions or gaps; invalid anchor suffixes

**Warnings** (render succeeds, quality suffers):

| Check | Threshold | Path in JSON |
|---|---|---|
| Top-face fill vs label text contrast | ratio < 3.0 | `.style.fill` |
| Node fill vs canvas background | ratio < 1.5 | `.style.fill` |
| Group fill vs direct-child fill | ratio < 1.3 | `.style.fill` |
| Label truncation risk | estimated px > 95% of node width | `.label` |
| Label too long | > 40 chars | `.label` |
| High connector out-degree | ≥ 5 outgoing from one part | `.connectors[N]` |
| Deep group nesting | depth > 3 | `.parts[id]` |
| Post-solve sibling overlaps | any footprint intersection | `.parts[id]` |

Contrast ratios use WCAG relative luminance (sRGB linearised). Only
`#RRGGBB` / `#RGB` colours are evaluated; named colours and
`rgba(…)` are skipped silently.

### I want to track whether an edit improved quality (golden diff)

```go
before := isotopo.Validate(docBefore)
after  := isotopo.Validate(docAfter)
diff   := isotopo.DiffIssues(before, after)
// diff.Fixed    — issues resolved by the edit
// diff.New      — issues introduced by the edit
// diff.Remained — unchanged issues
```

`DiffIssues` matches issues by `(Path, Message)` pair. Use it in an
agent loop to confirm each iteration moves toward zero issues, not
just changes them:

```python
before_issues = validate(dsl_v1)
for attempt in range(3):
    dsl = llm.fix(dsl, issues)
    after_issues = validate(dsl)
    diff = diff_issues(before_issues, after_issues)
    if not diff["new"] and not diff["remained"]:
        break          # all clean
    before_issues = after_issues
```

## When you want behaviour we don't have

These are the recurring asks. If you hit one:

| Want | Today's workaround | Long-term |
|---|---|---|
| custom polygon (n-sided) | use rectangle with explicit palette | extending iso25d/ — see [`../guides/extending.md`](../guides/extending.md) |
| wireframe / line-art look | manual stroke + transparent palette | tracked: line-only render mode |
| pattern on cylinder/sphere SIDE walls | top-face/disc patterns ship today | tracked: side-wall patterns |
| swimlane / sequence | author with `iso_text` rows | tracked: sequence_diagram lowering |

For everything else, search this file. If it's not here and not in
the references, it's probably a [missing primitive](../guides/extending.md).
