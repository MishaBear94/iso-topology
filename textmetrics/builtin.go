package textmetrics

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"sync"
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"oss.terrastruct.com/d2/d2renderers/d2fonts"
)

// BuiltinFont binds shaping and SVG embedding to an immutable font resource.
// The face and shaper have mutable caches; access is serialized per weight.
// Source Sans Pro is opt-in; arbitrary system font families stay estimated.
type BuiltinFont struct {
	weight, hash string
	data         []byte
	css          string
	mu           sync.Mutex
	face         *font.Face
	shaper       shaping.HarfbuzzShaper
	segmenter    shaping.Segmenter
	cache        map[string]GlyphMetrics
}

// GlyphMetrics uses em units and an SVG (downward-positive) baseline origin.
// Ink excludes spaces; Advance is the typographic cursor displacement.
// Known is false when any character needs an unbound font/script fallback.
type GlyphMetrics struct {
	Advance, Left, Top, Right, Bottom float64
	Known, HasInk                     bool
}

// CenteredWidth reserves both the advance and any ink extending outside it.
func (m GlyphMetrics) CenteredWidth() float64 {
	return math.Max(m.Advance, 2*math.Max(m.Advance/2-m.Left, m.Right-m.Advance/2))
}

var builtinOnce sync.Once
var builtinFonts []*BuiltinFont

func initBuiltinFonts() {
	d2fonts.FontFamiliesMu.Lock()
	defer d2fonts.FontFamiliesMu.Unlock()
	for i, style := range []d2fonts.FontStyle{d2fonts.FONT_STYLE_REGULAR, d2fonts.FONT_STYLE_SEMIBOLD, d2fonts.FONT_STYLE_BOLD} {
		data := bytes.Clone(d2fonts.FontFaces.Get(d2fonts.Font{Family: d2fonts.SourceSansPro, Style: style}))
		face, err := font.ParseTTF(bytes.NewReader(data))
		if err != nil {
			continue
		} // Missing resources never become zero-width text.
		f := &BuiltinFont{weight: []string{"400", "600", "700"}[i], hash: fmt.Sprintf("%x", sha256.Sum256(data)), data: data, face: face, cache: make(map[string]GlyphMetrics)}
		f.css = `<style data-isotopo-font="` + f.hash + `">@font-face{font-family:'Source Sans Pro';font-style:normal;font-weight:` + f.weight + `;src:url(data:font/ttf;base64,` + base64.StdEncoding.EncodeToString(data) + `) format('truetype')}</style>`
		builtinFonts = append(builtinFonts, f)
	}
}

// ResolveBuiltin recognizes the explicit family and supported static weights.
// It intentionally does not guess at CSS fallback lists or synthetic weights.
func ResolveBuiltin(family, weight string) *BuiltinFont {
	if strings.Trim(strings.TrimSpace(family), `"'`) != "Source Sans Pro" {
		return nil
	}
	switch strings.TrimSpace(weight) {
	case "normal":
		weight = "400"
	case "bold":
		weight = "700"
	case "":
		weight = "600"
	}
	builtinOnce.Do(initBuiltinFonts)
	for _, f := range builtinFonts {
		if f.weight == weight {
			return f
		}
	}
	return nil
}
func (f *BuiltinFont) Weight() string { return f.weight }
func (f *BuiltinFont) Hash() string   { return f.hash }
func (f *BuiltinFont) CSS() string    { return f.css }
func (f *BuiltinFont) Bytes() []byte  { return bytes.Clone(f.data) }

type singleFace struct{ face *font.Face }

func (f singleFace) ResolveFace(r rune) *font.Face { return f.face }

// Measure shapes complete runs, including kerning, ligatures and combining
// marks. The first rollout is horizontal Latin/Greek/Cyrillic and common
// punctuation. Bidi controls, missing glyphs and other scripts are explicit
// unknowns, not tofu metrics. Whitespace must already match SVG semantics.
func (f *BuiltinFont) Measure(text string) GlyphMetrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.cache[text]; ok {
		return m
	}
	m := f.measure(text)
	// Bound retained label memory even for long-lived Studio/WASM sessions.
	if len(text) <= 2048 {
		if len(f.cache) >= 256 {
			clear(f.cache)
		}
		f.cache[text] = m
	}
	return m
}
func (f *BuiltinFont) measure(text string) GlyphMetrics {
	if text == "" {
		return GlyphMetrics{Known: true}
	}
	for _, r := range text {
		s := language.LookupScript(r)
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || (s != language.Latin && s != language.Greek && s != language.Cyrillic && s != language.Common && s != language.Inherited) {
			return GlyphMetrics{}
		}
		if _, ok := f.face.NominalGlyph(r); !ok {
			return GlyphMetrics{}
		}
	}
	input := shaping.Input{Text: []rune(text), Direction: di.DirectionLTR, Size: fixed.I(1000), Language: language.NewLanguage("en")}
	input.RunEnd = len(input.Text)
	m := GlyphMetrics{Known: true}
	cv := func(v fixed.Int26_6) float64 { return float64(v) / 64000 }
	for _, run := range f.segmenter.Split(input, singleFace{f.face}) {
		out := f.shaper.Shape(run)
		for _, g := range out.Glyphs {
			if g.GlyphID == 0 {
				return GlyphMetrics{}
			}
			if g.Width != 0 && g.Height != 0 {
				left := m.Advance + cv(g.XOffset+g.XBearing)
				top := -cv(g.YOffset + g.YBearing)
				right, bottom := left+cv(g.Width), top-cv(g.Height)
				if !m.HasInk {
					m.Left, m.Top, m.Right, m.Bottom = left, top, right, bottom
					m.HasInk = true
				} else {
					m.Left = math.Min(m.Left, left)
					m.Top = math.Min(m.Top, top)
					m.Right = math.Max(m.Right, right)
					m.Bottom = math.Max(m.Bottom, bottom)
				}
			}
			m.Advance += cv(g.Advance)
		}
	}
	return m
}

// FontsInSVG only recognizes our exact embedded resource, never arbitrary
// uploaded data URLs. Rasterizers without CSS font loading use these bytes.
func FontsInSVG(svg string) []*BuiltinFont {
	if !strings.Contains(svg, `data-isotopo-font="`) {
		return nil
	}
	builtinOnce.Do(initBuiltinFonts)
	var out []*BuiltinFont
	for _, f := range builtinFonts {
		if strings.Contains(svg, f.css) {
			out = append(out, f)
		}
	}
	return out
}

// DeduplicateFonts keeps a single definition per immutable font resource.
// Other styles and author content are left intact.
func DeduplicateFonts(svg string) string {
	for _, f := range FontsInSVG(svg) {
		i := strings.Index(svg, f.css) + len(f.css)
		if !strings.Contains(svg[i:], f.css) {
			continue
		}
		svg = svg[:i] + strings.ReplaceAll(svg[i:], f.css, "")
	}
	return svg
}
