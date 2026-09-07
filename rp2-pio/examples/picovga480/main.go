//go:build rp2040 || rp2350

// Command picovga480 displays a framebuffer at 640x480 over VGA on the Pimoroni
// Pico VGA Demo Base, which implements the RP2040 VGA reference design.
//
// The picture is a 320x240 framebuffer of 16 bit RGB555 pixels, doubled to fill
// the screen. VESA specifies negative polarity for both of this mode's sync
// signals, where 800x600 has both positive; the library handles that, inverting
// the horizontal sync pad because the base program's side-set drives its pin
// high for the length of a pulse.
//
// Wiring is the Demo Base's own: nothing to connect beyond seating the board.
package main

import "github.com/tinygo-org/pio/rp2-pio/picovga"

// Two screen pixels to a framebuffer pixel each way, which is what the mode
// asks for at its default scale.
const (
	scale    = 2
	fbWidth  = 640 / scale
	fbHeight = 480 / scale
)

var pixels [fbWidth * fbHeight]uint16

func main() {
	d, err := picovga.New(picovga.PimoroniVGA, picovga.Mode640x480RGB555.Scaled(scale, scale))
	if err != nil {
		panic(err)
	}
	if d.Width() != fbWidth || d.Height() != fbHeight {
		panic("framebuffer does not match the display")
	}

	fb := &picovga.Framebuffer16{Pix: pixels[:], Width: fbWidth, Height: fbHeight}
	draw(fb)

	d.Start()
	for {
		for n := 0; n < d.Scanlines(); n++ {
			var px []uint16
			if row, ok := d.Row(n); ok {
				px = fb.Line(row)
			}
			d.Scanline(n, px)
		}
	}
}

// draw paints colour bars over a grid. The grid is the point of this pattern:
// at 320x240 doubled to 640x480 every framebuffer pixel is a two by two block,
// so single pixel lines show at once whether the doubling is clean.
func draw(fb *picovga.Framebuffer16) {
	fb.Fill(picovga.Black555)

	bars := []picovga.Colour{
		picovga.White555, picovga.Yellow555, picovga.Cyan555, picovga.Green555,
		picovga.Magenta555, picovga.Red555, picovga.Blue555, picovga.Black555,
	}
	barWidth := fb.Width / len(bars)
	for i, c := range bars {
		fb.FillRect(i*barWidth, 0, barWidth, fb.Height/3, c)
	}

	// A one pixel grid over the rest, every sixteen pixels.
	grid := picovga.RGB555(12, 12, 12)
	top := fb.Height / 3
	for x := 0; x < fb.Width; x += 16 {
		fb.VLine(x, top, fb.Height-top, grid)
	}
	for y := top; y < fb.Height; y += 16 {
		fb.HLine(0, y, fb.Width, grid)
	}

	// A grey ramp along the bottom to show the ladder's linearity.
	band := fb.Height / 8
	for x := 0; x < fb.Width; x++ {
		level := uint8(x * 32 / fb.Width)
		fb.VLine(x, fb.Height-band, band, picovga.RGB555(level, level, level))
	}

	fb.Rect(0, 0, fb.Width, fb.Height, picovga.White555)
}
