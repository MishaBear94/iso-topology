---
title: "CLI reference"
description: "Commands, flags, output files, and exit codes for isotopo."
---

iso-topology has two faces: a CLI (`isotopo`) and a Go library
(`github.com/MarkovWangRR/iso-topology`). Both expose the same
rendering pipeline.

## CLI

### `isotopo render` — produce SVG + embed assets

```
isotopo render [--layout dagre|elk] [--projection iso|top] [--readable] [--theme <name>] [--no-repair] [--report] <input> <output-dir>
```

| Argument | Meaning |
|---|---|
| `--layout dagre` | default; natural-bend polyline edges |
| `--layout elk` | orthogonal right-angle edges with obstacle avoidance |
| `--projection iso` | default; 2.5D isometric view |
| `--projection top` | flat top-down **plan view** — footprints + orthogonal edges, height dropped (good for reading the layout/flow). Overrides `canvas.projection`. |
| `--readable` | use upright screen labels, contrast chips, and a padding floor where the document has not supplied its own settings |
| `--theme <name>` | apply a built-in design system; inspect available names under `themes` in `isotopo capabilities` |
| `--no-repair` | skip the automatic projection-repair pass |
| `--report` | write the repair report to `report.json` in the output directory |
| `<input>` | path to `.yaml`, `.json`, or `.d2` (or `-` for stdin) |
| `<output-dir>` | directory to write `topology.{svg,html,…}` and `nodes/<id>.{svg,html,yaml}` |

The `--layout` flag is consulted only for `.d2` input (YAML/JSON
already encode placement). The projection can also be set per-document
with `canvas.projection` (see [DSL/YAML](./dsl-yaml.md)). Output structure
is documented in [OUTPUTS.md](../reference/output-layout.md).

`render` runs the full validator first (issues go to stderr) and returns
an exit code that mirrors `validate`, so an agent can gate on `render`
alone without a separate `validate` step:

| Exit code | Meaning |
|---|---|
| 0 | rendered a non-empty scene with no validation errors (warnings may have printed) |
| 3 | validation **errors** were present, **or** the document produced no scene (empty `topology.svg`) |
| 1 | I/O or parse failure (message on stderr) |

Warnings alone do **not** fail the render. On a code-3 result the
(partial or empty) output is still written for inspection — it is simply
no longer reported as success.

Examples:

```bash
# .d2 with dagre auto-layout
isotopo render scene.d2 ./out

# .d2 with ELK orthogonal routing
isotopo render --layout elk scene.d2 ./out-elk

# YAML composite
isotopo render scene.yaml ./out

# Stream a generated DSL through stdin
gen-dsl | isotopo render - ./out
```

### `isotopo capabilities` — discover supported features

```
isotopo capabilities
```

Emits a JSON document describing the live feature contract, including:

| Key | Contents |
|---|---|
| `version` | semantic version of this build |
| `inputs` | accepted file extensions and what they mean |
| `layouts` | layout engines for `.d2` |
| `shapes` | every shape name and its aliases |
| `primitives` | composition primitives (group, stack, annotation, …) with one-line syntax |
| `style_keys` | every field inside `palette` / `stroke` / `text` / `effects` |
| `themes` | available built-in design systems |
| `enums` | accepted values for fields with a closed vocabulary |

Designed for agents to read once at startup so they can generate
valid DSL without consulting source code. Example:

```bash
isotopo capabilities | jq '.shapes[].iso_name'
# "circle", "cloud", "composite", "cylinder", "group", …

isotopo capabilities | jq '.primitives[] | {name, syntax}'
```

### `isotopo validate` — structural check before render

```
isotopo validate <input>
```

Parses the input and runs structural validation: unknown shape names,
annotation anchors that don't resolve, connector references to
missing parts, unknown canvas grid modes, etc.

It also runs geometry/quality lints, including a **plan-view footprint
collision** check: after solving layout, every node and group is projected to
its top-down (x, y, w, d) footprint, and any two that **touch, cross, or cover**
one another are warned — except a child sitting inside its own group (legitimate
containment) and stack replicas. This catches `place`/`offset` values that make
boxes or group slabs collide in the flat plan view, which the 3-D cross-container
lint (it requires height overlap too) and the same-container sibling check (it
ignores mere touching) would miss.

Two more geometry lints:
- **label occlusion** — warns when a node sits over a group label or `iso_text`
  title in the rendered view. The renderer silently lifts an occluded group label
  on top, so this surfaces the defect as feedback instead of swallowing it.
- **container fit** — warns when a child's effective footprint exceeds 90% of a
  **fixed-size** group/boundary on either axis (it bursts the slab). Auto-sized
  containers grow to wrap their children, so they're exempt. Effective dims honour
  per-shape render floors (e.g. a cloud is measured at its drawn 200×140), so a
  cloud crammed into a small fixed group is caught.
- **rounded side band** — warns when a solid `palette.right` that is clearly
  different from `palette.left` is set on a **rounded** part (cornerRadius ≥ 6,
  including the default-rounded group slab). The rounded renderer collapses the
  two side walls into one band driven by top + left and never reads the solid
  right, so that colour is silently dropped. Normal same-hue iso shading is below
  the distance threshold and stays quiet; set `cornerRadius: 0` for distinct
  left/right faces.

Emits JSON of issues:

