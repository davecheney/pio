package picovga

import "testing"

func TestMode640x480RGB555(t *testing.T) {
	m := Mode640x480RGB555
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"HTotal", m.HTotal(), 800},
		{"VTotal", m.VTotal(), 525},
		{"Width", m.Width(), 320},
		{"Height", m.Height(), 240},
		{"CPP", int(m.CPP()), 4},
		{"LineCycles", m.LineCycles(), 1600},
		{"PIOFrequency", int(m.PIOFrequency()), 50_350_000},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	// VESA gives 31.469kHz and 59.94Hz for this mode.
	if line := int(m.PixelClock) / m.HTotal(); line < 31_400 || line > 31_540 {
		t.Errorf("line rate = %dHz, want about 31469Hz", line)
	}
	// Both sync signals are negative here, which is what makes Setup invert the
	// horizontal sync pad.
	if m.PositiveHSync || m.PositiveVSync {
		t.Errorf("sync polarity = (%v,%v), want both negative", m.PositiveHSync, m.PositiveVSync)
	}
}

// TestMode640x480Scales checks the mode still composes exact scanlines at the
// larger scale factors a target short of memory needs. 640x480 reaches further
// than 800x600 does, because it spends fewer clock cycles on a screen pixel and
// so has more room under the base program's limit.
func TestMode640x480Scales(t *testing.T) {
	for _, s := range []int{2, 4, 5, 8} {
		m := Mode640x480RGB555
		m.HScale, m.VScale = s, s
		if err := m.Validate(); err != nil {
			t.Errorf("scale %d: Validate: %v", s, err)
			continue
		}
		if got, want := m.VisibleLineCycles(), m.LineCycles(); got != want {
			t.Errorf("scale %d: visible line = %d cycles, want %d", s, got, want)
		}
		if got, want := m.BlankLineCycles(), m.LineCycles(); got != want {
			t.Errorf("scale %d: blank line = %d cycles, want %d", s, got, want)
		}
		if got, want := m.Width()*int(m.CPP()), m.HVisible*m.ClocksPerPixel; got != want {
			t.Errorf("scale %d: pixel cycles = %d, want %d", s, got, want)
		}
	}
}

// TestMode640x480Scale4 pins the configuration the Mandelbrot example uses:
// the same 160x120 framebuffer as 800x600 at scale 5, for less DMA traffic.
func TestMode640x480Scale4(t *testing.T) {
	m := Mode640x480RGB555
	m.HScale, m.VScale = 4, 4
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if m.Width() != 160 || m.Height() != 120 {
		t.Errorf("framebuffer = %dx%d, want 160x120", m.Width(), m.Height())
	}
	if got := int(m.CPP()); got != 8 {
		t.Errorf("CPP = %d, want 8", got)
	}
	// Bytes a frame, against 800x600 at scale 5 carrying the same picture.
	wide := Mode800x600RGB555
	wide.HScale, wide.VScale = 5, 5
	small := m.VVisible * m.Width() * 2
	large := wide.VVisible * wide.Width() * 2
	if small >= large {
		t.Errorf("640x480 moves %d bytes a frame, 800x600 moves %d; expected less", small, large)
	}
	t.Logf("%d bytes a frame against %d, %.0f%% less", small, large, 100*(1-float64(small)/float64(large)))
}
