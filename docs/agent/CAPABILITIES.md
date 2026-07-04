# Capabilities — v0.4.5

Generated from `CapabilityReport()`. Do not edit by hand — run
`go run ./tools/gen-docs` to regenerate after a code change.

Same content as `isotopo capabilities` JSON, but markdown for skim-reading.

## Input formats

| Extension | Layout | Description |
|---|---|---|
| `.yaml` | manual | hand-authored iso composite with precise placement |
| `.json` | manual | same shape as .yaml but JSON-encoded |
| `.d2` | auto | d2 graph source; isotopo runs auto-layout then translates |

## Layout engines (.d2 input)

| Name | Edges | Description |
|---|---|---|
| `dagre` | polyline | default; fast, natural-bend polyline edges |
| `elk` | orthogonal | ELK; orthogonal right-angle edges with obstacle avoidance |

## Shapes

Each row is one iso shape with every DSL alias accepted. The
height hint is the default extrusion multiplier — agents can
override per-part via `geom.h`.

| Iso shape | Accepted aliases | Height hint | Notes |
|---|---|---|---|
| **array1d** | `array1d` | 1.0 | v3.11 — linear row of N identical cells. params: countX (default 3), gap (default 6). Semantic: pipeline stages, replica group. |
| **array2d** | `array2d` | 1.0 | v3.11 — N×M grid of cells on the ground plane. params: countX, countY (default 3×3), gap. Semantic: shard matrix, node pool, partition table. |
| **array3d** | `array3d` | 1.0 | v3.11 — N×M×K volumetric grid. params: countX, countY, countZ (default 3×3×3), gap. Semantic: GPU cluster, tensor, embedding matrix. |
| **boundary** | `boundary` | 1.0 | v3.5 — a group whose substrate is a dashed OUTLINE-ONLY flat region (VPC / subnet / trust zone). Same nesting/layout/autosize as group; style.stroke restyles the dashes |
| **circle** | `circle`, `oval` | 1.0 |  |
| **cloud** | `cloud` | 0.8 | free-form rounded outline; styles via palette top/left (+ topGradient/leftGradient) — top paints the crown, left the wrap-around side band; right is unused (no right face, linted). Top-face icon + label supported with box-family anchor/scale semantics on the cloud's inscribed content rect |
| **composite** | `composite` | 1.0 | container — holds parts: [] of CompositePart entries |
| **custom_path** | `custom_path` | 1.0 | v3.10 — arbitrary polygon base extruded vertically. Provide path: in geom.params (M/L/Z SVG commands). Semantic: any brand or domain-specific shape. |
| **cylinder** | `cylinder`, `queue`, `stored-data`, `stored_data` | 1.0 |  |
| **group** | `group` | 1.0 | v2 primitive — translucent labeled substrate wrapping nested parts |
| **hexprism** | `hexagon` | 0.7 | v3.2 — 6-gon prism: API gateway / middleware semantics |
| **iso_text** | `text` | 0.3 | flat text panel (low extrusion) |
| **octprism** | `octprism` | 1.0 | v3.2 — 8-gon prism: firewall (stop-sign) / decision-gate semantics |
| **person** | `c4-person`, `c4_person`, `person` | 1.2 | human actor: sphere head fused onto a dome torso. Body halves take palette.left/right; palette.leftGradient/rightGradient shade the matching half, palette.topGradient shades BOTH halves as one body gradient (no drawn top face). effects.outline / dropShadow / backglow follow the true head+dome silhouette. style.faces and effects.pattern are ignored (validator warns) |
| **rack** | `rack` | 1.0 | v3.11 — server rack with slot shelves. params: slots (default 4). Semantic: physical server rack, blade chassis, equipment cabinet. |
| **rectangle** | `callout`, `class`, `code`, `document`, `hierarchy`, `image`, `package`, `page`, `rectangle`, `sequence-diagram`, `sequence_diagram`, `sql-table`, `sql_table`, `square`, `step` | 1.0 |  |
| **triprism** | `triprism` | 1.0 | v3.2 — 3-gon prism: alert / one-way fan-out semantics |
| **wedge** | `wedge` | 0.7 | v3.10 — sloped prism: back edge raised to full height, front edge at z=0. Semantic: ramp, data ingestion pipeline, traffic escalation. |

## Composition primitives

### `group`

**Where:** `node.parts[*]`

**Syntax:** `{shape: group, layout: {mode: row|column|grid|ring}, parts: [...], label: "…", geom: {h}}`

Wrap N parts in a translucent labeled iso substrate. Position children with layout (or per-child place) — the substrate then auto-sizes around them, so geom.w/d may be omitted. Child offsets are fine-tune deltas relative to the group.

