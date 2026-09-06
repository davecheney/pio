package main

import "github.com/tinygo-org/pio/rp2-pio/picovga"

// The framebuffer holds one byte a pixel, an index into a palette, and the
// scanline handed to DMA holds the two byte colours those indices stand for.
// Converting one to the other is done a line at a time while DMA is busy
// sending the line before it, which is time the display loop would otherwise
// spend waiting.
//
// Halving the framebuffer is the point: 320x240 costs 75KB indexed against
// 150KB direct, which is what lets this resolution fit an RP2040 at all. The
// palette can also be changed without redrawing anything.

// paletteSize is the number of colours an index can name.
const paletteSize = 256

// swap16 exchanges a colour's two bytes.
//
// DMA sends two pixels per 32 bit word with byte swapping turned on, which is
// what keeps the pair in the right order, and that reverses the bytes within
// each pixel as well. Holding the palette already swapped cancels it out and
// leaves the conversion a plain table lookup with nothing to do per pixel.
func swap16(v uint16) uint16 { return v>>8 | v<<8 }

// paletteEntry is the colour an index stands for. Index zero is black, for
// points that never escape, and the rest run through a ramp that cycles so the
// bands stay distinct however high the escape count goes.
func paletteEntry(i int) picovga.Colour {
	if i == 0 {
		return picovga.Black555
	}
	v := uint8((i - 1) & 31)
	switch ((i - 1) >> 5) % 3 {
	case 0:
		return picovga.RGB555(v, 0, 31-v)
	case 1:
		return picovga.RGB555(31-v, v, 0)
	default:
		return picovga.RGB555(0, 31-v, v)
	}
}

// newPalette builds the lookup table, holding each colour in the form DMA wants
// so that expanding a line costs one indexed load and one store a pixel.
func newPalette() [paletteSize]uint16 {
	var p [paletteSize]uint16
	for i := range p {
		p[i] = swap16(uint16(paletteEntry(i)))
	}
	return p
}

// index is the palette entry a point with the given escape count is drawn in.
func index(iter, maxIter int) uint8 {
	if iter >= maxIter {
		return 0
	}
	return uint8(1 + iter%(paletteSize-1))
}

// expand converts a row of palette indices into the colours DMA sends. dst must
// be at least as long as src.
func expand(src []uint8, dst []uint16, pal *[paletteSize]uint16) {
	dst = dst[:len(src)]
	for i, v := range src {
		dst[i] = pal[v]
	}
}
