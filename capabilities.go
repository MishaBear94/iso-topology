package isotopo

//go:generate go run ./tools/gen-docs

import (
	"sort"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// Capabilities is the agent-facing, structured description of what
// this build of isotopo can express. Emitted by the `isotopo
// capabilities` subcommand as JSON. Lets an agent generate valid DSL
// without reading source code.
//
// The capabilities are derived from runtime data (d2 shape catalog,
// registered primitives, layout engines) so they cannot drift from
// what the renderer actually accepts.
type Capabilities struct {
	Version    string          `json:"version"`
	Inputs     []InputFormat   `json:"inputs"`
	Layouts    []LayoutCap     `json:"layouts"`
	Shapes     []ShapeCap      `json:"shapes"`
	Primitives []PrimitiveCap  `json:"primitives"`
	StyleKeys  []StyleKeyGroup `json:"style_keys"`
	// Enums are the closed vocabularies the engine accepts verbatim — the
	// values an agent must get EXACTLY right (a typo silently falls back).
	// Surfaced so discovery and validation share one source: every value here
	// is also what Validate() rejects deviations from.
	Enums map[string][]string `json:"enums"`
	// Icons is the complete, agent-facing icon catalog: every name an agent
	// may write in the `icon:` field, with kind ("glyph"/"si"/"brand"),
	// the exact URI to emit, and a semantic one-liner so the agent can pick
	// the right icon without reading a separate Markdown file.
	// Strategy: prefer iso://si/<slug> when the node IS a named brand/product;
	// fall back to iso://glyph/<name> for generic concepts. Unknown names are
	// validation errors — pick from this list, do not invent names.
	Icons []IconCap `json:"icons"`
	// Themes are the built-in named design systems a scene opts into with
	// theme: { use: <name> } or the CLI --theme flag. Each supplies role
	// styles (see Roles), text defaults, sizing rhythm, and a matching canvas.
	Themes []ThemeCap `json:"themes"`
	// Roles are the semantic slots a part can declare (role: <name>) for the
	// active theme to style: hero = the focal element, tray = a category
	// container, chip = a compact member tile.
	Roles []string `json:"roles"`
}

// ThemeCap describes one built-in theme for discovery.
type ThemeCap struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// IconCap is one entry in the agent-facing icon catalog (SVG body excluded —
// too large for the capabilities payload; see docs/agent/ICONS.md for previews).
type IconCap struct {
	Kind        string `json:"kind"`        // "glyph" | "si" | "brand"
	Name        string `json:"name"`        // bare name (e.g. "database", "postgresql")
	URI         string `json:"uri"`         // exact string to write in `icon:` field
	Description string `json:"description"` // semantic one-liner for agent decision-making
}

// InputFormat describes one file extension the CLI accepts.
type InputFormat struct {
	Extension   string `json:"extension"`
	Description string `json:"description"`
	Layout      string `json:"layout"` // "auto" | "manual"
}

// LayoutCap describes one layout engine for .d2 input.
type LayoutCap struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	EdgeStyle   string `json:"edge_style"` // human-readable
}

// ShapeCap describes one shape the iso renderer can lower.
//
// AcceptedAs lists every alias an agent can use (d2 dsl + native iso
// names + snake/hyphen variants). HeightHint is the default extrusion
// multiplier — agents don't usually need to think about it but it's
// surfaced so they can pick when the answer should be "flat" or "tall".
type ShapeCap struct {
	IsoName    string   `json:"iso_name"`
	AcceptedAs []string `json:"accepted_as"`
	HeightHint float64  `json:"height_hint"`
	Notes      string   `json:"notes,omitempty"`
}

// PrimitiveCap describes one DSL composition primitive (group / stack
// / annotation / canvas-grid / ...) — the recipe an agent uses to
// express layered, repeated, or annotated structure.
type PrimitiveCap struct {
	Name    string            `json:"name"`
	Where   string            `json:"where"`  // YAML location
	Syntax  string            `json:"syntax"` // one-liner DSL form
	Purpose string            `json:"purpose"`
	Fields  map[string]string `json:"fields"` // field → semantics
}

