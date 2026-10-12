---
title: "YAML and JSON"
description: "The composition language for shapes, groups, placement, and connections."
---

The YAML DSL gives you **designer-grade iso composition** — styled
boards, hero scenes, stairs, infographics — without hand-computed
coordinates: parts are positioned **declaratively** with `layout`
(container arrangement) and `place` (sibling relations), and the
solver turns relations into world coordinates. Explicit
`(wx, wy, wz)` offsets remain available as a fine-tune escape hatch.

For auto-layout from a textual graph, see [DSL_D2.md](../reference/dsl-d2.md)
instead.

JSON is also accepted (same shape, JSON encoding); the parser
sniffs the leading byte. We document YAML below.

## Document structure

```yaml
canvas:        # optional — page-level backdrop
  ...
theme:         # optional — document-wide default style
  ...
nodes:         # required — map of node id → Node
  scene:
    shape: composite
    parts:
      - ...
    connectors:
      - ...
annotations:   # optional — screen-space callouts
  - ...
```

Every full scene typically has exactly one node named **`scene`** with
`shape: composite`. That node holds all the visible parts. The library
also accepts a single-node document with any name — see
[USAGE.md](../reference/cli.md) for `doc.Scene()`.

## Canvas

```yaml
canvas:
  background: "#FAFBFC"   # solid color behind the scene
  grid: iso               # iso | dots | hatch | solid | none
  gridColor: "#E2E6EE"    # pattern stroke / dot color
  gridStep: 40            # tile size in world units (default 40)
  projection: iso         # iso (default) | top
```

`grid: iso` draws a dashed-diamond ground plane that aligns with the
iso projection — gives the scene a sense of place. Skip the field
entirely to get a transparent background.

`projection: top` renders a flat **top-down plan view** instead of the
2.5D isometric one — each part as its footprint rectangle with
orthogonal connectors, height dropped. Use it to read the layout/flow as
a normal 2D diagram. Default (`iso`, or omitting the key) is unchanged.
The CLI `--projection` flag and the Studio view toggle override it
per-render without touching the source.

`gridStep` doubles as the **cell** unit used by `layout`/`place`
gaps, so arranged parts and orthogonal connectors land exactly on
the grid texture.

## Theme

A theme is a default `Style` plus optional per-shape overrides:

```yaml
theme:
  palette:                    # default face colors for every part
    top:   "#FFFFFF"
    left:  "#7E94B5"
    right: "#A6B7D2"
  stroke: {color: "#1F2937", width: 1.2}
  text:   {family: Inter, size: 12, weight: "600", color: "#1F2937"}

  shapes:                     # per-shape-type overrides (optional)
    cylinder:
      palette: {top: "#FFC", left: "#A82", right: "#D04"}
    person:
      palette: {top: "#EBC8A9", left: "#8E673E", right: "#A77F58"}
```

Resolution order for any part: **theme.Style → theme.shapes[shape] →
part.Style** (each step overrides matching fields, nils inherit).
Full Style field reference: [DSL_THEME.md](../reference/dsl-theme.md).

## Node

A `Node` is the top-level element. The most useful shape is
`composite` (a container holding `Parts` + optional `Connectors`):

```yaml
nodes:
  scene:
    shape: composite
    parts:
      - {id: a, shape: rectangle, geom: {w: 100, d: 100, h: 30}, label: A}
    connectors:
      - {from: a, to: b, arrow: triangle}
```

A non-composite Node (just `shape: cylinder` etc.) renders a single
iso element — convenient for sticker-sheet style outputs.

## CompositePart

One entry inside `nodes.scene.parts`:

```yaml
- id: api
  shape: rectangle
  geom:
    w: 140       # iso world X extent
    d: 90        # iso world Y extent (depth into screen)
    h: 30        # iso world Z extent (extrusion height)
  place:
    rightOf: gateway   # position = solved from a sibling relation
    gap: 2             # in cells
  label: "API Gateway"
  icon: "iso://brand/kafka"    # iso:// ref, http(s) URL, data: URI, or a
                               # local image path (./logo.svg — inlined at render)
  style:
    palette: {top: "#3B82F6"}
    text:    {color: "#FFFFFF"}
```

