---
title: "D2"
description: "Use D2 graph syntax with automatic layout and isometric rendering."
---

`.d2` is the [terrastruct/d2](https://github.com/terrastruct/d2) graph
DSL. iso-topology accepts `.d2` files as input and runs the d2
auto-layout pipeline, then translates the laid-out diagram into our
iso composite model.

**Why this path exists**: agents and code generators usually output
graph structure (nodes + edges), not pixel-precise layouts. The `.d2`
path lets them stay in their comfort zone and get an iso scene for
free.

For pixel-precise authoring, use the YAML path instead: [DSL_YAML.md](../reference/dsl-yaml.md).

## Quick example

```d2
user: User { shape: person }
api:  API Gateway
queue: Job Queue { shape: queue }
worker: Worker
db:    Database { shape: cylinder }
cache: Cache    { shape: cylinder }

user -> api: request
api  -> queue: enqueue
api  -> cache: lookup
queue -> worker
worker -> db:    write
worker -> cache: update
```

```bash
isotopo render scene.d2 ./out
```

Output: a `topology.svg` with all six elements iso-projected, dagre
auto-layout, an iso grid backdrop, and labeled connectors.

## What translates and how

### Shapes

The `mapD2Shape` catalog turns every d2 shape string into an iso
shape plus a height multiplier. Discover the full list with:

```bash
isotopo capabilities | jq '.shapes[]'
```

Highlights:

| d2 shape | Iso lowering | Height hint |
|---|---|---|
| `rectangle`, `square`, `package`, `sequence_diagram`, `hierarchy` | `rectangle` | 1.0 |
| `cylinder`, `queue`, `stored_data` | `cylinder` | 1.0 |
| `circle` | `circle` | 1.0 |
| `person`, `c4-person` | `person` | 1.2 (taller) |
| `cloud` | `cloud` | 0.8 |
| `class`, `sql_table` | `rectangle` | 1.4 (taller — shows rows on side face) |
| `text`, `code`, `image` | low rectangle / iso_text | 0.3 |
| `document`, `page` | flat rectangle | 0.4–0.5 |
| `hexagon`, `step` | rectangle (no native polygon yet) | 0.6–0.7 |
| `callout` | flat rectangle | 0.5 |

Unknown d2 shape names fall back to `rectangle` with the default
height. Skipping `mapD2Shape` is the right move for prototyping; you
can fill in better lowerings later without touching anything else.

### Connections

d2 connections become iso `Connector` entries:

```d2
api -> db: query
```

→ a connector with `arrow: triangle` and the label `"query"`. Stroke
color and dash come from the d2 style (or theme defaults if neither
side specified one).

### Nested containers → iso groups

d2 uses dot-notation OR brace-blocks to express containers:

```d2
cluster: Kubernetes Cluster {
  wn1: Worker Node 1 {
    proxy1: LiteLLM Proxy
    cache1: Cache { shape: cylinder }
  }
  ingress: Ingress Controller
}

users: Users { shape: person }
users -> cluster.ingress
cluster.ingress -> cluster.wn1.proxy1
```

iso-topology detects the hierarchy from the dot-separated shape ids
and emits one iso `group` per container level. The above produces:

- Kubernetes Cluster (outer translucent panel)
  - Worker Node 1 (inner panel)
    - LiteLLM Proxy + Cache (leaves)
  - Ingress Controller (leaf)
- Users (sibling of Cluster)

Nesting depth is unlimited.

### Connector targets after nesting

Use the full dot-path:

```d2
cluster.ingress -> cluster.wn1.proxy1
```

This translates verbatim — the iso connector's `From`/`To` keep the
full `cluster.wn1.proxy1` id. Validation and the per-part output use
that same id space.

## Layout engine

```bash
isotopo render --layout dagre scene.d2 ./out   # default
isotopo render --layout elk   scene.d2 ./out
```

| Engine | Best when |
|---|---|
| `dagre` | small/medium graphs; natural-bend polyline edges read as flowy |
| `elk` | denser graphs; orthogonal right-angle routing with obstacle avoidance |

Both engines are built into the binary; no extra install needed.

## What doesn't translate (yet)

These d2 features are silently dropped or simplified:

- **Icons**: d2's `icon: <url>` field is ignored. The iso primitive
  for the shape renders without an embedded image.
- **Shadow / 3D / multiple / double-border**: d2 has these as visual
  modifiers; iso renders the base shape only.
- **Image shapes**: require an `icon:` field; without one d2 itself
  rejects the source, so the case never reaches our translator.
- **Class / SQL Table rows**: d2 paints them on the front face; iso
  renders the rectangle envelope only (the row content is dropped).
  Workaround: use the YAML path and set `content.rows: [...]`.

If you hit one of these and want it lowered properly, see
[EXTENDING.md](../guides/extending.md) for the recipe — these are exactly the
gaps the catalog/translator architecture is designed to absorb.

## Inspecting the translation

`out/topology.d2` is a copy of the source DSL (so re-rendering later
doesn't need the original). Past that, the layout is opaque — d2
computes positions, we project them. If you need to debug layout,
run the standalone d2 CLI on the same `.d2` file and inspect its 2D
output first.

## Round-trip back to YAML

Each part also gets emitted as a per-part YAML fragment:

```
out/nodes/cluster.wn1.proxy1.yaml
```

That fragment is a self-contained iso-topology Document — you can
feed it to `isotopo render` to get the standalone iso of just that
proxy. Useful for building icon libraries from a generated topology.

See [OUTPUTS.md](../reference/output-layout.md) for the full output layout.
