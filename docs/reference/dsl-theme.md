# Style and Theme

Every iso part has a `Style` that controls its visual presentation —
fills, stroke, label typography, opacity, etc. The `Theme` block on a
Document provides defaults; per-part `style:` overrides them.

## The four style sub-blocks

```yaml
style:
  palette: {...}       # face fills (top / left / right)
  stroke:  {...}       # silhouette stroke
  text:    {...}       # label typography
  effects: {...}       # opacity, margin, corner radius
```

Each sub-block is optional and merges independently — set just the
fields you want to change.

### `palette`

Iso parts have three visible faces (top, left, right):

```yaml
palette:
  top:   "#FFFFFF"     # top face fill (lit-from-above)
  left:  "#7E94B5"     # left face (in shadow)
  right: "#A6B7D2"     # right face (mid-tone)
```

A clean palette uses three values on a single hue at different
lightness. Skip the fields you don't want to set; the theme defaults
fill in.

For shapes without three distinct faces (cylinder, circle, cloud,
person), `top` and `left`/`right` cover the lit and shadowed sides
respectively.

Each face can also take a **linear gradient** instead of a solid
fill — the gradient wins when both are set:

```yaml
palette:
  topGradient:   {from: "#FFE188", to: "#F59E0B", dir: down}
  leftGradient:  {from: "#F4F4F8", to: "#D8DAE3", dir: down}
  rightGradient: {from: "#FFFFFF", to: "#E8E8EE", dir: down}
```

`dir` is the gradient direction across the face (`down` is the
common choice — lit top edge falling to a darker base). On
`cornerRadius` parts, the rounded silhouette has one continuous side
band: it runs `rightGradient.from` at the top to `leftGradient.to`
at the bottom.

### `stroke`

```yaml
stroke:
  color: "#1F2937"     # CSS color
  width: 1.2           # SVG stroke-width
  dash:  "4 3"         # SVG stroke-dasharray syntax (or omit for solid)
```

Set `width: 0` to hide the silhouette entirely (useful for soft cards).

### `text`

Controls the label rendered on the top face (or screen-space, depending
on `orient`):

```yaml
text:
  family: "Inter, sans-serif"
  size:   12               # px
  weight: "600"            # CSS font-weight string
  color:  "#1F2937"
  orient: iso              # iso (default) | screen
  boxBg:     "#FFFFFF"     # used only with orient: screen
  boxBorder: "#1F2937"
```

Adaptive top-face layouts estimate label widths, wrap at word boundaries,
and may reduce the font down to 5px and shrink the icon to make room.
For these layouts, `size` is an upper bound; content that already fits
keeps its existing placement. When a long word must break, wrapping and
last-line balancing preserve whole grapheme clusters, including combining
accents and emoji sequences. Balancing never moves letters across an
original word boundary.

For system fonts, width estimates are not measurements of the selected font. Fixed
face dimensions can still be too small at the minimum font size: an
oversized cluster remains intact and may overflow. Increase the face size
or use a screen-space caption when the text needs more room. Explicit icon
anchors and offsets retain the author's placement instead of auto-fitting.

Validation reports a warning when the final estimated content bounds exceed
the preferred top-face padding budget. This may indicate lost breathing room
even when the content is still inside the physical face. Render reports expose
the final lines, font sizes, image viewports and affected sides as `content_fits`.
An extremely small face retains a positive icon viewport inside the face when
the padding itself is infeasible; that fallback still reports the unmet budget.
An icon never grows merely because the label needs more space.

For reproducible top-face or screen-label text, opt into the bundled Source Sans Pro font:

```yaml
style:
  text: {family: "Source Sans Pro", weight: "600", size: 18}
```

The bounded top-face renderer embeds the same immutable font resource that
it uses for shaping and fitting. Weights `400`/`normal`, `600`, and `700`/`bold`
are supported; use the exact family name rather than a CSS fallback list.
Latin, Greek, Cyrillic and supported punctuation/combining marks use shaped
advances and ink bounds. Missing glyphs, other scripts and system-font
fallback remain explicitly estimated in `content_fits`; this does not bind
a CJK or emoji font. Sphere labels, free isometric text, connector labels and
the separate top projection retain their existing measurement paths.

For automatically positioned content whose entire label has bound metrics,
the renderer tests rows and wrapped columns using actual glyph heights and
ink gaps. It preserves an already feasible full-size layout; otherwise it
prefers a larger feasible font, then a larger icon, then fewer layout items.
The search has a bounded candidate budget and does not claim a globally
optimal layout. Existing minimum-font and icon-reduction policies remain in
force, with two-decimal SVG placement rounding. Explicit blank lines remain.
Manual icon placement and unsupported-font labels retain their existing
layout policy. When no tested layout satisfies the budget, the full text is
retained and the unmet constraints are reported instead of hiding content.

For bound text, `content_fits[].fit.gaps` also reports insufficient ink
separation: adjacent visible lines prefer 0.12 em and icon/text boxes prefer
4 local units along either axis. This can produce a warning even when no
outer padding is exceeded. The icon box is its viewport; the measurement
does not certify opaque pixels or readability after shrinking the diagram.

With `orient: screen`, a label fully supported by the bound font uses its
shaped widths and heights to size the chip, pointer hit rectangle and obstacle
reservation together. Explicit newlines, blank lines and spaces are preserved;
text remains at the requested size, rounded to two decimals for SVG. The chip
keeps at least 8 horizontal and 5 vertical units around the ink, with extra
height for tall marks or multiple lines. Bound-font chips also avoid their
own projected body. Unknown-font or mixed fallback labels retain the existing
estimated single-line layout; they are not certified as shaped blocks.