| Field | Meaning |
|---|---|
| `geom` | h = substrate thickness (low, default 6); w/d only when overriding the auto-sized footprint |
| `label` | rendered on the top face of the substrate (or screen-space if style.text.orient=screen) |
| `layout` | auto-arrangement of the children — preferred over per-child coordinates |
| `parts` | list of CompositePart (other groups OK — supports unlimited nesting) |

### `stack`

**Where:** `node.parts[*].stack`

**Syntax:** `stack: {count: N, gap: g}`

Replicate a part N times vertically with a Z-gap. Bottom copy keeps the id; the rest are suffixed "~k" so connectors can target a specific layer.

| Field | Meaning |
|---|---|
| `count` | total number of layers (1 is a no-op) |
| `gap` | world Z step between layers (default = part.H + 4 if omitted) |

### `canvas-grid`

**Where:** `document.canvas`

**Syntax:** `canvas: {background: "#FAFBFC", grid: iso, gridColor: "#E2E6EE", gridStep: 36}`

Paint an iso-aware backdrop under the scene. grid: iso draws a diamond rhombus lattice that aligns with the part projection.

| Field | Meaning |
|---|---|
| `background` | solid background color (also bleed color behind patterns) |
| `depthSort` | v5.2 — true / false (default false). Painter's-algorithm depth sort: parts paint back-to-front by world depth (corner sum x+y+z), merged across groups, instead of author order; connector routes layer by their endpoints' depth instead of sitting under every body. Group slabs/platforms still paint at the very back. Turn on when parts from different groups overlap in projection or when author order would paint a back part over a front one. Iso projection only (no effect with projection: top). |
| `grid` | iso | dots | hatch | solid | none |
| `gridColor` | pattern stroke / dot color |
| `gridStep` | tile size in world units (default 40) |
| `padding` | v3.1 — uniform breathing margin in px around the final composition; use 40-80 for sparse hero shots |
| `projection` | v0.8 — iso (default) | top. top renders a flat top-down plan view (footprints + orthogonal edges, height dropped) for reading the layout/flow |

### `annotation`

**Where:** `document.annotations[*] | node.annotations[*] (v3.0)`

**Syntax:** `{anchor: <part-id>, text: "…", side: top|right|bottom|left, distance: 60}`

Screen-space callout pinned to a composite part. Multi-line text is supported via \n. Placement is collision-managed: the box never crosses or touches any part projection (6px clearance) and never overlaps other text; side/distance are the FIRST candidate, and the box slides to the nearest free peripheral position when occupied.

| Field | Meaning |
|---|---|
| `anchor` | id of the part to point at |
| `bg` | fill color (default white) |
| `border` | stroke color (default near-black) |
| `distance` | world-unit gap from part silhouette to box (default 60) |
| `side` | which direction the box sits relative to the part |
| `text` | multi-line text (split on \n) |

### `connector`

**Where:** `node.connectors[*]`

**Syntax:** `{from: <part-id>, to: <part-id>, routing: straight|orthogonal|orthogonal2|bezier, arrow: none|triangle, label: "…"}`

Directed line between two parts, optionally labeled and orthogonal-routed. Coplanarity contract: the two endpoints should share the same base height (wz / tier) — orthogonal connectors hug the ground plane, so same-tier links render as clean 2D L-shapes. Connecting endpoints at DIFFERENT heights forces a vertical drop segment (riser); do that only to express a deliberate cross-tier call (e.g. gateway layer → data layer), never as a side effect of giving peer nodes different wz.

