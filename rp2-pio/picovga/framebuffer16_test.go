package picovga

import "testing"

// bswap32 reverses the bytes of a word, as the DMA channel does when byte
// swapping is enabled.
func bswap32(v uint32) uint32 {
	return v<<24 | (v&0xff00)<<8 | (v>>8)&0xff00 | v>>24
}

// pioPixels models the path a stored row takes to the output pins: DMA reads it
// as little endian 32 bit words, byte swaps each one, and the output shift
// register, shifting left, hands "out pins, 16" the most significant half
// first. What comes back is the sequence of 16 bit values the pins see.
func pioPixels(row []uint16) []Colour {
	var out []Colour
	for i := 0; i+1 < len(row); i += 2 {
		w := uint32(row[i]) | uint32(row[i+1])<<16 // little endian read
		w = bswap32(w)                             // DMA byte swap
		out = append(out, Colour(w>>16), Colour(w))
	}
	return out
}

// TestPixelsReachPinsInOrder is the test the whole storage arrangement exists
// to satisfy: pixels drawn left to right must arrive at the pins left to right,
// with their values intact.
func TestPixelsReachPinsInOrder(t *testing.T) {
	f := NewFramebuffer16(8, 1)
	want := []Colour{
		RGB555(31, 0, 0), RGB555(0, 31, 0), RGB555(0, 0, 31), RGB555(31, 31, 31),
		RGB555(1, 2, 3), RGB555(4, 5, 6), RGB555(7, 8, 9), RGB555(10, 11, 12),
	}
	for x, c := range want {
		f.SetPixel(x, 0, c)
	}
	got := pioPixels(f.Line(0))
	if len(got) != len(want) {
		t.Fatalf("got %d pixels, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pixel %d reaches the pins as %#04x, want %#04x", i, got[i], want[i])
		}
	}
}

// TestStoredFormIsSwapped pins the storage convention itself, so a future
// change to the DMA configuration cannot silently disagree with it.
func TestStoredFormIsSwapped(t *testing.T) {
	f := NewFramebuffer16(2, 1)
	const c = Colour(0xabcd)
	f.SetPixel(0, 0, c)
	if got, want := f.Pix[0], uint16(0xcdab); got != want {
		t.Errorf("stored %#04x, want %#04x", got, want)
	}
	if got := f.Pixel(0, 0); got != c {
		t.Errorf("Pixel = %#04x, want %#04x", got, c)
	}
}

func TestRGB555Pack(t *testing.T) {
	for _, tc := range []struct {
		r, g, b uint8
		want    Colour
	}{
		{0, 0, 0, 0},
		{31, 0, 0, 0x001f},
		{0, 31, 0, 0x07c0},
		{0, 0, 31, 0xf800},
		{31, 31, 31, 0xffdf}, // every ladder bit set, bit 5 clear
		{0xff, 0, 0, 0x001f}, // components are masked, not allowed to bleed
	} {
		if got := RGB555(tc.r, tc.g, tc.b); got != tc.want {
			t.Errorf("RGB555(%d,%d,%d) = %#04x, want %#04x", tc.r, tc.g, tc.b, got, tc.want)
		}
	}
	// Bit 5 drives the SD card clock and must never be set by a colour.
	for r := uint8(0); r < 32; r++ {
		for g := uint8(0); g < 32; g++ {
			if c := RGB555(r, g, 31); c&(1<<5) != 0 {
				t.Fatalf("RGB555(%d,%d,31) sets bit 5", r, g)
			}
		}
	}
}

func TestColourChannels(t *testing.T) {
	c := RGB555(3, 17, 29)
	if got := c.Red(); got != 3 {
		t.Errorf("Red = %d, want 3", got)
	}
	if got := c.Green(); got != 17 {
		t.Errorf("Green = %d, want 17", got)
	}
	if got := c.Blue(); got != 29 {
		t.Errorf("Blue = %d, want 29", got)
	}
}

