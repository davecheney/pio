package presto

import (
	"math/bits"
	"testing"
	"unsafe"
)

func TestIndexedSizes(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"frame", unsafe.Sizeof(IndexedFrame{}), 230400},
		{"lines", unsafe.Sizeof([2]Line{}), 1920},
		{"palette", unsafe.Sizeof(Palette{}), 1024},
		{"timing", unsafe.Sizeof([timingVFront * 4]uint32{}), 7968},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: got %d bytes, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestPaletteExpansion(t *testing.T) {
	var frame IndexedFrame
	var palette Palette
	for i := range palette {
		palette[i] = PackPixels(RGB565(uint8(i), uint8(i*3), uint8(i*7)))
	}
	for i := range frame {
		frame[i] = uint8(i)
	}
	var line Line
	for row := 0; row < Height; row++ {
		frame.Expand(&line, row, &palette)
		for i := range line {
			first, second := frame[row*Width+2*i], frame[row*Width+2*i+1]
			// Independently simulate the two PIO MOV REVERSE operations.
			gotFirst := uint16(bits.Reverse32(line[i]))
			gotSecond := uint16(bits.Reverse32(line[i] << 16))
			wantFirst := uint16(bits.Reverse32(palette[first]))
			wantSecond := uint16(bits.Reverse32(palette[second]))
			if gotFirst != wantFirst || gotSecond != wantSecond {
				t.Fatalf("row %d pair %d: got %04x/%04x, want %04x/%04x",
					row, i, gotFirst, gotSecond, wantFirst, wantSecond)
			}
		}
	}
}

func TestRGB565Packing(t *testing.T) {
	for _, tc := range []struct {
		r, g, b uint8
		rgb     uint16
		packed  uint32
	}{
		{255, 0, 0, 0xf800, 0xf800f800},
		{0, 255, 0, 0x07e0, 0x07e007e0},
		{0, 0, 255, 0x001f, 0x001f001f},
		{255, 255, 255, 0xffff, 0xffffffff},
		{0, 0, 0, 0, 0},
	} {
		if got := RGB565(tc.r, tc.g, tc.b); got != tc.rgb {
			t.Errorf("RGB565: got %04x, want %04x", got, tc.rgb)
		}
		if got := PackPixels(tc.rgb); got != tc.packed {
			t.Errorf("PackPixels(%04x): got %08x, want %08x", tc.rgb, got, tc.packed)
		}
	}
}

func mustPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected panic")
		}
	}()
	f()
}

func TestIndexedRows(t *testing.T) {
	var frame IndexedFrame
	for row := 0; row < Height; row++ {
		dst := frame.Row(row)
		if len(dst) != Width || cap(dst) != Width {
			t.Fatalf("row length/capacity = %d/%d", len(dst), cap(dst))
		}
		dst[0], dst[Width-1] = uint8(row), uint8(row+1)
	}
	for row := 0; row < Height; row++ {
		if frame[row*Width] != uint8(row) || frame[(row+1)*Width-1] != uint8(row+1) {
			t.Fatalf("overlapping row %d", row)
		}
	}
	for _, row := range []int{-1, Height, int(^uint(0) >> 1)} {
		mustPanic(t, func() { frame.Row(row) })
		mustPanic(t, func() { frame.Expand(new(Line), row, new(Palette)) })
	}
}

func TestIndexedTiming(t *testing.T) {
	var table [timingVFront * 4]uint32
	indexedTiming(&table)
	active, boundaries, clocks := 0, 0, 0
	for row := 0; row < timingVFront; row++ {
		for segment, width := range []int{4, 16, 30, 480} {
			word := table[row*4+segment]
			gotWidth := int(word>>16&0x3fff) + 3
			if gotWidth != width || (word>>30&1 != 0) != (segment != 1) ||
				(word>>31 != 0) != (row >= 8) {
				t.Fatalf("incorrect timing at row %d segment %d: %08x", row, segment, word)
			}
			clocks += gotWidth
			switch uint16(word) {
			case pioIRQ4:
				active++
				if row < 13 || row >= 493 || segment != 2 {
					t.Fatal("active-line grant outside active back porch")
				}
			case pioIRQ0:
				boundaries++
				if row != 493 || segment != 0 {
					t.Fatal("frame boundary precedes end of visible scanout")
				}
			case pioNop:
			default:
				t.Fatalf("unexpected instruction %04x", uint16(word))
			}
		}
	}
	if active != 480 || boundaries != 1 || clocks != 263940 {
		t.Fatalf("grants/boundaries/clocks = %d/%d/%d", active, boundaries, clocks)
	}
}

func BenchmarkExpand(b *testing.B) {
	frame, palette := new(IndexedFrame), new(Palette)
	for i := range frame {
		frame[i] = uint8(i)
	}
	for i := range palette {
		palette[i] = PackPixels(uint16(i * 257))
	}
	var line Line
	b.SetBytes(Width)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame.Expand(&line, i%Height, palette)
	}
}
