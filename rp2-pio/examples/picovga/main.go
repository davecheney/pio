//go:build rp2040 || rp2350

// Command picovga displays a colour bar test pattern over VGA, driving the
// signal entirely from the PicoVGA base PIO program.
//
// The mode is 800x600 at 60Hz. That mode is used rather than 640x480 because
// VESA specifies positive polarity for both of its sync signals, which is what
// the base program's side-set produces directly; 640x480 needs negative sync,
// and inverting it means writing the pad control registers, which sit at
// different bit positions on RP2040 and RP2350.
//
// The pattern is drawn with the base program's sync and dark routines alone.
// The dark routine holds one colour on the output pins for a given number of
// cycles, so a row of colour bars is just a run of dark commands. Nothing here
// uses the pixel output routine, so no framebuffer and no DMA are needed; the
// CPU keeps the FIFO fed with a handful of control words per scanline.
//
// Wiring, following PicoVGA's default pin assignment:
//
//	GP0..GP7  eight colour bits through a resistor ladder, 3 red, 3 green,
//	          2 blue, to the VGA red, green and blue pins
//	GP8       horizontal sync, to VGA pin 13
//	GP9       vertical sync, to VGA pin 14
//
// VGA inputs are 0.7V into 75 ohms. Drive each colour pin through a resistor
// ladder rather than connecting it directly, and tie the VGA ground pins to
// board ground. The sync pins are 5V-tolerant TTL inputs and connect directly.
package main

import (
	"machine"

	pio "github.com/tinygo-org/pio/rp2-pio"
	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// Pin assignment. The colour pins must be consecutive starting at colourBase.
const (
	colourBase = machine.GP0
	colourBits = 8
	syncPin    = machine.GP8
	vsyncPin   = machine.GP9
)

// VESA 800x600 at 60Hz, in pixels. Both sync signals are positive polarity.
const (
	pixelClock = 40_000_000

	hVisible = 800
	hFront   = 40
	hSync    = 128
	hBack    = 88
	hTotal   = hVisible + hFront + hSync + hBack

	vVisible = 600
	vFront   = 1
	vSync    = 4
	vBack    = 23
	vTotal   = vVisible + vFront + vSync + vBack

	// cpp is the number of PIO clock cycles per pixel. At three cycles the
	// state machine runs at 120MHz, which divides exactly from the RP2350's
	// 150MHz default system clock.
	cpp = 3
)

// bars is the number of colour bars drawn across the visible part of a line.
const bars = 8

// rgb packs a colour into the eight bits the resistor ladder converts: three
// bits of red, three of green and two of blue.
func rgb(r, g, b uint8) uint8 {
	return r<<5 | g<<2 | b
}

// barColours are drawn left to right, in descending luminance.
var barColours = [bars]uint8{
	rgb(7, 7, 3), // white
	rgb(7, 7, 0), // yellow
	rgb(0, 7, 3), // cyan
	rgb(0, 7, 0), // green
	rgb(7, 0, 3), // magenta
	rgb(7, 0, 0), // red
	rgb(0, 0, 3), // blue
	rgb(0, 0, 0), // black
}

// syncCmd builds a control word running the sync routine for cycles PIO clocks.
func syncCmd(cycles uint32) uint32 {
	return picovga.Cmd(picovga.BaseOffset+picovga.BaseSync, cycles-3)
}

// darkCmd builds a control word holding colour on the output pins for cycles
// PIO clocks.
func darkCmd(cycles uint32, colour uint8) uint32 {
	return picovga.DarkCmd(cycles-4, colour)
}

// Line timings in PIO clock cycles.
const (
	syncCycles  = hSync * cpp
	backCycles  = hBack * cpp
	frontCycles = hFront * cpp
	barCycles   = (hVisible / bars) * cpp
	// blankCycles covers everything in a line after the sync pulse.
	blankCycles = (hTotal - hSync) * cpp
)

// visibleLine and blankLine are the control words for the two kinds of
// scanline. They are built once and then replayed for the life of the program.
var (
	visibleLine [3 + bars]uint32
	blankLine   [2]uint32
)

func buildLines() {
	visibleLine[0] = syncCmd(syncCycles)
	visibleLine[1] = darkCmd(backCycles, 0)
	for i, c := range barColours {
		visibleLine[2+i] = darkCmd(barCycles, c)
	}
	visibleLine[2+bars] = darkCmd(frontCycles, 0)

	blankLine[0] = syncCmd(syncCycles)
	blankLine[1] = darkCmd(blankCycles, 0)
}

// feed writes a scanline's control words, waiting whenever the FIFO is full.
// The state machine stalls if the FIFO ever runs dry, which shows up as a
// monitor that will not lock, so this loop must not be interrupted for long.
func feed(sm pio.StateMachine, words []uint32) {
	for _, w := range words {
		for sm.IsTxFIFOFull() {
		}
		sm.TxPut(w)
	}
}

func main() {
	buildLines()

	p := pio.PIO0
	prog, err := picovga.NewBase(cpp)
	if err != nil {
		panic(err)
	}
	// The base program is not relocatable: its control words carry absolute
	// jump addresses, so it must land at picovga.BaseOffset.
	offset, err := p.AddProgram(prog.Instructions, prog.Origin)
	if err != nil {
		panic(err)
	}
	sm, err := p.ClaimStateMachine()
	if err != nil {
		panic(err)
	}

	mode := p.PinMode()
	for i := 0; i < colourBits; i++ {
		(colourBase + machine.Pin(i)).Configure(machine.PinConfig{Mode: mode})
	}
	syncPin.Configure(machine.PinConfig{Mode: mode})
	sm.SetPindirsConsecutive(colourBase, colourBits, true)
	sm.SetPindirsConsecutive(syncPin, 1, true)

	// Vertical sync is not part of the PIO program; the base program has a
	// single side-set bit and spends it on horizontal sync.
	vsyncPin.Configure(machine.PinConfig{Mode: machine.PinOutput})

	whole, frac, err := pio.ClkDivFromFrequency(pixelClock*cpp, machine.CPUFrequency())
	if err != nil {
		panic(err)
	}

	cfg := pio.DefaultStateMachineConfig()
	cfg.SetWrap(offset+prog.WrapTarget, offset+prog.Wrap)
	cfg.SetSidesetParams(1, false, false)
	cfg.SetSidesetPins(syncPin)
	cfg.SetOutPins(colourBase, colourBits)
	// Control words are consumed most significant bits first, and each is
	// exactly 32 bits, so autopull at 32 refills the OSR once per command.
	cfg.SetOutShift(false, true, 32)
	// Joining the FIFOs gives eight words of slack instead of four.
	cfg.SetFIFOJoin(pio.FifoJoinTx)
	cfg.SetClkDivIntFrac(whole, frac)

	sm.Init(offset+prog.Entry, cfg)
	sm.SetEnabled(true)

	for {
		for line := 0; line < vTotal; line++ {
			vsyncPin.Set(line < vSync)
			if line >= vSync+vBack && line < vSync+vBack+vVisible {
				feed(sm, visibleLine[:])
			} else {
				feed(sm, blankLine[:])
			}
		}
	}
}
