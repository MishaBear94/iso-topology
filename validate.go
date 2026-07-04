package isotopo

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// Severity is how loud an Issue is. Errors mean the document won't
// render correctly; warnings mean it'll render but probably not how
// the author intended.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one structural problem found by Validate. Path is a
// JSONPath-style locator (e.g. "nodes.scene.parts[3].shape"); Message
// is one line; Suggest is the closest valid value when relevant.
//
// Designed for agent self-correction: an agent reads JSON-serialized
// Issues, finds the path, applies the suggestion, and re-validates —
// no human in the loop.
type Issue struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Message  string   `json:"message"`
	Suggest  string   `json:"suggest,omitempty"`
	// Repairable marks an issue the projection-repair loop clears on its own:
	// `render` auto-fixes it in memory, and `isotopo repair --write` persists
	// the fix into the source. Set by MarkRepairable; absent means the issue
	// needs a hand edit.
	Repairable bool `json:"repairable,omitempty"`
}

// Validate runs structural checks over a parsed Document. It catches:
//   - unknown shape names (with nearest-neighbor suggestion)
//   - annotation anchors that don't resolve to any part id
//   - connector from/to references that don't resolve
//   - unknown canvas.grid values
//   - empty groups (no nested parts) and orphan stack declarations
//
// Validate does NOT check YAML syntax — Parse already does that. It
// runs on a successfully-decoded Document and answers "is this
// document logically coherent?".
// maxGeomDim is the largest dimension that survives the integer-snapped viewBox
// without overflowing — past this, ceilOuterDims' int() conversion wraps and the
// SVG comes out unusable (width=0 / int64-max). Comfortably above any real
// diagram, low enough to catch a fat-fingered exponent.
const maxGeomDim = 1e7

// checkGeom flags geometry that renders degenerate or corrupts the canvas:
// non-finite (NaN/Inf) dimensions emit literal "NaN"/"+Inf" into the SVG, and
// absurd magnitudes overflow the viewBox. Shared by the node-level and part-level
// walks so a top-level shape and a nested part get the same feedback. Negative is
// a warning (zero is the legitimate "auto" case); non-finite/overflow is an error.
func checkGeom(g *Geom, path string, issues *[]Issue) {
	if g == nil {
		return
	}
	for _, d := range []struct {
		name string
		v    float64
	}{{"w", g.W}, {"d", g.D}, {"h", g.H}} {
		switch {
		case math.IsNaN(d.v) || math.IsInf(d.v, 0):
			*issues = append(*issues, Issue{
				Severity: SeverityError,
				Path:     fmt.Sprintf("%s.geom.%s", path, d.name),
				Message:  fmt.Sprintf("dimension %s must be a finite number, got %g", d.name, d.v),
			})
		case d.v < 0:
			*issues = append(*issues, Issue{
				Severity: SeverityWarning,
				Path:     fmt.Sprintf("%s.geom.%s", path, d.name),
				Message:  fmt.Sprintf("negative dimension %s=%g renders degenerate", d.name, d.v),
			})
		case d.v > maxGeomDim:
			*issues = append(*issues, Issue{
				Severity: SeverityError,
				Path:     fmt.Sprintf("%s.geom.%s", path, d.name),
				Message:  fmt.Sprintf("dimension %s=%g is too large; the canvas overflows", d.name, d.v),
			})
		}
	}
	// A huge polygon side count is a render-time resource sink — flag it (the
	// renderer also hard-caps at maxPrismSides as the actual safety net).
	if g.Sides > maxPrismSides {
		*issues = append(*issues, Issue{
			Severity: SeverityError,
			Path:     path + ".geom.sides",
			Message:  fmt.Sprintf("geom.sides=%d is too large (max %d); it would exhaust render resources", g.Sides, maxPrismSides),
		})
	}
}

