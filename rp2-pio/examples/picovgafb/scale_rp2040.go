//go:build rp2040

package main

// The RP2040 has 264KB of SRAM, which a 400x300 framebuffer would very nearly
// fill on its own, so this target stretches each framebuffer pixel over four
// screen pixels each way instead of two. That is 200x150, 60KB.
const (
	scale    = 4
	fbWidth  = 800 / scale
	fbHeight = 600 / scale
)

var pixels [fbWidth * fbHeight]uint16
