---
title: "Shapes and geometry"
description: "Geometry parameters, shapes, and the relationship between dimensions and projection."
---

iso-topology ships with a growing library of 3D shape families beyond the
classic box. Each family has a **shape provider** that computes projected
face polygons from a simple set of parameters; all providers share the same
surface-material vocabulary (`style.palette`, `style.faces`, gradients,
patterns, effects).

---

## Shape families at a glance

| Family | Built-in shapes | Key parameter |
|--------|----------------|---------------|
| **Box** | `rectangle`, `square` | — |
| **Prism** | `triprism`, `hexprism`, `octprism` | `geom.sides` |
| **Cylinder** | `cylinder`, `queue`, `stored_data` | — |
| **Circle** | `circle` | — |
| **Cloud** | `cloud` | — |
| **Wedge** | `wedge` | — |
| **Custom path** | `custom_path` | `geom.params.path` |
| **Array** | `array1d`, `array2d`, `array3d` | `countX`, `countY`, `countZ`, `gap` |
| **Rack** | `rack` | `params.slots` |

---

## Prism family (`triprism`, `hexprism`, `octprism`)

A regular n-gon extruded vertically. Each alias fixes the side count.

```yaml
nodes:
  gw:
    shape: hexprism
    geom: { w: 150, d: 150, h: 44 }
    label: "API Gateway"
    style:
      palette: { top: "#EDE9FE", left: "#6D28D9", right: "#8B5CF6" }
```

| Alias | Sides | Semantic use |
|-------|-------|-------------|
| `triprism` | 3 | Alert, one-way dispatch |
| `hexprism` | 6 | API gateway, middleware |
| `octprism` | 8 | Decision node, routing, gate / checkpoint |

---

## Effect pipeline (`style.effects.effects`)

Effects can now be expressed as an **ordered list** in addition to the
individual scalar fields. When the list is present it takes precedence.

```yaml
style:
  effects:
    effects:
      - kind: backglow
        color: "#A78BFA"
        radius: 40
        opacity: 0.5
      - kind: grain
        intensity: 0.2
        scale: 1.0
      - kind: outline
        color: "#DDD6FE"
        width: 2.5
        opacity: 0.9
```

### Supported effect kinds

| Kind | Parameters | Description |
|------|-----------|-------------|
| `backglow` | `color`, `radius`, `opacity` | Diffuse halo behind the shape |
| `blur` | `stdDev` (or `blur`) | Gaussian blur — ghost/fog nodes |
| `grain` | `intensity`, `scale` | Film-grain noise overlay |
| `outline` | `color`, `width`, `dash`, `opacity` | Silhouette accent ring |
| `dropShadow` | `color`, `dx`, `dy`, `blur` | Drop shadow under the shape |

The same kind may appear multiple times in the list; each entry is applied
independently in order. This allows, for example, a tight inner glow
followed by a wide outer glow by listing `backglow` twice with different
radii.

### Priority rules

```
style.effects.effects (list)  overrides  style.effects.backglow (scalar)
style.effects.effects (list)  overrides  style.effects.outline (scalar)
... and so on for every effect kind.
```

Scalar fields (the old API) remain fully supported and are unaffected when
`effects` list is absent.

---

## Wedge (`shape: wedge`)

A right-triangular prism on its side. The bottom is a rectangle at z=0; the
back edge (y=0) rises to full height h; the front edge (y=d) stays at z=0.
This produces a sloped ramp appearance.

**Visible faces:** `slope` (the inclined top face) and `right` (the right
triangular end face). The palette `top` colour fills the slope; `right`
fills the right triangle.

```yaml
nodes:
  ramp:
    shape: wedge
    geom: { w: 160, d: 140, h: 80 }
    label: "Traffic\nRamp"
    style:
      palette: { top: "#FEF3C7", left: "#D97706", right: "#F59E0B" }
      stroke: { color: "#92400E", width: 1.5 }
```

**Semantic guide:** ramp, data ingestion pipeline, traffic escalation,
progressive rollout, CI/CD pipeline stage.

---

## Custom path (`shape: custom_path`)

An arbitrary polygon base extruded vertically. The polygon is defined as an
SVG-subset path string (M/L/Z commands only) in `geom.params.path`. The
path coordinates are automatically normalised to fit the `w × d` footprint.