| Field | Meaning |
|---|---|
| `arrow` | none = no head; triangle = filled arrowhead at the dst |
| `bend` | v4.4 — {wx, wy} world offset on the route's interior so a Studio edge-drag shifts the line while both endpoints stay docked |
| `elbow` | v3.1 — orthogonal elbow bias: xFirst | yFirst (default: the axis the source face exits along). orthogonal2 auto-routes and ignores elbow. |
| `from` | source part id; "id.anchor" picks a specific face-centre (e.g. central.right-mid). Bare ids auto-pick the face FACING the other endpoint. STRONGLY PREFER a target at the SAME height (wz/tier) as this source — same-height pairs route as clean flat 2D lines; pick the coplanar candidate when several would work. |
| `labelBg` | pill background (default #FFFFFFEE) |
| `labelColor` | v3.1 — pill ink (default #1F2433); dim it for dark scenes |
| `labelFontSize` | v3.1 — pill font size (default 11) |
| `routing` | ALWAYS use orthogonal — every segment rides the iso ground axes, flush with the 2.5D grid (collinear endpoints collapse to one on-axis segment). orthogonal2 (opt-in v2) keeps the same iso-axis discipline but searches a multi-segment route that detours around node footprints and avoids crossing already-placed edges — reach for it when a dense scene makes plain orthogonal tunnel a node or cross another line. straight/bezier cut across the grid and are reserved for non-iso freeform sketches. Endpoints should be coplanar (same wz); see Purpose for cross-tier risers. |
| `to` | destination part id (same anchor syntax). Choose an endpoint coplanar with `from` (same wz/tier) whenever possible; only span heights for an intentional cross-tier link, which renders a vertical drop segment. |
| `waypoints` | v4.6 — explicit interior corner list [{wx, wy}, …] (world coords, source→target) the route threads through, so a Studio edge-drag can move ONE segment independently. Supersedes bend + the auto route; endpoints stay docked. Auto-kept iso-axis-aligned. |

### `layout`

**Where:** `node.layout | node.parts[*].layout (groups)`

**Syntax:** `layout: {mode: row|column|grid|ring, cols: N, gap: 1, padding: 1, align: start|center|end}`

Auto-arrange a container's parts along the iso ground axes — the preferred way to position parts. No hand-computed coordinates: row marches along world +x, column along +y, grid wraps row-major after cols, ring puts the FIRST child at the centre and distributes the rest on a circle around it (hub-and-spoke in one rule). A layout group's geom.w/d may be omitted; the substrate auto-sizes around the arranged content.

| Field | Meaning |
|---|---|
| `align` | cross-axis alignment within each track (default center) |
| `cols` | grid only; default ceil(sqrt(n)) |
| `gap` | space between children (ring: hub-to-satellite clearance), in CELLS (1 cell = gridStep, default 40 world units); default 1 |
| `mode` | row | column | grid | ring | auto. auto (v4.2) is connector-driven: declare parts + connectors with NO place/coords and the engine layers the graph into a balanced iso flow — per-part style (faces/prisms/effects) is untouched. The other modes arrange a chosen structure. |
| `padding` | content inset from the container edge, in cells; defaults to gap |

### `place`

**Where:** `node.parts[*].place`

**Syntax:** `place: {rightOf: <sibling-id>, inFrontOf: <sibling-id>, above: <sibling-id>, gap: 1, gapX: 2, gapY: 0, align: start|center|end}`

Position a part relative to a SIBLING — the preferred way to compose free-standing scenes. One constraint per axis: rightOf/leftOf pins world x, inFrontOf/behind pins world y (front = toward viewer), above pins world z. `above` is for DECORATIVE stacking — plinths, ghost volumes, toppers (the part sits flush ON TOP of the sibling, centred on its footprint). If an elevated part also needs a connector, keep it on the same tier (wz) as what it connects to, or the link will hang in a vertical drop; reserve `above` for parts with no connectors. Unpinned ground axes align to the referenced sibling per align. Chains are solved topologically (a stair = each tile rightOf+inFrontOf its predecessor). offset degrades to a fine-tune delta.

| Field | Meaning |
|---|---|
| `above` | sibling id — flush on its top face (z = its top), x/y centred unless also pinned |
| `align` | alignment along the unconstrained axis (default center) |
| `behind` | sibling id — -y side (mutually exclusive with inFrontOf) |
| `gap` | distance from the sibling's footprint, in cells; default 1 |
| `gapX` | overrides gap on the x axis only |
| `gapY` | overrides gap on the y axis only |
| `inFrontOf` | sibling id — +y side, toward the viewer |
| `leftOf` | sibling id — -x side (mutually exclusive with rightOf) |
| `rightOf` | sibling id — this part sits on its +x side |

### `preset`

**Where:** `theme.presets + node.parts[*].preset`

**Syntax:** `theme: {presets: {<name>: <Style>}}  →  part: {preset: <name>}`

Named design-system style presets — define a Style once in the theme, reference it by name from any part. Merges between the per-shape defaults and the part's own style block, so parts can still override individual fields. The portable replacement for YAML anchors (works in JSON, validates with suggestions). Typical systems: hero / satellite / ghost.

| Field | Meaning |
|---|---|
| `preset` | preset name on a Node or CompositePart; unknown names are validation errors with a nearest-name suggestion |
| `theme.presets` | map of preset name to a full Style block (palette / stroke / text / effects) |

### `icon`

**Where:** `node.parts[*].icon`

**Syntax:** `icon: "iso://glyph/<name>[/light|/RRGGBB]" | "iso://si/<slug>[/light|/RRGGBB]" | "iso://brand/<name>"`

Put an icon on a part's top face instead of text — the preferred look for showcase scenes. REAL brand logos (iso://si/<slug>, ~150 vendored from Simple Icons, CC0): mysql, postgresql, redis, apachekafka, docker, kubernetes, terraform, openai, anthropic, pytorch, grafana, ...; same /light and /RRGGBB variants as glyphs; full list in docs/agent/ICONS.md. Glyphs (generic, stroke style): cloud, database, bolt, chart, globe, shield, lock, gear, cpu, code, layers, rocket, user, mobile, browser, search, bell, queue; default ink, /light = white (dark tops), /RRGGBB = custom. Brand aliases (iso://brand/<name>, resolve to the real logo in its brand color): spark, hadoop, mysql, postgresql, hive, pulsar, kafka, redis, mongo, kubernetes, docker, github, gcp, vite, rolldown, oxc. Unknown iso:// icons are validation errors with nearest-name suggestions. Any other icon value is treated as an external resource: an http(s) URL, a data: URI, OR a LOCAL IMAGE FILE PATH (svg/png/jpg/gif/webp, absolute or relative, ~ and file:// accepted) — local paths are read and inlined as a data URI at render time so the output stays self-contained. Browsable index with previews: docs/agent/ICONS.md.

| Field | Meaning |
|---|---|
| `icon` | iso://glyph/<name>[/variant] | iso://brand/<name> | https://… | data:image/… | ./local.svg (inlined at render) |

## Style keys

Every field under each `style.*` sub-block.

| Block | Fields |
|---|---|
| `palette` | `top`, `left`, `right`, `topGradient {from, to, dir}`, `leftGradient {from, to, dir}`, `rightGradient {from, to, dir}` |
| `stroke` | `color`, `width`, `dash` |
| `faces (v3.3)` | `map of face name → {fill, strokes}; names: top|left|right (box family), top|side0..sideN-1 (prisms), "*" wildcard; outranks palette`, `fill {kind: solid|linearGradient|radialGradient|pattern, color, stops: [{offset 0..1, color}], angle (linear, degrees), cx/cy (radial 0..1), pattern {kind: hatch|dots, color, spacing, angle, projected}}`, `projected: true pins the pattern tile to the face's iso plane instead of screen space`, `strokes: [{color, width, dash, opacity}] — multiple re-traces per face, list order = paint order (put thin highlight lines after wide outlines)`, `note: a pattern fill REPLACES the base fill (transparent tile background); supported on the box family, prisms, and cylinders (top/left/right) — cloud/person/sphere keep palette only for now` |
| `text` | `family`, `size (a MAXIMUM — top-face labels auto-wrap at word boundaries and auto-shrink so they never overflow the face; icons are clamped to the face too)`, `weight`, `color`, `orient`, `boxBg`, `boxBorder` |
| `effects` | `opacity`, `margin`, `cornerRadius`, `dropShadow {dx, dy, blur, color}`, `backglow {color, radius, opacity}`, `blur (v3.7) — gaussian stdDev in px over the whole part; fog / ghost / de-emphasized layers`, `outline {color, width, dash, opacity} (v3.8) — accent ring along the part's full silhouette (selection / emphasis), distinct from per-face stroke; hugs the true outline of every shape`, `pattern {kind: hatch|dots, color, spacing, angle}`, `wireframe (bool — line-art: strokes only, no fills; ghost parts are exempt from overlap warnings)`, `grain {intensity 0..1, scale} (film-grain noise on the faces)`, `faceSplit (bool — rounded boxes only: split the single wrap-around side band into independent left/right faces so a rounded cube shades per-face like a sharp one; needs cornerRadius>0, no-op otherwise)` |

## Enums

Closed vocabularies the engine accepts verbatim — a value outside the set silently falls back, so emit these exactly. `isotopo validate` rejects deviations with a nearest-match suggestion.

| Key | Values |
|---|---|
| `annotation_side` | `top`, `right`, `bottom`, `left` |
| `connector_anchor` | `top`, `top-mid`, `center`, `bottom`, `bottom-mid`, `left`, `left-mid`, `right`, `right-mid`, `back`, `back-mid`, `front`, `front-mid` |
| `connector_arrow` | `none`, `triangle` |
| `connector_routing` | `straight`, `orthogonal`, `orthogonal2`, `bezier` |
| `layout_mode` | `row`, `column`, `grid`, `ring`, `auto` |
| `place_relation` | `rightOf`, `leftOf`, `inFrontOf`, `behind`, `above` |

## See also

- [`RECIPES.md`](RECIPES.md) — task → DSL primitive lookup
- [`schema/dsl.schema.json`](schema/dsl.schema.json) — JSON Schema for local lint
- [`../reference/dsl-yaml.md`](../reference/dsl-yaml.md) — full YAML grammar with examples
