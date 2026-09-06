package main

import "github.com/tinygo-org/pio/rp2-pio/picovga"

// VESA 640x480 at 60Hz, pixel doubled from a 320x240 framebuffer.
//
// This example keeps its own timings and scanline composition rather than using
// the picovga package's Mode, so that working on it cannot disturb the 800x600
// path. The only things it shares are the base program itself, the control word
// encoding that goes with it, and the framebuffer, none of which are specific
// to a mode.
//
// Horizontal timings are in screen pixels and vertical ones in scanlines. A
// framebuffer pixel covers scale screen pixels each way.
const (
	pixelClock = 25_175_000

	hVisible = 640
	hFront   = 16
	hSync    = 96
	hBack    = 48
	hTotal   = hVisible + hFront + hSync + hBack

	vVisible = 480
	vFront   = 10
	vSync    = 2
	vBack    = 33
	vTotal   = vVisible + vFront + vSync + vBack

	// clocksPerPixel is PIO clock cycles per screen pixel.
	clocksPerPixel = 4
	// scale is screen pixels per framebuffer pixel, each way.
	scale = 2

	fbWidth  = hVisible / scale
	fbHeight = vVisible / scale

	// cpp is PIO clock cycles per framebuffer pixel, which is what the base
	// program is assembled for.
	cpp = clocksPerPixel * scale

	// lineCycles is the length of a scanline in PIO clock cycles.
	lineCycles = hTotal * clocksPerPixel
	// pioFreq is the state machine clock rate needed to hit pixelClock.
	pioFreq = pixelClock * clocksPerPixel

	// VESA specifies negative polarity for both of this mode's sync signals,
	// unlike 800x600 where both are positive. The base program's side-set
	// drives its pin high during a pulse, so horizontal sync needs its pad
	// inverted; vertical sync is driven by the CPU and simply follows this.
	positiveHSync = false
	positiveVSync = false
)

// Cycle costs of the base program's routines, counted from the routine's first
// instruction up to and including the "out pc, 5" that dispatches the next
// command. The output routine's last pixel is a cycle short and its wrap back
// to the dispatch instruction costs one, so it runs one cycle longer than the
// pixels alone; the back porch is shortened by one to match.
func syncRoutineCycles(n int) int { return n + 3 }
func darkRoutineCycles(n int) int { return n + 4 }
func outputRoutineCycles() int    { return fbWidth*cpp + 1 }

func syncWord() uint32 {
	return picovga.Cmd(picovga.BaseOffset+picovga.BaseSync, hSync*clocksPerPixel-3)
}

func backPorchWord() uint32 {
	return picovga.Dark16Cmd(hBack*clocksPerPixel-4-1, 0)
}

func outputWord() uint32 {
	return picovga.Cmd(picovga.BaseOffset+picovga.BaseOutput, fbWidth-2)
}

func frontPorchWord() uint32 {
	return picovga.Dark16Cmd(hFront*clocksPerPixel-4, 0)
}

// darkRun assembles the commands holding colour for the given number of cycles.
// A 16 bit control word has only an 11 bit counter, so a long run is split.
func darkRun(cycles int, colour uint16) []uint32 {
	const max = picovga.Dark16MaxCount + 4
	var words []uint32
	for cycles >= 4 {
		c := cycles
		if c > max {
			c = max
			if rem := cycles - c; rem < 4 {
				c -= 4 - rem
			}
		}
		words = append(words, picovga.Dark16Cmd(uint32(c-4), colour))
		cycles -= c
	}
	return words
}

// blankWords blanks everything in a line after the sync pulse.
func blankWords() []uint32 {
	return darkRun(lineCycles-hSync*clocksPerPixel, 0)
}

// blankLineWords is a whole scanline with nothing visible on it.
func blankLineWords() []uint32 {
	return append([]uint32{syncWord()}, blankWords()...)
}

// wordCycles returns how long a control word runs for, by the routine it jumps
// to.
func wordCycles(w uint32) int {
	switch uint8(w >> 27) {
	case picovga.BaseOffset + picovga.BaseSync:
		return syncRoutineCycles(int(w & 0x07ff_ffff))
	case picovga.BaseOffset + picovga.BaseDark:
		return darkRoutineCycles(int((w >> 16) & picovga.Dark16MaxCount))
	case picovga.BaseOffset + picovga.BaseOutput:
		return outputRoutineCycles()
	}
	return 0
}

// lineCyclesOf totals the time a composed scanline occupies. A line that is not
// exactly lineCycles long will not hold a steady picture.
func lineCyclesOf(words []uint32) int {
	cycles := 0
	for _, w := range words {
		cycles += wordCycles(w)
	}
	return cycles
}

// inVSync reports whether the vertical sync pulse is asserted on a scanline.
func inVSync(line int) bool { return line < vSync }

// visibleLine maps a scanline to a framebuffer row, reporting false during the
// blanking intervals.
func visibleLine(line int) (int, bool) {
	top := vSync + vBack
	if line < top || line >= top+vVisible {
		return 0, false
	}
	return (line - top) / scale, true
}
