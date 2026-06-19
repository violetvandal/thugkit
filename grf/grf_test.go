package grf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestKnownChecksums(t *testing.T) {
	if got := Checksum("layer_infos"); got != 0x9a5ff0a3 {
		t.Fatalf("Checksum(layer_infos)=0x%08x want 0x9a5ff0a3", got)
	}
	if graphicNameField != 0xc3f4169a {
		t.Fatal("graphicNameField constant wrong")
	}
	// case-insensitive
	if Checksum("None") != Checksum("none") {
		t.Fatal("Checksum not case-insensitive")
	}
}

func fullCanvasLayer(slot string, scale float32) Layer {
	return Layer{TextureName: slot, PosX: 32, PosY: 32, Rot: 0, Scale: scale, HSVA: [4]uint32{0, 0, 100, 128}}
}

func TestBuildHeaderAndPadding(t *testing.T) {
	name := "MyTag"
	out, err := Build(name, []Layer{fullCanvasLayer("grap_50", 1.0)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0x8000 {
		t.Fatalf("file size = %d, want 32768", len(out))
	}
	if got := binary.LittleEndian.Uint32(out[8:]); got != uint32(len(name)+7) {
		t.Errorf("h08 = %d, want %d", got, len(name)+7)
	}
	datalen := binary.LittleEndian.Uint32(out[12:])
	if datalen <= 0x14 || datalen > 0x8000 {
		t.Errorf("datalen = %d out of range", datalen)
	}
	if got := binary.LittleEndian.Uint32(out[16:]); got != 1 {
		t.Errorf("h10 = %d, want 1", got)
	}
	if binary.LittleEndian.Uint32(out[4:]) != nameChecksum(name) {
		t.Error("cksum1 mismatch")
	}
	// tail padding is 0x69
	for i := datalen; i < 0x8000; i++ {
		if out[i] != 0x69 {
			t.Fatalf("pad byte at 0x%x = 0x%02x, want 0x69", i, out[i])
			break
		}
	}
}

// TestBuildParityVsPython proves the Go builder is byte-identical to the
// reference grflib.build_grf. Skipped if python3 / grflib.py aren't present.
func TestBuildParityVsPython(t *testing.T) {
	grflibDir := filepath.Join("..", "..", "..", "thug2-tag-importer")
	if _, err := os.Stat(filepath.Join(grflibDir, "grflib.py")); err != nil {
		t.Skip("grflib.py not present; skipping python parity")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	cases := []struct {
		name, slot string
		scale      float32
	}{
		{"MyTag", "grap_50", 1.0},
		{"violetvandal", "grap_50", 1.0},
		{"abc 123", "grap_42", 2.0},
		{"X", "grap_50", 0.5},
	}
	for _, c := range cases {
		py := fmt.Sprintf(
			`import sys;sys.path.insert(0,%q);import grflib;`+
				`l=dict(texture_name=%q,string='',font_id=0,pos_x=32,pos_y=32,rot=0.0,scale=float(%v),flip_h=0,flip_v=0,hsva=[0,0,100,128],layer_id=0);`+
				`sys.stdout.buffer.write(grflib.build_grf(%q,[l]))`,
			grflibDir, c.slot, c.scale, c.name)
		ref, err := exec.Command("python3", "-c", py).Output()
		if err != nil {
			t.Fatalf("python ref failed for %q: %v", c.name, err)
		}
		got, err := Build(c.name, []Layer{fullCanvasLayer(c.slot, c.scale)}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, ref) {
			d := -1
			for i := 0; i < len(got) && i < len(ref); i++ {
				if got[i] != ref[i] {
					d = i
					break
				}
			}
			t.Fatalf("name=%q slot=%q scale=%v: GRF differs from python (len go=%d py=%d, first diff @0x%x)",
				c.name, c.slot, c.scale, len(got), len(ref), d)
		}
	}
}
