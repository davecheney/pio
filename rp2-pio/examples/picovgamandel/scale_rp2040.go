//go:build rp2040

package main

// The RP2040 has 264KB of SRAM, of which rather less than the link report
// suggests is actually usable, so this target stretches each framebuffer pixel
// over five screen pixels each way: 160x120, 38KB.
const (
	scale    = 5
	fbWidth  = 800 / scale
	fbHeight = 600 / scale
)

var pixels [fbWidth * fbHeight]uint16
