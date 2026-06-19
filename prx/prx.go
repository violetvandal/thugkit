// Package prx reads and rewrites THUG2 .prx (PRE) archives.
//
// Format: header[12] = <u32 totalSize><u32 version=0xABCD0003><u32 numFiles>
//
//	then per file: <u32 dataSize><u32 compSize><u32 nameLen><u32 nameCRC>
//	               <name (nameLen bytes; null-terminated + 4-byte aligned)>
//	               <blob (compSize bytes if compSize else dataSize; 4-byte aligned)>
//
// compSize>0 => blob is LZSS-compressed to dataSize; compSize==0 => stored raw.
// The game's loader accepts raw entries, so a file can be replaced with an
// uncompressed blob (csize=0) without the compressor.
//
// This is a faithful Go port of tools/prx/prx.py — round-trips byte-identical.
package prx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
)

const Version = 0xABCD0003

// Entry is one file in the archive. Blob carries its 4-byte alignment padding so
// round-trips stay byte-identical (original padding is not always zero-filled).
type Entry struct {
	Dsize uint32 // uncompressed size
	Csize uint32 // compressed size (0 => stored raw)
	Nlen  uint32 // on-disk name-field length (null-terminated + 4-byte aligned)
	Crc   uint32 // name checksum (preserved as-is)
	Name  []byte // raw name field contents up to first NUL is the real name
	Blob  []byte // data including alignment padding
}

func align4(x int) int { return (x + 3) &^ 3 }

// Parse decodes an archive. It errors on a bad version or trailing data.
func Parse(d []byte) (ver uint32, entries []*Entry, err error) {
	if len(d) < 12 {
		return 0, nil, fmt.Errorf("short archive: %d bytes", len(d))
	}
	_ = binary.LittleEndian.Uint32(d[0:]) // total (recomputed on Build)
	ver = binary.LittleEndian.Uint32(d[4:])
	nfiles := binary.LittleEndian.Uint32(d[8:])
	if ver != Version {
		return 0, nil, fmt.Errorf("bad version 0x%x", ver)
	}
	off := 12
	for i := uint32(0); i < nfiles; i++ {
		if off+16 > len(d) {
			return 0, nil, fmt.Errorf("truncated entry header at 0x%x", off)
		}
		e := &Entry{
			Dsize: binary.LittleEndian.Uint32(d[off:]),
			Csize: binary.LittleEndian.Uint32(d[off+4:]),
			Nlen:  binary.LittleEndian.Uint32(d[off+8:]),
			Crc:   binary.LittleEndian.Uint32(d[off+12:]),
		}
		nameStart := off + 16
		nameEnd := nameStart + int(e.Nlen)
		if nameEnd > len(d) {
			return 0, nil, fmt.Errorf("truncated name at 0x%x", nameStart)
		}
		// keep the full name field (some carry junk after the NUL); trim only the
		// trailing NUL padding so the stored bytes match the python rstrip(b'\0').
		e.Name = bytes.TrimRight(d[nameStart:nameEnd], "\x00")
		blen := int(e.Csize)
		if blen == 0 {
			blen = int(e.Dsize)
		}
		dstart := nameEnd
		dend := dstart + align4(blen)
		if dend > len(d) {
			return 0, nil, fmt.Errorf("truncated blob at 0x%x", dstart)
		}
		e.Blob = d[dstart:dend]
		entries = append(entries, e)
		off = dend
	}
	if off != len(d) {
		return 0, nil, fmt.Errorf("trailing data: ended 0x%x of 0x%x", off, len(d))
	}
	return ver, entries, nil
}

// Build re-encodes entries into a complete archive.
func Build(ver uint32, entries []*Entry) ([]byte, error) {
	var body bytes.Buffer
	hdr := make([]byte, 16)
	for _, e := range entries {
		if int(e.Nlen) < len(e.Name) {
			return nil, fmt.Errorf("name longer than nlen for %q", e.Name)
		}
		if len(e.Blob)%4 != 0 {
			return nil, fmt.Errorf("blob not 4-byte aligned for %q", e.Name)
		}
		binary.LittleEndian.PutUint32(hdr[0:], e.Dsize)
		binary.LittleEndian.PutUint32(hdr[4:], e.Csize)
		binary.LittleEndian.PutUint32(hdr[8:], e.Nlen)
		binary.LittleEndian.PutUint32(hdr[12:], e.Crc)
		body.Write(hdr)
		body.Write(e.Name)
		body.Write(make([]byte, int(e.Nlen)-len(e.Name))) // NUL pad the name field
		body.Write(e.Blob)
	}
	total := 12 + body.Len()
	out := make([]byte, 12, total)
	binary.LittleEndian.PutUint32(out[0:], uint32(total))
	binary.LittleEndian.PutUint32(out[4:], ver)
	binary.LittleEndian.PutUint32(out[8:], uint32(len(entries)))
	out = append(out, body.Bytes()...)
	return out, nil
}

func normName(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "/", "\\")
}

// Find returns the entry whose real name (bytes before the first NUL) matches,
// case-insensitively with / normalised to \.
func Find(entries []*Entry, name string) *Entry {
	want := normName(name)
	for _, e := range entries {
		stored := e.Name
		if i := bytes.IndexByte(stored, 0); i >= 0 {
			stored = stored[:i]
		}
		if normName(string(stored)) == want {
			return e
		}
	}
	return nil
}

// ReplaceRaw stores newdata uncompressed (csize=0), 4-byte padded.
func ReplaceRaw(e *Entry, newdata []byte) {
	e.Dsize = uint32(len(newdata))
	e.Csize = 0
	pad := align4(len(newdata)) - len(newdata)
	e.Blob = append(append([]byte(nil), newdata...), make([]byte, pad)...)
}

// ReplaceCompressed LZSS-compresses newdata and stores it. It verifies the
// decoder reproduces the input exactly before committing the blob.
func ReplaceCompressed(e *Entry, newdata []byte) error {
	comp := Compress(newdata)
	if !bytes.Equal(Decompress(comp, len(newdata)), newdata) {
		return fmt.Errorf("LZSS compress round-trip mismatch")
	}
	e.Dsize = uint32(len(newdata))
	e.Csize = uint32(len(comp))
	pad := align4(len(comp)) - len(comp)
	e.Blob = append(comp, make([]byte, pad)...)
	return nil
}

// RawData returns an entry's uncompressed bytes (decompressing if needed).
func (e *Entry) RawData() []byte {
	if e.Csize == 0 {
		return e.Blob[:e.Dsize]
	}
	return Decompress(e.Blob[:e.Csize], int(e.Dsize))
}
