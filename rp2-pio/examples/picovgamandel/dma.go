//go:build rp2040 || rp2350

package main

import (
	"device/rp"
	"runtime/volatile"
	"unsafe"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

// A DMA channel, kept local to this example so that changing it cannot affect
// anything else. Pixels move faster than the CPU can write them, so the channel
// is started per scanline and polled.

// dmaChannelHW mirrors the per-channel register block; the trailing registers
// are alias windows, unused here.
type dmaChannelHW struct {
	READ_ADDR   volatile.Register32
	WRITE_ADDR  volatile.Register32
	TRANS_COUNT volatile.Register32
	CTRL_TRIG   volatile.Register32
	_           [12]volatile.Register32
}

type dmaChannel struct {
	hw  *dmaChannelHW
	idx uint8
}

func dmaChannelAt(idx uint8) dmaChannel {
	channels := (*[12]dmaChannelHW)(unsafe.Pointer(rp.DMA))
	return dmaChannel{hw: &channels[idx], idx: idx}
}

// txDREQ is the request signal pacing writes into a state machine's transmit
// FIFO. Each PIO block owns eight, four transmit then four receive.
func txDREQ(sm pio.StateMachine) uint32 {
	return uint32(sm.PIO().BlockIndex())*8 + uint32(sm.StateMachineIndex())
}

// ctrl builds the control word: 32 bits at a time, reading forwards through
// memory and writing repeatedly to one FIFO register, paced by dreq and byte
// swapped.
//
// The byte swap is what puts pixels on screen in the order they are stored. The
// state machine shifts its output register left, taking the most significant
// half of each word first, while a little endian framebuffer holds the leftmost
// pixel of each pair in the low half.
func (ch dmaChannel) ctrl(dreq uint32) uint32 {
	return rp.DMA_CH0_CTRL_TRIG_EN |
		uint32(2)<<rp.DMA_CH0_CTRL_TRIG_DATA_SIZE_Pos | // 32 bit transfers
		rp.DMA_CH0_CTRL_TRIG_INCR_READ |
		rp.DMA_CH0_CTRL_TRIG_BSWAP |
		rp.DMA_CH0_CTRL_TRIG_HIGH_PRIORITY |
		uint32(ch.idx)<<rp.DMA_CH0_CTRL_TRIG_CHAIN_TO_Pos | // chain to self means no chain
		dreq<<rp.DMA_CH0_CTRL_TRIG_TREQ_SEL_Pos
}

func (ch dmaChannel) busy() bool {
	return ch.hw.CTRL_TRIG.Get()&rp.DMA_CH0_CTRL_TRIG_BUSY != 0
}

// start begins transferring a framebuffer row, two pixels to a word, and
// returns immediately.
func (ch dmaChannel) start(dst *volatile.Register32, row []uint16, ctrl uint32) {
	ch.hw.CTRL_TRIG.Set(0) // disable before touching the addresses
	ch.hw.READ_ADDR.Set(uint32(uintptr(unsafe.Pointer(&row[0]))))
	ch.hw.WRITE_ADDR.Set(uint32(uintptr(unsafe.Pointer(dst))))
	ch.hw.TRANS_COUNT.Set(uint32(len(row) / 2))
	ch.hw.CTRL_TRIG.Set(ctrl)
}

// wait spins until the transfer completes. It does not yield: a scanline has to
// be handed over without a pause long enough to starve the FIFO.
func (ch dmaChannel) wait() {
	for ch.busy() {
	}
}
