package picovga

import "testing"

func TestRGB(t *testing.T) {
	for _, tc := range []struct {
		r, g, b uint8
		want    uint8
	}{
		{0, 0, 0, 0x00},
		{7, 7, 3, 0xff},
		{7, 0, 0, 0xe0},
		{0, 7, 0, 0x1c},
		{0, 0, 3, 0x03},
		// Components wider than their field are masked, not allowed to bleed
		// into the neighbouring colour.
		{0xff, 0, 0, 0xe0},
		{0, 0xff, 0, 0x1c},
		{0, 0, 0xff, 0x03},
	} {
		if got := RGB(tc.r, tc.g, tc.b); got != tc.want {
			t.Errorf("RGB(%d,%d,%d) = %#02x, want %#02x", tc.r, tc.g, tc.b, got, tc.want)
		}
	}
}

func TestNewFramebuffer(t *testing.T) {
	f := NewFramebuffer(8, 4)
	if f.Width != 8 || f.Height != 4 {
		t.Errorf("size = %dx%d, want 8x4", f.Width, f.Height)
	}
	if len(f.Pix) != 32 {
		t.Errorf("len(Pix) = %d, want 32", len(f.Pix))
	}
	for i, p := range f.Pix {
		if p != Black {
			t.Fatalf("pixel %d = %#02x, want black", i, p)
		}
	}
}

func TestNewFramebufferForMode(t *testing.T) {
	m := Mode800x600
	f := NewFramebufferForMode(m)
	if f.Width != m.Width() || f.Height != m.Height() {
		t.Errorf("size = %dx%d, want %dx%d", f.Width, f.Height, m.Width(), m.Height())
	}
}

func TestSetPixel(t *testing.T) {
	f := NewFramebuffer(4, 3)
	f.SetPixel(1, 2, Red)
	if got := f.Pixel(1, 2); got != Red {
		t.Errorf("Pixel(1,2) = %#02x, want %#02x", got, Red)
	}
	// Pixels are stored row by row.
	if got := f.Pix[2*4+1]; got != Red {
		t.Errorf("Pix[9] = %#02x, want %#02x", got, Red)
	}
	if got := f.Pixel(0, 0); got != Black {
		t.Errorf("Pixel(0,0) = %#02x, want black", got)
	}
}

// TestSetPixelClips checks that out of range coordinates are discarded rather
// than corrupting a neighbouring row or panicking.
func TestSetPixelClips(t *testing.T) {
	f := NewFramebuffer(4, 3)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {4, 0}, {0, 3}, {-1, -1}, {99, 99}} {
		f.SetPixel(p[0], p[1], White)
		if got := f.Pixel(p[0], p[1]); got != Black {
			t.Errorf("Pixel%v = %#02x, want black", p, got)
		}
	}
	for i, p := range f.Pix {
		if p != Black {
			t.Fatalf("clipped write reached pixel %d", i)
		}
	}
}

// TestLineAliases checks that a row handed out for DMA shares storage with the
// framebuffer, so drawing through either is visible to the other.
func TestLineAliases(t *testing.T) {
	f := NewFramebuffer(4, 3)
	line := f.Line(1)
	if len(line) != 4 {
		t.Fatalf("len(Line(1)) = %d, want 4", len(line))
	}
	line[2] = Green
	if got := f.Pixel(2, 1); got != Green {
		t.Errorf("Pixel(2,1) = %#02x, want %#02x", got, Green)
	}
	f.SetPixel(0, 1, Blue)
	if line[0] != Blue {
		t.Errorf("line[0] = %#02x, want %#02x", line[0], Blue)
	}
	for _, y := range []int{-1, 3} {
		if f.Line(y) != nil {
			t.Errorf("Line(%d) is not nil", y)
		}
	}
}

func TestFill(t *testing.T) {
	f := NewFramebuffer(4, 3)
	f.Fill(Cyan)
	for i, p := range f.Pix {
		if p != Cyan {
			t.Fatalf("pixel %d = %#02x, want %#02x", i, p, Cyan)
		}
	}
}

