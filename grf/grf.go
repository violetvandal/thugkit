// Package grf reads and writes THUG2 Create-A-Graphic (.GRF) tag files.
//
// Format (RE'd 2026-06-18, byte-exact round-trip verified):
//
//	header (0x14 bytes): u32 cksum0, u32 cksum1, u32 h08, u32 datalen, u32 h10
//	then a CStruct token stream [0x14 : datalen]: a graphic-name field + 10 layers.
//	token = <type:u8> <name> <value>; name is u16 LE for type<0x80, else u8.
//	trailer: file padded to 0x8000 (32768) with 0x69 ('i').
//
// Faithful Go port of the reference grflib.py. Deterministic — build_grf output
// is byte-identical to the Python builder for the same inputs.
package grf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"strings"
)

const (
	pad      = 0x69
	fileSize = 0x8000

	graphicNameField = 0xc3f4169a // field-name checksum of the graphic-name string
)

// Checksum is the Neversoft StringToChecksum: CRC32 (poly EDB88320, init
// 0xFFFFFFFF) with NO final XOR, case-insensitive. zlib.crc32 == crc32.IEEE.
func Checksum(s string) uint32 {
	return crc32.ChecksumIEEE([]byte(strings.ToLower(s))) ^ 0xffffffff
}

// LayerArrayField = Checksum("layer_infos") = 0x9a5ff0a3.
var LayerArrayField = Checksum("layer_infos")

// Array is a CStruct array token value. StructOnly => header-only (struct array).
type Array struct {
	StructOnly bool
	ElemType   byte
	Count      uint16
	Vals       []uint32 // f32 elements stored as Float32bits
}

// Token is one CStruct token. Val holds the value per the base type:
// f32→float32, string→string, name/u32→uint32, u8→uint8, u16→uint16,
// array→*Array, struct/int0→nil.
type Token struct {
	Type byte
	Name uint32
	Val  any
}

func nameWidth(t byte) int { return map[byte]int{0x00: 4, 0x40: 2, 0x80: 1}[t&0xc0] }
func base(t byte) byte     { return t & 0x3f }

func writeToken(out *bytes.Buffer, tok Token) error {
	out.WriteByte(tok.Type)
	if tok.Type == 0x00 {
		return nil
	}
	switch nameWidth(tok.Type) {
	case 4:
		binary.Write(out, binary.LittleEndian, tok.Name)
	case 2:
		binary.Write(out, binary.LittleEndian, uint16(tok.Name))
	case 1:
		out.WriteByte(byte(tok.Name))
	}
	switch base(tok.Type) {
	case 0x02:
		binary.Write(out, binary.LittleEndian, tok.Val.(float32))
	case 0x03:
		out.WriteString(tok.Val.(string))
		out.WriteByte(0)
	case 0x0d:
		binary.Write(out, binary.LittleEndian, tok.Val.(uint32))
	case 0x0a:
		// struct header: no value
	case 0x0c:
		a := tok.Val.(*Array)
		out.WriteByte(a.ElemType)
		binary.Write(out, binary.LittleEndian, a.Count)
		if !a.StructOnly {
			for _, x := range a.Vals {
				switch base(a.ElemType) {
				case 0x01, 0x0d:
					binary.Write(out, binary.LittleEndian, x)
				case 0x02:
					binary.Write(out, binary.LittleEndian, x) // already Float32bits
				case 0x10:
					out.WriteByte(byte(x))
				case 0x11:
					binary.Write(out, binary.LittleEndian, uint16(x))
				default:
					return fmt.Errorf("array elemtype 0x%02x unhandled", a.ElemType)
				}
			}
		}
	case 0x10:
		out.WriteByte(tok.Val.(uint8))
	case 0x11:
		binary.Write(out, binary.LittleEndian, tok.Val.(uint16))
	case 0x12:
		// int=0: no value
	default:
		return fmt.Errorf("can't write base 0x%02x", base(tok.Type))
	}
	return nil
}

// nameChecksum is header cksum1 (offset 4), validated by the editor:
// StringToChecksum over the serialized name field  03 9a16f4c3 <name> 00 00.
func nameChecksum(graphicName string) uint32 {
	region := append([]byte{0x03, 0x9a, 0x16, 0xf4, 0xc3}, []byte(graphicName)...)
	region = append(region, 0, 0)
	return crc32.ChecksumIEEE(region) ^ 0xffffffff
}

