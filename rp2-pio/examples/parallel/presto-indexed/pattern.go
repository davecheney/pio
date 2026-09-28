package main

import "github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"

type pattern struct {
	bars, border [presto.Width]uint8
}

func newPattern() *pattern {
	p := &pattern{}
	for x := 0; x < presto.Width; x++ {
		p.bars[x] = uint8(x / 60)
		p.border[x] = 8
	}
	p.bars[0], p.bars[1] = 8, 8
	p.bars[presto.Width-2], p.bars[presto.Width-1] = 8, 8
	return p
}

func (p *pattern) DrawRow(dst []uint8, row int, frame uint32) {
	if row < 2 || row >= presto.Height-2 {
		copy(dst, p.border[:])
		return
	}
	copy(dst, p.bars[:])
	if row >= 208 && row < 272 {
		x := 2 + int(frame%412)
		for i := x; i < x+64; i++ {
			dst[i] = 9
		}
	}
}
