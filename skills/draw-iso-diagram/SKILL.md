---
name: draw-iso-diagram
description: Generate design-grade 2.5D isometric architecture diagrams from text DSL using the isotopo CLI. Use when the user asks for an architecture diagram, system topology visual, isometric/2.5D illustration, infra or AI-pipeline graphic, or wants a diagram they can commit and diff. Produces SVG (plus per-element fragments) from YAML or d2 source.
---

# Draw an isometric architecture diagram

You are driving `isotopo`, a deterministic renderer that turns text DSL into
2.5D isometric SVG. You are BOTH a designer (topology, colour, icons, captions)
and a DSL engineer (you only emit grammar the engine recognises). You never
compute coordinates — you declare *relations* and the solver does the geometry.

Assume **zero prior knowledge**: the four steps below take you from "is this
tool even installed?" to a committed, professional diagram. Do them in order.

## 0 · Preflight — is the tool installed?

```bash
isotopo capabilities >/dev/null 2>&1 && echo "isotopo: ready"
```

If that prints nothing / errors, install it, in this order of preference:

```bash
# A) Go toolchain present → install the binary onto PATH
go install github.com/MarkovWangRR/iso-topology/cmd/isotopo@latest   # adds to $(go env GOPATH)/bin

# B) Inside a clone of the repo, no install needed → prefix every command with:
go run ./cmd/isotopo <args>

# C) No Go at all → tell the user; they need Go ≥1.21 (https://go.dev/dl) or a
#    prebuilt binary from the repo's Releases. Don't fake a render.
```

Re-run `isotopo capabilities >/dev/null && echo ok` until it succeeds. If you
used (B), read every `isotopo …` below as `go run ./cmd/isotopo …`.

## 1 · Learn the tool in one shot

`isotopo capabilities` (run ONCE per session) is the complete, current,
machine-readable inventory of every shape, primitive, style key, and enum you
may emit — it is the source of truth, not your memory. The whole workflow is
four commands:

| command | what it's for |
|---|---|
| `isotopo capabilities` | discover what's available (shapes, icons, style keys) |
| `isotopo validate <in>` | **legality self-check** — exit 0 clean / 2 warnings / 3 errors; JSON issues with `suggest` fixes |
| `isotopo evaluate <in>` | **layout-quality self-check** (headless, deterministic) — crossings, edges-through-nodes, overlaps, bends, flow axis |
| `isotopo render <in> <out-dir>` | produce `topology.svg` (+ html + per-part fragments) |

`validate` and `evaluate` are your eyes when you can't see pixels — lean on
them. `render` is only needed to ship or to eyeball observable polish.

## 2 · Vocabulary you may use (don't invent names)

**Input mode.** Plain box-and-arrow graph with no styling demands → write a
`.d2` file (`a -> b: label`, nested `{}` for groups) and let auto-layout do
everything. A composed, styled scene → write `.yaml` (rest of this skill).

**Shapes — pick by ROLE, don't make everything a rectangle:**

| role | shape |
|---|---|
| service / app | `rectangle` |
| database | `cylinder` |
| gateway / load balancer | `hexprism` |
| external system / SaaS | `cloud` |
| person / team | `person` |
| cluster / node pool / k8s | `rack` |
| frontend / console | `screen`, `browser-panel` |
| function / endpoint | `capsule` |
| queue / shard / tensor | `array1d`, `array2d`, `array3d` |
| route / branch | `diamond` |
| event / small node | `circle` |
| service variants (tier apart) | `prism`, `triprism`, `octprism` |
| accent (CDN/funnel/ring bus — sparingly) | `cone`, `dome`, `frustum`, `pyramid`, `wedge`, `torus` |
| title / legend / annotation | `iso_text` |
| **grouping container** | `group` (and `boundary` for a dashed zone) — see §3 |
| the required top wrapper | `composite` (the one `scene`) |

**Icons** (top face carries ONE icon + a short caption):
- `iso://si/<slug>` — ~150 REAL brand logos (`postgresql`, `apachekafka`,
  `docker`, `openai`, `snowflake`, …). Prefer a real logo when a node IS a
  brand/product.
