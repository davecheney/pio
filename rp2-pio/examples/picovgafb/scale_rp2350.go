//go:build rp2350

package main

// The RP2350 has 520KB of SRAM, enough for a framebuffer stretched two screen
// pixels each way: 400x300, 240KB.
const (
	scale    = 2
	fbWidth  = 800 / scale
	fbHeight = 600 / scale
)

var pixels [fbWidth * fbHeight]uint16