Screen labels and annotation boxes first try the existing preferred and
peripheral positions. If all fixed candidates collide, a bounded interval
search along four outward rays finds a free position among finite obstacles.
It uses the nearest such position to the preferred location, retaining the
existing direction order for ties, and expands the export bounds as needed.
Dense scenes can therefore place a label farther away; compact global label
assignment and target-scale readability remain separate concerns.

SVG text stays selectable and editable. A composite embeds each used font
weight once; the full TTF resource adds about 330–340 KB per weight to the SVG.
No network font request is needed. Built-in `Rasterize`/`snapshot` loads the
same resource into resvg, or uses Chrome/Chromium when resvg is unavailable.
ImageMagick delegates and external/custom rasterizers must be independently
configured for embedded fonts; an explicit `ISOTOPO_RASTERIZER` keeps its
existing two-argument contract. Default font selection is unchanged.

`orient: iso` paints the label on the iso-tilted top face. `orient:
screen` paints a horizontal label box BELOW the part's projected
bounding box — useful for parts that need a readable caption even
when the iso angle would make on-face text hard to read.

### `effects`

```yaml
effects:
  opacity: 0.9            # 0-1, multiplied across the whole part
  margin:  4              # padding inside the iso box (for content shapes)
  cornerRadius: 8         # rounded corners on box-family parts
  dropShadow: {dx: 0, dy: 10, blur: 16, color: "rgba(45,10,100,0.25)"}
  backglow:   {color: "#FFC857", radius: 80, opacity: 0.55}
  pattern:    {kind: hatch, color: "#FFFFFF", spacing: 8, angle: 30}
```

- `dropShadow` — Gaussian-blurred offset shadow under the part
  (screen units; positive dy = down). Use to lift heroes off the
  canvas.
- `backglow` — a blurred halo behind the silhouette. ONE per scene:
  it marks "the" central element.
- `pattern` — `hatch` or `dots` texture overlaid on box top faces,
  cylinder top ellipses, and circle discs.
- `wireframe: true` — line-art mode: strokes only, no fills. Pair
  with a dashed stroke for ghost volumes and floating frames;
  wireframe/none-filled parts are exempt from overlap warnings.
- `grain: {intensity: 0.45, scale: 0.8}` — monochrome film-grain
  noise on the faces (the "print / risograph" texture). Intensity
  0..1; ~0.4–0.5 keeps dark fills dark. Box, rounded box, cylinder.

## The Theme block

A `Theme` is a `Style` plus optional per-shape-type overrides:

```yaml
theme:
  # Defaults for every part:
  palette: {top: "#FFFFFF", left: "#7E94B5", right: "#A6B7D2"}
  stroke:  {color: "#1F2937", width: 1.2}
  text:    {family: Inter, size: 12, weight: "600", color: "#1F2937"}

  # Per-shape-type overrides (only when shape matches):
  shapes:
    cylinder:
      palette: {top: "#DEE1EB", left: "#0D32B2", right: "#1641C5"}
    person:
      palette: {top: "#EBC8A9", left: "#8E673E", right: "#A77F58"}
    cloud:
      palette: {top: "#FFFFFF", left: "#A5B8DC", right: "#C2CFE6"}
      stroke:  {color: "#5E6791"}
```

## Three-layer cascade

For any part, the renderer computes its final style by merging three
sources in order. Later layers override earlier ones; a missing field
inherits from the layer above:

```
1. theme            (document-level defaults)
2. theme.shapes[s]  (shape-specific defaults — optional)
3. part.style       (per-part overrides — optional)
```

Each merge is field-level. For example, if the theme says
`palette.top: "#FFF"` and the part says only `palette.left: "#888"`,
the final part keeps the white top and gains the gray left.

### Worked example

```yaml
theme:
  palette: {top: "#FFFFFF", left: "#7E94B5", right: "#A6B7D2"}
  shapes:
    cylinder:
      palette: {top: "#DEE1EB", left: "#0D32B2", right: "#1641C5"}

nodes:
  scene:
    shape: composite
    parts:
      - id: db
        shape: cylinder           # picks up the cylinder shape defaults
        style:
          palette: {top: "#FCD34D"}   # ⇐ only override top; keep blue sides
        ...
```

Final palette for `db`:
- top:   `#FCD34D` (from part.style — wins)
- left:  `#0D32B2` (from theme.shapes.cylinder — wins over theme)
- right: `#1641C5` (from theme.shapes.cylinder)

## Stroke and effects cascade the same way

Every sub-block follows the same rule. You can layer a theme that
sets only `stroke.color` with a per-shape default that sets only
`effects.cornerRadius` with a per-part override of `palette.top` —
all four sub-blocks merge independently.

## When you don't set a theme

Skip `theme:` entirely and parts use built-in defaults (white top,
gray sides, dark stroke, Inter font). This is fine for prototyping;
introduce a theme block when you want brand-consistent output.

## When you want NO style at all

Set `style: null` on a part to start from raw defaults, ignoring the
theme. Rarely needed — usually you want some inheritance.

## See also

- [DSL_YAML.md](../reference/dsl-yaml.md) — where `style` blocks slot into a Document
- [DSL_D2.md](../reference/dsl-d2.md) — how d2 styles translate (d2 has its own
  theme system; we map a subset of its fill/stroke into our cascade)
- `isotopo capabilities | jq '.style_keys'` — programmatic source of
  truth for every field name listed above
