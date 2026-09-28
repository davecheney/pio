package main

import (
	"testing"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

func TestPatternSmoke(t *testing.T) {
	p := newPattern()
	var frame presto.IndexedFrame
	var palette presto.Palette
	var line presto.Line
	for i := range palette {
		palette[i] = presto.PackPixels(uint16(i * 257))
	}
	for _, phase := range []uint32{0, 1, 411, 412, 3000} {
		square := 0
		for row := 0; row < presto.Height; row++ {
			dst := frame.Row(row)
			p.DrawRow(dst, row, phase)
			for x, pixel := range dst {
				border := x < 2 || x >= presto.Width-2 || row < 2 || row >= presto.Height-2
				if border && pixel != 8 {
					t.Fatalf("missing border at %d,%d", x, row)
				}
				if pixel == 9 {
					square++
					start := 2 + int(phase%412)
					if x < start || x >= start+64 || row < 208 || row >= 272 {
						t.Fatalf("misplaced square at %d,%d", x, row)
					}
				}
				if pixel > 9 {
					t.Fatalf("unexpected palette index %d", pixel)
				}
			}
			frame.Expand(&line, row, &palette)
		}
		if square != 64*64 {
			t.Fatalf("phase %d: square has %d pixels", phase, square)
		}
	}
}
