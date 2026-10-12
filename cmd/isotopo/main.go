// Command isotopo — DSL → iso topology renderer.
//
// Usage:
//
//	isotopo render [--layout dagre|elk] <input.yaml|input.d2|-> <output-dir>
//	isotopo capabilities                       # structured JSON of supported features
//
// The render subcommand output directory is laid out as two tiers:
//
//	<out>/
//	├── topology.svg              # the full scene
//	├── topology.html             # embed snippet + editable DSL source
//	├── topology.<yaml|d2>        # source copy (for re-rendering)
//	└── nodes/
//	    ├── _index.html           # gallery of all per-part SVGs
//	    ├── <id>.svg              # standalone iso element
//	    ├── <id>.html             # embed snippet + per-part DSL fragment
//	    └── <id>.yaml             # per-part DSL fragment
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	isotopo "github.com/MarkovWangRR/iso-topology"
)

// CLI flag state — set by parseFlags. Layout is consulted only for .d2
// inputs (YAML/JSON have no layout step).
var (
	flagLayout     = "dagre"
	flagProjection = ""    // "" | iso | top — overrides canvas.projection
	flagRepair     = true  // projection-repair loop runs by default (L1); --no-repair opts out
	flagReport     = false // L2: emit report.json (R breakdown + located defects + patches)
	flagReadable   = false // --readable: legibility-first "documentation" profile (#11)
	flagWrite      = false // repair: persist fixes into the source file
	flagTheme      = ""    // --theme <name>: layer a built-in theme under the doc
	flagCompose    = false // repair: also run the composition pass (alignment snapping)
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "render":
		args, err := parseFlags(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			usage()
			os.Exit(2)
		}
		if len(args) != 2 {
			usage()
			os.Exit(2)
		}
		code, err := renderFile(args[0], args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		os.Exit(code)
	case "snapshot":
		args, err := parseFlags(os.Args[2:])
		if err != nil || len(args) != 2 {
			usage()
			os.Exit(2)
		}
		code, err := snapshotFile(args[0], args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		os.Exit(code)
	case "capabilities":
		if err := emitCapabilities(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "serve":
		args, err := parseFlags(os.Args[2:])
		if err != nil || len(args) != 1 {
			usage()
			os.Exit(2)
		}
		if err := serveFile(args[0]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "repair":
		// isotopo repair <input.yaml> [--write]
		//   dry-run: print the fix report as JSON, touch nothing.
		//   --write: persist the fixes into the source file, comment-preserved.
		// exit: 0 = nothing to repair, 2 = repairs found (applied with --write).
		args, err := parseFlags(os.Args[2:])
		if err != nil || len(args) != 1 {
			usage()
			os.Exit(2)
		}
		code, err := repairFile(args[0], flagWrite)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		os.Exit(code)
	case "validate":
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		code, err := validateFile(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		os.Exit(code)
	case "evaluate":
		// isotopo evaluate <input> [output-dir]
		//   prints the plan-view layout scorecard as JSON; if output-dir is
		//   given, also writes plan.svg with crossings/tunnelling marked red.
		if len(os.Args) < 3 || len(os.Args) > 4 {
			usage()
			os.Exit(2)
		}
		outDir := ""
		if len(os.Args) == 4 {
			outDir = os.Args[3]
		}
		if err := evaluateFile(os.Args[2], outDir); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "preview":
		// isotopo preview [--projection iso|top] <input> <out.svg> <id> [id...]
		//   render a SUBSET of the scene (parts / containers / edge:N) to one SVG.
		args, err := parseFlags(os.Args[2:])
		if err != nil || len(args) < 3 {
			usage()
			os.Exit(2)
		}
		if err := previewFile(args[0], args[1], args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

// previewFile renders a SUBSET of the input's scene — selected by part id,
// container id, or "edge:N" — to a single SVG file. The selection is
// re-laid-out and cropped on its own, so the output is a standalone preview
// of just those elements (and any wire between them).
func previewFile(in, outSVG string, ids []string) error {
	var data []byte
	var err error
	if in == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(in)
	}
	if err != nil {
		return err
	}
	sourceLang, _ := classifyInput(in)
	doc, err := loadDocument(sourceLang, data)
	if err != nil {
		return err
	}
	// --projection top previews the flat plan view of the subset.
	if flagProjection != "" {
		if doc.Canvas == nil {
			doc.Canvas = &isotopo.Canvas{}
		}
		doc.Canvas.Projection = flagProjection
	}
	svg, err := isotopo.RenderSubgraph(doc, ids)
	if err != nil {
		return err
	}
	if outSVG == "-" {
		_, err = os.Stdout.WriteString(svg)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outSVG), 0o755); err != nil {
		return err
	}
	if err := writeFile(outSVG, []byte(svg)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "previewed %d selection(s) into %s\n", len(ids), outSVG)
	return nil
}

// evaluateFile scores the document's auto-layout connection quality from the
// flat top-down geometry and prints the scorecard as JSON. With outDir set, it
// also writes an annotated plan.svg (crossings + tunnelling edges in red).
func evaluateFile(in, outDir string) error {
	var data []byte
	var err error
	if in == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(in)
	}
	if err != nil {
		return err
	}
	sourceLang, _ := classifyInput(in)
	doc, err := loadDocument(sourceLang, data)
	if err != nil {
		return err
	}
	scene := doc.Scene()
	if scene == nil {
		return fmt.Errorf("document has no scene to evaluate")
	}
	svg, planReport := isotopo.RenderPlanAnnotated(scene, doc.Theme, doc.Canvas)
	isoReport := isotopo.EvaluateIso(scene, doc.Theme, doc.Canvas)
	// readability = the single iso-space objective the engine optimizes toward
	// (docs/design/layout-engine-master-plan.md). plan = the simplified preview
	// router; iso = the engine's REAL routes. Emitting all three makes the
	// objective and the gap explicit.
	readability := isotopo.Readability(doc)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{
		"readability": readability,
		"plan":        planReport,
		"iso":         isoReport,
		// composition = the POSITIVE aesthetics half (balance / alignment /
		// rhythm / aspect / hero dominance / color discipline), report-only,
		// with located findings an agent can act on.
		"composition": isotopo.EvaluateComposition(doc),
	}); err != nil {
		return err
	}
	if outDir != "" {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(outDir, "plan.svg"), []byte(svg)); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "wrote", filepath.Join(outDir, "plan.svg"))
	}
	return nil
}

// repairFile runs the projection-repair loop against the file and either
// reports the fixes (dry-run) or persists them into the source, comment-
// preserved. Exit codes: 0 = already clean, 2 = repairs found (and applied
// when write is set) — so an agent can gate on the code alone.
func repairFile(in string, write bool) (int, error) {
	data, err := os.ReadFile(in)
	if err != nil {
		return 1, err
	}
	lang, _ := classifyInput(in)
	out, fixes, err := isotopo.RepairSourceWithOptions(lang, data, isotopo.RepairOptions{Compose: flagCompose})
	if err != nil {
		return 1, err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"fixes":   fixes,
		"changed": len(fixes) > 0,
		"written": write && len(fixes) > 0,
	})
	if len(fixes) == 0 {
		return 0, nil
	}
	if write {
		if err := os.WriteFile(in, out, 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(os.Stderr, "repair: %d fix(es) written to %s\n", len(fixes), in)
	} else {
		fmt.Fprintf(os.Stderr, "repair: %d fix(es) available — re-run with --write to persist\n", len(fixes))
	}
	return 2, nil
}

// validateFile parses the input and runs Validate, emitting structured
// JSON of any issues. Exit code: 0 = clean, 2 = warnings only, 3 = any
// errors. Designed for agent CI loops.
func validateFile(in string) (int, error) {
	var data []byte
	var err error
	if in == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(in)
	}
	if err != nil {
		return 1, err
	}
	sourceLang, _ := classifyInput(in)
	doc, err := loadDocument(sourceLang, data)
	if err != nil {
		// Parse failure is a single hard error — emit as one Issue so
		// agents have a uniform JSON contract.
		out := map[string]any{
			"issues": []any{
				map[string]any{
					"severity": "error",
					"path":     "$",
					"message":  fmt.Sprintf("parse failed: %s", err),
				},
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 3, nil
	}
	issues := isotopo.Validate(doc)
	if sourceLang != "d2" {
		issues = append(issues, isotopo.UnknownKeyIssues(data)...)
	}
	// Flag issues the projection-repair loop clears on its own, so an agent can
	// tell "run `isotopo repair --write`" apart from "edit the source by hand".
	if repairedDoc, err := loadDocument(sourceLang, data); err == nil {
		isotopo.RepairAndReport(repairedDoc)
		after := isotopo.Validate(repairedDoc)
		if sourceLang != "d2" {
			after = append(after, isotopo.UnknownKeyIssues(data)...)
		}
		issues = isotopo.MarkRepairable(issues, after)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"issues": issues})
	for _, i := range issues {
		if i.Severity == isotopo.SeverityError {
			return 3, nil
		}
	}
	if len(issues) > 0 {
		return 2, nil
	}
	return 0, nil
}

// emitCapabilities writes the structured capability report as
// pretty-printed JSON. Agents call this at startup to learn what
// shapes, primitives, layouts, and style keys are available without
// reading source.
func emitCapabilities(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(isotopo.CapabilityReport())
}

// parseFlags strips known flags from argv and returns the positional
// remainder. Kept hand-rolled (no `flag` package) because the CLI has
// exactly one optional flag and we want it to appear in any position.
func parseFlags(argv []string) ([]string, error) {
	positional := make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--layout":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--layout requires a value")
			}
			flagLayout = argv[i+1]
			i++
		case strings.HasPrefix(a, "--layout="):
			flagLayout = strings.TrimPrefix(a, "--layout=")
		case a == "--projection":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--projection requires a value")
			}
			flagProjection = argv[i+1]
			i++
		case strings.HasPrefix(a, "--projection="):
			flagProjection = strings.TrimPrefix(a, "--projection=")
		case a == "--repair":
			flagRepair = true
		case a == "--no-repair":
			flagRepair = false
		case a == "--report":
			flagReport = true
		case a == "--readable":
			flagReadable = true
		case a == "--write":
			flagWrite = true
		case a == "--compose":
			flagCompose = true
		case a == "--theme":
			if i+1 >= len(argv) {
				return nil, fmt.Errorf("--theme requires a value")
			}
			flagTheme = argv[i+1]
			i++
		case strings.HasPrefix(a, "--theme="):
			flagTheme = strings.TrimPrefix(a, "--theme=")
		default:
			positional = append(positional, a)
		}
	}
	switch flagLayout {
	case "dagre", "elk":
	default:
		return nil, fmt.Errorf("invalid --layout %q (want dagre|elk)", flagLayout)
	}
	switch flagProjection {
	case "", "iso", "top":
	default:
		return nil, fmt.Errorf("invalid --projection %q (want iso|top)", flagProjection)
	}
	return positional, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  isotopo render [--layout dagre|elk] [--projection iso|top] [--readable] [--no-repair] [--report] <input.yaml|input.d2|-> <output-dir>
  isotopo snapshot [--no-repair] [--report] <input> <output-dir>
  isotopo capabilities

