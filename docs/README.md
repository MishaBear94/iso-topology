# iso-topology docs

This directory is also the source of the official Mintlify documentation site.
Start with [Quickstart](quickstart.mdx), browse the [Gallery](gallery.mdx), or read
[Maintaining the site](MAINTAINING.md) for preview, validation, and deployment.

This tree is organized by purpose, not topic — pick the section that
matches what you're trying to do.

## getting-started/

**You're new and want to render something real, fast.** Walk it in
order — each step builds on the last.

0. [`00-onboarding.md`](getting-started/00-onboarding.md) — fresh
   machine? paste one block into Claude; the agent installs
   everything and teaches you the workflow
1. [`01-install.md`](getting-started/01-install.md) — manual install
   paths (CLI + library)
2. [`02-first-scene.md`](getting-started/02-first-scene.md) — your
   first multi-element scene
3. [`03-grouping.md`](getting-started/03-grouping.md) — wrap related
   parts in a labeled container
4. [`04-replicas-annotations.md`](getting-started/04-replicas-annotations.md) —
   stacks, callouts, ground grid
5. [`05-publishing.md`](getting-started/05-publishing.md) — embed in
   docs / slides / LLM context

## guides/

**You have a specific task — recipes that show one solution.**

- [`scene-design.md`](guides/scene-design.md) — the visual style guide:
  hero/accent rules, composition patterns, presets, pre-ship checklist
- [`extending.md`](guides/extending.md) — add a new shape, primitive,
  or layout engine
- [`troubleshooting.md`](guides/troubleshooting.md) — failure modes
  by symptom

## reference/

**You know what you want — look up the exact field name.**

- [`api.md`](reference/api.md) — **the single API entry point**: every
  programmable surface (Go library + edit contract, HTTP, DSL, CLI, MCP)
- [`cli.md`](reference/cli.md) — CLI subcommands
- [`dsl-yaml.md`](reference/dsl-yaml.md) — YAML composite spec
- [`dsl-d2.md`](reference/dsl-d2.md) — `.d2` input spec (shape
  mapping, nested containers)
- [`dsl-theme.md`](reference/dsl-theme.md) — Style / Theme cascade:
  palette / stroke / text / effects
- [`output-layout.md`](reference/output-layout.md) — output dir
  layout + embedding recipes

## concepts/

**You want to understand the design.**

- [`why-isometric.md`](concepts/why-isometric.md) — design rationale,
  tradeoffs, influences

## agent/

**You're integrating iso-topology into an LLM agent.**

- [`CAPABILITIES.md`](agent/CAPABILITIES.md) — generated machine-readable
  capability inventory (same as `isotopo capabilities`)
- [`RECIPES.md`](agent/RECIPES.md) — task → DSL primitive lookup
- [`PROMPT_TEMPLATE.md`](agent/PROMPT_TEMPLATE.md) — drop-in system
  prompt (capability block auto-generated)
- [`SAMPLES.md`](agent/SAMPLES.md) — generated index of the golden
  fixtures: the few-shot library, one entry per worked scene
- [`ICONS.md`](agent/ICONS.md) — generated index of all built-in
  icons with previews (source of truth: `iso25d/brand_icons.go`)
- [`MCP.md`](agent/MCP.md) — the `isotopo-mcp` server: capabilities /
  validate / render as MCP tools for Claude-family clients
- [`../skills/`](../skills/README.md) — installable Claude Code skill
  encoding the full drawing workflow
- [`schema/dsl.schema.json`](agent/schema/dsl.schema.json) — JSON
  Schema for local lint (no CLI roundtrip)
- [`../llms.txt`](../llms.txt) — generated repo-root self-description
  for generative engines (llmstxt.org)

## How to navigate by question

| If you're asking… | Read |
|---|---|
| "I just want Claude to set everything up" | [getting-started/00-onboarding.md](getting-started/00-onboarding.md) |
| "How do I install it by hand?" | [getting-started/01-install.md](getting-started/01-install.md) |
| "Where do I start?" | [getting-started/02-first-scene.md](getting-started/02-first-scene.md) |
| "How do I express N replicas?" | [agent/RECIPES.md](agent/RECIPES.md) |
| "How do I position parts without coordinates?" | [agent/RECIPES.md](agent/RECIPES.md) § Positioning |
| "Why does my diagram look like clip-art?" | [guides/scene-design.md](guides/scene-design.md) |
| "Is there a full example like my task?" | [agent/SAMPLES.md](agent/SAMPLES.md) |
| "Why isn't my callout showing?" | [guides/troubleshooting.md](guides/troubleshooting.md) |
| "What does palette.left do?" | [reference/dsl-theme.md](reference/dsl-theme.md) |
| "How do I embed/call it from Go, or apply edits?" | [reference/api.md](reference/api.md) |
| "What's the difference between .d2 and .yaml?" | [concepts/why-isometric.md](concepts/why-isometric.md) |
| "How do I add a new iso shape?" | [guides/extending.md](guides/extending.md) |
| "How do I plug this into my agent?" | [agent/PROMPT_TEMPLATE.md](agent/PROMPT_TEMPLATE.md) |

## Documentation style

This tree follows [Diátaxis](https://diataxis.fr/) — four flavors of
docs (tutorial / how-to / reference / explanation) plus a dedicated
agent section. Each flavor has a different goal; mixing them in one
page makes both audiences worse.
