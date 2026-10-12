# API reference

The single entry point for every programmable surface of iso-topology. Pick the
surface you need — each row links to its authoritative contract; nothing is
documented in two places.

| Surface | What it is | Contract |
|---|---|---|
| **DSL** (input format) | the YAML / JSON / d2 you author or an agent generates | [CAPABILITIES.md](../agent/CAPABILITIES.md) (generated) · [dsl.schema.json](../agent/schema/dsl.schema.json) (generated) · [YAML](dsl-yaml.md) · [d2](dsl-d2.md) · [Style/Theme](dsl-theme.md) |
| **Go library** | import the engine — parse, render, validate, **and edit** DSL | **this page (below)** · generated godoc on [pkg.go.dev](https://pkg.go.dev/github.com/MarkovWangRR/iso-topology) |
| **HTTP API** | the Studio server's `/api/*` endpoints | [studio.md → Endpoints](../guides/studio.md#endpoints-for-tooling) |
| **CLI** | the `isotopo` command | [cli.md](cli.md) |
| **MCP** | tool server for Claude / Cursor / any MCP client | [MCP.md](../agent/MCP.md) |

> The **canonical, always-current** Go API is the godoc on
> [pkg.go.dev/github.com/MarkovWangRR/iso-topology](https://pkg.go.dev/github.com/MarkovWangRR/iso-topology)
> — it is generated from the source and never drifts. The sections below are the
> hand-written tour.

---

## Go library

Module path: `github.com/MarkovWangRR/iso-topology`

```bash
go get github.com/MarkovWangRR/iso-topology@latest
```

Three things you can do with the library:

1. **Render** a DSL document to SVG — [Quick example](#quick-example-parse--render)
2. **Edit** a DSL document by applying direct-manipulation ops (drag, set field,
   add/delete/duplicate) while preserving comments — [Editing for embedders](#direct-manipulation-editing-for-embedders)
3. **Introspect** the DSL contract — `CapabilityReport()`

### Quick example (parse → render)

```go
package main

import (
    "fmt"
    isotopo "github.com/MarkovWangRR/iso-topology"
)

func main() {
    yaml := []byte(`
canvas: {background: "#FAFBFC", grid: iso}
nodes:
  scene:
    shape: composite
    parts:
      - id: api
        shape: rectangle
        geom: {w: 120, d: 80, h: 30}
        label: API
      - id: db
        shape: cylinder
        offset: {wx: 200, wy: 0}
        geom: {w: 100, d: 100, h: 36}
        label: DB
    connectors:
      - {from: api, to: db, arrow: triangle}
`)
    // One-shot: validate + render any DSL text (yaml | json | d2) to one SVG.
    svg, issues, _ := isotopo.RenderSource("yaml", yaml)
    for _, i := range issues {
        fmt.Println(i.Severity, i.Path, i.Message)
    }
    fmt.Print(svg)
}
```

`RenderSource` is the convenience entry. For finer control (intercept a stage,
render per-part, pick the layout engine) use the pipeline functions in
[Public API surface](#public-api-surface).

### Consume the emitted scene

`RenderScene(doc)` returns a `SceneResult` with `SVG`, `Routes` and
`ScreenLabels`. Connector records use the contract below. Screen-label records
are collected during the same render, after obstacle placement, using the
emitter's decimal precision. Collection does not change the SVG bytes or rerun
label placement. This is a partial shared result: isometric body geometry and
top-face budgets still have separate resolvers.

`RenderSceneWithInteraction(doc)` additionally populates `SceneResult.Model`
(`model`, omitted from ordinary renders). Use this entry when displaying an
editable preview. In top projection, resolved footprints, accepted routes,
the coordinate frame and final title/connector-pill arrangement are shared by
SVG emission and the world interaction model. A content arrangement already
certified by route refinement is reused. The isometric interaction model
retains its established resolver. Studio render/edit responses use this
combined entry without changing their HTTP fields.

Reuse is confined to a single request, with no cache keyed by a mutable input
document. Separate calls own their models and nested selection/port slices;
an edited document is resolved anew. `BuildInteractionModel(doc)` remains
available for geometry-only callers and preserves the fast path for plan
scenes without bound titles. Plain `RenderScene` does not compute an extra
interaction model.

`RenderSceneWithReport(doc)` returns both the scene and a `RenderReport`, sharing
the emitted screen-label records. `BuildRenderReport(doc)` uses that entry point
when only the report is needed; other diagnostics retain their existing
resolvers and score definitions. Caption repair simulation is requested only
when a reported caption occlusion needs a padding patch. The CLI applies `--projection` before rendering and diagnostics,
and `--report` writes labels from the same render as `topology.svg`.

Each `ScreenLabel` describes one emitted label in layer order:

- `PartID` is the emitted instance identity, including any replica suffix.
  Anonymous labels retain an empty ID. `PartIndex` is the lowered geometry
  index, not an authored array index or a stable identifier across edits.
- `Bounds` is the submitted transparent pointer rectangle, in final SVG user
  coordinates before browser viewBox/CSS transforms and live drag. It does not
  include the chip's border stroke. Consumers must apply the current label
  group's screen transform when working in CSS pixels.
- `FontFamily`, `FontWeight` and `FontSize` describe the emitted font request.
  A fully bound block has `TextMetrics: "shaped-ink"` and a `FontHash` matching
  the embedded resource. Its `Lines` contain the emitted text, horizontal
  anchor, baseline, advance and optional glyph `InkBounds`. An empty line has
  zero advance and no ink bounds. Glyph boxes describe geometry, not a
  visibility mask or a readability guarantee.
- An unbound or unsupported block retains `TextMetrics: "estimated-advance"`.
  Its legacy single text node is recorded without invented advance, ink or
  font hash; normal SVG whitespace collapsing still applies. The browser's
  actual glyph envelope may exceed the estimated pointer rectangle.

The Go JSON field is `screen_labels`; `SceneResult` emits an authoritative
empty array for an absent isometric screen-label layer, including the top
projection. This does not mean that other text layers are absent. The report
omits `screen_labels` when empty. Studio's HTTP responses use `screenLabels`
and always include the array on a successful render. Separate renders own
their records; the scene and report returned together share the label slice.

### Observe content fitting

`ContentFits(doc)` observes the final top-face content layout after resolving
layout, groups, replicas, style, wrapping and row/column selection. It returns
deterministically ordered `ContentFitObservation` records without mutating
the input. `Path` locates the authored part when its ID is unambiguous;
`PartID` retains the expanded instance ID, and `PartIndex` is the index after
lowering (not the source array index). Anonymous or ambiguous instances fall
back to the owning node path and do not imply an automatic source patch.

Each record's `Fit` has status `estimated-fit`, `estimated-overflow`,
`unsupported`, or `empty`. Empty observations are omitted from `ContentFits`.
The fit describes the preferred padding and supported ink-separation budgets
in local face coordinates.
`Items` contain final text, font size, text/image viewport bounds,
and overflow on the left, top, right and bottom. `Manual` identifies retained
authored icon placement. A budget violation can mean insufficient padding
even when the content remains inside the physical face. It is not a proof of
glyph clipping, readability, opaque pixels, or correct font fallback.

For top-face labels explicitly using `Source Sans Pro` at weight `400`,
`600`, or `700`, `text_metrics: shaped-ink` identifies glyph ink bounds from
pure Go shaping of the exact embedded font; `font_hash` is its SHA-256.
Horizontal Latin/Greek/Cyrillic and supported punctuation/combining marks
are measured as complete runs, including kerning and ligatures. A line with
missing glyphs, unsupported script/control characters, or an unbound font
retains `text_metrics: estimated-advance`. `ProjectedContent.EstimatedText`
is true if any emitted text line is estimated. Bounds remain bounding boxes,
not glyph masks; SVG rounding, transforms and raster antialiasing still apply.
The existing fit status names remain conservative and unchanged.

Composite rendering records embedded fonts at their first actual use and
shares each immutable resource across subsequent parts in that render. This
avoids generating a full font payload for every part before removing duplicates.
Resource collection is local to the render; independent and concurrent renders
do not share emission state. Public standalone shape renderers and
`Convert2DTo25D` still return self-contained SVGs. Resource collection preserves
the first definition's position, geometry, typography, and serialized output.

For bound-font text, `Gaps` records unmet separation constraints. Each entry
contains `before` and `after` element indexes, `kind` (`text:text` or
`icon:text`), and `required`, `actual`, and `shortfall` distances. Adjacent
visible text lines require a vertical ink gap of 0.12 times their larger font
size; icon/text boxes require at least 4 local units along either axis.
Negative `actual` means overlap along the measured separation direction.
Shortfalls over 0.01 local units produce `estimated-overflow`, including when
all items satisfy the outer padding. Image bounds describe the viewport, not
opaque icon pixels. These are local layout preferences, not guarantees of
readability at a particular zoom. Unbound text does not certify ink gaps.

`iso25d.ShapeContent(shape, options).Fit` exposes the same observation next to
the projected elements; item indexes refer to those elements. Free captions,
spheres and other layouts without a bounded top-face budget remain explicitly
unsupported. The separate top projection has no isometric observations.
Screen-oriented labels are removed before observing their composite part's
icon-only face layout. Known fully transparent text is omitted from fit items.

Screen-label rendering separately uses bound glyph geometry when the entire
label supports the selected built-in font. It shares that geometry between
text baselines, background, pointer hit rectangle, obstacle reservation and
export bounds, and embeds the same font once per weight. This does not add
screen labels to `ContentFits`: its public records still describe bounded
top-face budgets. Unknown/mixed fallback labels retain their estimated path.

After placing all screen labels, a bounded compaction pass can move displaced
captions closer to their owners. It retains the chosen side and alignment,
reserves every other label, and checks the serialized rectangle against body,
content, submitted stroke and arrow envelopes. A move must improve the owner's
box distance by more than one SVG unit and cannot reduce any peer's relative
association margin (distance to peer minus distance to owner), allowing 0.01
SVG unit for coordinate rounding. It stays inside the prior label/owner
envelope with the same tolerance. The untouched axis retains its original
text anchor, avoiding a one-decimal-step shift from rounded rectangles. This is a
geometric association heuristic, not a proof of semantic clarity.

The pass makes at most four deterministic sweeps. Unknown screen blocks,
unmeasured top-face or connector text, annotations, structural text, face
surface layers and blur/shadow/glow effects retain the established arrangement
until their full envelopes are available. Clean preferred captions keep their
position. Rectangle caching and reusable scratch space bound allocation;
the pass does not rearrange nodes, change routes, resize text or displace a
peer to make room. It is a local refinement, not a global label assignment.

`ContentFitIssues(doc)` reports estimated overflow as warnings and is included
in `Validate`. `BuildRenderReport` adds `content_fits` and corresponding
`content-fit` defects without suggesting an unverified automatic patch.
The shared visual candidate guard protects each instance, text/icon kind,
violated side, and indexed gap separately, so reducing one violation cannot
pay for another. Gap indexes describe the final layout's elements; they are
not stable authored line identities across arbitrary reflow.
These observations are separate from the legacy scalar readability score;
a high score does not clear a reported content-fit defect or certify readability.

### Observe connector geometry

`RenderWithRouteInfo(scene, theme, canvas, annotations)` returns the same SVG
as `RenderWithCanvas`, plus per-connector records for an isometric composite.
It does not change the input document. Non-composites and the top projection
return no isometric route records.

- `World` and `Screen` retain the existing **pre-clip** edit/scorecard tracks.
  `World3D` adds the corresponding heights. For Bezier samples, the height
  interpolates between anchors; this is a declared lifting convention, not a
  physically solved cable.
- `Drawing.Points` contains the **submitted** path after endpoint trimming,
  clipping and coincident-path reuse, at SVG decimal precision. `Kind` is
  `polyline` or `quadratic`; the latter has start, control and end points.
- `Drawing.Provenance` records the source of each submitted segment as the
  emitter transforms it. `SourceSpace` is `preclip-polyline` (segment indexes
  into `World3D`) or `preclip-quadratic` (the original analytic curve, segment
  zero, **not** the sampled `World3D` polyline). It does not index authored
  waypoints. For `Kind: source`, final parameter `t` maps to
  `Source.T0 + t * (Source.T1 - Source.T0)` on `Source.Segment`.
  `World` contains two endpoints or three quadratic controls. The curve uses
  the same linear-height convention as the pre-clip samples.
- A provenance segment with `Kind: derived` records a new terminal shortcut
  chord and its input `Ancestors`; those inputs are ancestry, not an exact
  parameter mapping. `Kind: unresolved` retains ancestry and a `Reason` but
  omits world geometry, for example when coincidence reuse displaces the
  submitted path. Identical rounded projections retain their own heights.
  For segments with `World`, `ProjectionError` bounds the difference from
  the rounded submitted path in SVG user units. It is not a physical routing
  or pixel-visibility guarantee.
- `SourceRef` / `TargetRef` retain logical endpoints; `ResolvedSource` /
  `ResolvedTarget` retain the attachment references actually used. These may
  differ for promoted cross-group orthogonal connections.
- `Drawing.Attachments.Source` / `.Target` make the endpoint contract explicit:
  logical reference and instance, resolved reference/instance/anchor, submitted
  `Point`, final `RouteSegment` and `RouteT`. `VisualRule` is `fixed-contour` or
  `legacy-silhouette-trim`. `World` has the `producer-route-lift` convention,
  a `known` point or an `unresolved` status with a reason. This world point
  belongs to the final route, after trimming; it does not certify a physical
  interface on the owner's surface. Returned points do not alias the source.
- `PaintBeforePart` is the actual insertion position among rendered parts.
  `Arrow` is the submitted overlay polygon. `Label` records the pill rectangle
  and text anchor, not measured glyph bounds.
- `Drawing.Coverage` partitions each submitted segment by later opaque body
  fill coverage. `OpaqueParts` lists covering instance IDs in painter order
  (`#part:N` identifies an unnamed rendered part). `UnresolvedParts` records
  uncertain coverage, including unsupported envelopes or materials.

Coverage uses the scope `opaque-body-fill-centerline`. Empty coverage does
**not** certify pixel visibility: stroke width, dashed gaps, text, icons,
filters, translucent compositing and later overlays are outside that scope.
Curved body polygons carry `BoundaryError` in SVG user units, including a
0.03-unit coordinate-rounding allowance. The observation follows the current
painter order; it does not prove that order is physically correct in 3D.
Keep the pre-clip track when editing a route. `Drawing.Editable` obtains its
source indexes from `Drawing.Provenance` and excludes overlap with other
submitted segments. Removed pre-clip legs cannot veto an edit. Missing,
derived, unresolved or inconsistent source mappings are not editable.
Provenance alone does not make an overlapped segment visually distinguishable.

The internal depth experiment also records `Spatial.SubmittedCollision` for
supported polylines and opaque box scenes. Its scope is
`submitted-world-centerline`, using the `producer-route-lift` convention:
final source segments and derived chords are tested in their own world
geometry, after clipping and simplification. `Segment` indexes the final
polyline and `T0`/`T1` bound its interval inside an open box. Face contact and
isolated tangency do not count; entering an endpoint's own body does count.
Only `Status: complete` with empty `Intervals` means no interior occupancy in
the queried boxes. Missing or unsupported provenance is unresolved, never
equivalent to clear. The compositor publishes observations atomically with
the SVG; a legacy fallback omits this report. Historical `Spatial.Penetrations`
still indexes the pre-clip `World3D` track, so the two results may differ.
This does not certify physical surface attachments, cable thickness, curves,
or pixel visibility. Fixed contour ports retain their screen attachment and
base-height lift. The experiment remains disabled in public rendering.

Within that experiment, `Spatial.Arrow` gives a direction marker's owner,
terminal segment, depth plane and guarded clip region. The convention is
`terminal-tangent-billboard`: depth follows the last segment's tangent and is
constant across it. The plane uses coordinates relative to `ProjectionOrigin`.
Ownership does not exempt the endpoint's body from occlusion. Emitter-owned
markup offsets associate each arrow with its edge, including identical polygons
at different world heights; the original polygon, alpha and gradient are drawn
once. Missing/degenerate terminal geometry or inconsistent markup makes the
whole experiment fall back atomically. Labels retain their existing overlay
policy. Round stroke caps occupy only their outside half-disks: constant
endpoint depth must not leak back over the sloping segment interior. Surface
antialiasing at mixed-depth interfaces, general body ordering and mixed-shape
locality still require further work before public enablement.

`Spatial.Compositing` reports how the internal experiment submitted each edge:
`painter` uses an overlap-constrained primitive order, `hybrid` retains clips
against the listed `LineClipOwners` / `ArrowClipOwners`, and `clipped` retains
the prior full depth clip with a reason. Uniformly nearer bodies paint over the
primitive once, avoiding the second antialias coverage multiplication from a
complementary clip. Mixed-depth bodies must paint before that primitive and
contribute its local clip. The graph preserves overlapping connector stacking. Body groups are ordered
by their nearest surface depths; projected separation removes unnecessary
constraints. Emitted text/image content retains its established body order
until its full ink and depth are certified. Mixed body depth, a cycle or a work
budget limit retains the prior full clip.
Unverified structural identities instead roll back the entire depth submission.
`Drawing.PaintBeforePart` and `Spatial.Arrow.PaintBeforePart` report actual
submitted layers. Internal body groups retain their original `data-part`
geometry identity and publish a separate `data-paint-order`. The collector
commits that body order before computing painter coverage; native Studio hit
corridors remain beside their paths in the same DOM order. Full geometric `ClipRegion` and hit clipping remain distinct
from the local paint clip. The four SVG-unit guard is tested at 0.25/1/4 scales,
not certified for arbitrary device-pixel footprints. This policy is internal and
does not enable depth compositing in public rendering.

Connector export bounds include the actual rounded path's full round stroke
and arrow polygon, in addition to the established layout margin. Quadratic
bounds use analytic extrema. The extra one SVG-unit guard covers decimal
rounding; it is not a display-pixel antialiasing guarantee at arbitrary scales.

### Direct-manipulation editing (for embedders)

This is the contract a cloud service or a WASM client uses to turn a canvas
gesture into a new DSL document — the same logic that powers Studio's
drag/edit, exposed as **pure, stateless functions**:

- The transform is a pure function of `(format, src, op)` — **no server state**.
  (Studio works by POSTing the whole DSL each time; the text is the only source
  of truth.)
- The rewrite **preserves the user's comments and formatting** — it is line/regex
  text surgery, not a reformatting YAML round-trip.

```go
src := []byte(`# my diagram
nodes:
  scene:
    shape: composite
    layout: {mode: row, gap: 1}
    parts:
      - {id: api, shape: rectangle, geom: {w: 120, d: 80, h: 30}, label: API}
      - {id: db,  shape: cylinder,  geom: {w: 100, d: 100, h: 36}, label: DB}
`)

// Drag "api" by (+50, 0) world units. ApplyOp returns the rewritten DSL,
// a fresh SVG and validation issues; ApplyOpText returns only the new text.
op := isotopo.EditOp{Kind: "move", Target: "node", ID: "api", DWX: 50, DWY: 0}
newSrc, svg, issues, err := isotopo.ApplyOp("yaml", src, op)
_ = svg; _ = issues; _ = err
// newSrc still starts with "# my diagram" — comments survive.
```

**`EditOp` kinds** (the field set read depends on `Kind`):

| Kind | Fields used | Effect |
|---|---|---|
| `move` | `Target` (`node`\|`edge`), `ID`/`CI`, `DWX`,`DWY`, `Snap`, `Waypoints` | nudge a node offset, or move an edge (waypoints / bend). First move on an auto-layout scene freezes it to explicit coordinates. |
| `move-segment` | `CI`, `Segment`, `DWX`, `DWY` | move one pre-clip orthogonal 3-D segment; horizontal legs move perpendicularly, vertical legs move in XY. Writes `waypoints3d` and keeps resolved endpoints fixed. YAML/JSON explicit scenes only. |
| `set-field` | `Target` (`node`\|`edge`\|`canvas`), `ID`/`CI`, `Fields` (dotted path → value) | write any DSL field, e.g. `{"style.palette.top": "#FFF"}` |
| `add` | — | append a default rectangle to the scene |
| `delete` | `Target`, `ID`/`CI` | remove a node (and its connectors) or an edge. Refused for a **container** (group/lane) — empty it first |
| `duplicate` | `Target: node`, `ID` | clone a node with a fresh id, offset down-right. Refused for a **container** (cloning would collide its nested ids) |
| `reparent` | `ID` (node), `Target` (group id, or `""` for the scene root) | move a node into another group / out to the root; its stale offset is dropped so the new parent lays it out. No-op when the parent is unchanged; refused into its own descendant |

```go
// Apply only the text transform (e.g. you render separately, or with ELK):
newSrc, err := isotopo.ApplyOpText("yaml", src, op)
```

`move-segment` indexes `ConnectorRoute.World3D` from a render of the **same
source**. A UI should hit-test `Drawing.Editable` intervals, which map final
polyline segment parameters back to `RouteSegment` using the producer's exact
source record. Curves, ambiguous final projected overlaps and displaced
coincident paths have no edit mapping. These intervals are not proven pixel
visibility: intersect them with the active coverage result and native hit
testing. Studio also checks pointer ambiguity in client coordinates: a
different visible leg within one CSS pixel of the best hit distance prevents
selection, including a visible leg that has no edit mapping. Covered geometry
does not compete in that pointer-distance check.

The operation leaves input unchanged on rejection or a zero effective delta.
It rejects unsupported routes, invalid indices/coordinates, oversized results
and aliased connector mappings. YAML surgery targets the selected scene and
connector, preserving surrounding source and comments; the selected flow
mapping or waypoint field may be reformatted. JSON keeps fields and values,
with normalized indentation. Undo belongs to the caller: record history only
after accepting a valid source **and** SVG response.

For the lowest level — the individual comment-preserving text operations — import
the [`yamledit` sub-package](#sub-package-yamledit).

### Public API surface

Pipeline (each step is a separate function so you can intercept any stage):

```go
// 1. Parse input
isotopo.Parse(data []byte) (*Document, error)        // YAML or JSON auto-sniffed
isotopo.ParseYAML(data []byte) (*Document, error)
isotopo.ParseJSON(data []byte) (*Document, error)
isotopo.LoadInput(ctx, format string, data []byte, engine LayoutEngine) (*Document, error) // yaml|json|d2

// 2. For .d2 input: compile then translate
isotopo.CompileD2(ctx, src string, engine LayoutEngine) (*d2target.Diagram, error)
isotopo.Translate(diag *d2target.Diagram, opts *RenderOpts) *Document

// 3. Validate (optional)
isotopo.Validate(doc *Document) []Issue
isotopo.UnknownKeyIssues(data []byte) []Issue        // misspelled/unknown DSL keys

// 4. Render
isotopo.Render(n *Node, theme *Theme) string                          // single node, no canvas
isotopo.RenderWithCanvas(n, theme, canvas, anns) string                // scene with backdrop + callouts
                                                                       //   (canvas.Projection == "top" → flat plan view)
isotopo.RenderDocument(doc *Document) map[string]string                // every node → SVG
isotopo.RenderScene(doc *Document) SceneResult                        // SVG + routes + final screen labels
isotopo.RenderSceneWithInteraction(doc *Document) SceneResult         // same submitted scene + world interaction model
isotopo.RenderSceneWithReport(doc *Document) (SceneResult, RenderReport)
isotopo.RenderParts(doc *Document) map[string]string                   // atomic per-part SVGs
isotopo.NodeHTML(id, svg, yamlFragment string) string                  // self-contained viewer page for ONE node (svg + source)
isotopo.NodesIndexHTML(ids []string) string                            // gallery index page linking every per-node view
isotopo.RenderPlan(n, theme, canvas, anns) string                      // flat top-down plan view directly
```

One-shot render + the stateless edit contract (the embedding surface):

```go
isotopo.RenderSource(format string, src []byte) (svg string, issues []Issue, err error)
isotopo.ApplyOp(format string, src []byte, op EditOp) (newSrc []byte, svg string, issues []Issue, err error)
isotopo.ApplyOpText(format string, src []byte, op EditOp) (newSrc []byte, err error)
```

Layout evaluation — score the connection quality from the flat geometry
(see [`isotopo evaluate`](cli.md)):

```go
isotopo.EvaluatePlan(n, theme, canvas) *PlanReport          // plan-router routes
isotopo.EvaluateIso(n, theme, canvas) *PlanReport           // the engine's REAL routes
isotopo.EvaluatePlanText(format string, src []byte) (*PlanReport, error)
isotopo.EvaluateIsoText(format string, src []byte) (*PlanReport, error)
isotopo.RenderPlanAnnotated(n, theme, canvas) (svg string, report *PlanReport)  // plan view + red problem marks
```

Auxiliary:

```go
// Document model
doc.Scene() *Node                       // resolves the topology-level composite
isotopo.PartIDs(doc) []string           // every renderable part id
isotopo.PartFragments(doc) map[string]*Document  // each part as a standalone doc
isotopo.MarshalFragmentYAML(d) ([]byte, error)

// Resolve what the renderer actually uses (cascade + layout) for a part —
// the basis of Studio's "effective value" hints.
isotopo.ResolvePartStyle(doc *Document, id string) *Style
isotopo.ResolvePartGeom(doc *Document, id string) (w, d, h float64, ok bool)
isotopo.ResolvePartOffset(doc *Document, id string) (wx, wy, wz float64, ok bool)

// Theme cascade
isotopo.ResolveStyle(theme, shape, preset string, override *Style) *Style
isotopo.MergeStyle(base, over *Style) *Style

// Iso lowering
isotopo.Flatten(n *Node, theme *Theme) (shape string, opts iso25d.ConvertOpts)

// Introspection
isotopo.CapabilityReport() Capabilities
```

### Types worth knowing

| Type | What it is |
|---|---|
| `Document` | top-level parsed DSL (Canvas + Theme + Nodes + Annotations) |
| `Node` | one element. `Shape == "composite"` makes it a scene; nested `Parts` allowed |
| `CompositePart` | one sub-element inside a composite |
| `EditOp` | one direct-manipulation edit for `ApplyOp`/`ApplyOpText` (see table above) |
| `Stack` | replicate a part N times vertically |
| `Connector` | directed line between parts |
| `Annotation` | screen-space callout with a leader line |
| `Canvas` | document-level backdrop (`background`, `grid`, `gridColor`, `gridStep`, `projection`) |
| `Theme` | document-level default Style with optional per-shape overrides |
| `Style` | `{palette, stroke, text, effects}` — see [Style/Theme](dsl-theme.md) |
| `Issue` | a validation finding: `{Severity, Path, Message, Suggest}` |
| `PlanReport` | layout-quality scorecard from `EvaluatePlan`/`EvaluateIso` (crossings, edges-through-nodes, backward edges, bends, length, flow axis) |
| `LayoutEngine` | `LayoutDagre` (default) or `LayoutELK` |

### Sub-package: `yamledit`

`github.com/MarkovWangRR/iso-topology/yamledit` — comment- and
format-preserving text surgery on DSL source. `ApplyOp` is built on it; import it
directly only if you need a single operation (e.g. just `SetField`). Every
function takes the source string and returns the edited string, touching only
the lines it must:

```go
import "github.com/MarkovWangRR/iso-topology/yamledit"

out, ok := yamledit.SetField(src, line, []string{"style","palette","top"}, "#FFF")
```

Notable functions: `SetField`, `UpsertInlineKey`, `UpsertInlineList`,
`FreezeLayoutText`, `AddPart`, `DeletePart`, `DuplicatePart`,
`FindPartIDLine`, `FindConnectorLine`, `FindCanvasLine`, `ReadPath`. Full list
on [pkg.go.dev](https://pkg.go.dev/github.com/MarkovWangRR/iso-topology/yamledit).

### Sub-package: `iso25d`

The low-level iso renderer, if you want geometry without the DSL:

```go
import "github.com/MarkovWangRR/iso-topology/iso25d"

svg := iso25d.Convert2DTo25D("cylinder", iso25d.ConvertOpts{
    Width: 100, Depth: 100, Height: 40,
    TopFill: "#FFF", LeftFill: "#888", RightFill: "#BBB",
})
```

### Browser / WASM

The engine compiles for `GOOS=js GOARCH=wasm`, so `RenderSource` and
`ApplyOp`/`ApplyOpText` can run **client-side** (zero-latency edit + preview). The
only filesystem touch — inlining a local-file icon — is build-tagged off in the
js build and degrades to passing the reference through, so a wasm build never
fails on a local icon. Inline local icons as `data:` URIs before handing the DSL
to the module if you need them in the browser.


### Measured connector labels

`SceneResult.ConnectorLabels` and `RenderReport.ConnectorLabels` use
`connector_labels` (omitted when empty). Each record has `connector_index`
and `label`; the index refers to the scene's connector array in both iso and
top projections. Only fully bound labels populate this collection. Legacy or
unsupported-font labels are not certified by an absent record. Iso
`ConnectorDrawing.Label.Typography` references the same submitted geometry;
top still has no iso `Routes` or `ScreenLabels`.

`label.rect` is the final rounded pill box, and `label.anchor` is its centre,
not a text baseline. Optional `typography` contains font family/weight/size,
resource hash, final per-line baselines, advances, and ink bounds. Its
`placement` is `clear-within-scope` or `blocked`, with a machine-readable
`scope` naming body/arrow/pill AABBs and other-route stroke envelopes.
The latter contain real stroke width, round caps/joins and the declared
quadratic approximation error, rather than filling diagonal AABB corners.
An unchanged position's existing exact curve-bounds certificate is retained
if the new approximation extends beyond those bounds. Own route ink is
intentionally covered by an inline pill. This default scope excludes substrate
content, arbitrary-font ink, effects, and full pixel visibility.

In iso scenes with fully bounded body/content geometry, a still-blocked label
can be refined again without moving any already-clear label. A successful
refinement declares the more specific scope
`body-region,content-quad,other-route-stroke-envelope,arrow-aabb,pill-aabb`.
It protects separate filled faces, their stroke/curve-error envelopes,
measured text quads and icon viewports, including content outside a body and
on substrates. Substrate fills remain available for labels. Concave faces
retain their notches; a custom path's convex outline accent is independently
protected as strokes. The 6-unit gap applies to these bounds. This path is
conservative about all submitted faces, irrespective of opacity or occlusion;
it does not claim complete pixel visibility.

The entire extra pass falls back to the previous arrangement if any node
content lacks known metrics, a shape has no supported body envelope, or a
scene contains screen labels, annotations, legacy connector pills, structural
rows/cells, custom face surfaces, backgrounds, blur, shadows, glow, grain or
patterns. Currently supported bodies are box/rectangle/square, cylinder aliases,
circle, person, triangular/hexagonal/octagonal prisms, wedge, cloud and custom
paths; support still depends on the actual content. Person/circle captions and
cloud labels without icons bind the embedded Source Sans Pro resource when
the entire block has supported glyphs and weight (400, 600 or 700). These
captions preserve authored newline boundaries and the requested size
(serialized to two decimals), reserve real ink spacing, and expand the export
frame without moving the body in world coordinates. Their content-fit status
remains `unsupported`: measurable ink is not a claim that the caption fits a
bounded face or avoids other scene objects. Cloud captions without icons
retain their previous centroid; clouds with icons
retain the existing inscribed face layout and fitting report. Unknown fonts,
weights or partially unsupported blocks retain their previous metrics/path.
Unchanged labels keep their existing scope; a blocked result is
never promoted merely because geometry is unavailable. Top projection retains
its footprint routing path. Fully measured plan node titles are also protected
by their per-line ink AABBs (scope adds `plan-title-ink-aabb`). Unknown title
metrics retain the conservative joint-placement fallback. This synchronous optional pass has a shared
1,024 support-line/route-segment search-work budget and a 16,384 region-point
storage limit. It first screens for route/pill-only blockage. Reaching a limit
preserves prior positions/status and is not proof that no solution exists;
these deterministic budgets are not a wall-clock latency guarantee.

Coordinates
are root SVG user coordinates before browser transforms. Collection does
not modify the drawing or rerun placement. `RenderSceneWithReport` adds
`connector-label-placement` warnings from this same render; existing
`Validate` and readability scoring retain their separate diagnostics.
Report repair simulations copy connector records, so tentative route upgrades
cannot change the caller's authored routing or a subsequent render.

After the complete individual placement and eligible body refinement, a bounded
joint pass can arrange still-blocked labels together along their existing routes.
It fixes every already-clear label, offers safe interval-boundary/interior choices,
and compares connected groups with each label's exact old position as a fallback.
It favors more clear labels, then less movement within its finite candidate set.
An unresolved label never moves; a moved label must clear even its unresolved
peers. Independent components can improve separately. Search exhaustion retains
the best fully checked assignment and is not a proof that the scene is infeasible.
This pass requires reliable body/content bounds: unknown iso content/effects and
legacy pills keep the previous layout. Plan node titles still use ambient font
metrics, so plan scenes containing such titles retain their previous arrangement.
No node, route, arrow, font size or authored text is changed by joint placement.

`RenderSceneWithReport` also collects `typography.conflicts` for blocked
measured labels, shared by its returned scene and report. Plain `RenderScene`
keeps these detailed diagnostics absent so interactive previews do not build
or transfer a potentially quadratic conflict list. Both paths emit the same
SVG, label positions, text and placement status. Candidate repair gates request
the detailed observations on their own render pass; no diagnostic re-layout is
performed.

Each conflict identifies a `kind` (`body`, `route`, `arrow`, or `label`). Bodies
carry `part_id` and a flattened `part_index`; other counterparts carry the
scene's `connector_index`, including zero. Parallel connections remain distinct.
An unnamed body receives `#part:<index>`. Its `clearance_shortfall` is in root
SVG units and includes the declared 6-unit gap. For a rectangle it is the
minimum L-infinity translation out of the forbidden centre region. For a
route it is the maximum such penetration over its swept segments/chords;
connected collinear pieces of equal width are coalesced first. This is a
per-primitive envelope severity, not pixel overlap area, hidden route length,
or the minimum move that resolves the union of all conflicts. Body/arrow/pill
bounds retain their existing conservative scope. Own route ink is excluded;
own arrow ink is protected. Legacy peer pills retain an identity without
claiming measured legacy glyph ink. Rich body-region certificates are not
reinterpreted as body-AABB conflicts.

Conflict lists are deterministic and complete within this diagnostic scope;
report warnings name up to three counterparts while the structured list keeps
the rest. Clear labels keep their existing certificates and have no list.
An absent list in a plain preview, an unknown-font label, or an unsupported
scope must not be interpreted as a general visibility guarantee.

Measured connector presence and clear/blocked state also enter the per-edge
scene candidate defects (`connector-label-missing` / `connector-label-blocked`).
Already-blocked labels also contribute counterpart keys and measured
clearance shortfalls (`connector-label-body`, `-route`, `-arrow`, `-label`).
Routing upgrades now reject every new or worsened defect in the existing
per-object/per-edge scene map, including transferred crossings/tunnels and
new blockage or disappearance of a measured label. Existing body-repair gates
consume the same label keys. Aggregate improvement cannot buy a new pair or
increase an already-blocked label's measured shortfall. Other repair actuators
and unbound fonts retain their existing checks.

### Top projection node titles

`SceneResult.PlanLabels` and `RenderReport.PlanLabels` (`plan_labels`, omitted
when empty) describe titles in the submitted plan SVG. Studio render/edit
responses expose the same collection as `planLabels`. `part_index` is the
preorder footprint index, including containers and collapsed stacks; records
are returned in paint order. `part_id` preserves the emitted identity.

An explicit `Source Sans Pro` family with supported glyphs and static weight
400, 600 or 700 binds the same font for shaping and SVG embedding. The resolved
requested size is preserved to two decimal places. Leaf titles use a centred
budget with 8 units of horizontal and 6 units of vertical inset. Container
titles align left in the band above their first direct child's footprint,
reserving its outline and a 6-unit gap. Footprints are not resized or moved to
accommodate a title. Automatic plan routes may use the guarded refinement
described below when title translation alone cannot resolve an obstruction.

Wrapping preserves every grapheme, space and authored newline. It prefers
spaces, then permits grapheme-boundary breaks. `lines[].source_line` identifies
the authored newline index; concatenating that line's fragments reconstructs
its original text. Width wrapping is accepted only when its block fits the
available height at the requested size. Otherwise the authored lines remain.
The extra wrapping search stops after 65,536 measured input bytes and retains
remaining text; this limit does not cap the initial full-text shaping cost.

Records include the original `text`, actual SVG font request, resource hash,
`budget`, per-line baselines/advances/ink bounds and `overflow` in left, top,
right, bottom order. `status: within-budget` means ink lies in that title's
budget; it does not certify whole-scene visibility. `overflow` retains the full
text and adds a located `plan-label-fit` warning. Candidate guards preserve
per-part, per-side overflow severity. Export bounds and approximate world
selection regions include all bound ink, including overhangs. Selection adds
0.01 units to cover final SVG anchor rounding after canvas translation.

Unknown families, unsupported weights/scripts, and partially unsupported
blocks retain the original ambient-font SVG path: 11-unit centred leaf titles
and 13-unit, weight-600 container titles. Their records say `unmeasured`, list
the legacy SVG font request, and omit lines/hash rather than inventing bounds.
An unmeasured result's zero overflow array does not mean the text fits.
`ContentFits` remains the separate isometric top-face contract; plan titles
are not added to `ScreenLabels` or iso `Routes`. Title-to-title and route-to-title
visibility, arbitrary system fonts, complex scripts, global space allocation,
and target-display-scale readability remain outside this title-fit contract.

### Plan route separation

Detailed `RenderSceneWithReport` results expose `plan_route_crowding` on both
the scene and report. `EvaluatePlan` publishes the same observation under
`route_crowding`. Ordinary previews omit this diagnostic payload. It describes
the final submitted plan paths; the older `crossings` score keeps its existing
centreline definition and does not count every overlap or insufficient gap.

`status: complete` with an empty `conflicts` array establishes clearance only
within the declared `scope`. Each conflict identifies `connector_index`,
`peer_index`, the primitive pairing (`stroke-stroke`, `stroke-arrow`,
`arrow-stroke`, or `arrow-arrow`), a screen-space bounding rectangle and union
`area` in squared SVG units. This area measures intersections of clearance
envelopes, not painted overlap pixels. Stroke envelopes contain round caps,
joins, casing and continuous coverage of dash gaps; each route contributes
half the six-unit reading gap. Markers use the emitted triangle and its final
direction. Opaque leaf cores mask hidden regions; transparent/unknown fills
and container fills do not establish occlusion.

Shared endpoints receive only a local neighbourhood exemption, with radius
`12 + (widthA + widthB)/2 + 6` in SVG units. Both routes must dock at the same
serialized point on the same node. Long coincident runs and remote crossings
remain observable. Separate edges connecting the same pair of nodes receive
no such exemption, including short or bidirectional parallel edges.

Observations stop explicitly with `status: unresolved` and a reason when
geometry is non-finite or work limits are exceeded. Limits include 512 points
per route, 8192 scene route primitives, 262144 route pairs, a bounded clipping
work counter, and 256 region pieces per primitive-pair intersection. Partial
conflicts remain available but are not a clearance certificate. Reports add
located `plan-route-clearance` warnings and, when needed,
`plan-route-clearance-unresolved`.

After title-driven routing, automatic plan routes may also separate observed
route conflicts without requiring a title obstruction. This phase uses at
most 128 geometric probes and eight complete scene trials. A candidate must
remove a conflict, preserve every other counterpart's severity and existing
clear text positions, and satisfy the complete stroke/body/arrow/border
guards. Unknown text and legacy pills prevent changes; explicit paths and
fixed contour ports remain protected. Unresolved observations never authorize
a route change. Final export bounds include accepted detours even when the
scene contains no text.

### Plan title clearance and local movement

Bound titles additionally publish `placement` with a `status` of
`clear-within-scope` or `blocked`, an `offset` from the preferred layout, and
an explicit `scope`. Fit status remains independent: a title may fit its own
budget while a route crosses it. Detailed reports include conflicts identified
by counterpart kind, part index/ID or connector index, with the maximum
L-infinity clearance shortfall over its primitives. They use the same six-unit
gap as the final placement predicate. Ordinary previews omit the detailed
conflict list. `plan-label-placement` warnings locate unresolved titles;
`plan-title-blocked` and counterpart/severity keys protect subsequent scene
candidates against new or worsened conflicts.

The scope covers later-painted body AABBs, known title ink AABBs, route stroke
and casing envelopes, arrow AABBs and connector pill AABBs. Boundary interiors
remain empty; their dashed borders are conservatively treated as continuous
strips. A fully opaque rounded footprint's inner rectangular core can hide
older title or route ink according to the actual paint order. Container fills
paint before routes, so they cannot hide a later route. A leaf's own fill can
hide route ink beneath its internal title. Transparent, unknown CSS fills and
rounded corners outside the proven core never establish that exemption.
The envelopes retain conservative false positives; this is not a per-glyph
pixel visibility or full scene compositing certificate.

The plan content resolver freezes the complete connector-pill arrangement
before trying title moves. Only blocked, fully measured titles that already
fit their budgets may move, by rigid translation inside the same budget.
Text, size, wrapping, nodes, ports and routes stay fixed. Previously clear
titles stay at their exact anchors. A move is accepted only when every title
line clears all protected counterparts, including still-blocked peer titles
and all pills. Clearing a title can improve a fixed pill's final status; it
cannot move that pill or make a clear pill blocked. Render output, report and
interaction selection all use the resolved placement algorithm.

Any unmeasured nonempty title or legacy connector pill disables automatic title
moves for the scene; known geometry can still be observed. Unresolved titles
retain their original positions and report `retained_reason` such as
`unmeasured-text`, `infeasible-budget`, `no-safe-candidate` or `search-budget`.
Search is deterministic: at most 255 boundary hints per axis are reduced to
32 positions each, then at most 512 translations are checked by distance. All
titles share 65,536 line/obstacle predicate checks for candidate verification.
Only fully checked moves commit. These bounds do not cap initial/final
observation or typography work and do not prove that an unexamined feasible
position cannot exist. General visibility, arbitrary fonts and global layout
optimization remain separate work.

### Automatic plan routes around unresolved titles

After title translation, a residual route/arrow conflict may trigger a second
pass over automatic orthogonal routes. It tries alternative node-side and
title-side corridors with a scene-wide limit of 32 candidate evaluations.
Only a candidate that clears at least one previously blocked title can commit.
The entire resulting title and connector-pill arrangement is observed again;
no counterpart may acquire a new conflict or a greater clearance shortfall.
Previously clear titles keep their exact anchors. Clear pills on other edges
keep their boxes; the changed edge's own pill may follow its new route but
must retain clearance. Typography and node footprints remain fixed.

A detour must clear unrelated leaf footprints, including footprints on other
height levels, with its complete stroke/casing and a six-unit gap. Other
routes, arrows, nonterminal contacts with its own endpoints and self-crossings
are also checked. The last leg must fit the marker's full rear extent, casing
and reading gap before its preceding elbow. Arrow bounds are conservative. Congested shared-port hubs
may therefore retain the old route rather than accept coincident segments.
Unknown nonempty title ink or a legacy connector pill disables the pass.
The candidate limit does not cap initial observation, typography, or all work
inside one complete content evaluation; it is not a global optimum proof.

Explicit straight routes, bends, either waypoint form, elbow requests and
legacy named anchors retain the original route. Fixed contour ports support
detours that preserve both endpoint coordinates and outward directions.
Accepted route ink and arrows enlarge the final export bounds with a 16-unit
margin while retaining the coordinate origin used for verification, including
fixed-port routes. The input document is not rewritten. Plan rendering,
evaluation and interaction use the same deterministic routing algorithm;
isometric routing is unchanged by this pass.


### Isometric route clearance and conservative repair

Detailed `RenderSceneWithReport` results expose `iso_route_crowding` in both
`SceneResult` and `RenderReport`. Ordinary previews omit the diagnostic; their
SVG and submitted route geometry are identical. The observer consumes `Drawing`,
including final coordinate/stroke serialization, and the same render's body paint
order. It does not measure the editable, pre-trim `World` route as screen ink.

The public painter subtracts later known opaque **fill cores**, inset by the
provider's polygon error plus 0.02 SVG units. Earlier fills cannot hide a later
stroke. Arrows remain in the public overlay after body fills. An explicit internal
spatial-render experiment instead uses its own body-depth clip regions; this does
not enable that experiment for public rendering. Non-convex or uncertain opaque
coverage, unsupported curves, invalid geometry and exhausted budgets yield
`status: "unresolved"` with a reason. Only `complete` with no conflicts establishes
clearance **within the declared scope**. Translucent fill accumulation, strokes on
bodies, dash gaps, text, effects and connector-to-connector compositing are outside
that scope; the result is not a pixel visibility certificate.

Visible stroke/triangle regions are expanded by three units per side, giving a
six-unit reading gap with a conservative square envelope. Stroke pairs are
recorded only when their direction cosine magnitude is at least 0.98 and their
connected projected overlap exceeds 12 units. Ordinary transverse stroke
crossings remain outside the public warning. Their pairwise contact areas (and
short parallel contacts) are retained privately as candidate guards: replacing a
long overlap with a new remote X crossing cannot qualify as an improvement, even
when the edges share a node. Arrow contacts are recorded separately.
Conflict records retain `connector_index`, `peer_index`, `kind`, root SVG `bounds`
and the union `area` of clearance intersections. Kinds are `parallel-stroke`,
`stroke-arrow`, `arrow-stroke`, and `arrow-arrow`. Area is not a rendered pixel count.
Shared endpoint masks require the same instance and the same known world and
submitted point, have radius `12 + (widthA + widthB)/2 + 6`, and never exempt
parallel semantic edges connecting the same unordered node pair.

Limits are 512 routes, 512 points per route, 65,536 counted stroke/body/clip vertices (arrows separately require three points),
one million clipping work units and 256 intersection pieces per pair/category.
Processing occurs in the common projection frame to avoid numeric cancellation
when translating a diagram. Legacy `Readability` and `EvaluateIso.Crossings`
keep their previous meanings; new report warnings are `iso-route-clearance` and
`iso-route-clearance-unresolved`.

`RepairScene` can select `xFirst` or `yFirst` for an unconstrained `orthogonal`
connector implicated in a complete observation. It tries at most eight alternatives
per iteration. Acceptance requires fewer conflict identities, no increase in any
remaining pair's area or existing per-object visual defect, unchanged terminals,
arrows and label records, non-increasing world length/bends and final screen
length/vertical variation (0.03 serialization tolerance), non-decreasing visible
stroke-ink fraction, no export dimension growth beyond a two-unit integer-bound
rounding tolerance, and non-increasing legacy readability cost. Failed trials
restore the source. Explicit elbow, bend, waypoints, 3-D waypoints and anchor/port
references are preserved; unmeasured screen labels or legacy connector pills stop
this additional repair. Plain render and `--no-repair` never change source routes.
This bounded local choice does not solve global route allocation or establish
arbitrary-scene absence of visual defects.