- `iso://glyph/<name>` — generic concepts (`database`, `warehouse`, `stream`,
  `queue`, `cpu`, `gpu`, `model`, `agent`, `shield`, `chart`, `cloud`, …).
- `iso://brand/<name>` — a few engine-built brand marks (`spark`, `kafka`).
- Suffix `/light` for dark top faces, `/RRGGBB` to tint. Full index:
  `docs/agent/ICONS.md`. **Never invent a glyph name** — unknown names are
  validation errors.

**Icon fallback ladder** — follow in order, stop at the first success:

1. **Built-in catalog** (`isotopo capabilities` → `icons[]`): pick the entry
   whose `description` best matches the node's role. Prefer `iso://si/<slug>`
   for named products/brands; `iso://glyph/<name>` for generic concepts.
2. **Semantic glyph approximation**: if no slug matches the brand but a glyph
   fits conceptually (e.g. "queue" for any message broker, "database" for any
   store), use that glyph. A semantically close built-in beats an external fetch.
3. **icon-search** (requires `icon-search` on PATH — install once per session
   with `npm install -g icon-search` if missing):
   ```bash
   # save SVG to a temp file; isotopo inlines it at render time
   icon-search "<node label or product name>" --best > /tmp/icon-<id>.svg
   ```
   Then set `icon: "/tmp/icon-<id>.svg"` on the part. The renderer reads and
   inlines local paths as data URIs so the output SVG stays self-contained.
   If `icon-search` exits 2 (no match), proceed to step 4.
4. **Omit the icon** — leave the `icon:` field out entirely. The label alone
   is always valid; a missing icon is far better than a broken or invented one.

**Dark top faces require a light icon variant.** When a node's top fill is dark (luminance < ~18%, e.g. `#0A0A0A`–`#1F2937`), the default icon ink (`#1F2937`) becomes invisible. Always add `/light` for white icons or `/RRGGBB` for a custom tint:
```
icon: "iso://si/snowflake/light"    # white on dark top
icon: "iso://glyph/database/29B5E8" # tinted on dark top
```
`isotopo validate` will warn if you forget this on a built-in icon.