```json
{
  "issues": [
    {
      "severity": "error",
      "path": "nodes.scene.parts[2].shape",
      "message": "unknown shape \"cilinder\"",
      "suggest": "cylinder"
    }
  ]
}
```

| Exit code | Meaning |
|---|---|
| 0 | clean — no issues |
| 2 | warnings only — will render but probably not as intended |
| 3 | errors — render will likely fail or produce broken output |

In an agent loop:

```bash
isotopo validate scene.yaml > /tmp/issues.json
if [ "$?" = "3" ]; then
    # Apply suggestions, re-emit DSL, re-validate
    ...
fi
isotopo render scene.yaml ./out
```

### `isotopo evaluate` — score the auto-layout connection quality

```bash
isotopo evaluate <input> [output-dir]
```

Measures how reasonable the layout's wiring is, from the flat top-down
geometry (true world x,y — the iso projection skews distance/angle, so
crossings and lengths can't be measured there). Prints a JSON scorecard
to stdout; with an `output-dir`, also writes `plan.svg` with problems
marked in **red**.

The report has two halves — `plan` (the plan view's preview router) and
`iso` (the **engine's real routes**, parsed from the rendered SVG) — so
you can see both the achievable and the actual quality:

| Metric | Meaning (lower is better) |
|---|---|
| `crossings` | edge–edge crossings between non-adjacent edges |
| `edges_through_nodes` | edges tunnelling an unrelated node footprint |
| `backward_edges` | edges running against the dominant flow |
| `node_overlaps` | overlapping node footprints |
| `total_bends` / `total_edge_len` / `max_edge_len` | route complexity |
| `flow_axis` | inferred dominant flow (`vertical`\|`horizontal`\|`none`) |

z-stacked scenes are handled: a part on another z-floor, a shared
substrate both endpoints sit on, or a flat decoration (`h ≤ 2`) is not
counted as an obstacle.

```bash
# scorecard only
isotopo evaluate scene.yaml | jq '.iso'

# scorecard + annotated plan.svg with crossings/tunnelling in red
isotopo evaluate scene.yaml ./out
```

Library equivalents: `EvaluatePlan` / `EvaluateIso` / `RenderPlanAnnotated`
(see [API reference](./api.md)).

### `isotopo repair` — inspect or persist automatic fixes

```bash
isotopo repair scene.yaml
isotopo repair scene.yaml --write
isotopo repair scene.yaml --compose --write
```

Without `--write`, this prints a fix report without modifying the input. With
`--write`, it persists supported fixes while preserving comments. `--compose`
also enables bounded alignment snapping. Exit code `0` means no repairs were
found; `2` means repairs were found, including when they were written.

`render` applies its automatic repair pass to the rendered document by default.
Use `--no-repair` to inspect the authored scene without that pass, or `--report`
to save a report of what changed. Review the output: automatic repair does not
guarantee that every possible layout problem has been resolved.

### `isotopo snapshot` — rasterize a scene

```bash
isotopo snapshot scene.yaml ./out
```

Writes `topology.png` using the SVG viewBox as its viewport. This requires `resvg`
or ImageMagick. It accepts `--no-repair` and `--report`, like `render`.

### `isotopo preview` — render a subset

```bash
isotopo preview scene.yaml ./selection.svg api db
```

Select nodes by ID, a container and its children, or a connector such as
`edge:0`. The subset is laid out and cropped independently. Use `-` as the
output path to write SVG to stdout.

## `isotopo serve` — the Studio

The interactive page has a name and its own guide: **[isotopo Studio](../guides/studio.md)** — feature tour, design rationale, endpoints, and how it is tested. Quick reference below.

```bash
isotopo serve scene.yaml          # default port 8731
ISOTOPO_PORT=9000 isotopo serve scene.yaml
```

Serves the interactive viewer at `http://localhost:8731`:

- the YAML editor (line numbers, Tab inserts indent, drag the splitter
  to resize the pane) edits an **in-browser copy — the input file on
  disk is never written**; unsaved edits persist across a reload as a
  local draft, with a one-click `revert`
- hovering a node in the SVG highlights its source block; the filetab
  shows the absolute source path with a copy-path button
- scroll to zoom, drag to pan, double-click to reset
- edits re-render automatically (debounced) or with Cmd/Ctrl+Enter;
  validation errors show under the editor with fix suggestions, and
  the canvas keeps the last good render (with a badge) while the
  source is broken
- `↓ SVG` / `↓ PNG` export whatever the canvas currently shows —
  unsaved edits included — as `<name>.edited.svg` / `.png` (2x);
  `Download` saves the edited YAML copy
- `Browse nodes` opens the per-part gallery, served live from the
  current file content

Endpoints, for tooling: `GET /` viewer · `GET /topology.svg` fresh
render of the file on disk · `GET /nodes/_index.html` and
`/nodes/<id>.svg|.yaml|.html` gallery · `POST /api/render` renders the
posted source, returns `{"svg", "issues"}` · `GET /api/ping` health.

The same interactive page is written to `out/topology.html` by
`isotopo render`; opened as a static file it keeps everything except
live re-render (the toolbar tells you to start `isotopo serve`).

## Go library

The CLI is a thin wrapper over the Go engine. The full library API —
parsing, rendering, the stateless **edit** contract (`ApplyOp` /
`EditOp`), types, and the `yamledit` / `iso25d` sub-packages — lives in
the single **[API reference](./api.md)**.
