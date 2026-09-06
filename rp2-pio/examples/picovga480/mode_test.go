package main

import (
	"testing"

	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

func TestTimings(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want int
	}{
		{"hTotal", hTotal, 800},
		{"vTotal", vTotal, 525},
		{"fbWidth", fbWidth, 320},
		{"fbHeight", fbHeight, 240},
		{"cpp", cpp, 8},
		{"lineCycles", lineCycles, 3200},
		{"pioFreq", pioFreq, 100_700_000},
		{"framebuffer bytes", fbWidth * fbHeight * 2, 153_600},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	// VESA gives 31.469kHz and 59.94Hz for this mode.
	if line := pixelClock / hTotal; line < 31_400 || line > 31_540 {
		t.Errorf("line rate = %dHz, want about 31469Hz", line)
	}
	if frame := pixelClock / (hTotal * vTotal); frame != 59 {
		t.Errorf("frame rate = %dHz, want 59.94Hz", frame)
	}
	// The base program can only encode so many cycles per pixel.
	if cpp < picovga.BaseMinCPP || cpp > picovga.BaseMaxCPP {
		t.Errorf("cpp %d outside the base program's range %d..%d", cpp, picovga.BaseMinCPP, picovga.BaseMaxCPP)
	}
}

// TestLineCyclesExact is the check that decides whether the picture holds
// still: both kinds of scanline must occupy exactly one line of PIO clocks.
func TestLineCyclesExact(t *testing.T) {
	visible := []uint32{syncWord(), backPorchWord(), outputWord(), frontPorchWord()}
	if got := lineCyclesOf(visible); got != lineCycles {
		t.Errorf("visible line = %d cycles, want %d", got, lineCycles)
	}
	if got := lineCyclesOf(blankLineWords()); got != lineCycles {
		t.Errorf("blank line = %d cycles, want %d", got, lineCycles)
	}
	// The pixels must account for exactly the visible part of the line.
	if got, want := fbWidth*cpp, hVisible*clocksPerPixel; got != want {
		t.Errorf("pixel cycles = %d, want %d", got, want)
	}
}

// TestWordEncoding checks each control word dispatches to the right routine and
// carries a counter that fits its field.
func TestWordEncoding(t *testing.T) {
	for _, tc := range []struct {
		name     string
		word     uint32
		wantAddr uint8
		wantN    uint32
	}{
		{"sync", syncWord(), picovga.BaseOffset + picovga.BaseSync, hSync*clocksPerPixel - 3},
		{"output", outputWord(), picovga.BaseOffset + picovga.BaseOutput, fbWidth - 2},
	} {
		if addr := uint8(tc.word >> 27); addr != tc.wantAddr {
			t.Errorf("%s dispatches to %d, want %d", tc.name, addr, tc.wantAddr)
		}
		if n := tc.word & 0x07ff_ffff; n != tc.wantN {
			t.Errorf("%s counter = %d, want %d", tc.name, n, tc.wantN)
		}
	}
	for _, tc := range []struct {
		name  string
		word  uint32
		wantN uint32
	}{
		{"back porch", backPorchWord(), hBack*clocksPerPixel - 5},
		{"front porch", frontPorchWord(), hFront*clocksPerPixel - 4},
	} {
		if addr := uint8(tc.word >> 27); addr != picovga.BaseOffset+picovga.BaseDark {
			t.Errorf("%s dispatches to %d, want the dark routine", tc.name, addr)
		}
		if n := (tc.word >> 16) & picovga.Dark16MaxCount; n != tc.wantN {
			t.Errorf("%s counter = %d, want %d", tc.name, n, tc.wantN)
		}
		if uint16(tc.word) != 0 {
			t.Errorf("%s colour = %#04x, want black", tc.name, uint16(tc.word))
		}
	}
}

// TestBlankRunSplit checks the blanking run, too long for one 11 bit counter,
// is split into commands totalling exactly the intended time.
func TestBlankRunSplit(t *testing.T) {
	words := blankWords()
	if len(words) < 2 {
		t.Fatalf("blanking fits in %d words, expected it to be split", len(words))
	}
	total := 0
	for i, w := range words {
		n := (w >> 16) & picovga.Dark16MaxCount
		if n > picovga.Dark16MaxCount {
			t.Errorf("word %d counter %d overflows the 11 bit field", i, n)
		}
		total += wordCycles(w)
	}
	if want := lineCycles - hSync*clocksPerPixel; total != want {
		t.Errorf("blank run = %d cycles, want %d", total, want)
	}
}

// TestFrameWalk walks a whole frame the way the display loop does.
func TestFrameWalk(t *testing.T) {
	rows := make([]int, fbHeight)
	sync, visible, cycles := 0, 0, 0
	visibleCycles := lineCyclesOf([]uint32{syncWord(), backPorchWord(), outputWord(), frontPorchWord()})
	blankCycles := lineCyclesOf(blankLineWords())
	for line := 0; line < vTotal; line++ {
		if inVSync(line) {
			sync++
		}
		if row, ok := visibleLine(line); ok {
			if row < 0 || row >= fbHeight {
				t.Fatalf("line %d maps to row %d, outside the framebuffer", line, row)
			}
			rows[row]++
			visible++
			cycles += visibleCycles
		} else {
			cycles += blankCycles
		}
	}
	if sync != vSync {
		t.Errorf("vertical sync asserted for %d lines, want %d", sync, vSync)
	}
	if visible != vVisible {
		t.Errorf("%d visible lines, want %d", visible, vVisible)
	}
	for row, n := range rows {
		if n != scale {
			t.Fatalf("row %d shown %d times, want %d", row, n, scale)
		}
	}
	if want := vTotal * lineCycles; cycles != want {
		t.Errorf("frame = %d cycles, want %d", cycles, want)
	}
}
