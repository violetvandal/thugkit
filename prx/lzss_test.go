package prx

import (
	"bytes"
	"testing"
)

// roundTrip asserts Compress->Decompress reproduces the input exactly.
func roundTrip(t *testing.T, name string, data []byte) {
	t.Helper()
	comp := Compress(data)
	got := Decompress(comp, len(data))
	if !bytes.Equal(got, data) {
		t.Fatalf("%s: round-trip mismatch (in=%d comp=%d out=%d)", name, len(data), len(comp), len(got))
	}
}

func TestLZSSRoundTrip(t *testing.T) {
	rep := bytes.Repeat([]byte("ABCD"), 5000)      // periodic / overlap-heavy
	runs := bytes.Repeat([]byte{0x42}, 9000)       // RLE
	mixed := append(append([]byte("header"), rep...), runs...)
	// pseudo-random (incompressible) without importing math/rand: LCG
	rnd := make([]byte, 8192)
	var s uint32 = 0x12345678
	for i := range rnd {
		s = s*1664525 + 1013904223
		rnd[i] = byte(s >> 16)
	}
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"one", []byte{0x7f}},
		{"two", []byte{1, 2}},
		{"short", []byte("hi")},
		{"min-match", []byte("aaa")},
		{"rle", runs},
		{"periodic", rep},
		{"mixed", mixed},
		{"random", rnd},
		{"text", []byte("the quick brown fox jumps over the lazy dog, the quick brown fox")},
	}
	for _, c := range cases {
		roundTrip(t, c.name, c.data)
	}
}

// TestDecompressLiterals anchors the decoder's literal path against hand-built
// bytes (control byte 0xFF => 8 literals), independent of Compress.
func TestDecompressLiterals(t *testing.T) {
	comp := append([]byte{0xFF}, []byte("ABCDEFGH")...)
	got := Decompress(comp, 8)
	if string(got) != "ABCDEFGH" {
		t.Fatalf("literal decode = %q, want ABCDEFGH", got)
	}
}

// TestDecompressRespectsOutsize: decoder must stop at outsize even with more input.
func TestDecompressRespectsOutsize(t *testing.T) {
	comp := append([]byte{0xFF}, []byte("ABCDEFGH")...)
	got := Decompress(comp, 3)
	if string(got) != "ABC" {
		t.Fatalf("got %q, want ABC", got)
	}
}

// FuzzLZSS: any input must survive a Compress->Decompress round-trip.
func FuzzLZSS(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("aaaaaaaaaa"))
	f.Add([]byte("abcabcabcabcabc"))
	f.Add(bytes.Repeat([]byte{0}, 100))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Round-trip correctness is size-independent (verified to 4MB in timing
		// tests); cap fuzz inputs so the fuzzer explores edge cases fast instead
		// of timing out on large high-entropy blobs (~0.25s/MB). Real inputs
		// (qb_scripts ~1.4MB) are well within budget.
		if len(data) > 64*1024 {
			return
		}
		comp := Compress(data)
		got := Decompress(comp, len(data))
		if !bytes.Equal(got, data) {
			t.Fatalf("round-trip mismatch: in=%d out=%d", len(data), len(got))
		}
	})
}
