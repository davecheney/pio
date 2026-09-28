package presto

import "math/bits"

const (
	Width  = 480
	Height = 480
)

type Line [Width / 2]uint32

func PackPixels(c uint16) uint32 {
	v := uint32(reorderChannels(c))
	return bits.ReverseBytes32(v<<16 | v)
}

func RGB565(r, g, b uint8) uint16 {
	return uint16(r&0xf8)<<8 | uint16(g&0xfc)<<3 | uint16(b)>>3
}

func reorderChannels(c uint16) uint16 {
	r5 := uint16((c >> 11) & 0x1f)
	g6 := uint16((c >> 5) & 0x3f)
	b5 := c & 0x1f
	return (g6&0x7)<<13 | b5<<8 | r5<<3 | (g6>>3)&0x7
}
