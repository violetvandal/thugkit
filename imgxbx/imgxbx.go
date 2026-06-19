// Package imgxbx encodes an image into a THUG2 Xbox .img.xbx sprite
// (palettized 8-bit, swizzled) — the CAGR clip-art / custom-tag format.
//
// Port of the reference png2img.py. The format (header, BGRA palette, swizzle,
// vertical flip) is reproduced exactly; the color quantization is a Go
// median-cut (PIL's FASTOCTREE is not byte-reproducible, and any valid
// quantization yields a valid in-game tag). Zero external dependencies.
package imgxbx

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sort"
)

type rgba struct{ r, g, b, a uint8 }

// Encode loads an image file, resizes to size×size, and returns .img.xbx bytes.
// size must be a power of two (64/128/256).
func Encode(path string, size int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	px := resizeArea(src, size, size) // row-major, length size*size
	flipY(px, size, size)             // CAGR .img.xbx are stored vertically flipped

	indices, palette := medianCut(px, 256)

	var out bytes.Buffer
	w32, h32 := uint32(size), uint32(size)
	palsize := uint32(len(palette) * 4)
	binary.Write(&out, binary.LittleEndian, []uint32{2, 8, w32, h32, 0x13, 0})
	binary.Write(&out, binary.LittleEndian, []uint16{uint16(size), uint16(size)})
	binary.Write(&out, binary.LittleEndian, palsize)
	for _, c := range palette { // BGRA
		out.Write([]byte{c.b, c.g, c.r, c.a})
	}
	out.Write(swizzle8(indices, size, size))
	return out.Bytes(), nil
}

// resizeArea downscales/upscales by averaging the source box each target pixel
// covers (good quality for the usual downscale-to-64 case). Returns RGBA pixels.
func resizeArea(src image.Image, tw, th int) []rgba {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := make([]rgba, tw*th)
	for ty := 0; ty < th; ty++ {
		y0 := b.Min.Y + ty*sh/th
		y1 := b.Min.Y + (ty+1)*sh/th
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for tx := 0; tx < tw; tx++ {
			x0 := b.Min.X + tx*sw/tw
			x1 := b.Min.X + (tx+1)*sw/tw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, as, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					r, g, bb, a := src.At(x, y).RGBA() // 16-bit
					rs += uint64(r >> 8)
					gs += uint64(g >> 8)
					bs += uint64(bb >> 8)
					as += uint64(a >> 8)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			out[ty*tw+tx] = rgba{uint8(rs / n), uint8(gs / n), uint8(bs / n), uint8(as / n)}
		}
	}
	return out
}

func flipY(px []rgba, w, h int) {
	for y := 0; y < h/2; y++ {
		for x := 0; x < w; x++ {
			px[y*w+x], px[(h-1-y)*w+x] = px[(h-1-y)*w+x], px[y*w+x]
		}
	}
}

// medianCut quantizes pixels to <=maxColors and returns per-pixel palette
// indices (row-major) plus the palette (each entry = mean of its cluster).
func medianCut(px []rgba, maxColors int) ([]uint8, []rgba) {
	idxs := make([]int, len(px))
	for i := range idxs {
		idxs[i] = i
	}
	type box struct{ members []int }
	boxes := []box{{members: idxs}}

	channelRange := func(m []int) (ch int, span int) {
		var mn, mx [4]uint8
		for c := 0; c < 4; c++ {
			mn[c] = 255
		}
		for _, i := range m {
			p := []uint8{px[i].r, px[i].g, px[i].b, px[i].a}
			for c := 0; c < 4; c++ {
				if p[c] < mn[c] {
					mn[c] = p[c]
				}
				if p[c] > mx[c] {
					mx[c] = p[c]
				}
			}
		}
		for c := 0; c < 4; c++ {
			if s := int(mx[c]) - int(mn[c]); s > span {
				span, ch = s, c
			}
		}
		return ch, span
	}
	comp := func(i, c int) uint8 {
		switch c {
		case 0:
			return px[i].r
		case 1:
			return px[i].g
		case 2:
			return px[i].b
		default:
			return px[i].a
		}
	}

	for len(boxes) < maxColors {
		// pick the box with the largest channel span that has >1 member
		best, bestSpan, bestCh := -1, 0, 0
		for bi := range boxes {
			if len(boxes[bi].members) < 2 {
				continue
			}
			ch, span := channelRange(boxes[bi].members)
			if span > bestSpan {
				best, bestSpan, bestCh = bi, span, ch
			}
		}
		if best < 0 {
			break // nothing left to split
		}
		m := boxes[best].members
		sort.Slice(m, func(a, b int) bool { return comp(m[a], bestCh) < comp(m[b], bestCh) })
		mid := len(m) / 2
		left := box{members: m[:mid]}
		right := box{members: m[mid:]}
		boxes[best] = left
		boxes = append(boxes, right)
	}

	indices := make([]uint8, len(px))
	palette := make([]rgba, len(boxes))
	for bi := range boxes {
		var rs, gs, bs, as uint64
		for _, i := range boxes[bi].members {
			rs += uint64(px[i].r)
			gs += uint64(px[i].g)
			bs += uint64(px[i].b)
			as += uint64(px[i].a)
			indices[i] = uint8(bi)
		}
		n := uint64(len(boxes[bi].members))
		if n == 0 {
			continue
		}
		palette[bi] = rgba{
			uint8((rs + n/2) / n), uint8((gs + n/2) / n),
			uint8((bs + n/2) / n), uint8((as + n/2) / n),
		}
	}
	return indices, palette
}

// --- swizzle (faithful port of png2img.py) ---

func swizzleAxis(val, mask int) int {
	bit, res := 1, 0
	for bit <= mask {
		if mask&bit != 0 {
			res |= val & bit
		} else {
			val <<= 1
		}
		bit <<= 1
	}
	return res
}

func masks(w, h int) (mx, my int) {
	idx, bit := 1, 1
	for bit < w || bit < h {
		if bit < w {
			mx |= idx
			idx <<= 1
		}
		if bit < h {
			my |= idx
			idx <<= 1
		}
		bit <<= 1
	}
	return
}

func swizzle8(linear []uint8, w, h int) []byte {
	mx, my := masks(w, h)
	out := make([]byte, len(linear))
	for y := 0; y < h; y++ {
		sy := swizzleAxis(y, my)
		for x := 0; x < w; x++ {
			out[swizzleAxis(x, mx)|sy] = linear[y*w+x]
		}
	}
	return out
}
