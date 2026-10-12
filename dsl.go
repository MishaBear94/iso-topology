// Package nodedsl is a layered visual DSL for 2.5D iso node rendering.
//
// The shape (discrete enum) is orthogonal to the style (continuous palette
// of visual knobs). Style is structured into four sub-blocks — Palette,
// Stroke, Text, Effects — that mirror how design systems are typically
// organised. A Theme is just a Style; per-node Style is merged on top.
//
// Node             — one element on the canvas
// ├ Shape          — d2 shape type (rectangle | cylinder | hexagon | …)
// ├ Geom           — width / depth / height / sides (polygon only)
// ├ Style          — overrides this node only
// │  ├ Palette     — top / left / right face fills (+ optional gradient)
// │  ├ Stroke      — colour, width, dash, plus per-face overrides
// │  ├ Text        — family, size, weight, colour
// │  └ Effects     — opacity, margin, cornerRadius, dropShadow, per-face opacity
// ├ Label          — string written on the top face (\n splits to multiple lines)
// ├ Icon           — URL / data-URI; ScaleHint / Offset under style.icon
// ├ Content        — structural payload for class / sql_table / sequence_diagram
// └ Direction      — tail/arrow direction for callout / step
package isotopo

// Node is the public DSL root for a single 2.5D shape.
type Node struct {
	Shape     string     `yaml:"shape" json:"shape"`
	Geom      *Geom      `yaml:"geom,omitempty" json:"geom,omitempty"`
	Style     *Style     `yaml:"style,omitempty" json:"style,omitempty"`
	Role      string     `yaml:"role,omitempty" json:"role,omitempty"`     // semantic slot: theme.roles lookup
	Preset    string     `yaml:"preset,omitempty" json:"preset,omitempty"` // v2.5 — theme.presets reference
	Label     string     `yaml:"label,omitempty" json:"label,omitempty"`
	Icon      string     `yaml:"icon,omitempty" json:"icon,omitempty"`
	Content   *Content   `yaml:"content,omitempty" json:"content,omitempty"`
	Direction *Direction `yaml:"direction,omitempty" json:"direction,omitempty"`

	// v1.3 — composite multi-element nodes. When Shape == "composite", Parts
	// is the painter-ordered list of sub-elements stacked at iso-world offsets.
	Parts []*CompositePart `yaml:"parts,omitempty" json:"parts,omitempty"`

	// v1.3 — custom named anchors (in iso-world coords). Eight corners + six
	// face-centres are auto-derived; entries here are additive overrides.
	Anchors map[string]*WorldPoint `yaml:"anchors,omitempty" json:"anchors,omitempty"`

	// v3.0 — node-scoped annotations. Joined with the document-level
	// Annotations list at render time, so callouts can live next to the
	// scene that owns them instead of only at the document top level.
	Annotations []*Annotation `yaml:"annotations,omitempty" json:"annotations,omitempty"`

	// v1.4 — composite-only first-class connectors between parts. Each
	// connector references "partID.anchor" on its From/To. Resolved at
	// render time using each part's iso-projected anchor positions.
	Connectors []*Connector `yaml:"connectors,omitempty" json:"connectors,omitempty"`

	// v1.4 — fossflow-style integer iso grid. When GridStep > 0, any
	// CompositePart with `position: {i, j}` is placed at world
	// (i*GridStep, j*GridStep, 0) before applying any free-form offset.
	GridStep float64 `yaml:"gridStep,omitempty" json:"gridStep,omitempty"`

	// v2.2 — auto-arrangement of this composite's top-level parts. When
	// set, part positions are SOLVED (row / column / grid along the iso
	// ground axes) instead of authored; `offset` degrades to a fine-tune
	// delta on top of the solved position.
	Layout *Layout `yaml:"layout,omitempty" json:"layout,omitempty"`
}

