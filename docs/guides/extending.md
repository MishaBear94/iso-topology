---
title: "Extend the renderer"
description: "Add shapes, DSL primitives, layout engines, and editing controls."
---

This is the agent-targeted recipe doc. Each recipe answers "I want to
add X — where do I touch?". Pair every code change with a golden-file
test so drift is caught automatically.

## How to add a new shape

A "shape" is a leaf primitive — rectangle, cylinder, cloud, etc. The
shape ends up rendered by `iso25d.Convert2DTo25D` after going through
`Flatten` (in `flatten.go`) which produces an `iso25d.ConvertOpts`.

1. **Implement the iso geometry** — create `iso25d/shape_<name>.go`:

    - Define `Iso<Name>Opts` (just the parameters the geometry needs).
    - Implement `RenderIso<Name>(o Iso<Name>Opts) string` returning an
      SVG fragment (no `<svg>` wrapper — that's added by the composite).
    - Implement `apply<Name>(o ConvertOpts, target *Iso<Name>Opts)`
      that lowers the dispatcher's `ConvertOpts` into your opts.

2. **Register in the dispatcher** — edit `iso25d/shapes.go`. Add a
   `case "<name>":` arm in `Convert2DTo25D` that calls `apply<Name>`
   then `RenderIso<Name>`.

3. **Wire d2 input** — edit `translate.go::d2ShapeCatalog`. Add every
   d2 shape string that should lower to this iso shape (with its
   height multiplier hint). If the shape is iso-native and has no d2
   counterpart, skip this step — agents will reach it via YAML only.

4. **Make Validate happy** — `CapabilityReport()` reads the catalog,
   so as long as you added to `d2ShapeCatalog` (or to the native
   shapes list in `capabilities.go::buildShapeCaps`), Validate will
   accept the new shape in YAML/D2 inputs.

5. **Make it editable in Studio** *(easy to forget — keeps the GUI in
   sync with the renderer)*. The shape token also lives in three
   non-renderer places; all must agree or the Studio "Edit details"
   shape picker offers a token that silently falls back to rectangle:
    - `cmd/isotopo/main.go` → `shapeOptions` (the tile list) and
      `shapeClass` (which colour controls the shape gets: faces /
      outline / text / fill).
    - `studio/studio.js` → `optGlyph` (a small inline-SVG glyph keyed
      `'shape:<name>'`, so the tile is illustrated).
   Use only tokens the dispatcher actually accepts (`isotopo
   capabilities` → shapes → `accepted_as`).

6. **Add a golden test** — create `samples/node/<name>/input.yaml`
   (single-primitive) or `samples/topology/<name>-demo/input.yaml`
   (multi-element scene), then `go test -update -run TestGolden/.*<name>`
   to snapshot the expected SVG.

## How to add a new DSL primitive

A "primitive" is a composition feature like `group`, `stack`,
`annotation`, `canvas.grid`. Primitives don't map to a single iso
shape — they affect how parts get assembled.

1. **Declare the data** — add the field(s) to the right type in
   `dsl.go` with yaml/json tags. Use pointer types if the field is
   optional and should distinguish "missing" from "zero".

2. **Lower or inject** — choose one:

    - **Lowering primitive** (transforms the parts tree before
      rendering — like `stack` and `group`). Edit `lower.go`:
      extend `lowerCompositeParts` with the new case.

    - **Layer primitive** (post-rendered SVG overlay — like
      `canvas-grid` and `annotation`). Edit `inject.go`: add an
      `inject<Name>` function that splices a `<g data-layer="...">`
      into the rendered SVG.

3. **Wire the renderer** — edit `render.go::renderComposite`. Decide
   the paint order (earlier-injected = behind everything because of
   how `<svg>`-after-tag splicing pushes prior insertions deeper).

4. **Describe it** — edit `capabilities.go::buildPrimitiveCaps`. Add
   a `PrimitiveCap` entry. This is the agent's source of truth.

5. **Validate it** — edit `validate.go`. Add structural checks (does
   the anchor exist? is the side value valid? is the count positive?).
   Use the existing `nearest()` / `nearestID()` for "did you mean".

6. **Add a golden test** — same as for shapes.

## How to expose a field in the Studio "Edit details" editor

The right-click detail modal is **schema-driven**. To let users edit a
DSL field through the GUI (and have it write back, comment-preserving):

1. Add a `schemaField` to the relevant builder in `cmd/isotopo/main.go`:
   `nodeSchema(shape)`, `edgeSchema()`, or `canvasSchema()`. Give it the
   dotted YAML `Path` (e.g. `style.palette.top`), an English `Label` +
   `Desc`, a `Type` (`text` | `number` | `color` | `choice` | `icon`),
   a `Group`, and `Inline`/`Options` as needed.
2. Reads and writes are automatic: `/api/fields` reads the current value
   by dotted path; `/api/edit` writes it via the recursive
   `setField`/`setInInlineMap` surgery (creates missing intermediate
   maps). No per-field code.
3. For a `choice` field, add a glyph in `studio/studio.js::optGlyph`
   (keyed `'<lastPathSegment-or-key>:<value>'`) and, if the stored value
   differs from the label, a `optLabel` entry.
4. Keep it shape-appropriate: `nodeSchema` only offers colour controls a
   shape can actually render (see `shapeClass`) — don't show a knob that
   does nothing.

## How to add a new layout engine

d2 ships dagre and ELK. To add a third:

1. Implement a `d2graph.LayoutGraph` (see d2 docs).
2. Edit `compile.go::CompileD2`. Extend `layoutResolver`.
3. Edit `dsl.go`: add a `LayoutXxx LayoutEngine` constant.
4. Edit `cmd/isotopo/main.go::parseFlags` to accept the new `--layout xxx`.
5. Edit `capabilities.go::Layouts` so agents discover it.
6. Edit `validate.go` if the layout name appears in DSL anywhere
   (today it's CLI-only, so no validate change needed).
7. Add a golden test under `samples/topology/<name>-layout/input.d2`.

## The agent loop

```
1. read   →  isotopo capabilities   (learn what's available)
2. write  →  emit DSL
3. check  →  isotopo validate <file>   (catch typos before render)
4. render →  isotopo render <file> <out>   (produce SVG)
5. test   →  go test (./...)   (regression check)
```

If step 3 returns suggestions, apply them and re-run step 3. If step
5 fails with golden drift, decide per-case: intended → update golden
with `go test -update`; unintended → revert the change.

## Code structure cheat sheet

```
dsl.go              every public DSL type
parse.go            yaml/json → Document
translate.go        d2target.Diagram → Document
flatten.go          Node + Theme → iso25d.ConvertOpts
theme.go            3-layer style merge
render.go           public API: Render / RenderDocument / RenderParts
lower.go            group + stack expansion
inject.go           canvas-bg / screen-label / connector / annotation
svgutil.go          SVG string parse/edit
output.go           //go:embed studio/* → Studio page; per-part fragments
capabilities.go     introspection — what does this build support?
validate.go         structural validation with suggestions
compile.go          d2 → laid-out diagram

studio/             the Studio front-end (compiled into the binary)
├── studio.html     page shell ({{CSS}}/{{JS}} + data placeholders)
├── studio.css      all styles
└── studio.js       all behaviour — zoom/pan, code mapping, drag-to-edit,
                    detail editor, context menu, undo/redo
cmd/isotopo/main.go serve handlers: /api/render, /api/move (drag),
                    /api/fields + /api/edit (detail editor),
                    /api/op (delete/duplicate) — all comment-preserving
                    text surgery on the POSTed source; disk never written
tools/viewer-test/cdp_test.py   headless-Chrome regression suite for Studio

iso25d/             low-level iso geometry, one shape per file
├── composite.go    iso z-order painter
├── iso.go          IsoBox renderer + shared types
├── rounded.go      rounded-corner panel renderer
├── shapes.go       Convert2DTo25D dispatcher + ConvertOpts type
├── shape_box.go    (lives inside iso.go for historical reasons)
├── shape_cylinder.go
├── shape_sphere.go
├── shape_person.go
├── shape_text.go
└── shape_cloud.go

cmd/isotopo/        the CLI
├── main.go         subcommand routing + flag parsing
```

## Agent self-evolution: contracts vs. manual sync points

What makes this repo safe for an agent (including a future you) to evolve:

- **Machine-readable contract.** `isotopo capabilities` emits the full
  supported surface as JSON; `tools/gen-docs` regenerates
  `CAPABILITIES.md`, `dsl.schema.json`, `ICONS.md`, `llms.txt`,
  `SAMPLES.md` and patches `PROMPT_TEMPLATE.md` from code. **Run
  `go run ./tools/gen-docs` after any capability change** so the docs
  can't drift.
- **Deterministic regression.** Golden SVGs (`go test`) catch any render
  drift byte-for-byte; the Studio has a CDP suite (`cdp_test.py`).
- **Validation teaches.** `validate.go` returns did-you-mean suggestions
  and CI exit codes (0/2/3).

The real hazard is **parallel lists that must stay in sync** — an agent
that edits one and forgets the others ships a silent desync (this is how
`box`/`sphere`/`polygon` once leaked into the shape picker):

| Concept | Source of truth | Also hand-maintained in |
|---|---|---|
| Shape tokens | `iso25d/shapes.go` (dispatch + `accepted_as`) | `capabilities.go`, `cmd/isotopo/main.go` (`shapeOptions`/`shapeClass`), `studio/studio.js` (`optGlyph`) |
| Detail-editor fields | DSL structs in `dsl.go` | `cmd/isotopo/main.go` schema builders, `studio/studio.js` glyphs |
| Icon value forms | `iso25d/brand_icons.go` + `flatten.go` | `capabilities.go` note, gen-docs |

When you add a DSL capability, walk the whole row. The long-term fix is
to **derive** the detail schema and shape options from the capability
report / struct tags instead of hand-listing them — until then, this
table is the blast-radius checklist.

### Deferred roadmap (known, intentionally not yet built)

- Studio: **multi-select + marquee drag** (group move); **gradient editing**
  in the detail modal (palette `*Gradient` blocks are hand-edited today);
  a wider edge hit-area / grab-anywhere for edges fully occluded by their
  nodes (inherent geometry — partial mitigation only).
- Split `studio.js` (~45 KB, one file) into feature modules concatenated via
  embed; add JS lint/format in CI. Pure internal restructuring — deferred for
  its global-order risk vs. zero user-facing value.

*Done since this list was first written:* snap-to-grid, place-aware delete,
add-node, freeze bakes `wz`, NodeHTML/index extracted to `studio/`, and a
capability-coupled guard test for the shape picker.

## Don'ts

- **Don't bake icons / fonts into the binary** — SVG embeds them by
  URL. The default font stack ("Inter, sans-serif") relies on the
  browser/embedder.
- **Don't add new top-level export to the library API casually** —
  every export is part of the public contract. Prefer extending an
  existing function over adding a new one.
- **Don't modify a samples/ golden by hand** — always use
  `go test -update -run TestGolden/<name>` so re-generation matches
  the rest of the pipeline.
