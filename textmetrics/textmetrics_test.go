package textmetrics

import "testing"

// almostEq — the width model is pure float math; assert to a tight
// epsilon so refactors that change summation order are caught.
func almostEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestIsWide(t *testing.T) {
	wide := []rune{
		'中', '文', '标',           // CJK ideographs
		'あ', 'カ',                // kana
		'한',                      // Hangul syllable (≥ U+2E80)
		'ᄀ', 'ᇿ',        // Hangul Jamo (explicit range below the cutoff)
		'⺀',                  // exact cutoff
		'，', '。', '：', '！',     // full-width punctuation (U+FF0C, U+3002, …)
		'　',                      // ideographic space U+3000
		'Ａ',                      // full-width latin U+FF21
	}
	for _, r := range wide {
		if !IsWide(r) {
			t.Errorf("IsWide(%q U+%04X) = false, want true", r, r)
		}
	}
	narrow := []rune{
		'A', 'z', '0', ' ', '.', '-',
		'—',      // em-dash U+2014 — below the cutoff, kept narrow (frozen goldens depend on it)
		'·',      // middle dot U+00B7
		'é', 'ß', // accented latin
		'│',      // box-drawing U+2502 — special-cased by callers, not "wide"
		'ჿ', // just below the Jamo range
		'ሀ', // just above the Jamo range
		'⹿', // just below the cutoff
	}
	for _, r := range narrow {
		if IsWide(r) {
			t.Errorf("IsWide(%q U+%04X) = true, want false", r, r)
		}
	}
}

func TestTextPx_PureASCII(t *testing.T) {
	// Must be BIT-identical to the legacy per-site formula
	// float64(len([]rune(s)))*fontSize*factor — goldens are frozen
	// byte-for-byte on this expression.
	s := "Cache Cluster"
	for _, factor := range []float64{NarrowTopFace, NarrowIsoText, NarrowPartLabel, NarrowConnectorLabel, NarrowPersonLabel} {
		legacy := float64(len([]rune(s))) * 13 * factor
		if got := TextPx(s, 13, factor); got != legacy {
			t.Errorf("TextPx(%q, 13, %v) = %v, want bit-exact legacy %v", s, factor, got, legacy)
		}
	}
}

func TestTextPx_PureCJK(t *testing.T) {
	// 4 ideographs, 12 UTF-8 bytes. Width must be rune-based: 4 em.
	s := "消息队列"
	if got, want := TextPx(s, 20, NarrowScreenLabel), 4*20*WideEm; !almostEq(got, want) {
		t.Errorf("TextPx(%q) = %v, want %v", s, got, want)
	}
	// Regression: the byte-count bug made this 12*20*0.58 = 139.2.
	if got := TextPx(s, 20, NarrowScreenLabel); almostEq(got, float64(len(s))*20*NarrowScreenLabel) {
		t.Errorf("TextPx(%q) = %v — still counting bytes", s, got)
	}
}

func TestTextPx_Mixed(t *testing.T) {
	// "API 网关" → narrow: 'A','P','I',' ' (4) ; wide: '网','关' (2)
	s := "API 网关"
	want := 4*10*NarrowScreenLabel + 2*10*WideEm
	if got := TextPx(s, 10, NarrowScreenLabel); !almostEq(got, want) {
		t.Errorf("TextPx(%q) = %v, want %v", s, got, want)
	}
	nn, nw := Count(s)
	if nn != 4 || nw != 2 {
		t.Errorf("Count(%q) = (%d, %d), want (4, 2)", s, nn, nw)
	}
}

func TestTextPx_FullWidthPunctuation(t *testing.T) {
	// "你好，世界！" — 6 runes, all full-width (incl. ，U+FF0C and ！U+FF01).
	s := "你好，世界！"
	if got, want := TextPx(s, 11, NarrowPartLabel), 6*11*WideEm; !almostEq(got, want) {
		t.Errorf("TextPx(%q) = %v, want %v", s, got, want)
	}
	nn, nw := Count(s)
	if nn != 0 || nw != 6 {
		t.Errorf("Count(%q) = (%d, %d), want (0, 6)", s, nn, nw)
	}
}

func TestTextEm(t *testing.T) {
	if got, want := TextEm("ab中", 0.5), 2*0.5+1.0; !almostEq(got, want) {
		t.Errorf("TextEm = %v, want %v", got, want)
	}
}

func TestRuneEm(t *testing.T) {
	if got := RuneEm('中', 0.58); got != WideEm {
		t.Errorf("RuneEm('中') = %v, want %v", got, WideEm)
	}
	if got := RuneEm('a', 0.58); got != 0.58 {
		t.Errorf("RuneEm('a') = %v, want 0.58", got)
	}
}
