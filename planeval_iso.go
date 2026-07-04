package isotopo

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// planeval_iso.go is P0: score the ISO ENGINE'S REAL connector routes, not the
// plan view's simplified ones. The engine computes each route in WORLD space —
// the SAME world frame as the plan footprints — so we render iso, take those
// world polylines, and run the identical metric kernel. This is the honest
// scorecard for what users actually get, and the basis for later letting the
// scorecard DRIVE the engine router.
//
// WS-E: the routes now arrive through the in-memory collector
// (RenderWithRouteInfo / routecollect.go) instead of being regexed back out of
// the SVG's data-route attributes. That widens coverage to straight and bezier
// connectors — which emit no data-route and used to be scored as the plan
// view's simplified line — and decouples the evaluator from attribute
// serialization. The legacy regex path is kept below (isoRealRoutes) as the
// historical reference the parity tests pin the new path against.

var reIsoRoute = regexp.MustCompile(`data-connector="(\d+)" data-from="[^"]*" data-to="[^"]*" data-route="([^"]*)"`)

// isoRealRoutes (LEGACY, kept as the parity reference) renders the scene
// isometrically and parses each connector's real WORLD route from its
// data-route attribute, keyed by connector index. Straight-routed connectors
// emit no data-route and are simply absent.
// cloneSceneForEval deep-copies the parts (whose offsets/Layout applyLayout
// mutates) and the node's own Layout, sharing the connector slice (untouched by
// solving), so evaluating a scene never alters the caller's document.
func cloneSceneForEval(n *Node) *Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Parts = cloneParts(n.Parts)
	if n.Layout != nil {
		l := *n.Layout
		c.Layout = &l
	}
	return &c
}

func isoRealRoutes(n *Node, theme *Theme, canvas *Canvas) map[int][][2]float64 {
	cv := &Canvas{}
	if canvas != nil {
		c := *canvas
		cv = &c
	}
	cv.Projection = "iso" // force iso even if the document asked for top
	svg := RenderWithCanvas(n, theme, cv, nil)

	out := map[int][][2]float64{}
	for _, m := range reIsoRoute.FindAllStringSubmatch(svg, -1) {
		ci, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		var pts [][2]float64
		for _, corner := range strings.Fields(m[2]) {
			f := strings.Split(corner, ",")
			if len(f) != 4 {
				continue
			}
			wx, e1 := strconv.ParseFloat(f[2], 64)
			wy, e2 := strconv.ParseFloat(f[3], 64)
			if e1 == nil && e2 == nil {
				pts = append(pts, [2]float64{wx, wy})
			}
		}
		if len(pts) >= 2 {
			out[ci] = pts
		}
	}
	return out
}

// EvaluateIsoText parses a document and scores the iso engine's real routing.
func EvaluateIsoText(format string, src []byte) (*PlanReport, error) {
	doc, err := LoadInput(context.Background(), format, src, LayoutDagre)
	if err != nil {
		return nil, err
	}
	scene := doc.Scene()
	if scene == nil {
		return nil, fmt.Errorf("document has no scene to evaluate")
	}
	return EvaluateIso(scene, doc.Theme, doc.Canvas), nil
}

// isoCollectedRoutes renders the scene isometrically with the in-memory
// collector and returns each connector's world route keyed by connector
// index. orthoOnly restricts the result to orthogonal routes — exactly the
// connectors that serialize a data-route attribute, i.e. the legacy regex
// path's coverage; the parity tests lean on that equivalence.
//
// Coordinates are quantized through quantRoute to the same 3-decimal
// precision the data-route attribute carries, so the collector-backed scores
// reproduce the historical regex-parsed scores bit-for-bit on the shared
// coverage instead of drifting by serialization rounding.
func isoCollectedRoutes(n *Node, theme *Theme, canvas *Canvas, orthoOnly bool) map[int][][2]float64 {
	cv := &Canvas{}
	if canvas != nil {
		c := *canvas
		cv = &c
	}
	cv.Projection = "iso" // force iso even if the document asked for top
	_, routes := RenderWithRouteInfo(n, theme, cv, nil)

	out := map[int][][2]float64{}
	for _, r := range routes {
		// orthogonal2 serializes data-route just like orthogonal, so it is
		// inside the legacy regex path's coverage too.
		if orthoOnly && r.Routing != "orthogonal" && r.Routing != "orthogonal2" {
			continue
		}
		if len(r.World) < 2 {
			continue
		}
		pts := make([][2]float64, len(r.World))
		for i, p := range r.World {
			pts[i] = [2]float64{quantRoute(p[0]), quantRoute(p[1])}
		}
		out[r.Index] = pts
	}
	return out
}

// isoRouteSource selects where evaluateIsoWith gets the engine's real routes.
type isoRouteSource int

const (
	// routeSourceCollector — the in-memory collector, full coverage
	// (orthogonal + straight + bezier). The production source.
	routeSourceCollector isoRouteSource = iota
	// routeSourceCollectorOrtho — the collector restricted to orthogonal
	// routes: the exact coverage of the legacy regex path (parity tests).
	routeSourceCollectorOrtho
	// routeSourceRegex — the legacy data-route regex parse, kept as the
	// historical reference implementation.
	routeSourceRegex
)