// StyleKeyGroup describes one style sub-block (Palette, Stroke, Text,
// Effects) so an agent can fill them out correctly.
type StyleKeyGroup struct {
	Block  string   `json:"block"`
	Fields []string `json:"fields"`
}

// CapabilityReport returns the live capability set for this build.
// Pure function — no IO, no globals.
func CapabilityReport() Capabilities {
	return Capabilities{
		Version: "0.4.5",
		Inputs: []InputFormat{
			{".yaml", "hand-authored iso composite with precise placement", "manual"},
			{".json", "same shape as .yaml but JSON-encoded", "manual"},
			{".d2", "d2 graph source; isotopo runs auto-layout then translates", "auto"},
		},
		Layouts: []LayoutCap{
			{"dagre", "default; fast, natural-bend polyline edges", "polyline"},
			{"elk", "ELK; orthogonal right-angle edges with obstacle avoidance", "orthogonal"},
		},
		Shapes:     buildShapeCaps(),
		Themes:     buildThemeCaps(),
		Roles:      RoleNames,
		Primitives: buildPrimitiveCaps(),
		StyleKeys:  buildStyleKeyGroups(),
		Enums: map[string][]string{
			"connector_anchor":  anchorNames,
			"connector_arrow":   arrowNames,
			"connector_routing": routingNames,
			"layout_mode":       layoutModes,
			"place_relation":    placeRelations,
			"annotation_side":   annotationSides,
		},
		Icons: buildIconCaps(),
	}
}

// buildIconCaps converts the live icon catalog into the lightweight
// agent-facing representation (no SVG body).
func buildIconCaps() []IconCap {
	raw := iso25d.IconCatalog()
	out := make([]IconCap, 0, len(raw))
	for _, info := range raw {
		out = append(out, IconCap{
			Kind:        info.Kind,
			Name:        info.Name,
			URI:         info.URI,
			Description: info.Description,
		})
	}
	return out
}

