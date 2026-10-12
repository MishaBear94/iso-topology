// gen-docs writes two agent-facing files from the live capability
// surface so they can't drift from code:
//
//	docs/agent/CAPABILITIES.md   markdown rendering of CapabilityReport()
//	docs/agent/schema/dsl.schema.json   JSON Schema describing the YAML DSL
//
// Both are committed to the repo (so they're readable on GitHub raw,
// usable by agents offline). Re-run after any DSL or shape catalog
// change. CI can guard against drift with `git diff --exit-code`.
//
// Usage:
//
//	go run ./tools/gen-docs
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	isotopo "github.com/MarkovWangRR/iso-topology"
	"github.com/MarkovWangRR/iso-topology/iso25d"
)

func main() {
	if err := writeCapabilitiesMarkdown(); err != nil {
		fmt.Fprintln(os.Stderr, "CAPABILITIES.md:", err)
		os.Exit(1)
	}
	if err := writeDSLSchema(); err != nil {
		fmt.Fprintln(os.Stderr, "dsl.schema.json:", err)
		os.Exit(1)
	}
	if err := updatePromptTemplate(); err != nil {
		fmt.Fprintln(os.Stderr, "PROMPT_TEMPLATE.md:", err)
		os.Exit(1)
	}
	if err := writeSamplesIndex(); err != nil {
		fmt.Fprintln(os.Stderr, "SAMPLES.md:", err)
		os.Exit(1)
	}
	if err := writeLLMsTxt(); err != nil {
		fmt.Fprintln(os.Stderr, "llms.txt:", err)
		os.Exit(1)
	}
	if err := writeIconIndex(); err != nil {
		fmt.Fprintln(os.Stderr, "ICONS.md:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "ok: wrote CAPABILITIES.md, dsl.schema.json, SAMPLES.md, llms.txt, ICONS.md + docs/assets/icons/; patched PROMPT_TEMPLATE.md")
}

// writeLLMsTxt emits the repo-root llms.txt (https://llmstxt.org): the
// machine-discoverable self-description generative engines and agents
// fetch first. Derived from CapabilityReport so the facts (shape /
// primitive counts, version) can never drift from the code.
func writeLLMsTxt() error {
	cap := isotopo.CapabilityReport()
	const base = "https://raw.githubusercontent.com/MarkovWangRR/iso-topology/main/"

	var b strings.Builder
	b.WriteString("# iso-topology\n\n")
	fmt.Fprintf(&b, "> iso-topology is an open-source Go CLI and library that renders a small text DSL into design-grade 2.5D isometric SVG architecture diagrams. It is a diagram-as-code tool built agent-first: an LLM can discover the DSL (`isotopo capabilities`), validate documents before render (JSONPath-located issues with fix suggestions; exit codes 0 clean / 2 warnings / 3 errors), and produce deterministic, git-diffable SVG. Capabilities v%s: %d iso shapes, %d composition primitives, declarative layout/place positioning (no hand-computed coordinates), %d built-in icons including real brand logos.\n\n",
		cap.Version, len(cap.Shapes), len(cap.Primitives), len(iso25d.IconCatalog()))
	b.WriteString("Install: `go install github.com/MarkovWangRR/iso-topology/cmd/isotopo@latest` — single static binary, no runtime deps. Inputs: `.yaml` (declarative composition), `.d2` (auto-layout), `.json`. An MCP server (`isotopo-mcp`) exposes capabilities/validate/render as tools.\n\n")

	b.WriteString("## Agent integration\n\n")
	b.WriteString("- [Capabilities inventory](" + base + "docs/agent/CAPABILITIES.md): generated DSL reference — shapes, primitives, style keys\n")
	b.WriteString("- [JSON Schema](" + base + "docs/agent/schema/dsl.schema.json): lint candidate DSL locally before calling the CLI\n")
	b.WriteString("- [Prompt template](" + base + "docs/agent/PROMPT_TEMPLATE.md): drop-in system prompt with positioning rules and output contract\n")
	b.WriteString("- [Samples index](" + base + "docs/agent/SAMPLES.md): golden-tested worked scenes to imitate (few-shot library)\n")
	b.WriteString("- [Recipes](" + base + "docs/agent/RECIPES.md): task → DSL primitive mapping\n")
	b.WriteString("- [Icon index](" + base + "docs/agent/ICONS.md): every built-in glyph and brand badge with previews\n")
	b.WriteString("- [MCP server setup](" + base + "docs/agent/MCP.md): use from Claude / any MCP client without shelling out\n")
	b.WriteString("- [Agent skill](" + base + "skills/draw-iso-diagram/SKILL.md): installable Claude Code skill for drawing iso diagrams\n")
	b.WriteString("- [Studio guide](" + base + "docs/guides/studio.md): `isotopo serve <file>` opens a browser workbench (drag to lay out, right-click to restyle) — agents can't drive it, but should suggest it for human visual tweaks after rendering\n")

	b.WriteString("\n## Reference\n\n")
	b.WriteString("- [API reference](" + base + "docs/reference/api.md): single entry point for every programmable surface — Go library + stateless edit contract (ApplyOp/EditOp/RenderSource), HTTP, DSL, CLI, MCP\n")
	b.WriteString("- [YAML DSL](" + base + "docs/reference/dsl-yaml.md): layout/place composition, every field\n")
	b.WriteString("- [d2 DSL](" + base + "docs/reference/dsl-d2.md): auto-layout input path\n")
	b.WriteString("- [Style and theme](" + base + "docs/reference/dsl-theme.md): palette, gradients, effects cascade\n")
	b.WriteString("- [CLI](" + base + "docs/reference/cli.md)\n")

	b.WriteString("\n## Optional\n\n")
	b.WriteString("- [README](" + base + "README.md): pitch, gallery, FAQ, comparison vs Mermaid/D2\n")
	b.WriteString("- [Onboarding](" + base + "docs/getting-started/00-onboarding.md): one paste-into-Claude block that installs and teaches everything\n")
	b.WriteString("- [Tutorial](" + base + "docs/getting-started/01-install.md): manual path, five steps from install to published scene\n")
	b.WriteString("- [Why isometric](" + base + "docs/concepts/why-isometric.md): design rationale\n")

	return os.WriteFile("llms.txt", []byte(b.String()), 0o644)
}

// writeIconIndex materialises the built-in icon catalog: one real SVG
// file per icon under docs/assets/icons/ plus docs/agent/ICONS.md
// mapping preview → URI → description. The source of truth stays in
// iso25d/brand_icons.go (icons are Go string literals, not files);
// this generated folder+index exists so humans and agents can browse
// what each name looks like without reading Go source.
func writeIconIndex() error {
	cat := iso25d.IconCatalog()
	if err := os.MkdirAll("docs/assets/icons", 0o755); err != nil {
		return err
	}
	written := map[string]bool{}
	for _, ic := range cat {
		fn := fmt.Sprintf("docs/assets/icons/%s-%s.svg", ic.Kind, ic.Name)
		if err := os.WriteFile(fn, []byte(ic.SVG), 0o644); err != nil {
			return err
		}
		written[fn] = true
	}
	// Drop stale files for renamed/removed icons.
	if entries, err := os.ReadDir("docs/assets/icons"); err == nil {
		for _, e := range entries {
			fn := "docs/assets/icons/" + e.Name()
			if !written[fn] {
				_ = os.Remove(fn)
			}
		}
	}

	var b strings.Builder
	b.WriteString("# Built-in icons\n\n")
	b.WriteString("Generated from `iso25d.IconCatalog()` — the icons themselves are\n")
	b.WriteString("maintained as Go string literals in `iso25d/brand_icons.go`, and\n")
	b.WriteString("this index (plus the SVG files in `docs/assets/icons/`) is\n")
	b.WriteString("regenerated by `go run ./tools/gen-docs`. Do not edit by hand.\n\n")
	b.WriteString("Usage on any part: `icon: \"iso://glyph/<name>\"` (ink, for light\n")
	b.WriteString("tops), `iso://glyph/<name>/light` (white, for dark tops),\n")
	b.WriteString("`iso://glyph/<name>/<RRGGBB>` (any color), or\n")
	b.WriteString("`iso://brand/<name>` for letter badges.\n\n")
	b.WriteString("**Custom icons.** Beyond the built-ins, `icon` accepts an http(s)\n")
	b.WriteString("URL, a `data:` URI, or a **local image file path** (svg/png/jpg/\n")
	b.WriteString("gif/webp — absolute or relative, `~` and `file://` accepted). Local\n")
	b.WriteString("paths are read and inlined as a data URI at render time, so the\n")
	b.WriteString("output SVG stays self-contained. In Studio, the Edit-details → Icon\n")
	b.WriteString("field has a **Browse…** button that embeds a picked file directly.\n")

	b.WriteString("\n## Glyphs\n\n")
	b.WriteString("| | Name | URI | Description |\n|---|---|---|---|\n")
	for _, ic := range cat {
		if ic.Kind != "glyph" {
			continue
		}
		fmt.Fprintf(&b, "| <img src=\"../assets/icons/glyph-%s.svg\" width=\"22\"> | `%s` | `%s` | %s |\n",
			ic.Name, ic.Name, ic.URI, ic.Description)
	}
	b.WriteString("\n## Brand logos (Simple Icons)\n\n")
	b.WriteString("Real logos vendored from [Simple Icons](https://simpleicons.org)\n")
	b.WriteString("(CC0-1.0; see `iso25d/icons/si/NOTICE.txt` — brand names and\n")
	b.WriteString("logos may be trademarks of their respective owners). Same color\n")
	b.WriteString("variants as glyphs: `/light` for dark tops, `/<RRGGBB>` for any\n")
	b.WriteString("color. Curate the set in `tools/import-icons/allowlist.txt`.\n\n")
	b.WriteString("| | Name | URI | Description |\n|---|---|---|---|\n")
	for _, ic := range cat {
		if ic.Kind != "si" {
			continue
		}
		fmt.Fprintf(&b, "| <img src=%q width=%q> | `%s` | `%s` | %s |\n",
			"../assets/icons/si-"+ic.Name+".svg", "22", ic.Name, ic.URI, ic.Description)
	}
	b.WriteString("\n## Brand letter badges\n\n")
	b.WriteString("Generated monogram fallbacks for marks that are not vendored.\n\n")
	b.WriteString("| | Name | URI | Description |\n|---|---|---|---|\n")
	for _, ic := range cat {
		if ic.Kind != "brand" {
			continue
		}
		fmt.Fprintf(&b, "| <img src=\"../assets/icons/brand-%s.svg\" width=\"22\"> | `%s` | `%s` | %s |\n",
			ic.Name, ic.Name, ic.URI, ic.Description)
	}
	b.WriteString("\n## Adding an icon\n\n")
	b.WriteString("Add the 24×24 path markup to `glyphIcons` (plus a one-line entry\n")
	b.WriteString("in `glyphDescs` — a unit test fails if you forget) or a row to\n")
	b.WriteString("`brandBadges` in `iso25d/brand_icons.go`, then run\n")
	b.WriteString("`go generate ./...`. The name is addressable immediately.\n")

	return os.WriteFile("docs/agent/ICONS.md", []byte(b.String()), 0o644)
}

// updatePromptTemplate splices a freshly generated "minimal template"
// block (the drop-in system prompt, capability lines included) between
// sentinel comments in docs/agent/PROMPT_TEMPLATE.md. The narrative
// sections around it stay hand-written; the part that restates the
// capability surface is generated so it CANNOT drift from
// CapabilityReport() — the failure mode where the agent prompt taught
// a stale DSL is mechanically impossible now.
func updatePromptTemplate() error {
	const (
		path  = "docs/agent/PROMPT_TEMPLATE.md"
		begin = "<!-- BEGIN GENERATED: minimal-template (gen-docs) -->"
		end   = "<!-- END GENERATED: minimal-template -->"
	)
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(raw)
	bi := strings.Index(s, begin)
	ei := strings.Index(s, end)
	if bi < 0 || ei < 0 || ei < bi {
		return fmt.Errorf("sentinels %q / %q not found in %s", begin, end, path)
	}
	block := begin + "\n" + renderMinimalTemplate(isotopo.CapabilityReport()) + "\n" + end
	out := s[:bi] + block + s[ei+len(end):]
	return os.WriteFile(path, []byte(out), 0o644)
}

func renderMinimalTemplate(cap isotopo.Capabilities) string {
	var b strings.Builder
	b.WriteString("```text\n")
	b.WriteString("You are an assistant that produces iso-topology DSL. iso-topology\n")
	b.WriteString("renders 2.5D isometric architecture diagrams from textual DSL.\n\n")

	fmt.Fprintf(&b, "CAPABILITIES v%s (the only DSL you may emit):\n", cap.Version)
	b.WriteString("- Input formats: .yaml (manual composition) or .d2 (auto-layout).\n")
	isoNames := make([]string, 0, len(cap.Shapes))
	for _, sh := range cap.Shapes {
		isoNames = append(isoNames, sh.IsoName)
	}
	sort.Strings(isoNames)
	fmt.Fprintf(&b, "- Shapes: %s.\n", strings.Join(isoNames, ", "))
	b.WriteString("  (d2 aliases like queue/stored_data/hexagon also accepted — see CAPABILITIES.md.)\n")
	b.WriteString("- Composition primitives (YAML):\n")
	for _, p := range cap.Primitives {
		fmt.Fprintf(&b, "    %-11s %s\n", p.Name+":", p.Syntax)
	}
	b.WriteString("- Style sub-blocks:\n")
	for _, g := range cap.StyleKeys {
		fmt.Fprintf(&b, "    %-9s %s\n", g.Block+":", strings.Join(g.Fields, ", "))
	}

	b.WriteString(`
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
` + "```")
	return b.String()
}

// writeSamplesIndex generates docs/agent/SAMPLES.md from the header
// comment of every samples/*/*/input.* fixture — the golden corpus
// doubles as a few-shot library, and this index is how a human or an
// agent finds the fixture that demonstrates a given primitive.
func writeSamplesIndex() error {
	var b strings.Builder
	b.WriteString("# Samples index\n\n")
	b.WriteString("Generated from `samples/*/*/input.*` header comments — run\n")
	b.WriteString("`go run ./tools/gen-docs` to refresh. Every fixture is a\n")
	b.WriteString("golden-tested, copy-paste-ready example; `expected.svg` next to\n")
	b.WriteString("each input is the rendered output.\n\n")
	b.WriteString("Reading order for agents: start with the fixture whose\n")
	b.WriteString("description matches your task, imitate its structure, then check\n")
	b.WriteString("[`RECIPES.md`](RECIPES.md) for the primitive-level grammar.\n")

	for _, category := range []string{"node", "topology"} {
		fmt.Fprintf(&b, "\n## samples/%s\n\n", category)
		b.WriteString("| Fixture | Demonstrates |\n|---|---|\n")
		root := filepath.Join("samples", category)
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			desc, input := sampleHeader(filepath.Join(root, e.Name()))
			if input == "" {
				continue
			}
			fmt.Fprintf(&b, "| [`%s`](../../samples/%s/%s/%s) | %s |\n",
				e.Name(), category, e.Name(), input, desc)
		}
	}
	return os.WriteFile("docs/agent/SAMPLES.md", []byte(b.String()), 0o644)
}

// sampleHeader returns the fixture's one-line description (the first
// comment lines of its input file, joined until the first blank/non-
// comment line) and the input filename.
func sampleHeader(dir string) (desc, input string) {
	for _, name := range []string{"input.yaml", "input.d2", "input.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var lines []string
		for _, l := range strings.Split(string(raw), "\n") {
			t := strings.TrimSpace(l)
			if !strings.HasPrefix(t, "#") {
				break
			}
			t = strings.TrimSpace(strings.TrimPrefix(t, "#"))
			if t == "" {
				break
			}
			lines = append(lines, t)
			// Stop at the end of the first sentence, or after 4 lines.
			if strings.HasSuffix(t, ".") || len(lines) == 4 {
				break
			}
		}
		d := strings.Join(lines, " ")
		if i := strings.Index(d, ". "); i >= 0 {
			d = d[:i+1]
		}
		if len(d) > 160 {
			if cut := strings.LastIndex(d[:160], " "); cut > 0 {
				d = d[:cut] + " …"
			}
		}
		if d == "" {
			d = "(no header comment)"
		}
		return d, name
	}
	return "", ""
}

// writeCapabilitiesMarkdown renders CapabilityReport() into a
// human-skimmable markdown file. Same data as `isotopo capabilities`
// but committed so it's readable on GitHub without running the CLI.
func writeCapabilitiesMarkdown() error {
	cap := isotopo.CapabilityReport()
	var b strings.Builder

	fmt.Fprintf(&b, "# Capabilities — v%s\n\n", cap.Version)
	b.WriteString("Generated from `CapabilityReport()`. Do not edit by hand — run\n")
	b.WriteString("`go run ./tools/gen-docs` to regenerate after a code change.\n\n")
	b.WriteString("Same content as `isotopo capabilities` JSON, but markdown for skim-reading.\n\n")

	b.WriteString("## Input formats\n\n")
	b.WriteString("| Extension | Layout | Description |\n|---|---|---|\n")
	for _, in := range cap.Inputs {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", in.Extension, in.Layout, in.Description)
	}

	b.WriteString("\n## Layout engines (.d2 input)\n\n")
	b.WriteString("| Name | Edges | Description |\n|---|---|---|\n")
	for _, l := range cap.Layouts {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", l.Name, l.EdgeStyle, l.Description)
	}

	b.WriteString("\n## Shapes\n\n")
	b.WriteString("Each row is one iso shape with every DSL alias accepted. The\n")
	b.WriteString("height hint is the default extrusion multiplier — agents can\n")
	b.WriteString("override per-part via `geom.h`.\n\n")
	b.WriteString("| Iso shape | Accepted aliases | Height hint | Notes |\n|---|---|---|---|\n")
	for _, s := range cap.Shapes {
		aliases := "`" + strings.Join(s.AcceptedAs, "`, `") + "`"
		fmt.Fprintf(&b, "| **%s** | %s | %.1f | %s |\n",
			s.IsoName, aliases, s.HeightHint, s.Notes)
	}

	b.WriteString("\n## Composition primitives\n\n")
	for _, p := range cap.Primitives {
		fmt.Fprintf(&b, "### `%s`\n\n", p.Name)
		fmt.Fprintf(&b, "**Where:** `%s`\n\n", p.Where)
		fmt.Fprintf(&b, "**Syntax:** `%s`\n\n", p.Syntax)
		fmt.Fprintf(&b, "%s\n\n", p.Purpose)
		if len(p.Fields) > 0 {
			b.WriteString("| Field | Meaning |\n|---|---|\n")
			keys := make([]string, 0, len(p.Fields))
			for k := range p.Fields {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, "| `%s` | %s |\n", k, p.Fields[k])
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## Style keys\n\n")
	b.WriteString("Every field under each `style.*` sub-block.\n\n")
	b.WriteString("| Block | Fields |\n|---|---|\n")
	for _, g := range cap.StyleKeys {
		fmt.Fprintf(&b, "| `%s` | %s |\n", g.Block, "`"+strings.Join(g.Fields, "`, `")+"`")
	}

	b.WriteString("\n## Enums\n\n")
	b.WriteString("Closed vocabularies the engine accepts verbatim — a value outside the set " +
		"silently falls back, so emit these exactly. `isotopo validate` rejects deviations " +
		"with a nearest-match suggestion.\n\n")
	b.WriteString("| Key | Values |\n|---|---|\n")
	enumKeys := make([]string, 0, len(cap.Enums))
	for k := range cap.Enums {
		enumKeys = append(enumKeys, k)
	}
	sort.Strings(enumKeys)
	for _, k := range enumKeys {
		fmt.Fprintf(&b, "| `%s` | %s |\n", k, "`"+strings.Join(cap.Enums[k], "`, `")+"`")
	}

	b.WriteString("\n## See also\n\n")
	b.WriteString("- [`RECIPES.md`](RECIPES.md) — task → DSL primitive lookup\n")
	b.WriteString("- [`schema/dsl.schema.json`](schema/dsl.schema.json) — JSON Schema for local lint\n")
	b.WriteString("- [`../reference/dsl-yaml.md`](../reference/dsl-yaml.md) — full YAML grammar with examples\n")

	return os.WriteFile("docs/agent/CAPABILITIES.md", []byte(b.String()), 0o644)
}

// writeDSLSchema writes a JSON Schema (draft 2020-12) describing the
// YAML/JSON Document shape. Hand-written rather than reflected from
// struct tags so we can include semantic guidance (descriptions,
// enums, examples) that pure reflection can't produce.
//
// Agents can use this to lint candidate DSL locally before sending
// to `isotopo validate` — fewer network round-trips, faster loops.
func writeDSLSchema() error {
	cap := isotopo.CapabilityReport()

	// Collect shape names from CapabilityReport for the shape enum.
	allShapes := map[string]struct{}{}
	for _, s := range cap.Shapes {
		allShapes[s.IsoName] = struct{}{}
		for _, a := range s.AcceptedAs {
			allShapes[a] = struct{}{}
		}
	}
	shapeEnum := make([]string, 0, len(allShapes))
	for k := range allShapes {
		shapeEnum = append(shapeEnum, k)
	}
	sort.Strings(shapeEnum)

	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         "https://github.com/MarkovWangRR/iso-topology/blob/main/docs/agent/schema/dsl.schema.json",
		"title":       "iso-topology Document",
		"description": fmt.Sprintf("YAML/JSON DSL accepted by isotopo render. Generated for capabilities v%s.", cap.Version),
		"type":        "object",
		"required":    []string{"nodes"},
		"properties": map[string]any{
			"canvas":      canvasSchema(),
			"theme":       themeSchema(),
			"nodes":       nodesMapSchema(shapeEnum),
			"annotations": annotationsSchema(),
		},
	}

	enc, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("docs/agent/schema", 0o755); err != nil {
		return err
	}
	return os.WriteFile("docs/agent/schema/dsl.schema.json", append(enc, '\n'), 0o644)
}

func canvasSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Document-level backdrop (iso ground grid, solid color, etc.)",
		"properties": map[string]any{
			"background": map[string]any{"type": "string", "description": "Solid background color (CSS color)."},
			"grid":       map[string]any{"type": "string", "enum": []string{"iso", "dots", "hatch", "solid", "none"}, "description": "Backdrop pattern. iso = diamond rhombus lattice."},
			"gridColor":  map[string]any{"type": "string", "description": "Pattern stroke / dot color."},
			"gridStep":   map[string]any{"type": "number", "description": "Tile size in world units (default 40)."},
		},
	}
}

func themeSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Document-wide default Style plus optional per-shape-type overrides.",
		"properties": map[string]any{
			"palette": styleBlockSchema("Face fills (top/left/right), each optionally a {from, to, dir} gradient via topGradient/leftGradient/rightGradient.", []string{"top", "left", "right", "topGradient", "leftGradient", "rightGradient"}),
			"stroke":  styleBlockSchema("Silhouette stroke.", []string{"color", "width", "dash"}),
			"text":    styleBlockSchema("Label typography.", []string{"family", "size", "weight", "color", "orient", "boxBg", "boxBorder"}),
			"effects": styleBlockSchema("Visual modifiers. dropShadow {dx, dy, blur, color}; backglow {color, radius, opacity}; pattern {kind, color, spacing, angle}; wireframe (bool, line-art); grain {intensity, scale} (film-grain noise); faceSplit (bool — rounded boxes only: shade left/right faces independently, needs cornerRadius>0).", []string{"opacity", "margin", "cornerRadius", "dropShadow", "backglow", "pattern", "wireframe", "grain", "faceSplit"}),
			"shapes": map[string]any{
				"type":                 "object",
				"description":          "Per-shape-type Style overrides. Keys are iso shape names (rectangle, cylinder, …).",
				"additionalProperties": styleSchema(),
			},
			"presets": map[string]any{
				"type":                 "object",
				"description":          "Named design-system style presets. Parts reference them with `preset: <name>`; the preset merges between per-shape defaults and the part's own style.",
				"additionalProperties": styleSchema(),
			},
		},
	}
}

