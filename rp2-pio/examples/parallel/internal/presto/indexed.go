package presto

import "sync/atomic"

// FrameCount is the number of frames RunIndexed has completely scanned out.
// It is stored once per frame, at the timing SM's IRQ0 (first vertical-blank
// line), so a change marks the start of vertical blank. Zero means scanout is
// not yet established. It only increases while RunIndexed runs.
var FrameCount atomic.Uint32

// WaitFrame spins until count differs from last and returns the new value.
// It never yields, so a caller that owns a core keeps it.
func WaitFrame(count *atomic.Uint32, last uint32) uint32 {
	for {
		if n := count.Load(); n != last {
			return n
		}
	}
}

// IndexedFrame is allocated by the caller in internal SRAM. RunIndexed only
// reads it. Writes during scanout are not synchronized and can tear on screen;
// presto-teapot deliberately accepts that.
type IndexedFrame [Width * Height]uint8

// Palette entries are made with PackPixels. Keep the palette in internal SRAM
// and immutable while RunIndexed is running.
type Palette [256]uint32

func (f *IndexedFrame) Row(row int) []uint8 {
	if row < 0 || row >= Height {
		panic("presto: row out of bounds")
	}
	return f[row*Width : (row+1)*Width : (row+1)*Width]
}

// Expand runs from RAM on TinyGo (arm.ld copies .ramfuncs with .data) so that
// XIP cache misses caused by the other core cannot delay the scanline pump.
//
//go:section .ramfuncs
func (f *IndexedFrame) Expand(dst *Line, row int, palette *Palette) {
	src := (*[Width]uint8)(f.Row(row))
	out := dst[:]
	for i := range out {
		// PIO reverses ISR before driving pins: the upper half is sent first.
		out[i] = palette[src[2*i]]&0xffff0000 | palette[src[2*i+1]]&0x0000ffff
	}
}
