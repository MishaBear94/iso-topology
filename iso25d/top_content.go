// Top-face content fitting — the adaptive layout contract for icons
// and labels: an icon must NEVER overflow the top face, and a label
// must never overflow either — long text auto-wraps to the face width
// and, when wrapping alone can't save it, the font auto-shrinks (and
// finally the icon yields height). Pure functions, unit-tested in
// top_content_test.go.
package iso25d

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

const (
	minAutoFontSize = 5.0
	iconLabelGap    = 4.0
	lineHeightK     = 1.2
)

// topFit is the resolved layout for one top face's icon + label block.
type topFit struct {
	// Adjusted reports whether the adaptive path changed anything; when
	// false callers must reproduce the legacy geometry verbatim so
	// already-fitting content renders byte-identically.
	Adjusted bool

	Lines    []string
	FontSize float64
	IconSize float64

	// Block geometry (only meaningful when Adjusted): top-left-origin
	// face coords. IconTop is the icon's top edge; FirstLineY is the
	// vertical CENTER of the first text line.
	IconTop    float64
	FirstLineY float64
}

// runeWidth estimates a rune's advance at fontSize 1.0 — CJK and other
// wide scripts count as a full em, everything else as the top-face
// narrow factor. The model lives in the shared textmetrics package.
func runeWidth(r rune) float64 {
	return textmetrics.RuneEm(r, textmetrics.NarrowTopFace)
}

func lineWidth(line string, fs float64) float64 {
	w := 0.0
	for _, r := range line {
		w += runeWidth(r)
	}
	return w * fs
}

// wrapLine greedily wraps one logical line to maxW at fs. Splits on
// spaces when possible; a single over-long word (or spaceless CJK run)
// falls back to rune-level wrapping. Never returns an empty slice.
func wrapLine(line string, fs, maxW float64) []string {
	if lineWidth(line, fs) <= maxW {
		return []string{line}
	}
	var out []string
	var cur strings.Builder
	curW := 0.0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
	}
	for _, word := range strings.Split(line, " ") {
		wW := lineWidth(word, fs)
		spaceW := 0.0
		if cur.Len() > 0 {
			spaceW = 0.58 * fs
		}
		switch {
		case curW+spaceW+wW <= maxW:
			if cur.Len() > 0 {
				cur.WriteByte(' ')
				curW += spaceW
			}
			cur.WriteString(word)
			curW += wW
		case wW <= maxW:
			flush()
			cur.WriteString(word)
			curW = wW
		default:
			// Rune-level fallback for one over-long word.
			flush()
			for _, r := range word {
				rW := runeWidth(r) * fs
				if curW+rW > maxW {
					flush()
				}
				cur.WriteRune(r)
				curW += rW
			}
			flush()
		}
	}
	flush()
	if len(out) == 0 {
		return []string{line}
	}
	// v3.0 widow control: rune-level (CJK) wrapping can strand a single
	// character on the last line (实时动态信息 / 流). Pull one rune down
	// from the previous line when doing so still fits the width budget.
	if n := len(out); n >= 2 {
		last := []rune(out[n-1])
		prev := []rune(out[n-2])
		if len(last) == 1 && len(prev) >= 3 {
			cand := string(prev[len(prev)-1]) + string(last)
			if lineWidth(cand, fs) <= maxW {
				out[n-2] = string(prev[:len(prev)-1])
				out[n-1] = cand
			}
		}
	}
	return out
}

// longestUnbreakable returns the widest unit that word-boundary
// wrapping cannot split: a whole ASCII word, or a single rune for CJK
// runs (rune-level breaking is typographically fine there).
func longestUnbreakable(lines []string, fs float64) float64 {
	w := 0.0
	for _, line := range lines {
		for _, word := range strings.Split(line, " ") {
			cjk := false
			for _, r := range word {
				if r >= 0x2E80 {
					cjk = true
					break
				}
			}
			if cjk {
				w = math.Max(w, 1.0*fs)
			} else {
				w = math.Max(w, lineWidth(word, fs))
			}
		}
	}
	return w
}

func wrapAll(lines []string, fs, maxW float64) []string {
	var out []string
	for _, l := range lines {
		out = append(out, wrapLine(l, fs, maxW)...)
	}
	return out
}