// buildShapeCaps inverts d2ShapeCatalog (the canonical source of truth
// for which strings get accepted) into one entry per iso shape, with
// every alias collected and sorted.
func buildShapeCaps() []ShapeCap {
	type bucket struct {
		isoName    string
		hMul       float64
		acceptedAs map[string]struct{}
	}
	by := map[string]*bucket{}
	for d2name, prof := range d2ShapeCatalog {
		b, ok := by[prof.iso]
		if !ok {
			b = &bucket{isoName: prof.iso, hMul: prof.heightMul, acceptedAs: map[string]struct{}{}}
			by[prof.iso] = b
		}
		b.acceptedAs[d2name] = struct{}{}
	}
	// Add native iso names that aren't in the d2 catalog.
	for _, iso := range []string{"rectangle", "cylinder", "circle", "cloud", "person", "iso_text", "composite", "group",
		"triprism", "hexprism", "octprism", "boundary",
		"wedge", "custom_path",
		"array1d", "array2d", "array3d", "rack"} {
		if _, ok := by[iso]; !ok {
			by[iso] = &bucket{isoName: iso, hMul: 1.0, acceptedAs: map[string]struct{}{iso: {}}}
		}
	}
	// Deterministic height hint: the value above was taken from whichever alias
	// won the d2ShapeCatalog map-iteration race, so it differed per build and
	// leaked nondeterminism into the published contract. Re-pick it from the
	// alias that equals the iso name (the canonical one) when present, else the
	// lexicographically-first alias — a stable choice independent of map order.
	for _, b := range by {
		best := ""
		for a := range b.acceptedAs {
			if a == b.isoName {
				best = a
				break
			}
			if best == "" || a < best {
				best = a
			}
		}
		if prof, ok := d2ShapeCatalog[best]; ok {
			b.hMul = prof.heightMul
		}
	}
	notes := map[string]string{
		"composite": "container — holds parts: [] of CompositePart entries",
		"group":     "v2 primitive — translucent labeled substrate wrapping nested parts",
		"boundary":  "v3.5 — a group whose substrate is a dashed OUTLINE-ONLY flat region (VPC / subnet / trust zone). Same nesting/layout/autosize as group; style.stroke restyles the dashes",
		"iso_text":  "flat text panel (low extrusion)",
		"cloud":     "free-form rounded outline; no per-face palette overrides",
		"triprism":  "v3.2 — 3-gon prism: alert / one-way fan-out semantics",
		"hexprism":  "v3.2 — 6-gon prism: API gateway / middleware semantics",
		"octprism":  "v3.2 — 8-gon prism: firewall (stop-sign) / decision-gate semantics",
		"wedge":       "v3.10 — sloped prism: back edge raised to full height, front edge at z=0. Semantic: ramp, data ingestion pipeline, traffic escalation.",
		"custom_path": "v3.10 — arbitrary polygon base extruded vertically. Provide path: in geom.params (M/L/Z SVG commands). Semantic: any brand or domain-specific shape.",
		"array1d":       "v3.11 — linear row of N identical cells. params: countX (default 3), gap (default 6). Semantic: pipeline stages, replica group.",
		"array2d":       "v3.11 — N×M grid of cells on the ground plane. params: countX, countY (default 3×3), gap. Semantic: shard matrix, node pool, partition table.",
		"array3d":       "v3.11 — N×M×K volumetric grid. params: countX, countY, countZ (default 3×3×3), gap. Semantic: GPU cluster, tensor, embedding matrix.",
		"rack":          "v3.11 — server rack with slot shelves. params: slots (default 4). Semantic: physical server rack, blade chassis, equipment cabinet.",
	}
	// retiredShapes are removed from the public palette (v0.11): rendered broken
	// or unrecognisable (sphere→box, the capsule/dome/torus revolution bodies), or
	// visually indistinct from rectangle so their semantic was never conveyed
	// (diamond, screen), or redundant (prism overlaps hexprism; cone/pyramid/
	// frustum are geometric with weak architecture meaning). Filtering here drops
	// both the native iso name AND its d2 aliases, so the DSL reports them as an
	// unknown shape. Subtraction over fixing: a smaller, sharper palette.
	retiredShapes := map[string]bool{
		"sphere": true, "capsule": true, "dome": true, "torus": true,
		"diamond": true, "screen": true, "browser-panel": true,
		"prism": true, "cone": true, "pyramid": true, "frustum": true,
	}
	out := make([]ShapeCap, 0, len(by))
	for _, b := range by {
		if retiredShapes[b.isoName] {
			continue
		}
		ids := make([]string, 0, len(b.acceptedAs))
		for k := range b.acceptedAs {
			ids = append(ids, k)
		}
		sort.Strings(ids)
		out = append(out, ShapeCap{
			IsoName:    b.isoName,
			AcceptedAs: ids,
			HeightHint: b.hMul,
			Notes:      notes[b.isoName],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IsoName < out[j].IsoName })
	return out
}

func buildPrimitiveCaps() []PrimitiveCap {
	return []PrimitiveCap{
		{
			Name:    "group",
			Where:   "node.parts[*]",
			Syntax:  "{shape: group, layout: {mode: row|column|grid|ring}, parts: [...], label: \"…\", geom: {h}}",
			Purpose: "Wrap N parts in a translucent labeled iso substrate. Position children with layout (or per-child place) — the substrate then auto-sizes around them, so geom.w/d may be omitted. Child offsets are fine-tune deltas relative to the group.",
			Fields: map[string]string{
				"parts":  "list of CompositePart (other groups OK — supports unlimited nesting)",
				"layout": "auto-arrangement of the children — preferred over per-child coordinates",
				"label":  "rendered on the top face of the substrate (or screen-space if style.text.orient=screen)",
				"geom":   "h = substrate thickness (low, default 6); w/d only when overriding the auto-sized footprint",
			},
		},
		{
			Name:    "stack",
			Where:   "node.parts[*].stack",
			Syntax:  "stack: {count: N, gap: g}",
			Purpose: "Replicate a part N times vertically with a Z-gap. Bottom copy keeps the id; the rest are suffixed \"~k\" so connectors can target a specific layer.",
			Fields: map[string]string{
				"count": "total number of layers (1 is a no-op)",
				"gap":   "world Z step between layers (default = part.H + 4 if omitted)",
			},
		},
		{
			Name:    "canvas-grid",
			Where:   "document.canvas",
			Syntax:  "canvas: {background: \"#FAFBFC\", grid: iso, gridColor: \"#E2E6EE\", gridStep: 36}",
			Purpose: "Paint an iso-aware backdrop under the scene. grid: iso draws a diamond rhombus lattice that aligns with the part projection.",
			Fields: map[string]string{
				"background": "solid background color (also bleed color behind patterns)",
				"grid":       "iso | dots | hatch | solid | none",
				"gridColor":  "pattern stroke / dot color",
				"gridStep":   "tile size in world units (default 40)",
				"padding":    "v3.1 — uniform breathing margin in px around the final composition; use 40-80 for sparse hero shots",
				"projection": "v0.8 — iso (default) | top. top renders a flat top-down plan view (footprints + orthogonal edges, height dropped) for reading the layout/flow",
				"depthSort":  "v5.2 — true / false (default false). Painter's-algorithm depth sort: parts paint back-to-front by world depth (corner sum x+y+z), merged across groups, instead of author order; connector routes layer by their endpoints' depth instead of sitting under every body. Group slabs/platforms still paint at the very back. Turn on when parts from different groups overlap in projection or when author order would paint a back part over a front one. Iso projection only (no effect with projection: top).",
			},
		},
		{
			Name:    "annotation",
			Where:   "document.annotations[*] | node.annotations[*] (v3.0)",
			Syntax:  "{anchor: <part-id>, text: \"…\", side: top|right|bottom|left, distance: 60}",
			Purpose: "Screen-space callout pinned to a composite part. Multi-line text is supported via \\n. Placement is collision-managed: the box never crosses or touches any part projection (6px clearance) and never overlaps other text; side/distance are the FIRST candidate, and the box slides to the nearest free peripheral position when occupied.",
			Fields: map[string]string{
				"anchor":   "id of the part to point at",
				"text":     "multi-line text (split on \\n)",
				"side":     "which direction the box sits relative to the part",
				"distance": "world-unit gap from part silhouette to box (default 60)",
				"bg":       "fill color (default white)",
				"border":   "stroke color (default near-black)",
			},
		},
		{
			Name:    "connector",
			Where:   "node.connectors[*]",
			Syntax:  "{from: <part-id>, to: <part-id>, routing: straight|orthogonal|orthogonal2|bezier, arrow: none|triangle, label: \"…\"}",
			Purpose: "Directed line between two parts, optionally labeled and orthogonal-routed. Coplanarity contract: the two endpoints should share the same base height (wz / tier) — orthogonal connectors hug the ground plane, so same-tier links render as clean 2D L-shapes. Connecting endpoints at DIFFERENT heights forces a vertical drop segment (riser); do that only to express a deliberate cross-tier call (e.g. gateway layer → data layer), never as a side effect of giving peer nodes different wz.",
			Fields: map[string]string{
				"from":          "source part id; \"id.anchor\" picks a specific face-centre (e.g. central.right-mid). Bare ids auto-pick the face FACING the other endpoint. STRONGLY PREFER a target at the SAME height (wz/tier) as this source — same-height pairs route as clean flat 2D lines; pick the coplanar candidate when several would work.",
				"to":            "destination part id (same anchor syntax). Choose an endpoint coplanar with `from` (same wz/tier) whenever possible; only span heights for an intentional cross-tier link, which renders a vertical drop segment.",
				"routing":       "ALWAYS use orthogonal — every segment rides the iso ground axes, flush with the 2.5D grid (collinear endpoints collapse to one on-axis segment). orthogonal2 (opt-in v2) keeps the same iso-axis discipline but searches a multi-segment route that detours around node footprints and avoids crossing already-placed edges — reach for it when a dense scene makes plain orthogonal tunnel a node or cross another line. straight/bezier cut across the grid and are reserved for non-iso freeform sketches. Endpoints should be coplanar (same wz); see Purpose for cross-tier risers.",
				"arrow":         "none = no head; triangle = filled arrowhead at the dst",
				"bend":          "v4.4 — {wx, wy} world offset on the route's interior so a Studio edge-drag shifts the line while both endpoints stay docked",
				"waypoints":     "v4.6 — explicit interior corner list [{wx, wy}, …] (world coords, source→target) the route threads through, so a Studio edge-drag can move ONE segment independently. Supersedes bend + the auto route; endpoints stay docked. Auto-kept iso-axis-aligned.",
				"elbow":         "v3.1 — orthogonal elbow bias: xFirst | yFirst (default: the axis the source face exits along). orthogonal2 auto-routes and ignores elbow.",
				"labelBg":       "pill background (default #FFFFFFEE)",
				"labelColor":    "v3.1 — pill ink (default #1F2433); dim it for dark scenes",
				"labelFontSize": "v3.1 — pill font size (default 11)",
			},
		},
		{
			Name:    "layout",
			Where:   "node.layout | node.parts[*].layout (groups)",
			Syntax:  "layout: {mode: row|column|grid|ring, cols: N, gap: 1, padding: 1, align: start|center|end}",
			Purpose: "Auto-arrange a container's parts along the iso ground axes — the preferred way to position parts. No hand-computed coordinates: row marches along world +x, column along +y, grid wraps row-major after cols, ring puts the FIRST child at the centre and distributes the rest on a circle around it (hub-and-spoke in one rule). A layout group's geom.w/d may be omitted; the substrate auto-sizes around the arranged content.",
			Fields: map[string]string{
				"mode":    "row | column | grid | ring | auto. auto (v4.2) is connector-driven: declare parts + connectors with NO place/coords and the engine layers the graph into a balanced iso flow — per-part style (faces/prisms/effects) is untouched. The other modes arrange a chosen structure.",
				"cols":    "grid only; default ceil(sqrt(n))",
				"gap":     "space between children (ring: hub-to-satellite clearance), in CELLS (1 cell = gridStep, default 40 world units); default 1",
				"padding": "content inset from the container edge, in cells; defaults to gap",
				"align":   "cross-axis alignment within each track (default center)",
			},
		},
		{
			Name:    "place",
			Where:   "node.parts[*].place",
			Syntax:  "place: {rightOf: <sibling-id>, inFrontOf: <sibling-id>, above: <sibling-id>, gap: 1, gapX: 2, gapY: 0, align: start|center|end}",
			Purpose: "Position a part relative to a SIBLING — the preferred way to compose free-standing scenes. One constraint per axis: rightOf/leftOf pins world x, inFrontOf/behind pins world y (front = toward viewer), above pins world z. `above` is for DECORATIVE stacking — plinths, ghost volumes, toppers (the part sits flush ON TOP of the sibling, centred on its footprint). If an elevated part also needs a connector, keep it on the same tier (wz) as what it connects to, or the link will hang in a vertical drop; reserve `above` for parts with no connectors. Unpinned ground axes align to the referenced sibling per align. Chains are solved topologically (a stair = each tile rightOf+inFrontOf its predecessor). offset degrades to a fine-tune delta.",
			Fields: map[string]string{
				"rightOf":   "sibling id — this part sits on its +x side",
				"leftOf":    "sibling id — -x side (mutually exclusive with rightOf)",
				"inFrontOf": "sibling id — +y side, toward the viewer",
				"behind":    "sibling id — -y side (mutually exclusive with inFrontOf)",
				"above":     "sibling id — flush on its top face (z = its top), x/y centred unless also pinned",
				"gap":       "distance from the sibling's footprint, in cells; default 1",
				"gapX":      "overrides gap on the x axis only",
				"gapY":      "overrides gap on the y axis only",
				"align":     "alignment along the unconstrained axis (default center)",
			},
		},
		{
			Name:    "preset",
			Where:   "theme.presets + node.parts[*].preset",
			Syntax:  "theme: {presets: {<name>: <Style>}}  →  part: {preset: <name>}",
			Purpose: "Named design-system style presets — define a Style once in the theme, reference it by name from any part. Merges between the per-shape defaults and the part's own style block, so parts can still override individual fields. The portable replacement for YAML anchors (works in JSON, validates with suggestions). Typical systems: hero / satellite / ghost.",
			Fields: map[string]string{
				"theme.presets": "map of preset name to a full Style block (palette / stroke / text / effects)",
				"preset":        "preset name on a Node or CompositePart; unknown names are validation errors with a nearest-name suggestion",
			},
		},
		{
			Name:    "icon",
			Where:   "node.parts[*].icon",
			Syntax:  "icon: \"iso://glyph/<name>[/light|/RRGGBB]\" | \"iso://si/<slug>[/light|/RRGGBB]\" | \"iso://brand/<name>\"",
			Purpose: "Put an icon on a part's top face instead of text — the preferred look for showcase scenes. REAL brand logos (iso://si/<slug>, ~150 vendored from Simple Icons, CC0): mysql, postgresql, redis, apachekafka, docker, kubernetes, terraform, openai, anthropic, pytorch, grafana, ...; same /light and /RRGGBB variants as glyphs; full list in docs/agent/ICONS.md. Glyphs (generic, stroke style): cloud, database, bolt, chart, globe, shield, lock, gear, cpu, code, layers, rocket, user, mobile, browser, search, bell, queue; default ink, /light = white (dark tops), /RRGGBB = custom. Brand aliases (iso://brand/<name>, resolve to the real logo in its brand color): spark, hadoop, mysql, postgresql, hive, pulsar, kafka, redis, mongo, kubernetes, docker, github, gcp, vite, rolldown, oxc. Unknown iso:// icons are validation errors with nearest-name suggestions. Any other icon value is treated as an external resource: an http(s) URL, a data: URI, OR a LOCAL IMAGE FILE PATH (svg/png/jpg/gif/webp, absolute or relative, ~ and file:// accepted) — local paths are read and inlined as a data URI at render time so the output stays self-contained. Browsable index with previews: docs/agent/ICONS.md.",
			Fields: map[string]string{
				"icon": "iso://glyph/<name>[/variant] | iso://brand/<name> | https://… | data:image/… | ./local.svg (inlined at render)",
			},
		},
	}
}

func buildStyleKeyGroups() []StyleKeyGroup {
	return []StyleKeyGroup{
		{"palette", []string{
			"top", "left", "right",
			"topGradient {from, to, dir}", "leftGradient {from, to, dir}", "rightGradient {from, to, dir}",
		}},
		{"stroke", []string{"color", "width", "dash"}},
		{"faces (v3.3)", []string{
			"map of face name → {fill, strokes}; names: top|left|right (box family), top|side0..sideN-1 (prisms), \"*\" wildcard; outranks palette",
			"fill {kind: solid|linearGradient|radialGradient|pattern, color, stops: [{offset 0..1, color}], angle (linear, degrees), cx/cy (radial 0..1), pattern {kind: hatch|dots, color, spacing, angle, projected}}",
			"projected: true pins the pattern tile to the face's iso plane instead of screen space",
			"strokes: [{color, width, dash, opacity}] — multiple re-traces per face, list order = paint order (put thin highlight lines after wide outlines)",
			"note: a pattern fill REPLACES the base fill (transparent tile background); supported on the box family, prisms, and cylinders (top/left/right) — cloud/person/sphere keep palette only for now",
		}},
		{"text", []string{"family", "size (a MAXIMUM — top-face labels auto-wrap at word boundaries and auto-shrink so they never overflow the face; icons are clamped to the face too)", "weight", "color", "orient", "boxBg", "boxBorder"}},
		{"effects", []string{
			"opacity", "margin", "cornerRadius",
			"dropShadow {dx, dy, blur, color}",
			"backglow {color, radius, opacity}",
			"blur (v3.7) — gaussian stdDev in px over the whole part; fog / ghost / de-emphasized layers",
			"outline {color, width, dash, opacity} (v3.8) — accent ring along the part's full silhouette (selection / emphasis), distinct from per-face stroke; hugs the true outline of every shape",
			"pattern {kind: hatch|dots, color, spacing, angle}",
			"wireframe (bool — line-art: strokes only, no fills; ghost parts are exempt from overlap warnings)",
			"grain {intensity 0..1, scale} (film-grain noise on the faces)",
			"faceSplit (bool — rounded boxes only: split the single wrap-around side band into independent left/right faces so a rounded cube shades per-face like a sharp one; needs cornerRadius>0, no-op otherwise)",
		}},
	}
}

func buildThemeCaps() []ThemeCap {
	names := ThemeNames()
	out := make([]ThemeCap, 0, len(names))
	for _, n := range names {
		out = append(out, ThemeCap{Name: n, Description: ThemeDescription(n)})
	}
	return out
}
