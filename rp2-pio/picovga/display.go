//go:build rp2040 || rp2350

package picovga

import (
	"machine"
	"runtime/volatile"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

// Display drives a VGA signal one scanline at a time.
//
// It owns everything derived or exacting: which program to assemble and where
// to load it, how many clock cycles a pixel takes, the clock divider, the pin
// configuration and sync polarity, the DMA channel, and the order of words
// within a scanline. What it does not own is the loop, because the caller does.
//
// A scanline is a sync pulse, a back porch, an output command, the pixels, and
// a front porch, all reaching the state machine through one FIFO, and their
// order is the scanline. DMA carries only the pixels, so the words around them
// have to be written by something; that something is a loop, and it occupies a
// core for as long as the picture is up:
//
//	d.Start()
//	for {
//		for n := 0; n < d.Scanlines(); n++ {
//			var px []uint16
//			if row, ok := d.Row(n); ok {
//				px = fb.Line(row)
//			}
//			d.Scanline(n, px)
//		}
//	}
//
// Keeping the loop in the caller is what lets a row be produced rather than
// stored: it may be a slice of a framebuffer, a line expanded from palette
// indices, or generated on the spot from a character map or a tile map. A
// display driven entirely by chained DMA would cost no core but could only ever
// show what was already in memory.
type Display struct {
	mode Mode
	sm   pio.StateMachine
	dma  dmaChannel
	ctrl uint32
	txf  *volatile.Register32

	vsync    machine.Pin
	vsyncPol bool

	sync, back, output, front uint32
	blank                     []uint32

	// pending records whether a pixel transfer is still to be waited for.
	pending bool
}

// New prepares a display for the given mode, whose scale factors decide how
// large a framebuffer it wants. It reports an error for a combination the
// hardware cannot produce rather than leaving it to show as a picture that will
// not hold.
func New(cfg Config, m Mode) (*Display, error) {
	if cfg.DMAChannel >= numDMAChannels {
		return nil, ErrDMAChannel
	}
	sm, err := Setup(cfg, m)
	if err != nil {
		return nil, err
	}
	dma := dmaChannelAt(cfg.DMAChannel)
	return &Display{
		mode:     m,
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
	}, nil
}

// Mode returns the mode being displayed.
func (d *Display) Mode() Mode { return d.mode }

// Width and Height are the framebuffer size this display wants, derived from
// the mode and its scale, so that a framebuffer cannot disagree with the
// display it is drawn for.
func (d *Display) Width() int  { return d.mode.Width() }
func (d *Display) Height() int { return d.mode.Height() }

// Scanlines is the number of scanlines in a frame, blanking included. A loop
// must emit every one of them, in order.
func (d *Display) Scanlines() int { return d.mode.VTotal() }

// Row maps a scanline to the framebuffer row it shows, reporting false for the
// scanlines that fall in the blanking intervals.
func (d *Display) Row(n int) (int, bool) { return d.mode.VisibleLine(n) }

// Start enables the state machine. Scanline must be called promptly and
// steadily afterwards: the state machine stalls if its FIFO runs dry, which
// shows as a display that will not lock.
func (d *Display) Start() { d.sm.SetEnabled(true) }

// put writes one control word, waiting for room in the FIFO.
func (d *Display) put(w uint32) {
	for d.sm.IsTxFIFOFull() {
	}
	d.sm.TxPut(w)
}

// Scanline emits scanline n. px holds the pixels for a visible line and must be
// Width() words long; nil blanks the line, which is what the scanlines outside
// the visible area want.
//
// It returns once the pixels have reached the FIFO, not once they have been
// displayed.
func (d *Display) Scanline(n int, px []uint16) {
	d.ScanlineBegin(n, px)
	d.ScanlineEnd()
}

// ScanlineBegin emits a scanline's control words and starts its pixels on their
// way, without waiting for them. ScanlineEnd must be called before the next
// scanline is begun.
//
// The pair exists so that a caller with something to do can do it while the
// transfer is in flight, which is otherwise time spent waiting. Converting a
// row of palette indices into colours is the case it was added for. A caller
// with nothing to do should use Scanline.
func (d *Display) ScanlineBegin(n int, px []uint16) {
	d.vsync.Set(d.mode.InVSync(n) == d.vsyncPol)
	if px == nil {
		d.put(d.sync)
		for _, w := range d.blank {
			d.put(w)
		}
		d.pending = false
		return
	}
	d.put(d.sync)
	d.put(d.back)
	d.put(d.output)
	// The pixels must reach the FIFO after the output command and before the
	// front porch: all three go through the same FIFO and their order is the
	// scanline.
	d.dma.startPixels(d.txf, px, d.ctrl)
	d.pending = true
}

// ScanlineEnd waits for the pixels begun by ScanlineBegin and closes the
// scanline. It does nothing after a blanked line, which has no transfer.
func (d *Display) ScanlineEnd() {
	if !d.pending {
		return
	}
	d.dma.wait()
	d.put(d.front)
	d.pending = false
}
