package iso25d

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

func resourceTestParts() []CompositePart {
	var parts []CompositePart
	for _, weight := range []string{"400", "600", "700"} {
		for _, label := range []string{"", "中文", "AV office\nÁgj", "AV office\nÁgj"} {
			for _, shape := range []string{"person", "circle", "rectangle", "square", "cylinder", "stored_data", "queue", "cloud", "triprism", "hexprism", "octprism", "wedge", "custom_path", "array1d", "array2d", "array3d", "rack", "unknown", "iso_text"} {
				o := ConvertOpts{Width: 120, Depth: 110, Height: 36, Label: label, FontFamily: "Source Sans Pro", FontWeight: weight, FontSize: 17.23}
				if shape == "custom_path" {
					o.Params = map[string]any{"path": "M 0 0 L 120 0 L 120 30 L 35 30 L 35 110 L 0 110 Z"}
				}
				parts = append(parts, CompositePart{ID: fmt.Sprint(len(parts)), Shape: shape, Opts: o, OffWX: float64(len(parts)) * 130})
				if shape == "rectangle" || shape == "cloud" {
					o.Icon = ResolveBrandIcon("iso://glyph/database")
					o.CornerRadius = 20
					parts = append(parts, CompositePart{Shape: shape, Opts: o})
				}
			}
		}
	}
	return parts
}

// Compare every dispatched fragment with the independently rendered public
// standalone result, removing only resources already emitted earlier. This
// protects first-use placement, empty/unsupported content, dispatch aliases,
// mixed weights and the rounded-box secondary renderer.
func TestCompositeFontResourcesMatchStandalone(t *testing.T) {
	fonts := make(fontResources)
	seen := make(map[*textmetrics.BuiltinFont]bool)
	parts := resourceTestParts()
	for i, p := range parts {
		want := Convert2DTo25D(p.Shape, p.Opts)
		for _, f := range textmetrics.FontsInSVG(want) {
			if seen[f] {
				want = strings.ReplaceAll(want, f.CSS(), "")
			} else {
				seen[f] = true
				want = textmetrics.DeduplicateFonts(want)
			}
		}
		got := convert2DTo25D(p.Shape, p.Opts, fonts)
		if got != want {
			t.Fatalf("fragment %d (%s): shared resources changed content or failed to suppress a duplicate", i, p.Shape)
		}
	}
	if len(fonts) != 3 || len(seen) != 3 {
		t.Fatal("font collection lost a weight")
	}
	// Independent public calls must remain self-contained after a composite.
	for _, shape := range []string{"person", "circle", "cloud", "rectangle"} {
		o := ConvertOpts{Label: "AV office", FontFamily: "Source Sans Pro"}
		for i := 0; i < 2; i++ {
			if strings.Count(Convert2DTo25D(shape, o), `<style data-isotopo-font=`) != 1 {
				t.Fatal("standalone inherited a previous render's resource state")
			}
		}
	}
}

func TestCompositeFontResourcesStayRenderLocal(t *testing.T) {
	parts := []CompositePart{}
	for i, shape := range []string{"person", "cloud", "circle", "rectangle", "cylinder", "rack"} {
		parts = append(parts, CompositePart{ID: fmt.Sprint(i), Shape: shape, OffWX: float64(i) * 200, Opts: ConvertOpts{Label: "AV office", FontFamily: "Source Sans Pro", FontWeight: []string{"400", "600", "700"}[i%3]}})
	}
	want := RenderComposite(parts)
	if strings.Count(want, `<style data-isotopo-font=`) != 3 {
		t.Fatal("missing or duplicated shared font")
	}
	before := append([]CompositePart(nil), parts...)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 4 {
				if got := RenderComposite(parts); got != want {
					t.Error("another render consumed or changed this scene's resource")
				}
			}
		})
	}
	wg.Wait()
	if !reflect.DeepEqual(before, parts) {
		t.Fatal("resource collection changed the authored parts")
	}
}
