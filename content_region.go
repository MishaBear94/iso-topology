package isotopo

import (
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// partContentRegions observes each rendered text line or image viewport after
// wrapping/font fitting. Bound fonts use shaped ink boxes; other text uses
// estimates. Placement and projection come from the shared renderer layout.
func partContentRegions(p *CompositePart, theme *Theme, kind string) screenRegion {
	if p == nil {
		return nil
	}
	merged := ResolveStyleWithRole(theme, p.Shape, p.Role, p.Preset, p.Style)
	if merged != nil && merged.Text != nil && merged.Text.Orient == "screen" && kind == "text" {
		return nil
	}
	shape, o := flattenCompositeContent(p, theme)
	if kind == "text" && (strings.EqualFold(o.FontColor, "none") || strings.EqualFold(o.FontColor, "transparent")) {
		return nil
	}
	content := iso25d.ShapeContent(shape, o)
	var dx, dy float64
	if p.Offset != nil {
		dx, dy = projectIso(p.Offset.WX, p.Offset.WY, p.Offset.WZ)
	}
	var out screenRegion
	for _, e := range content.Elements {
		if e.Kind != kind || (kind == "text" && strings.TrimSpace(e.Text) == "") {
			continue
		}
		poly := make([][2]float64, 4)
		for i, v := range e.Quad {
			poly[i] = [2]float64{v[0] + dx, v[1] + dy}
		}
		out = append(out, poly)
	}
	return out
}

// Match the composite renderer's suppression of on-face text BEFORE layout.
// In particular, a screen label leaves an icon-only top face, with its own
// size and placement; retaining the suppressed label would observe a layout
// that the renderer never submitted.
func flattenCompositeContent(p *CompositePart, theme *Theme) (string, iso25d.ConvertOpts) {
	label := p.Label
	if merged := ResolveStyleWithRole(theme, p.Shape, p.Role, p.Preset, p.Style); merged != nil && merged.Text != nil && merged.Text.Orient == "screen" {
		label = ""
	}
	return Flatten(&Node{Shape: p.Shape, Geom: p.Geom, Style: p.Style, Preset: p.Preset, Role: p.Role, Label: label, Icon: p.Icon, Content: p.Content}, theme)
}

func contentCovered(lines, paint screenRegion, threshold float64) bool {
	if len(paint) == 0 {
		return false
	}
	pb := regionBounds(paint)
	for _, line := range lines {
		r := screenRegion{line}
		if !occOverlap(regionBounds(r), pb) {
			continue
		}
		area := regionArea(r)
		if area > 0 && regionIntersectionArea(r, paint)/area >= threshold {
			return true
		}
	}
	return false
}
