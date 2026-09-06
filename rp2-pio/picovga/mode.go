package picovga

import "errors"

// ErrBadMode reports a video mode whose timings the base program cannot drive.
var ErrBadMode = errors.New("picovga: invalid video mode")

// Mode describes a video mode: its pixel timings, how many PIO clock cycles
// each screen pixel occupies, and how far framebuffer pixels are stretched to
// fill the screen.
//
// Horizontal timings are in screen pixels and vertical timings in scanlines,
// following the usual VESA description of a mode. A framebuffer pixel covers
// HScale screen pixels across and VScale scanlines down, so a mode can be
// driven from a framebuffer far smaller than its visible area.
type Mode struct {
	Name string

	// PixelClock is the screen pixel rate in Hz.
	PixelClock uint32

	HVisible, HFront, HSync, HBack int
	VVisible, VFront, VSync, VBack int

	// ClocksPerPixel is the number of PIO clock cycles per screen pixel.
	ClocksPerPixel int

	// HScale and VScale are the screen pixels covered by one framebuffer
	// pixel, horizontally and vertically.
	HScale, VScale int

	// PositiveHSync and PositiveVSync give each sync signal's polarity. The
	// base program drives its side-set pin high during the sync pulse, so a
	// mode with a negative sync signal needs that pin inverted outside PIO.
	PositiveHSync, PositiveVSync bool

	// PixelBits is how many bits the base program writes per pixel: 8 for the
	// program NewBase assembles, 16 for NewBase16. It decides how much of a
	// dark control word is left over for the cycle counter.
	PixelBits int
}

// DarkMaxCount is the largest counter a dark control word can carry in this
// mode, and so the longest single blanking command is DarkMaxCount+4 cycles.
func (m Mode) DarkMaxCount() int {
	if m.PixelBits == 16 {
		return Dark16MaxCount
	}
	return 1<<19 - 1
}

// darkWord assembles a blanking or flat colour command for this mode's pixel
// width.
func (m Mode) darkWord(n uint32, colour uint16) uint32 {
	if m.PixelBits == 16 {
		return Dark16Cmd(n, colour)
	}
	return DarkCmd(n, uint8(colour))
}

// darkWordCycles returns how many cycles a dark control word runs for.
func (m Mode) darkWordCycles(w uint32) int {
	if m.PixelBits == 16 {
		return int((w>>16)&Dark16MaxCount) + 4
	}
	return int((w>>8)&(1<<19-1)) + 4
}

// darkRun assembles the commands needed to hold colour for the given number of
// cycles, splitting the run when it does not fit one counter.
func (m Mode) darkRun(cycles int, colour uint16) []uint32 {
	max := m.DarkMaxCount() + 4
	var words []uint32
	for cycles >= 4 {
		c := cycles
		if c > max {
			c = max
			// Leave a remainder a command can actually express.
			if rem := cycles - c; rem < 4 {
				c -= 4 - rem
			}
		}
		words = append(words, m.darkWord(uint32(c-4), colour))
		cycles -= c
	}
	return words
}

// Mode800x600 is VESA 800x600 at 60Hz, driven from a 400x300 framebuffer.
//
// Both of its sync signals are positive polarity, which is what the base
// program's side-set produces, so neither needs inverting.
var Mode800x600 = Mode{
	Name:       "800x600@60",
	PixelClock: 40_000_000,

	HVisible: 800, HFront: 40, HSync: 128, HBack: 88,
	VVisible: 600, VFront: 1, VSync: 4, VBack: 23,

	ClocksPerPixel: 3,
	HScale:         2,
	VScale:         2,

	PositiveHSync: true,
	PositiveVSync: true,
	PixelBits:     8,
}

// Mode800x600RGB555 is VESA 800x600 at 60Hz driven from a 400x300 framebuffer
// of 16 bit RGB555 pixels, the arrangement the RP2040 VGA reference design
// wires up. See NewBase16 for why that board needs 16 bits per pixel.
var Mode800x600RGB555 = Mode{
	Name:       "800x600@60 RGB555",
	PixelClock: 40_000_000,

	HVisible: 800, HFront: 40, HSync: 128, HBack: 88,
	VVisible: 600, VFront: 1, VSync: 4, VBack: 23,

	ClocksPerPixel: 3,
	HScale:         2,
	VScale:         2,

	PositiveHSync: true,
	PositiveVSync: true,
	PixelBits:     16,
}

