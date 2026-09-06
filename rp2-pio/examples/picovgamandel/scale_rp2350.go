//go:build rp2350

package main

// The RP2350 has 520KB of SRAM, enough for a framebuffer stretched two screen
// pixels each way: 320x240, 150KB. Four times the pixels of the RP2040 build,
// so the picture takes about four times as long to draw.
const (
	scale    = 2
	fbWidth  = 640 / scale
	fbHeight = 480 / scale
)

var pixels [fbWidth * fbHeight]uint16