// Connector is a directed line between two anchor points on different
// composite parts. Style follows EdgeDSL conventions (stroke colour /
// width / dash). The From/To strings have the form "partID" or
// "partID.anchor" — e.g. "central.right-mid" or just "central" (which
// resolves to the centre of that part).
type Connector struct {
	// strokeGenerated marks a Stroke fabricated by a translator (the .d2 path
	// copies d2's default edge color) rather than authored; theme application
	// clears it so edges take the engine's neutral default. Never serialised.
	strokeGenerated bool    `yaml:"-" json:"-"`
	From            string  `yaml:"from" json:"from"`
	To              string  `yaml:"to" json:"to"`
	Stroke          *Stroke `yaml:"stroke,omitempty" json:"stroke,omitempty"`
	Arrow           string  `yaml:"arrow,omitempty" json:"arrow,omitempty"` // none | triangle (v1.4)
	Label           string  `yaml:"label,omitempty" json:"label,omitempty"`
	LabelBg         string  `yaml:"labelBg,omitempty" json:"labelBg,omitempty"`
	// v3.1 — pill ink + size so dark scenes can run dim mid-route labels.
	LabelColor    string  `yaml:"labelColor,omitempty" json:"labelColor,omitempty"`
	LabelFontSize float64 `yaml:"labelFontSize,omitempty" json:"labelFontSize,omitempty"`
	// Source Sans Pro binds the entire connector label to an embedded resource.
	LabelFontFamily string `yaml:"labelFontFamily,omitempty" json:"labelFontFamily,omitempty"`
	LabelFontWeight string `yaml:"labelFontWeight,omitempty" json:"labelFontWeight,omitempty"`
	// v3.1 — elbow bias for the orthogonal router: xFirst | yFirst.
	// Default picks the axis the source face exits along.
	Elbow string `yaml:"elbow,omitempty" json:"elbow,omitempty"`
	// v4.4 — bend: a world (wx, wy) offset applied to the route's
	// INTERIOR waypoints, so a Studio edge-drag shifts the line while
	// both endpoints stay docked to their anchors.
	Bend *WorldPoint `yaml:"bend,omitempty" json:"bend,omitempty"`
	// v4.6 — waypoints: explicit interior CORNER points (world wx/wy) the
	// route must thread through, source→target. Lets a Studio edge-drag move
	// ONE segment independently (drawio-style orthogonal editing) instead of
	// shifting the whole line. When set, it supersedes Bend and the auto
	// staircase; endpoints stay docked to their anchors.
	Waypoints []WorldPoint `yaml:"waypoints,omitempty" json:"waypoints,omitempty"`
	// Explicit world-space interior corners, including height. When present,
	// this route supersedes the legacy ground-plane Waypoints and Bend for
	// orthogonal routing. Other routing modes keep it inactive.
	Waypoints3D []WorldPoint `yaml:"waypoints3d,omitempty" json:"waypoints3d,omitempty"`

	// v1.5 — routing strategy:
	//   "straight"    — single segment from anchor to anchor (default)
	//   "orthogonal"  — U-shape bending along iso ground axes (world +x then
	//                    world +y then world +x), reads like a PCB / Manhattan
	//                    route in iso projection
	//   "orthogonal2" — opt-in v2 of the orthogonal router: multi-segment
	//                    obstacle- AND crossing-aware Manhattan route searched
	//                    on a sparse grid (see inject_router2.go). Same stubs,
	//                    risers and anchor semantics as "orthogonal", but it
	//                    detours around nodes instead of tunnelling and dodges
	//                    already-placed edges; fan-out channel width adapts to
	//                    the face span. "orthogonal" output is unchanged.
	Routing string `yaml:"routing,omitempty" json:"routing,omitempty"`
}

// These slices are the discrete vocabularies the renderer/solver understands —
// the single source for validation (reject typos), nearest-neighbor
// suggestions, AND the capability contract's `enums` map, so the set an agent
// discovers can never drift from the set the engine accepts. Empty is allowed
// for arrow/routing (no arrow; default straight route).
var (
	arrowNames      = []string{"none", "triangle"}
	routingNames    = []string{"straight", "orthogonal", "orthogonal2", "bezier"}
	layoutModes     = []string{"row", "column", "grid", "ring", "auto"}
	placeRelations  = []string{"rightOf", "leftOf", "inFrontOf", "behind", "above"}
	annotationSides = []string{"top", "right", "bottom", "left"}
)