func Validate(doc *Document) []Issue {
	if doc == nil {
		return []Issue{{Severity: SeverityError, Path: "$", Message: "document is nil"}}
	}
	var issues []Issue

	issues = append(issues, themeRoleIssues(doc)...)

	// Build the universe of valid ids (every CompositePart in every
	// scene, including nested-in-groups). Connector and annotation
	// references must point into this set.
	//
	// Stack expansion is accounted for: a part with stack.count = 3
	// contributes ids "pods_a", "pods_a~1", "pods_a~2" — same ids the
	// lowering pass will emit at render time. Without this, every
	// existing showcase using stacked replicas would false-fail.
	allIDs := map[string]struct{}{}
	registerID := func(p *CompositePart) {
		if p.ID == "" {
			return
		}
		allIDs[p.ID] = struct{}{}
		if p.Stack != nil && p.Stack.Count > 1 {
			for k := 1; k < p.Stack.Count; k++ {
				allIDs[fmt.Sprintf("%s~%d", p.ID, k)] = struct{}{}
			}
		}
	}
	for _, n := range doc.Nodes {
		walkAtomicParts(n.Parts, registerID)
		// group nodes themselves are addressable too
		var collectGroupIDs func(parts []*CompositePart)
		collectGroupIDs = func(parts []*CompositePart) {
			for _, p := range parts {
				if isContainerShape(p.Shape) {
					registerID(p)
					collectGroupIDs(p.Parts)
				}
			}
		}
		collectGroupIDs(n.Parts)
	}

	// Duplicate part ids: connectors and annotations bind targets BY id, so two
	// parts sharing an id make every reference to it ambiguous — the renderer
	// silently picks the first and the second is unreachable. (Direct-manip
	// "duplicate" of a container used to mint exactly this; this catches it
	// wherever it comes from, including hand-edited YAML.)
	idCount := map[string]int{}
	var countIDs func(parts []*CompositePart)
	countIDs = func(parts []*CompositePart) {
		for _, p := range parts {
			if p == nil {
				continue
			}
			if p.ID != "" {
				idCount[p.ID]++
				// Mirror registerID's stack expansion so a replica id (a~1) that
				// collides with an explicit part of the same id is caught too.
				if p.Stack != nil && p.Stack.Count > 1 {
					for k := 1; k < p.Stack.Count; k++ {
						idCount[fmt.Sprintf("%s~%d", p.ID, k)]++
					}
				}
			}
			countIDs(p.Parts)
		}
	}
	for _, n := range doc.Nodes {
		countIDs(n.Parts)
	}
	dupIDs := make([]string, 0)
	for id, c := range idCount {
		if c > 1 {
			dupIDs = append(dupIDs, id)
		}
	}
	sort.Strings(dupIDs)
	for _, id := range dupIDs {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     "nodes",
			Message:  fmt.Sprintf("duplicate part id %q — %d parts share it; connector/annotation references to it are ambiguous", id, idCount[id]),
		})
	}

	// Validate canvas.projection
	if doc.Canvas != nil && doc.Canvas.Projection != "" {
		valid := []string{"iso", "top"}
		if !contains(valid, strings.ToLower(doc.Canvas.Projection)) {
			issues = append(issues, Issue{
				// Warning, not Error: render falls back to iso, so a typo
				// shouldn't blank the whole diagram.
				Severity: SeverityWarning,
				Path:     "canvas.projection",
				Message:  fmt.Sprintf("unknown projection %q (using iso)", doc.Canvas.Projection),
				Suggest:  nearest(doc.Canvas.Projection, valid),
			})
		}
	}

	// Validate canvas.grid
	if doc.Canvas != nil && doc.Canvas.Grid != "" {
		valid := []string{"iso", "dots", "hatch", "solid", "none"}
		if !contains(valid, strings.ToLower(doc.Canvas.Grid)) {
			issues = append(issues, Issue{
				// Warning, not Error: render falls back to the default grid.
				Severity: SeverityWarning,
				Path:     "canvas.grid",
				Message:  fmt.Sprintf("unknown grid mode %q (using default)", doc.Canvas.Grid),
				Suggest:  nearest(doc.Canvas.Grid, valid),
			})
		}
	}

	// v5.2 — canvas.depthSort only affects the iso painter order; the top-down
	// plan view has no painter stacking, so the combination is silently inert.
	// Surface it as a warning so the author knows the flag isn't doing anything.
	if doc.Canvas != nil && doc.Canvas.DepthSort && strings.ToLower(doc.Canvas.Projection) == "top" {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     "canvas.depthSort",
			Message:  "canvas.depthSort has no effect with projection: top (plan view has no painter stacking)",
			Suggest:  "remove depthSort or use the default iso projection",
		})
	}

	// canvas.padding feeds the viewBox directly; a non-finite or astronomically
	// large value overflows it (the renderer also clamps as the safety net).
	if doc.Canvas != nil {
		if p := doc.Canvas.Padding; math.IsNaN(p) || math.IsInf(p, 0) || p > maxGeomDim {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     "canvas.padding",
				Message:  fmt.Sprintf("canvas.padding=%g must be a finite, reasonable value", p),
			})
		}
	}

	validShapes := validShapeList()

	// Walk every Node and its Parts. We collect node and part issues
	// with full JSONPath-style locators.
	for nodeID, n := range doc.Nodes {
		nodePath := fmt.Sprintf("nodes.%s", nodeID)
		if !contains(validShapes, n.Shape) {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     nodePath + ".shape",
				Message:  fmt.Sprintf("unknown shape %q", n.Shape),
				Suggest:  nearest(n.Shape, validShapes),
			})
		}
		checkGeom(n.Geom, nodePath, &issues)
		for i, p := range n.Parts {
			validatePart(p, fmt.Sprintf("%s.parts[%d]", nodePath, i), validShapes, &issues)
		}
		for i, c := range n.Connectors {
			cPath := fmt.Sprintf("%s.connectors[%d]", nodePath, i)
			// A self-loop (from==to) projects to a zero-length segment: the
			// arrow points in an arbitrary direction and no route is visible.
			if c.From != "" && connectorTarget(c.From) == connectorTarget(c.To) {
				issues = append(issues, Issue{
					Severity: SeverityWarning,
					Path:     cPath,
					Message:  fmt.Sprintf("self-loop connector (from==to==%q) renders as a zero-length edge", connectorTarget(c.From)),
				})
			}
			if _, ok := allIDs[connectorTarget(c.From)]; !ok && c.From != "" {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Path:     cPath + ".from",
					Message:  fmt.Sprintf("connector references unknown part id %q", c.From),
					Suggest:  nearestID(c.From, allIDs),
				})
			}
			if _, ok := allIDs[connectorTarget(c.To)]; !ok && c.To != "" {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Path:     cPath + ".to",
					Message:  fmt.Sprintf("connector references unknown part id %q", c.To),
					Suggest:  nearestID(c.To, allIDs),
				})
			}
			// A bad anchor suffix ("db.lft") resolves the part fine then
			// silently falls back to top-mid — a wrong render with no signal.
			// Validate it so the agent gets a "did you mean .left" instead.
			checkAnchor(c.From, cPath+".from", allIDs, &issues)
			checkAnchor(c.To, cPath+".to", allIDs, &issues)
			// arrow/routing typos likewise fall back silently to the default.
			if c.Arrow != "" && !contains(arrowNames, c.Arrow) {
				issues = append(issues, Issue{
					// Warning, not Error: the renderer falls back to the default
					// arrow, so a typo shouldn't blank the whole diagram.
					Severity: SeverityWarning, Path: cPath + ".arrow",
					Message: fmt.Sprintf("unknown arrow %q (using default)", c.Arrow),
					Suggest: nearest(c.Arrow, arrowNames),
				})
			}
			if c.Routing != "" && !contains(routingNames, c.Routing) {
				issues = append(issues, Issue{
					// Warning, not Error: the renderer falls back to a straight
					// route, so a typo shouldn't blank the whole diagram.
					Severity: SeverityWarning, Path: cPath + ".routing",
					Message: fmt.Sprintf("unknown routing %q (using default)", c.Routing),
					Suggest: nearest(c.Routing, routingNames),
				})
			}
		}
	}

	// v2.5 — preset references must exist in theme.presets.
	presetNames := map[string]struct{}{}
	if doc.Theme != nil {
		for name := range doc.Theme.Presets {
			presetNames[name] = struct{}{}
		}
	}
	checkPreset := func(preset, path string) {
		if preset == "" {
			return
		}
		if _, ok := presetNames[preset]; !ok {
			cand := make([]string, 0, len(presetNames))
			for n := range presetNames {
				cand = append(cand, n)
			}
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path + ".preset",
				Message:  fmt.Sprintf("preset %q is not defined in theme.presets", preset),
				Suggest:  nearest(preset, cand),
			})
		}
	}
	for nodeID, n := range doc.Nodes {
		checkPreset(n.Preset, "nodes."+nodeID)
		var walkPresets func(parts []*CompositePart, prefix string)
		walkPresets = func(parts []*CompositePart, prefix string) {
			for i, p := range parts {
				if p == nil {
					continue
				}
				pp := fmt.Sprintf("%s.parts[%d]", prefix, i)
				checkPreset(p.Preset, pp)
				walkPresets(p.Parts, pp)
			}
		}
		walkPresets(n.Parts, "nodes."+nodeID)
	}

	// v2.9 — icon URIs must resolve: an unknown iso://… icon renders
	// as a silently-missing image, which is worse than an error.
	checkIcon := func(icon, path string) {
		if icon == "" {
			return
		}
		// Only built-in iso://… refs are validated against the catalog. Any
		// other value (a data: URI, an http(s) URL, or a local file path the
		// renderer inlines) is an external resource — pass it through.
		if !strings.HasPrefix(icon, "iso://") {
			return
		}
		if iso25d.KnownIsoIconURI(icon) {
			return
		}
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     path + ".icon",
			Message:  fmt.Sprintf("unknown built-in icon %q", icon),
			Suggest:  nearest(icon, iso25d.IconNames()),
		})
	}
	for nodeID, n := range doc.Nodes {
		checkIcon(n.Icon, "nodes."+nodeID)
		var walkIcons func(parts []*CompositePart, prefix string)
		walkIcons = func(parts []*CompositePart, prefix string) {
			for i, p := range parts {
				if p == nil {
					continue
				}
				pp := fmt.Sprintf("%s.parts[%d]", prefix, i)
				checkIcon(p.Icon, pp)
				walkIcons(p.Parts, pp)
			}
		}
		walkIcons(n.Parts, "nodes."+nodeID)
	}

	// v3.3 — style.faces structural checks.
	checkFaces := func(st *Style, path string) {
		if st == nil || st.Faces == nil {
			return
		}
		kinds := []string{"solid", "linearGradient", "radialGradient", "pattern"}
		for name, face := range st.Faces {
			if face == nil || face.Fill == nil {
				continue
			}
			fp := fmt.Sprintf("%s.faces.%s.fill", path, name)
			f := face.Fill
			if f.Kind != "" && !contains(kinds, f.Kind) {
				issues = append(issues, Issue{
					Severity: SeverityError, Path: fp + ".kind",
					Message: fmt.Sprintf("unknown fill kind %q", f.Kind),
					Suggest: nearest(f.Kind, kinds),
				})
			}
			if strings.Contains(f.Kind, "Gradient") {
				if len(f.Stops) < 2 {
					issues = append(issues, Issue{
						Severity: SeverityError, Path: fp + ".stops",
						Message: "gradient needs at least 2 stops",
					})
				}
				for i, s := range f.Stops {
					if s.Offset < 0 || s.Offset > 1 {
						issues = append(issues, Issue{
							Severity: SeverityError,
							Path:     fmt.Sprintf("%s.stops[%d].offset", fp, i),
							Message:  "offset must be within 0..1",
						})
					}
				}
			}
			if f.Kind == "pattern" && f.Pattern == nil {
				issues = append(issues, Issue{
					Severity: SeverityError, Path: fp + ".pattern",
					Message: "kind: pattern requires a pattern block",
				})
			}
		}
	}
	for nodeID, n := range doc.Nodes {
		checkFaces(n.Style, "nodes."+nodeID+".style")
		var walkFaceStyles func(parts []*CompositePart, prefix string)
		walkFaceStyles = func(parts []*CompositePart, prefix string) {
			for i, p := range parts {
				if p == nil {
					continue
				}
				pp := fmt.Sprintf("%s.parts[%d]", prefix, i)
				checkFaces(p.Style, pp+".style")
				walkFaceStyles(p.Parts, pp)
			}
		}
		walkFaceStyles(n.Parts, "nodes."+nodeID)
	}
	if doc.Theme != nil {
		for name, ps := range doc.Theme.Presets {
			checkFaces(ps, "theme.presets."+name)
		}
	}

	// v2.2 — layout/place dry run (dangling refs, cycles, conflicting
	// constraints → errors; post-solve sibling overlaps → warnings).
	// Runs against a clone so Validate never mutates the document.
	for nodeID, n := range doc.Nodes {
		if n.Shape != "composite" {
			continue
		}
		for _, iss := range layoutIssues(n, doc.Canvas) {
			iss.Path = strings.Replace(iss.Path, "nodes.scene", "nodes."+nodeID, 1)
			issues = append(issues, iss)
		}
	}

	// Annotations — document-level and node-level (v3.0) share checks.
	type annAt struct {
		a    *Annotation
		path string
	}
	var annList []annAt
	for i, a := range doc.Annotations {
		annList = append(annList, annAt{a, fmt.Sprintf("annotations[%d]", i)})
	}
	for nodeID, n := range doc.Nodes {
		for i, a := range n.Annotations {
			annList = append(annList, annAt{a, fmt.Sprintf("nodes.%s.annotations[%d]", nodeID, i)})
		}
	}
	for _, item := range annList {
		a, aPath := item.a, item.path
		if a.Anchor == "" {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     aPath + ".anchor",
				Message:  "annotation requires an anchor (part id)",
			})
			continue
		}
		if _, ok := allIDs[a.Anchor]; !ok {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     aPath + ".anchor",
				Message:  fmt.Sprintf("annotation anchor %q does not match any part id", a.Anchor),
				Suggest:  nearestID(a.Anchor, allIDs),
			})
		}
		if a.Side != "" && !contains(annotationSides, a.Side) {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     aPath + ".side",
				Message:  fmt.Sprintf("unknown side %q", a.Side),
				Suggest:  nearest(a.Side, annotationSides),
			})
		}
	}

	// v: render-time geometry lint — cross-container part collisions a
	// structural pass can't see (see lint.go).
	issues = append(issues, VisualLint(doc)...)

	// v: plan-view footprint collisions — node/group footprints that touch,
	// cross, or cover one another in the top-down projection (see validate_plan.go).
	issues = append(issues, PlanFootprintIssues(doc)...)

	// v: label occlusion — a node sitting over a group label / title (the silent
	// render-time auto-fix surfaced as negative feedback; see occlusion.go).
	issues = append(issues, LabelOcclusionIssues(doc)...)

	// v: container fit — a child bursting a fixed-size group (>90% on an axis,
	// effective dims; see validate_fit.go).
	issues = append(issues, ContainerFitIssues(doc)...)

	// v: rounded side band — a solid palette.right silently dropped on a rounded
	// (or default-rounded group) part (see validate_rounded.go).
	issues = append(issues, RoundedSideIgnoredIssues(doc)...)

	// v: visual quality warnings (contrast, labels, connectivity, nesting).
	issues = append(issues, VisualContrastIssues(doc)...)
	issues = append(issues, labelIssues(doc)...)
	issues = append(issues, outDegreeIssues(doc)...)
	issues = append(issues, connectorTierIssues(doc)...)
	issues = append(issues, nestingIssues(doc)...)
	issues = append(issues, faceSplitIssues(doc)...)
	issues = append(issues, styleConsistencyIssues(doc)...)

	return issues
}

