package iso25d

// v3.12 — prism projected side patterns + palette gradient dispatch.
//
// Numeric contract: SideMatrixForEdge must (a) reproduce the box
// left/right matrices byte-for-byte for the axis-aligned edges, and
// (b) produce the analytically-derived shear for every visible wall of
// a hexprism, so Projected hatch/dot tiles hug each wall's true iso
// plane instead of falling back to the top-face matrix.

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// TestSideMatrixForEdge_BoxParity — the generalized matrix collapses to
// the historical box constants for the two axis-aligned wall edges.
func TestSideMatrixForEdge_BoxParity(t *testing.T) {
	// Box left wall: base edge along world +x (y = d).
	if got, want := SideMatrixForEdge(1, 0), sideMatrix("left"); got != want {
		t.Errorf("edge (1,0): got %q, want box left %q", got, want)
	}
	// Box right wall: base edge along world +y (x = w). The u axis is
	// canonicalized to point screen-right, exactly the box constant.
	if got, want := SideMatrixForEdge(0, 1), sideMatrix("right"); got != want {
		t.Errorf("edge (0,1): got %q, want box right %q", got, want)
	}
	// Normalization: a scaled edge yields the identical matrix.
	if a, b := SideMatrixForEdge(37.5, 0), SideMatrixForEdge(1, 0); a != b {
		t.Errorf("edge length must not matter: %q vs %q", a, b)
	}
	// Winding: the reversed edge yields the identical matrix.
	if a, b := SideMatrixForEdge(-1, 0), SideMatrixForEdge(1, 0); a != b {
		t.Errorf("edge winding must not matter: %q vs %q", a, b)
	}
	// Degenerate edge → no matrix (caller falls back to the top plane).
	if got := SideMatrixForEdge(0, 0); got != "" {
		t.Errorf("zero edge: got %q, want empty", got)
	}
	// The camera-facing diagonal projects to a pure vertical compression
	// — and the flipped diagonal must print +0, never "-0.0000".
	if got, want := SideMatrixForEdge(1, 1), "matrix(0.0000 0.7071 0 1 0 0)"; got != want {
		t.Errorf("edge (1,1): got %q, want %q", got, want)
	}
	if a, b := SideMatrixForEdge(-1, -1), SideMatrixForEdge(1, 1); a != b {
		t.Errorf("flipped diagonal must not emit negative zero: %q vs %q", a, b)
	}
}

// expectedWallMatrix recomputes the projection analytically: a unit
// world vector along the base edge maps to ((ex-ey)·cos30, (ex+ey)·sin30)
// on screen; the vertical tile axis follows world z.
func expectedWallMatrix(ax, ay, bx, by float64) string {
	ex, ey := bx-ax, by-ay
	l := math.Hypot(ex, ey)
	ex, ey = ex/l, ey/l
	ux := (ex - ey) * cos30
	uy := (ex + ey) * sin30
	if ux < 0 || (ux == 0 && uy < 0) {
		ux, uy = 0-ux, 0-uy
	}
	return fmt.Sprintf("matrix(%.4f %.4f 0 1 0 0)", ux, uy)
}