// CompositePart is one sub-element of a composite node.
type CompositePart struct {
	ID    string `yaml:"id,omitempty" json:"id,omitempty"` // v1.4 — referenced by Connectors
	Shape string `yaml:"shape" json:"shape"`
	// groupLabel marks the iso_text the lowering pass emits for a group's name
	// (internal; never parsed). The occlusion pass repaints these last so an
	// upper-layer node can't cover a lane label. Unexported → not serialised.
	groupLabel bool `yaml:"-" json:"-"`
	// styleGenerated marks a Style fabricated by a translator (the .d2 path
	// synthesises fill/stroke for every shape) rather than authored. Theme
	// application may displace generated styles so the design system can own
	// the look; authored styles always win. Unexported → never serialised.
	styleGenerated bool `yaml:"-" json:"-"`
	// labelFor is the id of the group this injected label captions. The render
	// pass emits the label's <g> with data-part-id=labelFor (so clicking the
	// caption selects/drags the parent group in Studio) WITHOUT registering it
	// in the interaction model / connector anchor map. Unexported → not parsed.
	labelFor string `yaml:"-" json:"-"`
	Geom     *Geom  `yaml:"geom,omitempty" json:"geom,omitempty"`
	Style    *Style `yaml:"style,omitempty" json:"style,omitempty"`
	Preset   string `yaml:"preset,omitempty" json:"preset,omitempty"` // v2.5 — theme.presets reference
	// Role is the part's SEMANTIC slot in the diagram — hero | tray | chip —
	// which a theme maps to a complete look (theme.roles) and, when the part
	// declares no geom, a sizing rhythm (theme.roleGeoms). Roles let a scene
	// declare WHAT each part is while the theme decides HOW it looks, so the
	// same topology re-skins coherently under any named theme.
	Role    string      `yaml:"role,omitempty" json:"role,omitempty"`
	Label   string      `yaml:"label,omitempty" json:"label,omitempty"`
	Icon    string      `yaml:"icon,omitempty" json:"icon,omitempty"`
	Content *Content    `yaml:"content,omitempty" json:"content,omitempty"`
	Offset  *WorldPoint `yaml:"offset,omitempty" json:"offset,omitempty"`

	// v1.4 — fossflow-style iso grid position. Applied if the parent
	// composite has GridStep > 0. Multiplied by GridStep, then any Offset
	// is added on top (mixing snap + free-form).
	Position *GridPos `yaml:"position,omitempty" json:"position,omitempty"`

	// v2 — replica stack. When Count > 1, the part is expanded into N
	// identical copies stacked vertically with the given Z gap; the
	// bottom copy keeps the declared id, the rest are suffixed
	// "<id>~1", "<id>~2", … so connectors can still reach a specific
	// layer. Models server racks / HA pods / replicas without forcing
	// the author (or an agent) to copy-paste N declarations.
	Stack *Stack `yaml:"stack,omitempty" json:"stack,omitempty"`

	// set by lowering (groupSubstrate) — never authored. Substrate slabs
	// are hoisted to the front of the painter order and the connector
	// layer is spliced in directly above them, so routes show on group
	// plates instead of vanishing underneath.
	isSubstrate bool

	// v2 — nested parts. Only honored when Shape == "group". The group
	// itself renders as a low-extrusion translucent substrate; each
	// nested part is laid down on top with its Offset interpreted
	// relative to the group's own offset.
	Parts []*CompositePart `yaml:"parts,omitempty" json:"parts,omitempty"`

	// v2.2 — declarative position relative to a SIBLING part (same parts
	// list). Solved by the layout pass before lowering; Offset becomes a
	// fine-tune delta added on top of the solved position. Mutually
	// exclusive with being inside a `layout` container.
	Place *Place `yaml:"place,omitempty" json:"place,omitempty"`

	// v2.2 — auto-arrangement of nested parts. Only honored when Shape ==
	// "group". When set, the group's footprint (geom.w/d) may be omitted —
	// it is derived from the arranged content plus padding.
	Layout *Layout `yaml:"layout,omitempty" json:"layout,omitempty"`
}

// Layout arranges a container's parts along the iso ground axes so the
// author never computes coordinates. All lengths are in CELLS — one cell
// = the document's grid step (node.gridStep, else canvas.gridStep, else
// 40 world units) — so arranged parts stay in register with the canvas
// grid texture and with orthogonal connector channels.
type Layout struct {
	// Mode: "row" (children march along world +x), "column" (world +y),
	// or "grid" (row-major wrap after Cols children).
	Mode string `yaml:"mode" json:"mode"`
	// Cols — grid mode only. Defaults to ceil(sqrt(len(parts))).
	Cols int `yaml:"cols,omitempty" json:"cols,omitempty"`
	// Gap between adjacent children, in cells. Default 1.
	Gap *float64 `yaml:"gap,omitempty" json:"gap,omitempty"`
	// Padding between content and the container edge, in cells.
	// Defaults to Gap.
	Padding *float64 `yaml:"padding,omitempty" json:"padding,omitempty"`
	// Align: cross-axis alignment of children within their row/column
	// track — "start" | "center" | "end". Default "center".
	Align string `yaml:"align,omitempty" json:"align,omitempty"`
	// Engine — scene-root mode "auto" only (v5): which layered engine
	// solves the connector graph. "" (default) keeps the built-in
	// longest-path solver, byte-identical to before; "elk" delegates to
	// the Eclipse Layout Kernel and "dagre" to dagre (both embedded in
	// the d2 dependency — pure offline JS, deterministic). Prefer "elk"
	// for dense DAGs with heavy fan-in/fan-out where the built-in
	// solver leaves crossings/tunnels. Engine coordinates are snapped
	// to the cell lattice; on engine failure the built-in solver runs
	// instead and a Warning is reported.
	Engine string `yaml:"engine,omitempty" json:"engine,omitempty"`
}

