package isotopo

import (
	"fmt"
	"strconv"
)

// SceneResult is a single render's submitted SVG, connector geometry and
// screen-label geometry. It does not yet replace the separate body, top-face
// budget or isometric world interaction resolvers. Empty ScreenLabels is authoritative
// for this layer; the plan renderer has no isometric screen-label layer.
type SceneResult struct {
	SVG               string                 `json:"svg"`
	Routes            []ConnectorRoute       `json:"routes"`
	ScreenLabels      []ScreenLabel          `json:"screen_labels"`
	ConnectorLabels   []ConnectorLabelRecord `json:"connector_labels,omitempty"`
	PlanLabels        []PlanLabel            `json:"plan_labels,omitempty"`
	IsoRouteCrowding  *IsoRouteCrowding      `json:"iso_route_crowding,omitempty"`
	PlanRouteCrowding *PlanRouteCrowding     `json:"plan_route_crowding,omitempty"` // detailed reports only
	Model             []PartModel            `json:"model,omitempty"`               // requested by RenderSceneWithInteraction
}

// ScreenLabelRect uses final SVG user coordinates, before any browser viewBox
// scale, CSS transform or live drag. ScreenLabel.Bounds is the emitted pointer
// hit rect; line InkBounds is measured glyph geometry, not a visibility mask
// or the chip border's stroke bounds.
type ScreenLabelRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ScreenLabel identifies one emitted label in layer order. PartIndex is the
// lowered geometry index, not an authored source array index. PartID retains
// the emitted instance identity; anonymous parts do not invent source IDs.
type ScreenLabel struct {
	PartID      string            `json:"part_id,omitempty"`
	PartIndex   int               `json:"part_index"`
	Bounds      ScreenLabelRect   `json:"bounds"`
	FontFamily  string            `json:"font_family"`
	FontWeight  string            `json:"font_weight"`
	FontSize    float64           `json:"font_size"`
	TextMetrics string            `json:"text_metrics"`
	FontHash    string            `json:"font_hash,omitempty"`
	Lines       []ScreenLabelLine `json:"lines"`
}

// Text is the emitted text node's content. In an estimated legacy label the
// browser still applies normal SVG whitespace collapsing. InkBounds/Advance
// are available only for a fully bound block; a known blank line has no ink.
type ScreenLabelLine struct {
	Text      string           `json:"text"`
	X         float64          `json:"x"`
	Baseline  float64          `json:"baseline"`
	Advance   *float64         `json:"advance,omitempty"`
	InkBounds *ScreenLabelRect `json:"ink_bounds,omitempty"`
}

// RenderScene collects screen labels at their producer, after obstacle
// placement and at the SVG emitter's precision. Collection never changes SVG
// bytes and does not parse geometry back out of the SVG or rerun placement.
func RenderScene(doc *Document) SceneResult {
	return renderSceneObserved(doc, false)
}

// RenderSceneWithInteraction returns the preview and its world interaction
// model together. Plan geometry and final title placement are resolved once
// and shared; iso interaction retains its established body/content resolver.
// Each call owns its result, including the nested selection and port slices.
func RenderSceneWithInteraction(doc *Document) SceneResult {
	return renderSceneResolved(doc, false, true)
}

// Detailed label conflicts can be quadratic in a dense scene. Reports publish
// them; previews omit them. Internal candidate gates can still require those
// observations to certify a route, and retain their accepted content layout.
func renderSceneObserved(doc *Document, labelConflicts bool) SceneResult {
	return renderSceneResolved(doc, labelConflicts, false)
}

func renderSceneResolved(doc *Document, labelConflicts, interaction bool) SceneResult {
	out := SceneResult{ScreenLabels: make([]ScreenLabel, 0)}
	if doc == nil || doc.Scene() == nil {
		return out
	}
	n := doc.Scene()
	if doc.Canvas != nil && doc.Canvas.Projection == "top" {
		plan := resolvePlanScene(n, doc.Theme, doc.Canvas, true, labelConflicts)
		out.SVG = renderResolvedPlan(plan, doc.Canvas, nil, &out.ConnectorLabels, &out.PlanLabels)
		if labelConflicts {
			out.PlanRouteCrowding = plan.crowding
		}
		if interaction {
			out.Model = buildInteractionModel(doc, plan)
		}
		return out
	}
	if interaction {
		out.Model = BuildInteractionModel(doc)
	}
	if n.Shape != "composite" {
		out.SVG = RenderWithCanvas(n, doc.Theme, doc.Canvas, doc.Annotations)
		return out
	}
	rc := &routeCollector{screenLabels: &out.ScreenLabels, labelConflicts: labelConflicts}
	out.SVG = renderComposite(n, doc.Theme, doc.Canvas, doc.Annotations, rc)
	out.Routes = rc.routes
	if labelConflicts {
		out.IsoRouteCrowding = observeIsoRouteCrowding(rc.routes, rc.paint)
	}
	for _, route := range out.Routes {
		if route.Drawing != nil && route.Drawing.Label != nil && route.Drawing.Label.Typography != nil {
			out.ConnectorLabels = append(out.ConnectorLabels, ConnectorLabelRecord{route.Index, *route.Drawing.Label})
		}
	}
	return out
}

