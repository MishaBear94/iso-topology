package isotopo

// A plan scene lives for one request. Its resolved geometry, accepted routes,
// frame and final content are shared by consumers; there is no document-keyed
// or cross-request cache that can become stale after an edit.
type resolvedPlanScene struct {
	node          *Node
	rects         []planRect
	byID          map[string]planRect
	edges         []planEdge
	ox, oy        float64
	width, height float64
	details       bool
	content       *planContentLayout
	crowding      *PlanRouteCrowding
}

func resolvePlanScene(n *Node, theme *Theme, canvas *Canvas, optimize, details bool) *resolvedPlanScene {
	if n == nil {
		return &resolvedPlanScene{details: details}
	}
	n, rects := resolvedPlanGeometry(n, theme, canvas)
	return resolvePlanSceneGeometry(n, rects, canvas, optimize, details)
}

func resolvePlanSceneGeometry(n *Node, rects []planRect, canvas *Canvas, optimize, details bool) *resolvedPlanScene {
	p := &resolvedPlanScene{node: n, rects: rects, details: details}
	p.byID, p.edges = routePlanGeometry(n, rects, optimize)
	if len(rects) == 0 {
		return p
	}
	p.ox, p.oy, p.width, p.height = planFrame(rects, p.edges, canvas)
	if optimize {
		p.edges, p.content = refinePlanRoutesContent(rects, p.edges, p.ox, p.oy, 32)
		p.edges, p.content, p.crowding = refinePlanRouteCrowding(rects, p.edges, p.content, p.ox, p.oy)
	}
	return p
}

func (p *resolvedPlanScene) layoutContent() *planContentLayout {
	if p.content == nil {
		c := resolvePlanContent(p.rects, p.edges, p.ox, p.oy, p.details)
		p.content = &c
	}
	return p.content
}

func (p *resolvedPlanScene) evaluate(defects visualDefects) *PlanReport {
	var leaves []planRect
	for _, r := range p.rects {
		if !r.container {
			leaves = append(leaves, r)
		}
	}
	report := evalGeomWithDefects(leaves, p.edges, defects)
	if p.crowding == nil {
		p.crowding = observePlanRouteCrowding(p.rects, p.edges, p.ox, p.oy)
	}
	report.RouteCrowding = p.crowding
	if defects != nil {
		for k, v := range planCrowdingDefects(p.crowding) {
			defects[k] = v
		}
	}
	return report
}
