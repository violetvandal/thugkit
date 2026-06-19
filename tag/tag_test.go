package tag

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/violetvandal/thugkit/imgxbx"
	"github.com/violetvandal/thugkit/prx"
)

func align4(x int) int { return (x + 3) &^ 3 }

// writeCagpieces makes a synthetic cagpieces.prx holding one slot entry.
func writeCagpieces(t *testing.T, path, entryName string, data []byte) {
	t.Helper()
	nb := []byte(entryName)
	pad := align4(len(data)) - len(data)
	e := &prx.Entry{
		Dsize: uint32(len(data)), Nlen: uint32(align4(len(nb) + 1)),
		Name: nb, Blob: append(append([]byte(nil), data...), make([]byte, pad)...),
	}
	blob, err := prx.Build(prx.Version, []*prx.Entry{e})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	if err := os.WriteFile(path, blob, 0644); err != nil {
		t.Fatal(err)
	}
}

func writePNG(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 64, 255})
		}
	}
	p := filepath.Join(dir, "art.png")
	f, _ := os.Create(p)
	png.Encode(f, img)
	f.Close()
	return p
}

func slotData(t *testing.T, prxPath, suffix string) []byte {
	t.Helper()
	d, err := os.ReadFile(prxPath)
	if err != nil {
		t.Fatal(err)
	}
	_, es, err := prx.Parse(d)
	if err != nil {
		t.Fatal(err)
	}
	e := prx.FindBySuffix(es, suffix)
	if e == nil {
		t.Fatalf("slot %s not found in %s", suffix, prxPath)
	}
	return e.RawData()
}

func TestRunBuildsTag(t *testing.T) {
	root := t.TempDir()
	gamedir := filepath.Join(root, "game")
	cag := filepath.Join(gamedir, "Data", "pre", "cagpieces.prx")
	writeCagpieces(t, cag, "grap_50.img.xbx", []byte("ORIGINAL-SPRITE-PLACEHOLDER"))
	img := writePNG(t, root, 90, 90)
	out := filepath.Join(root, "out")

	res, err := Run(Options{Image: img, GameDir: gamedir, Name: "TestTag", Slot: "grap_50", Size: 64, OutDir: out})
	if err != nil {
		t.Fatal(err)
	}

	// 1) out files exist
	if _, err := os.Stat(res.GRF); err != nil {
		t.Fatalf("GRF missing: %v", err)
	}
	if filepath.Base(res.GRF) != "TestTag.GRF" {
		t.Errorf("GRF name = %s", filepath.Base(res.GRF))
	}

	// 2) the patched cagpieces slot now holds exactly our encoded sprite
	want, err := imgxbx.Encode(img, 64)
	if err != nil {
		t.Fatal(err)
	}
	got := slotData(t, res.PRX, "grap_50.img.xbx")
	if !bytes.Equal(got, want) {
		t.Fatalf("slot blob != encoded image (got %d bytes, want %d)", len(got), len(want))
	}

	// 3) GRF is a valid 0x8000 file with cksum1 = StringToChecksum of the name field
	grfBytes, _ := os.ReadFile(res.GRF)
	if len(grfBytes) != 0x8000 {
		t.Fatalf("GRF size = %d, want 32768", len(grfBytes))
	}
	if got := binary.LittleEndian.Uint32(grfBytes[8:]); got != uint32(len("TestTag")+7) {
		t.Errorf("h08 = %d", got)
	}
}

func TestRunInstall(t *testing.T) {
	root := t.TempDir()
	gamedir := filepath.Join(root, "game")
	cag := filepath.Join(gamedir, "Data", "pre", "cagpieces.prx")
	writeCagpieces(t, cag, "grap_50.img.xbx", []byte("ORIG"))
	os.MkdirAll(filepath.Join(gamedir, "Save"), 0755)
	img := writePNG(t, root, 64, 64)

	res, err := Run(Options{Image: img, GameDir: gamedir, Name: "Inst", Size: 64,
		OutDir: filepath.Join(root, "out"), Install: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.InstalledPRX == "" || res.InstalledGRF == "" {
		t.Fatalf("install did not report paths: %+v", res)
	}
	// .orig backup of the original cagpieces was made
	if _, err := os.Stat(cag + ".orig"); err != nil {
		t.Errorf("no .orig backup of cagpieces: %v", err)
	}
	// installed GRF landed in Save/
	if _, err := os.Stat(filepath.Join(gamedir, "Save", "Inst.GRF")); err != nil {
		t.Errorf("installed GRF missing: %v", err)
	}
	// installed cagpieces equals our output
	a, _ := os.ReadFile(res.PRX)
	b, _ := os.ReadFile(cag)
	if !bytes.Equal(a, b) {
		t.Error("installed cagpieces != produced cagpieces")
	}
}

func TestDefaultName(t *testing.T) {
	cases := map[string]string{
		"/x/My Cool Art.png":           "My Cool Art",
		"/x/weird@#$name!.jpg":         "weirdname",
		"/x/way_too_long_tag_name.png": "waytoolongtagna", // _ removed, then capped to 15
		"/x/!!!.png":                   "CustomTag",
	}
	for in, want := range cases {
		if got := defaultName(in); got != want {
			t.Errorf("defaultName(%q) = %q, want %q", in, got, want)
		}
	}
}
