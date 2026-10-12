---
title: "Agent onboarding"
description: "Give a coding agent the installation steps, capabilities, and workflow for drawing diagrams."
---

You're on a fresh machine and you use Claude Code (or any capable
coding agent). You shouldn't have to read install docs — the agent
should do every bit of setup and then teach YOU the workflow. Paste
the block below into Claude, verbatim, and come back when it's done.

````markdown
Set up the iso-topology diagram toolchain on this machine, then teach
me how to use it. Work autonomously; only stop if something needs my
password or a decision only I can make. Reply in the language I use.

## 1 · Install (idempotent — skip whatever is already present)
- Ensure Go ≥ 1.25 (`go version`); if missing, install it with the
  system package manager (macOS: `brew install go`; Debian/Ubuntu:
  `sudo apt install golang-go`; otherwise https://go.dev/dl).
- `go install github.com/MarkovWangRR/iso-topology/cmd/isotopo@main`
- `go install github.com/MarkovWangRR/iso-topology/cmd/isotopo-mcp@main`
- Ensure `$(go env GOPATH)/bin` is on PATH for this session.
- Install the drawing skill so future sessions already know the
  workflow:
  `mkdir -p ~/.claude/skills/draw-iso-diagram && curl -sL https://raw.githubusercontent.com/MarkovWangRR/iso-topology/main/skills/draw-iso-diagram/SKILL.md -o ~/.claude/skills/draw-iso-diagram/SKILL.md`

## 2 · Verify with a real render
- `isotopo capabilities | head -20` must print JSON.
- Render the showcase sample into ./diagrams/hello:
  `curl -sL https://raw.githubusercontent.com/MarkovWangRR/iso-topology/main/samples/topology/ai-platform/input.yaml -o /tmp/hello.yaml && isotopo render /tmp/hello.yaml ./diagrams/hello`
- Open ./diagrams/hello/topology.html and tell me what I should see.

## 3 · From now on, whenever I ask for a diagram
- Read `isotopo capabilities` once per session; imitate the closest
  fixture from
  https://raw.githubusercontent.com/MarkovWangRR/iso-topology/main/docs/agent/SAMPLES.md
  and follow the visual rules in
  https://raw.githubusercontent.com/MarkovWangRR/iso-topology/main/docs/guides/scene-design.md
- Author YAML with layout/place relations ONLY — never hand-computed
  coordinates; connectors are always routing: orthogonal.
- Loop `isotopo validate <file>` until exit 0, then render into
  ./diagrams/<kebab-case-name>/ and give me the topology.html path.
- Keep the YAML next to the output as input.yaml; when I ask for
  changes, edit it and re-render the same folder.

## 4 · Finish by telling me
- three example requests that show off what this tool does well, and
- how I should phrase change requests so you can apply them precisely.
````

That's the whole onboarding. Everything below is what you'll know
AFTER the agent finishes — kept here for reference.

## How to ask for a diagram

A good request names four things — subject, mood, accent, hero:

> Draw my RAG pipeline: docs → ETL → embeddings on a back plane,
> app → retriever → LLM on a front plane, shared vector DB between
> them. **Dark mode, emerald accent, the vector DB is the hero.**

Useful vocabulary the agent understands directly:

| You say | The agent reaches for |
|---|---|
| "X 围绕 Y" / "ring of X around Y" | `layout: ring` |
| "一块面板/棋盘" / "a board of cells" | `group` + `layout: grid` |
| "堆叠副本" / "3 replicas" | `stack: {count: 3}` |
| "X 在 Y 右边/后面/上面" | `place: rightOf / behind / above` |
| "预算/容量幽灵框" | dashed wireframe ghost via `place: above` |
| "暗色霓虹 / 白底印刷 / 电路板风" | the gallery's style registers |

## Where results live, how to iterate

Every diagram lands in `./diagrams/<name>/`:

- `topology.html` — the interactive viewer (zoom/pan, hover a node
  to highlight its source); run `isotopo serve <input.yaml>` and the
  same page re-renders live as the YAML is edited — against a COPY,
  the original file is never touched
- `topology.svg` — drop straight into docs / slides / Notion
- `input.yaml` — the source of truth; your change requests edit this
- `nodes/<id>.svg` — each element as a standalone sticker

Change requests work best when they name parts and relations the way
the YAML does — "move the cache to the right of the gateway",
"make the GPU pool the hero", "swap the accent to violet" — the
agent edits `input.yaml` and re-renders the same folder, so the
`topology.html` you have open just needs a refresh.

## When something looks wrong

| Symptom | What to tell the agent |
|---|---|
| an element is missing | "run `isotopo validate` and fix every issue" — dangling ids and typos come back with did-you-mean suggestions |
| two things overlap | "validate reports the colliding pair — increase that gap" |
| a line cuts across the grid | "all connectors must be routing: orthogonal" |
| text spills or looks cramped | shorten the label, or just say "let the caption sit below the part" (`orient: screen`) |
| wrong vibe entirely | point at a gallery scene: "make it look like clickhouse-hub" |

Prefer doing things by hand instead? The manual path starts at
[01-install.md](./01-install.md).
