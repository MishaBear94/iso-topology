// Regression tests for the byte-count width bugs fixed by the shared
// textmetrics package: CJK is 3 bytes per glyph in UTF-8, so any
// len(string)-based width estimate inflated Chinese labels ~3×.
package isotopo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// TestScreenLabelCJKBoxWidth renders a part whose label is routed to the
// screen-space path (style.text.orient: screen) with a pure-CJK label
// and asserts the label box is sized by RUNES (one em per ideograph),
// not by UTF-8 bytes (the pre-fix inject.go behavior).
func TestScreenLabelCJKBoxWidth(t *testing.T) {
	yaml := `
canvas: { background: "#FFFFFF" }
nodes:
  scene:
    shape: composite
    parts:
      - id: title
        shape: iso_text
        geom: { w: 200, d: 26, h: 4 }
        label: "消息队列"
        style:
          text: { orient: screen, size: 20 }
`
	doc, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svg := renderTopology(doc)
	if svg == "" {
		t.Fatal("empty render")
	}
	idx := strings.Index(svg, `data-screen-label="1"`)
	if idx < 0 {
		t.Fatalf("no screen label emitted:\n%s", svg)
	}
	group := svg[idx:]
	if end := strings.Index(group, "</g>"); end >= 0 {
		group = group[:end]
	}

	// 4 ideographs at size 20 → 4·20·1.0em + 16 padding = 96.00.
	wantW := textmetrics.TextPx("消息队列", 20, textmetrics.NarrowScreenLabel) + 16
	want := fmt.Sprintf(`width="%.2f"`, wantW)
	if !strings.Contains(group, want) {
		t.Errorf("screen label box: want %s in %q", want, group)
	}

	// The byte-count bug: 12 bytes · 20 · 0.58 + 16 = 155.20.
	buggy := fmt.Sprintf(`width="%.2f"`, float64(len("消息队列"))*20*0.58+16)
	if strings.Contains(group, buggy) {
		t.Errorf("screen label box still sized by bytes (%s):\n%q", buggy, group)
	}
}

// TestScreenLabelMixedBoxWidth locks the mixed latin+CJK box width.
func TestScreenLabelMixedBoxWidth(t *testing.T) {
	yaml := `
canvas: { background: "#FFFFFF" }
nodes:
  scene:
    shape: composite
    parts:
      - id: gw
        shape: rectangle
        geom: { w: 120, d: 80, h: 24 }
        label: "API 网关"
        style:
          text: { orient: screen, size: 20 }
`
	doc, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svg := renderTopology(doc)
	// narrow "API " (4) + wide "网关" (2): 4·20·0.58 + 2·20 + 16 = 102.40
	want := fmt.Sprintf(`width="%.2f"`, 4*20*0.58+2*20*1.0+16)
	if !strings.Contains(svg, want) {
		t.Errorf("mixed screen label box: want %s in output", want)
	}
}

// TestLabelIssues_TruncationCJKRuneAware — validate_visual's truncation
// estimate must count runes, not bytes. 10 ideographs = 30 bytes: the
// old estimate (30·7 = 210px) falsely flagged a label that fits
// (10·12 = 120px ≤ availPx 171).
func TestLabelIssues_TruncationCJKRuneAware(t *testing.T) {
	mk := func(label string) *Document {
		return &Document{
			Nodes: map[string]*Node{
				"scene": {
					Shape: "composite",
					Parts: []*CompositePart{
						{
							ID:    "p1",
							Shape: "rectangle",
							Label: label,
							Geom:  &Geom{W: 5}, // availPx = 5·40·0.9·0.95 = 171
						},
					},
				},
			},
		}
	}

	// Fits: 10 CJK runes → 120px ≤ 171px. (Bytes said 210px > 171px.)
	issues := labelIssues(mk("数据库集群主从复制链"))
	if hasWarning(issues, "label may be truncated") {
		t.Errorf("CJK label that fits was flagged (byte counting?): %v", issues)
	}

	// Genuinely too wide: 20 CJK runes → 240px > 171px.
	issues = labelIssues(mk("数据库集群主从复制链数据库集群主从复制链"))
	if !hasWarning(issues, "label may be truncated") {
		t.Errorf("over-wide CJK label not flagged: %v", issues)
	}

	// ASCII behavior is frozen: 10 chars · 7 = 70px ≤ 171px, no warning.
	issues = labelIssues(mk("ABCDEFGHIJ"))
	if hasWarning(issues, "label may be truncated") {
		t.Errorf("ASCII estimate drifted: %v", issues)
	}
}
