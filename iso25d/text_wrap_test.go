package iso25d

import (
	"reflect"
	"strings"
	"testing"
)

// These boundaries are explicit fixtures, independent of the segmenter used
// by production. Joining wrapped fragments must recover every original byte.
func TestWrapPreservesTextClusters(t *testing.T) {
	for _, tc := range []struct {
		name     string
		clusters []string
	}{
		{"accent", []string{"a", "e\u0301", "o\u0308\u0304", "b"}},
		{"emoji-joiner", []string{"A", "👩🏽‍💻", "👨‍👩‍👧‍👦", "B"}},
		{"flag", []string{"🇨🇳", "🇺🇳", "🇯🇵"}},
		{"keycap", []string{"1\ufe0f\u20e3", "2\ufe0f\u20e3", "3\ufe0f\u20e3"}},
		{"variation", []string{"✈\ufe0f", "漢\U000e0100", "字"}},
		{"hangul", []string{"\u1100\u1161\u11a8", "\u1102\u1161", "한"}},
		{"vowel-mark", []string{"कि", "มี", "بِ", "a"}},
		{"emoji-tag", []string{"🏴\U000e0067\U000e0062\U000e0065\U000e006e\U000e0067\U000e007f", "X"}},
		{"space-with-accent", []string{"a", " \u0301", "b"}},
		{"plain", []string{"中", "文", "a", "b", "c", "d", "e"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Join(tc.clusters, "")
			boundaries := map[int]bool{0: true}
			offset := 0
			for _, cluster := range tc.clusters {
				offset += len(cluster)
				boundaries[offset] = true
			}
			for _, fs := range []float64{5, 10, 18} {
				for _, width := range []float64{0, 1, 5, 11.6, 20, 30, 60, 120} {
					lines := wrapLine(input, fs, width)
					if strings.Join(lines, "") != input {
						t.Fatalf("fs=%g width=%g changed text: %q", fs, width, lines)
					}
					offset = 0
					for _, line := range lines {
						if line == "" {
							t.Fatalf("unexpected empty fragment: %q", lines)
						}
						offset += len(line)
						if !boundaries[offset] {
							t.Fatalf("fs=%g width=%g split a cluster: %q", fs, width, lines)
						}
						if lineWidth(line, fs) > width+1e-8 {
							atomic := false
							for _, cluster := range tc.clusters {
								atomic = atomic || line == cluster
							}
							if !atomic {
								t.Fatalf("fs=%g width=%g overflow could be split safely: %q", fs, width, line)
							}
						}
					}
				}
			}
		})
	}
}

func TestWrappedWordsPreservedInSVG(t *testing.T) {
	o := ConvertOpts{Width: 30, Depth: 120, Height: 12, Label: "abc d", FontSize: 18}
	var lines []string
	for _, e := range svgContent(t, Convert2DTo25D("rectangle", o)) {
		if e.Kind == "text" {
			lines = append(lines, e.Text)
		}
	}
	if want := []string{"abc", "d"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("SVG changed word identity: got %q, want %q", lines, want)
	}
}

func TestWrapWidowPreservesWordBoundaries(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width float64
		want  []string
	}{
		{"abc d", 18, []string{"abc", "d"}},
		{"one x", 18, []string{"one", "x"}},
		{"中文字符 x", 40, []string{"中文字符", "x"}},
		{"abc\u0301d", 24, []string{"ab", "c\u0301d"}},
		{"实时动态信息流", 30, []string{"实时动", "态信", "息流"}},
	} {
		if got := wrapLine(tc.text, 10, tc.width); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.text, got, tc.want)
		}
	}
}

func TestFitRetainsOversizedCluster(t *testing.T) {
	// The entire cluster is wider than this face even at the font floor.
	// Keep it intact; fixed dimensions cannot always satisfy content fitting.
	text := "👨‍👩‍👧‍👦"
	fit := fitTopContent(22, 120, []string{text}, false, 0, 18)
	if len(fit.Lines) != 1 || fit.Lines[0] != text {
		t.Fatalf("infeasible width broke the label: %+v", fit)
	}
	if fit.FontSize != minAutoFontSize {
		t.Fatalf("font floor changed: %g", fit.FontSize)
	}
}

func TestWrappedClustersMatchEmittedSVG(t *testing.T) {
	const cluster = "👩🏽‍💻"
	label := strings.Repeat(cluster, 3)
	for _, shape := range []string{"rectangle", "cylinder", "hexprism", "array2d", "rack"} {
		t.Run(shape, func(t *testing.T) {
			o := ConvertOpts{Width: 35, Depth: 90, Height: 12, Label: label, FontSize: 18}
			actual := svgContent(t, Convert2DTo25D(shape, o))
			observed := ShapeContent(shape, o).Elements
			var lines []string
			for _, e := range actual {
				if e.Kind == "text" {
					lines = append(lines, e.Text)
					if e.Text == "" || strings.ReplaceAll(e.Text, cluster, "") != "" {
						t.Fatalf("actual SVG split a cluster: %q", e.Text)
					}
				}
			}
			if strings.Join(lines, "") != label || len(lines) < 2 {
				t.Fatalf("expected preserved wrapped label, got %q", lines)
			}
			if len(actual) != len(observed) {
				t.Fatalf("SVG and content observer disagree: %d != %d", len(actual), len(observed))
			}
			for i, e := range actual {
				if e.Text != observed[i].Text {
					t.Fatalf("observer line %d = %q, SVG = %q", i, observed[i].Text, e.Text)
				}
			}
		})
	}
}
