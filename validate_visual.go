package isotopo

import (
	"fmt"
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// iconHasColorSuffix returns true when the iso:// URI ends with a 6-hex-digit
// color override (e.g. "iso://glyph/database/4B5563"). These always have the
// form "…/<6 hex chars>" at the end.
func iconHasColorSuffix(uri string) bool {
	parts := strings.Split(uri, "/")
	if len(parts) == 0 {
		return false
	}
	last := parts[len(parts)-1]
	if len(last) != 6 {
		return false
	}
	for _, c := range last {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ── colour helpers ────────────────────────────────────────────────────────────

// parseHex parses #RRGGBB or #RGB; returns (r,g,b,true) or (0,0,0,false).
func parseHex(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) == 0 || s[0] != '#' {
		return
	}
	s = s[1:]
	switch len(s) {
	case 6:
		var v uint32
		for _, c := range s {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= uint32(c - '0')
			case c >= 'a' && c <= 'f':
				v |= uint32(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v |= uint32(c-'A') + 10
			default:
				return
			}
		}
		r, g, b = uint8(v>>16), uint8(v>>8), uint8(v)
		ok = true
	case 3:
		var v uint32
		for _, c := range s {
			v <<= 4
			var nib uint32
			switch {
			case c >= '0' && c <= '9':
				nib = uint32(c - '0')
			case c >= 'a' && c <= 'f':
				nib = uint32(c-'a') + 10
			case c >= 'A' && c <= 'F':
				nib = uint32(c-'A') + 10
			default:
				return
			}
			v |= nib
		}
		r = uint8((v>>8)&0xf*17)
		g = uint8((v>>4)&0xf*17)
		b = uint8(v&0xf*17)
		ok = true
	}
	return
}

func sRGBLinear(c uint8) float64 {
	f := float64(c) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

func relativeLuminance(r, g, b uint8) float64 {
	return 0.2126*sRGBLinear(r) + 0.7152*sRGBLinear(g) + 0.0722*sRGBLinear(b)
}

func contrastRatio(l1, l2 float64) float64 {
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// lumOf parses a hex colour and returns (luminance, true) or (0, false).
func lumOf(hex string) (float64, bool) {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return 0, false
	}
	return relativeLuminance(r, g, b), true
}

// topFillColors returns EVERY candidate colour the renderer may paint on the
// top face, in the renderer's own precedence: the v3.3 per-face `faces.top`
// override (which OUTRANKS palette) first, then palette, then the default box
// fill. A gradient contributes all its stops so the contrast check can test the
// worst one. Reading `faces` is what stops a `faces`-styled dark node (every
// preset uses faces, not palette) from being silently checked against the light
// default fill — the cause of dark-text-on-dark-faces passing validation.
func topFillColors(st *Style) []string {
	if st != nil && st.Faces != nil {
		if fs := st.Faces["top"]; fs != nil && fs.Fill != nil {
			if cs := fillSpecColors(fs.Fill); len(cs) > 0 {
				return cs
			}
		}
	}
	if st != nil && st.Palette != nil {
		if st.Palette.Top != "" {
			return []string{st.Palette.Top}
		}
		if g := st.Palette.TopGradient; g != nil {
			var out []string
			if g.From != "" {
				out = append(out, g.From)
			}
			if g.To != "" {
				out = append(out, g.To)
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	// Single source of truth: the same default the renderer paints.
	return []string{iso25d.DefaultIsoBox().TopFill}
}

// fillSpecColors returns a FillSpec's representative colours: the solid colour,
// or every gradient stop.
func fillSpecColors(f *FillSpec) []string {
	if f == nil {
		return nil
	}
	var out []string
	if f.Color != "" {
		out = append(out, f.Color)
	}
	for _, s := range f.Stops {
		if s.Color != "" {
			out = append(out, s.Color)
		}
	}
	// A pattern face (hatch/dots) paints its ink colour over the face; that ink
	// is the surface a label sits against, so it must be read too — otherwise a
	// pattern-topped node falls through to the default fill and is checked
	// against a colour that is never painted.
	if f.Pattern != nil && f.Pattern.Color != "" {
		out = append(out, f.Pattern.Color)
	}
	return out
}

// topFillColor returns a single representative top-face fill (the first
// candidate) for checks that need one colour (vs canvas background, vs icon).
func topFillColor(st *Style) string {
	if cs := topFillColors(st); len(cs) > 0 {
		return cs[0]
	}
	return iso25d.DefaultIsoBox().TopFill
}

// visuallySeparated reports whether a part carries its own separation from the
// canvas — a drop shadow, a silhouette outline, or a stroke. Such a part never
// "vanishes into the background" even when its top fill nearly matches the
// canvas (the intentional white-card-on-light-grey design language), so the
// background-contrast heuristic must not fire on it.
func visuallySeparated(st *Style) bool {
	if st == nil {
		return false
	}
	if st.Stroke != nil && st.Stroke.Color != "" {
		return true
	}
	if st.Effects != nil && (st.Effects.DropShadow != nil || st.Effects.Outline != nil || st.Effects.Backglow != nil) {
		return true
	}
	return false
}

func textColor(st *Style) string {
	if st != nil && st.Text != nil && st.Text.Color != "" {
		return st.Text.Color
	}
	return "#1E293B"
}

// faceSplitIssues warns when effects.faceSplit is enabled but cannot take
// effect, so the flag is never a silent no-op. It applies only to the rounded
// box family (needs cornerRadius>0): on a sharp box the faces already shade
// independently, and a non-box shape ignores it entirely.
func faceSplitIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue
	for nodeID, n := range doc.Nodes {
		prefix := fmt.Sprintf("nodes.%s", nodeID)
		var walk func(parts []*CompositePart, path string)
		walk = func(parts []*CompositePart, path string) {
			for _, p := range parts {
				if p == nil {
					continue
				}
				pPath := fmt.Sprintf("%s.parts[%s]", path, p.ID)
				eff := ResolveStyleWithRole(doc.Theme, p.Shape, p.Role, p.Preset, p.Style)
				if eff != nil && eff.Effects != nil && eff.Effects.FaceSplit != nil && *eff.Effects.FaceSplit {
					cr := 0.0
					if eff.Effects.CornerRadius != nil {
						cr = *eff.Effects.CornerRadius
					}
					switch {
					case !boxFamilyShape(p.Shape):
						issues = append(issues, Issue{
							Severity: SeverityWarning,
							Path:     pPath + ".style.effects.faceSplit",
							Message:  fmt.Sprintf("faceSplit applies only to the rounded box family; shape %q ignores it", p.Shape),
						})
					case cr <= 0:
						issues = append(issues, Issue{
							Severity: SeverityWarning,
							Path:     pPath + ".style.effects.faceSplit",
							Message:  "faceSplit has no effect without cornerRadius>0 — a sharp box already shades its left/right faces independently",
						})
					}
				}
				walk(p.Parts, pPath)
			}
		}
		walk(n.Parts, prefix)
	}
	return issues
}

// ── 1. VisualContrastIssues ───────────────────────────────────────────────────

// VisualContrastIssues checks colour contrast ratios:
//
//   - top face fill vs label text colour (< 3.0 → warning)
//   - top face fill vs canvas background (< 1.5 → warning)
//   - group/boundary top fill vs direct child top fill (< 1.3 → warning)
//   - iso:// icon with default ink on a dark top fill (luminance < 0.18, no /light or /RRGGBB suffix → warning)
func VisualContrastIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue

	// Canvas background luminance
	canvasBg := "#F8FAFC"
	if doc.Canvas != nil && doc.Canvas.Background != "" {
		canvasBg = doc.Canvas.Background
	}
	bgLum, bgOK := lumOf(canvasBg)

	for nodeID, n := range doc.Nodes {
		prefix := fmt.Sprintf("nodes.%s", nodeID)
		var walkContrast func(parts []*CompositePart, pathPrefix string)
		walkContrast = func(parts []*CompositePart, pathPrefix string) {
			for _, p := range parts {
				if p == nil {
					continue
				}
				pPath := fmt.Sprintf("%s.parts[%s]", pathPrefix, p.ID)
				if p.ID == "" {
					pPath = pathPrefix + ".parts[?]"
				}

				// Resolve the EFFECTIVE style the renderer actually paints
				// (theme → per-shape → preset → node overrides) before reading
				// colours — otherwise preset-/gradient-sourced fills are invisible
				// here and the check false-flags them against the default box fill.
				eff := ResolveStyleWithRole(doc.Theme, p.Shape, p.Role, p.Preset, p.Style)
				fill := topFillColor(eff)
				fillLum, fillOK := lumOf(fill)

				if fillOK {
					// a) top fill vs label text — only meaningful when the part
					// actually carries a label on its face. Test the WORST top-face
					// fill (a gradient is readable at one stop, invisible at the
					// other), and escalate a near-invisible result to an error:
					// such a label is gone in the render, not merely cramped.
					txt := textColor(eff)
					if txtLum, ok := lumOf(txt); ok && p.Label != "" {
						// Track the worst stop (for the warning) AND the best (for
						// the error). A gradient is flagged if ANY stop is low
						// contrast, but only escalated to an error when EVEN ITS
						// MOST READABLE stop is near-invisible — so a small light
						// sheen highlight over an otherwise-dark face (where the
						// label is plainly readable) stays a warning, while a solid
						// dark-on-dark face (every stop fails) is an error.
						worst, best, worstFill := math.Inf(1), math.Inf(-1), fill
						for _, f := range topFillColors(eff) {
							if fl, ok := lumOf(f); ok {
								cr := contrastRatio(fl, txtLum)
								if cr < worst {
									worst, worstFill = cr, f
								}
								if cr > best {
									best = cr
								}
							}
						}
						if !math.IsInf(worst, 1) && worst < 3.0 {
							sev, bound := SeverityWarning, "3.0"
							if best < 1.5 {
								sev, bound = SeverityError, "1.5 across the whole face — text is effectively invisible"
							}
							issues = append(issues, Issue{
								Severity: sev,
								Path:     pPath + ".style.fill",
								Message:  fmt.Sprintf("low contrast between top fill %s and text %s (ratio %.2f < %s)", worstFill, txt, worst, bound),
							})
						}
					}

					// b) top fill vs canvas background — skipped when the part
					// separates itself from the canvas via shadow/outline/stroke,
					// or is an iso_text (text-only: it has no real box, so its fill
					// is deliberately the background; its readability is the
					// text-vs-background contrast tested in (a), not this).
					if bgOK && !visuallySeparated(eff) && p.Shape != "iso_text" {
						cr := contrastRatio(fillLum, bgLum)
						if cr < 1.5 {
							issues = append(issues, Issue{
								Severity: SeverityWarning,
								Path:     pPath + ".style.fill",
								Message:  fmt.Sprintf("top fill %s is nearly indistinguishable from canvas background %s (ratio %.2f < 1.5)", fill, canvasBg, cr),
							})
						}
					}

					// c) icon vs top fill — the default ink color (#1F2937) is used
					// when no /light or /RRGGBB suffix is present. On a dark top face
					// that ink is invisible. Warn so authors add /light (or /RRGGBB).
					if p.Icon != "" && fillLum < 0.18 {
						uri := p.Icon
						hasSuffix := strings.Contains(uri, "/light") ||
							iconHasColorSuffix(uri)
						isBuiltin := strings.HasPrefix(uri, "iso://glyph/") ||
							strings.HasPrefix(uri, "iso://si/") ||
							strings.HasPrefix(uri, "iso://brand/")
						if isBuiltin && !hasSuffix {
							issues = append(issues, Issue{
								Severity: SeverityWarning,
								Path:     pPath + ".icon",
								Message:  fmt.Sprintf("icon %q uses default ink color on a dark top fill %s — add /light or /RRGGBB suffix (e.g. %s/light) so the icon is visible", uri, fill, uri),
								Suggest:  uri + "/light",
							})
						} else if !isBuiltin && fillLum < 0.18 {
							issues = append(issues, Issue{
								Severity: SeverityWarning,
								Path:     pPath + ".icon",
								Message:  fmt.Sprintf("external icon on dark top fill %s — verify the icon has a light-colored or transparent background so it is visible (isotopo cannot check external icon colors)", fill),
							})
						}
					}
				}

				// c) group/boundary vs direct children — skipped when the child
				// separates itself from the tray by a NON-fill channel (border,
				// drop shadow, glow). Premium card styles (e.g. white card on a
				// white tray) read fine via the shadow/border; only a child with
				// no such channel actually melts into the slab.
				if isContainerShape(p.Shape) && fillOK {
					for _, child := range p.Parts {
						if child == nil {
							continue
						}
						childEff := ResolveStyleWithRole(doc.Theme, child.Shape, child.Role, child.Preset, child.Style)
						if visuallySeparated(childEff) {
							continue
						}
						childFill := topFillColor(childEff)
						if childLum, ok := lumOf(childFill); ok {
							cr := contrastRatio(fillLum, childLum)
							if cr < 1.3 {
								childPath := fmt.Sprintf("%s.parts[%s]", pPath, child.ID)
								issues = append(issues, Issue{
									Severity: SeverityWarning,
									Path:     childPath + ".style.fill",
									Message:  fmt.Sprintf("child top fill %s has low contrast against group fill %s (ratio %.2f < 1.3) and no border/shadow to separate them", childFill, fill, cr),
								})
							}
						}
					}
					walkContrast(p.Parts, pPath)
				} else {
					walkContrast(p.Parts, pPath)
				}
			}
		}
		walkContrast(n.Parts, prefix)
	}
	return issues
}

// ── 2 + 3. labelIssues ───────────────────────────────────────────────────────

// labelIssues checks label truncation risk (item 2) and label length (item 3).
func labelIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue

	for nodeID, n := range doc.Nodes {
		prefix := fmt.Sprintf("nodes.%s", nodeID)
		var walkLabel func(parts []*CompositePart, pathPrefix string)
		walkLabel = func(parts []*CompositePart, pathPrefix string) {
			for _, p := range parts {
				if p == nil {
					continue
				}
				pPath := fmt.Sprintf("%s.parts[%s]", pathPrefix, p.ID)
				if p.ID == "" {
					pPath = pathPrefix + ".parts[?]"
				}

				// Only leaf parts (non-container, or container but we still check label)
				if p.Label != "" {
					// Item 3: label length
					runes := []rune(p.Label)
					if len(runes) > 40 {
						issues = append(issues, Issue{
							Severity: SeverityWarning,
							Path:     pPath + ".label",
							Message:  fmt.Sprintf("label is long (%d chars); consider using a shorter label or splitting into label+sublabel", len(runes)),
						})
					}

					// Item 2: truncation risk
					if p.Geom != nil && p.Geom.W > 0 {
						// Rune-aware estimate — byte counting inflated
						// CJK labels ~3× (3 bytes per ideograph).
						estPx := estimateLabelPx(p.Label)
						availPx := int(p.Geom.W * 40 * 0.9 * 0.95)
						if estPx > availPx {
							issues = append(issues, Issue{
								Severity: SeverityWarning,
								Path:     pPath + ".label",
								Message:  fmt.Sprintf("label may be truncated: estimated width %dpx exceeds node width %dupx", estPx, int(p.Geom.W*40)),
							})
						}
					}
				}

				walkLabel(p.Parts, pPath)
			}
		}
		walkLabel(n.Parts, prefix)
	}
	return issues
}

// ── 4. outDegreeIssues ───────────────────────────────────────────────────────

// outDegreeIssues warns when a single part has 5 or more outgoing connectors.
func outDegreeIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue

	scene := doc.Scene()
	if scene == nil {
		return nil
	}

	// Count out-degree per part id (strip anchor suffix)
	outCount := map[string]int{}
	firstIdx := map[string]int{}
	for i, c := range scene.Connectors {
		if c.From == "" {
			continue
		}
		id := connectorTarget(c.From)
		outCount[id]++
		if _, seen := firstIdx[id]; !seen {
			firstIdx[id] = i
		}
	}

	// Emit at most one issue per part (the first connector that pushed it to ≥5)
	type hit struct {
		id    string
		count int
		idx   int
	}
	var hits []hit
	for id, cnt := range outCount {
		if cnt >= 5 {
			hits = append(hits, hit{id, cnt, firstIdx[id]})
		}
	}
	// Stable order
	for i := 0; i < len(hits); i++ {
		for j := i + 1; j < len(hits); j++ {
			if hits[i].idx > hits[j].idx {
				hits[i], hits[j] = hits[j], hits[i]
			}
		}
	}
	for _, h := range hits {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     fmt.Sprintf("nodes.scene.connectors[%d]", h.idx),
			Message:  fmt.Sprintf("part %q has %d outgoing connectors; consider grouping into a sub-diagram", h.id, h.count),
		})
	}
	return issues
}

