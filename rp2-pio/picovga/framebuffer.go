package picovga

// RGB packs a colour into the eight bits the base program drives onto its
// output pins: three bits of red, three of green and two of blue. The
// components are taken from the low bits of each argument.
//
// This is the arrangement a 3-3-2 resistor ladder converts, and matches the
// pin order the base program writes, red on the most significant bits.
func RGB(r, g, b uint8) uint8 {
	return r&7<<5 | g&7<<2 | b&3
}

// Colours available exactly in the 3-3-2 encoding.
var (
	Black   = RGB(0, 0, 0)
	White   = RGB(7, 7, 3)
	Red     = RGB(7, 0, 0)
	Green   = RGB(0, 7, 0)
	Blue    = RGB(0, 0, 3)
	Yellow  = RGB(7, 7, 0)
	Cyan    = RGB(0, 7, 3)
	Magenta = RGB(7, 0, 3)
)

// Framebuffer is an 8 bit per pixel image, one byte per pixel in the encoding
// RGB produces. Rows are contiguous and stored top to bottom, so a row can be
// handed to DMA directly.
//
// Drawing operations clip to the framebuffer, so coordinates outside it are
// discarded rather than panicking.
type Framebuffer struct {
	// Pix holds the pixels, Width*Height bytes, row by row.
	Pix           []uint8
	Width, Height int
}

// NewFramebuffer returns a framebuffer of the given size, filled with black.
// It panics if either dimension is not positive.
func NewFramebuffer(width, height int) *Framebuffer {
	if width <= 0 || height <= 0 {
		panic("picovga: framebuffer dimensions must be positive")
	}
	return &Framebuffer{
		Pix:    make([]uint8, width*height),
		Width:  width,
		Height: height,
	}
}

// NewFramebufferForMode returns a framebuffer sized for the mode.
func NewFramebufferForMode(m Mode) *Framebuffer {
	return NewFramebuffer(m.Width(), m.Height())
}

// Line returns row y. The slice aliases the framebuffer, so writes to it are
// visible on screen. It returns nil if y is outside the framebuffer.
func (f *Framebuffer) Line(y int) []uint8 {
	if y < 0 || y >= f.Height {
		return nil
	}
	return f.Pix[y*f.Width : (y+1)*f.Width]
}

// Pixel returns the colour at x, y, or Black outside the framebuffer.
func (f *Framebuffer) Pixel(x, y int) uint8 {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return Black
	}
	return f.Pix[y*f.Width+x]
}

// SetPixel sets the colour at x, y, ignoring coordinates outside the
// framebuffer.
func (f *Framebuffer) SetPixel(x, y int, c uint8) {
	if x < 0 || x >= f.Width || y < 0 || y >= f.Height {
		return
	}
	f.Pix[y*f.Width+x] = c
}

// Fill sets every pixel to c.
func (f *Framebuffer) Fill(c uint8) {
	for i := range f.Pix {
		f.Pix[i] = c
	}
}

// clipRect narrows a rectangle to the framebuffer, reporting whether anything
// is left of it.
func (f *Framebuffer) clipRect(x, y, w, h int) (int, int, int, int, bool) {
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
func (f *Framebuffer) FillRect(x, y, w, h int, c uint8) {
	x, y, w, h, ok := f.clipRect(x, y, w, h)
	if !ok {
		return
	}
	for row := y; row < y+h; row++ {
		line := f.Pix[row*f.Width+x : row*f.Width+x+w]
		for i := range line {
			line[i] = c
		}
	}
}

// HLine draws a horizontal run of w pixels starting at x, y.
func (f *Framebuffer) HLine(x, y, w int, c uint8) { f.FillRect(x, y, w, 1, c) }

// VLine draws a vertical run of h pixels starting at x, y.
func (f *Framebuffer) VLine(x, y, h int, c uint8) { f.FillRect(x, y, 1, h, c) }

// Rect draws the outline of a w by h rectangle whose top left corner is x, y.
func (f *Framebuffer) Rect(x, y, w, h int, c uint8) {
	if w <= 0 || h <= 0 {
		return
	}
	f.HLine(x, y, w, c)
	f.HLine(x, y+h-1, w, c)
	f.VLine(x, y, h, c)
	f.VLine(x+w-1, y, h, c)
}
