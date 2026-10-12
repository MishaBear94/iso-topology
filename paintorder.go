package isotopo

import "math"

// orderCompositeParts is the shared painter order for rendering and occlusion
// evaluation: group substrates, then thin platforms carrying other parts, then
// bodies. Optional depth sorting preserves those partitions. This changes the
// slice and marks platforms, so callers must pass their own lowered parts.
func orderCompositeParts(flat []*CompositePart, canvas *Canvas, gridStep float64) ([]*CompositePart, int) {
	dims := func(p *CompositePart) (x, y, z, w, d, h float64) {
		w, d, h = partDimensions(p)
		x, y, z = partLocalOffset(p, gridStep)
		return
	}
	isPlatform := make([]bool, len(flat))
	for i, p := range flat {
		if p.isSubstrate {
			continue
		}
		px, py, pz, pw, pd, ph := dims(p)
		if ph > 24 {
			continue
		}
		for j, q := range flat {
			if i == j || q.isSubstrate {
				continue
			}
			qx, qy, qz, qw, qd, _ := dims(q)
			if math.Abs(qz-(pz+ph)) > 0.5 {
				continue
			}
			cx, cy := qx+qw/2, qy+qd/2
			if cx > px && cx < px+pw && cy > py && cy < py+pd {
				isPlatform[i] = true
				break
			}
		}
	}
	ordered := make([]*CompositePart, 0, len(flat))
	for _, p := range flat {
		if p.isSubstrate {
			ordered = append(ordered, p)
		}
	}
	for i, p := range flat {
		if !p.isSubstrate && isPlatform[i] {
			p.isSubstrate = true // connector layers treat a platform like a group slab
			ordered = append(ordered, p)
		}
	}
	nSubstrates := len(ordered)
	for _, p := range flat {
		if !p.isSubstrate {
			ordered = append(ordered, p)
		}
	}
	if canvas != nil && canvas.DepthSort {
		depthSortParts(ordered, nSubstrates, gridStep)
	}
	return ordered, nSubstrates
}
