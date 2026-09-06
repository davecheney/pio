package picovga

import "testing"

// The golden instruction words below were produced by the pico-sdk pioasm from
// the original PicoVGA vga.pio:
//
//	pioasm -o go vga.pio
//
// pioasm emits zero in the delay field of every instruction this port
// parameterises by clocks per pixel, so the comparison masks those fields out
// and checks them separately.

// extra is an instruction whose delay field carries a clocks-per-pixel wait,
// together with the correction subtracted from cpp to produce it.
type extra struct {
	offset uint8
	corr   uint8
}

type progCase struct {
	name        string
	build       func(uint8) (Program, error)
	golden      []uint16
	sidesetBits uint8
	extras      []extra
	cpp         uint8
	origin      int8
	wrapTarget  uint8
	wrap        uint8
	minCPP      uint8
	maxCPP      uint8
}

func cases() []progCase {
	return []progCase{
		{
			name: "base16", build: NewBase16, cpp: 8, sidesetBits: 1,
			origin: BaseOffset, wrapTarget: 10, wrap: 14,
			minCPP: Base16MinCPP, maxCPP: Base16MaxCPP,
			extras: []extra{{12, 2}, {14, 2}},
			golden: []uint16{
				0x703b, 0x1041, 0x70a5, 0x602b, 0x6010, 0x0045, 0x60a5, 0xc044,
				0x607b, 0xc504, 0x60a5, 0x603b, 0x6010, 0x004c, 0x6010,
			},
		},
		{
			name: "base", build: NewBase, cpp: 8, sidesetBits: 1,
			origin: BaseOffset, wrapTarget: 10, wrap: 14,
			minCPP: BaseMinCPP, maxCPP: BaseMaxCPP,
			extras: []extra{{12, 2}, {14, 2}},
			golden: []uint16{
				0x703b, 0x1041, 0x70a5, 0x6033, 0x6008, 0x0045, 0x60a5, 0xc044,
				0x607b, 0xc504, 0x60a5, 0x603b, 0x6008, 0x004c, 0x6008,
			},
		},
		{
			name: "key", build: NewKeyLayer, cpp: 12,
			origin: LayerOffset, wrapTarget: 0, wrap: 12,
			minCPP: KeyMinCPP, maxCPP: KeyMaxCPP,
			extras: []extra{{11, 6}},
			golden: []uint16{
				0x80a0, 0x2044, 0x602d, 0x0043, 0x6048, 0x602b, 0xa0c1, 0x6028,
				0x00aa, 0x000b, 0xa001, 0xa026, 0x0046,
			},
		},
		{
			name: "black", build: NewBlackLayer, cpp: 10,
			origin: LayerOffset, wrapTarget: 0, wrap: 10,
			minCPP: BlackMinCPP, maxCPP: BlackMaxCPP,
			extras: []extra{{8, 4}, {10, 3}},
			golden: []uint16{
				0x80a0, 0x2044, 0x6030, 0x0043, 0x6030, 0x6048, 0x006a, 0xa002,
				0x0045, 0x0000, 0x0045,
			},
		},
		{
			name: "white", build: NewWhiteLayer, cpp: 10,
			origin: LayerOffset, wrapTarget: 0, wrap: 9,
			minCPP: WhiteMinCPP, maxCPP: WhiteMaxCPP,
			extras: []extra{{9, 4}},
			golden: []uint16{
				0x80a0, 0x2044, 0x6030, 0x0043, 0x6030, 0x6048, 0x0088, 0x0009,
				0xa002, 0x0045,
			},
		},
		{
			name: "mono", build: NewMonoLayer, cpp: 10,
			origin: LayerOffset, wrapTarget: 0, wrap: 15,
			minCPP: MonoMinCPP, maxCPP: MonoMaxCPP,
			extras: []extra{{12, 4}, {15, 2}},
			golden: []uint16{
				0x80a0, 0x2044, 0x602c, 0x0043, 0x60c8, 0x604b, 0x6021, 0x002e,
				0x6021, 0x002b, 0x000c, 0xa006, 0x0088, 0x0000, 0x6008, 0x008e,
			},
		},
		{
			name: "rle", build: NewRLELayer, cpp: 9,
			origin: LayerOffset, wrapTarget: 12, wrap: 16,
			minCPP: RLEMinCPP, maxCPP: RLEMaxCPP,
			extras: []extra{{5, 1}, {6, 3}, {7, 2}, {9, 2}, {11, 3}, {14, 2}, {16, 3}},
			golden: []uint16{
				0x60a8, 0x2044, 0x6220, 0x0043, 0x000c, 0x0045, 0x000c, 0xa001,
				0x6048, 0xa001, 0x0089, 0xa001, 0x6028, 0x60a8, 0x6008, 0x004e,
				0x6008,
			},
		},
	}
}

