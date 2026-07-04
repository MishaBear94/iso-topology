package iso25d

import (
	"math"
	"strings"
	"testing"
)

// ── person style extension: body gradient / outline / dropShadow / backglow ──

// TestPersonBodyGradient — per-half gradients override the solid body fills
// and land as linearGradient defs.
func TestPersonBodyGradient(t *testing.T) {
	o := DefaultIsoPerson()
	o.BodyLeftGradient = &FaceGradient{From: "#FFC98A", To: "#B86A2B", Dir: "down"}
	o.BodyRightGradient = &FaceGradient{From: "#FFB66A", To: "#8F4E1D", Dir: "down"}
	svg := RenderIsoPerson(o)

	for _, want := range []string{
		`<linearGradient id="person-body-left-grad"`,
		`<linearGradient id="person-body-right-grad"`,
		`data-face="body-left" d="`,
		`fill="url(#person-body-left-grad)"`,
		`fill="url(#person-body-right-grad)"`,
		`stop-color="#FFC98A"`,
		`stop-color="#8F4E1D"`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("body-gradient render missing %q", want)
		}
	}
	// The solid fills must be fully replaced on the body halves.
	if strings.Contains(svg, `fill="`+o.BodyLeft+`"`) {
		t.Errorf("body-left still painted with the solid fill %s despite a gradient", o.BodyLeft)
	}
}

// TestPersonTopGradientFallback — applyPerson maps a topGradient onto BOTH
// dome halves (the dome draws no top face), while an explicit side gradient
// keeps priority for its half.
func TestPersonTopGradientFallback(t *testing.T) {
	top := &FaceGradient{From: "#8EC5FF", To: "#2C5896", Dir: "down"}

	p := DefaultIsoPerson()
	applyPerson(ConvertOpts{TopGradient: top}, &p)
	if p.BodyLeftGradient != top || p.BodyRightGradient != top {
		t.Fatalf("topGradient alone must shade both halves; got left=%v right=%v", p.BodyLeftGradient, p.BodyRightGradient)
	}

	left := &FaceGradient{From: "#111111", To: "#222222"}
	p2 := DefaultIsoPerson()
	applyPerson(ConvertOpts{TopGradient: top, LeftGradient: left}, &p2)
	if p2.BodyLeftGradient != left {
		t.Errorf("explicit leftGradient must outrank the topGradient fallback")
	}
	if p2.BodyRightGradient != top {
		t.Errorf("right half without its own gradient must fall back to topGradient")
	}
}

// TestPersonOutline — effects.outline traces a silhouette accent ring with
// the standard data-face="outline" contract.
func TestPersonOutline(t *testing.T) {
	o := DefaultIsoPerson()
	o.OutlineColor = "#9ED0FF"
	o.OutlineWidth = 2
	o.OutlineDash = "5 3"
	o.OutlineOpacity = 0.9
	svg := RenderIsoPerson(o)

	for _, want := range []string{
		`data-face="outline"`,
		`stroke="#9ED0FF"`,
		`stroke-width="2.00"`,
		`stroke-dasharray="5 3"`,
		`stroke-opacity="0.900"`,
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("outline render missing %q", want)
		}
	}
}

// TestPersonDropShadow — effects.dropShadow emits the blur filter def and
// hangs it on the wrapper <g>, mirroring the box path.
func TestPersonDropShadow(t *testing.T) {
	o := DefaultIsoPerson()
	o.ShadowDx, o.ShadowDy, o.ShadowBlur = 4, 6, 5
	o.ShadowColor = "#05080F"
	svg := RenderIsoPerson(o)

	if !strings.Contains(svg, `<filter id="person-shadow"`) {
		t.Errorf("dropShadow render missing the filter def")
	}
	if !strings.Contains(svg, `<g data-face="wrap" filter="url(#person-shadow)">`) {
		t.Errorf("dropShadow filter not applied to the wrapper <g>")
	}
	// Same gate as emitBoxDefs: an empty color stays disabled.
	o2 := DefaultIsoPerson()
	o2.ShadowDx, o2.ShadowDy, o2.ShadowBlur = 4, 6, 5
	if strings.Contains(RenderIsoPerson(o2), "person-shadow") {
		t.Errorf("shadow without a color must stay disabled")
	}
}