// IsoReport is the iso scorecard: the shared PlanReport plus iso-only extras.
type IsoReport struct {
	*PlanReport
	// LeafNodeOverlaps counts overlapping LEAF-footprint pairs — the exact
	// metric EvaluatePlan publishes as NodeOverlaps — so iso and plan reports
	// can be compared under one overlap definition. The embedded NodeOverlaps
	// keeps its iso-specific meaning (distinct top-level parts including
	// group slabs) untouched.
	LeafNodeOverlaps int `json:"leaf_node_overlaps"`
	// BodyOcclusions (WS-H) counts nodes whose projected silhouette a nearer
	// (later-painted) body covers beyond bodyOcclusionThreshold — see
	// BodyOcclusions in occlusion.go. Iso-only, height-induced.
	BodyOcclusions int `json:"body_occlusions"`
}

// EvaluateIso scores the scene using the engine's real connector routes:
// orthogonal, straight AND bezier geometry all come from the in-memory
// collector, so the score measures exactly what is painted. Node footprints
// are the same applyLayout output.
func EvaluateIso(n *Node, theme *Theme, canvas *Canvas) *PlanReport {
	return EvaluateIsoReport(n, theme, canvas).PlanReport
}

// EvaluateIsoReport is EvaluateIso plus the iso-only extras (see IsoReport).
func EvaluateIsoReport(n *Node, theme *Theme, canvas *Canvas) *IsoReport {
	return evaluateIsoWith(n, theme, canvas, routeSourceCollector)
}

// evaluateIsoWith is the shared evaluator body; src picks the route source so
// the parity tests can score the SAME scene through the legacy regex path,
// the coverage-matched collector path, and the full collector path.
func evaluateIsoWith(n *Node, theme *Theme, canvas *Canvas, src isoRouteSource) *IsoReport {
	// Evaluation must NOT mutate the caller's scene. buildPlanModel and the
	// route renders both run applyLayout (which clears Layout/Place and writes
	// Offsets) in place, so score a deep clone — otherwise callers like
	// Readability silently solve-and-clear the doc, breaking any later pass
	// (e.g. RepairScene) that needs the original Layout declarations.
	n = cloneSceneForEval(n)
	// WS-H: body-vs-body occlusion, measured on the same clone before the plan
	// model solves it in place (bodyOcclusionsInNode clones again internally).
	bodyOcc := len(bodyOcclusionsInNode(n, theme, canvas, bodyOcclusionThreshold))
	rects, _, edges := buildPlanModel(n, theme, canvas)
	var real map[int][][2]float64
	switch src {
	case routeSourceRegex:
		real = isoRealRoutes(n, theme, canvas)
	case routeSourceCollectorOrtho:
		real = isoCollectedRoutes(n, theme, canvas, true)
	default:
		real = isoCollectedRoutes(n, theme, canvas, false)
	}
	for i := range edges {
		if r, ok := real[edges[i].ci]; ok {
			edges[i].pts = r
		}
	}
	var leaves []planRect
	for _, r := range rects {
		if !r.container {
			leaves = append(leaves, r)
		}
	}
	rep := evalGeom(leaves, edges)
	// evalGeom's own overlap count IS the plan definition (leaf pairs only) —
	// publish it under LeafNodeOverlaps before the iso-specific overwrite.
	leafOverlaps := rep.NodeOverlaps
	// Overlap reflects the VISUAL truth: count collisions between distinct
	// top-level parts' footprints — group SLABS included, not just leaves — so a
	// tray colliding with another tray (which leaf-only counting misses) is in R,
	// matching validate and the eye. Parent/child pairs share an owner and don't
	// count.
	rep.NodeOverlaps = countTopLevelCollisions(n.Parts, rects)
	return &IsoReport{PlanReport: rep, LeafNodeOverlaps: leafOverlaps, BodyOcclusions: bodyOcc}
}

// countTopLevelCollisions returns the number of distinct top-level-part pairs
// whose footprints collide (Z-aware), excluding parent/child. Footprint = the
// part's own rect (a group's slab, or a leaf's box).
func countTopLevelCollisions(topParts []*CompositePart, rects []planRect) int {
	descTop := map[string]string{}
	var mark func(top string, p *CompositePart)
	mark = func(top string, p *CompositePart) {
		if p.ID != "" {
			descTop[p.ID] = top
		}
		for _, c := range p.Parts {
			if c != nil {
				mark(top, c)
			}
		}
	}
	topIDs := map[string]bool{}
	for _, p := range topParts {
		if p != nil {
			mark(p.ID, p)
			if p.ID != "" {
				topIDs[p.ID] = true
			}
		}
	}
	var boxes []planRect
	for _, r := range rects {
		if !r.container || topIDs[r.id] {
			boxes = append(boxes, r)
		}
	}
	seen := map[[2]string]bool{}
	for i := 0; i < len(boxes); i++ {
		for j := i + 1; j < len(boxes); j++ {
			ta, tb := descTop[boxes[i].id], descTop[boxes[j].id]
			if ta == "" || tb == "" || ta == tb {
				continue
			}
			if !rectsOverlap(boxes[i], boxes[j]) {
				continue
			}
			key := [2]string{ta, tb}
			if ta > tb {
				key = [2]string{tb, ta}
			}
			seen[key] = true
		}
	}
	return len(seen)
}
