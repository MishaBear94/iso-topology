package isotopo

import (
	"math"
	"reflect"
	"strings"
)

// This actuator uses the public painter, not the experimental depth renderer.
// It only chooses the other existing elbow order for an automatic route. A
// strict reduction in conflict identities must preserve every observed defect,
// terminal, marker, label, per-edge complexity and visible ink fraction.
func repairIsoRouteCrowding(doc *Document, scene *Node) bool {
	if doc == nil || scene == nil || len(scene.Connectors) < 2 || (doc.Canvas != nil && doc.Canvas.Projection == "top") {
		return false
	}
	eligible := func(c *Connector) bool {
		return c != nil && c.Routing == "orthogonal" && c.Elbow == "" && c.Bend == nil && len(c.Waypoints) == 0 && len(c.Waypoints3D) == 0 && !strings.Contains(c.From, ".") && !strings.Contains(c.To, ".")
	}
	any := false
	for _, c := range scene.Connectors {
		any = any || eligible(c)
	}
	if !any {
		return false
	}
	before := renderSceneObserved(doc, true)
	if before.IsoRouteCrowding == nil || before.IsoRouteCrowding.Status != "complete" || len(before.IsoRouteCrowding.Conflicts) == 0 {
		return false
	}
	// Legacy pills and unbound screen labels cannot prove their real text ink.
	for _, label := range before.ScreenLabels {
		if label.TextMetrics != "shaped-ink" {
			return false
		}
	}
	for _, r := range before.Routes {
		if r.Drawing != nil && r.Drawing.Label != nil && r.Drawing.Label.Typography == nil {
			return false
		}
	}
	baseCost := Readability(doc).Cost
	defects := sceneVisualDefects(scene, doc.Theme, doc.Canvas)
	changed, attempts := false, 0
	for i, c := range scene.Connectors {
		if !eligible(c) {
			continue
		}
		implicated := false
		for _, v := range before.IsoRouteCrowding.Conflicts {
			implicated = implicated || v.ConnectorIndex == i || v.PeerIndex == i
		}
		if !implicated {
			continue
		}
		for _, elbow := range []string{"xFirst", "yFirst"} {
			if attempts == 8 {
				return changed
			}
			attempts++
			c.Elbow = elbow
			after := renderSceneObserved(doc, true)
			crowding := after.IsoRouteCrowding
			if crowding != nil && crowding.Status == "complete" && len(crowding.Conflicts) < len(before.IsoRouteCrowding.Conflicts) &&
				isoCrowdingDefects(crowding).noWorseThan(isoCrowdingDefects(before.IsoRouteCrowding)) && isoCrowdingRouteQuality(before, after) {
				cost := Readability(doc).Cost
				if cost <= baseCost+1e-9 {
					next := sceneVisualDefects(scene, doc.Theme, doc.Canvas)
					if next.noWorseThan(defects) {
						before, defects, baseCost, changed = after, next, cost, true
						break
					}
				}
			}
			c.Elbow = ""
		}
	}
	return changed
}

func isoCrowdingRouteQuality(before, after SceneResult) bool {
	// Root dimensions include bodies, final route ink and export padding.
	// Two units cover integer rounding of the two independently moving bounds.
	frame := func(svg string) (float64, float64, bool) {
		root, err := findRootSVG(svg)
		if err != nil {
			return 0, 0, false
		}
		_, _, w, h, err := parseViewBox(extractAttr(root.tag(), "viewBox"))
		return w, h, err == nil && w > 0 && h > 0
	}
	aw, ah, aok := frame(before.SVG)
	bw, bh, bok := frame(after.SVG)
	if !aok || !bok || bw > aw+2 || bh > ah+2 {
		return false
	}
	if len(before.Routes) != len(after.Routes) || !reflect.DeepEqual(before.ScreenLabels, after.ScreenLabels) || !reflect.DeepEqual(before.ConnectorLabels, after.ConnectorLabels) {
		return false
	}
	for i, a := range before.Routes {
		b := after.Routes[i]
		x, y := a.Drawing, b.Drawing
		if a.Index != b.Index || x == nil || y == nil || len(x.Points) < 2 || len(y.Points) < 2 || x.ProjectionOrigin != y.ProjectionOrigin ||
			x.Points[0] != y.Points[0] || x.Points[len(x.Points)-1] != y.Points[len(y.Points)-1] || !reflect.DeepEqual(x.Arrow, y.Arrow) {
			return false
		}
		if reflect.DeepEqual(x.Points, y.Points) {
			continue
		}
		// World length/turns protect the editable route; final screen length and
		// vertical variation prevent a hidden shortcut or a new visual tent.
		if planLen(b.World) > planLen(a.World)+1e-6 || planBends(b.World) > planBends(a.World) {
			return false
		}
		length := func(points [][2]float64) (float64, float64) {
			l, v := 0., 0.
			for j := 1; j < len(points); j++ {
				dx, dy := points[j][0]-points[j-1][0], points[j][1]-points[j-1][1]
				l += math.Hypot(dx, dy)
				v += math.Abs(dy)
			}
			return l, v
		}
		al, av := length(x.Points)
		bl, bv := length(y.Points)
		if bl > al+.03 || bv > av+.03 {
			return false
		}
		visible := func(c *IsoRouteCrowding, index int) float64 {
			g := c.ink[index]
			all := regionArea(g.total)
			if all <= 0 {
				return 0
			}
			return regionArea(g.raw) / all
		}
		if visible(after.IsoRouteCrowding, b.Index)+1e-9 < visible(before.IsoRouteCrowding, a.Index) {
			return false
		}
	}
	return true
}
