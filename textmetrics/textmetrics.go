// Package textmetrics is the repo's single source of truth for
// font-engine-free text width estimation.
//
// Every renderer/validator that needs "roughly how wide will this
// string paint?" historically grew its own empirical factor. Those
// factors are frozen here as named constants — one per call site —
// because each was tuned by eye against that site's font size, weight
// and padding, and changing any one of them shifts frozen golden SVGs.
//
// The rune model is deliberately tiny and deterministic:
//
//   - a WIDE rune (CJK and other full-width scripts) advances one full
//     em (WideEm);
//   - every other rune advances the caller's narrow factor (em units).
//
// Width is a function of RUNES, never of bytes: CJK is 3 bytes per
// glyph in UTF-8, so byte counting inflates Chinese labels ~3×.
//
// This package sits below both the root isotopo package and iso25d so
// geometry code and validators share one metric.
package textmetrics

// WideEm is the advance of a wide (full-width) rune, in em units.
const WideEm = 1.0

// Per-call-site narrow-rune advance factors (em units at font size 1).
// The values are load-bearing: they reproduce, bit-for-bit, the
// empirical constants each site shipped with. Do not "harmonize" them
// without regenerating goldens.
const (
	// NarrowTopFace — iso25d top-face icon+label block fitting
	// (top_content.go wrap/shrink decisions).
	NarrowTopFace = 0.58

	// NarrowIsoText — the standalone iso_text shape's bbox estimate
	// (iso25d/shape_text.go).
	NarrowIsoText = 0.55

	// NarrowPartLabel — occlusion's label footprint/ride heuristics
	// (occlusion.go).
	NarrowPartLabel = 0.62

	// NarrowConnectorLabel — connector label chip sizing (inject.go).
	NarrowConnectorLabel = 0.64

	// NarrowScreenLabel — screen-space part label boxes (inject.go).
	NarrowScreenLabel = 0.58

	// NarrowPersonLabel — the person shape's bottom label bbox
	// (iso25d/shape_person.go).
	NarrowPersonLabel = 0.6

	// NarrowAnnotation — annotation callout boxes (inject.go).
	NarrowAnnotation = 0.58

	// BoxDrawingEm — box-drawing rules (U+2500–U+257F) inside
	// annotation text: wider than latin, narrower than CJK.
	BoxDrawingEm = 0.7
)

// IsWide reports whether r paints as a full-width glyph.
//
// The core rule is the repo-wide "≥ U+2E80" cutoff (start of the CJK
// Radicals Supplement; covers CJK ideographs, kana, Hangul syllables,
// full-width forms and full-width punctuation). Hangul Jamo
// (U+1100–U+11FF) sit below the cutoff but render full-width, so they
// are included explicitly.
func IsWide(r rune) bool {
	if r >= 0x2E80 {
		return true
	}
	return r >= 0x1100 && r <= 0x11FF // Hangul Jamo
}

// RuneEm is the estimated advance of a single rune in em units: WideEm
// for wide runes, otherwise the caller's narrow factor.
func RuneEm(r rune, narrow float64) float64 {
	if IsWide(r) {
		return WideEm
	}
	return narrow
}

// Count returns how many narrow and how many wide runes s contains.
func Count(s string) (narrow, wide int) {
	for _, r := range s {
		if IsWide(r) {
			wide++
		} else {
			narrow++
		}
	}
	return narrow, wide
}

// TextPx estimates the painted width of s at fontSize, with narrow
// runes advancing narrow em and wide runes WideEm.
//
// The float expression is ordered float64(count)*fontSize*factor on
// purpose: for pure-narrow strings it is bit-identical to the legacy
// per-site formula float64(len([]rune(s)))*fontSize*factor, keeping
// frozen goldens byte-stable.
func TextPx(s string, fontSize, narrow float64) float64 {
	nn, nw := Count(s)
	return float64(nn)*fontSize*narrow + float64(nw)*fontSize*WideEm
}

// TextEm is TextPx at font size 1 — the width of s in em units.
func TextEm(s string, narrow float64) float64 {
	return TextPx(s, 1, narrow)
}
