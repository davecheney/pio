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

import "github.com/tinygo-org/pio/rp2-pio/picovga"

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
	mode := picovga.Mode640x480RGB555
	cfg := picovga.PimoroniVGA

	fb := &picovga.Framebuffer{
		Pix:    pixels[:],
		Width:  fbWidth,
		Height: fbHeight,
	}
	pal = newPalette()

	// The render has a core of its own, so it runs flat out alongside the
	// display without either waiting for the other.
	go render(newRenderer(fb, maxIter))

	sm, err := picovga.Setup(cfg, mode)
	if err != nil {
		panic(err)
	}
	if mode.Width() != fb.Width || mode.Height() != fb.Height {
		panic("framebuffer does not match the mode")
	}
	dma := dmaChannelAt(0)
	ctrl := dma.ctrl(txDREQ(sm))
	txf := sm.TxReg()

	sync, back, out, front := mode.SyncWord(), mode.BackPorchWord(), mode.OutputWord(), mode.FrontPorchWord()
	blank := mode.BlankWords()

	put := func(w uint32) {
		for sm.IsTxFIFOFull() {
		}
		sm.TxPut(w)
	}

	cur := 0
	sm.SetEnabled(true)
	for {
		// The first visible line of a frame has nothing before it to have been
		// converted during, so it is done here, in the vertical blanking.
		if row, ok := mode.VisibleLine(mode.VSync + mode.VBack); ok {
			expand(fb.Line(row), lines[cur][:], &pal)
		}
		for line := 0; line < mode.VTotal(); line++ {
			cfg.VSync.Set(mode.InVSync(line) == mode.PositiveVSync)
			if _, visible := mode.VisibleLine(line); !visible {
				put(sync)
				for _, w := range blank {
					put(w)
				}
				continue
			}
			put(sync)
			put(back)
			put(out)
			// The pixels must reach the FIFO after the output command and
			// before the front porch: all three go through the same FIFO and
			// their order is the line.
			dma.start(txf, lines[cur][:], ctrl)
			// While that is going out, convert the line after it into the other
			// buffer. This is the whole point: the conversion costs nothing
			// that was not already being spent waiting.
			next := 1 - cur
			if row, ok := mode.VisibleLine(line + 1); ok {
				expand(fb.Line(row), lines[next][:], &pal)
			}
			dma.wait()
			put(front)
			cur = next
		}
	}
}

// render draws the set over and over, zooming a little further in each time. It
// runs on the second core, so it is free to take as long as it needs and never
// pauses between pictures.
func render(r *renderer) {
	for {
		r.draw()
		r.zoom()
	}
}