func styleBlockSchema(desc string, fields []string) map[string]any {
	props := map[string]any{}
	for _, f := range fields {
		props[f] = map[string]any{"description": "see reference/dsl-theme.md"}
	}
	return map[string]any{
		"type":        "object",
		"description": desc,
		"properties":  props,
	}
}

func styleSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Style block (Palette, Stroke, Text, Effects).",
		"properties": map[string]any{
			"palette": map[string]any{"type": "object"},
			"stroke":  map[string]any{"type": "object"},
			"text":    map[string]any{"type": "object"},
			"effects": map[string]any{"type": "object"},
		},
	}
}

func nodesMapSchema(shapeEnum []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"description":          "Map of node id → Node. The 'scene' node (or the single node if there's only one) is treated as the topology root and picks up Canvas + Annotations.",
		"additionalProperties": nodeSchema(shapeEnum),
	}
}

func nodeSchema(shapeEnum []string) map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"shape"},
		"properties": map[string]any{
			"shape":      map[string]any{"type": "string", "enum": shapeEnum, "description": "One of the iso shapes from capabilities."},
			"geom":       geomSchema(),
			"style":      styleSchema(),
			"preset":     map[string]any{"type": "string", "description": "Name of a theme.presets entry to merge under this node's style."},
			"label":      map[string]any{"type": "string"},
			"icon":       map[string]any{"type": "string"},
			"content":    map[string]any{"type": "object"},
			"parts":      map[string]any{"type": "array", "items": compositePartSchema(shapeEnum), "description": "Children — only consulted when shape == composite or group."},
			"connectors": map[string]any{"type": "array", "items": connectorSchema(), "description": "Directed lines between parts (composite only)."},
			"gridStep":   map[string]any{"type": "number", "description": "Iso grid step for fossflow-style position: {i, j} placement; also the layout/place cell size."},
			"layout":     layoutSchema(),
		},
	}
}

