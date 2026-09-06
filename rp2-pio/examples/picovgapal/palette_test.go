package main

import (
	"testing"

	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// TestExpandMatchesDirect is what this demo exists to show: a line expanded
// from palette indices is bit for bit what a framebuffer of colours would have
// held, so DMA cannot tell the two apart and the byte order works out the same.
func TestExpandMatchesDirect(t *testing.T) {
	const width = 320
	pal := newPalette()

	direct := picovga.NewFramebuffer16(width, 1)
	indexed := make([]uint8, width)
	for x := 0; x < width; x++ {
		i := uint8(x * 251 % paletteSize) // strides the whole palette
		indexed[x] = i
		direct.SetPixel(x, 0, paletteEntry(int(i)))
	}

	line := make([]uint16, width)
	expand(indexed, line, &pal)

	for x := range line {
		if line[x] != direct.Pix[x] {
			t.Fatalf("pixel %d: expanded %#04x, a direct framebuffer holds %#04x",
				x, line[x], direct.Pix[x])
		}
	}
}

// TestExpandEveryIndex checks every entry of the palette survives the round
// trip, not just the ones a stride happens to visit.
func TestExpandEveryIndex(t *testing.T) {
	pal := newPalette()
	src := make([]uint8, paletteSize)
	for i := range src {
		src[i] = uint8(i)
	}
	dst := make([]uint16, paletteSize)
	expand(src, dst, &pal)

	fb := picovga.NewFramebuffer16(paletteSize, 1)
	for i := 0; i < paletteSize; i++ {
		fb.SetPixel(i, 0, paletteEntry(i))
	}
	for i := range dst {
		if dst[i] != fb.Pix[i] {
			t.Fatalf("index %d expands to %#04x, want %#04x", i, dst[i], fb.Pix[i])
		}
	}
}

// TestExpandLeavesRestOfBuffer checks a short row does not disturb whatever
// follows it in a line buffer.
func TestExpandLeavesRestOfBuffer(t *testing.T) {
	pal := newPalette()
	dst := make([]uint16, 8)
	for i := range dst {
		dst[i] = 0xdead
	}
	expand([]uint8{1, 2, 3}, dst, &pal)
	for i := 3; i < len(dst); i++ {
		if dst[i] != 0xdead {
			t.Errorf("expand wrote past the row, at %d", i)
		}
	}
}

func TestPaletteEntries(t *testing.T) {
	if got := paletteEntry(0); got != picovga.Black555 {
		t.Errorf("index 0 = %#04x, want black", got)
	}
	if paletteEntry(1) == picovga.Black555 {
		t.Error("index 1 should not be black; only points in the set are")
	}
	// Bit 5 drives the SD card clock and no colour may set it.
	for i := 0; i < paletteSize; i++ {
		if uint16(paletteEntry(i))&(1<<5) != 0 {
			t.Fatalf("palette entry %d sets bit 5", i)
		}
	}
}

func TestIndex(t *testing.T) {
	const maxIter = 96
	if got := index(maxIter, maxIter); got != 0 {
		t.Errorf("a point in the set has index %d, want 0", got)
	}
	if got := index(maxIter+50, maxIter); got != 0 {
		t.Errorf("a count past the limit has index %d, want 0", got)
	}
	for i := 0; i < maxIter; i++ {
		if index(i, maxIter) == 0 {
			t.Fatalf("escape count %d has index 0, which is reserved for the set", i)
		}
	}
}