// Place positions a part relative to a sibling's footprint. Give one
// constraint per axis: RightOf/LeftOf pin world x, InFrontOf/Behind
// pin world y ("front" = toward the viewer = +y, "behind" = away =
// -y), Above pins world z (the part sits ON TOP of the sibling —
// z = sibling.z + sibling.h). With an axis unconstrained, it aligns
// to the referenced sibling per Align. Gaps are in cells (default 1);
// GapX/GapY override Gap per ground axis. Above is flush (no gap).
type Place struct {
	RightOf   string   `yaml:"rightOf,omitempty" json:"rightOf,omitempty"`
	LeftOf    string   `yaml:"leftOf,omitempty" json:"leftOf,omitempty"`
	InFrontOf string   `yaml:"inFrontOf,omitempty" json:"inFrontOf,omitempty"`
	Behind    string   `yaml:"behind,omitempty" json:"behind,omitempty"`
	Above     string   `yaml:"above,omitempty" json:"above,omitempty"`
	Gap       *float64 `yaml:"gap,omitempty" json:"gap,omitempty"`
	GapX      *float64 `yaml:"gapX,omitempty" json:"gapX,omitempty"`
	GapY      *float64 `yaml:"gapY,omitempty" json:"gapY,omitempty"`
	// Align: alignment along the unconstrained axis relative to the
	// (single) referenced sibling — "start" | "center" | "end".
	// Default "center".
	Align string `yaml:"align,omitempty" json:"align,omitempty"`
}

// Stack tells the renderer to replicate a composite part vertically.
// Count = total number of layers (1 is a no-op). Gap = world Z step
// between layers. If Gap <= 0 we use the part's own H + a small margin
// so layers visually sit ON TOP of each other rather than overlap.
type Stack struct {
	Count int     `yaml:"count" json:"count"`
	Gap   float64 `yaml:"gap,omitempty" json:"gap,omitempty"`
}

// GridPos is an integer iso-grid coordinate.
type GridPos struct {
	I int `yaml:"i" json:"i"`
	J int `yaml:"j" json:"j"`
}

// WorldPoint is a point in iso world coords (wx, wy, wz). Used for both
// composite offsets and node anchors.
type WorldPoint struct {
	WX float64 `yaml:"wx,omitempty" json:"wx,omitempty"`
	WY float64 `yaml:"wy,omitempty" json:"wy,omitempty"`
	WZ float64 `yaml:"wz,omitempty" json:"wz,omitempty"`
}

// Geom is the local-frame box dimensions used by the iso projection. Sides
// is consumed only by `shape: polygon` — number of sides for the polygonal
// prism (3 = triangular, 5 = pentagon, 8 = octagon, etc.).
type Geom struct {
	W        float64        `yaml:"w,omitempty" json:"w,omitempty"`
	D        float64        `yaml:"d,omitempty" json:"d,omitempty"`
	H        float64        `yaml:"h,omitempty" json:"h,omitempty"`
	Sides    int            `yaml:"sides,omitempty" json:"sides,omitempty"`
	TopScale float64        `yaml:"topScale,omitempty" json:"topScale,omitempty"`
	Params   map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
}

// Style groups every visual knob. Each sub-block is a pointer so the merge
// step can distinguish "explicitly empty" from "inherit from parent".
type Style struct {
	Palette *Palette `yaml:"palette,omitempty" json:"palette,omitempty"`
	Stroke  *Stroke  `yaml:"stroke,omitempty" json:"stroke,omitempty"`
	Text    *Text    `yaml:"text,omitempty" json:"text,omitempty"`
	Effects *Effects `yaml:"effects,omitempty" json:"effects,omitempty"`
	// v3.3 — per-face surface overrides; outranks Palette.
	Faces map[string]*FaceStyle `yaml:"faces,omitempty" json:"faces,omitempty"`
}

// Palette assigns the three iso-face fills. Each face may be either a
// solid colour (Top / Left / Right) or a linear gradient (TopGradient
// etc.); when a gradient is set it OVERRIDES the matching solid fill.
type Palette struct {
	Top   string `yaml:"top,omitempty" json:"top,omitempty"`
	Left  string `yaml:"left,omitempty" json:"left,omitempty"`
	Right string `yaml:"right,omitempty" json:"right,omitempty"`

	// v2.1 — per-face linear gradients (face goes from From → To along Dir).
	TopGradient   *FaceGradient `yaml:"topGradient,omitempty" json:"topGradient,omitempty"`
	LeftGradient  *FaceGradient `yaml:"leftGradient,omitempty" json:"leftGradient,omitempty"`
	RightGradient *FaceGradient `yaml:"rightGradient,omitempty" json:"rightGradient,omitempty"`
}

// FaceGradient is a linear gradient applied to one iso face.
//
//	from: start colour (any CSS colour, e.g. "#FFE188" or "rgba(...)")
//	to:   end colour
//	dir:  "down" (default) | "up" | "left" | "right" | "diag"
type FaceGradient struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
	Dir  string `yaml:"dir,omitempty" json:"dir,omitempty"`
}

