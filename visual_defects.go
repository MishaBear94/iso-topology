package isotopo

// visualDefect identifies the actual objects involved, not just a category's
// total. Connector indexes preserve parallel-edge identity; node pairs are
// ordered only where direction matters (body occlusion).
type visualDefect struct {
	kind       string
	a, b       string
	edge, peer int
}

type visualDefects map[visualDefect]float64

func (d visualDefects) add(k visualDefect) {
	if d != nil {
		d[k]++
	}
}

func nodePairDefect(kind, a, b string) visualDefect {
	if a > b {
		a, b = b, a
	}
	return visualDefect{kind: kind, a: a, b: b}
}

// sceneVisualDefects observes the current evaluator's supported geometry. It
// is a conservative candidate gate, not a proof of arbitrary 3-D visibility.
// Keep defect identity and severity so fixing one pair cannot buy a new pair.
func sceneVisualDefects(n *Node, theme *Theme, canvas *Canvas) visualDefects {
	if canvas != nil && canvas.Projection == "top" {
		// The top projection uses its own router. Testing its moves against
		// iso routes could accept a crossing the user actually sees in plan.
		p := resolvePlanScene(n, theme, canvas, true, true)
		d, _ := planRouteContentDefects(*p.layoutContent())
		p.evaluate(d)
		addPlanLabelFitDefects(d, p.rects)
		for _, issue := range planFootprintComposite(n, canvas, "scene") {
			d.add(visualDefect{kind: "plan", a: issue.Path, b: issue.Message})
		}
		return d
	}
	var observed *SceneResult
	if n != nil && len(n.Connectors) >= 2 {
		s := renderSceneObserved(&Document{Nodes: map[string]*Node{"scene": n}, Theme: theme, Canvas: canvas}, true)
		observed = &s
	}
	d := connectorLabelDefectsObserved(n, theme, canvas, observed)
	if observed != nil && observed.IsoRouteCrowding != nil {
		for k, v := range isoCrowdingDefects(observed.IsoRouteCrowding) {
			d[k] = v
		}
	}
	evaluateIsoWithDefects(n, theme, canvas, routeSourceCollector, d)
	addContentFitDefects(d, contentFitsInNode(n, theme, canvas, "scene"))
	for _, issue := range labelOcclusionComposite(n, theme, canvas, "scene") {
		d.add(visualDefect{kind: "label", a: issue.Path, b: issue.Message})
	}
	for _, oc := range bodyOcclusionsInNode(n, theme, canvas, bodyOcclusionThreshold) {
		d[visualDefect{kind: "body", a: oc.Hidden, b: oc.Occluder}] = oc.Frac
	}
	for _, issue := range lintComposite(n, theme, canvas, "scene") {
		d.add(visualDefect{kind: "solid", a: issue.Path, b: issue.Message})
	}
	return d
}

func (after visualDefects) noWorseThan(before visualDefects) bool {
	for key, severity := range after {
		if severity > before[key]+1e-9 {
			return false
		}
	}
	return true
}
