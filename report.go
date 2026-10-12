package isotopo

// L2 of the agent-loop harness plan: one authoritative render report so an agent
// reads a single structured payload — the readability breakdown plus every
// residual defect located and, where the fix is reliable, a machine-applicable
// patch — instead of stitching validate+evaluate+occlusion and guessing the edit.

// Patch is a declarative, machine-applicable DSL edit that clears a defect.
type Patch struct {
	Target string  `json:"target"` // part id or label to edit
	Field  string  `json:"field"`  // e.g. "layout.padding"
	Value  float64 `json:"value"`
}

// ReportDefect is one defect, located, with a fix where we can generate a
// reliable one.
type ReportDefect struct {
	Kind     string `json:"kind"`     // caption-occlusion | neighbour-occlusion | content-fit | overlap | crossing | tunnel
	Severity string `json:"severity"` // warning | error
	Location string `json:"location"` // part/edge id(s) the defect involves
	Message  string `json:"message"`
	Patch    *Patch `json:"patch,omitempty"`
}

// RenderReport is the single payload emitted by `render --report`.
type RenderReport struct {
	Readability       ReadabilityReport       `json:"readability"`
	Defects           []ReportDefect          `json:"defects"`
	ContentFits       []ContentFitObservation `json:"content_fits,omitempty"`
	ScreenLabels      []ScreenLabel           `json:"screen_labels,omitempty"`
	ConnectorLabels   []ConnectorLabelRecord  `json:"connector_labels,omitempty"`
	PlanLabels        []PlanLabel             `json:"plan_labels,omitempty"`
	IsoRouteCrowding  *IsoRouteCrowding       `json:"iso_route_crowding,omitempty"`
	PlanRouteCrowding *PlanRouteCrowding      `json:"plan_route_crowding,omitempty"`
}

// BuildRenderReport assembles the report for the current scene state: R plus
// breakdown, and every label/caption occlusion located — with a padding patch
// attached to each in-group caption-ride (the value the repair loop converges
// to, so applying it is guaranteed to clear the ride).
func BuildRenderReport(doc *Document) RenderReport {
	_, report := RenderSceneWithReport(doc)
	return report
}

func buildRenderReport(doc *Document, labels []ScreenLabel) RenderReport {
	rep := RenderReport{Readability: Readability(doc), ScreenLabels: labels}
	if doc == nil {
		return rep
	}
	rep.ContentFits = ContentFits(doc)
	for _, observation := range rep.ContentFits {
		if issue, ok := contentFitIssue(observation); ok {
			rep.Defects = append(rep.Defects, ReportDefect{Kind: "content-fit", Severity: string(issue.Severity), Location: observation.Path, Message: issue.Message})
		}
	}
	var pad map[string]float64
	for _, is := range LabelOcclusionIssues(doc) {
		d := ReportDefect{Severity: string(is.Severity), Message: is.Message}
		if m := captionOcclRe.FindStringSubmatch(is.Message); m != nil {
			// The repair simulation only supplies caption padding patches.
			// Plan reports and reports without caption rides do not use it.
			if pad == nil {
				pad = captionPatchValues(doc)
			}
			d.Kind = "caption-occlusion"
			label := m[1]
			d.Location = groupTarget(doc, label)
			if v, ok := pad[label]; ok {
				d.Patch = &Patch{Target: d.Location, Field: "layout.padding", Value: v}
			}
		} else {
			// A label covered by a node from another module — the screen-space
			// neighbour-label class. Located, but no reliable auto-patch yet (L4).
			d.Kind = "neighbour-occlusion"
		}
		rep.Defects = append(rep.Defects, d)
	}
	return rep
}

// captionPatchValues simulates the repair loop on a clone and reads the front
// padding each caption-ride group converged to, keyed by group label — the
// patch value that clears that group's ride.
func captionPatchValues(doc *Document) map[string]float64 {
	clone := cloneDocParts(doc)
	RepairScene(clone)
	out := map[string]float64{}
	walkParts(clone, func(p *CompositePart) {
		if p.Label != "" && p.Layout != nil && p.Layout.Padding != nil {
			out[p.Label] = *p.Layout.Padding
		}
	})
	return out
}

// groupTarget returns the id of the group carrying the given label (so a patch
// targets it precisely), falling back to the label itself.
func groupTarget(doc *Document, label string) string {
	target := label
	walkParts(doc, func(p *CompositePart) {
		if p.Label == label && p.ID != "" {
			target = p.ID
		}
	})
	return target
}

// ApplyPatch applies a patch to the document in place, returning whether it hit
// a target. Patch targets match by part id or label.
func ApplyPatch(doc *Document, p Patch) bool {
	if doc == nil {
		return false
	}
	var target *CompositePart
	walkParts(doc, func(part *CompositePart) {
		if target == nil && (part.ID == p.Target || part.Label == p.Target) {
			target = part
		}
	})
	if target == nil {
		return false
	}
	switch p.Field {
	case "layout.padding":
		if target.Layout == nil {
			target.Layout = &Layout{}
		}
		v := p.Value
		target.Layout.Padding = &v
		return true
	}
	return false
}

// cloneDocParts isolates fields changed by the repair simulation. Routing
// repair also assigns connector.Routing, so those records must not alias the
// caller's document even though their read-only nested geometry can be shared.
func cloneDocParts(doc *Document) *Document {
	nd := *doc
	nd.Nodes = map[string]*Node{}
	for k, n := range doc.Nodes {
		if n == nil {
			nd.Nodes[k] = nil
			continue
		}
		c := *n
		c.Parts = cloneParts(n.Parts)
		if n.Connectors != nil {
			c.Connectors = make([]*Connector, len(n.Connectors))
			for i, connector := range n.Connectors {
				if connector != nil {
					copy := *connector
					c.Connectors[i] = &copy
				}
			}
		}
		if n.Layout != nil {
			l := *n.Layout
			c.Layout = &l
		}
		nd.Nodes[k] = &c
	}
	return &nd
}