// Stroke describes the wireframe. Dash uses SVG stroke-dasharray syntax.
type Stroke struct {
	Color string   `yaml:"color,omitempty" json:"color,omitempty"`
	Width *float64 `yaml:"width,omitempty" json:"width,omitempty"`
	Dash  string   `yaml:"dash,omitempty" json:"dash,omitempty"`
	// Gradient, when set on a connector, fills the line with a linear gradient
	// from the SOURCE end to the TARGET end. Direction follows the route, so
	// FaceGradient.Dir is ignored here; only From/To colours are used.
	Gradient *FaceGradient `yaml:"gradient,omitempty" json:"gradient,omitempty"`
}

// Text controls the label rendered on the top face. Orient picks how
// the label is composed:
//
//	"" / "iso"   — default. Label sits inside the top face, tilted onto
//	                the iso plane via a matrix transform.
//	"screen"     — label is rendered as a horizontal box BELOW the part's
//	                projected bounding box. Background and border are read
//	                from BoxBg / BoxBorder; otherwise transparent.
//	                Composite-only: takes effect on CompositePart entries.
type Text struct {
	Family    string   `yaml:"family,omitempty" json:"family,omitempty"`
	Size      *float64 `yaml:"size,omitempty" json:"size,omitempty"`
	Weight    string   `yaml:"weight,omitempty" json:"weight,omitempty"`
	Color     string   `yaml:"color,omitempty" json:"color,omitempty"`
	Orient    string   `yaml:"orient,omitempty" json:"orient,omitempty"`
	BoxBg     string   `yaml:"boxBg,omitempty" json:"boxBg,omitempty"`
	BoxBorder string   `yaml:"boxBorder,omitempty" json:"boxBorder,omitempty"`
}

// EffectItem is one entry in an ordered effects pipeline (style.effects list).
// Kind is one of: backglow | blur | grain | outline | dropShadow.
// Parameters mirror the flat Effect sub-structs but are unified here so a
// single list can hold any mix of effects in author-controlled order.
type EffectItem struct {
	Kind string `yaml:"kind" json:"kind"`

	// backglow
	Color   string  `yaml:"color,omitempty" json:"color,omitempty"`
	Radius  float64 `yaml:"radius,omitempty" json:"radius,omitempty"`
	Opacity float64 `yaml:"opacity,omitempty" json:"opacity,omitempty"`

	// blur
	StdDev float64 `yaml:"stdDev,omitempty" json:"stdDev,omitempty"`

	// grain
	Intensity float64 `yaml:"intensity,omitempty" json:"intensity,omitempty"`
	Scale     float64 `yaml:"scale,omitempty" json:"scale,omitempty"`

	// outline
	Width float64 `yaml:"width,omitempty" json:"width,omitempty"`
	Dash  string  `yaml:"dash,omitempty" json:"dash,omitempty"`

	// dropShadow
	Dx   float64 `yaml:"dx,omitempty" json:"dx,omitempty"`
	Dy   float64 `yaml:"dy,omitempty" json:"dy,omitempty"`
	Blur float64 `yaml:"blur,omitempty" json:"blur,omitempty"`
}

// Effects holds compositional knobs that aren't tied to a single face.
// v2.1 — DropShadow / Backglow / Pattern lift the existing iso25d-internal
// features into the DSL so authors and agents can opt into them.
type Effects struct {
	Opacity      *float64 `yaml:"opacity,omitempty" json:"opacity,omitempty"`
	Margin       *float64 `yaml:"margin,omitempty" json:"margin,omitempty"`
	CornerRadius *float64 `yaml:"cornerRadius,omitempty" json:"cornerRadius,omitempty"`

	// v2.1 — soft drop shadow under the projected silhouette.
	DropShadow *DropShadow `yaml:"dropShadow,omitempty" json:"dropShadow,omitempty"`
	// v2.1 — diffuse halo behind the part (use for "glowing" focal nodes).
	Backglow *Backglow `yaml:"backglow,omitempty" json:"backglow,omitempty"`
	// v3.7 (M4) — gaussian blur over the whole part: fog/ghost nodes,
	// de-emphasized背景层. Value = stdDeviation in px.
	Blur *float64 `yaml:"blur,omitempty" json:"blur,omitempty"`
	// v2.1 — repeating surface texture overlaid on the top face
	// (hatch lines, dot grid, square grid).
	Pattern *Pattern `yaml:"pattern,omitempty" json:"pattern,omitempty"`

	// v2.6 — line-art mode: faces render unfilled, strokes only.
	Wireframe *bool `yaml:"wireframe,omitempty" json:"wireframe,omitempty"`
	// faceSplit (rounded boxes only): split the single continuous side band at
	// the front vertical edge into independent left (LeftFill) and right
	// (RightFill) faces, so a rounded cube shades per-face like a sharp one —
	// real per-face lighting. Default off keeps the classic wrap-around side
	// gradient (no change to existing diagrams).
	FaceSplit *bool `yaml:"faceSplit,omitempty" json:"faceSplit,omitempty"`
	// v2.6 — film-grain noise overlay on the faces.
	Grain *Grain `yaml:"grain,omitempty" json:"grain,omitempty"`
	// v3.8 (M4) — accent stroke traced along the part's full silhouette,
	// distinct from per-face style.stroke: a selection / emphasis ring
	// that hugs the outer outline. Painted on top of every face.
	Outline *Outline `yaml:"outline,omitempty" json:"outline,omitempty"`

	// v3.9 — ordered effect pipeline. When List is non-empty it OVERRIDES
	// the individual scalar fields above: the renderer applies effects in
	// list order, allowing backglow+grain+outline to stack in author-
	// controlled order. The same effect kind may appear multiple times.
	List []*EffectItem `yaml:"effects,omitempty" json:"effects,omitempty"`
}

