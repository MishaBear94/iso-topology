package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	isotopo "github.com/MarkovWangRR/iso-topology"
)

func TestRenderReportUsesCommittedProjection(t *testing.T) {
	oldReport, oldProjection, oldRepair := flagReport, flagProjection, flagRepair
	t.Cleanup(func() { flagReport, flagProjection, flagRepair = oldReport, oldProjection, oldRepair })
	flagReport, flagRepair = true, false
	dir := t.TempDir()
	in := filepath.Join(dir, "scene.yaml")
	data := []byte("nodes:\n  scene:\n    shape: composite\n    parts:\n      - id: node\n        shape: rectangle\n        label: AV office\n        style: {text: {orient: screen, family: Source Sans Pro, size: 18}}\n")
	if err := os.WriteFile(in, data, 0644); err != nil {
		t.Fatal(err)
	}
	for _, projection := range []string{"iso", "top"} {
		t.Run(projection, func(t *testing.T) {
			flagProjection = projection
			out := filepath.Join(dir, projection)
			code, err := renderFile(in, out)
			if err != nil || code != 0 {
				t.Fatalf("%d %v", code, err)
			}
			b, err := os.ReadFile(filepath.Join(out, "report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var report isotopo.RenderReport
			if err = json.Unmarshal(b, &report); err != nil {
				t.Fatal(err)
			}
			svg, _ := os.ReadFile(filepath.Join(out, "topology.svg"))
			if projection == "top" {
				if len(report.ScreenLabels) != 0 || strings.Contains(string(svg), `data-screen-label=`) {
					t.Fatal("top report contains iso labels")
				}
				if len(report.PlanLabels) != 1 || report.PlanLabels[0].FontSize != 18 || !strings.Contains(string(svg), `data-plan-label=`) {
					t.Fatal("top report lost submitted node typography")
				}
			} else {
				if len(report.ScreenLabels) != 1 || !strings.Contains(string(svg), `data-screen-label=`) {
					t.Fatal("iso labels missing from report")
				}
			}
		})
	}
}
