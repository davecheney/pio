//go:build rp2040 || rp2350

package picovga

import (
	"errors"
	"machine"
	"runtime/volatile"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

// ColourPins is the number of colour pins the 16 bit base program drives.
const ColourPins = 16

var (
	// ErrModePixelBits reports a mode that is not 16 bits per pixel.
	ErrModePixelBits = errors.New("picovga: driver needs a 16 bit per pixel mode")
	// ErrFramebufferSize reports a framebuffer that does not match the mode.
	ErrFramebufferSize = errors.New("picovga: framebuffer does not match the mode")
)

// Config describes how the VGA hardware is wired up.
type Config struct {
	// PIO is the block to run the base program on.
	PIO *pio.PIO
	// ColourBase is the first of ColourPins consecutive colour pins.
	ColourBase machine.Pin
	// HSync is driven by the state machine's side-set.
	HSync machine.Pin
	// VSync is driven by the CPU, the base program having only one side-set bit.
	VSync machine.Pin
	// DMAChannel carries pixels to the state machine.
	DMAChannel uint8

	// SkipColourPins marks colour pin offsets that must not be handed to the
	// PIO, bit n standing for ColourBase+n. A pad only follows the state
	// machine once its function select points at the PIO, so a pin left out
	// here keeps whatever function it already had and is undisturbed by video
	// output, even though the state machine still writes a bit for it.
	SkipColourPins uint16
}

// PimoroniVGA is the wiring of the Pimoroni Pico VGA Demo Base, which follows
// the RP2040 VGA reference design: red on GP0 to GP4, green on GP6 to GP10 and
// blue on GP11 to GP15, with sync on GP16 and GP17. GP5 sits inside the colour
// range but drives the SD card clock rather than the resistor ladder, so the
// colour bit at that position is always written low.
var PimoroniVGA = Config{
	PIO:        pio.PIO0,
	ColourBase: machine.GP0,
	HSync:      machine.GP16,
	VSync:      machine.GP17,
	DMAChannel: 0,
	// GP5 lies inside the colour range but drives the SD card clock, so it is
	// left alone. Colours never set that bit either, so the state machine
	// writes a constant zero to a pin it does not own.
	SkipColourPins: 1 << 5,
}

// VGA drives a framebuffer to a VGA display using the base program and one DMA
// channel. The CPU issues a few control words per scanline and DMA streams the
// pixels, so the picture costs the processor very little.
type VGA struct {
	mode Mode
	fb   *Framebuffer16
	sm   pio.StateMachine
	dma  dmaChannel
	ctrl uint32
	txf  *volatile.Register32

	vsync    machine.Pin
	vsyncPol bool

	sync, back, output, front uint32
	blank                     []uint32
}

// Setup claims a state machine, loads the 16 bit base program and configures
// the pins and clock for the mode, leaving it disabled.
//
// It is the part of NewVGA that has nothing to do with DMA, exported so a
// program can drive the state machine itself by writing control words. Pixel
// output needs DMA, but sync and flat colour do not.
func Setup(cfg Config, m Mode) (pio.StateMachine, error) {
	var sm pio.StateMachine
	if err := m.Validate(); err != nil {
		return sm, err
	}
	if m.PixelBits != 16 {
		return sm, ErrModePixelBits
	}
	prog, err := NewBase16(m.CPP())
	if err != nil {
		return sm, err
	}
	p := cfg.PIO
	offset, err := p.AddProgram(prog.Instructions, prog.Origin)
	if err != nil {
		return sm, err
	}
	sm, err = p.ClaimStateMachine()
	if err != nil {
		return sm, err
	}

	mode := p.PinMode()
	for i := 0; i < ColourPins; i++ {
		if cfg.SkipColourPins&(1<<uint(i)) != 0 {
			continue
		}
		(cfg.ColourBase + machine.Pin(i)).Configure(machine.PinConfig{Mode: mode})
		sm.SetPindirsConsecutive(cfg.ColourBase+machine.Pin(i), 1, true)
	}
	cfg.HSync.Configure(machine.PinConfig{Mode: mode})
	sm.SetPindirsConsecutive(cfg.HSync, 1, true)
	cfg.VSync.Configure(machine.PinConfig{Mode: machine.PinOutput})

	whole, frac, err := pio.ClkDivFromFrequency(m.PIOFrequency(), machine.CPUFrequency())
	if err != nil {
		return sm, err
	}

	smcfg := pio.DefaultStateMachineConfig()
	smcfg.SetWrap(offset+prog.WrapTarget, offset+prog.Wrap)
	smcfg.SetSidesetParams(1, false, false)
	smcfg.SetSidesetPins(cfg.HSync)
	smcfg.SetOutPins(cfg.ColourBase, ColourPins)
	// Control words and pixel pairs are both exactly 32 bits, so autopull at 32
	// refills the output register once per command or per two pixels.
	smcfg.SetOutShift(false, true, 32)
	// Joining the FIFOs gives eight words of slack instead of four.
	smcfg.SetFIFOJoin(pio.FifoJoinTx)
	smcfg.SetClkDivIntFrac(whole, frac)
	sm.Init(offset+prog.Entry, smcfg)
	return sm, nil
}

// NewVGA prepares the state machine, pins and DMA channel to display fb in the
// given mode. Call Start to begin output.
func NewVGA(cfg Config, m Mode, fb *Framebuffer16) (*VGA, error) {
	if fb.Width != m.Width() || fb.Height != m.Height() {
		return nil, ErrFramebufferSize
	}
	if len(fb.Pix) < fb.Width*fb.Height || fb.Width%2 != 0 {
		return nil, ErrFramebufferSize
	}
	if cfg.DMAChannel >= numDMAChannels {
		return nil, ErrDMAChannel
	}
	sm, err := Setup(cfg, m)
	if err != nil {
		return nil, err
	}

	dma := dmaChannelAt(cfg.DMAChannel)
	v := &VGA{
		mode:     m,
		fb:       fb,
		sm:       sm,
		dma:      dma,
		ctrl:     dma.ctrl(txDREQ(sm)),
		txf:      sm.TxReg(),
		vsync:    cfg.VSync,
		vsyncPol: m.PositiveVSync,
		sync:     m.SyncWord(),
		back:     m.BackPorchWord(),
		output:   m.OutputWord(),
		front:    m.FrontPorchWord(),
		blank:    m.BlankWords(),
	}
	return v, nil
}

// Start enables the state machine. Run must be called promptly afterwards, as
// the state machine stalls until it is given control words.
func (v *VGA) Start() {
	v.sm.SetEnabled(true)
}

// Framebuffer returns the framebuffer being displayed.
func (v *VGA) Framebuffer() *Framebuffer16 { return v.fb }

// put writes one control word, waiting for room in the FIFO.
func (v *VGA) put(w uint32) {
	for v.sm.IsTxFIFOFull() {
	}
	v.sm.TxPut(w)
}

// Frame outputs one complete frame, returning after the last scanline.
func (v *VGA) Frame() {
	m := v.mode
	for line := 0; line < m.VTotal(); line++ {
		v.vsync.Set(m.InVSync(line) == v.vsyncPol)
		row, visible := m.VisibleLine(line)
		if !visible {
			v.put(v.sync)
			for _, w := range v.blank {
				v.put(w)
			}
			continue
		}
		v.put(v.sync)
		v.put(v.back)
		v.put(v.output)
		// The pixels must reach the FIFO after the output command and before
		// the front porch, so the transfer is started here and waited for.
		v.dma.startPixels(v.txf, v.fb.Line(row), v.ctrl)
		v.dma.wait()
		v.put(v.front)
	}
}

// Run outputs frames forever.
func (v *VGA) Run() {
	for {
		v.Frame()
	}
}