// Outline is a silhouette-following accent stroke (emphasis / selection
// ring). Unlike per-face strokes it traces the whole projected outline.
type Outline struct {
	Color   string  `yaml:"color,omitempty" json:"color,omitempty"`
	Width   float64 `yaml:"width,omitempty" json:"width,omitempty"`
	Dash    string  `yaml:"dash,omitempty" json:"dash,omitempty"`
	Opacity float64 `yaml:"opacity,omitempty" json:"opacity,omitempty"`
}

// Grain overlays monochrome film-grain noise on the part's faces —
// the "risograph / print" texture. Intensity 0..1 (how visible),
// Scale = noise frequency (default 0.8; smaller = coarser speckle).
type Grain struct {
	Intensity float64 `yaml:"intensity,omitempty" json:"intensity,omitempty"`
	Scale     float64 `yaml:"scale,omitempty" json:"scale,omitempty"`
}

// DropShadow describes a Gaussian-blurred offset shadow under the part.
//
//	dx, dy: offset in screen units (positive y = down)
//	blur:   Gaussian stdDeviation (0 = crisp)
//	color:  any CSS colour; rgba("...") gets you semi-transparent shadows
type DropShadow struct {
	Dx    float64 `yaml:"dx,omitempty" json:"dx,omitempty"`
	Dy    float64 `yaml:"dy,omitempty" json:"dy,omitempty"`
	Blur  float64 `yaml:"blur,omitempty" json:"blur,omitempty"`
	Color string  `yaml:"color,omitempty" json:"color,omitempty"`
}

// v3.3 (M3) — per-face surface overrides: style.faces maps a face name
// ("top" / "left" / "right", prism "side0".."sideN", or "*" wildcard)
// to a fill + stroke-layer description that outranks style.palette.
type FaceStyle struct {
	Fill    *FillSpec     `yaml:"fill,omitempty" json:"fill,omitempty"`
	Strokes []StrokeLayer `yaml:"strokes,omitempty" json:"strokes,omitempty"`
}

type FillSpec struct {
	Kind  string      `yaml:"kind,omitempty" json:"kind,omitempty"` // solid | linearGradient | radialGradient | pattern
	Color string      `yaml:"color,omitempty" json:"color,omitempty"`
	Stops []ColorStop `yaml:"stops,omitempty" json:"stops,omitempty"`
	Angle float64     `yaml:"angle,omitempty" json:"angle,omitempty"` // linear: degrees, 0 = left→right
	Cx    *float64    `yaml:"cx,omitempty" json:"cx,omitempty"`       // radial: centre 0..1
	Cy    *float64    `yaml:"cy,omitempty" json:"cy,omitempty"`
	// v3.4 — pin the gradient axis to the face's iso plane (like
	// pattern projected) instead of screen space.
	Projected bool         `yaml:"projected,omitempty" json:"projected,omitempty"`
	Pattern   *FacePattern `yaml:"pattern,omitempty" json:"pattern,omitempty"`
}

type ColorStop struct {
	Offset float64 `yaml:"offset" json:"offset"`
	Color  string  `yaml:"color" json:"color"`
}

type FacePattern struct {
	Kind      string  `yaml:"kind,omitempty" json:"kind,omitempty"` // hatch | dots
	Color     string  `yaml:"color,omitempty" json:"color,omitempty"`
	Spacing   float64 `yaml:"spacing,omitempty" json:"spacing,omitempty"`
	Angle     float64 `yaml:"angle,omitempty" json:"angle,omitempty"`
	Projected bool    `yaml:"projected,omitempty" json:"projected,omitempty"` // tile follows the face's iso transform
}