// TestPersonBackglow — effects.backglow paints a silhouette-shaped halo
// beneath the wrapper so part opacity never dims it.
func TestPersonBackglow(t *testing.T) {
	o := DefaultIsoPerson()
	o.BackglowColor = "#FFAA55"
	o.BackglowRadius = 30
	o.BackglowOpacity = 0.5
	svg := RenderIsoPerson(o)

	if !strings.Contains(svg, `<filter id="person-backglow"`) {
		t.Errorf("backglow render missing the blur filter def")
	}
	halo := strings.Index(svg, `data-face="backglow"`)
	wrap := strings.Index(svg, `data-face="wrap"`)
	if halo < 0 {
		t.Fatalf("backglow render missing the halo path")
	}
	if halo > wrap {
		t.Errorf("backglow halo must paint BEFORE the wrapper group (halo@%d wrap@%d)", halo, wrap)
	}
}

// TestPersonNoEffectsByteStable — with none of the new knobs set, the render
// must not grow any effect markup (the existing goldens enforce full byte
// parity; this is the in-package tripwire).
func TestPersonNoEffectsByteStable(t *testing.T) {
	svg := RenderIsoPerson(DefaultIsoPerson())
	for _, banned := range []string{
		"person-shadow", "person-backglow", "person-body-left-grad",
		"person-body-right-grad", `data-face="outline"`, `data-face="backglow"`,
	} {
		if strings.Contains(svg, banned) {
			t.Errorf("effect-free person render leaked %q", banned)
		}
	}
}

// TestPersonSilhouetteGeometry — the sampled silhouette is closed around the
// figure: every point sits on (or inside a tolerance of) the head circle,
// dome ellipse, or ground arc, and the crown actually clears the dome apex.
func TestPersonSilhouetteGeometry(t *testing.T) {
	o := DefaultIsoPerson()
	rx := o.BodyWidth / 2
	ry := rx * sin30 / cos30
	h := o.BodyHeight
	headR := o.HeadRadius
	headCy := -headR + headR*0.20

	sil := personFigureSilhouette(rx, ry, h, headR, headCy, "sphere")
	if len(sil) < 40 {
		t.Fatalf("silhouette too coarse: %d points", len(sil))
	}
	minY := math.Inf(1)
	for _, p := range sil {
		if p[1] < minY {
			minY = p[1]
		}
	}
	wantTop := headCy - headR
	if math.Abs(minY-wantTop) > 0.01 {
		t.Errorf("crown top = %.2f, want head top %.2f — silhouette must wrap the head", minY, wantTop)
	}

	// Every sample lies on one of the three generating curves.
	onCurve := func(x, y float64) bool {
		if d := math.Hypot(x, y-headCy); math.Abs(d-headR) < 1e-6 {
			return true
		}
		if e := x*x/(rx*rx) + (y-h)*(y-h)/(h*h); math.Abs(e-1) < 1e-6 {
			return true
		}
		if e := x*x/(rx*rx) + (y-h)*(y-h)/(ry*ry); y >= h && math.Abs(e-1) < 1e-6 {
			return true
		}
		return false
	}
	for i, p := range sil {
		if !onCurve(p[0], p[1]) {
			t.Fatalf("silhouette point %d (%.3f, %.3f) is on none of head/dome/ground curves", i, p[0], p[1])
		}
	}

	// Block head style keeps the body-only fallback (no crown above the apex).
	blk := personFigureSilhouette(rx, ry, h, headR, headCy, "block")
	for _, p := range blk {
		if p[1] < -0.01 {
			t.Fatalf("block-head silhouette rose above the apex: (%.2f, %.2f)", p[0], p[1])
		}
	}
}
