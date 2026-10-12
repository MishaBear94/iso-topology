---
title: "Troubleshooting"
description: "Resolve installation, rendering, validation, and output-quality issues."
---

Quick answers to the failure modes people hit. Search by symptom.

## Install / build

### `go install` says "module found, but does not contain package github.com/MarkovWangRR/iso-topology/cmd/isotopo"

Stop using `@v0.2.0` — that release was broken (`.gitignore` excluded
the CLI source from the zip). Use `@v0.2.1` or later:

```bash
go install github.com/MarkovWangRR/iso-topology/cmd/isotopo@v0.2.1
```

### `go install` says "command not found: isotopo"

The binary went into `$GOBIN` (default `$HOME/go/bin`) but that's not
on your `PATH`. Add this to `~/.zshrc` or `~/.bashrc`:

```bash
export PATH="$HOME/go/bin:$PATH"
```

Verify with `echo $PATH | tr ':' '\n' | grep go`.

### `go install` says "go: requires Go 1.25 or later"

iso-topology builds against Go 1.25+. Either upgrade Go or pin to an
older release if one exists for your toolchain.

### `pkg.go.dev` shows 404 for a fresh release

The proxy → pkg.go.dev pipeline is async; the page renders 5–15
minutes after `proxy.golang.org` caches the release. If you need
faster: visit `https://pkg.go.dev/github.com/MarkovWangRR/iso-topology@vX.Y.Z`
directly — that forces an on-demand fetch.

## Render failures

### `isotopo render` says "isotopo: d2lib compile: ... missing slog.Logger in context"

That's not an error — it's a debug log from d2lib. Your render
succeeded if you see `rendered N node(s) into <out>` after it. The
log is harmless and silenced in v0.2.1+; if you still see it, you're
on an older binary.

### `isotopo render` says "image shape must include an icon field"

The d2 source has `shape: image` without an `icon: <url>` field.
d2 itself rejects this. Either supply an icon URL or pick another
shape:

```d2
icon_node: My Service { shape: image; icon: "https://example.com/icon.svg" }
```

### Output dir contains topology.svg but it's near-empty / 0 bytes

Two common causes:

1. The input had no `nodes:` block at the top level (YAML/JSON).
2. The input's `scene` composite has no `parts:`.

Run `isotopo validate <input>` — it'll flag the structural problem.

### My DSL renders but layout looks wrong

Most "wrong layout" complaints come from one of:

| Symptom | Likely cause | Fix |
|---|---|---|
| Parts overlap inside a group | hand-written offsets too tight | switch the group to `layout: {mode: row\|grid}` or `place` the children — the solver spaces them; `validate` warns with the exact colliding pair |
| Connector skips a face | default anchor is centroid | use `from: api.right-mid` to pin an anchor face |
| Stack only shows one layer | `stack.count` is ≤ 1 (no-op) | set `count: 3, gap: 10` |
| Annotation invisible | anchor id doesn't resolve | run `validate` — it shows "did you mean" |
| Iso grid not drawn | `canvas:` block missing or `grid: none` | add `canvas: {grid: iso, gridColor: "#EEE"}` |

### `.d2` input layout is rigid / vertical column

dagre defaults to top-down. Try ELK for orthogonal routing with
obstacle avoidance:

```bash
isotopo render --layout elk scene.d2 ./out
```

For more layout control, switch to YAML — declarative `layout`/`place`
composition without hand-computed coordinates; see
[`../reference/dsl-yaml.md`](../reference/dsl-yaml.md).

## Validation messages

### "annotation anchor "X" does not match any part id"

The anchor refers to an id that doesn't exist after stack expansion.
Common pitfall: you wrote `anchor: pod` but the stack has 3 layers —
all renderable ids are `pod`, `pod~1`, `pod~2`. The bottom layer is
`pod` (no suffix), top is `pod~<count-1>`.

The `suggest` field in the JSON error tells you the closest valid id.

### "unknown shape "X""

You typo'd a shape name. The `suggest` field carries the nearest
known shape via Levenshtein distance.

```json
{"path": "nodes.scene.parts[0].shape", "message": "unknown shape \"cilinder\"", "suggest": "cylinder"}
```

Full shape inventory: `isotopo capabilities | jq '.shapes[].iso_name'`.

### "unknown grid mode "X""

`canvas.grid` accepts `iso | dots | hatch | solid | none`. Most
typos hit `square` (use `iso`) or `cross` (use `iso`).

### "connector references unknown part id "X""

Same as the annotation anchor case. Common: you nested the part
inside a group and forgot the dot-path:

```d2
cluster: Cluster { api: API }
edge -> api          # ❌ no top-level `api`
edge -> cluster.api  # ✅
```

## Library use

### `import "github.com/MarkovWangRR/iso-topology"` doesn't resolve

If you're behind a corporate proxy or running `GOPROXY=off`, the
public proxy lookup fails. Set `GOPROXY=https://proxy.golang.org,direct`
or add the module to your private mirror.

### My Go code doesn't see the new `Document.Scene()` helper

The method was added in v0.2.1. Bump your `require` and run
`go mod tidy`:

```go.mod
require github.com/MarkovWangRR/iso-topology v0.2.1
```

## Output quality

### SVG opens fine but PNG export looks blurry

Don't rasterize the SVG with `qlmanage` or screenshot tools — they
use 1× DPI. Use `rsvg-convert -w 1600` or headless Chrome with
`--force-device-scale-factor=2`.

### Text labels are clipped by the iso transform

The iso transform tilts the top face, which means long labels
sometimes spill off. Fix options:

- Switch the label to screen-orient (renders as a horizontal box
  under the part):

  ```yaml
  style: {text: {orient: screen, boxBg: "#FFFFFF", boxBorder: "#1F2937"}}
  ```

- Make the part bigger (`geom.w` × `geom.d` both up)
- Shorten the label

## Still stuck?

`isotopo validate` catches ~80% of structural problems before render.
For the remaining 20%, file an issue with:

- Your DSL input (yaml or d2)
- The `isotopo --version` / commit
- What you expected vs what rendered

`isotopo capabilities` output as an attachment never hurts.
