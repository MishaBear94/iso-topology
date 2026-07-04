# Samples index

Generated from `samples/*/*/input.*` header comments — run
`go run ./tools/gen-docs` to refresh. Every fixture is a
golden-tested, copy-paste-ready example; `expected.svg` next to
each input is the rendered output.

Reading order for agents: start with the fixture whose
description matches your task, imitate its structure, then check
[`RECIPES.md`](RECIPES.md) for the primitive-level grammar.

## samples/node

| Fixture | Demonstrates |
|---|---|
| [`array2d`](../../samples/node/array2d/input.yaml) | (no header comment) |
| [`array3d`](../../samples/node/array3d/input.yaml) | (no header comment) |
| [`circle`](../../samples/node/circle/input.yaml) | circle — the iso sphere: one ball with equator shading and a top label. |
| [`cloud`](../../samples/node/cloud/input.yaml) | cloud — free-form rounded outline for external systems; no per-face palette. |
| [`composite`](../../samples/node/composite/input.yaml) | composite — a stat card built from three primitives sharing one bbox. |
| [`custom-path`](../../samples/node/custom-path/input.yaml) | (no header comment) |
| [`cylinder`](../../samples/node/cylinder/input.yaml) | cylinder — the database/queue primitive: elliptical top, curved side wall. |
| [`effect-pipeline`](../../samples/node/effect-pipeline/input.yaml) | (no header comment) |
| [`faces-demo`](../../samples/node/faces-demo/input.yaml) | (no header comment) |
| [`group`](../../samples/node/group/input.yaml) | group — a low-extrusion translucent substrate that wraps nested parts. |
| [`hexprism`](../../samples/node/hexprism/input.yaml) | (no header comment) |
| [`iso-text`](../../samples/node/iso-text/input.yaml) | iso_text — flat text panel (near-zero extrusion); good for titles and captions. |
| [`person`](../../samples/node/person/input.yaml) | person — human actor: sphere head tangent to a hemispherical dome body. |
| [`person-effects`](../../samples/node/person-effects/input.yaml) | person-effects — the person shape speaking the box family's style vocabulary: a body gradient (topGradient shades both dome halves; left/ rightGradient … |
| [`rack`](../../samples/node/rack/input.yaml) | (no header comment) |
| [`rectangle`](../../samples/node/rectangle/input.yaml) | rectangle — the bread-and-butter iso box: three faces, top label, extrusion h. |
| [`wedge`](../../samples/node/wedge/input.yaml) | (no header comment) |

## samples/topology

| Fixture | Demonstrates |
|---|---|
| [`ai-platform`](../../samples/topology/ai-platform/input.yaml) | ai-platform v2 — flagship hero, reimagined with the v3.3 surface pipeline. |
| [`auto-flow`](../../samples/topology/auto-flow/input.yaml) | (no header comment) |
| [`clickhouse-hub`](../../samples/topology/clickhouse-hub/input.yaml) | ClickHouse — real-time data warehouse & database, hub-and-spoke ecosystem map. |
| [`data-fabric`](../../samples/topology/data-fabric/input.yaml) | data-fabric — connector-driven AUTO layout (v4.2). |
| [`devtool-pipeline`](../../samples/topology/devtool-pipeline/input.yaml) | devtool-pipeline — CI/CD neon. |
| [`duckdb-handdrawn`](../../samples/topology/duckdb-handdrawn/input.yaml) | DuckDB — classic hand-drawn data architecture (sketch / whiteboard feel). |
| [`lakehouse-agent`](../../samples/topology/lakehouse-agent/input.yaml) | lakehouse-agent — Databricks Lakehouse + LLM agent application architecture. |
| [`langchain-app`](../../samples/topology/langchain-app/input.yaml) | LangChain — typical agent application architecture, in the LangChain visual language: deep navy-black stage, ONE cool-blue glow accent, hairline-bordered … |
| [`microservice`](../../samples/topology/microservice/input.d2) | microservice — minimal .d2 auto-layout path: person → services → database. |
| [`plan-view-2d`](../../samples/topology/plan-view-2d/input.yaml) | 2D top-down (plan) view — projection declared in the scene's own canvas, so `isotopo render` and the golden test both render it flat. |
| [`platform-board`](../../samples/topology/platform-board/input.yaml) | platform-board — PCB / circuit-board hero shot, landing-page style. |
| [`rag-pipeline`](../../samples/topology/rag-pipeline/input.yaml) | rag-pipeline — a RAG system, now under connector-driven auto-layout. |
| [`theme-clean-light`](../../samples/topology/theme-clean-light/input.yaml) | (no header comment) |
| [`theme-clickhouse-dark`](../../samples/topology/theme-clickhouse-dark/input.yaml) | (no header comment) |
| [`theme-handdrawn-paper`](../../samples/topology/theme-handdrawn-paper/input.yaml) | (no header comment) |
| [`theme-navy-glass`](../../samples/topology/theme-navy-glass/input.yaml) | (no header comment) |
| [`training-compute`](../../samples/topology/training-compute/input.yaml) | training-compute — where the GPU hours of a training run go. |
| [`vpc-boundary`](../../samples/topology/vpc-boundary/input.yaml) | (no header comment) |
