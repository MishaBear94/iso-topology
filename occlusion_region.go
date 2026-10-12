package isotopo

import "github.com/MarkovWangRR/iso-topology/iso25d"

// occPaintRegions caches per-part lowering for one read-only observation. Body
// geometry must not be substituted here: transparent faces do not hide text,
// and semantic envelopes (array/rack) do not prove any filled pixel coverage.
type occPaintObservation struct {
	regions    map[*CompositePart]screenRegion
	unresolved map[*CompositePart]bool
	uncertain  bool
}

func occPaintRegions(flat []*CompositePart, theme *Theme) occPaintObservation {
	uncertain := false
	out := make(map[*CompositePart]screenRegion, len(flat))
	unresolved := make(map[*CompositePart]bool)
	for _, p := range flat {
		if p == nil || p.groupLabel || p.isSubstrate || isContainerShape(p.Shape) {
			continue
		}
		shape, o := Flatten(&Node{Shape: p.Shape, Geom: p.Geom, Style: p.Style, Preset: p.Preset, Role: p.Role}, theme)
		coverage := iso25d.ShapePaint(shape, o, .5)
		uncertain = uncertain || coverage.Uncertain
		if coverage.Uncertain {
			unresolved[p] = true
		}
		r := screenRegion(coverage.Polygons)
		var x, y float64
		if p.Offset != nil {
			x, y = projectIso(p.Offset.WX, p.Offset.WY, p.Offset.WZ)
		}
		for _, poly := range r {
			for i := range poly {
				poly[i][0] += x
				poly[i][1] += y
			}
		}
		out[p] = r
	}
	return occPaintObservation{regions: out, unresolved: unresolved, uncertain: uncertain}
}