func compositePartSchema(shapeEnum []string) map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"shape"},
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "description": "Stable id — referenced by connectors and annotations."},
			"shape":    map[string]any{"type": "string", "enum": shapeEnum},
			"geom":     geomSchema(),
			"style":    styleSchema(),
			"preset":   map[string]any{"type": "string", "description": "Name of a theme.presets entry to merge under this part's style."},
			"label":    map[string]any{"type": "string"},
			"icon":     map[string]any{"type": "string"},
			"content":  map[string]any{"type": "object"},
			"offset":   worldPointSchema(),
			"position": map[string]any{"type": "object", "properties": map[string]any{"i": map[string]any{"type": "integer"}, "j": map[string]any{"type": "integer"}}},
			"stack":    stackSchema(),
			"parts":    map[string]any{"type": "array", "description": "Nested parts — only honored when shape == group."},
			"place":    placeSchema(),
			"layout":   layoutSchema(),
		},
	}
}

func placeSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Declarative position relative to a SIBLING part — preferred over offset. rightOf/leftOf pin world x, inFrontOf/behind pin world y (front = toward viewer). Chains solve topologically; offset degrades to a fine-tune delta.",
		"properties": map[string]any{
			"rightOf":   map[string]any{"type": "string", "description": "Sibling part id — this part sits on its +x side."},
			"leftOf":    map[string]any{"type": "string", "description": "Sibling part id — -x side. Mutually exclusive with rightOf."},
			"inFrontOf": map[string]any{"type": "string", "description": "Sibling part id — +y side (toward viewer)."},
			"behind":    map[string]any{"type": "string", "description": "Sibling part id — -y side. Mutually exclusive with inFrontOf."},
			"above":     map[string]any{"type": "string", "description": "Sibling part id — this part sits flush ON TOP of it (z = its top), centred on its footprint unless x/y are also pinned."},
			"gap":       map[string]any{"type": "number", "description": "Distance from the sibling's footprint in CELLS (1 cell = gridStep, default 40 world units). Default 1."},
			"gapX":      map[string]any{"type": "number", "description": "Overrides gap on the x axis only."},
			"gapY":      map[string]any{"type": "number", "description": "Overrides gap on the y axis only."},
			"align":     map[string]any{"type": "string", "enum": []string{"start", "center", "end"}, "description": "Alignment along the unconstrained axis (default center)."},
		},
	}
}

func layoutSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Auto-arrange this container's parts along the iso ground axes — no hand-computed coordinates. On a group, geom.w/d may be omitted (substrate auto-sizes around content).",
		"required":    []string{"mode"},
		"properties": map[string]any{
			"mode":    map[string]any{"type": "string", "enum": []string{"row", "column", "grid", "ring", "auto"}, "description": "row = world +x, column = world +y, grid = row-major wrap, ring = first child at the centre with the rest on a circle around it, auto = connector-driven layering of the whole scene (v4.2)."},
			"engine":  map[string]any{"type": "string", "enum": []string{"elk", "dagre"}, "description": "v5 — scene-root mode auto only: delegate the arrangement to d2's embedded ELK / dagre engine (offline, deterministic, grid-snapped). Prefer elk for dense DAGs; omit for the built-in solver."},
			"cols":    map[string]any{"type": "integer", "description": "Grid mode only. Default ceil(sqrt(n))."},
			"gap":     map[string]any{"type": "number", "description": "Space between children in cells. Default 1."},
			"padding": map[string]any{"type": "number", "description": "Content inset from container edge in cells. Defaults to gap."},
			"align":   map[string]any{"type": "string", "enum": []string{"start", "center", "end"}, "description": "Cross-axis alignment within each track (default center)."},
		},
	}
}

func geomSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"w":     map[string]any{"type": "number", "description": "Iso world X extent."},
			"d":     map[string]any{"type": "number", "description": "Iso world Y (depth) extent."},
			"h":     map[string]any{"type": "number", "description": "Iso world Z (height) extrusion."},
			"sides": map[string]any{"type": "integer", "description": "Polygon sides (polygon shape only)."},
		},
	}
}

func worldPointSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wx": map[string]any{"type": "number"},
			"wy": map[string]any{"type": "number"},
			"wz": map[string]any{"type": "number"},
		},
	}
}

func stackSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"count"},
		"properties": map[string]any{
			"count": map[string]any{"type": "integer", "minimum": 1, "description": "Total number of layers."},
			"gap":   map[string]any{"type": "number", "description": "World-Z step between layers. Defaults to part.geom.h + 4 if omitted."},
		},
	}
}

func connectorSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"from", "to"},
		"properties": map[string]any{
			"from":            map[string]any{"type": "string", "description": "Source part id (or id.anchor for a specific face center)."},
			"to":              map[string]any{"type": "string", "description": "Destination part id (same syntax)."},
			"label":           map[string]any{"type": "string"},
			"labelBg":         map[string]any{"type": "string"},
			"labelColor":      map[string]any{"type": "string"},
			"labelFontSize":   map[string]any{"type": "number", "description": "Default 13 in iso, 10 in top."},
			"labelFontFamily": map[string]any{"type": "string", "description": "Source Sans Pro opts into embedded font resources, measured multiline labels and continuous inline placement. Unknown fonts/scripts remain estimated."},
			"labelFontWeight": map[string]any{"type": "string", "description": "Default 600. Embedded font weights: 400, 600, 700; normal and bold aliases."},
			"arrow":           map[string]any{"type": "string", "enum": []string{"none", "triangle"}},
			"routing":         map[string]any{"type": "string", "enum": []string{"straight", "orthogonal", "orthogonal2", "bezier"}, "description": "orthogonal bends along iso ground axes (grid-aligned); bezier = soft quadratic arc."},
			"waypoints3d":     map[string]any{"type": "array", "maxItems": 512, "items": worldPointSchema(), "description": "Explicit interior XYZ world corners for orthogonal/orthogonal2. Omitted coordinates are absolute zero. A nonempty list overrides waypoints, bend and automatic routing; endpoints remain resolved from from/to."},
			"stroke": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"color": map[string]any{"type": "string"},
					"width": map[string]any{"type": "number"},
					"dash":  map[string]any{"type": "string", "description": "SVG stroke-dasharray syntax e.g. \"4 3\"."},
				},
			},
		},
	}
}

func annotationsSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":     "object",
			"required": []string{"anchor", "text"},
			"properties": map[string]any{
				"anchor":   map[string]any{"type": "string", "description": "Id of the part the callout points at."},
				"text":     map[string]any{"type": "string", "description": "Multi-line via \\n."},
				"side":     map[string]any{"type": "string", "enum": []string{"top", "right", "bottom", "left"}},
				"distance": map[string]any{"type": "number"},
				"bg":       map[string]any{"type": "string"},
				"border":   map[string]any{"type": "string"},
				"color":    map[string]any{"type": "string"},
				"fontSize": map[string]any{"type": "number"},
			},
		},
	}
}
