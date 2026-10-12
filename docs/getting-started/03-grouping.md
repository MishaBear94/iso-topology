---
title: "Groups and boundaries"
description: "Organize services into nested containers in D2 and YAML."
---

In step 02 we drew five flat services. Real architectures have
**boundaries** — a VPC, a cluster, a region. iso-topology has two
ways to express them:

1. `.d2` nested syntax — auto-translated to iso `group`
2. YAML `shape: group` — for precise placement

We'll do the `.d2` path first because it's the same source you wrote
in step 02 — just one extra brace.

## Wrap the API tier in a VPC

Rewrite `services.d2`:

```d2
user:  User { shape: person }
edge:  Edge LB

vpc: AWS VPC {
  api:    API Gateway
  worker: Worker
  db:     Database { shape: cylinder }
}

user    -> edge:        HTTPS
edge    -> vpc.api:     route
vpc.api -> vpc.worker:  enqueue
vpc.worker -> vpc.db:   write
```

Re-render:

```bash
isotopo render services.d2 ./out
open ./out/topology.html
```

The three services inside `vpc { … }` now sit on a translucent
labeled iso panel. Arrows from outside reference parts via
dot-notation: `vpc.api`, `vpc.worker`, `vpc.db`.

## Nesting goes deeper

You can nest any number of levels:

```d2
cluster: Kubernetes Cluster {
  region_a: Region A {
    pod: API Pod
    cache: Cache { shape: cylinder }
  }
  region_b: Region B {
    pod: API Pod
    cache: Cache { shape: cylinder }
  }
}
```

iso-topology emits one translucent substrate per level — outer
faded, inner stronger. Connectors reference full dot-paths:
`cluster.region_a.pod -> cluster.region_b.pod`.

## YAML equivalent (manual composition)

When you want control over composition that auto-layout can't give
(styled boards, stairs, hero scenes), switch to the YAML DSL. Same
primitive — `shape: group` — but you still don't compute
coordinates: give the group a `layout` and it arranges the children
and sizes its own substrate around them:

```yaml
nodes:
  scene:
    shape: composite
    parts:
      - id: vpc
        shape: group
        label:  "AWS VPC"
        layout: {mode: row, gap: 1.5}
        parts:
          - id: api
            shape: rectangle
            geom:  {w: 120, d: 80, h: 30}
            label: API
          - id: db
            shape: cylinder
            geom:  {w: 100, d: 100, h: 36}
            label: DB
```

`mode: row` marches the children along the iso x-axis; `gap` is in
**cells** (1 cell = the canvas `gridStep`, default 40 world units).
Free-standing parts outside a group are positioned the same
declarative way with `place:`:

```yaml
      - id: monitor
        shape: rectangle
        place: {rightOf: vpc, gap: 2}
        geom:  {w: 100, d: 100, h: 40}
        label: Monitoring
```

Hand-written `offset: {wx, wy}` coordinates still work — but treat
them as a fine-tune delta on top of `place`/`layout`, not the
primary mechanism. Every scene in
[samples/topology](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology) is coordinate-free; the
[langchain-app sample](https://github.com/MishaBear94/iso-topology/blob/main/samples/topology/langchain-app/input.yaml)
shows grid containers, place chains and auto-sizing in one file.

Full grammar: [`reference/dsl-yaml.md`](../reference/dsl-yaml.md).

## Next step

→ [`04-replicas-annotations.md`](./04-replicas-annotations.md) —
auto-replicate parts and add screen-space callouts.