type StrokeLayer struct {
	Color   string  `yaml:"color,omitempty" json:"color,omitempty"`
	Width   float64 `yaml:"width,omitempty" json:"width,omitempty"`
	Dash    string  `yaml:"dash,omitempty" json:"dash,omitempty"`
	Opacity float64 `yaml:"opacity,omitempty" json:"opacity,omitempty"`
}

// Backglow is the soft radial halo painted BEHIND the part's silhouette.
//
//	radius:  blur radius in screen units (typical: 40–120)
//	opacity: 0..1, defaults to 0.6 when 0
type Backglow struct {
	Color   string  `yaml:"color,omitempty" json:"color,omitempty"`
	Radius  float64 `yaml:"radius,omitempty" json:"radius,omitempty"`
	Opacity float64 `yaml:"opacity,omitempty" json:"opacity,omitempty"`
}

// Pattern is a repeating texture overlay on the top face. Use to suggest
// "data surface" or "mesh" without drawing per-pixel content.
//
//	kind:    "hatch" | "dots" | "grid"
//	spacing: distance between repeats in local units (default ~10)
//	angle:   degrees, only relevant for hatch (default 0)
type Pattern struct {
	Kind    string  `yaml:"kind,omitempty" json:"kind,omitempty"`
	Color   string  `yaml:"color,omitempty" json:"color,omitempty"`
	Spacing float64 `yaml:"spacing,omitempty" json:"spacing,omitempty"`
	Angle   float64 `yaml:"angle,omitempty" json:"angle,omitempty"`
}

// Content is the structural payload for shapes whose visual is composed of
// rows or cells. Header lands on the top face; Rows are drawn on the
// front-right face with one row per band. Cells (v1.3) is a 2D grid drawn
// on the TOP face (iso-tilted via the top matrix).
type Content struct {
	Header     string     `yaml:"header,omitempty" json:"header,omitempty"`
	Rows       []string   `yaml:"rows,omitempty" json:"rows,omitempty"`
	RowColors  []string   `yaml:"rowColors,omitempty" json:"rowColors,omitempty"`
	Cells      [][]string `yaml:"cells,omitempty" json:"cells,omitempty"` // [row][col]
	CellColors [][]string `yaml:"cellColors,omitempty" json:"cellColors,omitempty"`
}

// Direction is the tail/arrow orientation for callout / step.
//
//	callout.tail ∈ {"down", "up", "left", "right"}    (default: "down")
//	step.arrow   ∈ {"right", "left"}                  (default: "right")
type Direction struct {
	Tail  string   `yaml:"tail,omitempty" json:"tail,omitempty"`
	Arrow string   `yaml:"arrow,omitempty" json:"arrow,omitempty"`
	Size  *float64 `yaml:"size,omitempty" json:"size,omitempty"`
}

// Theme is the document-level default Style plus optional per-shape-type
// defaults. Resolution order for any node:
//
//	theme (base)  →  theme.shapes[node.Shape]  →  node.Style
//
// Each step is a field-level merge: later non-empty fields override
// earlier ones; nil sub-blocks inherit.
type Theme struct {
	Style  `yaml:",inline" json:",inline"`
	Shapes map[string]*Style `yaml:"shapes,omitempty" json:"shapes,omitempty"`

	// v2.5 — named style presets. A part opts in with `preset: <name>`
	// and gets the preset merged between the per-shape defaults and its
	// own style block. Presets are the portable replacement for YAML
	// anchors: they survive JSON encoding, they're visible to agents
	// through the schema, and one theme can ship a whole design system
	// (hero / satellite / ghost / …) that parts reference by name.
	Presets map[string]*Style `yaml:"presets,omitempty" json:"presets,omitempty"`

	// Use names a built-in theme (see ThemeNames) to layer UNDER this theme:
	// the named theme supplies the design system (roles, presets, text,
	// canvas when the doc declares none) and any fields set here override it.
	// Resolved once at parse time; unknown names surface through Validate.
	Use string `yaml:"use,omitempty" json:"use,omitempty"`
	// Roles maps a part's semantic role (hero | tray | chip) to a complete
	// look. Resolved between the per-shape defaults and the named preset, so
	// `role:` gives the design-system look while `preset:`/`style:` still
	// override per part.
	Roles map[string]*Style `yaml:"roles,omitempty" json:"roles,omitempty"`
	// RoleGeoms supplies a default footprint per role, applied only when the
	// part declares no geom of its own — the theme's sizing rhythm (heroes
	// large, chips small) without touching any authored geometry.
	RoleGeoms map[string]*Geom `yaml:"roleGeoms,omitempty" json:"roleGeoms,omitempty"`
}

