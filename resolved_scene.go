package isotopo

import "github.com/MarkovWangRR/iso-topology/iso25d"

// resolvedLayout owns every field the layout solver writes. Rendering and
// observation must not consume the author's Layout/Place declarations or bake
// solved coordinates into the input document.
func resolvedLayout(n *Node, canvas *Canvas) *Node {
	n = cloneSceneForEval(n)
	applyLayout(n, canvas)
	return n
}

// partLocalOffset combines the two authored coordinate systems exactly once.
// Lowered parts have Position cleared and carry only an absolute Offset.
func partLocalOffset(p *CompositePart, gridStep float64) (x, y, z float64) {
	x, y = partGridOffset(p, gridStep)
	if p.Offset != nil {
		x, y, z = x+p.Offset.WX, y+p.Offset.WY, p.Offset.WZ
	}
	return
}

func partGridOffset(p *CompositePart, gridStep float64) (x, y float64) {
	if p != nil && p.Position != nil && gridStep > 0 {
		return float64(p.Position.I) * gridStep, float64(p.Position.J) * gridStep
	}
	return 0, 0
}

func includeGridOffsets(parts []*CompositePart, gridStep float64) func() {
	if gridStep <= 0 {
		return func() {}
	}
	type shifted struct {
		p         *CompositePart
		x, y      float64
		hadOffset bool
	}
	var changes []shifted
	var walk func([]*CompositePart)
	walk = func(parts []*CompositePart) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			if p.Position != nil && gridStep > 0 {
				x, y := partGridOffset(p, gridStep)
				changes = append(changes, shifted{p, x, y, p.Offset != nil})
				if p.Offset == nil {
					p.Offset = &WorldPoint{}
				}
				p.Offset.WX += x
				p.Offset.WY += y
			}
			walk(p.Parts)
		}
	}
	walk(parts)
	return func() {
		for _, c := range changes {
			c.p.Offset.WX -= c.x
			c.p.Offset.WY -= c.y
			if !c.hadOffset && *c.p.Offset == (WorldPoint{}) {
				c.p.Offset = nil
			}
		}
	}
}

// partDimensions follows the renderer's actual shape dispatch and defaults.
// Original options remain untouched: supplying a previously omitted dimension
// can affect other defaults (notably a person's head proportions).
func partDimensions(p *CompositePart) (w, d, h float64) {
	if isContainerShape(p.Shape) {
		p = groupSubstrate(p)
	}
	o := iso25d.ConvertOpts{}
	if p.Geom != nil {
		o.Width, o.Depth, o.Height = p.Geom.W, p.Geom.D, p.Geom.H
		o.Sides, o.TopScale, o.Params = p.Geom.Sides, p.Geom.TopScale, p.Geom.Params
	}
	return iso25d.ShapeDimensions(p.Shape, o)
}