```yaml
nodes:
  shield:
    shape: custom_path
    geom:
      w: 140
      d: 140
      h: 50
      params:
        path: "M 70,0 L 140,30 L 140,90 L 70,140 L 0,90 L 0,30 Z"
    label: "WAF"
    style:
      palette: { top: "#EFF6FF", left: "#1D4ED8", right: "#2563EB" }
      stroke: { color: "#1E3A8A", width: 1.5 }
```

**Supported path commands:**

| Command | Syntax | Description |
|---------|--------|-------------|
| `M` | `M x,y` | Move to / first point |
| `L` | `L x,y` | Line to |
| `Z` | `Z` | Close path (optional) |

**Face names:** `top` (the top polygon) and `side0` … `sideN-1` (vertical
walls, one per polygon edge). Wall shading follows the same nx ≥ ny →
right-palette, nx < ny → left-palette rule as prisms.

**Fallback:** if `path` is missing or unparseable the shape renders as a
box (rectangle footprint).

**Semantic guide:** any brand or domain-specific shape — shield (WAF),
pentagon (security), hexagon (API), star burst (CDN), custom logo outline.

---

## Surface vocabulary

All shape families support the same `style.faces` overrides for per-face
fills, multi-stop gradients, and multi-layer strokes. Face names depend on
the shape:

| Shape family | Face names |
|-------------|-----------|
| Box | `top`, `left`, `right` |
| Prism | `top`, `side0` … `sideN-1` |
| Cylinder | `top`, `body`, `bottom-cap` |
| Wedge | `slope`, `right` |
| Custom path | `top`, `side0` … `sideN-1` |

```yaml
style:
  faces:
    top:
      fill:
        kind: linearGradient
        stops:
          - { offset: 0, color: "#A78BFA" }
          - { offset: 1, color: "#6D28D9" }
        angle: 135
      stroke:
        layers:
          - { color: "#DDD6FE", width: 1.5, offset: -1 }   # inner highlight
    side0:
      fill: { kind: solid, color: "#4338CA" }
```

See [DSL YAML reference](./dsl-yaml.md) for the full `style.faces` schema.

---

## Array shapes (`shape: array1d` / `array2d` / `array3d`)

Tiled cell grids: `array1d` is a linear row, `array2d` is a flat N×M grid,
`array3d` is a volumetric N×M×K grid. Each cell is a small iso box sharing
the same palette. Painter order is correct for the iso camera.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `countX` | 3 | Number of cells along world X |
| `countY` | 3 (`array2d`/`array3d`) or 1 (`array1d`) | Cells along world Y |
| `countZ` | 1 (`array1d`/`array2d`) or 3 (`array3d`) | Cells along world Z |
| `gap` | 6 | Gap between cells in world units |

```yaml
nodes:
  matrix:
    shape: array2d
    geom: { w: 200, d: 200, h: 24 }
    label: "Shard Matrix"
    params:
      countX: 4
      countY: 4
      gap: 6
    style:
      palette: { top: "#DBEAFE", left: "#1E40AF", right: "#2563EB" }
```

| Shape | Default grid | Semantic use |
|-------|-------------|--------------|
| `array1d` | 3×1×1 | Pipeline stages, replica group |
| `array2d` | 3×3×1 | Shard matrix, node pool, partition table |
| `array3d` | 3×3×3 | GPU cluster, tensor, embedding matrix |

---

## Rack (`shape: rack`)

A server rack: outer frame box with N horizontal slab shelves inside.
Use `params.slots` to set the shelf count.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `slots` | 4 | Number of slot shelves |

```yaml
nodes:
  rack:
    shape: rack
    geom: { w: 80, d: 60, h: 220 }
    label: "Server Rack"
    params:
      slots: 5
    style:
      palette: { top: "#334155", left: "#0F172A", right: "#1E293B" }
      stroke: { color: "#64748B", width: 1.2 }
```

| Family | Built-in shapes | Key parameter |
|--------|----------------|---------------|
| **Array** | `array1d`, `array2d`, `array3d` | `countX`, `countY`, `countZ`, `gap` |
| **Rack** | `rack` | `params.slots` |
