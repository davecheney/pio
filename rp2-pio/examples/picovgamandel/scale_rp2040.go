//go:build rp2040

package main

// The RP2040 has 264KB of SRAM, of which rather less than the link report
// suggests is actually usable, so this target stretches each framebuffer pixel
// over four screen pixels each way: 160x120, 38KB.
const (
	scale    = 4
	fbWidth  = 640 / scale
	fbHeight = 480 / scale
)

var pixels [fbWidth * fbHeight]uint16
