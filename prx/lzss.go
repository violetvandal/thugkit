package prx

// THUG2 .prx LZSS (Neversoft / Okumura-style).
// Control byte, LSB-first; bit=1 => literal byte; bit=0 => match.
// Match = 2 bytes (lo, hi): pos = lo | ((hi & 0xF0) << 4); len = (hi & 0x0F) + 3;
// copy len bytes from the 4096-byte ring buffer at pos.
//
// Faithful port of tools/prx/lzss.py. Any valid encoding the decoder reproduces
// is acceptable (verified by round-trip), so this need not byte-match Python.

const (
	lzN         = 4096
	lzF         = 18
	lzThreshold = 2
	lzMinMatch  = lzThreshold + 1 // 3
	lzMaxMatch  = lzF             // 18
)

// Decompress expands comp into outsize bytes.
func Decompress(comp []byte, outsize int) []byte {
	ring := make([]byte, lzN)
	r := lzN - lzF
	out := make([]byte, 0, outsize)
	i := 0
	for len(out) < outsize && i < len(comp) {
		flags := comp[i]
		i++
		for b := 0; b < 8; b++ {
			if len(out) >= outsize || i >= len(comp) {
				break
			}
			if (flags>>uint(b))&1 == 1 { // literal
				c := comp[i]
				i++
				out = append(out, c)
				ring[r] = c
				r = (r + 1) % lzN
			} else { // match
				lo := comp[i]
				hi := comp[i+1]
				i += 2
				pos := int(lo) | ((int(hi) & 0xF0) << 4)
				length := (int(hi) & 0x0F) + lzThreshold + 1
				for k := 0; k < length; k++ {
					if len(out) >= outsize {
						break
					}
					c := ring[(pos+k)%lzN]
					out = append(out, c)
					ring[r] = c
					r = (r + 1) % lzN
				}
			}
		}
	}
	return out
}

// Compress encodes data into a stream Decompress reproduces exactly.
func Compress(data []byte) []byte {
	n := len(data)
	out := make([]byte, 0, n/2+16)
	// hash chain: 3-byte key -> positions, oldest-first (newest at the tail).
	chains := make(map[uint32][]int)
	const maxCandidates = 256

	key := func(p int) uint32 {
		return uint32(data[p]) | uint32(data[p+1])<<8 | uint32(data[p+2])<<16
	}

	p := 0
	r := lzN - lzF // mirror the decoder's ring write pointer
	for p < n {
		var flag byte
		chunk := make([]byte, 0, 16)
		for b := 0; b < 8; b++ {
			if p >= n {
				break
			}
			bestLen := 0
			bestDist := 0
			if p+lzMinMatch <= n {
				limit := p - lzN // oldest allowed source position
				cands := chains[key(p)]
				tried := 0
				// newest-first: walk from the tail backwards.
				for j := len(cands) - 1; j >= 0; j-- {
					src := cands[j]
					if src <= limit {
						break // rest are too old
					}
					maxl := lzMaxMatch
					if n-p < maxl {
						maxl = n - p
					}
					l := 0
					for l < maxl && data[src+l] == data[p+l] {
						l++
					}
					if l > bestLen {
						bestLen = l
						bestDist = p - src
						if l == lzMaxMatch {
							break
						}
					}
					tried++
					if tried >= maxCandidates {
						break
					}
				}
			}
			if bestLen >= lzMinMatch {
				pos := ((r - bestDist) % lzN + lzN) % lzN
				lo := byte(pos & 0xFF)
				hi := byte(((pos >> 4) & 0xF0) | ((bestLen - lzMinMatch) & 0x0F))
				chunk = append(chunk, lo, hi)
				for k := 0; k < bestLen; k++ {
					if p+2 < n {
						kk := key(p)
						chains[kk] = append(chains[kk], p)
					}
					p++
					r = (r + 1) % lzN
				}
			} else {
				flag |= 1 << uint(b) // literal
				if p+2 < n {
					kk := key(p)
					chains[kk] = append(chains[kk], p)
				}
				chunk = append(chunk, data[p])
				p++
				r = (r + 1) % lzN
			}
		}
		out = append(out, flag)
		out = append(out, chunk...)
	}
	return out
}