// ── 5. connectorTierIssues ─────────────────────────────────────────────────────

// connectorTierIssues warns when a connector's two endpoints sit at
// different base heights (wz / tier). Orthogonal connectors hug the
// ground plane, so a height mismatch forces a vertical drop segment
// (riser): fine when it expresses a deliberate cross-tier call, but
// usually it's an accident — peer nodes were given different wz, or one
// endpoint is inside a group whose substrate height lifted it. The base
// heights are read from the SAME lowering the renderer uses, so group
// elevation (geom.h) and stacks are accounted for exactly.
func connectorTierIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	scene := doc.Scene()
	if scene == nil || len(scene.Connectors) == 0 {
		return nil
	}

	// Flatten parts exactly as the renderer does, then index each
	// atomic part's absolute base z by id.
	flat := lowerCompositeParts(scene.Parts, 0, 0, 0)
	wzByID := make(map[string]float64, len(flat))
	for _, p := range flat {
		if p == nil || p.ID == "" {
			continue
		}
		if p.Offset != nil {
			wzByID[p.ID] = p.Offset.WZ
		} else {
			wzByID[p.ID] = 0
		}
	}

	const eps = 0.5
	var issues []Issue
	for i, c := range scene.Connectors {
		if c.From == "" || c.To == "" {
			continue
		}
		fromID := connectorTarget(c.From)
		toID := connectorTarget(c.To)
		fz, ok1 := wzByID[fromID]
		tz, ok2 := wzByID[toID]
		if !ok1 || !ok2 {
			continue // unresolved endpoints are caught by the reference checks
		}
		if math.Abs(fz-tz) > eps {
			issues = append(issues, Issue{
				Severity: SeverityWarning,
				Path:     fmt.Sprintf("nodes.scene.connectors[%d]", i),
				Message: fmt.Sprintf(
					"connector endpoints %q (wz %.0f) and %q (wz %.0f) sit on different tiers; the orthogonal route will sprout a vertical drop segment. Put both on the same wz unless this is a deliberate cross-tier call.",
					fromID, fz, toID, tz),
			})
		}
	}
	return issues
}