// TestPrismSideMatrices_Hexprism — every visible wall of a square-
// footprint hexprism pins its Projected pattern with the exact
// edge-derived shear (numeric assertion per wall).
func TestPrismSideMatrices_Hexprism(t *testing.T) {
	const w, d, h = 120.0, 120.0, 40.0
	const n = 6
	base := prismBase(w, d, n)

	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = w, d, h
	o.FaceSurfaces = map[string]*FaceSurface{
		"*": {Fill: &FaceFill{
			Kind:    "pattern",
			Pattern: &FacePatternSpec{Kind: "hatch", Color: "#334455", Spacing: 6, Projected: true},
		}},
	}
	svg := RenderIsoPrism(o, n)

	// Analytic visibility: outward normal with nx+ny > 0. For the
	// square-footprint hexprism that's side1, side2, side3.
	visible := map[int]bool{}
	cx, cy := w/2, d/2
	for k := 0; k < n; k++ {
		a, b := base[k], base[(k+1)%n]
		nx, ny := b[1]-a[1], a[0]-b[0]
		mx, my := (a[0]+b[0])/2-cx, (a[1]+b[1])/2-cy
		if nx*mx+ny*my < 0 {
			nx, ny = -nx, -ny
		}
		visible[k] = nx+ny > 1e-9
	}
	if !visible[1] || !visible[2] || !visible[3] || visible[0] || visible[4] || visible[5] {
		t.Fatalf("visibility assumption drifted: %v", visible)
	}

	// Hard-coded expectations (the analytic values, %.4f-rounded):
	//   side1 edge dir (0,1)             → the box right matrix
	//   side2 edge dir (-cos30, sin30)   → (1.1830, 0.1830) after flip
	//   side3 edge dir (-cos30, -sin30)  → (0.3170, 0.6830) after flip
	want := map[int]string{
		1: "matrix(0.8660 -0.5000 0 1 0 0)",
		2: "matrix(1.1830 0.1830 0 1 0 0)",
		3: "matrix(0.3170 0.6830 0 1 0 0)",
	}
	for k, wm := range want {
		a, b := base[k], base[(k+1)%n]
		if got := expectedWallMatrix(a[0], a[1], b[0], b[1]); got != wm {
			t.Errorf("side%d analytic matrix: got %q, want %q", k, got, wm)
		}
		if got := SideMatrixForEdge(b[0]-a[0], b[1]-a[1]); got != wm {
			t.Errorf("side%d SideMatrixForEdge: got %q, want %q", k, got, wm)
		}
		if !strings.Contains(svg, `patternTransform="`+wm) {
			t.Errorf("side%d wall matrix %q missing from rendered SVG", k, wm)
		}
	}
	// The old bug: every projected side pattern fell back to the TOP
	// plane matrix. The "*" wildcard also covers the lid, which
	// legitimately uses it — so exactly ONE top-matrix occurrence,
	// never one per wall.
	if got := strings.Count(svg, `patternTransform="`+isoTopMatrix); got != 1 {
		t.Errorf("top-plane matrix occurrences: got %d, want exactly 1 (the lid) — walls must not fall back to it", got)
	}
	// One namespaced pattern def per visible wall.
	for _, k := range []int{1, 2, 3} {
		if !strings.Contains(svg, fmt.Sprintf(`facepat-side%d`, k)) {
			t.Errorf("missing pattern def for side%d", k)
		}
	}
}

// TestPrismProjectedGradient_UsesWallMatrix — a Projected linear
// gradient on a prism wall carries the wall's gradientTransform.
func TestPrismProjectedGradient_UsesWallMatrix(t *testing.T) {
	const w, d, h = 120.0, 120.0, 40.0
	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = w, d, h
	o.FaceSurfaces = map[string]*FaceSurface{
		"side1": {Fill: &FaceFill{
			Kind:      "linearGradient",
			Projected: true,
			Stops:     []FaceStop{{Offset: 0, Color: "#111111"}, {Offset: 1, Color: "#EEEEEE"}},
		}},
	}
	svg := RenderIsoPrism(o, 6)
	if !strings.Contains(svg, `gradientTransform="matrix(0.8660 -0.5000 0 1 0 0)"`) {
		t.Errorf("projected gradient on side1 must carry the wall matrix; svg lacks it")
	}
}

// TestPrismPaletteGradients_NormalDispatch — palette left/right
// gradients reach the prism walls, split by normal dominance exactly
// like the box's two visible walls; top gradient covers the lid.
func TestPrismPaletteGradients_NormalDispatch(t *testing.T) {
	const w, d, h = 120.0, 120.0, 40.0
	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = w, d, h
	o.TopGradient = &FaceGradient{From: "#FFFFFF", To: "#DDDDEE", Dir: "diag"}
	o.LeftGradient = &FaceGradient{From: "#6D28D9", To: "#3B1D74", Dir: "down"}
	o.RightGradient = &FaceGradient{From: "#8B5CF6", To: "#5B3DB0", Dir: "down"}
	svg := RenderIsoPrism(o, 6)

	for _, id := range []string{"prism-grad-top", "prism-grad-left", "prism-grad-right"} {
		if !strings.Contains(svg, `<linearGradient id="`+id+`"`) {
			t.Errorf("missing gradient def %s", id)
		}
	}
	// side1 normal (1,0) → right group; side2 (0.5,0.866) and side3
	// (-0.5,0.866) → left group; the lid → top.
	assertFaceFill := func(face, ref string) {
		t.Helper()
		marker := `data-face="` + face + `"`
		i := strings.Index(svg, marker)
		if i < 0 {
			t.Errorf("face %s not rendered", face)
			return
		}
		seg := svg[i:]
		if j := strings.Index(seg, "/>"); j >= 0 {
			seg = seg[:j]
		}
		if !strings.Contains(seg, `fill="url(#`+ref+`)"`) {
			t.Errorf("face %s: want fill url(#%s), got element %q", face, ref, seg)
		}
	}
	assertFaceFill("side1", "prism-grad-right")
	assertFaceFill("side2", "prism-grad-left")
	assertFaceFill("side3", "prism-grad-left")
	assertFaceFill("top", "prism-grad-top")
}