// HTotal is the total line length in screen pixels.
func (m Mode) HTotal() int { return m.HVisible + m.HFront + m.HSync + m.HBack }

// VTotal is the total frame height in scanlines.
func (m Mode) VTotal() int { return m.VVisible + m.VFront + m.VSync + m.VBack }

// Width is the framebuffer width in pixels.
func (m Mode) Width() int { return m.HVisible / m.HScale }

// Height is the framebuffer height in pixels.
func (m Mode) Height() int { return m.VVisible / m.VScale }

// CPP is the number of PIO clock cycles per framebuffer pixel, the value the
// base program is assembled for.
func (m Mode) CPP() uint8 { return uint8(m.ClocksPerPixel * m.HScale) }

// PIOFrequency is the state machine clock rate in Hz.
func (m Mode) PIOFrequency() uint32 { return m.PixelClock * uint32(m.ClocksPerPixel) }

// LineCycles is the total length of a scanline in PIO clock cycles.
func (m Mode) LineCycles() int { return m.HTotal() * m.ClocksPerPixel }

// Validate reports whether the mode can be driven by the base program.
func (m Mode) Validate() error {
	if m.HScale < 1 || m.VScale < 1 || m.ClocksPerPixel < 1 {
		return ErrBadMode
	}
	if m.HVisible%m.HScale != 0 || m.VVisible%m.VScale != 0 {
		return ErrBadMode
	}
	cpp := m.ClocksPerPixel * m.HScale
	if cpp < BaseMinCPP || cpp > BaseMaxCPP {
		return ErrCPPRange
	}
	// Every command needs a non-negative counter, and the back porch also
	// absorbs the output routine's trailing cycle.
	if m.HSync*m.ClocksPerPixel < 3 || m.HBack*m.ClocksPerPixel < 5 || m.HFront*m.ClocksPerPixel < 4 {
		return ErrBadMode
	}
	if m.Width() < 2 {
		return ErrBadMode
	}
	if m.PixelBits != 8 && m.PixelBits != 16 {
		return ErrBadMode
	}
	// The porches are single commands, so their counters must fit.
	if m.HBack*m.ClocksPerPixel-5 > m.DarkMaxCount() || m.HFront*m.ClocksPerPixel-4 > m.DarkMaxCount() {
		return ErrBadMode
	}
	return nil
}

// Cycle costs of each base program routine, counted from the routine's first
// instruction up to and including the "out pc, 5" that dispatches the next
// command.
//
// The sync and dark routines spend three and four cycles respectively on top of
// their loop counter. The output routine's trailing pixel is one cycle short,
// and its wrap back to the dispatch instruction costs one, so it runs for one
// cycle longer than the pixels alone; the back porch is shortened to match.
func syncRoutineCycles(n int) int { return n + 3 }
func darkRoutineCycles(n int) int { return n + 4 }
func outputRoutineCycles(width int, cpp uint8) int {
	return width*int(cpp) + 1
}

// SyncWord returns the control word driving the horizontal sync pulse.
func (m Mode) SyncWord() uint32 {
	return Cmd(BaseOffset+BaseSync, uint32(m.HSync*m.ClocksPerPixel-3))
}

// BackPorchWord returns the control word for the blanking between the sync
// pulse and the first pixel. It is one cycle short of the nominal back porch,
// absorbing the extra cycle the output routine spends wrapping.
func (m Mode) BackPorchWord() uint32 {
	return m.darkWord(uint32(m.HBack*m.ClocksPerPixel-4-1), 0)
}

// OutputWord returns the control word that streams one framebuffer line. The
// pixel bytes for the line must follow it in the FIFO.
func (m Mode) OutputWord() uint32 {
	return Cmd(BaseOffset+BaseOutput, uint32(m.Width()-2))
}

