package picovga

import "testing"

func TestMode800x600(t *testing.T) {
	m := Mode800x600
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"HTotal", m.HTotal(), 1056},
		{"VTotal", m.VTotal(), 628},
		{"Width", m.Width(), 400},
		{"Height", m.Height(), 300},
		{"CPP", int(m.CPP()), 6},
		{"LineCycles", m.LineCycles(), 3168},
		{"PIOFrequency", int(m.PIOFrequency()), 120_000_000},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	// VESA gives 37.879kHz line rate and 60.317Hz frame rate for this mode.
	lineRate := int(m.PixelClock) / m.HTotal()
	if lineRate < 37_800 || lineRate > 37_960 {
		t.Errorf("line rate = %dHz, want about 37879Hz", lineRate)
	}
	frameRate := int(m.PixelClock) / (m.HTotal() * m.VTotal())
	if frameRate != 60 {
		t.Errorf("frame rate = %dHz, want 60Hz", frameRate)
	}
}

// TestLineCyclesExact checks that both kinds of scanline occupy exactly one
// line's worth of PIO clocks. A line that is even one cycle short or long
// walks the picture sideways, so this is the arithmetic most worth pinning.
func TestLineCyclesExact(t *testing.T) {
	m := Mode800x600
	if got, want := m.VisibleLineCycles(), m.LineCycles(); got != want {
		t.Errorf("visible line = %d cycles, want %d", got, want)
	}
	if got, want := m.BlankLineCycles(), m.LineCycles(); got != want {
		t.Errorf("blank line = %d cycles, want %d", got, want)
	}
	// The visible portion must be exactly the pixels the framebuffer supplies.
	if got, want := m.Width()*int(m.CPP()), m.HVisible*m.ClocksPerPixel; got != want {
		t.Errorf("pixel cycles = %d, want %d", got, want)
	}
}

// TestControlWords checks each control word dispatches to the right routine and
// carries the counter that routine expects.
func TestControlWords(t *testing.T) {
	m := Mode800x600
	for _, tc := range []struct {
		name     string
		word     uint32
		wantAddr uint8
		wantN    uint32
	}{
		{"sync", m.SyncWord(), BaseOffset + BaseSync, uint32(m.HSync*m.ClocksPerPixel - 3)},
		{"output", m.OutputWord(), BaseOffset + BaseOutput, uint32(m.Width() - 2)},
	} {
		if addr := uint8(tc.word >> 27); addr != tc.wantAddr {
			t.Errorf("%s address = %d, want %d", tc.name, addr, tc.wantAddr)
		}
		if n := tc.word & 0x07ff_ffff; n != tc.wantN {
			t.Errorf("%s counter = %d, want %d", tc.name, n, tc.wantN)
		}
	}
	// Dark words carry a 19 bit counter and an 8 bit colour.
	for _, tc := range []struct {
		name  string
		word  uint32
		wantN uint32
	}{
		{"backPorch", m.BackPorchWord(), uint32(m.HBack*m.ClocksPerPixel - 5)},
		{"frontPorch", m.FrontPorchWord(), uint32(m.HFront*m.ClocksPerPixel - 4)},
	} {
		if addr := uint8(tc.word >> 27); addr != BaseOffset+BaseDark {
			t.Errorf("%s address = %d, want %d", tc.name, addr, BaseOffset+BaseDark)
		}
		if n := (tc.word >> 8) & 0x7ffff; n != tc.wantN {
			t.Errorf("%s counter = %d, want %d", tc.name, n, tc.wantN)
		}
		if n := (tc.word >> 8) & 0x7ffff; n >= 1<<19 {
			t.Errorf("%s counter %d overflows the 19 bit field", tc.name, n)
		}
		if col := uint8(tc.word); col != Black {
			t.Errorf("%s colour = %#02x, want black", tc.name, col)
		}
	}
}

// TestFrameWalk walks a whole frame the way the driver does and checks the
// shape of the output: how long vertical sync is asserted, how many lines carry
// pixels, and that every framebuffer row is shown VScale times in order.
func TestFrameWalk(t *testing.T) {
	m := Mode800x600
	rowUses := make([]int, m.Height())
	vsyncLines, visibleLines, cycles := 0, 0, 0
	lastRow := -1
	for line := 0; line < m.VTotal(); line++ {
		if m.InVSync(line) {
			vsyncLines++
		}
		if row, ok := m.VisibleLine(line); ok {
			visibleLines++
			cycles += m.VisibleLineCycles()
			if row < 0 || row >= m.Height() {
				t.Fatalf("line %d maps to row %d, outside the framebuffer", line, row)
			}
			if row < lastRow {
				t.Fatalf("line %d maps to row %d, which goes backwards from %d", line, row, lastRow)
			}
			lastRow = row
			rowUses[row]++
		} else {
			cycles += m.BlankLineCycles()
		}
	}
	if vsyncLines != m.VSync {
		t.Errorf("vertical sync asserted for %d lines, want %d", vsyncLines, m.VSync)
	}
	if visibleLines != m.VVisible {
		t.Errorf("%d visible lines, want %d", visibleLines, m.VVisible)
	}
	for row, n := range rowUses {
		if n != m.VScale {
			t.Fatalf("framebuffer row %d shown %d times, want %d", row, n, m.VScale)
		}
	}
	if want := m.VTotal() * m.LineCycles(); cycles != want {
		t.Errorf("frame = %d cycles, want %d", cycles, want)
	}
}

func TestModeValidate(t *testing.T) {
	bad := func(f func(*Mode)) Mode {
		m := Mode800x600
		f(&m)
		return m
	}
	for _, tc := range []struct {
		name string
		mode Mode
	}{
		{"zero HScale", bad(func(m *Mode) { m.HScale = 0 })},
		{"indivisible width", bad(func(m *Mode) { m.HScale = 3 })},
		{"cpp too large", bad(func(m *Mode) { m.ClocksPerPixel = 16 })},
		{"back porch too short", bad(func(m *Mode) { m.HBack = 1 })},
	} {
		if err := tc.mode.Validate(); err == nil {
			t.Errorf("%s: Validate returned nil, want an error", tc.name)
		}
	}
}
