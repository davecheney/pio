package presto

// IndexedFrame is allocated by the caller in internal SRAM. RunIndexed only
// reads it; callers must not modify it while scanout is active.
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

func (f *IndexedFrame) Expand(dst *Line, row int, palette *Palette) {
	src := (*[Width]uint8)(f.Row(row))
	out := dst[:]
	for i := range out {
		// PIO reverses ISR before driving pins: the upper half is sent first.
		out[i] = palette[src[2*i]]&0xffff0000 | palette[src[2*i+1]]&0x0000ffff
	}
}
