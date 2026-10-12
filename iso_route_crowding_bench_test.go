package isotopo

import (
	"encoding/json"
	"testing"
)

// Keep preview, diagnostics and repair separate: the detailed observer is not
// on the ordinary preview path. Repair includes cloning the authored document.
func BenchmarkIsoRouteCrowding(b *testing.B) {
	for _, kind := range []string{"preview", "report", "repair", "dense-report"} {
		b.Run(kind, func(b *testing.B) {
			doc, err := Parse([]byte(isoCrowdingRepairFixture))
			if err != nil {
				b.Fatal(err)
			}
			if kind == "dense-report" {
				for i := 0; i < 20; i++ {
					doc.Scene().Connectors = append(doc.Scene().Connectors, &Connector{From: "a", To: "b", Routing: "orthogonal"})
				}
			}
			raw, err := json.Marshal(doc)
			if err != nil {
				b.Fatal(err)
			}
			RenderScene(doc) // warm immutable font resources before timing
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				switch kind {
				case "preview":
					RenderScene(doc)
				case "report", "dense-report":
					RenderSceneWithReport(doc)
				case "repair":
					var candidate Document
					if err := json.Unmarshal(raw, &candidate); err != nil {
						b.Fatal(err)
					}
					RepairScene(&candidate)
				}
			}
		})
	}
}
