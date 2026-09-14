//go:build rp2350 && rp2350b

package main

import "github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"

// Static light-grey-on-dark-grey palette: 90% white text on a 10% white
// background. Computed once (not per frame or per pixel) since the colours
// no longer cycle.
const (
	foregroundLevel = uint8(230) // ~90% of 255
	backgroundLevel = uint8(26)  // ~10% of 255
)

func clockColorWords() (foreground, background uint32) {
	foreground = presto.PackPixels(presto.RGB565(foregroundLevel, foregroundLevel, foregroundLevel))
	background = presto.PackPixels(presto.RGB565(backgroundLevel, backgroundLevel, backgroundLevel))
	return foreground, background
}