// delayMask returns the mask of the delay field left once the side-set bits are
// taken from the top of the five-bit delay/side-set field.
func delayMask(sidesetBits uint8) uint16 {
	return uint16(0x1f>>sidesetBits) << 8
}

// TestProgramsMatchPIOASM checks each ported program against the words pioasm
// produced for the same source, ignoring the cpp-derived delay fields.
func TestProgramsMatchPIOASM(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.build(tc.cpp)
			if err != nil {
				t.Fatalf("build(%d): %v", tc.cpp, err)
			}
			if len(p.Instructions) != len(tc.golden) {
				t.Fatalf("length = %d, want %d", len(p.Instructions), len(tc.golden))
			}
			mask := delayMask(tc.sidesetBits)
			isExtra := make(map[uint8]bool, len(tc.extras))
			for _, e := range tc.extras {
				isExtra[e.offset] = true
			}
			for i, got := range p.Instructions {
				want := tc.golden[i]
				if isExtra[uint8(i)] {
					got &^= mask // delay is cpp-derived, checked separately
				}
				if got != want {
					t.Errorf("instruction %d = %#04x, want %#04x", i, got, want)
				}
			}
		})
	}
}

// TestExtraDelays checks that each cpp-derived instruction carries the wait the
// original library patches in, across the program's whole cpp range.
func TestExtraDelays(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			mask := delayMask(tc.sidesetBits)
			for cpp := tc.minCPP; cpp <= tc.maxCPP; cpp++ {
				p, err := tc.build(cpp)
				if err != nil {
					t.Fatalf("build(%d): %v", cpp, err)
				}
				for _, e := range tc.extras {
					want := uint16(delay(cpp, e.corr)) << 8
					if want&^mask != 0 {
						t.Fatalf("cpp %d: delay at %d overflows the delay field", cpp, e.offset)
					}
					if got := p.Instructions[e.offset] & mask; got != want {
						t.Errorf("cpp %d: delay at %d = %#04x, want %#04x", cpp, e.offset, got, want)
					}
				}
			}
		})
	}
}

// TestProgramLayout checks the offsets a driver needs to load and steer each
// program.
func TestProgramLayout(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.build(tc.cpp)
			if err != nil {
				t.Fatalf("build(%d): %v", tc.cpp, err)
			}
			if p.Origin != tc.origin {
				t.Errorf("Origin = %d, want %d", p.Origin, tc.origin)
			}
			if p.WrapTarget != tc.wrapTarget {
				t.Errorf("WrapTarget = %d, want %d", p.WrapTarget, tc.wrapTarget)
			}
			if p.Wrap != tc.wrap {
				t.Errorf("Wrap = %d, want %d", p.Wrap, tc.wrap)
			}
			if int(p.Wrap) >= len(p.Instructions) {
				t.Errorf("Wrap %d is past the end of a %d instruction program", p.Wrap, len(p.Instructions))
			}
		})
	}
}

// TestCPPRange checks that clocks-per-pixel values outside a program's encodable
// range are rejected rather than silently mis-assembled.
func TestCPPRange(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.build(tc.minCPP - 1); err != ErrCPPRange {
				t.Errorf("build(%d) error = %v, want ErrCPPRange", tc.minCPP-1, err)
			}
			if _, err := tc.build(tc.maxCPP + 1); err != ErrCPPRange {
				t.Errorf("build(%d) error = %v, want ErrCPPRange", tc.maxCPP+1, err)
			}
		})
	}
}

// TestBaseFitsWithLayer checks the two programs co-reside: a layer program at
// LayerOffset must not run into the base program at BaseOffset.
func TestBaseFitsWithLayer(t *testing.T) {
	base, err := NewBase(8)
	if err != nil {
		t.Fatal(err)
	}
	if got := int(base.Origin) + len(base.Instructions); got > 32 {
		t.Errorf("base program ends at %d, past the 32 instruction memory", got)
	}
	for _, tc := range cases() {
		if tc.origin != LayerOffset {
			continue
		}
		p, err := tc.build(tc.cpp)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Instructions) > BaseOffset {
			t.Errorf("%s layer is %d instructions, overruns the base program at %d",
				tc.name, len(p.Instructions), BaseOffset)
		}
	}
}