func TestNamedColours555(t *testing.T) {
	for _, tc := range []struct {
		name    string
		c       Colour
		r, g, b uint8
	}{
		{"black", Black555, 0, 0, 0},
		{"white", White555, 31, 31, 31},
		{"red", Red555, 31, 0, 0},
		{"green", Green555, 0, 31, 0},
		{"blue", Blue555, 0, 0, 31},
		{"yellow", Yellow555, 31, 31, 0},
		{"cyan", Cyan555, 0, 31, 31},
		{"magenta", Magenta555, 31, 0, 31},
	} {
		if tc.c.Red() != tc.r || tc.c.Green() != tc.g || tc.c.Blue() != tc.b {
			t.Errorf("%s = (%d,%d,%d), want (%d,%d,%d)", tc.name,
				tc.c.Red(), tc.c.Green(), tc.c.Blue(), tc.r, tc.g, tc.b)
		}
	}
}

func TestFramebuffer16Draw(t *testing.T) {
	f := NewFramebuffer16(6, 5)
	f.Fill(Blue555)
	if got := f.Pixel(0, 0); got != Blue555 {
		t.Errorf("Fill left %#04x, want blue", got)
	}
	f.FillRect(1, 1, 2, 2, Red555)
	if got := f.Pixel(1, 1); got != Red555 {
		t.Errorf("Pixel(1,1) = %#04x, want red", got)
	}
	if got := f.Pixel(3, 1); got != Blue555 {
		t.Errorf("Pixel(3,1) = %#04x, want blue", got)
	}
	// Fill through DMA form stays consistent with the logical form.
	for _, c := range pioPixels(f.Line(0)) {
		if c != Blue555 {
			t.Fatalf("row 0 has %#04x, want all blue", c)
		}
	}
}

func TestFramebuffer16Clips(t *testing.T) {
	f := NewFramebuffer16(4, 4)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {4, 0}, {0, 4}, {99, 99}} {
		f.SetPixel(p[0], p[1], White555)
		if got := f.Pixel(p[0], p[1]); got != Black555 {
			t.Errorf("Pixel%v = %#04x, want black", p, got)
		}
	}
	for i, p := range f.Pix {
		if p != 0 {
			t.Fatalf("clipped write reached pixel %d", i)
		}
	}
	f.FillRect(-2, -2, 3, 3, White555)
	if got := f.Pixel(0, 0); got != White555 {
		t.Errorf("Pixel(0,0) = %#04x, want white", got)
	}
	if got := f.Pixel(1, 1); got != Black555 {
		t.Errorf("Pixel(1,1) = %#04x, want black", got)
	}
	for _, y := range []int{-1, 4} {
		if f.Line(y) != nil {
			t.Errorf("Line(%d) is not nil", y)
		}
	}
}

// TestFramebuffer16ForMode checks the framebuffer a mode asks for is the size
// the DMA path assumes: an even width, so pixels pair into 32 bit words.
func TestFramebuffer16ForMode(t *testing.T) {
	m := Mode800x600RGB555
	f := NewFramebuffer16ForMode(m)
	if f.Width != m.Width() || f.Height != m.Height() {
		t.Fatalf("size = %dx%d, want %dx%d", f.Width, f.Height, m.Width(), m.Height())
	}
	if f.Width%2 != 0 {
		t.Fatalf("width %d is odd, DMA moves pixels in pairs", f.Width)
	}
	if got, want := len(f.Pix)*2, 240_000; got != want {
		t.Errorf("framebuffer = %d bytes, want %d", got, want)
	}
}

func TestNewFramebuffer16Panics(t *testing.T) {
	for _, sz := range [][2]int{{0, 4}, {4, 0}, {-2, 4}, {5, 4}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewFramebuffer16%v did not panic", sz)
				}
			}()
			NewFramebuffer16(sz[0], sz[1])
		}()
	}
}
