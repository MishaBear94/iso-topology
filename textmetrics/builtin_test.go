package textmetrics

import (
	"math"
	"strings"
	"sync"
	"testing"
)

func TestBoundFontShaping(t *testing.T) {
	for _, weight := range []string{"400", "600", "700"} {
		t.Run(weight, func(t *testing.T) {
			f := ResolveBuiltin("Source Sans Pro", weight)
			if f != nil && f.Measure("") != (GlyphMetrics{Known: true}) {
				t.Fatal("empty line metrics")
			}
			if f == nil {
				t.Fatal("font resource unavailable")
			}
			for _, s := range []string{"iiii", "WWWW", "AV", "office", "e\u0301", "Ágj", "Αθήνα", "Москва", "A  B"} {
				m := f.Measure(s)
				if !m.Known || !m.HasInk || m.Advance <= 0 || m.Top >= m.Bottom {
					t.Fatalf("%q: %+v", s, m)
				}
			}
			if f.Measure("iiii").Advance >= f.Measure("WWWW").Advance/2 {
				t.Fatal("proportional glyph advances lost")
			}
			if f.Measure("AV").Advance >= f.Measure("A").Advance+f.Measure("V").Advance {
				t.Fatal("kerning lost")
			}
			if f.Measure("e\u0301").Advance >= f.Measure("e").Advance*1.1 {
				t.Fatal("combining mark treated as an independent letter")
			}
			for _, s := range []string{"中文", "👨‍👩‍👧‍👦", "مرحبا", "क्ष", "A\tB", "A\u202eB", "A\u200dB"} {
				if f.Measure(s).Known {
					t.Fatalf("unbound fallback %q was certified", s)
				}
			}
			copy := f.Bytes()
			copy[0] ^= 255
			if copy[0] == f.Bytes()[0] {
				t.Fatal("mutable font resource escaped")
			}
			combined := f.CSS() + "<g/>" + f.CSS()
			if got := DeduplicateFonts(combined); got != f.CSS()+"<g/>" {
				t.Fatal("font definitions not deduplicated")
			}
			if len(FontsInSVG(combined)) != 1 || len(FontsInSVG(strings.Replace(f.CSS(), "base64,", "base64,garbage", 1))) != 0 {
				t.Fatal("unverified resource accepted")
			}
		})
	}
	for _, family := range []string{"Arial", "Helvetica Neue, Arial, sans-serif", "Source Sans Pro, sans-serif"} {
		if ResolveBuiltin(family, "600") != nil {
			t.Fatal("system font was bound")
		}
	}
	if ResolveBuiltin("Source Sans Pro", "500") != nil {
		t.Fatal("synthetic weight was bound")
	}
}

func TestBoundFontConcurrentMeasurements(t *testing.T) {
	f := ResolveBuiltin("Source Sans Pro", "600")
	want := f.Measure("AV office e\u0301")
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if got := f.Measure("AV office e\u0301"); got != want {
					t.Error("nondeterministic shaping")
				}
			}
		}()
	}
	wg.Wait()
}

// Independent font-table/Chromium reference: U, Udieresis and uni01D7 share
// the same pair-positioning class. typesetting v0.3.5 dropped later coverage
// ranges from its digest and silently skipped the accent variants' kerning.
func TestBoundFontExtendedLatinKerning(t *testing.T) {
	f := ResolveBuiltin("Source Sans Pro", "600")
	for _, s := range []string{"Ug", "Üg", "U\u0308g", "Ǘg", "U\u0308\u0301g"} {
		m := f.Measure(s)
		if !m.Known || math.Abs(m.Advance-1.165) > 1e-8 {
			t.Fatalf("extended coverage kerning lost for %q: %+v", s, m)
		}
	}
	m := f.Measure("Ǘgj")
	if math.Abs(m.Advance-1.47) > 1e-8 || math.Abs(m.Right-1.408) > 1e-8 {
		t.Fatalf("accent ink shifted by missed kerning: %+v", m)
	}
}
