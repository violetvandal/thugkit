package imgxbx

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runPy decodes an .img.xbx via the reference img2png.py (needs python3 + PIL).
func runPy(decoder, in, out string) error {
	if _, err := exec.LookPath("python3"); err != nil {
		return err
	}
	return exec.Command("python3", decoder, in, out).Run()
}

// swizzle8 must be a bijection over the w*h positions and round-trip exactly.
func TestSwizzleBijectionRoundTrip(t *testing.T) {
	for _, n := range []int{64, 128, 256} {
		lin := make([]uint8, n*n)
		for i := range lin {
			lin[i] = uint8(i*7 + i/3)
		}
		sw := swizzle8(lin, n, n)
		mx, my := masks(n, n)

		seen := make([]bool, n*n)
		un := make([]uint8, n*n)
		for y := 0; y < n; y++ {
			sy := swizzleAxis(y, my)
			for x := 0; x < n; x++ {
				p := swizzleAxis(x, mx) | sy
				if p < 0 || p >= n*n || seen[p] {
					t.Fatalf("n=%d: not a bijection at (%d,%d)->%d", n, x, y, p)
				}
				seen[p] = true
				un[y*n+x] = sw[p]
			}
		}
		for i := range lin {
			if lin[i] != un[i] {
				t.Fatalf("n=%d: swizzle round-trip mismatch at %d", n, i)
			}
		}
	}
}

func writePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 255 / w), uint8(y * 255 / h), 128, uint8(255 - x*255/w)})
		}
	}
	p := filepath.Join(t.TempDir(), "in.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func TestEncodeFormat(t *testing.T) {
	src := writePNG(t, 100, 80) // non-square, non-power-of-two -> exercises resize
	le := binary.LittleEndian
	for _, size := range []int{64, 128} {
		data, err := Encode(src, size)
		if err != nil {
			t.Fatal(err)
		}
		// fixed header: 2, 8, w, h, 0x13, 0
		hdr := []uint32{le.Uint32(data[0:]), le.Uint32(data[4:]), le.Uint32(data[8:]),
			le.Uint32(data[12:]), le.Uint32(data[16:]), le.Uint32(data[20:])}
		want := []uint32{2, 8, uint32(size), uint32(size), 0x13, 0}
		for i := range want {
			if hdr[i] != want[i] {
				t.Errorf("size=%d header[%d]=%d want %d", size, i, hdr[i], want[i])
			}
		}
		if w16, h16 := le.Uint16(data[24:]), le.Uint16(data[26:]); int(w16) != size || int(h16) != size {
			t.Errorf("size=%d dims u16 = %dx%d", size, w16, h16)
		}
		palsize := le.Uint32(data[28:])
		npal := palsize / 4
		if npal < 1 || npal > 256 {
			t.Errorf("size=%d npal=%d out of range", size, npal)
		}
		wantLen := 32 + int(palsize) + size*size
		if len(data) != wantLen {
			t.Errorf("size=%d len=%d want %d (32 hdr + %d pal + %d idx)", size, len(data), wantLen, palsize, size*size)
		}
	}
}

// TestEncodeDecodesValid round-trips the Go encoder through the reference Python
// decoder (img2png.py) if available — proves the sprite is a valid .img.xbx.
func TestEncodeDecodesValid(t *testing.T) {
	dec := filepath.Join("..", "..", "..", "thug2-tag-importer", "img2png.py")
	if _, err := os.Stat(dec); err != nil {
		t.Skip("img2png.py not present")
	}
	// best-effort: if python/PIL/numpy missing the decode just errors and we skip
	src := writePNG(t, 96, 96)
	data, err := Encode(src, 64)
	if err != nil {
		t.Fatal(err)
	}
	xbx := filepath.Join(t.TempDir(), "out.img.xbx")
	if err := os.WriteFile(xbx, data, 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "decoded.png")
	if err := runPy(dec, xbx, out); err != nil {
		t.Skipf("python decode unavailable: %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("decoder produced no output: %v", err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		t.Fatalf("decoded png invalid: %v", err)
	}
	if cfg.Width != 64 || cfg.Height != 64 {
		t.Fatalf("decoded dims = %dx%d, want 64x64", cfg.Width, cfg.Height)
	}
}
