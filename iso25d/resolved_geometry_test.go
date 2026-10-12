package iso25d

import (
	"math"
	"regexp"
	"strconv"
	"testing"
)

// Read the actual top-face vertices emitted by each renderer, undo the
// standalone SVG's origin, and compare their centre with an independent world
// projection. This catches frame drift even when overlays agree with each other.
func TestPartOriginMatchesRenderedTopFace(t *testing.T) {
	for _, tc := range []struct {
		name, shape string
		opts        ConvertOpts
		w, d, h     float64
	}{
		{"default box", "rectangle", ConvertOpts{}, 140, 140, 80},
		{"square with unequal inputs", "square", ConvertOpts{Width: 180, Depth: 80, Height: 30}, 180, 180, 30},
		{"rounded box", "rectangle", ConvertOpts{Width: 180, Depth: 120, Height: 30, CornerRadius: 24}, 180, 120, 30},
		{"clamped radius", "rectangle", ConvertOpts{Width: 80, Depth: 120, Height: 30, CornerRadius: 100}, 80, 120, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svg := Convert2DTo25D(tc.shape, tc.opts)
			m := regexp.MustCompile(`<(?:polygon|path) data-face="top"[^>]*(?:points|d)="([^"]+)"`).FindStringSubmatch(svg)
			if len(m) != 2 {
				t.Fatalf("missing top-face geometry: %s", svg)
			}
			nums := regexp.MustCompile(`-?\d+(?:\.\d+)?`).FindAllString(m[1], -1)
			minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for i := 0; i+1 < len(nums); i += 2 {
				px, _ := strconv.ParseFloat(nums[i], 64)
				py, _ := strconv.ParseFloat(nums[i+1], 64)
				minX, maxX = math.Min(minX, px), math.Max(maxX, px)
				minY, maxY = math.Min(minY, py), math.Max(maxY, py)
			}
			ox, oy := PartOriginOffset(tc.shape, tc.opts)
			x, y := (minX+maxX)/2-ox, (minY+maxY)/2-oy
			wx, wy := (tc.w-tc.d)/2*cos30, (tc.w+tc.d)/2*sin30-tc.h
			if math.Hypot(x-wx, y-wy) > .02 {
				t.Fatalf("actual top centre (%g,%g), world projection (%g,%g): origin drifts %.3f px", x, y, wx, wy, math.Hypot(x-wx, y-wy))
			}
		})
	}
}

// The dome apex is an emitted path vertex, so this checks the actual figure
// frame including label-driven SVG width without reusing the origin formula.
func TestPersonOriginMatchesRenderedDome(t *testing.T) {
	for _, o := range []ConvertOpts{
		{},
		{Width: 180, Depth: 80, Height: 45},
		{Width: 120, Depth: 120, Height: 50, Label: "A very long operator label that expands the SVG", FontSize: 22},
	} {
		svg := Convert2DTo25D("person", o)
		m := regexp.MustCompile(`<path data-face="body-left" d="M ([0-9.]+) ([0-9.]+)`).FindStringSubmatch(svg)
		if len(m) != 3 {
			t.Fatal("missing dome apex")
		}
		x, _ := strconv.ParseFloat(m[1], 64)
		y, _ := strconv.ParseFloat(m[2], 64)
		w, d, h := 120.0, 120.0, 50.0
		if o.Width > 0 {
			w = o.Width
		}
		if o.Depth > 0 {
			d = o.Depth
		}
		if o.Height > 0 {
			h = o.Height
		}
		ox, oy := PartOriginOffset("person", o)
		wantX := (w - d) * math.Sqrt(3) / 4
		wantY := (w+d)/4 - h
		if math.Hypot(x-ox-wantX, y-oy-wantY) > .02 {
			t.Fatalf("apex/origin drift with opts %+v: got (%g,%g) want (%g,%g)", o, x-ox, y-oy, wantX, wantY)
		}
	}
}
