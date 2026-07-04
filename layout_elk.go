// ELK / dagre engine bridge for the composite `layout: {mode: auto}`
// pass (master plan §2 — "don't reinvent the world-plane core").
//
// The built-in auto arranger (arrangeAuto, layout.go) is a longest-path
// Sugiyama-lite: fine for pipelines, but on dense DAGs with heavy
// fan-in/fan-out its fixed barycenter/median sweeps leave avoidable
// crossings and tunnels. `layout: {mode: auto, engine: elk|dagre}`
// opts a scene into d2's mature layered engines instead — the same
// engines compile.go already wires for .d2 input, so this adds no new
// dependency and stays fully offline (both are goja-embedded JS; no
// network, no clock, deterministic for identical input).
//
// Bridge contract:
//   - node footprints (world W×D) become d2 shape width×height; the
//     extrusion clearance arrangeAuto reserves (H*0.6 along +x) is
//     baked into the width so tall parts keep clear air on the iso
//     canvas exactly like the built-in path;
//   - d2 lays the graph out in its screen plane with `direction:
//     right`; screen x→world x, screen y→world y — the same axis map
//     translate.go uses for .d2 input — then positions are normalised
//     to origin and snapped to the cell lattice, so engine output
//     stays grid-aligned and byte-deterministic;
//   - any failure (unknown engine, compile error, timeout) falls back
//     to the built-in path with a Warning, never a hard error;
//   - engine == "" never reaches this file: the default path is
//     byte-identical to before this feature existed.
package isotopo

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// layoutEngines is the discrete vocabulary for layout.engine — the
// single source for validation, did-you-mean suggestions and the
// capability contract (same pattern as layoutModes).
var layoutEngines = []string{"elk", "dagre"}

func validLayoutEngine(e string) bool {
	for _, v := range layoutEngines {
		if e == v {
			return true
		}
	}
	return false
}

// layoutEngineTimeout bounds one goja layout run. The embedded JS
// engines finish small graphs in well under a second; the bound only
// exists so a pathological input degrades to the built-in path
// instead of hanging a render.
const layoutEngineTimeout = 30 * time.Second

// layoutEngineIssues reports engine misuse the solve path cannot see.
// The engine only dispatches at the scene root under mode auto
// (arrangeEngineAuto owns THAT enum error), so this covers the rest:
// engine on a non-auto layout or on a group layout is silently unused
// — warn; an unknown engine value there still gets its did-you-mean.
// root marks the scene root's layout (where mode auto engages engines).
func layoutEngineIssues(lay *Layout, path string, root bool) []Issue {
	if lay == nil || lay.Engine == "" {
		return nil
	}
	active := root && lay.Mode == "auto"
	var issues []Issue
	if !validLayoutEngine(lay.Engine) && !active {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     path + ".engine",
			Message:  fmt.Sprintf("unknown layout engine %q", lay.Engine),
			Suggest:  nearest(lay.Engine, layoutEngines),
		})
	}
	if !active {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     path + ".engine",
			Message:  "layout.engine only applies to the scene root's layout: {mode: auto}; it is ignored here",
		})
	}
	return issues
}

