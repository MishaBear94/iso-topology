// Top-face content fitting uses estimated text widths to wrap labels,
// shrink fonts and yield icon space. Fixed face dimensions can still be
// infeasible at the font floor; preserving text takes priority over fitting.
package iso25d

import (
	"math"
	"strings"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
	"github.com/rivo/uniseg"
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

type textWidth func(string, float64) float64

func lineWidth(line string, fs float64) float64 {
	w := 0.0
	for _, r := range line {
		w += runeWidth(r)
	}
	return w * fs
}

// textClusters preserves user-perceived characters during emergency wrapping.
// Cluster segmentation is not font measurement: retain the existing advance
// estimate instead of treating every cluster as one glyph or one em.
func textClusters(s string) []string {
	var out []string
	state := -1
	for s != "" {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		out = append(out, cluster)
	}
	return out
}

// wrapWords retains the existing ASCII-space word policy, but a space with
// attached combining marks is part of its cluster, not a removable separator.
func wrapWords(line string) []string {
	if !strings.Contains(line, " ") {
		return []string{line}
	}
	var words []string
	start, offset := 0, 0
	rest, state := line, -1
	for rest != "" {
		var cluster string
		cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
		if cluster == " " {
			words = append(words, line[start:offset])
			start = offset + 1
		}
		offset += len(cluster)
	}
	return append(words, line[start:])
}

// wrapLine greedily wraps one logical line to maxW at fs. Splits on spaces
// when possible; an over-long word falls back to grapheme clusters. A single
// cluster wider than maxW remains intact, even when it cannot fit. Never
// returns an empty slice. This is not a full Unicode line-breaking algorithm.
func wrapLine(line string, fs, maxW float64, metrics ...textWidth) []string {
	lineWidth := lineWidth
	if len(metrics) > 0 && metrics[0] != nil {
		lineWidth = metrics[0]
	}
	if lineWidth(line, fs) <= maxW {
		return []string{line}
	}
	var out []string
	var splitWordAfter []bool
	var cur strings.Builder
	curW := 0.0
	flush := func(splitWord bool) {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			splitWordAfter = append(splitWordAfter, splitWord)
			cur.Reset()
			curW = 0
		}
	}
	for _, word := range wrapWords(line) {
		wW := lineWidth(word, fs)
		spaceW := 0.0
		if cur.Len() > 0 {
			spaceW = lineWidth(" ", fs)
		}
		candidateW := curW + spaceW + wW
		if len(metrics) > 0 && metrics[0] != nil {
			candidate := word
			if cur.Len() > 0 {
				candidate = cur.String() + " " + word
			}
			candidateW = lineWidth(candidate, fs)
		}
		switch {
		case candidateW <= maxW:
			if cur.Len() > 0 {
				cur.WriteByte(' ')
				curW += spaceW
			}
			cur.WriteString(word)
			curW = candidateW
		case wW <= maxW:
			flush(false)
			cur.WriteString(word)
			curW = wW
		default:
			// Emergency breaks must not split accents, emoji or joiners.
			flush(false)
			for _, cluster := range textClusters(word) {
				width := lineWidth(cluster, fs)
				nextW := curW + width
				if len(metrics) > 0 && metrics[0] != nil {
					nextW = lineWidth(cur.String()+cluster, fs)
				}
				if nextW > maxW {
					flush(true)
				}
				cur.WriteString(cluster)
				if len(metrics) > 0 && metrics[0] != nil {
					curW = lineWidth(cur.String(), fs)
				} else {
					curW += width
				}
			}
			flush(false)
		}
	}
	flush(false)
	if len(out) == 0 {
		return []string{line}
	}
	// Rebalance a stranded cluster only across an emergency break inside
	// the same word. Crossing a word boundary would turn "abc d" into
	// "ab / cd", changing which letters belong to the author's words.
	if n := len(out); n >= 2 && splitWordAfter[n-2] {
		last := textClusters(out[n-1])
		prev := textClusters(out[n-2])
		if len(last) == 1 && len(prev) >= 3 {
			cand := prev[len(prev)-1] + last[0]
			if lineWidth(cand, fs) <= maxW {
				out[n-2] = strings.Join(prev[:len(prev)-1], "")
				out[n-1] = cand
			}
		}
	}
	return out
}

