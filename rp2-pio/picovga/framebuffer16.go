package picovga

// Colour is a pixel for the RP2040 VGA reference design, whose resistor ladder
// takes five bits per channel: red on GP0 to GP4, green on GP6 to GP10 and blue
// on GP11 to GP15. The bit positions here are the pin positions, so bit 5,
// which drives the SD card clock rather than the ladder, is always zero.
type Colour uint16

// RGB555 packs five bits per channel into a Colour. Components are taken from
// the low five bits of each argument.
func RGB555(r, g, b uint8) Colour {
	return Colour(uint16(r&0x1f) | uint16(g&0x1f)<<6 | uint16(b&0x1f)<<11)
}

// Red, Green and Blue return a Colour's channels.
func (c Colour) Red() uint8   { return uint8(c) & 0x1f }
func (c Colour) Green() uint8 { return uint8(c>>6) & 0x1f }
func (c Colour) Blue() uint8  { return uint8(c>>11) & 0x1f }

// Colours available exactly in RGB555.
const (
	Black555   = Colour(0)
	White555   = Colour(0x1f | 0x1f<<6 | 0x1f<<11)
	Red555     = Colour(0x1f)
	Green555   = Colour(0x1f << 6)
	Blue555    = Colour(0x1f << 11)
	Yellow555  = Colour(0x1f | 0x1f<<6)
	Cyan555    = Colour(0x1f<<6 | 0x1f<<11)
	Magenta555 = Colour(0x1f | 0x1f<<11)
)

// swap16 exchanges a pixel's two bytes.
func swap16(v uint16) uint16 { return v>>8 | v<<8 }

// Framebuffer16 is an image of Colour pixels, stored ready for DMA.
//
// Pixels are held with their bytes exchanged. DMA moves two pixels per 32 bit
// word, and the state machine's output register shifts left, taking the most
// significant 16 bits of each word first. On a little endian machine that would
// emit each pair of pixels in the wrong order, so the transfer runs with byte
// swapping turned on; swapping the whole word puts the pairs right but reverses
// the bytes within each pixel, and storing pixels pre-swapped cancels that out.
//
// SetPixel and Pixel do the exchange, so callers work in ordinary Colour
// values; only Line, which exists to be handed to DMA, exposes the stored form.
//
// Drawing operations clip to the framebuffer.
type Framebuffer16 struct {
	// Pix holds the pixels in stored, byte exchanged form, row by row.
	Pix           []uint16
	Width, Height int
}

// NewFramebuffer16 returns a framebuffer of the given size, filled with black.
// The width must be even, since DMA moves pixels two at a time. It panics if
// the dimensions are not usable.
func NewFramebuffer16(width, height int) *Framebuffer16 {
	if width <= 0 || height <= 0 {
		panic("picovga: framebuffer dimensions must be positive")
	}
	if width%2 != 0 {
		panic("picovga: framebuffer width must be even")
	}
	return &Framebuffer16{
		Pix:    make([]uint16, width*height),
		Width:  width,
		Height: height,
	}
}

// NewFramebuffer16ForMode returns a framebuffer sized for the mode.
func NewFramebuffer16ForMode(m Mode) *Framebuffer16 {
	return NewFramebuffer16(m.Width(), m.Height())
}

// Line returns row y in stored form, ready to hand to DMA. The slice aliases
// the framebuffer. It returns nil if y is outside the framebuffer.
func (f *Framebuffer16) Line(y int) []uint16 {
	if y < 0 || y >= f.Height {
		return nil
	}
	return f.Pix[y*f.Width : (y+1)*f.Width]
}

// Pixel returns the colour at x, y, or black outside the framebuffer.
func (f *Framebuffer16) Pixel(x, y int) Colour {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return Black555
	}
	return Colour(swap16(f.Pix[y*f.Width+x]))
}

// SetPixel sets the colour at x, y, ignoring coordinates outside the
// framebuffer.
func (f *Framebuffer16) SetPixel(x, y int, c Colour) {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return
	}
	f.Pix[y*f.Width+x] = swap16(uint16(c))
}

// Fill sets every pixel to c.
func (f *Framebuffer16) Fill(c Colour) {
	stored := swap16(uint16(c))
	for i := range f.Pix {
		f.Pix[i] = stored
	}
}

// clipRect narrows a rectangle to the framebuffer, reporting whether anything
// is left of it.
func (f *Framebuffer16) clipRect(x, y, w, h int) (int, int, int, int, bool) {
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > f.Width {
		w = f.Width - x
	}
	if y+h > f.Height {
		h = f.Height - y
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0, false
	}
	return x, y, w, h, true
}

// FillRect fills a w by h rectangle whose top left corner is x, y.
func (f *Framebuffer16) FillRect(x, y, w, h int, c Colour) {
	x, y, w, h, ok := f.clipRect(x, y, w, h)
	if !ok {
		return
	}
	stored := swap16(uint16(c))
	for row := y; row < y+h; row++ {
		line := f.Pix[row*f.Width+x : row*f.Width+x+w]
		for i := range line {
			line[i] = stored
		}
	}
}

// HLine draws a horizontal run of w pixels starting at x, y.
func (f *Framebuffer16) HLine(x, y, w int, c Colour) { f.FillRect(x, y, w, 1, c) }

// VLine draws a vertical run of h pixels starting at x, y.
func (f *Framebuffer16) VLine(x, y, h int, c Colour) { f.FillRect(x, y, 1, h, c) }

// Rect draws the outline of a w by h rectangle whose top left corner is x, y.
func (f *Framebuffer16) Rect(x, y, w, h int, c Colour) {
	if w <= 0 || h <= 0 {
		return
	}
	f.HLine(x, y, w, c)
	f.HLine(x, y+h-1, w, c)
	f.VLine(x, y, h, c)
	f.VLine(x+w-1, y, h, c)
}
