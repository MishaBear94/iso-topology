package isotopo

// repair_routing.go — WS-G: the ROUTING actuator of the projection-repair
// loop (RepairScene).
//
// readability.go scores crossings and tunnels, but until now no actuator
// repaired them — they were "measured, never fixed". This pass closes that
// gap for the class the engine can fix by itself: an ORTHOGONAL connector
// implicated in a crossing or a tunnel is upgraded to routing:"orthogonal2"
// (the opt-in obstacle- and crossing-aware v2 router, inject_router2.go),
// exactly the escalation docs/agent guidance already tells authors to make
// by hand.
//
// Discipline (the ratchet): the upgrade is GREEDY and MEASURED. Connectors
// are visited in declaration order; each tentative upgrade is re-scored with
// the collector-backed iso evaluator (EvaluateIso via Readability) and kept
// only when the weighted routing-defect sum
//
//	wTunnel·tunnels + wCrossing·crossings   (3·T + 1·C, readability.go)
//
// strictly decreases AND the overall readability cost strictly decreases,
// without worsening any supported per-object/per-edge visual defect,
// including missing/blocked measured labels —
// otherwise the connector is rolled back to "orthogonal" untouched. A scene
// whose defects the v2 router cannot improve therefore renders byte-for-byte
// identically (conservative trigger: no defect → no action; no strict win →
// no action). At most maxRoutingUpgradesPerRound connectors change per
// RepairScene iteration; the loop's existing maxIters budget and no-change
// convergence test bound the whole process.

// maxRoutingUpgradesPerRound caps how many connectors one repair iteration
// may upgrade, so a dense scene escalates gradually and every accepted step
// stays individually measured against the converging loop.
const maxRoutingUpgradesPerRound = 3

// routingDefectSum is the weighted crossing/tunnel sum the actuator
// optimizes, in readability.go's own weighting (tunnel 3, crossing 1).
func routingDefectSum(r ReadabilityReport) float64 {
	return wTunnel*float64(r.Tunnels) + wCrossing*float64(r.Crossings)
}

// repairRouting upgrades orthogonal connectors implicated in a crossing or
// tunnel to orthogonal2, one at a time, keeping only strict improvements.
// Returns whether any connector's routing changed.
func repairRouting(doc *Document, scene *Node) bool {
	if doc == nil || scene == nil || len(scene.Connectors) == 0 {
		return false
	}
	// Candidate screen before any (expensive) measurement: only a plain
	// orthogonal connector with no hand-authored route override (bend /
	// waypoints — a user-dragged route is the user's call, never rerouted).
	candidate := func(c *Connector) bool {
		return c != nil && c.Routing == "orthogonal" && c.Elbow == "" && c.Bend == nil && len(c.Waypoints) == 0 && len(c.Waypoints3D) == 0
	}
	any := false
	for _, c := range scene.Connectors {
		if candidate(c) {
			any = true
			break
		}
	}
	if !any {
		return false
	}
	before := Readability(doc)
	base := routingDefectSum(before)
	if base <= 0 {
		return false // no crossing/tunnel anywhere — nothing to repair
	}
	baseCost := before.Cost
	defectsBefore := sceneVisualDefects(scene, doc.Theme, doc.Canvas)
	implicated := routingDefectConnectors(scene, doc.Theme, doc.Canvas)
	changed, upgrades := false, 0
	for ci := 0; ci < len(scene.Connectors) && upgrades < maxRoutingUpgradesPerRound; ci++ {
		c := scene.Connectors[ci]
		if !candidate(c) || !implicated[ci] {
			continue
		}
		c.Routing = "orthogonal2"
		now := Readability(doc)
		sum := routingDefectSum(now)
		var nextDefects visualDefects
		if sum < base-1e-9 && now.Cost < baseCost-1e-9 {
			nextDefects = sceneVisualDefects(scene, doc.Theme, doc.Canvas)
		}
		if nextDefects != nil && nextDefects.noWorseThan(defectsBefore) {
			base, baseCost = sum, now.Cost
			defectsBefore = nextDefects
			changed = true
			upgrades++
			if base <= 0 {
				break // routing-clean — stop escalating
			}
		} else {
			c.Routing = "orthogonal" // no strict win — roll back untouched
		}
	}
	return changed
}

// routingDefectConnectors returns the set of connector indices whose REAL
// rendered iso route (in-memory collector, WS-E) participates in a tunnel or
// an edge-edge crossing — the same kernel and exclusions as evalGeom, but
// attributed per connector so the actuator knows which edge to escalate.
func routingDefectConnectors(scene *Node, theme *Theme, canvas *Canvas) map[int]bool {
	n := cloneSceneForEval(scene)
	rects, _, edges := buildPlanModel(n, theme, canvas)
	real := isoCollectedRoutes(n, theme, canvas, false)
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
	bad := map[int]bool{}
	for _, e := range edges {
		if routeTunnels(e.pts, e.from.id, e.to.id, edgeZLevel(e.from, e.to), leaves) > 0 {
			bad[e.ci] = true
		}
	}
	for i := 0; i < len(edges); i++ {
		for j := i + 1; j < len(edges); j++ {
			if edgesAdjacent(edges[i], edges[j]) {
				continue // shared endpoint = hub, not a crossing (evalGeom)
			}
			if routesIntersect(edges[i].pts, edges[j].pts) {
				bad[edges[i].ci] = true
				bad[edges[j].ci] = true
			}
		}
	}
	return bad
}

// routesIntersect reports whether two polyline routes properly cross
// (interior intersection, endpoints touching excluded — segInt's rule).
func routesIntersect(a, b [][2]float64) bool {
	for i := 1; i < len(a); i++ {
		for j := 1; j < len(b); j++ {
			if _, ok := segInt(a[i-1], a[i], b[j-1], b[j]); ok {
				return true
			}
		}
	}
	return false
}