// validatePart recurses into one CompositePart's structural checks.
func validatePart(p *CompositePart, path string, validShapes []string, issues *[]Issue) {
	if p == nil {
		return
	}
	if !contains(validShapes, p.Shape) {
		*issues = append(*issues, Issue{
			Severity: SeverityError,
			Path:     path + ".shape",
			Message:  fmt.Sprintf("unknown shape %q", p.Shape),
			Suggest:  nearest(p.Shape, validShapes),
		})
	}
	if isContainerShape(p.Shape) && len(p.Parts) == 0 {
		*issues = append(*issues, Issue{
			Severity: SeverityWarning,
			Path:     path + ".parts",
			Message:  "group has no nested parts — renders as an empty substrate",
		})
	}
	if p.Stack != nil && p.Stack.Count <= 0 {
		*issues = append(*issues, Issue{
			Severity: SeverityWarning,
			Path:     path + ".stack.count",
			Message:  "stack.count must be > 0; ignored",
		})
	}
	// Negative numbers are always a mistake (zero is the legitimate "auto"
	// case, so only < 0 is flagged). A negative dimension renders degenerate;
	// a negative gap pulls a part back over its own anchor.
	checkGeom(p.Geom, path, issues)
	// Offset coordinates feed the projection directly; a non-finite one emits a
	// literal NaN/Inf into the SVG transform — same hazard as geom, caught here.
	if o := p.Offset; o != nil {
		for _, d := range []struct {
			name string
			v    float64
		}{{"wx", o.WX}, {"wy", o.WY}, {"wz", o.WZ}} {
			if math.IsNaN(d.v) || math.IsInf(d.v, 0) {
				*issues = append(*issues, Issue{
					Severity: SeverityError,
					Path:     fmt.Sprintf("%s.offset.%s", path, d.name),
					Message:  fmt.Sprintf("offset.%s must be a finite number, got %g", d.name, d.v),
				})
			}
		}
	}
	if p.Place != nil {
		// A place block with no recognized relation means the agent used a key
		// the decoder dropped (e.g. `below:` — not a relation; use `above` /
		// `inFrontOf`). The part then silently falls back to auto-layout.
		if p.Place.RightOf == "" && p.Place.LeftOf == "" && p.Place.InFrontOf == "" &&
			p.Place.Behind == "" && p.Place.Above == "" {
			*issues = append(*issues, Issue{
				Severity: SeverityWarning,
				Path:     path + ".place",
				Message:  "place has no recognized relation (one of " + strings.Join(placeRelations, "/") + "); the part falls back to auto-layout",
			})
		}
		for _, g := range []struct {
			name string
			v    *float64
		}{{"gap", p.Place.Gap}, {"gapX", p.Place.GapX}, {"gapY", p.Place.GapY}} {
			if g.v != nil && *g.v < 0 {
				*issues = append(*issues, Issue{
					Severity: SeverityWarning,
					Path:     fmt.Sprintf("%s.place.%s", path, g.name),
					Message:  fmt.Sprintf("negative %s pulls the part back over its anchor", g.name),
				})
			}
		}
	}
	if p.Layout != nil && p.Layout.Gap != nil && *p.Layout.Gap < 0 {
		*issues = append(*issues, Issue{
			Severity: SeverityWarning,
			Path:     path + ".layout.gap",
			Message:  "negative layout gap overlaps children",
		})
	}
	for i, child := range p.Parts {
		validatePart(child, fmt.Sprintf("%s.parts[%d]", path, i), validShapes, issues)
	}
}

