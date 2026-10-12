package isotopo

// connectorTextPlacement retains the complete baseline arrangement before
// any refinement. A previously clear label keeps its exact anchor; an improved
// blocked label must clear every other label's current position, including
// later and still-blocked labels. No improvement borrows another label's slot.
type connectorTextPlacement struct {
	x, y  float64
	clear bool
	box   screenRect
}

func connectorTextBox(req connectorTextRequest, x, y float64) screenRect {
	left, top := svgCoord(x-req.block.width/2), svgCoord(y-req.block.height/2)
	return screenRect{left, top, left + svgCoord(req.block.width), top + svgCoord(req.block.height)}
}

func resolveConnectorText(requests []connectorTextRequest, bodies, legacyPills []screenRect, legacyObstacles []connectorTextObstacle, strokes []connectorTextStroke) []connectorTextPlacement {
	if len(requests) == 0 {
		return nil
	}
	positions := make([]connectorTextPlacement, len(requests))
	static := append(append([]screenRect(nil), bodies...), legacyPills...)
	placed := append([]screenRect(nil), static...)
	// Reproduce the previous AABB arrangement first, including its midpoint
	// fallback. This makes the compatibility baseline a whole-scene state.
	for i, req := range requests {
		obs := append([]screenRect(nil), placed...)
		for _, o := range legacyObstacles {
			if o.owner != req.index {
				obs = append(obs, o.rect)
			}
		}
		x, y, clear := connectorTextPosition(req.points, req.curved, req.block.width, req.block.height, obs)
		box := connectorTextBox(req, x, y)
		positions[i] = connectorTextPlacement{x, y, clear, box}
		placed = append(placed, box)
	}
	for i := range positions {
		for j := range positions {
			if i != j && collides(positions[i].box, []screenRect{positions[j].box}) {
				positions[i].clear = false
			}
		}
	}
	// Arrows keep their conservative box. The route-stroke boxes are replaced
	// only by envelopes that include real width, caps, joins and curve error.
	for _, o := range legacyObstacles {
		if o.owner < 0 {
			static = append(static, o.rect)
		}
	}
	for i, req := range requests {
		if positions[i].clear {
			continue
		}
		obs := append([]screenRect(nil), static...)
		for j, p := range positions {
			if i != j {
				obs = append(obs, p.box)
			}
		}
		var other []connectorTextStroke
		for _, stroke := range strokes {
			if stroke.owner != req.index {
				other = append(other, stroke)
			}
		}
		x, y, clear := connectorTextPosition(req.points, req.curved, req.block.width, req.block.height, obs, other...)
		if clear {
			positions[i] = connectorTextPlacement{x, y, true, connectorTextBox(req, x, y)}
		}
	}
	// Report final geometry, including formerly conservative failures that
	// were already clear at the retained midpoint. Never keep a stale claim.
	for i, req := range requests {
		// Routes do not change during this pass. Retain either the exact
		// curve-AABB baseline proof or a successful envelope-search proof.
		// A chord's error square may extend outside the true curve AABB;
		// that extra conservatism must not revoke an existing certificate.
		routeClear := positions[i].clear
		obs := append([]screenRect(nil), static...)
		for j, p := range positions {
			if i != j {
				obs = append(obs, p.box)
			}
		}
		positions[i].clear = !collides(positions[i].box, obs)
		if routeClear {
			continue
		}
		for _, stroke := range strokes {
			if stroke.owner != req.index && connectorTextStrokeHits(positions[i].box, []connectorTextStroke{stroke}) {
				positions[i].clear = false
				break
			}
		}
	}
	return positions
}

// The legacy obstacles preserve clean accepted positions and their safety
// certificates. Refined placements use the tighter swept stroke envelopes.
func connectorTextStrokeObstacles(owner int, pts [][2]float64, curved bool, width float64) []connectorTextObstacle {
	var out []connectorTextObstacle
	add := func(p [][2]float64, q bool) {
		b := connectorStrokeBounds(p, q, width)
		out = append(out, connectorTextObstacle{owner, screenRect{b.minX, b.minY, b.maxX, b.maxY}})
	}
	if curved {
		add(pts, true)
	} else {
		for i := 1; i < len(pts); i++ {
			add(pts[i-1:i+1], false)
		}
	}
	return out
}