// ── 5. nestingIssues ─────────────────────────────────────────────────────────

// nestingIssues warns when group nesting exceeds 3 levels deep.
func nestingIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}
	var issues []Issue

	for nodeID, n := range doc.Nodes {
		prefix := fmt.Sprintf("nodes.%s", nodeID)
		var walkNesting func(parts []*CompositePart, pathPrefix string, depth int)
		walkNesting = func(parts []*CompositePart, pathPrefix string, depth int) {
			for _, p := range parts {
				if p == nil {
					continue
				}
				pPath := fmt.Sprintf("%s.parts[%s]", pathPrefix, p.ID)
				if p.ID == "" {
					pPath = pathPrefix + ".parts[?]"
				}
				if depth > 3 {
					issues = append(issues, Issue{
						Severity: SeverityWarning,
						Path:     pPath,
						Message:  fmt.Sprintf("nesting depth %d exceeds recommended maximum of 3", depth),
					})
				}
				if isContainerShape(p.Shape) {
					walkNesting(p.Parts, pPath, depth+1)
				}
			}
		}
		walkNesting(n.Parts, prefix, 1)
	}
	return issues
}

// ── 6. styleConsistencyIssues ────────────────────────────────────────────────

// styleConsistencyIssues finds clusters of nodes that share the same visual
// strategy (same preset, or same bucketed fill+stroke when no preset is used),
// then flags any node whose text color or icon suffix pattern is an outlier
// within its cluster.
//
// Algorithm:
//  1. Collect every non-container leaf part with resolved fill, textColor, icon.
//  2. Assign each a cluster key: preset name if set, else "<fillBucket>/<strokeBucket>".
//  3. For clusters of size ≥ 2, compute the majority text color and icon-suffix
//     pattern (none / light / hex).
//  4. Any part that disagrees with the majority emits a warning.
func styleConsistencyIssues(doc *Document) []Issue {
	if doc == nil {
		return nil
	}

	type partInfo struct {
		id          string
		path        string
		clusterKey  string
		textColor   string // resolved, lowercase #rrggbb
		iconSuffix  string // "none", "light", or "hex"
	}

	var parts []partInfo

	for nodeID, n := range doc.Nodes {
		prefix := fmt.Sprintf("nodes.%s", nodeID)
		var walk func(ps []*CompositePart, pathPrefix string)
		walk = func(ps []*CompositePart, pathPrefix string) {
			for _, p := range ps {
				if p == nil {
					continue
				}
				pPath := fmt.Sprintf("%s.parts[%s]", pathPrefix, p.ID)
				if p.ID == "" {
					pPath = pathPrefix + ".parts[?]"
				}

				eff := ResolveStyleWithRole(doc.Theme, p.Shape, p.Role, p.Preset, p.Style)
				fill := topFillColor(eff)

				// Cluster key
				key := ""
				if p.Preset != "" {
					key = "preset:" + p.Preset
				} else {
					key = "fill:" + luminanceBucket(fill) + "/stroke:" + resolvedStrokeBucket(eff)
				}

				// Icon suffix pattern
				suffix := "none"
				if p.Icon != "" {
					uri := p.Icon
					if strings.Contains(uri, "/light") {
						suffix = "light"
					} else if iconHasColorSuffix(uri) {
						suffix = "hex"
					}
				}

				parts = append(parts, partInfo{
					id:         p.ID,
					path:       pPath,
					clusterKey: key,
					textColor:  strings.ToLower(textColor(eff)),
					iconSuffix: suffix,
				})

				walk(p.Parts, pPath)
			}
		}
		walk(n.Parts, prefix)
	}

	// Group by cluster key
	byCluster := map[string][]partInfo{}
	for _, pi := range parts {
		byCluster[pi.clusterKey] = append(byCluster[pi.clusterKey], pi)
	}

	var issues []Issue
	for _, cluster := range byCluster {
		if len(cluster) < 2 {
			continue
		}

		// Majority text color
		textFreq := map[string]int{}
		for _, pi := range cluster {
			if pi.textColor != "" {
				textFreq[pi.textColor]++
			}
		}
		majorityText := majorityKey(textFreq)

		// Majority icon suffix
		suffixFreq := map[string]int{}
		for _, pi := range cluster {
			if pi.iconSuffix != "none" {
				suffixFreq[pi.iconSuffix]++
			}
		}
		// Only consider suffix majority when most nodes in the cluster carry an icon
		iconsPresent := suffixFreq["light"] + suffixFreq["hex"]
		majorityIconSuffix := ""
		if iconsPresent*2 > len(cluster) { // strict majority have an icon
			majorityIconSuffix = majorityKey(suffixFreq)
		}

		for _, pi := range cluster {
			// Text color outlier
			if majorityText != "" && pi.textColor != "" && pi.textColor != majorityText {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     pi.path + ".style.text.color",
					Message: fmt.Sprintf(
						"node %q text color %s differs from the cluster majority %s — nodes sharing the same visual strategy (%s) should use consistent label colors",
						pi.id, pi.textColor, majorityText, pi.clusterKey),
				})
			}

			// Icon suffix outlier (only when the cluster has a clear majority)
			if majorityIconSuffix != "" && pi.iconSuffix != "none" && pi.iconSuffix != majorityIconSuffix {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     pi.path + ".icon",
					Message: fmt.Sprintf(
						"node %q uses icon suffix %q but cluster majority uses %q — nodes in the same visual group should use the same icon color convention",
						pi.id, pi.iconSuffix, majorityIconSuffix),
				})
			}
		}
	}
	return issues
}

// luminanceBucket buckets a hex fill colour into "dark", "mid", or "light"
// for cluster-key purposes when no preset is present.
func luminanceBucket(hex string) string {
	lum, ok := lumOf(hex)
	if !ok {
		return "unknown"
	}
	switch {
	case lum < 0.18:
		return "dark"
	case lum < 0.50:
		return "mid"
	default:
		return "light"
	}
}

// resolvedStrokeBucket returns the first two characters of the stroke color
// (enough to cluster by hue family) or "none" when no stroke is set.
func resolvedStrokeBucket(st *Style) string {
	if st == nil || st.Stroke == nil || st.Stroke.Color == "" {
		return "none"
	}
	c := strings.ToLower(strings.TrimPrefix(st.Stroke.Color, "#"))
	if len(c) >= 2 {
		return c[:2]
	}
	return c
}

// majorityKey returns the key with the highest count; ties go to lexically
// smaller key so the result is deterministic.
func majorityKey(freq map[string]int) string {
	best, bestCount := "", 0
	for k, cnt := range freq {
		if cnt > bestCount || (cnt == bestCount && k < best) {
			best, bestCount = k, cnt
		}
	}
	return best
}