**Connectors:** `routing: orthogonal` (rides the iso grid — the default you
want) | `orthogonal2` (obstacle- and crossing-aware multi-segment router —
switch a specific edge to it when `evaluate` reports that edge tunnelling a
node or crossing another edge and placement nudges won't clear it) |
`straight` | `bezier`; arrow `triangle` | `none`. Async links differ by
`stroke.dash`, not by routing. Hairline widths (1–2) beat thick pipes.

**Colour** — there is NO `style.fill`. A box is shaded by its three iso faces
`style.palette { top: <light>, left: <mid>, right: <dark> }` (or per-face
`*Gradient`). Reusable swatches (top/left/right triples):
blue `#60A5FA/#3B82F6/#2563EB` · green `#4ADE80/#22C55E/#16A34A` ·
purple `#C084FC/#A855F7/#9333EA` · orange `#FDBA74/#FB923C/#EA580C` ·
amber `#FCD34D/#F59E0B/#D97706` · teal `#5EEAD4/#14B8A6/#0D9488`. On dark tops, use `iso://…/light` icons (white ink) or `iso://…/RRGGBB` for a custom tint — the default ink is near-black and invisible on dark faces.

**Contrast is non-negotiable: the TOP-FACE FILL must never be close in
lightness to the top-face TEXT + ICON colour** — the caption and glyph sit on
the top face, and a label that matches its background is invisible. Rule of
thumb: a light top (`#FFFFFF`, pastel) takes dark ink (`style.text.color` ~
`#1B2230`, icon untinted/dark); a dark/saturated top takes light ink (text
`#F1F5F9`, `icon: "iso://…/light"`). Aim for a clear lightness gap, not a subtle
one. When in doubt, white-or-pastel top + dark ink, or deep top + white ink.

## 2.5 · Style library — pick a visual language before touching palette

**Do this before hand-crafting any colour.** The repo ships 28 reference styles
(`samples/style_refer/`) — rendered swatches covering every mood from noir-neon
to frosted-glass to chalk-monolith. Each style's `theme.presets` + `canvas`
block is a drop-in visual language; copy it, then layer your topology on top.

**Retrieval (run once per session, ~5 s):**

```bash
# 1. Load the index — one JSON, 28 entries
cat samples/style_refer/index.json
```

Each entry has:
- `essence` — one-sentence visual summary
- `triggers` — keyword list for fuzzy matching
- `mood` / `tone` (light|dark) / `use_cases` — semantic tags
- `features` — texture tags (`glow`, `grain`, `gradient`, `wireframe`, …)
- `dsl_file` — path to the reusable YAML preset block

**Selection algorithm:**

1. Extract tone from user request (dark / moody / noir → `tone: dark`; clean /
   product / bright → `tone: light`; unspecified → no filter).
2. Score each style: count keyword overlaps between the user's request and that
   style's `triggers + mood + use_cases + essence` (case-insensitive).
3. Pick the highest-scoring entry whose `tone` matches (or any if unspecified).
4. Read its `dsl_file` → copy the `canvas:` block and the entire
   `theme.presets:` block verbatim into your new YAML.
5. Only hand-craft palette when no style scores above ~2 keyword matches.

**Application:**

```yaml
# ① paste from dsl_file as-is:
canvas: { background: "#050507", grid: none, padding: 80 }
theme:
  presets:
    cubeA: { … }   # hero tile
    cubeB: { … }   # secondary tile
    cubeC: { … }   # muted tile

# ② add your topology on top:
nodes:
  scene:
    shape: composite
    parts:
      - { id: gateway, shape: hexprism, preset: cubeA, icon: "…", label: "…" }
      - { id: db,      shape: cylinder, preset: cubeB, icon: "…", label: "…" }
    connectors:
      - { from: gateway, to: db, routing: orthogonal, arrow: triangle }
```

The preset names (`cubeA/B/C`) are internal to each style file — rename them
to role-meaningful names (`hero`, `satellite`, `ghost`) when you apply them.

## 3 · Design FIRST, then build

Decide these BEFORE writing any DSL — they are what separate "senior architect"
from "auto-layout dump":

1. **Palette, up front. ≤ 3 hues for the whole diagram** (+ optionally one
   accent hue for the single most important node). Pick the domain's
   conventional colours (AWS amber, cloud blue, data green, security red…). When
   you have many tiers, separate sub-items by **light/dark shades of the SAME
   hue** — never add a 4th hue. >3 hues reads as noise.
2. **One hero, the rest quiet.** At most one node gets gradient flanks +
   `backglow` + `dropShadow`. Everything else stays calm.
3. **Group with real containers.** To draw a "zone/lane/module", use a `group`
   with nested `parts:` and a `layout: { mode: row|column|grid }` — it arranges
   its children AND its slab auto-wraps them (omit the group's `geom.w/d` so it
   sizes to fit). `boundary` is the same but renders as a dashed outline-only
   region (VPC / trust zone). (This is a real container — see the pitfall in §6
   about NOT faking it with a bare rectangle.)
4. **Keep connectors simple — wire at the group level.** When several nodes in
   a group talk to the outside, route ONE representative/entry node out, not a
   wire per node. And the same on the RECEIVING end: **an edge bound for a group
   should land on that group (its representative/entry node), not dive into a
   specific deep child** — inter-module links read as module→module, with
   intra-group detail expressed by intra-group edges. Fewer, higher-level wires
   = far more readable.
5. **Aim for a near-square canvas.** Architecture diagrams should not be long
   bars. Plan the module layout as a roughly square grid, not one long row.

For a big/vague request, divide and conquer: (a) sketch 2–6 modules with their
members (6–12 meaningful nodes, not 3 toy boxes); (b) lock the palette + icon
+ size conventions; (c) build one module at a time, `validate` each before the
next; (d) wire cross-module edges, then `evaluate` + `render`. A single small
diagram can skip the module loop.

## 4 · Positioning rules (hard requirements)

- NEVER hand-compute coordinates. Pick ONE anchor part (no `place`); chain the
  rest with `place: {rightOf|leftOf|above|inFrontOf|behind: <sibling-id>, gap: N}`.
  `gap` is in CELLS (1 cell = canvas `gridStep`, default 40). Arrange a
  container's children with `layout: {mode: row|column|grid|ring, gap: N}`.
- **Dense DAG (15+ nodes, multi-layer fan-in/fan-out)?** Add an engine to the
  auto container: `layout: { mode: auto, engine: elk }` (or `dagre`) — a real
  layered layout replaces the built-in heuristic and typically eliminates most
  crossings/tunnels. Offline and deterministic; falls back to the built-in
  path (with a warning) if the engine can't handle the graph.
- **Parts intentionally overlapping in projection** (stairs, stacked tiers,
  dense boards) and something paints in the wrong order? Set
  `canvas: { depthSort: true }` — parts then paint back-to-front by world
  depth instead of authoring order.
- A stair = each tile `{rightOf: prev, inFrontOf: prev}`. A board = one `group`
  with `layout: {mode: grid}`, `geom.w/d` omitted (auto-sizes). A ring =
  satellites on the anchor's four sides + four diagonals.
- `offset` is a fine-tune delta only; `offset.wz` lifts a part (the one axis the
  solver never sets). Sizes stay semantic: hero 150–220, standard ~90–120 with
  h 30–60, thin tiles h 8–20.
- Reuse styles with `theme.presets` (define `hero`/`tile`/`ghost` once, parts
  say `preset: tile`) instead of repeating style blocks.

Guides: `docs/guides/scene-design.md`, task→primitive `docs/agent/RECIPES.md`.

## 5 · Self-check loop, then render

```bash
isotopo validate scene.yaml     # legality — fix every error (exit 3) first
isotopo evaluate scene.yaml     # layout quality — JSON scorecard (plan + real iso routes)
isotopo render  scene.yaml ./out
```

- `validate`: issues are JSONPath-located with a `suggest` — apply it, re-run.
  Overlap warnings name the colliding pair → raise that `place` gap. **Never
  render at exit 3.**
- `evaluate`: aim for `crossings: 0` and `edges_through_nodes: 0`. A nonzero
  count means a connector tunnels a node or two edges cross — fix by routing the
  group's representative node (§3.4), nudging placement, switching the guilty
  edge to `routing: orthogonal2` (§2), or — for a dense DAG — re-laying the
  container with `layout: { mode: auto, engine: elk }` (§4). `render` also runs
  a repair pass by default that escalates tunnelling/crossing edges to
  `orthogonal2` on its own when that measurably improves the scene.
  `evaluate scene.yaml ./out` also writes `plan.svg` marking the problems red.
- **Aspect ratio:** read the rendered `topology.svg` `viewBox` (or `width`/
  `height`). If w:h is more lopsided than ~2.2:1 (or 1:2.2), re-grid the modules
  toward square before shipping.
- If you can rasterize (headless Chrome at the SVG's viewBox size), take ONE
  screenshot and check: nothing clipped, labels legible, one hero, on-grid
  connectors. Cap at ~3 renders per round — don't spin.

Once `validate`/`evaluate` are clean, do the visual self-review in §5.5 BEFORE
the §8 handoff — don't stop at a silent file write.

## 5.5 · Look at it — mandatory visual self-review BEFORE you ship

`validate`/`evaluate` prove the scene is *legal* and the *wiring* is clean; they
say NOTHING about whether it actually looks right. So before delivery you MUST
rasterize the SVG (headless Chrome at the `viewBox` size) and review the PNG
*with your own eyes*, as a designer doing QA — actually judge it, don't just
confirm it rendered. Walk BOTH axes below; every "no" is a fix-then-re-render,
not a ship. A diagram that fails either axis is not done.

**A · Colour sense — is the palette defensible, or does it break colour common sense?**
- **≤ 3 hues** (+ at most one accent). A 4th hue, or a new hue per node, reads as
  noise → collapse sub-tiers to light/dark shades of the SAME hue.
- **Every label + icon is legible on its OWN top face** — a clear lightness gap,
  never ink≈background. Light/pastel top → dark ink; dark/saturated top → light
  ink + `…/light` icon. A caption you can't read at a glance is a hard fail.
- **No clashing or vibrating pairs** (saturated red touching saturated green,
  neon-on-white), and no muddy near-greys pretending to be a colour. Prefer the
  domain's conventional hues (cloud blue, data green, security red, AWS amber…),
  not arbitrary picks.
- **Tiles separate from their tray and the canvas** — a node must not melt into
  the slab it sits on or the background (the validator's contrast warnings catch
  the worst of this — but your eye is the final judge).
- **One hero louder, the rest calm** — exactly one node carries the accent / glow;
  the field is not a rainbow of equals.

**B · Composition sense — do the proportions and structure read?**
- **Parent ⊃ child sizing is harmonious** — a group slab comfortably wraps its
  children with even padding: no child bursting out of its tray, no tray dwarfing
  one lonely node, no child nearly as large as its own parent. Disproportionate
  parent/child sizing is the #1 "looks off" tell — fix the child `geom` or the
  group padding/`gap`.
- **Sibling sizes are consistent** — same-role tiles share a size; only the hero
  is bigger, and on purpose. Nothing else is randomly out-sized.
- **Balanced, near-square canvas** — visual weight isn't dumped in one corner, and
  the modules aren't a long thin bar (re-grid toward square per §5).
- **Breathing room** — nodes and group slabs don't touch or crowd each other; the
  plan-view footprint check reports no overlap/touch.
- **Nothing clipped, alignment holds, connectors ride the grid** and don't skewer
  a node.

If anything fails, name what's wrong in plain product terms ("the storage tray is
swallowing its label"; "the two source tiles are different sizes"; "purple clashes
with the orange hero"), fix the palette / `preset` / `geom` / `place`, and
re-render. Cap at ~3 review rounds — converge, don't fiddle. Only a diagram that
passes BOTH axes proceeds to §8.

## 6 · Pitfalls — the things that actually go wrong

- **Faking a group with a bare rectangle.** To group nodes, use a real `group`
  with nested `parts:` + `layout` — it arranges and auto-wraps its children. A
  big rectangle placed `behind:` others does NOT contain or move them and won't
  resize. (`boundary` = dashed-outline container, same nesting.)
- **A top-face fill too close to its label/icon colour** → the caption/glyph
  vanish into the background. Keep a clear lightness gap (light top → dark ink,
  dark top → light ink / `…/light` icon). See §2.
- **Fanning every node in a group out** — or aiming an incoming edge at a deep
  child instead of the group. Wire group→group through representative/entry
  nodes (§3.4); intra-group detail stays inside. Wire-per-node is the #1
  ugliness.
- **More than 3 hues**, or a new hue per node. Same role → same hue, shades for
  sub-tiers, one accent for the hero.
- **Inventing an icon glyph name.** Icons are only `iso://si|glyph|brand/<name>`
  from the built-in catalog; unknown names are validation errors. If nothing
  fits, follow the fallback ladder in §2 (semantic glyph → icon-search → omit).
- **Referencing a non-existent id** in `place`/`connector`, or giving one node
  **two `place` relations** / duplicate `place`.
- **An illegal arrow/routing** (only `triangle`/`none`; `orthogonal`/`straight`/
  `bezier`) or a stray top-level key (`edges:`, connector fields inside `geom`).
  Top level is ONLY `canvas` / `theme` / `nodes`; `nodes` holds ONE `composite`
  scene.
- **A long-bar canvas** (re-grid toward square) or rendering before `validate`
  is clean.

## 7 · Voice & when to ask

Think out loud in PRODUCT language ("add a Redis cache between the gateway and
the services"), never expose YAML field names, validator diagnostics, or struct
names — the user sees a design tool, not the engine. If the canvas is empty and
the request has zero specifics ("make something cool"), ask ONE clarifying
question with 2–3 complete, directly-actionable options; otherwise infer
sensibly and proceed.

## 8 · Deliver & hand off — ALWAYS do this last

A render isn't done until you've told the human, in plain language, what was
produced and how to take it further. Never end with a silent file write. Close
every diagram with:

**a) Name the artifacts** (give the actual paths from your `render`/`evaluate`):
- `out/topology.svg` — the diagram (commit / embed this).
- `out/topology.html` — the diagram next to its editable source.
- `out/topology.<yaml|d2>` — the source copy.
- `out/nodes/` — a per-element gallery (`_index.html` + `<id>.svg/.html/.yaml`).
- `out/plan.svg` — if you ran `evaluate <in> out`, the flat layout with any
  crossings/overlaps marked.

**b) Open the live preview for the human.** Start Studio (it's a long-running
server — launch it in the background) and give them the URL:

```bash
isotopo serve scene.yaml      # → http://localhost:8731  (leave it running)
```

**c) Spell out the human-in-the-loop options** (this is where a person beats
round-tripping through you): in Studio they can **drag nodes to reposition**,
**drag on empty canvas to marquee-select several nodes** (shift-click adds or
removes; dragging any selected node **moves the whole selection together**, and
Delete removes it — each as a single undo step; pan with alt- or middle-drag),
**drag a node onto a group to move it in / onto the canvas to move it out**
(the target group highlights), **right-click to restyle / edit details**,
**toggle ◳ Iso / ◰ Plan**, **undo/redo**, and **export ↓ SVG / ↓ PNG / ↓ YAML**.
Studio edits live in an in-browser copy (the file on disk is never written), so
to keep changes they use **↓ YAML**. You can't drive Studio yourself — surfacing
it is the handoff. Guide: `docs/guides/studio.md`.

