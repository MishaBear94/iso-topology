// Text width estimation — the root package's facade over the shared
// textmetrics engine.
//
// All rune-aware width math lives in the textmetrics subpackage so the
// iso25d geometry package (which the root package imports, and which
// therefore cannot import the root package back) shares the exact same
// metric. Root-package call sites go through the helpers below; the
// per-site empirical factors are the named constants in
// textmetrics/textmetrics.go.
package isotopo

import (
	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

// textWidthPx estimates the painted width of s at fontSize: wide (CJK
// etc.) runes advance one em, everything else advances narrow em.
// Rune-aware — never counts bytes (CJK is 3 bytes/glyph in UTF-8, so
// byte counting inflates Chinese labels ~3×).
func textWidthPx(s string, fontSize, narrow float64) float64 {
	return textmetrics.TextPx(s, fontSize, narrow)
}

// Validator label-width model (validate_visual.go): the legacy check
// assumed 7px per BYTE (len(label)*7) at the default label font. That
// per-byte advance is kept for narrow runes — so ASCII and multi-byte
// narrow glyphs (em-dash, middle dot, accents) estimate exactly as
// before — while wide (CJK etc.) runes get the full-em equivalent
// (≈7/0.58, rounded) instead of 3 bytes · 7 = 21px.
const (
	validateNarrowPx = 7
	validateWidePx   = 12
)

// estimateLabelPx is the validator's truncation-risk width estimate
// for a part label, in screen px.
func estimateLabelPx(label string) int {
	narrowBytes, wide := textmetrics.CountWideBytes(label)
	return narrowBytes*validateNarrowPx + wide*validateWidePx
}
