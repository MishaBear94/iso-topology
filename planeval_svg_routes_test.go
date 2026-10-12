package isotopo

import (
	"reflect"
	"testing"
)

// The reference must read the actual SVG independently of its serialization
// order. Otherwise adding a clip attribute silently scores the plan fallback.
func TestIsoSVGRouteAttributes(t *testing.T) {
	want := map[int][][2]float64{7: {{10, 20}, {110, 20}, {110, 70}}}
	for _, attrs := range []string{
		`data-connector="7" data-from="a" data-to="b" data-route="0,0,10,20 1,1,110,20 2,2,110,70"`,
		`data-connector="7" clip-path="url(#visible)" data-from="a" data-to="b" data-route="0,0,10,20 1,1,110,20 2,2,110,70"`,
		`data-route='0,0,10,20 1,1,110,20 2,2,110,70' data-to='b' data-from='a' data-connector='7'`,
		"data-connector=\"7\"\n data-from=\"a&amp;b\"\n data-to=\"c\"\n data-route=\"0,0,10,20 1,1,110,20 2,2,110,70\"",
	} {
		svg := `<svg xmlns="http://www.w3.org/2000/svg"><path ` + attrs + `/></svg>`
		if got := isoRoutesFromSVG(svg); !reflect.DeepEqual(got, want) {
			t.Errorf("attributes %s: got %v, want %v", attrs, got, want)
		}
	}
}

func TestIsoSVGRouteRequiresAnActualPath(t *testing.T) {
	attrs := `data-connector="7" data-from="a" data-to="b" data-route="0,0,10,20 1,1,110,20"`
	svg := `<svg><!-- <path ` + attrs + `/> --><g ` + attrs + `/><path data-connector="8" d="M 0,0 L 1,1"/></svg>`
	if got := isoRoutesFromSVG(svg); len(got) != 0 {
		t.Fatalf("non-path metadata was treated as a route: %v", got)
	}
}
