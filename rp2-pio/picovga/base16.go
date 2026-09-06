package picovga

import (
	pio "github.com/tinygo-org/pio/rp2-pio"
)

// The RP2040 VGA reference design, which the Pimoroni Pico VGA Demo Base
// implements, wires five resistor ladder bits to each colour channel: red on
// GP0 to GP4, green on GP6 to GP10 and blue on GP11 to GP15, leaving GP5 to the
// SD card clock. The channels are therefore six pins apart, and a PIO "out
// pins" writes a run of consecutive pins, so no eight pin window reaches all
// three channels. Driving that board needs sixteen pins per pixel.
//
// NewBase16 is the base program widened to do that. It is otherwise the program
// NewBase assembles: same routines at the same offsets, same sync side-set.
//
// Widening the pixel writes costs the dark routine its colour field. A control
// word has 32 bits, five of which are the jump address, so an eight bit colour
// could sit beside a nineteen bit counter but a sixteen bit colour cannot. The
// counter shrinks to eleven bits, which caps one dark command at
// Dark16MaxCount+4 cycles; blanking longer than that is split across two
// commands, which Mode.BlankWords does.

// Dark16MaxCount is the largest counter a 16 bit dark control word can carry.
const Dark16MaxCount = 1<<11 - 1

// Clocks-per-pixel range the 16 bit base program can encode, as for NewBase.
const (
	Base16MinCPP = BaseMinCPP
	Base16MaxCPP = BaseMaxCPP
)

// Dark16Cmd assembles a control word for the 16 bit dark routine, holding
// colour on the output pins for n+4 clock cycles. n must not exceed
// Dark16MaxCount.
func Dark16Cmd(n uint32, colour uint16) uint32 {
	return Cmd(BaseOffset+BaseDark, n<<16|uint32(colour))
}

// NewBase16 assembles the 16 bit per pixel base layer program for cpp clock
// cycles per pixel. Label offsets are those of NewBase: BaseSync, BaseEntry,
// BaseDark, BaseIRQSet and BaseOutput.
func NewBase16(cpp uint8) (Program, error) {
	if cpp < Base16MinCPP || cpp > Base16MaxCPP {
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
			asm.Out(pio.OutDestX, 11).Side(0).Encode(),    //  3: out x, 11      side 0
			asm.Out(pio.OutDestPins, 16).Side(0).Encode(), //  4: out pins, 16   side 0
			asm.Jmp(pio.JmpXNZeroDec, 5).Side(0).Encode(), //  5: jmp x--, 5     side 0
			asm.Out(pio.OutDestPC, 5).Side(0).Encode(),    //  6: out pc, 5      side 0
			// Layer synchronisation: pulse IRQ 4 so the layers start together.
			asm.IRQClear(false, 4).Side(0).Encode(),        //  7: irq clear 4   side 0
			asm.Out(pio.OutDestNull, 27).Side(0).Encode(),  //  8: out null, 27  side 0
			asm.IRQSet(false, 4).Side(0).Delay(5).Encode(), //  9: irq set 4     side 0 [5]
			// .wrap_target
			asm.Out(pio.OutDestPC, 5).Side(0).Encode(), // 10: out pc, 5      side 0
			// Pixel output, one 16 bit pixel every cpp cycles.
			asm.Out(pio.OutDestX, 27).Side(0).Encode(),             // 11: out x, 27     side 0
			asm.Out(pio.OutDestPins, 16).Side(0).Delay(d).Encode(), // 12: out pins, 16  side 0 [cpp-2]
			asm.Jmp(pio.JmpXNZeroDec, baseExtra1).Side(0).Encode(), // 13: jmp x--, 12   side 0
			asm.Out(pio.OutDestPins, 16).Side(0).Delay(d).Encode(), // 14: out pins, 16  side 0 [cpp-2]
			// .wrap
		},
	}, nil
}
