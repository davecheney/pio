//go:build rp2040 || rp2350

// Command picovgafb displays a framebuffer over VGA on the Pimoroni Pico VGA
// Demo Base, which implements the RP2040 VGA reference design.
//
// The mode is 800x600 at 60Hz drawn from a 400x300 framebuffer of 16 bit RGB555
// pixels, each framebuffer pixel covering two screen pixels each way. 800x600
// is used because VESA gives both of its sync signals positive polarity, which
// is what the base program's side-set produces without inverting a pad.
//
// The board wires five resistor ladder bits to each colour channel, six pins
// apart, so a pixel needs sixteen consecutive pins and therefore sixteen bits.
// The CPU writes a few control words per scanline and DMA streams the pixels,
// leaving the processor almost entirely free.
//
// Wiring is the Demo Base's own: nothing to connect beyond seating the board.
package main

import (
	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// The framebuffer is a package level array so it lands in BSS rather than the
// heap. At 400x300 it is 240KB, which fits the RP2350 but not the RP2040.
var pixels [400 * 300]uint16

func main() {
	mode := picovga.Mode800x600RGB555
	fb := &picovga.Framebuffer16{
		Pix:    pixels[:],
		Width:  mode.Width(),
		Height: mode.Height(),
	}
	draw(fb)

	vga, err := picovga.NewVGA(picovga.PimoroniVGA, mode, fb)
	if err != nil {
		panic(err)
	}
	vga.Start()
	vga.Run()
}

// draw paints a test pattern: colour bars, a grey ramp, and colour ramps for
// each channel, inside a white border.
func draw(fb *picovga.Framebuffer16) {
	fb.Fill(picovga.Black555)

	bars := []picovga.Colour{
		picovga.White555, picovga.Yellow555, picovga.Cyan555, picovga.Green555,
		picovga.Magenta555, picovga.Red555, picovga.Blue555, picovga.Black555,
	}
	barWidth := fb.Width / len(bars)
	for i, c := range bars {
		fb.FillRect(i*barWidth, 0, barWidth, fb.Height/2, c)
	}

	// A grey ramp below the bars shows how linear the resistor ladder is.
	top := fb.Height / 2
	for x := 0; x < fb.Width; x++ {
		level := uint8(x * 32 / fb.Width)
		fb.VLine(x, top, 40, picovga.RGB555(level, level, level))
	}

	// One ramp per channel, to check each ladder separately.
	for i, ch := range []func(uint8) picovga.Colour{
		func(v uint8) picovga.Colour { return picovga.RGB555(v, 0, 0) },
		func(v uint8) picovga.Colour { return picovga.RGB555(0, v, 0) },
		func(v uint8) picovga.Colour { return picovga.RGB555(0, 0, v) },
	} {
		y := top + 40 + i*30
		for x := 0; x < fb.Width; x++ {
			fb.VLine(x, y, 30, ch(uint8(x*32/fb.Width)))
		}
	}

	fb.Rect(0, 0, fb.Width, fb.Height, picovga.White555)
}