input formats:
  .yaml / .json  manual iso composite (precise placement)
  .d2            d2 graph source (auto-layout)

flags:
  --layout dagre      natural-bend polyline edges (default)
  --layout elk        orthogonal right-angle edges with obstacle avoidance
  --projection iso    2.5D isometric view (default)
  --projection top    flat top-down plan view (footprints + orthogonal edges)
  --readable          legibility-first "documentation" profile: upright screen
                      labels with a canvas-aware contrast chip + a padding floor
                      (opt-in; only fills gaps the author left blank)
  --theme <name>      layer a built-in design system under the document: role
                      styles (hero/tray/chip), text defaults, and the matching
                      canvas when the doc declares none. Names: run
                      "isotopo capabilities" (themes). Also available in-DSL
                      as theme: { use: <name> }

subcommands:
  render         render an input file to <output-dir> (auto-repairs by default;
                 --no-repair opts out, --report writes report.json)
  snapshot       render + rasterize to a FAITHFUL deterministic topology.png
                 (viewport == viewBox, no trim) — a trustworthy image to look at,
                 not a mis-cropping rasterization. Needs resvg (or ImageMagick).
  capabilities   emit structured JSON of supported shapes, primitives,
                 layouts, and style keys — intended for agents to read
                 before generating DSL
  validate <in>  parse + structural validate the input file. Emits JSON
                 of issues with paths and "did you mean" suggestions;
                 issues the repair loop can clear carry "repairable": true.
                 exit: 0 = clean, 2 = warnings only, 3 = errors
  repair <in> [--compose] [--write]
                 run the projection-repair loop (occlusions, overlaps,
                 label contrast) and persist the fixes into the source
                 file, comment-preserved. --compose additionally runs the
                 composition pass: bounded alignment snapping of
                 explicitly-offset parts onto neighbours' tracks (raises
                 evaluate's composition score; never creates overlaps).
                 Without --write, dry-run: print the fix report only.
                 exit: 0 = already clean, 2 = repairs found
  evaluate <in> [out-dir]
                 score auto-layout connection quality from the flat plan
                 view (crossings, edges-through-nodes, backward edges,
                 lengths, bends). JSON to stdout; with out-dir also writes
                 plan.svg with problems marked in red
  preview [--projection iso|top] <in> <out.svg> <id> [id...]
                 render a SUBSET of the scene to one SVG — a single node, a
                 container group (brings its whole subtree), or "edge:N" (the
                 connector plus both endpoints). Connectors whose endpoints
                 are both inside the selection are kept. The subset is
                 re-laid-out and cropped on its own. out.svg "-" writes stdout
  serve <in>     local live-preview server (default :8731, override with
                 ISOTOPO_PORT): the interactive topology.html with hover
                 source-mapping, zoom/pan, edit-to-re-render against an
                 in-browser COPY (the input file is never written),
                 SVG/PNG export of the edited canvas, and the per-node
                 gallery at /nodes/`)
}

// renderFile renders the input into outDir and returns a process exit
// code alongside any I/O error. The exit code mirrors `validate`'s
// contract so an agent can gate on `render` alone:
//
//	0  rendered a non-empty scene with no validation errors
//	3  validation errors were present, OR the document produced no scene
//	1  an I/O / parse failure (returned as the error)
//
// Warnings alone do not fail the render (exit stays 0). Rendering still
// writes whatever it produced even on a code-3 result, so the partial
// output is available for inspection — it just is no longer reported as
// success.
// snapshotFile (L3 of the agent-loop harness plan) renders the scene and then
// rasterizes it to a FAITHFUL, deterministic PNG: viewport == the SVG viewBox,
// NO auto-trim — so the image's geometry matches the SVG exactly and the agent
// can trust what it sees, retiring the qlmanage→magick→trim pipeline that
// mis-crops and lies. Repair + report run as for `render`.
func snapshotFile(in, outDir string) (int, error) {
	code, err := renderFile(in, outDir)
	if err != nil || code != 0 {
		return code, err
	}
	svg := filepath.Join(outDir, "topology.svg")
	png := filepath.Join(outDir, "topology.png")
	if err := rasterize(svg, png); err != nil {
		return 1, err
	}
	fmt.Fprintln(os.Stderr, "snapshot →", png)
	return 0, nil
}

// rasterize delegates to the library's rasterizer (resvg → magick → headless
// Chrome/Chromium → $ISOTOPO_RASTERIZER), keeping the SVG's intrinsic size 1:1.
func rasterize(svg, png string) error {
	return isotopo.Rasterize(svg, png)
}

func renderFile(in, outDir string) (int, error) {
	var data []byte
	var err error
	if in == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(in)
	}
	if err != nil {
		return 1, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 1, err
	}
	nodesDir := filepath.Join(outDir, "nodes")
	if err := os.MkdirAll(nodesDir, 0o755); err != nil {
		return 1, err
	}

	sourceLang, sourceExt := classifyInput(in)
	doc, err := loadDocument(sourceLang, data)
	if err != nil {
		return 1, err
	}
	// --theme: layer a built-in design system (roles + text + canvas when the
	// doc declares none) under whatever the doc already styles — how a plain
	// .d2 graph or bare topology gets the full themed look from one flag.
	if flagTheme != "" {
		if err := isotopo.ApplyTheme(doc, flagTheme); err != nil {
			return 1, err
		}
	}
	// --readable: layer the legibility-first documentation profile (screen
	// labels + contrast chip + padding floor) over the loaded doc before repair
	// and render (#11). Opt-in; only fills gaps the author left blank.
	if flagReadable {
		isotopo.ApplyReadableProfile(doc)
	}
	// Resolve the projection before diagnostics and rendering so their
	// collected screen-space geometry belongs to the same view.
	if flagProjection != "" {
		if doc.Canvas == nil {
			doc.Canvas = &isotopo.Canvas{}
		}
		doc.Canvas.Projection = flagProjection
	}
	if flagRepair {
		if iters, fixed := isotopo.RepairAndReport(doc); len(fixed) > 0 {
			fmt.Fprintf(os.Stderr, "repaired (%d fix(es), %d iteration(s)):\n", len(fixed), iters)
			for _, f := range fixed {
				fmt.Fprintf(os.Stderr, "  - %s\n", f)
			}
		}
	}
	// Report screen labels come from the same emission as topology.svg.
	var topologySVG string
	if flagReport {
		scene, report := isotopo.RenderSceneWithReport(doc)
		topologySVG = scene.SVG
		js, _ := json.MarshalIndent(report, "", "  ")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return 1, err
		}
		if err := writeFile(filepath.Join(outDir, "report.json"), js); err != nil {
			return 1, err
		}
		fmt.Fprintf(os.Stderr, "report: R=%.3f, %d residual defect(s) → %s\n",
			report.Readability.Score, len(report.Defects), filepath.Join(outDir, "report.json"))
	}

	// Pre-flight: run the full validator and surface any issues to stderr
	// before rendering. Errors are prefixed with "error:" so they are
	// immediately visible; warnings use "warn:". Rendering continues in
	// both cases — the output may be incomplete but is never silently wrong.
	errCount, warnCount := 0, 0
	if issues := isotopo.Validate(doc); len(issues) > 0 {
		for _, iss := range issues {
			switch iss.Severity {
			case isotopo.SeverityError:
				errCount++
				fmt.Fprintf(os.Stderr, "error: %s — %s\n", iss.Path, iss.Message)
			case isotopo.SeverityWarning:
				warnCount++
				fmt.Fprintf(os.Stderr, "warn:  %s — %s\n", iss.Path, iss.Message)
			}
		}
		if errCount > 0 {
			fmt.Fprintf(os.Stderr, "render: %d error(s), %d warning(s) — output may be incomplete\n", errCount, warnCount)
		}
	}

	if !flagReport {
		topologySVG = renderTopologySVG(doc)
	}
	// An empty topology SVG means the document resolved to no renderable
	// scene (no scene node, or a scene with no parts). Writing a 0-byte
	// topology.svg and exiting 0 is the worst case for an agent: a green
	// light over an empty canvas. Attribute it and fail the exit code.
	emptyScene := topologySVG == ""
	if emptyScene {
		fmt.Fprintln(os.Stderr, "render: document produced no scene — no renderable nodes were resolved (expected a scene node with parts under 'nodes')")
	}
	if err := writeFile(filepath.Join(outDir, "topology.svg"), []byte(topologySVG)); err != nil {
		return 1, err
	}

	sourceFilename := "topology" + sourceExt
	if err := writeFile(filepath.Join(outDir, sourceFilename), data); err != nil {
		return 1, err
	}

	absCopy, err := filepath.Abs(filepath.Join(outDir, sourceFilename))
	if err != nil {
		absCopy = sourceFilename
	}
	topologyHTML := isotopo.TopologyHTML(topologySVG, string(data), sourceLang, absCopy)
	if err := writeFile(filepath.Join(outDir, "topology.html"), []byte(topologyHTML)); err != nil {
		return 1, err
	}

	parts := isotopo.RenderParts(doc)
	frags := isotopo.PartFragments(doc)
	ids := isotopo.PartIDs(doc)
	for _, id := range ids {
		svg := parts[id]
		if svg == "" {
			continue
		}
		if err := writeFile(filepath.Join(nodesDir, id+".svg"), []byte(svg)); err != nil {
			return 1, err
		}
		var fragYAML []byte
		if f := frags[id]; f != nil {
			fragYAML, err = isotopo.MarshalFragmentYAML(f)
			if err != nil {
				return 1, err
			}
			if err := writeFile(filepath.Join(nodesDir, id+".yaml"), fragYAML); err != nil {
				return 1, err
			}
		}
		nodeHTML := isotopo.NodeHTML(id, svg, string(fragYAML))
		if err := writeFile(filepath.Join(nodesDir, id+".html"), []byte(nodeHTML)); err != nil {
			return 1, err
		}
	}

	indexHTML := isotopo.NodesIndexHTML(ids)
	if err := writeFile(filepath.Join(nodesDir, "_index.html"), []byte(indexHTML)); err != nil {
		return 1, err
	}

	fmt.Fprintf(os.Stderr, "rendered %d node(s) into %s\n", len(ids), outDir)

	// Exit-code contract (mirrors validate): errors or an empty scene fail
	// the render so an agent gating on the exit code never treats a broken
	// or empty output as success. Warnings alone stay at 0.
	if errCount > 0 || emptyScene {
		return 3, nil
	}
	return 0, nil
}

// renderTopologySVG returns the topology-level scene SVG. Resolution
// is delegated to doc.Scene() so the CLI stays in sync with the
// library's scene rules.
func renderTopologySVG(doc *isotopo.Document) string {
	if scene := doc.Scene(); scene != nil {
		// --projection overrides whatever canvas.projection the document set.
		if flagProjection != "" {
			if doc.Canvas == nil {
				doc.Canvas = &isotopo.Canvas{}
			}
			doc.Canvas.Projection = flagProjection
		}
		return isotopo.RenderWithCanvas(scene, doc.Theme, doc.Canvas, doc.Annotations)
	}
	return ""
}

// loadDocument routes the input through the right parser based on file
// extension. YAML/JSON go through the composite parser directly; .d2 is
// compiled + auto-laid-out by d2lib first, then translated to a
// composite document. The layout engine comes from the --layout CLI
// flag (default dagre).
func loadDocument(lang string, data []byte) (*isotopo.Document, error) {
	engine := isotopo.LayoutDagre
	if flagLayout == "elk" {
		engine = isotopo.LayoutELK
	}
	return isotopo.LoadInput(context.Background(), lang, data, engine)
}

// classifyInput maps an input path to (sourceLang, fileExtension). The
// language drives loadDocument; the extension drives the
// `topology.<ext>` source-copy filename so users can re-render later.
func classifyInput(path string) (lang, ext string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".d2":
		return "d2", ".d2"
	case ".json":
		return "json", ".json"
	default:
		return "yaml", ".yaml"
	}
}

func writeFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// serveFile starts a local preview server for one input file: GET /
// renders the CURRENT file content into the interactive topology.html,
// and POST /api/render renders whatever source the page's editor holds
// — a copy living in the browser; the original file is never written.
func serveFile(in string) error {
	if _, err := os.Stat(in); err != nil {
		return err
	}
	sourceLang, _ := classifyInput(in)
	absIn, err := filepath.Abs(in)
	if err != nil {
		absIn = in
	}

	port := os.Getenv("ISOTOPO_PORT")
	if port == "" {
		port = "8731"
	}

	type renderResult struct {
		svg          string
		model        []isotopo.PartModel
		routes       []isotopo.ConnectorRoute
		screenLabels []isotopo.ScreenLabel
		planLabels   []isotopo.PlanLabel
		issues       []isotopo.Issue
	}
	render := func(lang string, data []byte, projection string) (renderResult, error) {
		doc, err := loadDocument(lang, data)
		if err != nil {
			return renderResult{issues: []isotopo.Issue{{Severity: isotopo.SeverityError, Path: "$", Message: err.Error()}}}, nil
		}
		// Studio view switch: an iso/top override for THIS render only, never
		// written back to the document the editor holds (a pure preview).
		if projection != "" {
			if doc.Canvas == nil {
				doc.Canvas = &isotopo.Canvas{}
			}
			doc.Canvas.Projection = projection
		}
		issues := isotopo.Validate(doc)
		if lang != "d2" {
			issues = append(issues, isotopo.UnknownKeyIssues(data)...)
		}
		for _, i := range issues {
			if i.Severity == isotopo.SeverityError {
				return renderResult{issues: issues}, nil
			}
		}
		if flagProjection != "" {
			if doc.Canvas == nil {
				doc.Canvas = &isotopo.Canvas{}
			}
			doc.Canvas.Projection = flagProjection
		}
		scene := isotopo.RenderSceneWithInteraction(doc)
		svg := scene.SVG
		if svg == "" {
			issues = append(issues, isotopo.Issue{
				Severity: isotopo.SeverityError, Path: "$",
				Message: "document renders no scene — it has no nodes (or the scene resolves empty)",
			})
			return renderResult{issues: issues}, nil
		}
		// Geometry and selection belong to the same submitted scene. Plan
		// previews reuse the layout and content already certified for the SVG.
		return renderResult{svg: svg, model: scene.Model, routes: scene.Routes, screenLabels: scene.ScreenLabels, planLabels: scene.PlanLabels, issues: issues}, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	// GET /api/source — the CURRENT on-disk content of the edited file. Studio
	// polls this so the UI stays bound to the live file: external edits (another
	// editor, or this same file rewritten) are pulled back into the canvas.
	mux.HandleFunc("GET /api/source", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(in)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})
	mux.HandleFunc("POST /api/render", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		lang := r.URL.Query().Get("format")
		if lang == "" {
			lang = sourceLang
		}
		res, err := render(lang, body, r.URL.Query().Get("projection"))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"svg": res.svg, "issues": res.issues, "model": res.model, "routes": res.routes, "screenLabels": res.screenLabels, "planLabels": res.planLabels})
	})
	// POST /api/save — the ONE write-back path. Studio edits live in the
	// browser copy; this is the explicit "Overwrite" action that persists the
	// editor's current content to the original source file on disk. Guarded so
	// a blank or unparseable document can never clobber the file (warnings are
	// fine — only a hard load error blocks the write).
	mux.HandleFunc("POST /api/save", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.TrimSpace(string(body)) == "" {
			http.Error(w, "refusing to overwrite the file with empty content", 422)
			return
		}
		lang := r.URL.Query().Get("format")
		if lang == "" {
			lang = sourceLang
		}
		if _, err := loadDocument(lang, body); err != nil {
			http.Error(w, "not saved — document does not parse: "+err.Error(), 422)
			return
		}
		if err := os.WriteFile(absIn, body, 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "path": absIn, "bytes": len(body)})
	})
	// v4.4 — drag-to-edit: the editor copy + a move op come in, the
	// server text-edits the YAML (preserving comments/formatting), then
	// renders. kind=node writes an absolute offset on the part; kind=edge
	// writes a bend delta on the ci-th connector. Returns {yaml, svg,
	// issues} so Studio updates editor AND canvas in one round-trip.
	mux.HandleFunc("POST /api/move", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		q := r.URL.Query()
		atof := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
		lang := q.Get("format")
		if lang == "" {
			lang = sourceLang
		}
		ci, _ := strconv.Atoi(q.Get("ci"))
		// dwx, dwy are a WORLD-space drag DELTA; ApplyOpText resolves the
		// target's current position and adds the delta. snap>0 rounds to grid.
		op := isotopo.EditOp{
			Kind: "move", Target: q.Get("kind"), ID: q.Get("id"), CI: ci,
			DWX: atof(q.Get("dwx")), DWY: atof(q.Get("dwy")), Snap: atof(q.Get("snap")),
		}
		if q.Get("segment") != "" {
			segment, err := strconv.Atoi(q.Get("segment"))
			if err != nil {
				http.Error(w, "invalid segment", 400)
				return
			}
			if q.Get("kind") != "edge" || q.Get("ids") != "" || q.Get("wp") != "" {
				http.Error(w, "segment editing requires one edge and no legacy waypoint override", 400)
				return
			}
			if _, err := strconv.Atoi(q.Get("ci")); err != nil {
				http.Error(w, "invalid connector index", 400)
				return
			}
			for _, key := range []string{"dwx", "dwy"} {
				if _, err := strconv.ParseFloat(q.Get(key), 64); err != nil {
					http.Error(w, "invalid segment delta", 400)
					return
				}
			}
			op.Kind, op.Segment = "move-segment", segment
		}
		// v5 — Studio group move: ids=<JSON string array> applies the same
		// delta to every listed node atomically (all-or-nothing, one text
		// surgery, one round-trip). Absent → the single-id path is untouched.
		if idsQ := q.Get("ids"); idsQ != "" {
			var ids []string
			if err := json.Unmarshal([]byte(idsQ), &ids); err != nil {
				http.Error(w, "bad ids: "+err.Error(), 400)
				return
			}
			op.Kind, op.Target, op.ID, op.IDs = "move-multi", "node", "", ids
		}
		// v4.6 — Studio posts an explicit interior waypoint list (drawio-style
		// per-segment edit) as wp; absent → ApplyOpText falls back to a bend.
		if wp := q.Get("wp"); wp != "" {
			if err := json.Unmarshal([]byte(wp), &op.Waypoints); err != nil {
				http.Error(w, "bad wp: "+err.Error(), 400)
				return
			}
		}
		out, oerr := isotopo.ApplyOpText(lang, body, op)
		if oerr != nil {
			http.Error(w, oerr.Error(), 422)
			return
		}
		res, _ := render(lang, out, r.URL.Query().Get("projection"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"yaml": string(out), "svg": res.svg, "issues": res.issues, "model": res.model, "routes": res.routes, "screenLabels": res.screenLabels, "planLabels": res.planLabels})
	})
	// v4.7 — detail editor. /api/fields returns the scalar fields actually
	// present in a node/edge's YAML, each with a semantic label, for the
	// right-click "edit details" modal. /api/edit writes the user's changes
	// back (comment-preserving) and re-renders.
	mux.HandleFunc("POST /api/fields", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		q := r.URL.Query()
		ci, _ := strconv.Atoi(q.Get("ci"))
		fields, ferr := isotopo.Fields(sourceLang, body, q.Get("kind"), q.Get("id"), ci)
		if ferr != nil {
			http.Error(w, ferr.Error(), 422)
			return
		}
		fields = isotopo.LocalizeFields(fields, q.Get("uilang"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"fields": fields})
	})
	mux.HandleFunc("POST /api/edit", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		q := r.URL.Query()
		var changes map[string]string
		if err := json.Unmarshal([]byte(q.Get("f")), &changes); err != nil {
			http.Error(w, "bad f: "+err.Error(), 400)
			return
		}
		lang := q.Get("format")
		if lang == "" {
			lang = sourceLang
		}
		ci, _ := strconv.Atoi(q.Get("ci"))
		// The synthetic "@iconColor" field is handled inside ApplyOp's set-field
		// path now (translated to an icon-ref suffix), so the handler just passes
		// the changes through — UI and headless/WASM callers share one path.
		op := isotopo.EditOp{Kind: "set-field", Target: q.Get("kind"), ID: q.Get("id"), CI: ci, Fields: changes}
		outB, oerr := isotopo.ApplyOpText(lang, body, op)
		if oerr != nil {
			http.Error(w, oerr.Error(), 422)
			return
		}
		out := string(outB)
		res, _ := render(lang, outB, r.URL.Query().Get("projection"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"yaml": out, "svg": res.svg, "issues": res.issues, "model": res.model, "routes": res.routes, "screenLabels": res.screenLabels, "planLabels": res.planLabels})
	})
	// v4.8 — structural ops: delete a node (+its connectors) or edge, or
	// duplicate a node. Comment-preserving text surgery on the posted source.
	mux.HandleFunc("POST /api/op", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		q := r.URL.Query()
		lang := q.Get("format")
		if lang == "" {
			lang = sourceLang
		}
		ci, _ := strconv.Atoi(q.Get("ci"))
		// op ∈ add|delete|duplicate, kind ∈ node|edge → one EditOp kind.
		op := isotopo.EditOp{Kind: q.Get("op"), Target: q.Get("kind"), ID: q.Get("id"), CI: ci}
		switch q.Get("op") {
		case "add", "delete", "duplicate", "add-edge", "reparent":
		default:
			http.Error(w, "op must be add|add-edge|delete|duplicate|reparent", 400)
			return
		}
		if q.Get("op") == "add-edge" {
			op.Fields = map[string]string{
				"from": q.Get("from"), "to": q.Get("to"),
				"fromAnchor": q.Get("fanchor"), "toAnchor": q.Get("tanchor"),
			}
		}
		if q.Get("op") == "reparent" {
			// reparent's Target is the DESTINATION group id ("" = scene root),
			// not a node|edge kind.
			op.Target = q.Get("target")
		}
		outB, oerr := isotopo.ApplyOpText(lang, body, op)
		if oerr != nil {
			http.Error(w, oerr.Error(), 422)
			return
		}
		out := string(outB)
		res, _ := render(lang, outB, r.URL.Query().Get("projection"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"yaml": out, "svg": res.svg, "issues": res.issues, "model": res.model, "routes": res.routes, "screenLabels": res.screenLabels, "planLabels": res.planLabels})
	})
	// The per-part gallery the footer links to. The render command writes
	// these as files; serve answers them on the fly from the CURRENT file
	// content so the link works in live mode too.
	mux.HandleFunc("GET /nodes/", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(in)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		doc, err := loadDocument(sourceLang, data)
		if err != nil {
			http.Error(w, err.Error(), 422)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/nodes/")
		if name == "" || name == "_index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(isotopo.NodesIndexHTML(isotopo.PartIDs(doc))))
			return
		}
		ext := filepath.Ext(name)
		id := strings.TrimSuffix(name, ext)
		switch ext {
		case ".svg":
			svg := isotopo.RenderParts(doc)[id]
			if svg == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(svg))
		case ".yaml":
			f := isotopo.PartFragments(doc)[id]
			if f == nil {
				http.NotFound(w, r)
				return
			}
			y, err := isotopo.MarshalFragmentYAML(f)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
			w.Write(y)
		case ".html":
			svg := isotopo.RenderParts(doc)[id]
			if svg == "" {
				http.NotFound(w, r)
				return
			}
			var fragYAML []byte
			if f := isotopo.PartFragments(doc)[id]; f != nil {
				fragYAML, _ = isotopo.MarshalFragmentYAML(f)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(isotopo.NodeHTML(id, svg, string(fragYAML))))
		default:
			http.NotFound(w, r)
		}
	})
	mux.HandleFunc("GET /topology.svg", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(in)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		res, _ := render(sourceLang, data, "")
		svg := res.svg
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write([]byte(svg))
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(in)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		res, _ := render(sourceLang, data, "")
		svg := res.svg
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Never let the browser serve a stale Studio page — the template
		// changes between builds and a cached copy would run old JS (the
		// source of the "still teleports after I fixed it" reports).
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		w.Write([]byte(isotopo.TopologyHTML(svg, string(data), sourceLang, absIn)))
	})

	fmt.Printf("isotopo Studio · %s\nopen:  http://localhost:%s\nlive editing — UI changes auto-save to %s, and external edits to it reload in the UI\n", in, port, in)
	return http.ListenAndServe("localhost:"+port, mux)
}
