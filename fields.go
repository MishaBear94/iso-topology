package isotopo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/MarkovWangRR/iso-topology/iso25d"
	"github.com/MarkovWangRR/iso-topology/yamledit"
	"gopkg.in/yaml.v3"
)

// ── Detail editor (Studio right-click → Edit details) ────────────────────
//
// The form is SCHEMA-DRIVEN, not "show whatever scalar keys exist": each DSL
// key that matters for visual tuning is declared here ONCE with an English
// label, a one-line description, and an input type. detailSchema is the single
// source of truth — designing the DSL means deciding each key's semantics up
// front. Values are read from the element's current YAML (any depth, dotted
// path) and changes are written back in place (comment-preserving).

// Field is one editable, visually-impactful DSL field — one row of the Studio
// detail form. Fields() returns a slice of these for an element, each with its
// current Value, an effective-value hint (Eff) for inherited/computed fields,
// and an Empty placeholder for genuinely-unset ones.
type Field struct {
	Path    string   `json:"key"`   // dotted YAML path; also the form key
	Label   string   `json:"label"` // English, human-readable
	Desc    string   `json:"desc"`  // one-line semantic description
	Type    string   `json:"type"`  // text | number | color | icon | choice
	Options []string `json:"options,omitempty"`
	Group   string   `json:"group,omitempty"` // section header in the modal
	// Collapsed marks the field's group as collapsed by default in the modal
	// (read from the group's first field). Core groups stay open; advanced /
	// rarely-touched groups (Position, Gradients, Border, Effects) fold away so
	// the form has a clear primary→advanced hierarchy instead of one long dump.
	Collapsed bool   `json:"collapsed,omitempty"`
	Inline    bool   `json:"inline,omitempty"` // compact, laid out side-by-side
	Value     string `json:"value"`            // current value, filled per request
	// Eff is the EFFECTIVE resolved colour (theme→shape→preset cascade,
	// gradients flattened to their start colour) for a color field whose own
	// Value is empty — what the canvas actually shows. The form paints the
	// swatch with it so the picker matches reality, while Value stays empty so
	// an untouched field keeps inheriting. Only set for color-type fields.
	Eff string `json:"eff,omitempty"`
	// Empty is the muted placeholder for a field that has NEITHER an own value
	// NOR a resolved Eff — i.e. genuinely unset. It states what leaving it
	// blank means ("0", "1", "none", "auto"), so no field is ever a mystery
	// blank box.
	Empty string `json:"empty,omitempty"`
	// Sub is a second-level section title WITHIN a tab (group). Fields sharing a
	// Sub render as one titled card, so a compound object (a shadow, one face)
	// reads as a unit instead of loose siblings. Blank = top level of the tab.
	Sub string `json:"sub,omitempty"`
	// ShowWhen gates visibility on another field: "path" shows this field only
	// when that path has a non-empty value; "path=val" requires an exact value.
	// Lets secondary params (gradient direction, shadow offset) and conditional
	// ones (grid columns) appear only when they matter.
	ShowWhen string `json:"showWhen,omitempty"`
	// Control overrides the default widget for the Type: "slider" (numeric
	// 0..max with a unit) or "segmented" (a choice rendered as a button row).
	Control string  `json:"control,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	Min     float64 `json:"min,omitempty"`
	Max     float64 `json:"max,omitempty"`
	Step    float64 `json:"step,omitempty"`
}

// nodeSchema / edgeSchema declare the editable, visually-impactful fields.
// shapeOptions is DERIVED from the capability report (the single source of
// truth for what the renderer accepts), so adding a shape to the engine makes
// it appear in the Studio picker automatically — no hand-maintained list to
// drift. Only `composite` (the scene container, never a node choice) is
// dropped, and iso_text is shown under its friendlier `text` alias.
func shapeOptions() []string {
	var out []string
	for _, s := range CapabilityReport().Shapes {
		switch s.IsoName {
		case "composite":
			// scene container only — not a per-node shape
		case "iso_text":
			out = append(out, "text")
		default:
			out = append(out, s.IsoName)
		}
	}
	return out
}

// shapeClass buckets a shape by how it takes colour, so the detail form only
// offers controls that actually affect that shape:
//
//	faces   — top/left/right palette (box family, prisms, cylinder, group)
//	outline — dashed border only (boundary)
//	text    — label colour/size (iso_text)
//	fill    — a single surface colour (circle, cloud, person)
func shapeClass(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "boundary":
		return "outline"
	case "text", "iso_text", "title":
		return "text"
	case "circle", "cloud", "person", "c4-person", "c4_person":
		return "fill"
	default: // rectangle/box/square, triprism/hexprism/octprism, cylinder, group, …
		return "faces"
	}
}

// grp stamps a Group name and collapsed-default onto a run of fields, so
// nodeSchema reads as an ordered list of labelled sections (primary groups
// open, advanced ones collapsed) rather than a flat field soup.
func grp(group string, collapsed bool, fs ...Field) []Field {
	for i := range fs {
		fs[i].Group = group
		fs[i].Collapsed = collapsed
	}
	return fs
}

// sect stamps a Group + Sub (second-level card title) onto a run of fields. The
// card renders expanded.
func sect(group, sub string, fs ...Field) []Field {
	for i := range fs {
		fs[i].Group = group
		fs[i].Sub = sub
	}
	return fs
}

// sectc is sect for a card that should render collapsed by default (advanced /
// rarely-touched: effects, precise offset, repeat).
func sectc(group, sub string, fs ...Field) []Field {
	fs = sect(group, sub, fs...)
	for i := range fs {
		fs[i].Collapsed = true
	}
	return fs
}

// faceFill builds one face's Fill card: a base colour, an optional gradient
// end, and a direction that only appears once a gradient end is set.
func faceFill(sub, base, gradTo, gradDir string) []Field {
	return sect("Fill", sub,
		Field{Path: base, Label: "Base", Desc: "Base color (also the gradient start)", Type: "color", Inline: true},
		Field{Path: gradTo, Label: "Gradient to", Desc: "Leave blank for a solid fill", Type: "color", Inline: true},
		Field{Path: gradDir, Label: "Direction", Type: "choice", Options: dirOpts, ShowWhen: gradTo, Inline: true},
	)
}

// reusable option sets for choice fields.
var (
	dashOpts   = []string{"", "6 4", "1 5"} // solid | dashed | dotted
	weightOpts = []string{"", "400", "500", "600", "700", "800"}
	dirOpts    = []string{"", "down", "up", "left", "right", "diag"}
	alignOpts  = []string{"", "start", "center", "end"}
)

// emptyHint is the muted placeholder for a field with no own value and no
// resolved effective value — it states the consequence of leaving it blank, so
// the field never reads as "missing". Choice fields are skipped: their blank
// option tile (e.g. "Solid", "(none)") already shows the empty state.
func emptyHint(f Field) string {
	switch f.Path {
	case "offset.wx", "offset.wy", "offset.wz":
		return "0" // no manual nudge
	case "stack.count":
		return "1" // no replication
	case "stack.gap", "place.gap", "layout.gap", "layout.padding", "layout.cols":
		return "auto"
	case "geom.w", "geom.d", "geom.h", "geom.sides":
		return "" // size has shape-defaults not surfaced here; leave blank
	}
	switch f.Type {
	case "color":
		return "none"
	case "number":
		return "none" // an effect amount left at 0 is off
	case "text":
		if strings.HasPrefix(f.Path, "place.") {
			return "none"
		}
		return "" // label / preset / family — optional content, blank is self-evident
	}
	return ""
}

func formLayoutContainer(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "group", "boundary", "composite":
		return true
	}
	return false
}

// nodeSchema is shape-aware AND ordered as a hierarchy: Content → Size →
// Position → Layout → Fill → Gradients → Border → Label → Effects. Primary
// groups (Content/Size/Fill/Label) open; the rest collapse by default. The
// colour/face sections depend on what the shape can render, so users never
// tune a knob that does nothing.
func nodeSchema(shape string) []Field {
	class := shapeClass(shape)
	isCloud := strings.EqualFold(strings.TrimSpace(shape), "cloud")
	var f []Field

	// ===== Tab: Shape & Size =====
	f = append(f, sect("ShapeSize", "",
		Field{Path: "shape", Label: "Shape", Desc: "Geometric form of the node", Type: "choice", Options: shapeOptions()},
		Field{Path: "preset", Label: "Style preset", Desc: "Named style from theme.presets", Type: "text"},
	)...)
	f = append(f, sect("ShapeSize", "Size",
		Field{Path: "geom.w", Label: "Width", Type: "number", Unit: "px", Inline: true},
		Field{Path: "geom.d", Label: "Depth", Type: "number", Unit: "px", Inline: true},
		Field{Path: "geom.h", Label: "Height", Type: "number", Unit: "px", Inline: true},
	)...)

	// ===== Tab: Icon & Text =====
	// Label text lives with its typography in one card; icon is its own card.
	f = append(f, sect("IconText", "Typography",
		Field{Path: "label", Label: "Label text", Desc: "Text rendered on the node", Type: "text"},
		Field{Path: "style.text.color", Label: "Color", Type: "color", Inline: true},
		Field{Path: "style.text.size", Label: "Font size", Type: "number", Unit: "px", Inline: true},
		Field{Path: "style.text.weight", Label: "Weight", Type: "choice", Options: weightOpts, Inline: true},
		Field{Path: "style.text.family", Label: "Font family", Type: "text"},
		Field{Path: "style.text.orient", Label: "Placement", Desc: "On the face, or flat below", Type: "choice", Control: "segmented", Options: []string{"", "iso", "screen"}},
	)...)
	if !isCloud {
		f = append(f, sect("IconText", "Icon",
			Field{Path: "icon", Label: "Icon", Desc: "iso ref, image URL, or a local file", Type: "icon"},
			Field{Path: "@iconColor", Label: "Icon color", Desc: "Tint for iso:// glyph/logo icons", Type: "color", Inline: true},
		)...)
	}

	// ===== Tabs: Fill + Border (shape-class dependent) =====
	switch class {
	case "outline": // boundary
		f = append(f, sect("Border", "",
			Field{Path: "style.stroke.color", Label: "Color", Desc: "Outline color (CSS color)", Type: "color", Inline: true},
			Field{Path: "style.stroke.width", Label: "Width", Type: "number", Unit: "px", Inline: true},
			Field{Path: "style.stroke.dash", Label: "Style", Type: "choice", Options: dashOpts, Inline: true},
		)...)
	case "text":
		// label IS the element; styled in Icon & Text
	case "fill": // circle / cloud / person
		f = append(f, sect("Fill", "",
			Field{Path: "style.palette.top", Label: "Fill color", Desc: "Surface fill (CSS color)", Type: "color"},
		)...)
		f = append(f, sect("Border", "",
			Field{Path: "style.stroke.color", Label: "Color", Type: "color", Inline: true},
			Field{Path: "style.stroke.width", Label: "Width", Type: "number", Unit: "px", Inline: true},
			Field{Path: "style.stroke.dash", Label: "Style", Type: "choice", Options: dashOpts, Inline: true},
		)...)
	default: // faces — one card per visible face, in the Fill tab
		f = append(f, faceFill("Top face", "style.palette.top", "style.palette.topGradient.to", "style.palette.topGradient.dir")...)
		f = append(f, faceFill("Left face", "style.palette.left", "style.palette.leftGradient.to", "style.palette.leftGradient.dir")...)
		f = append(f, faceFill("Right face", "style.palette.right", "style.palette.rightGradient.to", "style.palette.rightGradient.dir")...)
		f = append(f, sect("Fill", "",
			Field{Path: "style.effects.faceSplit", Label: "Split left / right faces", Desc: "Shade the two side faces independently (needs corner radius)", Type: "bool", ShowWhen: "style.effects.cornerRadius"},
		)...)
		f = append(f, sect("Border", "",
			Field{Path: "style.stroke.color", Label: "Color", Type: "color", Inline: true},
			Field{Path: "style.stroke.width", Label: "Width", Type: "number", Unit: "px", Inline: true},
			Field{Path: "style.stroke.dash", Label: "Style", Type: "choice", Options: dashOpts, Inline: true},
			Field{Path: "style.effects.cornerRadius", Label: "Corner radius", Desc: "Rounds the vertical edges", Type: "number", Control: "slider", Unit: "px", Min: 0, Max: 40, Step: 1, Inline: true},
		)...)
	}

	// ===== Tab: Effects (shadow & texture) =====
	if class == "faces" || class == "fill" {
		f = append(f, sect("Effects", "Light & blur",
			Field{Path: "style.effects.opacity", Label: "Opacity", Type: "number", Control: "slider", Min: 0, Max: 1, Step: 0.05, Inline: true},
			Field{Path: "style.effects.blur", Label: "Blur", Desc: "Fog / ghost", Type: "number", Control: "slider", Unit: "px", Min: 0, Max: 20, Step: 1, Inline: true},
		)...)
		f = append(f, sectc("Effects", "Drop shadow",
			Field{Path: "style.effects.dropShadow.color", Label: "Color", Desc: "Drop shadow under the silhouette", Type: "color", Inline: true},
			Field{Path: "style.effects.dropShadow.dx", Label: "X", Type: "number", Unit: "px", ShowWhen: "style.effects.dropShadow.color", Inline: true},
			Field{Path: "style.effects.dropShadow.dy", Label: "Y", Type: "number", Unit: "px", ShowWhen: "style.effects.dropShadow.color", Inline: true},
			Field{Path: "style.effects.dropShadow.blur", Label: "Blur", Type: "number", Unit: "px", ShowWhen: "style.effects.dropShadow.color", Inline: true},
		)...)
		f = append(f, sectc("Effects", "Glow",
			Field{Path: "style.effects.backglow.color", Label: "Color", Desc: "Soft halo behind the part", Type: "color", Inline: true},
			Field{Path: "style.effects.backglow.radius", Label: "Radius", Type: "number", Unit: "px", ShowWhen: "style.effects.backglow.color", Inline: true},
			Field{Path: "style.effects.backglow.opacity", Label: "Opacity", Type: "number", Control: "slider", Min: 0, Max: 1, Step: 0.05, ShowWhen: "style.effects.backglow.color", Inline: true},
		)...)
		f = append(f, sectc("Effects", "Texture",
			Field{Path: "style.effects.pattern.kind", Label: "Texture", Desc: "Top-face texture", Type: "choice", Options: []string{"", "hatch", "dots", "grid"}, Inline: true},
			Field{Path: "style.effects.pattern.color", Label: "Color", Type: "color", ShowWhen: "style.effects.pattern.kind", Inline: true},
			Field{Path: "style.effects.pattern.spacing", Label: "Spacing", Type: "number", Unit: "px", ShowWhen: "style.effects.pattern.kind", Inline: true},
			Field{Path: "style.effects.pattern.angle", Label: "Angle", Desc: "Degrees (hatch)", Type: "number", Unit: "deg", ShowWhen: "style.effects.pattern.kind", Inline: true},
		)...)
	}

	// ===== Tab: Position =====
	f = append(f, sect("Position", "Relative placement",
		Field{Path: "place.rightOf", Label: "Right of", Desc: "Sibling id to sit to the right of", Type: "text", Inline: true},
		Field{Path: "place.leftOf", Label: "Left of", Type: "text", Inline: true},
		Field{Path: "place.above", Label: "Above", Type: "text", Inline: true},
		Field{Path: "place.behind", Label: "Behind", Type: "text", Inline: true},
		Field{Path: "place.inFrontOf", Label: "In front of", Type: "text", Inline: true},
		Field{Path: "place.gap", Label: "Gap", Desc: "Gap in cells", Type: "number", Inline: true},
	)...)
	f = append(f, sectc("Position", "Precise offset",
		Field{Path: "offset.wx", Label: "X", Desc: "World-space fine-tune", Type: "number", Inline: true},
		Field{Path: "offset.wy", Label: "Y", Type: "number", Inline: true},
		Field{Path: "offset.wz", Label: "Z lift", Desc: "Lift off the ground plane", Type: "number", Inline: true},
	)...)
	f = append(f, sectc("Position", "Repeat",
		Field{Path: "stack.count", Label: "Copies", Desc: "Replica layers (1 = none)", Type: "number", Inline: true},
		Field{Path: "stack.gap", Label: "Gap", Desc: "Z-step between replicas", Type: "number", Inline: true},
	)...)

	// ===== Tab: Layout (containers only) =====
	if formLayoutContainer(shape) {
		f = append(f, sect("Layout", "",
			Field{Path: "layout.mode", Label: "Mode", Desc: "Auto-arrange children", Type: "choice", Options: []string{"", "row", "column", "grid", "ring", "auto"}},
			Field{Path: "layout.gap", Label: "Child gap", Desc: "Spacing between children, cells", Type: "number", ShowWhen: "layout.mode", Inline: true},
			Field{Path: "layout.cols", Label: "Columns", Desc: "Grid mode only", Type: "number", ShowWhen: "layout.mode=grid", Inline: true},
			Field{Path: "layout.padding", Label: "Padding", Desc: "Inner margin, cells", Type: "number", ShowWhen: "layout.mode", Inline: true},
			Field{Path: "layout.align", Label: "Align", Desc: "Cross-axis alignment", Type: "choice", Options: alignOpts, ShowWhen: "layout.mode", Inline: true},
		)...)
	}

	// Tab order.
	nodeGroupOrder := map[string]int{
		"ShapeSize": 0, "IconText": 1, "Fill": 2, "Border": 3,
		"Effects": 4, "Position": 5, "Layout": 6,
	}
	sort.SliceStable(f, func(i, j int) bool {
		return nodeGroupOrder[f[i].Group] < nodeGroupOrder[f[j].Group]
	})
	return f
}

func canvasSchema() []Field {
	out := grp("Background", false,
		Field{Path: "background", Label: "Fill color", Desc: "Canvas fill behind the diagram (CSS color)", Type: "color"},
		Field{Path: "grid", Label: "Grid pattern", Desc: "Background texture", Type: "choice", Options: []string{"none", "iso", "dots", "hatch", "solid"}},
		Field{Path: "gridColor", Label: "Grid color", Desc: "Grid/texture line color (CSS color)", Type: "color"},
		Field{Path: "gridStep", Label: "Grid step", Desc: "Grid cell size in world units (blank = default)", Type: "number"},
	)
	out = append(out, grp("Layout", false,
		Field{Path: "padding", Label: "Padding", Desc: "Outer breathing margin around the scene, px", Type: "number"},
	)...)
	return out
}

func edgeSchema() []Field {
	// Two tabs only — an edge has little to configure, so cards (not tabs) carry
	// the second level. Connection = who + how it routes; Style = how it looks.
	out := sect("Connection", "Endpoints",
		Field{Path: "from", Label: "From", Desc: "Source node", Type: "choice", Control: "select", Inline: true},
		Field{Path: "to", Label: "To", Desc: "Target node", Type: "choice", Control: "select", Inline: true},
	)
	out = append(out, sect("Connection", "Routing",
		Field{Path: "routing", Label: "Routing", Desc: "Path style between endpoints", Type: "choice", Options: []string{"orthogonal", "straight", "bezier"}},
		Field{Path: "arrow", Label: "Arrowhead", Desc: "Marker at the target end", Type: "choice", Options: []string{"none", "triangle"}, Inline: true},
		Field{Path: "elbow", Label: "Elbow bias", Desc: "Turn order (right-angle routes only)", Type: "choice", Options: []string{"", "xFirst", "yFirst"}, Inline: true, ShowWhen: "routing=orthogonal"},
	)...)
	out = append(out, sect("Style", "Stroke",
		Field{Path: "stroke.color", Label: "Color", Desc: "Stroke color (CSS color)", Type: "color", Inline: true},
		Field{Path: "stroke.width", Label: "Width", Type: "number", Control: "slider", Unit: "px", Min: 0, Max: 12, Step: 0.5, Inline: true},
		Field{Path: "stroke.dash", Label: "Style", Desc: "Solid, dashed, or dotted", Type: "choice", Options: dashOpts},
	)...)
	out = append(out, sectc("Style", "Gradient",
		Field{Path: "stroke.gradient.to", Label: "Gradient to", Desc: "Optional; fades from the stroke color to this", Type: "color", Inline: true},
	)...)
	out = append(out, sectc("Style", "Text",
		Field{Path: "label", Label: "Text", Desc: "Text rendered mid-route", Type: "text"},
		Field{Path: "labelColor", Label: "Text color", Type: "color", Inline: true},
		Field{Path: "labelBg", Label: "Background", Type: "color", Inline: true},
		Field{Path: "labelFontSize", Label: "Font size", Type: "number", Unit: "px", Inline: true},
		Field{Path: "labelFontFamily", Label: "Font family", Desc: "Source Sans Pro embeds the font and measures the complete label", Type: "text"},
		Field{Path: "labelFontWeight", Label: "Font weight", Type: "choice", Options: []string{"", "400", "600", "700"}, Inline: true},
	)...)
	return out
}

// same detail form. Stateless: a pure function of (format, src, kind, id, ci).
func Fields(format string, src []byte, kind, id string, ci int) ([]Field, error) {
	var root map[string]interface{}
	if err := yaml.Unmarshal(src, &root); err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}
	var subtree map[string]interface{}
	var fields []Field
	switch kind {
	case "node":
		subtree = yamledit.FindNodeMap(root, id)
		shape, _ := subtree["shape"].(string) // nil subtree → "" → face schema; guarded below
		fields = nodeSchema(shape)
	case "edge":
		conns := yamledit.FindConnectors(root)
		if ci >= 0 && ci < len(conns) {
			subtree, _ = conns[ci].(map[string]interface{})
		}
		fields = edgeSchema()
	case "canvas":
		// canvas may be absent — still show the schema so the user can add a
		// background/grid from scratch (the editor creates the block on save).
		subtree, _ = root["canvas"].(map[string]interface{})
		if subtree == nil {
			subtree = map[string]interface{}{}
		}
		fields = canvasSchema()
	default:
		return nil, fmt.Errorf("unknown kind %q (want node|edge|canvas)", kind)
	}
	if subtree == nil {
		return nil, fmt.Errorf("%s %q not found in source", kind, id)
	}
	for i := range fields {
		// "@iconColor" is synthetic — the tint lives in the icon ref suffix
		// (iso://glyph|si/<name>/RRGGBB), not a YAML key. Surface the current
		// suffix so the picker reflects the existing colour.
		if fields[i].Path == "@iconColor" {
			fields[i].Value = iconColorOf(yamledit.ReadPath(subtree, "icon"))
			continue
		}
		fields[i].Value = yamledit.ReadPath(subtree, fields[i].Path)
	}
	// For a node, fill the EFFECTIVE value of any inherited (empty) style.*
	// field by resolving the full cascade — colours, sizes, weights, radii,
	// the lot — so EVERY tab reflects what the canvas paints (shown as an
	// "inherits …" hint) rather than a blank that wrongly reads as "unset".
	if kind == "node" {
		if doc, err := LoadInput(context.Background(), format, src, LayoutDagre); err == nil {
			if eff := ResolvePartStyle(doc, id); eff != nil {
				shape := yamledit.ReadPath(subtree, "shape")
				for i := range fields {
					if fields[i].Value != "" || !strings.HasPrefix(fields[i].Path, "style.") {
						continue
					}
					fields[i].Eff = effValue(eff, fields[i].Path)
					// An unset solid face on a box-family part is NOT blank on the
					// canvas — the renderer paints iso25d.DefaultIsoBox's fill. Surface
					// that so the swatch matches the canvas instead of reading "unset".
					if fields[i].Eff == "" && boxFamilyShape(shape) {
						fields[i].Eff = defaultFaceEff(fields[i].Path)
					}
				}
			}
			// Auto-sized containers (a group with no authored geom.w/d) get their
			// SOLVER-COMPUTED size as the hint, so empty W/D reads as "auto 380",
			// not "missing". Height too, when defaulted.
			if gw, gd, gh, okg := ResolvePartGeom(doc, id); okg {
				for i := range fields {
					if fields[i].Value != "" {
						continue
					}
					switch fields[i].Path {
					case "geom.w":
						fields[i].Eff = fstrv(gw)
					case "geom.d":
						fields[i].Eff = fstrv(gd)
					case "geom.h":
						fields[i].Eff = fstrv(gh)
					}
				}
			}
		}
	}
	// Edges have no style cascade, but the renderer draws every connector with
	// active DEFAULTS when unset — a grey orthogonal line. Surface those so an
	// empty routing/stroke reads as "default orthogonal", not "missing".
	// (Keep in sync with the connector defaults in inject.go.)
	if kind == "edge" {
		edgeDefaults := map[string]string{
			"routing":      "orthogonal",
			"arrow":        "none",
			"stroke.color": "#7A8390",
			"stroke.width": "1.4",
		}
		for i := range fields {
			if fields[i].Value != "" {
				continue
			}
			if d, ok := edgeDefaults[fields[i].Path]; ok {
				fields[i].Eff = d
			}
		}
	}
	// Final pass: any field still with no value AND no effective hint gets a
	// muted "what blank means" placeholder, so nothing is a mystery blank box.
	for i := range fields {
		if fields[i].Value == "" && fields[i].Eff == "" {
			fields[i].Empty = emptyHint(fields[i])
		}
	}
	return fields, nil
}

// fstr renders an optional float (nil → ""); fstrv renders a plain float,
// treating 0 as unset so a default-zero knob doesn't show a noisy "inherits 0".
func fstr(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}
func fstrv(v float64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// fnum formats a float INCLUDING zero — used for the parameters of an effect
// that is already present (e.g. a drop shadow's dx:0), where 0 is a real value
// to show, not "unset". Dropping it would leave a half-filled effect.
func fnum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// effValue maps ANY style.* dotted path to its resolved value in a merged
// Style — colours, numbers, weights, dirs, the lot — flattening a face gradient
// to its start colour for the solid swatch. The form shows this as the
// "inherits …" hint on inherited fields so EVERY tab reflects what the canvas
// paints, not just the colour fields. Returns "" when nothing is set.
// boxFamilyShape reports whether a shape renders as the box / slab family —
// the shapes whose UNSET side faces fall back to iso25d.DefaultIsoBox's fills
// (rectangle and its aliases, plus the group/boundary substrates, which draw
// through the same rounded-box path). Only these get a default-face eff hint;
// cylinders, prisms, clouds, persons etc. resolve their faces differently, so a
// box-default hint would be wrong for them.
func boxFamilyShape(shape string) bool {
	switch shape {
	case "rectangle", "square", "group", "boundary", "callout", "class", "code",
		"document", "hierarchy", "image", "package", "page", "step",
		"sql-table", "sql_table", "sequence-diagram", "sequence_diagram":
		return true
	}
	return false
}

// defaultFaceEff returns the built-in fill the box family paints for an unset
// solid palette face (from iso25d.DefaultIsoBox), so the editor's swatch matches
// what the canvas actually shows instead of a grey "unset".
func defaultFaceEff(path string) string {
	d := iso25d.DefaultIsoBox()
	switch path {
	case "style.palette.top":
		return d.TopFill
	case "style.palette.left":
		return d.LeftFill
	case "style.palette.right":
		return d.RightFill
	}
	return ""
}

func effValue(st *Style, path string) string {
	if st == nil {
		return ""
	}
	switch path {
	// ── palette (solid; gradient flattens to its start colour) ──
	case "style.palette.top":
		if st.Palette != nil {
			if g := st.Palette.TopGradient; g != nil {
				return g.From
			}
			return st.Palette.Top
		}
	case "style.palette.left":
		if st.Palette != nil {
			if g := st.Palette.LeftGradient; g != nil {
				return g.From
			}
			return st.Palette.Left
		}
	case "style.palette.right":
		if st.Palette != nil {
			if g := st.Palette.RightGradient; g != nil {
				return g.From
			}
			return st.Palette.Right
		}
	case "style.palette.topGradient.from":
		if st.Palette != nil && st.Palette.TopGradient != nil {
			return st.Palette.TopGradient.From
		}
	case "style.palette.topGradient.to":
		if st.Palette != nil && st.Palette.TopGradient != nil {
			return st.Palette.TopGradient.To
		}
	case "style.palette.topGradient.dir":
		if st.Palette != nil && st.Palette.TopGradient != nil {
			return st.Palette.TopGradient.Dir
		}
	case "style.palette.leftGradient.from":
		if st.Palette != nil && st.Palette.LeftGradient != nil {
			return st.Palette.LeftGradient.From
		}
	case "style.palette.leftGradient.to":
		if st.Palette != nil && st.Palette.LeftGradient != nil {
			return st.Palette.LeftGradient.To
		}
	case "style.palette.leftGradient.dir":
		if st.Palette != nil && st.Palette.LeftGradient != nil {
			return st.Palette.LeftGradient.Dir
		}
	case "style.palette.rightGradient.from":
		if st.Palette != nil && st.Palette.RightGradient != nil {
			return st.Palette.RightGradient.From
		}
	case "style.palette.rightGradient.to":
		if st.Palette != nil && st.Palette.RightGradient != nil {
			return st.Palette.RightGradient.To
		}
	case "style.palette.rightGradient.dir":
		if st.Palette != nil && st.Palette.RightGradient != nil {
			return st.Palette.RightGradient.Dir
		}
	// ── stroke ──
	case "style.stroke.color":
		if st.Stroke != nil {
			return st.Stroke.Color
		}
	case "style.stroke.width":
		if st.Stroke != nil {
			return fstr(st.Stroke.Width)
		}
	case "style.stroke.dash":
		if st.Stroke != nil {
			return st.Stroke.Dash
		}
	// ── text ──
	case "style.text.color":
		if st.Text != nil {
			return st.Text.Color
		}
	case "style.text.size":
		if st.Text != nil {
			return fstr(st.Text.Size)
		}
	case "style.text.weight":
		if st.Text != nil {
			return st.Text.Weight
		}
	case "style.text.family":
		if st.Text != nil {
			return st.Text.Family
		}
	case "style.text.orient":
		if st.Text != nil && st.Text.Orient != "" {
			return st.Text.Orient
		}
		return "iso" // render default: label sits on the iso top face
	// ── effects ──
	case "style.effects.opacity":
		if st.Effects != nil && st.Effects.Opacity != nil {
			return fstr(st.Effects.Opacity)
		}
		return "1" // render default: fully opaque
	case "style.effects.blur":
		if st.Effects != nil {
			return fstr(st.Effects.Blur)
		}
	case "style.effects.cornerRadius":
		if st.Effects != nil {
			return fstr(st.Effects.CornerRadius)
		}
	case "style.effects.dropShadow.color":
		if st.Effects != nil && st.Effects.DropShadow != nil {
			return st.Effects.DropShadow.Color
		}
	case "style.effects.dropShadow.dx":
		if st.Effects != nil && st.Effects.DropShadow != nil {
			return fnum(st.Effects.DropShadow.Dx)
		}
	case "style.effects.dropShadow.dy":
		if st.Effects != nil && st.Effects.DropShadow != nil {
			return fnum(st.Effects.DropShadow.Dy)
		}
	case "style.effects.dropShadow.blur":
		if st.Effects != nil && st.Effects.DropShadow != nil {
			return fnum(st.Effects.DropShadow.Blur)
		}
	case "style.effects.backglow.color":
		if st.Effects != nil && st.Effects.Backglow != nil {
			return st.Effects.Backglow.Color
		}
	case "style.effects.backglow.radius":
		if st.Effects != nil && st.Effects.Backglow != nil {
			return fnum(st.Effects.Backglow.Radius)
		}
	case "style.effects.backglow.opacity":
		if st.Effects != nil && st.Effects.Backglow != nil {
			return fnum(st.Effects.Backglow.Opacity)
		}
	case "style.effects.pattern.kind":
		if st.Effects != nil && st.Effects.Pattern != nil {
			return st.Effects.Pattern.Kind
		}
	case "style.effects.pattern.color":
		if st.Effects != nil && st.Effects.Pattern != nil {
			return st.Effects.Pattern.Color
		}
	case "style.effects.pattern.spacing":
		if st.Effects != nil && st.Effects.Pattern != nil {
			return fnum(st.Effects.Pattern.Spacing)
		}
	case "style.effects.pattern.angle":
		if st.Effects != nil && st.Effects.Pattern != nil {
			return fnum(st.Effects.Pattern.Angle)
		}
	}
	return ""
}

// spliceIconColor folds a tint into an icon ref: only iso glyph/logo refs
// carry a "/RRGGBB" (or "/light") colour suffix, so it returns "" for URLs,
// data URIs, local paths and brand aliases (nothing to tint). A blank hex
// strips the suffix, reverting to the icon's default ink.
func spliceIconColor(icon, hex string) string {
	icon = strings.TrimSpace(icon)
	if !strings.HasPrefix(icon, "iso://glyph/") && !strings.HasPrefix(icon, "iso://si/") {
		return ""
	}
	base := icon
	if i := strings.LastIndex(base, "/"); i > len("iso://") {
		if suf := base[i+1:]; suf == "light" || isHex6(suf) {
			base = base[:i]
		}
	}
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if hex == "" {
		return base
	}
	return base + "/" + strings.ToUpper(hex)
}

// iconColorOf returns the "#RRGGBB" tint currently encoded in an icon ref,
// or "" if the ref has no hex colour suffix.
func iconColorOf(icon string) string {
	icon = strings.TrimSpace(icon)
	if i := strings.LastIndex(icon, "/"); i > len("iso://") {
		if suf := icon[i+1:]; isHex6(suf) {
			return "#" + strings.ToUpper(suf)
		}
	}
	return ""
}

func isHex6(s string) bool {
	if len(s) != 6 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