// longestUnbreakable returns the widest unit that word-boundary
// wrapping prefers not to split: a whole narrow-script word, or a grapheme
// cluster for wide-script runs. Emergency wrapping may split a narrow word
// at the font floor, but it must still preserve each cluster.
func longestUnbreakable(lines []string, fs float64, metrics ...textWidth) float64 {
	lineWidth := lineWidth
	if len(metrics) > 0 && metrics[0] != nil {
		lineWidth = metrics[0]
	}
	w := 0.0
	for _, line := range lines {
		for _, word := range wrapWords(line) {
			cjk := false
			for _, r := range word {
				if r >= 0x2E80 {
					cjk = true
					break
				}
			}
			if cjk {
				for _, cluster := range textClusters(word) {
					w = math.Max(w, lineWidth(cluster, fs))
				}
			} else {
				w = math.Max(w, lineWidth(word, fs))
			}
		}
	}
	return w
}

func wrapAll(lines []string, fs, maxW float64, metrics ...textWidth) []string {
	var out []string
	for _, l := range lines {
		out = append(out, wrapLine(l, fs, maxW, metrics...)...)
	}
	return out
}

func widestLine(lines []string, fs float64, metrics ...textWidth) float64 {
	lineWidth := lineWidth
	if len(metrics) > 0 && metrics[0] != nil {
		lineWidth = metrics[0]
	}
	w := 0.0
	for _, l := range lines {
		w = math.Max(w, lineWidth(l, fs))
	}
	return w
}

// fitTopContent decides whether the legacy geometry already keeps the
// content inside the face (Adjusted=false → caller renders the old
// way, byte-identical), and otherwise wraps text, shrinks the font,
// and finally shrinks the icon toward fitting with padding. An intact cluster
// or a large number of lines can remain too large at the font floor.
//
// hasIcon/iconScale describe the icon; lines is the label split on
// explicit "\n" (empty slice = no label).
func fitTopContent(faceW, faceD float64, lines []string, hasIcon bool, iconScale, fontSize float64, metrics ...textWidth) topFit {
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
		if widestLine(lines, fs, metrics...) > availW {
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
		limit := math.Min(availW, availH)
		if limit <= 0 {
			// The preferred padding itself is infeasible. Retain a visible
			// icon within the face instead of emitting a negative/zero image.
			limit = math.Max(0, math.Min(faceW, faceD))
		}
		iconSize = math.Min(iconSize, limit)
	}
	var wrapped []string
	wrappedFontSize := -1.0
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
	unit := 0.0
	if hasLabel && fs > minAutoFontSize {
		unit = longestUnbreakable(lines, 1.0, metrics...)
	}
	for i := 0; i < 24; i++ {
		// Shrink BEFORE wrapping while an unbreakable unit (a whole
		// ASCII word) can't fit the width — breaking a word internally
		// is the last resort, not the first. Width scales linearly
		// with the font, so solve for the exact size in one step.
		if hasLabel && fs > minAutoFontSize {
			// 0.01 tolerance + 0.999 safety factor keep the exact
			// solve from oscillating on float round-trip.
			if unit > 0 && unit*fs > availW+0.01 {
				fs = math.Max(minAutoFontSize, availW/unit*0.999)
				continue
			}
		}
		wrapped = nil
		if hasLabel {
			wrapped = wrapAll(lines, fs, availW, metrics...)
			wrappedFontSize = fs
		}
		if widestLine(wrapped, fs, metrics...) <= availW && blockH() <= availH {
			break
		}
		if hasLabel && fs > minAutoFontSize {
			fs = math.Max(minAutoFontSize, fs*0.9)
			continue
		}
		// Font at floor: the icon yields the remaining height.
		if hasIcon {
			textH := float64(len(wrapped)) * fs * lineHeightK
			if hasLabel {
				textH += iconLabelGap
			}
			// An icon already smaller than the historical 8-unit floor must
			// never grow in response to a shortage of space.
			iconSize = math.Min(iconSize, math.Max(math.Min(8, iconSize), availH-textH))
		}
		break
	}
	// The iteration budget may end immediately after reducing the font.
	// Resolve the final lines at the font that will actually be emitted.
	if hasLabel && wrappedFontSize != fs {
		wrapped = wrapAll(lines, fs, availW, metrics...)
	}

	// Centre the block vertically inside the padded face.
	bh := blockH()
	top := pad + math.Max(0, (availH-bh)/2)
	if availH <= 0 {
		top = math.Max(0, (faceD-bh)/2)
	}
	fit := topFit{Adjusted: true, Lines: wrapped, FontSize: fs, IconSize: iconSize}
	if hasIcon {
		fit.IconTop = top
		fit.FirstLineY = top + iconSize + iconLabelGap + fs*0.6
	} else {
		fit.FirstLineY = top + fs*0.6
	}
	return fit
}
