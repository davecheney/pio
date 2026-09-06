package picovga

import "testing"

func TestMode800x600RGB555(t *testing.T) {
	m := Mode800x600RGB555
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"Width", m.Width(), 400},
		{"Height", m.Height(), 300},
		{"CPP", int(m.CPP()), 6},
		{"LineCycles", m.LineCycles(), 3168},
		{"PIOFrequency", int(m.PIOFrequency()), 120_000_000},
		{"PixelBits", m.PixelBits, 16},
		{"DarkMaxCount", m.DarkMaxCount(), 2047},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	// 400x300 of 16 bit pixels is what has to fit in RAM.
	if got, want := m.Width()*m.Height()*2, 240_000; got != want {
		t.Errorf("framebuffer = %d bytes, want %d", got, want)
	}
}

// TestBlankWordsSplit checks that blanking too long for one 11 bit counter is
// split into commands that together run for exactly the intended time.
func TestBlankWordsSplit(t *testing.T) {
	m := Mode800x600RGB555
	words := m.BlankWords()
	if len(words) < 2 {
		t.Fatalf("got %d blank words, expected the run to be split", len(words))
	}
	total := 0
	for i, w := range words {
		if addr := uint8(w >> 27); addr != BaseOffset+BaseDark {
			t.Errorf("word %d dispatches to %d, want the dark routine at %d", i, addr, BaseOffset+BaseDark)
		}
		n := (w >> 16) & Dark16MaxCount
		if n > Dark16MaxCount {
			t.Errorf("word %d counter %d overflows the 11 bit field", i, n)
		}
		if colour := uint16(w); colour != 0 {
			t.Errorf("word %d colour = %#04x, want black", i, colour)
		}
		total += m.darkWordCycles(w)
	}
	if want := m.LineCycles() - m.HSync*m.ClocksPerPixel; total != want {
		t.Errorf("blank run = %d cycles, want %d", total, want)
	}
}

// TestRGB555LineCyclesExact is the 16 bit counterpart of TestLineCyclesExact:
// both kinds of scanline must occupy exactly one line of PIO clocks.
func TestRGB555LineCyclesExact(t *testing.T) {
	m := Mode800x600RGB555
	if got, want := m.VisibleLineCycles(), m.LineCycles(); got != want {
		t.Errorf("visible line = %d cycles, want %d", got, want)
	}
	if got, want := m.BlankLineCycles(), m.LineCycles(); got != want {
		t.Errorf("blank line = %d cycles, want %d", got, want)
	}
}

func TestDark16Cmd(t *testing.T) {
	const n, colour = 1234, 0xbeef
	w := Dark16Cmd(n, colour)
	if addr := uint8(w >> 27); addr != BaseOffset+BaseDark {
		t.Errorf("address = %d, want %d", addr, BaseOffset+BaseDark)
	}
	if got := (w >> 16) & Dark16MaxCount; got != n {
		t.Errorf("counter = %d, want %d", got, n)
	}
	if got := uint16(w); got != colour {
		t.Errorf("colour = %#04x, want %#04x", got, colour)
	}
	// The three fields must not overlap: 5 + 11 + 16 is exactly 32 bits.
	if w != uint32(BaseOffset+BaseDark)<<27|n<<16|colour {
		t.Errorf("word = %#08x, fields overlap", w)
	}
}

// TestRGB555FrameWalk repeats the frame walk for the 16 bit mode, since it uses
// a different blank line composition.
func TestRGB555FrameWalk(t *testing.T) {
	m := Mode800x600RGB555
	visible, cycles := 0, 0
	for line := 0; line < m.VTotal(); line++ {
		if _, ok := m.VisibleLine(line); ok {
			visible++
			cycles += m.VisibleLineCycles()
		} else {
			cycles += m.BlankLineCycles()
		}
	}
	if visible != m.VVisible {
		t.Errorf("%d visible lines, want %d", visible, m.VVisible)
	}
	if want := m.VTotal() * m.LineCycles(); cycles != want {
		t.Errorf("frame = %d cycles, want %d", cycles, want)
	}
}
