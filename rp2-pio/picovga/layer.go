package picovga

import (
	pio "github.com/tinygo-org/pio/rp2-pio"
)

// The overlapped layer programs all load at LayerOffset and use no side-set, so
// the full five delay bits are available. Each waits on IRQ 4 from the base
// program, counts out a start delay, then emits one pixel every cpp cycles.

// Key layer label offsets, relative to the start of the program.
const (
	KeyIdle   = 0
	KeyEntry  = 1
	keyCorr1  = 6
	KeyMinCPP = 6
	KeyMaxCPP = 37
)

// NewKeyLayer assembles the layer program that treats one key colour as
// transparent, for cpp clock cycles per pixel.
//
// Control word, shifted out left: 11 bits number of pixels - 1, 8 bits key
// colour, 13 bits start delay in clock cycles - 7 between IRQ and first pixel.
// The pixel count must be a multiple of 4.
func NewKeyLayer(cpp uint8) (Program, error) {
	if cpp < KeyMinCPP || cpp > KeyMaxCPP {
		return Program{}, ErrCPPRange
	}
	asm := pio.AssemblerV0{}
	return Program{
		Origin:     LayerOffset,
		WrapTarget: 0,
		Wrap:       12,
		Idle:       KeyIdle,
		Entry:      KeyEntry,
		Instructions: []uint16{
			// .wrap_target
			asm.Pull(false, true).Encode(),                                            //  0: pull block
			asm.WaitIRQ(false, false, 4).Encode(),                                     //  1: wait 0 irq, 4
			asm.Out(pio.OutDestX, 13).Encode(),                                        //  2: out x, 13
			asm.Jmp(pio.JmpXNZeroDec, 3).Encode(),                                     //  3: jmp x--, 3
			asm.Out(pio.OutDestY, 8).Encode(),                                         //  4: out y, 8
			asm.Out(pio.OutDestX, 11).Encode(),                                        //  5: out x, 11
			asm.Mov(pio.MovDestISR, pio.MovSrcX).Encode(),                             //  6: mov isr, x
			asm.Out(pio.OutDestX, 8).Encode(),                                         //  7: out x, 8
			asm.Jmp(pio.JmpXNotEqualY, 10).Encode(),                                   //  8: jmp x != y, 10
			asm.Jmp(pio.JmpAlways, 11).Encode(),                                       //  9: jmp 11
			asm.Mov(pio.MovDestPins, pio.MovSrcX).Encode(),                            // 10: mov pins, x
			asm.Mov(pio.MovDestX, pio.MovSrcISR).Delay(delay(cpp, keyCorr1)).Encode(), // 11: mov x, isr [cpp-6]
			asm.Jmp(pio.JmpXNZeroDec, 6).Encode(),                                     // 12: jmp x--, 6
			// .wrap
		},
	}, nil
}

// Black layer label offsets, relative to the start of the program.
const (
	BlackIdle   = 0
	BlackEntry  = 1
	blackCorr1  = 4
	blackCorr2  = 3
	BlackMinCPP = 4
	BlackMaxCPP = 34
)

// NewBlackLayer assembles the layer program that treats black as transparent,
// for cpp clock cycles per pixel. Black pixels cannot be displayed.
//
// Control word, shifted out left: 16 bits number of pixels - 1, 16 bits start
// delay in clock cycles - 5 between IRQ and first pixel. The pixel count must
// be a multiple of 4.
func NewBlackLayer(cpp uint8) (Program, error) {
	if cpp < BlackMinCPP || cpp > BlackMaxCPP {
		return Program{}, ErrCPPRange
	}
	asm := pio.AssemblerV0{}
	return Program{
		Origin:     LayerOffset,
		WrapTarget: 0,
		Wrap:       10,
		Idle:       BlackIdle,
		Entry:      BlackEntry,
		Instructions: []uint16{
			// .wrap_target
			asm.Pull(false, true).Encode(),                                      //  0: pull block
			asm.WaitIRQ(false, false, 4).Encode(),                               //  1: wait 0 irq, 4
			asm.Out(pio.OutDestX, 16).Encode(),                                  //  2: out x, 16
			asm.Jmp(pio.JmpXNZeroDec, 3).Encode(),                               //  3: jmp x--, 3
			asm.Out(pio.OutDestX, 16).Encode(),                                  //  4: out x, 16
			asm.Out(pio.OutDestY, 8).Encode(),                                   //  5: out y, 8
			asm.Jmp(pio.JmpYZero, 10).Encode(),                                  //  6: jmp !y, 10
			asm.Mov(pio.MovDestPins, pio.MovSrcY).Encode(),                      //  7: mov pins, y
			asm.Jmp(pio.JmpXNZeroDec, 5).Delay(delay(cpp, blackCorr1)).Encode(), //  8: jmp x--, 5 [cpp-4]
			asm.Jmp(pio.JmpAlways, 0).Encode(),                                  //  9: jmp 0
			asm.Jmp(pio.JmpXNZeroDec, 5).Delay(delay(cpp, blackCorr2)).Encode(), // 10: jmp x--, 5 [cpp-3]
			// .wrap
		},
	}, nil
}

