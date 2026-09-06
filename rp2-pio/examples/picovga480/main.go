//go:build rp2040 || rp2350

// Command picovga480 displays a framebuffer at 640x480 over VGA on the Pimoroni
// Pico VGA Demo Base, which implements the RP2040 VGA reference design.
//
// The picture is a 320x240 framebuffer of 16 bit RGB555 pixels, doubled to fill
// the screen. Unlike the 800x600 examples, VESA specifies negative polarity for
// both of this mode's sync signals: vertical sync is driven by the CPU and
// simply inverted, while horizontal sync comes from the base program's
// side-set, which drives its pin high for the length of a pulse, so that pad is
// inverted instead of assembling a second program.
//
// This example deliberately keeps its own timings, state machine setup and DMA
// rather than sharing the picovga package's driver, so that work on it cannot
// disturb the 800x600 path. The only things shared are the base program, the
// control word encoding that belongs with it, and the framebuffer.
//
// Note the framebuffer is 150KB. That fits an RP2350 easily but is close to the
// limit of what will run on an RP2040, where the runtime needs more room than
// the link report suggests. If the picture comes up black on an RP2040, raise
// scale to 4 in mode.go for a 160x120 framebuffer.
//
// Wiring is the Demo Base's own: nothing to connect beyond seating the board.
package main

import (
	"machine"

	pio "github.com/tinygo-org/pio/rp2-pio"
	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// Pin assignment of the RP2040 VGA reference design: red on GP0 to GP4, green
// on GP6 to GP10, blue on GP11 to GP15, sync on GP16 and GP17. GP5 lies inside
// the colour range but drives the SD card clock, so it is never handed to the
// PIO; a pad only follows a state machine once its function select points at
// it, so the bit written for it goes nowhere.
const (
	colourBase = machine.GP0
	colourPins = 16
	skipPin    = 5
	hSyncPin   = machine.GP16
	vSyncPin   = machine.GP17
	dmaChan    = 0
)

// The framebuffer is a package level array so it lands in BSS rather than the
// heap.
var pixels [fbWidth * fbHeight]uint16

func main() {
	fb := &picovga.Framebuffer16{
		Pix:    pixels[:],
		Width:  fbWidth,
		Height: fbHeight,
	}
	draw(fb)

	sm := start()
	dma := dmaChannelAt(dmaChan)
	ctrl := dma.ctrl(txDREQ(sm))
	txf := sm.TxReg()

	sync, back, out, front := syncWord(), backPorchWord(), outputWord(), frontPorchWord()
	blank := blankLineWords()

	put := func(w uint32) {
		for sm.IsTxFIFOFull() {
		}
		sm.TxPut(w)
	}

	sm.SetEnabled(true)
	for {
		for line := 0; line < vTotal; line++ {
			vSyncPin.Set(inVSync(line) == positiveVSync)
			row, visible := visibleLine(line)
			if !visible {
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
			dma.start(txf, fb.Line(row), ctrl)
			dma.wait()
			put(front)
		}
	}
}

// start loads the base program and configures the state machine, pins and clock
// for this mode, leaving it disabled.
func start() pio.StateMachine {
	prog, err := picovga.NewBase16(cpp)
	if err != nil {
		panic(err)
	}
	p := pio.PIO0
	offset, err := p.AddProgram(prog.Instructions, prog.Origin)
	if err != nil {
		panic(err)
	}
	sm, err := p.ClaimStateMachine()
	if err != nil {
		panic(err)
	}

	mode := p.PinMode()
	for i := 0; i < colourPins; i++ {
		if i == skipPin {
			continue
		}
		pin := colourBase + machine.Pin(i)
		pin.Configure(machine.PinConfig{Mode: mode})
		sm.SetPindirsConsecutive(pin, 1, true)
	}
	hSyncPin.Configure(machine.PinConfig{Mode: mode})
	// Configure rewrites the pad's control register, so the polarity override
	// has to come after it.
	setPinInvert(hSyncPin, !positiveHSync)
	sm.SetPindirsConsecutive(hSyncPin, 1, true)
	vSyncPin.Configure(machine.PinConfig{Mode: machine.PinOutput})

	whole, frac, err := pio.ClkDivFromFrequency(pioFreq, machine.CPUFrequency())
	if err != nil {
		panic(err)
	}

	cfg := pio.DefaultStateMachineConfig()
	cfg.SetWrap(offset+prog.WrapTarget, offset+prog.Wrap)
	cfg.SetSidesetParams(1, false, false)
	cfg.SetSidesetPins(hSyncPin)
	cfg.SetOutPins(colourBase, colourPins)
	// Control words and pixel pairs are both exactly 32 bits, so autopull at 32
	// refills the output register once per command or per two pixels.
	cfg.SetOutShift(false, true, 32)
	// Joining the FIFOs gives eight words of slack instead of four.
	cfg.SetFIFOJoin(pio.FifoJoinTx)
	cfg.SetClkDivIntFrac(whole, frac)
	sm.Init(offset+prog.Entry, cfg)
	return sm
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