func widestLine(lines []string, fs float64) float64 {
	w := 0.0
	for _, l := range lines {
		w = math.Max(w, lineWidth(l, fs))
	}
	return w
}

// fitTopContent decides whether the legacy geometry already keeps the
// content inside the face (Adjusted=false → caller renders the old
// way, byte-identical), and otherwise wraps text, shrinks the font,
// and finally shrinks the icon until the block fits with padding.
//
// hasIcon/iconScale describe the icon; lines is the label split on
// explicit "\n" (empty slice = no label).
func fitTopContent(faceW, faceD float64, lines []string, hasIcon bool, iconScale, fontSize float64) topFit {
	if iconScale <= 0 {
		iconScale = 0.4
	}
	iconSize := 0.0
	if hasIcon {
		iconSize = math.Min(faceW, faceD) * iconScale
	}
	hasLabel := len(lines) > 0
	fs := fontSize

	pad := math.Max(6, 0.07*math.Min(faceW, faceD))
	availW := faceW - 2*pad
	availH := faceD - 2*pad

	// ── legacy-geometry fit check (no wrapping, original lines) ──
	fitsLegacy := func() bool {
		if hasIcon && (iconSize > availW || iconSize > availH) {
			return false
		}
		if !hasLabel {
			return true
		}
		if widestLine(lines, fs) > availW {
			return false
		}
		n := float64(len(lines))
		blockLift := 0.0
		if hasIcon {
			blockLift = (fs*lineHeightK*n + iconLabelGap) / 2
		}
		var top, bottom float64
		lineH := fs * lineHeightK
		if hasIcon {
			top = faceD*0.5 - iconSize*0.5 - blockLift
			lyCenter := faceD*0.5 + iconSize*0.5 + fs*0.7 + 3 - blockLift
			bottom = lyCenter + (n-1)*lineH/2 + fs*0.6
		} else {
			top = faceD*0.5 - (n-1)*lineH/2 - fs*0.6
			bottom = faceD*0.5 + (n-1)*lineH/2 + fs*0.6
		}
		return top >= pad && bottom <= faceD-pad
	}
	if fitsLegacy() {
		return topFit{Adjusted: false, Lines: lines, FontSize: fs, IconSize: iconSize}
	}

	// ── adaptive path: wrap → shrink font → shrink icon ──
	if hasIcon {
		iconSize = math.Min(iconSize, math.Min(availW, availH))
	}
	var wrapped []string
	blockH := func() float64 {
		h := float64(len(wrapped)) * fs * lineHeightK
		if hasIcon {
			h += iconSize
			if hasLabel {
				h += iconLabelGap
			}
		}
		return h
	}
	for i := 0; i < 24; i++ {
		// Shrink BEFORE wrapping while an unbreakable unit (a whole
		// ASCII word) can't fit the width — breaking a word mid-rune
		// is the last resort, not the first. Width scales linearly
		// with the font, so solve for the exact size in one step.
		if hasLabel && fs > minAutoFontSize {
			// 0.01 tolerance + 0.999 safety factor keep the exact
			// solve from oscillating on float round-trip.
			if unit := longestUnbreakable(lines, 1.0); unit*fs > availW+0.01 {
				fs = math.Max(minAutoFontSize, availW/unit*0.999)
				continue
			}
		}
		wrapped = nil
		if hasLabel {
			wrapped = wrapAll(lines, fs, availW)
		}
		if widestLine(wrapped, fs) <= availW && blockH() <= availH {
			break
		}
		if fs > minAutoFontSize {
			fs = math.Max(minAutoFontSize, fs*0.9)
			continue
		}
		// Font at floor: the icon yields the remaining height.
		if hasIcon {
			textH := float64(len(wrapped))*fs*lineHeightK + iconLabelGap
			iconSize = math.Max(8, availH-textH)
		}
		break
	}

	// Centre the block vertically inside the padded face.
	bh := blockH()
	top := pad + math.Max(0, (availH-bh)/2)
	fit := topFit{Adjusted: true, Lines: wrapped, FontSize: fs, IconSize: iconSize}
	if hasIcon {
		fit.IconTop = top
		fit.FirstLineY = top + iconSize + iconLabelGap + fs*0.6
	} else {
		fit.FirstLineY = top + fs*0.6
	}
	return fit
}