Sizes (`geom`) are explicit and semantic — heroes big, tiles small.
Positions come from `place`/`layout` (next section); an `offset:
{wx, wy, wz}` field is applied as a **delta on top** of the solved
position (or as the absolute position when no relation is given —
legacy documents render unchanged).

The world axes form a left-handed iso frame:
- **wx** runs to the right and down (after projection) — `rightOf`
  moves along +wx,
- **wy** runs to the left and down — `inFrontOf` moves along +wy
  (toward the viewer),
- **wz** is vertical screen up.

For a beginner, just remember: `behind` = away from the viewer
(smaller wy), `inFrontOf` = toward the viewer (bigger wy), and
bigger wz = higher off the ground.

## Shapes

Native iso shapes (see `isotopo capabilities`):

| Shape | Looks like |
|---|---|
| `rectangle` | iso box with top/left/right faces |
| `cylinder` | iso cylinder; also covers `queue`, `stored_data` |
| `circle` | round disc with depth |
| `cloud` | rounded blob; flat top with depth |
| `person` | cylinder body + round head |
| `iso_text` | low-extrusion label panel |
| `composite` | container; needs `parts` |
| `group` | composition primitive — see below |

## Composition primitives

### `place` — position relative to a sibling

The default way to position free-standing parts. One constraint per
ground axis; the solver resolves chains topologically:

```yaml
- id: gateway
  shape: rectangle                       # no place → anchors the scene
  geom: {w: 100, d: 100, h: 90}
- id: api
  shape: rectangle
  place: {rightOf: gateway, gap: 2}      # 2 cells to gateway's +x side
  geom: {w: 120, d: 80, h: 30}
- id: s2
  shape: rectangle
  place: {rightOf: api, inFrontOf: api, gap: 0}   # corner-to-corner stair
  geom: {w: 120, d: 80, h: 30}
```

| Field | Meaning |
|---|---|
| `rightOf` / `leftOf` | pin world x to the sibling's +x / −x side (mutually exclusive) |
| `inFrontOf` / `behind` | pin world y toward / away from the viewer (mutually exclusive) |
| `above` | pin world z — sit flush ON TOP of the sibling, centred on its footprint unless x/y are also pinned |
| `gap` | distance from the sibling's footprint, in cells (default 1) |
| `gapX` / `gapY` | override `gap` per ground axis |
| `align` | `start` \| `center` \| `end` — alignment along the unconstrained axis (default center) |

References must be **siblings** (same `parts:` list). Dangling refs
and cycles are validation errors; if the solved scene still has
overlapping siblings you get a warning naming the exact pair.

### `layout` — auto-arrange a container's children

On a `group` (or the root composite). Children need no positions at
all; a layout group's substrate **auto-sizes** around the content,
so its `geom.w/d` may be omitted:

```yaml
- id: board
  shape: group
  label: "Dashboard"
  geom: {h: 20}                          # thickness only
  layout: {mode: grid, cols: 3, gap: 0.5, padding: 0.7}
  parts:
    - {id: c1, shape: rectangle, geom: {w: 90, d: 90, h: 4}, label: test}
    - {id: c2, shape: rectangle, geom: {w: 90, d: 90, h: 4}, label: dev}
    # …
```

| Field | Meaning |
|---|---|
| `mode` | `row` (world +x) \| `column` (world +y) \| `grid` (row-major wrap) \| `ring` (first child = hub at the centre, rest on a circle) |
| `cols` | grid only; default ceil(√n) |
| `gap` | space between children, in cells (default 1) |
| `padding` | content inset from the container edge, in cells (defaults to gap) |
| `align` | cross-axis alignment within each track (default center) |

### `group` — labeled translucent container

Wrap N parts in a translucent rounded substrate with a corner label.
Position children with `layout` (above) or per-child `place`; the
substrate auto-sizes around the solved content:

```yaml
- id: cluster
  shape: group
  label:  "Kubernetes Cluster"
  layout: {mode: row, gap: 1.5}
  parts:
    - {id: api,    shape: rectangle, geom: {w: 120, d: 80, h: 30}}
    - {id: worker, shape: rectangle, geom: {w: 120, d: 80, h: 30}}
```

Groups nest unboundedly — a group inside a group inside a group
works; inner groups are solved first so outer layouts see their
final footprint. Hand-positioned children (`offset` relative to the
group, no auto-size) remain supported for legacy documents.

### `stack` — auto-replicate N times

Stamp a part N times vertically with a Z-gap. The bottom copy keeps
the declared id; layers above are suffixed `~1`, `~2`, …:

```yaml
- id: pods
  shape: rectangle
  geom: {w: 110, d: 110, h: 36}
  stack: {count: 3, gap: 10}     # 3 layers, 10 units apart in Z
  label: "Pods"
```

Connectors and annotations can target a specific layer:

```yaml
- {from: ingress, to: pods}      # bottom layer
- {from: ingress, to: pods~2}    # top layer
```

If `gap` is omitted, the renderer defaults to `part.geom.h + 4` so
layers visually sit atop each other.

### `connectors` — directed lines between parts

```yaml
nodes:
  scene:
    shape: composite
    parts: [...]
    connectors:
      - from: api               # part id (or "id.anchor" — see below)
        to:   db
        routing: orthogonal     # straight (default) | orthogonal | bezier
        arrow:   triangle       # none (default) | triangle
        label:   "query"
        stroke:  {color: "#1F2937", width: 1.2, dash: "4 3"}
```

For a measured multiline label, set `labelFontFamily: Source Sans Pro`.
`labelFontWeight` supports `400`, `600` (default), `700`, `normal`, and `bold`;
`labelFontSize` defaults to 13 in iso and 10 in top. The embedded resource is
shared by measurement and SVG/PNG output. Explicit newlines, blank lines and
spaces are preserved when the entire block is supported. Other families or
unsupported scripts retain estimated browser-font geometry. Unset font fields
preserve legacy output; an explicit size is now also honored in top projection.

Measured pills are placed after all routes and arrows are known. The whole
previous AABB arrangement is retained first. Already-clear labels keep their
exact anchors; blocked labels search continuous intervals on their own path
while protecting every other label's current box. Body and arrow bounding
boxes remain conservative. Other routes use swept stroke envelopes that
include round caps/joins and a bounded quadratic approximation error,
releasing the empty corners of diagonal route boxes. An already-safe position
keeps its existing exact curve-bounds certificate, including when the curve
subdivision cap makes the new envelope more conservative. Clearance is 6 SVG
units. Substrates are excluded as body obstacles; their content,
effects and arbitrary-font ink are outside this clearance scope. Quadratic
labels stay on the submitted curve. Initial placement is deterministic and
greedy. If individual refinement finds no feasible interval, the
full text keeps its baseline anchor (the path midpoint when the baseline
search itself failed). Final status is checked against the whole arrangement;
`render --report` records a
`connector-label-placement` warning for every affected measured label. Increase spacing or adjust the route;
the renderer never silently shrinks or truncates the text to make it fit.

