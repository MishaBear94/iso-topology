---
title: "Your first scene"
description: "Render a small service architecture from D2 and inspect its output."
---

We've installed `isotopo`. Now we'll render a real architecture
diagram — three layers of services with arrows between them.

## Goal

A `.d2` source with five nodes; one render command; an SVG you can
drop into a doc.

## The DSL

Save this as `services.d2`:

```d2
user:    User { shape: person }
edge:    Edge LB
api:     API Gateway
worker:  Worker
db:      Database { shape: cylinder }

user   -> edge:    HTTPS
edge   -> api:     route
api    -> worker:  enqueue
worker -> db:      write
```

Three things to notice:

1. **Every line that has no `->` declares a node**. `id: Label { shape: ... }` is the full form; `id: Label` is the most common
   short form.
2. **Shapes are picked by name**. `person`, `cylinder`, etc. — the
   full list is in [`reference/dsl-d2.md`](../reference/dsl-d2.md) or
   live via `isotopo capabilities`.
3. **Arrows are `from -> to: label`**. Label is optional.

## Render it

```bash
isotopo render services.d2 ./out
open ./out/topology.html
```

You should see a tilted-iso scene with five elements: a person, two
boxes, a worker, and a database cylinder. Connectors carry the
labels you wrote.

## What just happened

iso-topology compiled your `.d2` via the dagre layout engine,
translated the result into our `Document` model, and rendered the
top-level "scene" composite into one SVG. The output dir has:

- `out/topology.svg` — the scene as a single SVG file
- `out/topology.html` — the SVG side-by-side with your editable DSL
- `out/topology.d2` — a copy of your source (renamed for the
  re-render pipeline)
- `out/nodes/<id>.svg`, `.html`, `.yaml` — one self-contained
  iso sticker per element

Try opening `out/nodes/db.svg` — it's the database cylinder all by
itself. Useful for sticker sheets.

## Validate before render

Make a typo:

```d2
user: User { shape: persen }   # 'persen' instead of 'person'
```

Run validate:

```bash
isotopo validate services.d2
```

You'll get a JSON error with `"suggest": "person"`. Apply, re-run.

## Next step

→ [`03-grouping.md`](./03-grouping.md) — wrap related parts in a
labeled container.
