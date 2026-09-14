//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

func colorWords(elapsed time.Duration) (foreground, background uint32) {
	fr, fg, fb, br, bg, bb := colorRGB(elapsed)
	return presto.PackPixels(presto.RGB565(fr, fg, fb)), presto.PackPixels(presto.RGB565(br, bg, bb))
}
