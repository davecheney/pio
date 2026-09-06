//go:build rp2040 || rp2350

// Command picovgabars displays colour bars over VGA on the Pimoroni Pico VGA
// Demo Base, which implements the RP2040 VGA reference design.
//
// This is the smaller of the two VGA examples and the one to reach for first.
// It uses neither a framebuffer nor DMA: a scanline of flat colour needs only
// the base program's sync and dark routines, so the CPU writes eleven control
// words per line and nothing else has to be working. If a display will not lock
// onto this, the problem is in the mode timings or the wiring rather than in
// the pixel path. See the picovgafb example for a framebuffer driven by DMA.
//
// The mode is 800x600 at 60Hz, chosen because VESA gives both of its sync
// signals positive polarity, which is what the base program's side-set produces
// without inverting a pad.
//
// Wiring is the Demo Base's own: red on GP0 to GP4, green on GP6 to GP10, blue
// on GP11 to GP15, horizontal sync on GP16 and vertical sync on GP17. GP5 lies
// inside that range but drives the SD card clock, and is left alone.
package main

import (
	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// Bars are drawn left to right in descending luminance, the usual arrangement
// for a test pattern.
var bars = []picovga.Colour{
	picovga.White555,
	picovga.Yellow555,
	picovga.Cyan555,
	picovga.Green555,
	picovga.Magenta555,
	picovga.Red555,
	picovga.Blue555,
	picovga.Black555,
}

func main() {
	mode := picovga.Mode800x600RGB555
	cfg := picovga.PimoroniVGA

	sm, err := picovga.Setup(cfg, mode)
	if err != nil {
		panic(err)
	}

	// Both kinds of scanline are assembled once and replayed for the life of
	// the program.
	visible := mode.FlatLineWords(bars)
	blank := mode.BlankLineWords()

	sm.SetEnabled(true)
	for {
		for line := 0; line < mode.VTotal(); line++ {
			cfg.VSync.Set(mode.InVSync(line) == mode.PositiveVSync)
			words := blank
			if _, ok := mode.VisibleLine(line); ok {
				words = visible
			}
			// The state machine stalls if the FIFO runs dry, which shows up as
			// a display that will not lock, so this loop must not be held up.
			for _, w := range words {
				for sm.IsTxFIFOFull() {
				}
				sm.TxPut(w)
			}
		}
	}
}
