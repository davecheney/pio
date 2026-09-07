//go:build rp2040 || rp2350

// Command picovgamandel draws the Mandelbrot set into a framebuffer displayed
// at 640x480 over VGA on the Pimoroni Pico VGA Demo Base.
//
// The display loop has no spare time in it: it hands control words to a state
// machine and waits on DMA, and it can never stop doing so. Both chips have two
// cores, so the render runs on the other one, painting into the framebuffer
// while this core sends it. That needs the cores scheduler, which is not the
// default for these targets:
//
//	tinygo flash -target=pico -scheduler=cores ./rp2-pio/examples/picovgamandel/
//
// Built without it the render goroutine is never given a core, since the
// display loop holds the only one and never yields, and the screen stays blank.
//
// The framebuffer is written by the render and read by DMA at the same time
// without any locking. The worst that does is show a half drawn picture, which
// is the point of drawing it progressively.
//
// Wiring is the Demo Base's own: nothing to connect beyond seating the board.
package main

import (
	"github.com/tinygo-org/pio/rp2-pio/examples/internal/mandel"
	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

func main() {
	d, err := picovga.New(picovga.PimoroniVGA, picovga.Mode640x480RGB555.Scaled(scale, scale))
	if err != nil {
		panic(err)
	}
	if d.Width() != fbWidth || d.Height() != fbHeight {
		panic("framebuffer does not match the display")
	}

	fb := &picovga.Framebuffer16{Pix: pixels[:], Width: fbWidth, Height: fbHeight}
	fb.Fill(picovga.Black555)

	// The render has a core of its own, so it runs flat out alongside the
	// display without either waiting for the other.
	go render(mandel.New(d.Width(), d.Height(), painter{fb}))

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

// render draws the set over and over, zooming a little further in each time. It
// runs on the second core, so it is free to take as long as it needs and never
// pauses between pictures.
func render(r *mandel.Renderer) {
	for {
		r.Draw()
		r.Zoom()
	}
}
