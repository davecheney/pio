package main

import (
	"testing"

	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// TestPalette moved here when the fractal itself moved to a shared package: the
// colour scheme is this demo's, not the set's.
func TestPalette(t *testing.T) {
	if got := palette(64, 64); got != picovga.Black555 {
		t.Errorf("points in the set should be black, got %#04x", got)
	}
	if got := palette(65, 64); got != picovga.Black555 {
		t.Errorf("counts past the limit should be black, got %#04x", got)
	}
	if palette(0, 64) == picovga.Black555 {
		t.Error("points that escape at once should not be black")
	}
	// A colour must never set bit 5, which drives the SD card clock.
	for i := 0; i < 512; i++ {
		if palette(i, 1000)&(1<<5) != 0 {
			t.Fatalf("palette(%d) sets bit 5", i)
		}
	}
}

// TestPainterWritesFramebuffer checks the painter puts a pixel where it was
// told to, which is the whole of this demo's contribution to drawing the set.
func TestPainterWritesFramebuffer(t *testing.T) {
	fb := picovga.NewFramebuffer16(8, 4)
	p := painter{fb}
	p.Paint(3, 2, 0, 64)
	if got := fb.Pixel(3, 2); got != palette(0, 64) {
		t.Errorf("Pixel(3,2) = %#04x, want %#04x", got, palette(0, 64))
	}
	if got := fb.Pixel(0, 0); got != picovga.Black555 {
		t.Errorf("Pixel(0,0) = %#04x, want black", got)
	}
}