For eligible iso scenes, an additional pass can release empty corners of body
bounding boxes. It combines filled shape faces, actual stroke widths and
curve error with measured node text and icon bounds; concave notches remain
usable where no real outline crosses them. Only still-blocked labels move,
and every peer pill, foreign route and arrow remains protected. Successful
labels declare a `body-region,content-quad,...` scope in the report. This also
protects measured substrate content, while allowing labels on substrate fills.
The extra pass requires bounded content throughout the scene; unknown fonts
or shapes, screen labels, annotations, legacy connector pills and unsupported
effects retain the previous layout. Person/circle captions and cloud labels
without icons now use embedded Source Sans Pro metrics for complete supported
blocks at weights 400, 600 and 700. These captions preserve authored
newlines and font size, expanding the export canvas when needed; this does not
certify that a long caption fits inside its node or avoids other objects.
Cloud captions without icons keep their original centroid; clouds with icons
retain the existing bounded face layout. A partially unsupported block still
falls back. See the [API clearance contract](./api.md#measured-connector-labels)
for the exact scope and fallbacks. No route, font size or authored text is
changed by this refinement. The optional pass has bounded search work; if its
budget runs out, unresolved labels retain their prior position and status.

A subsequent bounded joint pass can release labels that block each other's
available slots. Already-clear labels stay fixed. Still-blocked labels may share
a corridor by choosing different safe positions on their existing routes;
every changed label must clear all final peer positions, including labels that
remain unresolved. It preserves node/route geometry and full text, and retains
only fully checked assignments if the work budget runs out. Its finite choices
do not guarantee a globally optimal packing. Unknown iso content/effects, legacy
pills, and plan node titles using ambient fonts retain the previous arrangement.

`render --report` identifies the body, route, arrow or peer label involved in
each measured blockage. Its structured `typography.conflicts` list retains
connection indexes (including parallel edges) and a conservative
`clearance_shortfall` in SVG units. This measures intrusion into the declared
6-unit safety gap; it is not pixel overlap area or a complete relocation
solution. Routing repair cannot trade an existing blockage for a new
counterpart or deepen its measured shortfall merely because total readability
cost decreases. Detailed lists are collected for reports and repair candidates;
ordinary `RenderScene` previews keep the same geometry without that payload.


`orthogonal` bends along the iso ground axes (every segment projects
to exactly ±30°, in register with `canvas.grid: iso`) — prefer it
for architecture flows. `bezier` draws a single soft quadratic arc —
reads as async/replication traffic.

**Manual routing** (usually written by the Studio edge-drag, but valid
to hand-author): an orthogonal connector may carry explicit interior
corners so the line takes a path you choose while both endpoints stay
docked to their nodes:

```yaml
      - from: api
        to:   db
        routing: orthogonal
        waypoints: [{wx: 120, wy: -40}, {wx: 120, wy: 60}]   # world corners, source→target
```

`waypoints` is a list of interior corner points in world coordinates
(source → target); the route threads through them and is kept
iso-axis-aligned. `bend: {wx, wy}` is the older single-corner form
(a world offset applied to the auto route's interior); `waypoints`
supersedes it.

For a route that preserves **height**, use `waypoints3d` (Studio segment
drags now write this field):

```yaml
      - from: api.port-right
        to: db.port-left
        routing: orthogonal2
        waypoints3d:
          - {wx: 120, wy: 40, wz: 0}
          - {wx: 120, wy: 40, wz: 80}
          - {wx: 200, wy: 40, wz: 80}
```

These are interior **absolute world XYZ** positions, in source-to-target
order; an omitted coordinate means zero, including `wz`. Endpoints are
resolved from `from` and `to` at their current heights. When consecutive
points differ on multiple axes, the route inserts bridges in X, then Y,
then Z order. Authored corners are retained. Plan view uses the XY
projection of the same intent and collapses vertical legs.

A nonempty `waypoints3d` overrides `waypoints`, `bend` and automatic
routing. The old fields retain their original semantics and can remain in
the source; removing `waypoints3d` restores that earlier intent. This field
is active with `orthogonal` or `orthogonal2`; switching to straight or
Bezier keeps the saved corners inactive and reports a warning, so switching
back restores them. It allows at most 512 interior corners and finite
coordinates within the geometry bounds. Explicit routes can cross
bodies; choosing corners is not an obstacle-clearance guarantee.

Legacy anchor selectors include `api.right-mid`, `api.top`, and
`api.front`. Orthogonal routes choose a facing side for bare IDs;
straight and bezier routes start from the default top centre.

For a fixed visible attachment, use `api.port-left`, `api.port-back`,
`api.port-right`, or `api.port-front`. Studio creates these references
with `orthogonal2` routing. The renderer and editor resolve the same
body boundary, and fan-out never moves the selected endpoint. In plan
view, the same names attach to footprint-side midpoints. A stack instance
remains addressable, for example `workers~2.port-right`; plan view shows
its connection on the collapsed stack footprint.

Fixed ports describe projected contour attachments, not physical 3D face
centres. Array/rack shapes currently use a semantic envelope; text-only
parts use legacy anchors. A forced straight line, curve, bend or waypoint
can still cross a body: preserving an endpoint does not prove route
visibility or obstacle clearance. Multiple edges on one fixed port share
a junction; use bare IDs when automatic endpoint distribution is desired.

### `annotations` — screen-space callouts

Document-level field (NOT inside a node):

```yaml
annotations:
  - anchor: api                  # id of a part to point at
    side:   right                # top | right | bottom | left
    distance: 60                 # world-unit gap from part silhouette
    text: "p95 < 100ms\nregion edge"
    bg:     "#FFFFFF"
    border: "#1F2937"
    fontSize: 11
```

The renderer draws a rounded text box with a dashed leader line back
to the anchor part. Multi-line text is supported via `\n`.

Annotation boxes and `orient: screen` labels are
**collision-managed**: they never cross or touch a part's projected
silhouette (6px clearance), never overlap each other, and when the
requested spot is occupied the search escalates distance WITHIN the
preferred direction first (so sibling labels keep a consistent
relative position) before falling to the next side, peripheral sides
first. The leader line starts on the silhouette edge FACING the box —
never across the node's own body. `side`/`distance` are treated as
the preferred candidate, not an absolute position.

## Worked example

Note: not a single hand-written coordinate.

```yaml
canvas:
  background: "#FAFBFC"
  grid: iso
  gridColor: "#E2E6EE"
  gridStep: 40

theme:
  palette: {top: "#FFFFFF", left: "#7E94B5", right: "#A6B7D2"}
  stroke:  {color: "#1F2937", width: 1.2}
  text:    {family: Inter, size: 12, weight: "600", color: "#1F2937"}

nodes:
  scene:
    shape: composite
    parts:
      - id: cluster
        shape: group
        label:  "Cluster"
        layout: {mode: row, gap: 1.5}
        parts:
          - id: api
            shape: rectangle
            geom:  {w: 120, d: 80, h: 30}
            label: "API"
          - id: pods
            shape: rectangle
            geom:  {w: 100, d: 100, h: 30}
            label: "Pods"
            stack: {count: 3, gap: 10}
      - id: ingress
        shape: cloud
        place: {behind: cluster, gap: 2}
        geom:  {w: 120, d: 80, h: 24}
        label: "Ingress"
    connectors:
      - {from: ingress, to: cluster, routing: orthogonal, arrow: triangle}
      - {from: api, to: pods, routing: orthogonal, arrow: triangle}

annotations:
  - anchor: pods~2
    side:   right
    text:   "3 replicas"
    fontSize: 11
```

Render with:

```bash
isotopo render scene.yaml ./out
open ./out/topology.html
```

## Round-trip-able per-part fragments

For every CompositePart with an `id`, the CLI emits
`out/nodes/<id>.yaml` — a self-contained Document with just that one
part. Drop it back into `isotopo render` to get the standalone iso of
that single element. Useful for building icon libraries from a scene.

See [OUTPUTS.md](../reference/output-layout.md) for the full output structure.

## Validating before render

```bash
isotopo validate scene.yaml
```

Catches: unknown shape names ("did you mean cylinder?"), annotation
anchors that don't resolve, missing connector targets, bad grid mode
names — plus the layout solver's dry run: dangling `place` refs
(with nearest-id suggestion), `place` cycles, conflicting
constraints, and post-solve sibling overlaps (warning naming the
exact pair and overlap size). JSON output with paths is ready for
agent self-correction. Exit codes: 0 clean, 2 warnings only, 3
errors.
