package prx

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// rawEntry builds an uncompressed entry with a correctly-sized name field.
func rawEntry(name string, data []byte) *Entry {
	nb := []byte(name)
	nlen := align4(len(nb) + 1) // null-terminated + 4-byte aligned
	pad := align4(len(data)) - len(data)
	return &Entry{
		Dsize: uint32(len(data)),
		Csize: 0,
		Nlen:  uint32(nlen),
		Crc:   0xDEADBEEF,
		Name:  nb,
		Blob:  append(append([]byte(nil), data...), make([]byte, pad)...),
	}
}

func TestBuildParseRoundTrip(t *testing.T) {
	in := []*Entry{
		rawEntry("a.qb", []byte("WXYZ")),
		rawEntry(`sub\b.qb`, []byte("MN")),     // odd length -> padded
		rawEntry("c.qb", []byte("0123456789")),
	}
	blob, err := Build(Version, in)
	if err != nil {
		t.Fatal(err)
	}
	ver, out, err := Parse(blob)
	if err != nil {
		t.Fatal(err)
	}
	if ver != Version {
		t.Fatalf("ver = 0x%x", ver)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d entries, want %d", len(out), len(in))
	}
	for i := range in {
		if !bytes.Equal(out[i].Name, in[i].Name) || out[i].Dsize != in[i].Dsize {
			t.Errorf("entry %d mismatch: %q/%d vs %q/%d", i, out[i].Name, out[i].Dsize, in[i].Name, in[i].Dsize)
		}
	}
	// rebuild must be byte-identical (idempotent)
	blob2, _ := Build(ver, out)
	if !bytes.Equal(blob, blob2) {
		t.Fatal("rebuild not byte-identical")
	}
}

// TestHeaderBytes anchors the on-disk header against hand-computed values.
func TestHeaderBytes(t *testing.T) {
	blob, err := Build(Version, []*Entry{rawEntry("a.qb", []byte("WXYZ"))})
	if err != nil {
		t.Fatal(err)
	}
	// header: total=40, ver, nfiles=1 ; entry hdr: dsize=4 csize=0 nlen=8 crc
	wantTotal := 12 + 16 + 8 + 4 // 40
	if got := binary.LittleEndian.Uint32(blob[0:]); int(got) != wantTotal {
		t.Errorf("total = %d, want %d", got, wantTotal)
	}
	if got := binary.LittleEndian.Uint32(blob[4:]); got != Version {
		t.Errorf("version = 0x%x", got)
	}
	if got := binary.LittleEndian.Uint32(blob[8:]); got != 1 {
		t.Errorf("nfiles = %d", got)
	}
	if len(blob) != wantTotal {
		t.Errorf("len = %d, want %d", len(blob), wantTotal)
	}
}

func TestParseRejectsBadVersion(t *testing.T) {
	bad := make([]byte, 12)
	binary.LittleEndian.PutUint32(bad[4:], 0x12345678)
	if _, _, err := Parse(bad); err == nil {
		t.Fatal("expected error on bad version")
	}
}

func TestFind(t *testing.T) {
	entries := []*Entry{
		rawEntry(`scripts\game\Foo.QB`, []byte("x")),
		// junk after NUL in the name field (real archives do this)
		{Dsize: 1, Nlen: 20, Name: []byte("global_flags.qb\x00ca"), Blob: []byte{'y', 0, 0, 0}},
	}
	if Find(entries, `scripts/game/foo.qb`) == nil { // / normalised, case-insensitive
		t.Error("case/slash-insensitive find failed")
	}
	if Find(entries, "global_flags.qb") == nil { // match before the NUL junk
		t.Error("junk-after-NUL find failed")
	}
	if Find(entries, "missing.qb") != nil {
		t.Error("found a nonexistent entry")
	}
}

func TestReplaceRaw(t *testing.T) {
	e := rawEntry("x.qb", []byte("old"))
	ReplaceRaw(e, []byte("hello")) // len 5 -> padded to 8
	if e.Dsize != 5 || e.Csize != 0 {
		t.Fatalf("dsize/csize = %d/%d", e.Dsize, e.Csize)
	}
	if len(e.Blob)%4 != 0 || len(e.Blob) != 8 {
		t.Fatalf("blob len = %d, want 8 (aligned)", len(e.Blob))
	}
	if !bytes.Equal(e.RawData(), []byte("hello")) {
		t.Fatalf("RawData = %q", e.RawData())
	}
}

func TestReplaceCompressed(t *testing.T) {
	data := bytes.Repeat([]byte("THUG2"), 400) // compressible
	e := rawEntry("q.qb", []byte("old"))
	if err := ReplaceCompressed(e, data); err != nil {
		t.Fatal(err)
	}
	if e.Csize == 0 || e.Csize >= e.Dsize {
		t.Fatalf("expected compression: csize=%d dsize=%d", e.Csize, e.Dsize)
	}
	if len(e.Blob)%4 != 0 {
		t.Fatal("compressed blob not aligned")
	}
	if !bytes.Equal(e.RawData(), data) {
		t.Fatal("RawData != original after compress")
	}
	// survives a full archive round-trip
	blob, _ := Build(Version, []*Entry{e})
	_, out, err := Parse(blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out[0].RawData(), data) {
		t.Fatal("compressed entry lost data through Build/Parse")
	}
}
