# Agent prompt template

Drop-in system prompt for an LLM that emits iso-topology DSL. Tested
with Claude, GPT-4-class, and small local models. Customize the
`<TASK>` section for your use case.

## Minimal template

The block below is GENERATED from `CapabilityReport()` — run
`go run ./tools/gen-docs` after any DSL change and it stays current.
Don't edit between the sentinels.

<!-- BEGIN GENERATED: minimal-template (gen-docs) -->
```text
You are an assistant that produces iso-topology DSL. iso-topology
renders 2.5D isometric architecture diagrams from textual DSL.

CAPABILITIES v0.4.5 (the only DSL you may emit):
- Input formats: .yaml (manual composition) or .d2 (auto-layout).
- Shapes: array1d, array2d, array3d, boundary, circle, cloud, composite, custom_path, cylinder, group, hexprism, iso_text, octprism, person, rack, rectangle, triprism, wedge.
  (d2 aliases like queue/stored_data/hexagon also accepted — see CAPABILITIES.md.)
- Composition primitives (YAML):
    group:      {shape: group, layout: {mode: row|column|grid|ring}, parts: [...], label: "…", geom: {h}}
    stack:      stack: {count: N, gap: g}
    canvas-grid: canvas: {background: "#FAFBFC", grid: iso, gridColor: "#E2E6EE", gridStep: 36}
    annotation: {anchor: <part-id>, text: "…", side: top|right|bottom|left, distance: 60}
    connector:  {from: <part-id>, to: <part-id>, routing: straight|orthogonal|bezier, arrow: none|triangle, label: "…"}
    layout:     layout: {mode: row|column|grid|ring, cols: N, gap: 1, padding: 1, align: start|center|end}
    place:      place: {rightOf: <sibling-id>, inFrontOf: <sibling-id>, above: <sibling-id>, gap: 1, gapX: 2, gapY: 0, align: start|center|end}
    preset:     theme: {presets: {<name>: <Style>}}  →  part: {preset: <name>}
    icon:       icon: "iso://glyph/<name>[/light|/RRGGBB]" | "iso://si/<slug>[/light|/RRGGBB]" | "iso://brand/<name>"
- Style sub-blocks:
    palette:  top, left, right, topGradient {from, to, dir}, leftGradient {from, to, dir}, rightGradient {from, to, dir}, note: on prisms (v3.12) left/rightGradient shade the visible side walls by normal dominance — +x-facing walls take right, +y-facing walls take left, matching the box's two-wall semantics
    stroke:   color, width, dash
    faces (v3.3): map of face name → {fill, strokes}; names: top|left|right (box family), top|side0..sideN-1 (prisms), "*" wildcard; outranks palette, fill {kind: solid|linearGradient|radialGradient|pattern, color, stops: [{offset 0..1, color}], angle (linear, degrees), cx/cy (radial 0..1), pattern {kind: hatch|dots, color, spacing, angle, projected}}, projected: true pins the pattern tile to the face's iso plane instead of screen space — on prisms (v3.12) each side0..sideN-1 wall gets its own edge-aligned plane matrix, so hatch/dots hug every wall, not just box left/right, strokes: [{color, width, dash, opacity}] — multiple re-traces per face, list order = paint order (put thin highlight lines after wide outlines), note: a pattern fill REPLACES the base fill (transparent tile background); supported on the box family, prisms, and cylinders (top/left/right) — cloud/person/sphere keep palette only for now
    text:     family, size (a MAXIMUM — top-face labels auto-wrap at word boundaries and auto-shrink so they never overflow the face; icons are clamped to the face too), weight, color, orient, boxBg, boxBorder
    effects:  opacity, margin, cornerRadius, dropShadow {dx, dy, blur, color}, backglow {color, radius, opacity}, blur (v3.7) — gaussian stdDev in px over the whole part; fog / ghost / de-emphasized layers, outline {color, width, dash, opacity} (v3.8) — accent ring along the part's full silhouette (selection / emphasis), distinct from per-face stroke; hugs the true outline of every shape, pattern {kind: hatch|dots, color, spacing, angle}, wireframe (bool — line-art: strokes only, no fills; ghost parts are exempt from overlap warnings), grain {intensity 0..1, scale} (film-grain noise on the faces), faceSplit (bool — rounded boxes only: split the single wrap-around side band into independent left/right faces so a rounded cube shades per-face like a sharp one; needs cornerRadius>0, no-op otherwise)

POSITIONING RULES:
- NEVER hand-compute coordinates. Pick ONE anchor part per scene and
  chain everything else off it with place; arrange container children
  with layout. All gaps/padding are in CELLS (1 cell = gridStep).
- A stair = each tile {rightOf: prev, inFrontOf: prev}. A dashboard
  grid = one group with layout {mode: grid}. Region substrates with
  layout or place children auto-size — omit their geom.w/d.
- offset is a fine-tune delta on top of a solved position. Reach for
  it last, never as the primary mechanism.
- Sizes stay explicit and semantic: hero parts big (geom 200+),
  supporting tiles small (60-130).

OUTPUT CONTRACT:
- Emit exactly one fenced code block containing valid YAML (or .d2).
- The YAML must have a top-level nodes: map with a node named scene
  whose shape is composite.
- Use ids that are short, lowercase, and snake_case.
- Validate mentally before emitting: every place/connector/annotation
  reference must name an existing part id; place refs must be
  SIBLINGS (same parts list); no place cycles.

VALIDATION LOOP:
- The harness runs isotopo validate (exit 0 clean / 2 warnings only /
  3 errors) and may send the JSON issues back. Each issue has:
    severity, path (JSONPath into your DSL), message, suggest.
- Overlap warnings name the exact colliding pair — raise that place
  gap or rearrange, then re-emit the COMPLETE corrected YAML.

WHEN UNSURE:
- Prefer .d2 input for plain box-and-arrow graphs; use .yaml when the
  scene needs composition (groups, stairs, stacks, styled boards).
- Default shapes: rectangle = compute, cylinder = data, person =
  human actor, cloud = external system.
- Default canvas: {background: "#FAFBFC", grid: iso, gridColor: "#E2E6EE", gridStep: 40}.
- Connectors: ALWAYS routing orthogonal — every segment must ride the
  iso grid. Style async links with dash, never with bezier/straight.

<TASK>
{user's actual prompt here}
</TASK>
```
<!-- END GENERATED: minimal-template -->

