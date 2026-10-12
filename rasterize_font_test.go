package isotopo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/MarkovWangRR/iso-topology/textmetrics"
)

func TestBoundFontRasterResource(t *testing.T) {
	bin, err := exec.LookPath("resvg")
	if err != nil {
		t.Skip("resvg not installed")
	}
	f := textmetrics.ResolveBuiltin("Source Sans Pro", "600")
	dir := t.TempDir()
	svgPath := filepath.Join(dir, "input.svg")
	got := filepath.Join(dir, "got.png")
	want := filepath.Join(dir, "want.png")
	fontPath := filepath.Join(dir, "font.ttf")
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="400" height="80">` + f.CSS() + `<text x="10" y="50" font-family="Source Sans Pro" font-size="28" font-weight="600">iiii WWWW AV office é</text></svg>`
	for path, data := range map[string][]byte{svgPath: []byte(svg), fontPath: f.Bytes()} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := rasterizeResvg(bin, svgPath, got); err != nil {
		t.Fatal(err)
	}
	if data, err := exec.Command(bin, "--skip-system-fonts", "--use-font-file", fontPath, svgPath, want).CombinedOutput(); err != nil {
		t.Fatalf("reference render: %v %s", err, data)
	}
	a, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("rasterizer font differs from the embedded resource")
	}
}
