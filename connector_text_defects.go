package isotopo

import "strings"

// These keys preserve connector identity, including parallel semantic edges.
// Besides presence/state, preserve the counterparts and measured clearance
// shortfall of already-blocked labels. Aggregate improvement cannot pay for a
// newly obstructed object or a deeper existing obstruction.
func connectorLabelPlacementDefects(n *Node, theme *Theme, canvas *Canvas) visualDefects {
	return connectorLabelDefectsObserved(n, theme, canvas, nil)
}

func connectorLabelDefectsObserved(n *Node, theme *Theme, canvas *Canvas, observed *SceneResult) visualDefects {
	out := visualDefects{}
	if n == nil {
		return out
	}
	size := 13.0
	if canvas != nil && canvas.Projection == "top" {
		size = 10
	}
	measured := map[int]bool{}
	for i, c := range n.Connectors {
		if c != nil && c.LabelFontFamily != "" && strings.TrimSpace(c.Label) != "" && layoutConnectorText(c, size).font != nil {
			measured[i] = true
		}
	}
	if len(measured) == 0 {
		return out
	}
	if observed == nil {
		s := renderSceneObserved(&Document{Nodes: map[string]*Node{"scene": n}, Theme: theme, Canvas: canvas}, true)
		observed = &s
	}
	scene := observed
	for _, record := range scene.ConnectorLabels {
		delete(measured, record.ConnectorIndex)
		if record.Label.Typography.Placement == "blocked" {
			out[visualDefect{kind: "connector-label-blocked", edge: record.ConnectorIndex}] = 1
		}
	}
	for i := range measured {
		out[visualDefect{kind: "connector-label-missing", edge: i}] = 1
	}
	addConnectorTextConflictDefects(out, scene.ConnectorLabels)
	return out
}