func TestFillRect(t *testing.T) {
	f := NewFramebuffer(5, 5)
	f.FillRect(1, 1, 3, 2, White)
	want := [][5]uint8{
		{0, 0, 0, 0, 0},
		{0, 1, 1, 1, 0},
		{0, 1, 1, 1, 0},
		{0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0},
	}
	for y := range want {
		for x := range want[y] {
			got := f.Pixel(x, y)
			expect := Black
			if want[y][x] == 1 {
				expect = White
			}
			if got != expect {
				t.Errorf("Pixel(%d,%d) = %#02x, want %#02x", x, y, got, expect)
			}
		}
	}
}

// TestFillRectClips checks rectangles straddling each edge are trimmed rather
// than wrapping onto the next row.
func TestFillRectClips(t *testing.T) {
	f := NewFramebuffer(4, 4)
	f.FillRect(-2, -2, 3, 3, White) // only 1x1 lands, at the origin
	if got := f.Pixel(0, 0); got != White {
		t.Errorf("Pixel(0,0) = %#02x, want white", got)
	}
	if got := f.Pixel(1, 1); got != Black {
		t.Errorf("Pixel(1,1) = %#02x, want black", got)
	}

	f.Fill(Black)
	f.FillRect(3, 3, 10, 10, White) // clipped to the bottom right pixel
	if got := f.Pixel(3, 3); got != White {
		t.Errorf("Pixel(3,3) = %#02x, want white", got)
	}
	count := 0
	for _, p := range f.Pix {
		if p == White {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%d pixels set, want 1", count)
	}

	// Degenerate and wholly offscreen rectangles draw nothing.
	f.Fill(Black)
	f.FillRect(0, 0, 0, 5, White)
	f.FillRect(0, 0, 5, -1, White)
	f.FillRect(10, 10, 2, 2, White)
	for i, p := range f.Pix {
		if p != Black {
			t.Fatalf("pixel %d was drawn", i)
		}
	}
}

func TestHVLine(t *testing.T) {
	f := NewFramebuffer(5, 5)
	f.HLine(1, 0, 3, Red)
	f.VLine(0, 1, 3, Blue)
	for x := 1; x < 4; x++ {
		if got := f.Pixel(x, 0); got != Red {
			t.Errorf("Pixel(%d,0) = %#02x, want red", x, got)
		}
	}
	for y := 1; y < 4; y++ {
		if got := f.Pixel(0, y); got != Blue {
			t.Errorf("Pixel(0,%d) = %#02x, want blue", y, got)
		}
	}
	if got := f.Pixel(4, 0); got != Black {
		t.Errorf("Pixel(4,0) = %#02x, want black", got)
	}
}

func TestRect(t *testing.T) {
	f := NewFramebuffer(4, 4)
	f.Rect(0, 0, 4, 4, White)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			edge := x == 0 || y == 0 || x == 3 || y == 3
			want := Black
			if edge {
				want = White
			}
			if got := f.Pixel(x, y); got != want {
				t.Errorf("Pixel(%d,%d) = %#02x, want %#02x", x, y, got, want)
			}
		}
	}
}

// TestDrawThenReadBackByLine writes a pattern and reads it back the way the
// driver streams it, one row at a time, checking the bytes reach DMA in the
// order they were drawn.
func TestDrawThenReadBackByLine(t *testing.T) {
	f := NewFramebuffer(8, 4)
	for y := 0; y < f.Height; y++ {
		for x := 0; x < f.Width; x++ {
			f.SetPixel(x, y, uint8(y*f.Width+x))
		}
	}
	for y := 0; y < f.Height; y++ {
		line := f.Line(y)
		for x, got := range line {
			if want := uint8(y*f.Width + x); got != want {
				t.Fatalf("row %d byte %d = %d, want %d", y, x, got, want)
			}
		}
	}
}

func TestNewFramebufferPanics(t *testing.T) {
	for _, sz := range [][2]int{{0, 4}, {4, 0}, {-1, 4}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewFramebuffer%v did not panic", sz)
				}
			}()
			NewFramebuffer(sz[0], sz[1])
		}()
	}
}
