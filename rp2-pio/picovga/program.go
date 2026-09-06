// Package picovga is a Go port of the PIO programs from PicoVGA, Miroslav
// Nemecek's VGA/TV display library for the RP2040 and RP2350.
//
// PicoVGA generates a VGA signal entirely from PIO. A base state machine drives
// horizontal sync and pixel output by executing a stream of 32-bit control
// words fed through its TX FIFO. The top five bits of each control word are a
// jump address, so the base program acts as a small interpreter: it is handed a
// command, runs the corresponding routine, then fetches the next command with
// "out pc, 5".
//
// Up to three further state machines can overlay extra layers on top of the
// base image. Each runs one of the layer programs in this package and
// synchronises with the base machine through PIO IRQ 4.
//
// The base program must be loaded at instruction offset BaseOffset and a layer
// program at LayerOffset, because the jump addresses carried in control words
// are absolute. Jump targets inside the programs themselves are stored
// program-relative; PIO.AddProgram relocates them when it loads the program.
//
// Every program is parameterised by cpp, the number of PIO clock cycles per
// pixel. The original library patches the delay field of selected instructions
// at run time; this port assembles those delays directly.
//
// PicoVGA is by Miroslav Nemecek and is released under the Unlicense.
// https://github.com/Panda381/PicoVGA
package picovga

import (
	"errors"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

// Instruction memory offsets the programs must be loaded at.
const (
	// BaseOffset is where the base program must be loaded.
	BaseOffset = 17
	// LayerOffset is where an overlapped layer program must be loaded.
	LayerOffset = 0
)

// ErrCPPRange reports a clocks-per-pixel value the program cannot encode.
var ErrCPPRange = errors.New("picovga: clocks per pixel out of range for program")

// Program is an assembled PicoVGA PIO program along with the offsets a driver
// needs to load, wrap and steer it. Offsets are relative to the start of the
// program; a driver adds Origin to them once the program is loaded.
type Program struct {
	// Instructions is the assembled program.
	Instructions []uint16
	// Origin is the instruction memory offset the program must load at.
	Origin int8
	// WrapTarget and Wrap bound the hardware wrap region.
	WrapTarget uint8
	Wrap       uint8
	// Idle is the instruction the state machine parks on between frames.
	Idle uint8
	// Entry is the instruction a state machine is restarted at to begin a frame.
	Entry uint8
}

// Cmd assembles a base program control word: a jump address in the top five
// bits and a parameter in the remainder. addr is an absolute instruction
// address, so callers add BaseOffset to a label offset.
func Cmd(addr uint8, n uint32) uint32 {
	return uint32(addr)<<27 | n
}

// DarkCmd assembles a control word for the base program's dark routine, which
// holds colour on the output pins for n clock cycles. Colour 0 blanks the line.
func DarkCmd(n uint32, colour uint8) uint32 {
	return Cmd(BaseOffset+BaseDark, n<<8|uint32(colour))
}

// delay returns the extra wait to encode at an instruction whose clocks-per-pixel
// correction is corr, clamping as the original library does when cpp is below it.
func delay(cpp, corr uint8) uint8 {
	if cpp < corr {
		return 0
	}
	return cpp - corr
}

// Base program label offsets, relative to the start of the program.
const (
	BaseSync   = 0  // emit a sync pulse
	BaseEntry  = 2  // fetch and dispatch the next control word
	BaseDark   = 3  // hold a colour for a number of cycles
	BaseIRQSet = 7  // raise IRQ 4 to start the overlapped layers
	BaseOutput = 11 // stream pixels
	baseExtra1 = 12
)

// Clocks-per-pixel range the base program can encode. The upper bound follows
// from the four delay bits left once one side-set bit is taken.
const (
	BaseMinCPP = 2
	BaseMaxCPP = 17
)

// NewBase assembles the base layer program for cpp clock cycles per pixel. The
// program uses one side-set bit, driving the sync pin.
func NewBase(cpp uint8) (Program, error) {
	if cpp < BaseMinCPP || cpp > BaseMaxCPP {
		return Program{}, ErrCPPRange
	}
	asm := pio.AssemblerV0{SidesetBits: 1}
	d := delay(cpp, 2)
	return Program{
		Origin:     BaseOffset,
		WrapTarget: 10,
		Wrap:       14,
		Idle:       BaseEntry,
		Entry:      BaseEntry,
		Instructions: []uint16{
			// Sync pulse, held for the control word's cycle count.
			asm.Out(pio.OutDestX, 27).Side(1).Encode(),    //  0: out x, 27       side 1
			asm.Jmp(pio.JmpXNZeroDec, 1).Side(1).Encode(), //  1: jmp x--, 1      side 1
			asm.Out(pio.OutDestPC, 5).Side(1).Encode(),    //  2: out pc, 5       side 1
			// Dark, or a flat colour, for the control word's cycle count.
			asm.Out(pio.OutDestX, 19).Side(0).Encode(),    //  3: out x, 19      side 0
			asm.Out(pio.OutDestPins, 8).Side(0).Encode(),  //  4: out pins, 8    side 0
			asm.Jmp(pio.JmpXNZeroDec, 5).Side(0).Encode(), //  5: jmp x--, 5     side 0
			asm.Out(pio.OutDestPC, 5).Side(0).Encode(),    //  6: out pc, 5      side 0
			// Layer synchronisation: pulse IRQ 4 so the layers start together.
			asm.IRQClear(false, 4).Side(0).Encode(),        //  7: irq clear 4   side 0
			asm.Out(pio.OutDestNull, 27).Side(0).Encode(),  //  8: out null, 27  side 0
			asm.IRQSet(false, 4).Side(0).Delay(5).Encode(), //  9: irq set 4     side 0 [5]
			// .wrap_target
			asm.Out(pio.OutDestPC, 5).Side(0).Encode(), // 10: out pc, 5      side 0
			// Pixel output, one pixel every cpp cycles.
			asm.Out(pio.OutDestX, 27).Side(0).Encode(),             // 11: out x, 27    side 0
			asm.Out(pio.OutDestPins, 8).Side(0).Delay(d).Encode(),  // 12: out pins, 8  side 0 [cpp-2]
			asm.Jmp(pio.JmpXNZeroDec, baseExtra1).Side(0).Encode(), // 13: jmp x--, 12  side 0
			asm.Out(pio.OutDestPins, 8).Side(0).Delay(d).Encode(),  // 14: out pins, 8  side 0 [cpp-2]
			// .wrap
		},
	}, nil
}