// Canvas is composition-level metadata for a Document. v2 extended:
// `grid` selects an iso-aware backdrop ("iso" = diamond rhombus lattice,
// "dots" = vertex dots, "hatch" = single-axis stripes, "" = none) so
// authors can pick the right "ground" without computing pattern math.
type Canvas struct {
	Background string  `yaml:"background,omitempty" json:"background,omitempty"`
	Grid       string  `yaml:"grid,omitempty" json:"grid,omitempty"`
	GridColor  string  `yaml:"gridColor,omitempty" json:"gridColor,omitempty"`
	GridStep   float64 `yaml:"gridStep,omitempty" json:"gridStep,omitempty"`
	// v3.1 — uniform breathing margin (px) added around the final
	// composition. Sparse hero shots need air the content bbox can't
	// describe; default 0 keeps the tight crop.
	Padding float64 `yaml:"padding,omitempty" json:"padding,omitempty"`
	// v0.8 — projection mode. "iso" (default, empty) renders the 2.5D
	// isometric view; "top" renders a flat top-down plan (footprints +
	// orthogonal connectors, height dropped) via planview.go.
	Projection string `yaml:"projection,omitempty" json:"projection,omitempty"`
	// AspectRatio constrains the output SVG to a minimum width-to-height
	// ratio by expanding the shorter axis symmetrically with neutral
	// background. 0 (default) applies the built-in default of 16/10 to
	// match Mac internal displays. Set to -1 to disable (tight crop).
	// Example: 1.6 = 16:10, 1.7778 = 16:9, 1.5 = 3:2.
	AspectRatio float64 `yaml:"aspectRatio,omitempty" json:"aspectRatio,omitempty"`
	// DepthSort (v5.2) enables true painter's-algorithm ordering: lowered
	// parts are stably sorted back-to-front by projected world depth
	// (corner sum x+y+z along the iso view axis), merged ACROSS groups,
	// so paint order no longer follows author/source order. Connector
	// routes are layered by their endpoints' depth instead of being fixed
	// under every body. The substrate/platform partition is preserved
	// (group slabs always paint at the very back). Default false keeps
	// the legacy author-order painting byte-for-byte.
	DepthSort bool `yaml:"depthSort,omitempty" json:"depthSort,omitempty"`
}

// Annotation is a screen-space callout pinned to a composite part. It
// renders as a rounded text box (multi-line if Text contains '\n') with
// a thin leader line back to the anchor part's projected silhouette.
//
// Anchor is the id of a CompositePart inside the document's "scene"
// node. Side picks which direction the box sits relative to the
// anchor's projected centre: "top" | "right" | "bottom" | "left"
// (default "right"). Distance is the world-unit gap from the part
// silhouette to the box (default 60).
type Annotation struct {
	Anchor   string  `yaml:"anchor" json:"anchor"`
	Text     string  `yaml:"text" json:"text"`
	Side     string  `yaml:"side,omitempty" json:"side,omitempty"`
	Distance float64 `yaml:"distance,omitempty" json:"distance,omitempty"`
	Bg       string  `yaml:"bg,omitempty" json:"bg,omitempty"`
	Border   string  `yaml:"border,omitempty" json:"border,omitempty"`
	Color    string  `yaml:"color,omitempty" json:"color,omitempty"`
	FontSize float64 `yaml:"fontSize,omitempty" json:"fontSize,omitempty"`
}

// Document bundles a Canvas + Theme + Nodes. This is the canonical
// top-level shape of a NodeDSL .yaml or .json file.
type Document struct {
	Canvas      *Canvas          `yaml:"canvas,omitempty" json:"canvas,omitempty"`
	Theme       *Theme           `yaml:"theme,omitempty" json:"theme,omitempty"`
	Nodes       map[string]*Node `yaml:"nodes" json:"nodes"`
	Annotations []*Annotation    `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// Scene returns the document-level composite node that should pick up
// Canvas + Annotations layers when rendered. The canonical convention
// is a node named "scene" inside Nodes; if there is exactly one node
// and no "scene" key, that node is treated as the scene. Returns nil
// if the document is empty.
//
// Use this in preference to `doc.Nodes["scene"]` directly — the helper
// stays correct even for single-node documents that don't bother
// naming the entry "scene".
func (d *Document) Scene() *Node {
	if d == nil || len(d.Nodes) == 0 {
		return nil
	}
	if n := d.Nodes["scene"]; n != nil {
		return n
	}
	if len(d.Nodes) == 1 {
		for _, n := range d.Nodes {
			return n
		}
	}
	return nil
}

// isContainerShape reports whether a part shape wraps nested parts.
// "boundary" (v3.5, family F) is a group whose substrate renders as a
// dashed outline-only region — the VPC/subnet/trust-zone semantic —
// instead of a solid slab.
func isContainerShape(s string) bool { return s == "group" || s == "boundary" }