// TestPrismPaletteGradients_NilKeepsLegacyBytes — with no gradients the
// render is byte-identical to the pre-v3.12 output (golden safety).
func TestPrismPaletteGradients_NilKeepsLegacyBytes(t *testing.T) {
	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = 150, 150, 44
	svg := RenderIsoPrism(o, 6)
	if strings.Contains(svg, "prism-grad") {
		t.Errorf("no gradients set, yet gradient defs emitted")
	}
	if !strings.Contains(svg, `data-face="side1" points=`) {
		t.Errorf("expected plain side faces")
	}
	if !strings.Contains(svg, `fill="`+o.RightFill+`"`) || !strings.Contains(svg, `fill="`+o.LeftFill+`"`) {
		t.Errorf("palette solid fills must be untouched")
	}
}

// TestPrismGradient_FeatureInteractions — gradients + projected side
// pattern + backglow + grain + outline coexist on one prism (the
// repo's feature-interaction rule: new visual features must compose).
func TestPrismGradient_FeatureInteractions(t *testing.T) {
	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = 140, 140, 48
	o.LeftGradient = &FaceGradient{From: "#6D28D9", To: "#3B1D74", Dir: "down"}
	o.RightGradient = &FaceGradient{From: "#8B5CF6", To: "#5B3DB0", Dir: "down"}
	o.BackglowColor, o.BackglowRadius, o.BackglowOpacity = "#A78BFA", 30, 0.5
	o.GrainIntensity, o.GrainScale = 0.3, 1.2
	o.OutlineColor, o.OutlineWidth = "#4C1D95", 1.5
	o.FaceSurfaces = map[string]*FaceSurface{
		"side2": {Fill: &FaceFill{
			Kind:    "pattern",
			Pattern: &FacePatternSpec{Kind: "dots", Color: "#FFFFFF", Spacing: 5, Projected: true},
		}},
	}
	svg := RenderIsoPrism(o, 6)

	// faces override outranks the palette gradient on side2 …
	if !strings.Contains(svg, `data-face="side2" points=`) || !strings.Contains(svg, `facepat-side2`) {
		t.Errorf("side2 pattern override missing")
	}
	if !strings.Contains(svg, `patternTransform="matrix(1.1830 0.1830 0 1 0 0)`) {
		t.Errorf("side2 projected pattern must use its wall matrix")
	}
	// … while side1/side3 keep their palette gradients.
	if !strings.Contains(svg, `fill="url(#prism-grad-right)"`) || !strings.Contains(svg, `fill="url(#prism-grad-left)"`) {
		t.Errorf("palette gradients must survive alongside the pattern override")
	}
	for _, want := range []string{"prism-backglow", "prism-grain", `data-face="outline"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("effect %q missing when combined with gradients", want)
		}
	}
	// Determinism: same input, same bytes.
	if again := RenderIsoPrism(o, 6); again != svg {
		t.Errorf("render is not deterministic")
	}
}

// TestPrismSideMatrices_Triprism — asymmetric footprint (w≠d): the
// matrix follows the true edge of the inscribed ellipse, not a unit
// n-gon assumption.
func TestPrismSideMatrices_Triprism(t *testing.T) {
	const w, d, h = 160.0, 90.0, 40.0
	const n = 3
	base := prismBase(w, d, n)
	o := DefaultIsoBox()
	o.Width, o.Depth, o.Height = w, d, h
	o.FaceSurfaces = map[string]*FaceSurface{
		"*": {Fill: &FaceFill{
			Kind:    "pattern",
			Pattern: &FacePatternSpec{Kind: "hatch", Color: "#223344", Spacing: 7, Projected: true},
		}},
	}
	svg := RenderIsoPrism(o, n)
	found := 0
	for k := 0; k < n; k++ {
		a, b := base[k], base[(k+1)%n]
		wm := expectedWallMatrix(a[0], a[1], b[0], b[1])
		if strings.Contains(svg, `patternTransform="`+wm) {
			found++
		}
	}
	// A triprism shows 1 or 2 walls depending on orientation; at least
	// one must carry its exact edge matrix, and only the lid (also
	// covered by the "*" wildcard) may use the top-plane fallback.
	if found == 0 {
		t.Errorf("no visible triprism wall carries its edge-derived matrix")
	}
	if got := strings.Count(svg, `patternTransform="`+isoTopMatrix); got != 1 {
		t.Errorf("top-plane matrix occurrences: got %d, want exactly 1 (the lid)", got)
	}
}