## How to use this

### Option A — full system prompt

Paste the template into your system prompt slot. Replace `<TASK>`
with the user's actual diagram request. Pipe the model output
through `isotopo validate`; if it fails, send the JSON back to the
model with "Apply the suggestions and emit a corrected YAML."

### Option B — minimal context injection

If the model has tool access, just give it:

```text
You can render iso architecture diagrams. Generate DSL using the
schema at docs/agent/schema/dsl.schema.json and the reference at
docs/agent/CAPABILITIES.md; imitate the fixture from
docs/agent/SAMPLES.md that best matches the task. Use
`isotopo validate <file>` to check before claiming done.
```

### Option C — fully autonomous loop

For agentic frameworks (Claude Code, Cursor, custom):

```python
def render_diagram(task: str) -> bytes:
    for attempt in range(3):
        dsl = llm.generate(system=PROMPT_TEMPLATE, user=task)
        issues = run(["isotopo", "validate", "-"], input=dsl).stdout
        if issues_clean(issues):
            return run(["isotopo", "render", "-", "/tmp/out"], input=dsl)
        task = f"Previous attempt had these issues:\n{issues}\nFix and re-emit."
    raise RuntimeError("agent loop exhausted")
```

## Worked example

User prompt:

> Draw the data ingestion pipeline: user uploads to S3, a Lambda
> trigger pushes the metadata to a Postgres database. Show the user
> as a person icon, the upload bucket as a cloud, Lambda as a normal
> compute box, and Postgres as a cylinder.

Agent output (validated; note: zero hand-computed coordinates — the
user anchors the scene, everything else chains off it with `place`):

````yaml
canvas: {background: "#FAFBFC", grid: iso, gridColor: "#E2E6EE", gridStep: 40}

nodes:
  scene:
    shape: composite
    parts:
      - {id: user,    shape: person,    geom: {w: 30, d: 30, h: 50}, label: User}
      - {id: bucket,  shape: cloud,     place: {rightOf: user,   gap: 2}, geom: {w: 120, d: 80, h: 30}, label: "S3 Bucket"}
      - {id: lambda,  shape: rectangle, place: {rightOf: bucket, gap: 2}, geom: {w: 120, d: 80, h: 30}, label: Lambda}
      - {id: db,      shape: cylinder,  place: {rightOf: lambda, gap: 2}, geom: {w: 100, d: 100, h: 36}, label: Postgres}
    connectors:
      - {from: user,   to: bucket, arrow: triangle, routing: orthogonal, label: upload}
      - {from: bucket, to: lambda, arrow: triangle, routing: orthogonal, label: trigger}
      - {from: lambda, to: db,     arrow: triangle, routing: orthogonal, label: write}
````

Render with `isotopo render scene.yaml ./out`.

## Things to tell the model NOT to do

- Don't hand-compute coordinates. `offset:` in generated DSL is a
  smell — if the model emits more than the occasional fine-tune
  delta, repeat the POSITIONING RULES section.
- Don't invent shape names. Stick to the enum in CAPABILITIES.md.
- Don't emit prose around the YAML — exactly one fenced code block.
- Don't reach for `group` for two-element groups — place them as
  siblings instead. Groups are for 3+ elements that share a semantic
  boundary (and then give the group a `layout`, not offsets).
- Don't set `geom.h` higher than 80 for a single element unless it's
  the scene's hero — visually reads as a billboard, not a service.
- Don't use `stack` for fewer than 3 replicas — at N=1 it's a no-op
  and at N=2 it reads as a layered cake.

## Token budget

The full template above is ~700 tokens. CAPABILITIES.md adds another
~1.5k tokens if you want to include it verbatim instead of just
referencing it; one matching fixture from SAMPLES.md is usually the
better spend (~300–900 tokens, and it demonstrates composition, not
just grammar).

For models with strict context budgets, drop the WHEN UNSURE and
worked-example sections — they're nice-to-have, not required for
correct output. Keep POSITIONING RULES; it has the highest
correctness-per-token of the whole prompt.