// FrontPorchWord returns the control word for the blanking after the last pixel.
func (m Mode) FrontPorchWord() uint32 {
	return m.darkWord(uint32(m.HFront*m.ClocksPerPixel-4), 0)
}

// BlankWords returns the control words blanking everything in a line after the
// sync pulse, used for lines outside the visible area. A 16 bit mode has only
// an 11 bit counter, so this is more than one command in all but the shortest
// modes.
func (m Mode) BlankWords() []uint32 {
	return m.darkRun(m.LineCycles()-m.HSync*m.ClocksPerPixel, 0)
}

// VisibleLineCycles is the total length of a visible scanline in PIO clock
// cycles, summed from the routines a visible line actually runs.
func (m Mode) VisibleLineCycles() int {
	return syncRoutineCycles(m.HSync*m.ClocksPerPixel-3) +
		darkRoutineCycles(m.HBack*m.ClocksPerPixel-4-1) +
		outputRoutineCycles(m.Width(), m.CPP()) +
		darkRoutineCycles(m.HFront*m.ClocksPerPixel-4)
}

// BlankLineCycles is the total length of a blanked scanline in PIO clock cycles.
func (m Mode) BlankLineCycles() int {
	cycles := syncRoutineCycles(m.HSync*m.ClocksPerPixel - 3)
	for _, w := range m.BlankWords() {
		cycles += m.darkWordCycles(w)
	}
	return cycles
}

// InVSync reports whether the vertical sync pulse is asserted on the given line.
func (m Mode) InVSync(line int) bool { return line < m.VSync }

// VisibleLine maps a scanline to a framebuffer row, reporting false for lines
// in the blanking intervals.
func (m Mode) VisibleLine(line int) (int, bool) {
	top := m.VSync + m.VBack
	if line < top || line >= top+m.VVisible {
		return 0, false
	}
	return (line - top) / m.VScale, true
}

// wordCycles returns how many cycles a control word occupies, including the
// dispatch of the next command, by looking at the routine it jumps to.
func (m Mode) wordCycles(w uint32) int {
	switch uint8(w >> 27) {
	case BaseOffset + BaseSync:
		return syncRoutineCycles(int(w & 0x07ff_ffff))
	case BaseOffset + BaseDark:
		return m.darkWordCycles(w)
	case BaseOffset + BaseOutput:
		return outputRoutineCycles(m.Width(), m.CPP())
	}
	return 0
}

// LineCyclesOf returns the total time a composed scanline occupies, in PIO
// clock cycles. A line that does not come to exactly LineCycles will not hold a
// steady picture, so this is worth checking against a line you assemble.
func (m Mode) LineCyclesOf(words []uint32) int {
	cycles := 0
	for _, w := range words {
		cycles += m.wordCycles(w)
	}
	return cycles
}

// FlatLineWords assembles a scanline of flat colour bars, dividing the visible
// area equally between the colours given. Such a line needs neither a
// framebuffer nor DMA, since it uses only the sync and dark routines, which
// makes it a useful first test of a display's timing.
//
// The bar widths are apportioned so they always total the visible area exactly,
// even when the colours do not divide it evenly.
func (m Mode) FlatLineWords(colours []Colour) []uint32 {
	words := make([]uint32, 0, len(colours)+4)
	words = append(words, m.SyncWord())
	// The full back porch here: nothing shortens it, there being no output
	// routine on this line to spend the extra cycle.
	words = append(words, m.darkWord(uint32(m.HBack*m.ClocksPerPixel-4), 0))
	visible, used := m.HVisible*m.ClocksPerPixel, 0
	for i, c := range colours {
		end := visible * (i + 1) / len(colours)
		words = append(words, m.darkRun(end-used, uint16(c))...)
		used = end
	}
	words = append(words, m.darkWord(uint32(m.HFront*m.ClocksPerPixel-4), 0))
	return words
}

// BlankLineWords assembles a scanline with nothing visible on it.
func (m Mode) BlankLineWords() []uint32 {
	return append([]uint32{m.SyncWord()}, m.BlankWords()...)
}