// White layer label offsets, relative to the start of the program.
const (
	WhiteIdle   = 0
	WhiteEntry  = 1
	whiteCorr1  = 4
	WhiteMinCPP = 4
	WhiteMaxCPP = 35
)

// NewWhiteLayer assembles the layer program that treats white as transparent,
// for cpp clock cycles per pixel. White pixels cannot be displayed and source
// pixels must be incremented by one.
//
// Control word, shifted out left: 16 bits number of pixels - 1, 16 bits start
// delay in clock cycles - 5 between IRQ and first pixel. The pixel count must
// be a multiple of 4.
func NewWhiteLayer(cpp uint8) (Program, error) {
	if cpp < WhiteMinCPP || cpp > WhiteMaxCPP {
		return Program{}, ErrCPPRange
	}
	asm := pio.AssemblerV0{}
	return Program{
		Origin:     LayerOffset,
		WrapTarget: 0,
		Wrap:       9,
		Idle:       WhiteIdle,
		Entry:      WhiteEntry,
		Instructions: []uint16{
			// .wrap_target
			asm.Pull(false, true).Encode(),                                      //  0: pull block
			asm.WaitIRQ(false, false, 4).Encode(),                               //  1: wait 0 irq, 4
			asm.Out(pio.OutDestX, 16).Encode(),                                  //  2: out x, 16
			asm.Jmp(pio.JmpXNZeroDec, 3).Encode(),                               //  3: jmp x--, 3
			asm.Out(pio.OutDestX, 16).Encode(),                                  //  4: out x, 16
			asm.Out(pio.OutDestY, 8).Encode(),                                   //  5: out y, 8
			asm.Jmp(pio.JmpYNZeroDec, 8).Encode(),                               //  6: jmp y--, 8
			asm.Jmp(pio.JmpAlways, 9).Encode(),                                  //  7: jmp 9
			asm.Mov(pio.MovDestPins, pio.MovSrcY).Encode(),                      //  8: mov pins, y
			asm.Jmp(pio.JmpXNZeroDec, 5).Delay(delay(cpp, whiteCorr1)).Encode(), //  9: jmp x--, 5 [cpp-4]
			// .wrap
		},
	}, nil
}

// Mono layer label offsets, relative to the start of the program.
const (
	MonoIdle   = 0
	MonoEntry  = 1
	monoCorr1  = 4
	monoCorr2  = 2
	MonoMinCPP = 2
	MonoMaxCPP = 33
)

// NewMonoLayer assembles the layer program that draws either a monochrome
// transparent pattern or opaque colour pixels, for cpp clock cycles per pixel.
//
// Control word, shifted out left: 1 bit mode flag (0 opaque colour, 1 mono
// transparent), 11 bits number of pixels - 1, 8 bits key colour, 12 bits start
// delay in clock cycles - 8 between IRQ and the first mono pixel, or - 6 for a
// colour pixel. The pixel count must be a multiple of 32 in mono mode, or 4 in
// colour mode. Mono mode needs at least four clock cycles per pixel.
func NewMonoLayer(cpp uint8) (Program, error) {
	if cpp < MonoMinCPP || cpp > MonoMaxCPP {
		return Program{}, ErrCPPRange
	}
	asm := pio.AssemblerV0{}
	return Program{
		Origin:     LayerOffset,
		WrapTarget: 0,
		Wrap:       15,
		Idle:       MonoIdle,
		Entry:      MonoEntry,
		Instructions: []uint16{
			// .wrap_target
			asm.Pull(false, true).Encode(),                                      //  0: pull block
			asm.WaitIRQ(false, false, 4).Encode(),                               //  1: wait 0 irq, 4
			asm.Out(pio.OutDestX, 12).Encode(),                                  //  2: out x, 12
			asm.Jmp(pio.JmpXNZeroDec, 3).Encode(),                               //  3: jmp x--, 3
			asm.Out(pio.OutDestISR, 8).Encode(),                                 //  4: out isr, 8
			asm.Out(pio.OutDestY, 11).Encode(),                                  //  5: out y, 11
			asm.Out(pio.OutDestX, 1).Encode(),                                   //  6: out x, 1
			asm.Jmp(pio.JmpXZero, 14).Encode(),                                  //  7: jmp !x, 14
			asm.Out(pio.OutDestX, 1).Encode(),                                   //  8: out x, 1
			asm.Jmp(pio.JmpXZero, 11).Encode(),                                  //  9: jmp !x, 11
			asm.Jmp(pio.JmpAlways, 12).Encode(),                                 // 10: jmp 12
			asm.Mov(pio.MovDestPins, pio.MovSrcISR).Encode(),                    // 11: mov pins, isr
			asm.Jmp(pio.JmpYNZeroDec, 8).Delay(delay(cpp, monoCorr1)).Encode(),  // 12: jmp y--, 8 [cpp-4]
			asm.Jmp(pio.JmpAlways, 0).Encode(),                                  // 13: jmp 0
			asm.Out(pio.OutDestPins, 8).Encode(),                                // 14: out pins, 8
			asm.Jmp(pio.JmpYNZeroDec, 14).Delay(delay(cpp, monoCorr2)).Encode(), // 15: jmp y--, 14 [cpp-2]
			// .wrap
		},
	}, nil
}