// arrangeEngineAuto is the mode-auto dispatch target for engine ==
// "elk" | "dagre". It positions parts from the connector graph via the
// chosen d2 engine and reports whether it succeeded; false means the
// caller must run the built-in auto path instead (an Issue explaining
// why has already been appended).
func arrangeEngineAuto(parts []*CompositePart, conns []*Connector, lay *Layout, cell float64, path string, issues *[]Issue) bool {
	engine := ""
	if lay != nil {
		engine = lay.Engine
	}
	if !validLayoutEngine(engine) {
		*issues = append(*issues, Issue{
			Severity: SeverityError,
			Path:     path + ".layout.engine",
			Message:  fmt.Sprintf("unknown layout engine %q; using the built-in auto layout", engine),
			Suggest:  nearest(engine, layoutEngines),
		})
		return false
	}

	// Same id collection + pin policy as arrangeAuto: declared order,
	// first declaration wins on duplicate ids, and a part that already
	// carries an explicit offset (author coordinate or Studio drag) is
	// honoured as-is — it still shapes the graph, but the engine never
	// overwrites its position.
	idx := map[string]int{}
	order := []string{}
	for _, p := range parts {
		if p == nil || p.ID == "" {
			continue
		}
		if _, dup := idx[p.ID]; !dup {
			idx[p.ID] = len(order)
			order = append(order, p.ID)
		}
	}
	if len(order) == 0 {
		return false
	}
	pinned := map[string]bool{}
	for _, id := range order {
		if parts[idx[id]].Offset != nil {
			pinned[id] = true
		}
	}

	// Build a minimal d2 source with synthetic keys n0..nN, so author
	// ids never need escaping and can never collide with d2 keywords.
	// Shapes carry empty labels + explicit width/height, making the
	// layout a pure function of footprints and edges (no font metrics
	// in the loop). Two clearances are baked into the dimensions:
	//   - width reserves the extrusion clearance (H*0.6) arrangeAuto's
	//     column advance reserves, so tall parts in adjacent ranks
	//     keep the same clear air as on the built-in path;
	//   - both axes reserve one extra CELL of snap slack: the final
	//     grid-snap moves each part by up to cell/2 per axis, so two
	//     parts the engine placed close could otherwise round INTO
	//     each other — the slack keeps real footprints separated by
	//     at least the engine's own gap after rounding.
	var b strings.Builder
	b.WriteString("direction: right\n")
	for i, id := range order {
		p := parts[idx[id]]
		w, d := partFootprint(p)
		h := partHeight(p)
		fmt.Fprintf(&b, "n%d: \"\" {width: %d; height: %d}\n",
			i, int(math.Ceil(w+h*0.6+cell)), int(math.Ceil(d+cell)))
	}
	for _, c := range conns {
		if c == nil {
			continue
		}
		u, v := connectorTarget(c.From), connectorTarget(c.To)
		ui, uok := idx[u]
		vi, vok := idx[v]
		if !uok || !vok || u == v {
			continue
		}
		fmt.Fprintf(&b, "n%d -> n%d\n", ui, vi)
	}

	ctx, cancel := context.WithTimeout(context.Background(), layoutEngineTimeout)
	defer cancel()
	diagram, err := CompileD2(ctx, b.String(), LayoutEngine(engine))
	if err != nil {
		*issues = append(*issues, Issue{
			Severity: SeverityWarning,
			Path:     path + ".layout.engine",
			Message:  fmt.Sprintf("layout engine %q failed (%v); using the built-in auto layout", engine, err),
		})
		return false
	}

	pos := map[string][2]float64{}
	for i := range diagram.Shapes {
		s := &diagram.Shapes[i]
		pos[s.ID] = [2]float64{float64(s.Pos.X), float64(s.Pos.Y)}
	}
	minX, minY := math.Inf(1), math.Inf(1)
	for i := range order {
		xy, ok := pos[fmt.Sprintf("n%d", i)]
		if !ok {
			*issues = append(*issues, Issue{
				Severity: SeverityWarning,
				Path:     path + ".layout.engine",
				Message:  fmt.Sprintf("layout engine %q returned an incomplete layout; using the built-in auto layout", engine),
			})
			return false
		}
		minX, minY = math.Min(minX, xy[0]), math.Min(minY, xy[1])
	}

	snap := func(v float64) float64 { return math.Round(v/cell) * cell }
	for i, id := range order {
		if pinned[id] {
			continue // honour the explicit/dragged offset
		}
		p := parts[idx[id]]
		xy := pos[fmt.Sprintf("n%d", i)]
		if p.Offset == nil {
			p.Offset = &WorldPoint{}
		}
		p.Offset.WX = snap(xy[0] - minX)
		p.Offset.WY = snap(xy[1] - minY)
	}
	return true
}
