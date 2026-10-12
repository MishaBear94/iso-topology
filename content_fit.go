package isotopo

import (
	"fmt"
	"math"
	"sort"

	"github.com/MarkovWangRR/iso-topology/iso25d"
)

// ContentFitObservation locates a resolved instance's final top-face layout.
// PartIndex is the index after group/stack lowering, not an authored array
// index. Path points to the authored part when an unambiguous ID is available,
// otherwise to its node. No automatic source patch is implied.
type ContentFitObservation struct {
	NodeID    string                  `json:"node_id"`
	PartID    string                  `json:"part_id,omitempty"`
	PartIndex int                     `json:"part_index"`
	Path      string                  `json:"path"`
	Fit       iso25d.ContentFitResult `json:"fit"`
}

// ContentFits returns bounded top-face layout observations in deterministic
// node/instance order without mutating the author document. Free captions and
// other unsupported shapes remain explicitly unsupported; the separate plan
// renderer has no observations here. Each item identifies its measurement;
// these observations do not certify legibility or visibility.
func ContentFits(doc *Document) []ContentFitObservation {
	if doc == nil || (doc.Canvas != nil && doc.Canvas.Projection == "top") {
		return nil
	}
	names := make([]string, 0, len(doc.Nodes))
	for name := range doc.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []ContentFitObservation
	for _, name := range names {
		out = append(out, contentFitsInNode(doc.Nodes[name], doc.Theme, doc.Canvas, name)...)
	}
	return out
}

func contentFitsInNode(n *Node, theme *Theme, canvas *Canvas, nodeID string) []ContentFitObservation {
	if n == nil || (canvas != nil && canvas.Projection == "top") {
		return nil
	}
	path := fmt.Sprintf("nodes.%s", nodeID)
	if n.Shape != "composite" {
		shape, o := Flatten(n, theme)
		fit := iso25d.ShapeContent(shape, o).Fit
		if fit.Status == "empty" {
			return nil
		}
		return []ContentFitObservation{{NodeID: nodeID, PartIndex: -1, Path: path, Fit: fit}}
	}
	paths := map[string]string{}
	var sourcePaths func([]*CompositePart, string)
	sourcePaths = func(parts []*CompositePart, parent string) {
		for i, p := range parts {
			if p == nil {
				continue
			}
			pp := fmt.Sprintf("%s.parts[%d]", parent, i)
			if p.ID != "" {
				if _, exists := paths[p.ID]; exists {
					paths[p.ID] = path // ambiguous IDs must not suggest a wrong source part
				} else {
					paths[p.ID] = pp
				}
			}
			sourcePaths(p.Parts, pp)
		}
	}
	sourcePaths(n.Parts, path)
	clone := resolvedLayout(n, canvas)
	flat := lowerCompositeParts(clone.Parts, 0, 0, 0, clone.GridStep)
	var out []ContentFitObservation
	for i, p := range flat {
		if p == nil {
			continue
		}
		shape, o := flattenCompositeContent(p, theme)
		fit := iso25d.ShapeContent(shape, o).Fit
		if fit.Status == "empty" {
			continue
		}
		pp := paths[p.ID]
		if pp == "" {
			pp = paths[stackBase(p.ID)]
		}
		if pp == "" {
			pp = path
		}
		out = append(out, ContentFitObservation{NodeID: nodeID, PartID: p.ID, PartIndex: i, Path: pp, Fit: fit})
	}
	return out
}

func contentFitIssue(o ContentFitObservation) (Issue, bool) {
	if o.Fit.Status != "estimated-overflow" {
		return Issue{}, false
	}
	maxExcess := 0.0
	for _, item := range o.Fit.Items {
		for _, v := range item.Overflow {
			maxExcess = math.Max(maxExcess, v)
		}
	}
	name := o.PartID
	if name == "" {
		name = o.NodeID
	}
	measurement := "estimated text advances and image viewports"
	for _, item := range o.Fit.Items {
		if item.TextMetrics == "shaped-ink" {
			measurement = "shaped text bounds, any fallback estimates and image viewports"
			break
		}
	}
	message := fmt.Sprintf("content on %q exceeds its preferred top-face padding budget by up to %.2f local units using %s; inspect the rendered label or enlarge the content area", name, maxExcess, measurement)
	if len(o.Fit.Gaps) > 0 {
		shortfall := 0.0
		for _, gap := range o.Fit.Gaps {
			shortfall = math.Max(shortfall, gap.Shortfall)
		}
		if maxExcess <= .01 {
			message = fmt.Sprintf("content on %q does not meet its preferred top-face ink separation: up to %.2f local units missing between text lines or icon/text", name, shortfall)
		} else {
			message += fmt.Sprintf("; preferred ink separation is short by up to %.2f local units", shortfall)
		}
	}
	if o.Fit.Manual {
		message += "; the authored icon placement has been preserved"
	}
	return Issue{Severity: SeverityWarning, Path: o.Path, Message: message, Suggest: "inspect content at the target display size; adjust face dimensions or authored content placement"}, true
}

// ContentFitIssues reports estimated budget overflow as a warning, including
// intentional authored placements. Unknown font ink is not a structural error.
func ContentFitIssues(doc *Document) []Issue {
	var out []Issue
	for _, o := range ContentFits(doc) {
		if issue, ok := contentFitIssue(o); ok {
			out = append(out, issue)
		}
	}
	return out
}

// Keep each instance, content kind and violated side separate. A smaller
// overflow in one label must not pay for overflowing a different icon/label.
func addContentFitDefects(out visualDefects, observations []ContentFitObservation) {
	for _, o := range observations {
		if o.Fit.Status != "estimated-overflow" {
			continue
		}
		keyFor := func(kind string) visualDefect {
			key := visualDefect{kind: "content-fit", a: o.PartID, b: kind, peer: -1}
			if o.PartID == "" {
				key.peer = o.PartIndex // separate namespace from authored string IDs
			}
			return key
		}
		for _, gap := range o.Fit.Gaps {
			key := keyFor(fmt.Sprintf("%s:gap:%d:%d", gap.Kind, gap.Before, gap.After))
			out[key] = math.Max(out[key], gap.Shortfall)
		}
		for _, item := range o.Fit.Items {
			if item.Invalid {
				out[keyFor(item.Kind+":invalid")] = 1
			}
			for side, v := range item.Overflow {
				if v > .01 {
					key := keyFor(fmt.Sprintf("%s:%d", item.Kind, side))
					out[key] = math.Max(out[key], v)
				}
			}
		}
	}
}
