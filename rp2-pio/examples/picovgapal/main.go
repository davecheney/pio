//go:build rp2040 || rp2350

// Command picovgapal draws the Mandelbrot set into an 8 bit palettised
// framebuffer displayed at 640x480 over VGA on the Pimoroni Pico VGA Demo Base.
//
// The base program puts sixteen bits on the pins for every pixel, so a
// palettised framebuffer has to be converted to colours somewhere. This does it
// a scanline at a time, into one of two line buffers, while DMA is busy sending
// the other. That is time the display loop would otherwise spend waiting, so
// the conversion is close to free: one indexed load and one store a pixel, the
// palette being held in the byte order DMA wants so nothing has to be swapped.
//
// The gain is memory. 320x240 costs 75KB of indices against 150KB of colours,
// which is what lets this resolution fit an RP2040 at all, and the palette can
// be changed without redrawing anything.
//
// A line is converted for every scanline, even though at this scale each row of
// the framebuffer is shown twice and the second could reuse the first. Doing
// the work twice is the harder case, and the one worth knowing fits.
//
// The two cores divide as they do in the picovgamandel demo: this one owns the
// video timing, the conversion and DMA, and the other draws into the
// framebuffer and knows nothing about any of it. That needs the cores
// scheduler, which is not the default for these targets:
//
//	tinygo flash -target=pico -scheduler=cores ./rp2-pio/examples/picovgapal/
//
// Wiring is the Demo Base's own: nothing to connect beyond seating the board.
package main

import (
	"github.com/tinygo-org/pio/rp2-pio/examples/internal/mandel"
	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// The framebuffer is 320x240 of palette indices, which is what the mode asks
// for at its default scale. Package level so it lands in BSS, not the heap.
const (
	fbWidth  = 320
	fbHeight = 240
)

var (
	pixels [fbWidth * fbHeight]uint8
	// Two line buffers: one being sent, one being filled.
	lines [2][fbWidth]uint16
	pal   [paletteSize]uint16
)

func main() {
	d, err := picovga.New(picovga.PimoroniVGA, picovga.Mode640x480RGB555)
	if err != nil {
		panic(err)
	}
	if d.Width() != fbWidth || d.Height() != fbHeight {
		panic("framebuffer does not match the display")
	}

	fb := &picovga.Framebuffer{Pix: pixels[:], Width: fbWidth, Height: fbHeight}
	pal = newPalette()

	// The render has a core of its own, so it runs flat out alongside the
	// display without either waiting for the other.
	go render(mandel.New(d.Width(), d.Height(), painter{fb}))

	cur := 0
	d.Start()
	for {
		// The first visible scanline has nothing before it to have been
		// converted during, so it is done here, in the vertical blanking.
		expand(fb.Line(0), lines[cur][:], &pal)

		for n := 0; n < d.Scanlines(); n++ {
			_, visible := d.Row(n)
			var px []uint16
			if visible {
				px = lines[cur][:]
			}
			d.ScanlineBegin(n, px)
			// While those pixels go out, convert the row the next visible
			// scanline needs into the other buffer. This is the whole point:
			// the conversion costs nothing that was not already being spent
			// waiting.
			if row, ok := d.Row(n + 1); ok {
				expand(fb.Line(row), lines[1-cur][:], &pal)
			}
			d.ScanlineEnd()
			if visible {
				cur = 1 - cur
			}
		}
	}
}

// render draws the set over and over, zooming in between pictures. It runs on
// the second core, so it is free to take as long as it needs.
func render(r *mandel.Renderer) {
	for {
		r.Draw()
		r.Zoom()
	}
}