// RLE layer label offsets, relative to the start of the program. Idle, Skip,
// Skip1, Run, Raw1 and Raw are the token dispatch targets: the compressed
// stream carries these addresses in the low byte of each token.
const (
	RLEIdle  = 0
	RLEEntry = 1
	RLESkip  = 5
	RLESkip1 = 6
	RLERun   = 7
	RLERaw1  = 11
	RLERaw   = 14

	rleCorrSkip  = 1
	rleCorrSkip1 = 3
	rleCorrRun   = 2
	rleCorrRun2  = 2
	rleCorrRaw1  = 3
	rleCorrRaw   = 2
	rleCorrRaw2  = 3

	RLEMinCPP = 3
	RLEMaxCPP = 32
)

// NewRLELayer assembles the run-length compressed layer program for cpp clock
// cycles per pixel. Its input is shifted out left with the low byte first, and
// consists of tokens whose second byte is the address of the routine handling
// them.
func NewRLELayer(cpp uint8) (Program, error) {
	if cpp < RLEMinCPP || cpp > RLEMaxCPP {
		return Program{}, ErrCPPRange
	}
	asm := pio.AssemblerV0{}
	return Program{
		Origin:     LayerOffset,
		WrapTarget: 12,
		Wrap:       16,
		Idle:       RLEIdle,
		Entry:      RLEEntry,
		Instructions: []uint16{
			asm.Out(pio.OutDestPC, 8).Encode(),                                            //  0: out pc, 8      idle
			asm.WaitIRQ(false, false, 4).Encode(),                                         //  1: wait 0 irq, 4  entry
			asm.Out(pio.OutDestX, 32).Delay(2).Encode(),                                   //  2: out x, 32 [2]
			asm.Jmp(pio.JmpXNZeroDec, 3).Encode(),                                         //  3: jmp x--, 3
			asm.Jmp(pio.JmpAlways, 12).Encode(),                                           //  4: jmp 12
			asm.Jmp(pio.JmpXNZeroDec, 5).Delay(delay(cpp, rleCorrSkip)).Encode(),          //  5: jmp x--, 5 [cpp-1]  skip
			asm.Jmp(pio.JmpAlways, 12).Delay(delay(cpp, rleCorrSkip1)).Encode(),           //  6: jmp 12 [cpp-3]      skip1
			asm.Mov(pio.MovDestPins, pio.MovSrcX).Delay(delay(cpp, rleCorrRun)).Encode(),  //  7: mov pins, x [cpp-2] run
			asm.Out(pio.OutDestY, 8).Encode(),                                             //  8: out y, 8
			asm.Mov(pio.MovDestPins, pio.MovSrcX).Delay(delay(cpp, rleCorrRun2)).Encode(), //  9: mov pins, x [cpp-2]
			asm.Jmp(pio.JmpYNZeroDec, 9).Encode(),                                         // 10: jmp y--, 9
			asm.Mov(pio.MovDestPins, pio.MovSrcX).Delay(delay(cpp, rleCorrRaw1)).Encode(), // 11: mov pins, x [cpp-3] raw1
			// .wrap_target
			asm.Out(pio.OutDestX, 8).Encode(),                                   // 12: out x, 8   raw_next
			asm.Out(pio.OutDestPC, 8).Encode(),                                  // 13: out pc, 8
			asm.Out(pio.OutDestPins, 8).Delay(delay(cpp, rleCorrRaw)).Encode(),  // 14: out pins, 8 [cpp-2] raw
			asm.Jmp(pio.JmpXNZeroDec, 14).Encode(),                              // 15: jmp x--, 14
			asm.Out(pio.OutDestPins, 8).Delay(delay(cpp, rleCorrRaw2)).Encode(), // 16: out pins, 8 [cpp-3]
			// .wrap
		},
	}, nil
}