Speak in product terms ("your diagram's at out/topology.svg; I've opened the
live editor at localhost:8731 — drag things around or restyle there, and hit
Download YAML to keep edits"), not engine internals.

## Minimal scene template

```yaml
canvas: { background: "#F5F6F8", grid: iso, gridColor: "#E8ECF2", gridStep: 40 }
theme:
  text: { family: "Inter, system-ui, sans-serif", size: 10, weight: "600", color: "#3F3F46" }
nodes:
  scene:
    shape: composite
    parts:
      - id: hero
        shape: rectangle
        geom: { w: 170, d: 170, h: 24 }
        icon: "iso://glyph/sparkles/7C5CFC"
        label: "Core"
        style:
          palette:
            top: "#FFFFFF"
            leftGradient:  { from: "#7C3AED", to: "#4C1D95", dir: "down" }
            rightGradient: { from: "#8B5CF6", to: "#5B21B6", dir: "down" }
          effects: { cornerRadius: 14, backglow: { color: "#A78BFA", radius: 46, opacity: 0.4 } }
      - id: store
        shape: cylinder
        place: { rightOf: hero, gap: 2.5 }
        geom: { w: 118, d: 118, h: 40 }
        icon: "iso://glyph/database"
        label: "Store"
        style: { palette: { top: "#C084FC", left: "#9333EA", right: "#7E22CE" } }
    connectors:
      - { from: hero, to: store, routing: orthogonal, arrow: triangle, stroke: { color: "#8B5CF6", width: 1.2 } }
```

## References (fetch on demand, don't preload)

- Full DSL grammar: `docs/reference/dsl-yaml.md`
- Task → primitive mapping: `docs/agent/RECIPES.md`
- Icon index: `docs/agent/ICONS.md` · Machine schema: `docs/agent/schema/dsl.schema.json`
- Layout scorecard / `evaluate`: `docs/reference/cli.md`
- MCP alternative to shelling out: `docs/agent/MCP.md`
```
