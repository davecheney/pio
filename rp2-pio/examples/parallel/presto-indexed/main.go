//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

const frameLimit = 3000

var frame presto.IndexedFrame

var palette presto.Palette

func main() {
	// USB CDC enumerates after boot; repeat the earliest marker so a monitor
	// attached a little late still sees that the runtime reached main.
	for i := 0; i < 12; i++ {
		println("indexed: boot marker", i, "single SRAM frame bytes", len(frame), "frame limit", frameLimit)
		time.Sleep(250 * time.Millisecond)
	}
	colors := [...]uint16{0xffff, 0xffe0, 0x07ff, 0x07e0, 0xf81f, 0xf800, 0x001f, 0x0000, 0xffff, 0xfd20}
	for i := range colors {
		palette[i] = presto.PackPixels(colors[i])
	}
	println("indexed: palette ready; drawing static image")
	demo := newPattern()
	for row := 0; row < presto.Height; row++ {
		demo.DrawRow(frame.Row(row), row, 0)
	}
	println("indexed: static image ready")
	presto.RunIndexed(&frame, &palette, frameLimit)
}