// RenderSceneWithReport shares the exact emitted screen/connector-label records with
// the report. Other report diagnostics retain their existing resolvers.
func RenderSceneWithReport(doc *Document) (SceneResult, RenderReport) {
	scene := renderSceneObserved(doc, true)
	report := buildRenderReport(doc, scene.ScreenLabels)
	report.ConnectorLabels = scene.ConnectorLabels
	report.PlanLabels = scene.PlanLabels
	report.PlanRouteCrowding = scene.PlanRouteCrowding
	report.IsoRouteCrowding = scene.IsoRouteCrowding
	if c := scene.IsoRouteCrowding; c != nil {
		if c.Status != "complete" {
			report.Defects = append(report.Defects, ReportDefect{Kind: "iso-route-clearance-unresolved", Severity: "warning", Location: "iso routes", Message: c.Reason})
		}
		for _, conflict := range c.Conflicts {
			report.Defects = append(report.Defects, ReportDefect{Kind: "iso-route-clearance", Severity: "warning", Location: fmt.Sprintf("connector[%d], connector[%d]", conflict.ConnectorIndex, conflict.PeerIndex), Message: "submitted isometric routes lack separation within the declared visible-envelope scope (" + conflict.Kind + ")"})
		}
	}
	if c := scene.PlanRouteCrowding; c != nil {
		if c.Status != "complete" {
			report.Defects = append(report.Defects, ReportDefect{Kind: "plan-route-clearance-unresolved", Severity: "warning", Location: "plan routes", Message: c.Reason})
		}
		for _, conflict := range c.Conflicts {
			report.Defects = append(report.Defects, ReportDefect{Kind: "plan-route-clearance", Severity: "warning", Location: fmt.Sprintf("connector[%d], connector[%d]", conflict.ConnectorIndex, conflict.PeerIndex), Message: "submitted plan routes lack separation outside their shared endpoint neighbourhood (" + conflict.Kind + ")"})
		}
	}
	for _, label := range scene.PlanLabels {
		if label.Placement != nil && label.Placement.Status == "blocked" {
			location := label.PartID
			if location == "" {
				location = fmt.Sprintf("plan-part[%d]", label.PartIndex)
			}
			report.Defects = append(report.Defects, ReportDefect{Kind: "plan-label-placement", Severity: "warning", Location: location, Message: "plan title intersects protected geometry within its declared placement scope"})
		}
		if label.Status == "overflow" {
			location := label.PartID
			if location == "" {
				location = fmt.Sprintf("plan-part[%d]", label.PartIndex)
			}
			report.Defects = append(report.Defects, ReportDefect{Kind: "plan-label-fit", Severity: "warning", Location: location, Message: fmt.Sprintf("plan title ink exceeds its available budget (left/top/right/bottom): %.2f / %.2f / %.2f / %.2f", label.Overflow[0], label.Overflow[1], label.Overflow[2], label.Overflow[3])})
		}
	}
	for _, record := range scene.ConnectorLabels {
		if record.Label.Typography.Placement == "blocked" {
			report.Defects = append(report.Defects, ReportDefect{Kind: "connector-label-placement", Severity: "warning", Location: fmt.Sprintf("connector[%d]", record.ConnectorIndex), Message: connectorTextConflictMessage(record.Label.Typography.Conflicts)})
		}
	}
	return scene, report
}

func observeScreenLabel(index int, p partInfo, block screenTextBlock, cx, y float64, family, weight string) ScreenLabel {
	out := ScreenLabel{PartID: p.id, PartIndex: index,
		Bounds:     ScreenLabelRect{svgCoord(cx - block.width/2), svgCoord(y), svgCoord(block.width), svgCoord(block.height)},
		FontFamily: family, FontWeight: weight, TextMetrics: "estimated-advance"}
	if block.font == nil {
		// Legacy text is emitted at one decimal, while its box was computed
		// with the requested size. Observe that real contract without fixing it
		// by silently inventing known glyph bounds for an arbitrary font.
		out.FontSize, _ = strconv.ParseFloat(strconv.FormatFloat(block.size, 'f', 1, 64), 64)
		out.Lines = []ScreenLabelLine{{Text: p.screenLabel, X: svgCoord(cx), Baseline: svgCoord(y+block.height/2) + .35*out.FontSize}}
		return out
	}
	out.FontSize, out.TextMetrics, out.FontHash = block.size, "shaped-ink", block.font.Hash()
	for _, line := range block.lines {
		x, baseline := svgCoord(cx), svgCoord(y+line.baseline)
		m := block.font.Measure(line.text)
		advance := m.Advance * out.FontSize
		l := ScreenLabelLine{Text: line.text, X: x, Baseline: baseline, Advance: &advance}
		if m.HasInk {
			l.InkBounds = &ScreenLabelRect{X: x + (m.Left-m.Advance/2)*out.FontSize, Y: baseline + m.Top*out.FontSize,
				Width: (m.Right - m.Left) * out.FontSize, Height: (m.Bottom - m.Top) * out.FontSize}
		}
		out.Lines = append(out.Lines, l)
	}
	return out
}