func connectorTarget(ref string) string {
	if dot := strings.Index(ref, "."); dot >= 0 {
		return ref[:dot]
	}
	return ref
}

// checkAnchor flags a bad ".anchor" suffix on a connector endpoint. Only runs
// when the part id itself resolves (otherwise the id error already covers it),
// so the agent never gets two errors for one bad ref.
func checkAnchor(ref, path string, allIDs map[string]struct{}, issues *[]Issue) {
	dot := strings.Index(ref, ".")
	if dot < 0 {
		return
	}
	if _, ok := allIDs[ref[:dot]]; !ok {
		return
	}
	suffix := ref[dot+1:]
	if contains(anchorNames, suffix) {
		return
	}
	*issues = append(*issues, Issue{
		// Warning, not Error: the renderer falls back to top-mid, so a bad anchor
		// shouldn't blank the whole diagram — surface it without killing render.
		Severity: SeverityWarning,
		Path:     path,
		Message:  fmt.Sprintf("unknown anchor %q on %q — renders at top-mid; valid anchors: top/bottom/left/right/front/back (+ -mid)", suffix, ref[:dot]),
		Suggest:  nearest(suffix, anchorNames),
	})
}

func validShapeList() []string {
	cap := CapabilityReport()
	set := map[string]struct{}{}
	for _, s := range cap.Shapes {
		set[s.IsoName] = struct{}{}
		for _, alias := range s.AcceptedAs {
			set[alias] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// nearest returns the candidate with the smallest Levenshtein distance
// to `bad`, but only if that distance is reasonable (≤ 3) — otherwise
// the suggestion would be misleading.
func nearest(bad string, candidates []string) string {
	bad = strings.ToLower(strings.TrimSpace(bad))
	bestScore := 999
	best := ""
	for _, c := range candidates {
		d := levenshtein(bad, strings.ToLower(c))
		if d < bestScore {
			bestScore = d
			best = c
		}
	}
	if bestScore > 3 {
		return ""
	}
	return best
}

func nearestID(bad string, ids map[string]struct{}) string {
	cand := make([]string, 0, len(ids))
	for id := range ids {
		cand = append(cand, id)
	}
	return nearest(bad, cand)
}

// levenshtein is the classical edit-distance. Iterative two-row form.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// themeRoleIssues checks the named-theme surface: theme.use must name a
// built-in theme (a typo would silently render unthemed — the exact class of
// silent fallback capabilities warns agents about), and role: must be one of
// the semantic slots themes style (or a custom role the theme itself defines).
func themeRoleIssues(doc *Document) []Issue {
	var issues []Issue
	if doc.Theme != nil && doc.Theme.Use != "" {
		if _, ok := builtinThemes[doc.Theme.Use]; !ok {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     "theme.use",
				Message:  fmt.Sprintf("unknown theme %q — the scene would render unthemed", doc.Theme.Use),
				Suggest:  nearest(doc.Theme.Use, ThemeNames()),
			})
		}
	}
	validRoles := map[string]bool{}
	for _, r := range RoleNames {
		validRoles[r] = true
	}
	if doc.Theme != nil {
		for r := range doc.Theme.Roles { // a theme may define custom roles
			validRoles[r] = true
		}
	}
	known := make([]string, 0, len(validRoles))
	for r := range validRoles {
		known = append(known, r)
	}
	sort.Strings(known)
	for nodeID, n := range doc.Nodes {
		if n == nil {
			continue
		}
		var walk func(parts []*CompositePart, prefix string)
		walk = func(parts []*CompositePart, prefix string) {
			for i, p := range parts {
				if p == nil {
					continue
				}
				path := fmt.Sprintf("%s.parts[%d]", prefix, i)
				if p.ID != "" {
					path = fmt.Sprintf("%s.parts[%s]", prefix, p.ID)
				}
				if p.Role != "" && !validRoles[p.Role] {
					issues = append(issues, Issue{
						Severity: SeverityWarning,
						Path:     path + ".role",
						Message:  fmt.Sprintf("unknown role %q (known: %s) — the part renders unstyled by the theme", p.Role, strings.Join(known, ", ")),
						Suggest:  nearest(p.Role, known),
					})
				}
				walk(p.Parts, path)
			}
		}
		walk(n.Parts, "nodes."+nodeID)
	}
	return issues
}
