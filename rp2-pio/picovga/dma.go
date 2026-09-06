//go:build rp2040 || rp2350

package picovga

import (
	"device/rp"
	"errors"
	"runtime/volatile"
	"unsafe"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

// Video output needs a DMA channel to feed pixels to the state machine faster
// than the CPU can. The DMA support in piolib is not exported, so the little of
// it needed here is repeated: a single channel, started and polled by hand.

// numDMAChannels is the count available on the RP2040. The RP2350 has more, but
// there is no need to reach them.
const numDMAChannels = 12

// ErrDMAChannel reports a DMA channel index outside the usable range.
var ErrDMAChannel = errors.New("picovga: invalid DMA channel")

// dmaChannelHW mirrors the per-channel register block. The trailing registers
// are the alias windows, which are unused here.
type dmaChannelHW struct {
	READ_ADDR   volatile.Register32
	WRITE_ADDR  volatile.Register32
	TRANS_COUNT volatile.Register32
	CTRL_TRIG   volatile.Register32
	_           [12]volatile.Register32
}

// dmaChannel is one DMA channel, addressed by index.
type dmaChannel struct {
	hw  *dmaChannelHW
	idx uint8
}

// dmaChannelAt returns the channel with the given index.
func dmaChannelAt(idx uint8) dmaChannel {
	channels := (*[numDMAChannels]dmaChannelHW)(unsafe.Pointer(rp.DMA))
	return dmaChannel{hw: &channels[idx], idx: idx}
}

// txDREQ returns the data request signal that paces writes into a state
// machine's transmit FIFO. Each PIO block owns eight DREQ numbers, four for
// transmit followed by four for receive.
func txDREQ(sm pio.StateMachine) uint32 {
	return uint32(sm.PIO().BlockIndex())*8 + uint32(sm.StateMachineIndex())
}

// ctrl builds the control word for pixel transfers: 32 bits at a time, reading
// forwards through memory and writing repeatedly to one FIFO register, paced by
// dreq and byte swapped.
//
// The byte swap is what puts pixels on screen in the order they are stored. The
// state machine shifts its output register left, taking the most significant
// byte of each word first, while a little endian framebuffer puts the leftmost
// pixel of each group of four in the least significant byte.
func (ch dmaChannel) ctrl(dreq uint32) uint32 {
	return rp.DMA_CH0_CTRL_TRIG_EN |
		uint32(2)<<rp.DMA_CH0_CTRL_TRIG_DATA_SIZE_Pos | // 32 bit transfers
		rp.DMA_CH0_CTRL_TRIG_INCR_READ |
		rp.DMA_CH0_CTRL_TRIG_BSWAP |
		rp.DMA_CH0_CTRL_TRIG_HIGH_PRIORITY |
		uint32(ch.idx)<<rp.DMA_CH0_CTRL_TRIG_CHAIN_TO_Pos | // chain to self, meaning no chain
		dreq<<rp.DMA_CH0_CTRL_TRIG_TREQ_SEL_Pos
}

// busy reports whether a transfer is still in flight.
func (ch dmaChannel) busy() bool {
	return ch.hw.CTRL_TRIG.Get()&rp.DMA_CH0_CTRL_TRIG_BUSY != 0
}

// start begins transferring words 32 bit words from src to dst and returns
// immediately.
func (ch dmaChannel) start(dst *volatile.Register32, src unsafe.Pointer, words int, ctrl uint32) {
	ch.hw.CTRL_TRIG.Set(0) // disable before touching the addresses
	ch.hw.READ_ADDR.Set(uint32(uintptr(src)))
	ch.hw.WRITE_ADDR.Set(uint32(uintptr(unsafe.Pointer(dst))))
	ch.hw.TRANS_COUNT.Set(uint32(words))
	ch.hw.CTRL_TRIG.Set(ctrl)
}

// startPixels begins transferring a framebuffer row, which holds two pixels per
// 32 bit word.
func (ch dmaChannel) startPixels(dst *volatile.Register32, row []uint16, ctrl uint32) {
	ch.start(dst, unsafe.Pointer(&row[0]), len(row)/2, ctrl)
}

// wait spins until the transfer completes. It does not yield, because a video
// line has to be handed over without a pause long enough to starve the FIFO.
func (ch dmaChannel) wait() {
	for ch.busy() {
	}
}