// Layer describes one Create-A-Graphic layer for the builder.
type Layer struct {
	TextureName string
	String      string
	FontID      int
	CanvasID    uint32 // 0 => default Checksum(cag_canvas_<idx>)
	PosX, PosY  int
	Rot         float32
	Scale       float32
	FlipH       bool
	FlipV       bool
	HSVA        [4]uint32
	LayerID     int
	layerIDSet  bool
}

// i8 emits a 1-byte int token, or the zero-value form (no value bytes) if v==0,
// matching the reference builder's int packing.
func i8(name uint32, v int) Token {
	if v != 0 {
		return Token{0x50, name, uint8(v)}
	}
	return Token{0x52, name, nil}
}

func emitLayer(l Layer, idx int) []Token {
	tid := Checksum("none")
	if l.TextureName != "" {
		tid = Checksum(l.TextureName)
	}
	canvas := l.CanvasID
	if canvas == 0 {
		canvas = Checksum(fmt.Sprintf("cag_canvas_%d", idx))
	}
	lid := idx
	if l.layerIDSet {
		lid = l.LayerID
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	return []Token{
		{0x4d, 0x01, tid},
		{0x43, 0x02, l.TextureName},
		{0x43, 0x03, l.String},
		{0x4d, 0x04, canvas},
		i8(0x05, l.FontID),
		{0x50, 0x06, uint8(l.PosX)},
		{0x50, 0x07, uint8(l.PosY)},
		{0x42, 0x08, l.Rot},   // rot always float
		{0x82, 0x25, l.Scale}, // scale always float (u8-name field)
		i8(0x0a, b2i(l.FlipH)),
		i8(0x0c, b2i(l.FlipV)),
		{0x4c, 0x0d, &Array{ElemType: 0x01, Count: 4, Vals: l.HSVA[:]}},
		i8(0x0e, lid),
		{0x00, 0, nil},
	}
}

// Build constructs a complete, game-loadable .GRF from scratch with a correct
// header. layers is padded to 10. h10 defaults to 1 (pass 1).
func Build(graphicName string, layers []Layer, h10 uint32) ([]byte, error) {
	ls := make([]Layer, 0, 10)
	for i, l := range layers {
		if i >= 10 {
			break
		}
		if !l.layerIDSet {
			l.LayerID, l.layerIDSet = i, true
		}
		ls = append(ls, l)
	}
	for len(ls) < 10 {
		ls = append(ls, Layer{LayerID: len(ls), layerIDSet: true})
	}
	// Match the reference builder's per-field defaults (dict.get): empty layers
	// take pos=32, scale=1.0, hsva=[0,0,100,128]. Zero-value => use default.
	for i := range ls {
		if ls[i].PosX == 0 {
			ls[i].PosX = 32
		}
		if ls[i].PosY == 0 {
			ls[i].PosY = 32
		}
		if ls[i].Scale == 0 {
			ls[i].Scale = 1.0
		}
		if ls[i].HSVA == [4]uint32{} {
			ls[i].HSVA = [4]uint32{0, 0, 100, 128}
		}
	}

	tokens := []Token{
		{0x03, graphicNameField, graphicName},
		{0x00, 0, nil},
		{0x0c, LayerArrayField, &Array{StructOnly: true, ElemType: 0x0a, Count: 10}},
	}
	for i, l := range ls {
		tokens = append(tokens, emitLayer(l, i)...)
	}

	var body bytes.Buffer
	for _, t := range tokens {
		if err := writeToken(&body, t); err != nil {
			return nil, err
		}
	}
	cksum1 := nameChecksum(graphicName)
	cksum0 := crc32.ChecksumIEEE(body.Bytes()) ^ 0xffffffff
	h08 := uint32(len(graphicName) + 7)
	datalen := uint32(0x14 + body.Len())

	var out bytes.Buffer
	hdr := []uint32{cksum0, cksum1, h08, datalen, h10}
	for _, v := range hdr {
		binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(body.Bytes())
	for out.Len() < fileSize {
		out.WriteByte(pad)
	}
	return out.Bytes(), nil
}

// Float32bits is a helper for callers building f32 array values.
func Float32bits(f float32) uint32 { return math.Float32bits(f) }
