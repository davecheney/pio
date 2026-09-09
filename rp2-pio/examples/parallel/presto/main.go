//go:build rp2350 && rp2350b

package main

import (
	"device/rp"
	"machine"
	"math/bits"
	"time"
	"unsafe"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

const (
	width  = 480
	height = 480

	lcdD0     = machine.GPIO1
	lcdHSync  = machine.GPIO19
	lcdVSync  = machine.GPIO20
	lcdDE     = machine.GPIO21
	lcdDotClk = machine.GPIO22

	lcdSCK       = machine.GPIO26
	lcdDAT       = machine.GPIO27
	lcdCS        = machine.GPIO28
	lcdBacklight = machine.GPIO45

	// maxTimingHz is the ceiling Pimoroni's driver uses when picking the
	// timing state machine's integer clock divider (st7701.cpp's
	// max_pio_clk). The panel free-runs its own scan if the frame rate
	// drops far below the ~47 Hz this yields, which shows up as a
	// vertically stretched, partially drawn frame.
	maxTimingHz = 34_000_000
)

const (
	timingVPulse   = 8
	timingVBack    = 5 + timingVPulse
	timingVDisplay = height + timingVBack
	timingVFront   = 5 + timingVDisplay

	timingHFront   = 4
	timingHPulse   = 16
	timingHBack    = 30
	timingHDisplay = width
)

const (
	cmdSWRESET    = 0x01
	cmdSLPOUT     = 0x11
	cmdDISPON     = 0x29
	cmdMADCTL     = 0x36
	cmdCOLMOD     = 0x3a
	cmdCND2BKxSEL = 0xff
)

const (
	pioNop  = 0xb042
	pioIRQ4 = 0xd004
)

const (
	black   = 0x0000
	white   = 0xffff
	red     = 0xf800
	green   = 0x07e0
	blue    = 0x001f
	yellow  = 0xffe0
	cyan    = 0x07ff
	magenta = 0xf81f
)

const (
	borderWidth = 6  // white frame around the whole screen
	cornerSize  = 60 // size of the orientation marker squares
	crossWidth  = 4  // thickness of the center crosshair
	numBars     = 8  // number of vertical color bars
)

var (
	lines [numLineKinds][width / 2]uint32
	bars  = [numBars]uint16{white, yellow, cyan, green, magenta, red, blue, black}
)

// The test pattern only ever contains four distinct scanlines, so they are
// all built once at startup and each active row simply points its DMA at
// the right one. Generating pixels on the fly instead is far too slow: a
// line lasts about 42 us, which is nowhere near enough time to evaluate
// the pattern for 480 pixels.
const (
	lineWhite        = iota // border rows and the crosshair's horizontal arm
	lineTopCorner           // rows crossing the top-left/top-right markers
	lineBottomCorner        // rows crossing the bottom-left/right markers
	lineBars                // plain colour bars plus the crosshair's stem
	numLineKinds
)

func main() {
	time.Sleep(2 * time.Second)

	initControlPins()
	initDisplay()
	buildLines()

	p := pio.PIO1
	dataSM, err := p.ClaimStateMachine()
	must("claim data state machine", err)
	timingSM, err := p.ClaimStateMachine()
	must("claim timing state machine", err)

	initScanoutPIO(p, dataSM, timingSM)
	setBacklight(true)

	for {
		scanFrame(timingSM, dataSM)
	}
}

func initControlPins() {
	for _, pin := range []machine.Pin{lcdCS, lcdSCK, lcdDAT, lcdBacklight} {
		pin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	}
	lcdCS.High()
	lcdSCK.Low()
	lcdDAT.Low()
	setBacklight(false)
}

func setBacklight(on bool) {
	if on {
		lcdBacklight.High()
	} else {
		lcdBacklight.Low()
	}
}

func initDisplay() {
	command(cmdSWRESET)
	time.Sleep(150 * time.Millisecond)

	command(cmdCND2BKxSEL, 0x77, 0x01, 0x00, 0x00, 0x10)
	command(0xc0, 0x3b, 0x00)
	command(0xc1, 0x0d, 0x02)
	command(0xc2, 0x31, 0x01)
	command(0xcd, 0x08)
	command(0xb0, 0x00, 0x11, 0x18, 0x0e, 0x11, 0x06, 0x07, 0x08, 0x07, 0x22, 0x04, 0x12, 0x0f, 0xaa, 0x31, 0x18)
	command(0xb1, 0x00, 0x11, 0x19, 0x0e, 0x12, 0x07, 0x08, 0x08, 0x08, 0x22, 0x04, 0x11, 0x11, 0xa9, 0x32, 0x18)
	command(0xc3, 0x80, 0x2e, 0x0e)

	command(cmdCND2BKxSEL, 0x77, 0x01, 0x00, 0x00, 0x11)
	command(0xb0, 0x60)
	command(0xb1, 0x32)
	command(0xb2, 0x07)
	command(0xb3, 0x80)
	command(0xb5, 0x49)
	command(0xb7, 0x85)
	command(0xb8, 0x21)
	command(0xc1, 0x78)
	command(0xc2, 0x78)

	command(0xe0, 0x00, 0x1b, 0x02)
	command(0xe1, 0x08, 0xa0, 0x00, 0x00, 0x07, 0xa0, 0x00, 0x00, 0x00, 0x44, 0x44)
	command(0xe2, 0x11, 0x11, 0x44, 0x44, 0xed, 0xa0, 0x00, 0x00, 0xec, 0xa0, 0x00, 0x00)
	command(0xe3, 0x00, 0x00, 0x11, 0x11)
	command(0xe4, 0x44, 0x44)
	command(0xe5, 0x0a, 0xe9, 0xd8, 0xa0, 0x0c, 0xeb, 0xd8, 0xa0, 0x0e, 0xed, 0xd8, 0xa0, 0x10, 0xef, 0xd8, 0xa0)
	command(0xe6, 0x00, 0x00, 0x11, 0x11)
	command(0xe7, 0x44, 0x44)
	command(0xe8, 0x09, 0xe8, 0xd8, 0xa0, 0x0b, 0xea, 0xd8, 0xa0, 0x0d, 0xec, 0xd8, 0xa0, 0x0f, 0xee, 0xd8, 0xa0)
	command(0xeb, 0x02, 0x00, 0xe4, 0xe4, 0x88, 0x00, 0x40)
	command(0xec, 0x3c, 0x00)
	command(0xed, 0xab, 0x89, 0x76, 0x54, 0x02, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x20, 0x45, 0x67, 0x98, 0xba)
	command(cmdMADCTL, 0x00)

	command(cmdCND2BKxSEL, 0x77, 0x01, 0x00, 0x00, 0x13)
	command(0xe5, 0xe4)

	command(cmdCND2BKxSEL, 0x77, 0x01, 0x00, 0x00, 0x00)
	// Pimoroni's shipped firmware always sets COLMOD to 18bpp/RGB666,
	// even when driving the panel with a 16-bit-wide RGB565 data bus (the
	// two extra data lines are simply tied low): see st7701.cpp's
	// common_init(), which sends 0x66 unconditionally.
	command(cmdCOLMOD, 0x66)
	command(0x21)
	time.Sleep(time.Millisecond)
	command(cmdSLPOUT)
	time.Sleep(120 * time.Millisecond)
	command(cmdDISPON)
	time.Sleep(50 * time.Millisecond)
}

func command(cmd byte, data ...byte) {
	lcdCS.Low()
	write9(false, cmd)
	for _, b := range data {
		write9(true, b)
	}
	lcdCS.High()
}

func write9(data bool, value byte) {
	if data {
		lcdDAT.High()
	} else {
		lcdDAT.Low()
	}
	pulseClock()
	for bit := 7; bit >= 0; bit-- {
		if value&(1<<uint(bit)) != 0 {
			lcdDAT.High()
		} else {
			lcdDAT.Low()
		}
		pulseClock()
	}
}

func pulseClock() {
	lcdSCK.High()
	lcdSCK.Low()
}

func initScanoutPIO(p *pio.PIO, dataSM, timingSM pio.StateMachine) {
	timingHz, dataHz := scanoutClocks()
	asm := pio.AssemblerV0{SidesetBits: 1}

	// This is Pimoroni's st7701_parallel program (16 data pins, RGB565):
	// used for their plain framebuffer (non-palette) path per
	// st7701.cpp's init(), which selects this program and only 16 data
	// pins whenever no palette is configured - the two extra data lines
	// (D16/D17) are simply tied low as plain GPIO outputs in that case.
	// Each loop iteration shifts out two pixels packed into one 32-bit
	// FIFO word; `mov pins, ::isr` reverses the full 32-bit ISR before
	// writing its low 16 bits to the data pins.
	dataProgram := [...]uint16{
		asm.Mov(pio.MovDestX, pio.MovSrcY).Side(1).Encode(),
		asm.WaitIRQ(true, false, 4).Side(1).Encode(),
		asm.Out(pio.OutDestISR, 32).Side(1).Encode(),
		asm.MovReverse(pio.MovDestPins, pio.MovSrcISR).Side(1).Delay(1).Encode(),
		asm.In(pio.InSrcNull, 16).Side(1).Delay(1).Encode(),
		asm.MovReverse(pio.MovDestPins, pio.MovSrcISR).Side(1).Delay(1).Encode(),
		asm.Jmp(pio.JmpXNZeroDec, 2).Side(1).Encode(),
		asm.Mov(pio.MovDestPins, pio.MovSrcNull).Side(1).Encode(),
	}
	dataOffset, err := p.AddProgram(dataProgram[:], -1)
	must("add data PIO program", err)
	dataCfg := asm.DefaultStateMachineConfig(dataOffset, dataProgram[:])
	dataCfg.SetOutPins(lcdD0, 16)
	dataCfg.SetSidesetPins(lcdDE)
	dataCfg.SetOutShift(true, true, 32)
	dataCfg.SetInShift(false, false, 32)
	dataCfg.SetFIFOJoin(pio.FifoJoinTx)
	setClkDiv(&dataCfg, dataHz)

	timingProgram := [...]uint16{
		asm.Out(pio.OutDestPins, 2).Side(0).Encode(),
		asm.Out(pio.OutDestX, 14).Side(1).Encode(),
		asm.Nop().Side(0).Encode(),
		asm.Jmp(pio.JmpXNZeroDec, 2).Side(1).Encode(),
		asm.Out(pio.OutDestExec, 16).Side(0).Encode(),
	}
	timingOffset, err := p.AddProgram(timingProgram[:], -1)
	must("add timing PIO program", err)
	timingCfg := asm.DefaultStateMachineConfig(timingOffset, timingProgram[:])
	timingCfg.SetOutPins(lcdHSync, 2)
	timingCfg.SetSidesetPins(lcdDotClk)
	timingCfg.SetOutShift(false, true, 32)
	timingCfg.SetFIFOJoin(pio.FifoJoinTx)
	setClkDiv(&timingCfg, timingHz)

	pinCfg := machine.PinConfig{Mode: p.PinMode()}
	for pin := lcdD0; pin <= machine.GPIO16; pin++ {
		pin.Configure(pinCfg)
	}
	for pin := lcdHSync; pin <= lcdDotClk; pin++ {
		pin.Configure(pinCfg)
	}
	// D16/D17 (the two extra RGB666 data lines) are unused in 16-pin
	// RGB565 mode; tie them low as plain GPIO outputs, matching
	// Pimoroni's non-palette init() path.
	for _, pin := range []machine.Pin{machine.GPIO17, machine.GPIO18} {
		pin.Configure(machine.PinConfig{Mode: machine.PinOutput})
		pin.Low()
	}

	dataSM.Init(dataOffset, dataCfg)
	dataSM.SetPinsMasked(0, pinMask(lcdD0, 16)|pinMask(lcdDE, 1))
	dataSM.SetPindirsMasked(pinMask(lcdD0, 16)|pinMask(lcdDE, 1), pinMask(lcdD0, 16)|pinMask(lcdDE, 1))
	dataSM.SetY(width/2 - 1)

	timingSM.Init(timingOffset, timingCfg)
	timingSM.SetPinsMasked(0, pinMask(lcdHSync, 4))
	timingSM.SetPindirsMasked(pinMask(lcdHSync, 4), pinMask(lcdHSync, 4))

	dataSM.SetEnabled(true)
	timingSM.SetEnabled(true)
}

// scanoutClocks reproduces the clock divider Pimoroni's driver derives at
// runtime: an integer divider taking the system clock down to at most
// maxTimingHz, rounded up to an even value so the data state machine can
// run at exactly twice the timing state machine's rate. At the stock
// 150 MHz system clock this gives a divider of 6, so the timing state
// machine runs at 25 MHz and the data state machine at 50 MHz.
//
// This matters more than it looks: the timing state machine toggles the
// dot clock with its side-set pin, so the panel's dot clock is half the
// timing rate, and the resulting line and frame rates must stay close to
// what the panel expects. Running the whole chain too slowly does not
// simply produce a dimmer or slower picture - the panel keeps scanning at
// its own rate and only the first fraction of the frame is ever drawn,
// vertically stretched over the full screen.
func scanoutClocks() (timingHz, dataHz uint32) {
	cpu := machine.CPUFrequency()
	div := (cpu + maxTimingHz - 1) / maxTimingHz
	if div&1 != 0 {
		div++
	}
	return cpu / div, cpu / (div / 2)
}

func setClkDiv(cfg *pio.StateMachineConfig, hz uint32) {
	whole, frac, err := pio.ClkDivFromFrequency(hz, machine.CPUFrequency())
	must("calculate PIO clock divider", err)
	cfg.SetClkDivIntFrac(whole, frac)
}

func scanFrame(timingSM, dataSM pio.StateMachine) {
	for row := 0; row < timingVFront; row++ {
		vsyncHigh := row >= timingVPulse
		active := row >= timingVBack && row < timingVDisplay
		sourceRow := row - timingVBack

		putTiming(timingSM, true, vsyncHigh, timingHFront, pioNop)
		putTiming(timingSM, false, vsyncHigh, timingHPulse, pioNop)

		if active {
			startLineDMA(dataSM, lines[lineKind(sourceRow)][:])
		}

		instr := uint16(pioNop)
		if active {
			instr = pioIRQ4
		}
		putTiming(timingSM, true, vsyncHigh, timingHBack, instr)
		putTiming(timingSM, true, vsyncHigh, timingHDisplay, pioNop)

		if active {
			waitLineDMA()
		}
	}
}

func putTiming(sm pio.StateMachine, hsync, vsync bool, pixelClocks uint16, instr uint16) {
	word := uint32(pixelClocks-3) << 16
	if hsync {
		word |= 1 << 30
	}
	if vsync {
		word |= 1 << 31
	}
	word |= uint32(instr)

	for sm.IsTxFIFOFull() {
	}
	sm.TxPut(word)
}

// pixelColor returns the RGB565 colour of the static test pattern at (x, y).
// It draws, from outermost to innermost: a white border spanning the full
// screen (checks dimensions/full-screen updates), four differently coloured
// corner squares (checks orientation and mirroring at a glance), a centre
// crosshair (checks the exact centre lands where expected), and a set of
// vertical colour bars in a well known order (checks colour channel wiring).
func pixelColor(x, y int) uint16 {
	if x < borderWidth || x >= width-borderWidth || y < borderWidth || y >= height-borderWidth {
		return white
	}
	switch {
	case y < borderWidth+cornerSize:
		if x < borderWidth+cornerSize {
			return red // top-left
		}
		if x >= width-borderWidth-cornerSize {
			return green // top-right
		}
	case y >= height-borderWidth-cornerSize:
		if x < borderWidth+cornerSize {
			return blue // bottom-left
		}
		if x >= width-borderWidth-cornerSize {
			return yellow // bottom-right
		}
	}
	const half = crossWidth / 2
	cx, cy := width/2, height/2
	if (x >= cx-half && x < cx+half) || (y >= cy-half && y < cy+half) {
		return white
	}
	return bars[x/(width/numBars)]
}

// expandScanline fills dst with the RGB565 pixels for row, packed two
// per 32-bit word ready for startLineDMA. Pimoroni's real firmware reads a
// plain uint16 RGB565 framebuffer directly into a DMA channel configured
// with a byte-swap (channel_config_set_bswap), and only then feeds the PIO,
// whose `mov pins, ::isr` reverses the full 32-bit ISR before writing its
// low 16 bits to the data pins. This example has no framebuffer or DMA
// byte-swap, so it reproduces the same net transform in software: pack the
// two raw pixel values exactly as they'd sit in memory, then apply the same
// byte-swap the hardware DMA would have performed.
func expandScanline(dst *[width / 2]uint32, row int) {
	for i := 0; i < width/2; i++ {
		c0 := reorderChannels(pixelColor(2*i, row))
		c1 := reorderChannels(pixelColor(2*i+1, row))
		dst[i] = bits.ReverseBytes32(uint32(c1)<<16 | uint32(c0))
	}
}

// buildLines renders one representative row for each distinct kind of
// scanline in the test pattern.
func buildLines() {
	expandScanline(&lines[lineWhite], 0)
	expandScanline(&lines[lineTopCorner], borderWidth)
	expandScanline(&lines[lineBottomCorner], height-borderWidth-1)
	expandScanline(&lines[lineBars], borderWidth+cornerSize)
}

// lineKind reports which prebuilt scanline row should be displayed with.
func lineKind(row int) int {
	const half = crossWidth / 2
	cy := height / 2
	switch {
	case row < borderWidth || row >= height-borderWidth:
		return lineWhite
	case row < borderWidth+cornerSize:
		return lineTopCorner
	case row >= height-borderWidth-cornerSize:
		return lineBottomCorner
	case row >= cy-half && row < cy+half:
		return lineWhite
	default:
		return lineBars
	}
}

// reorderChannels pre-permutes an RGB565 colour's bits so that, after
// passing through the existing byte-swap + full-word-bit-reverse pixel
// pipeline below, the correct bit lands on each physical D-pin of this
// Presto board.
//
// The 16 data pins are D0-D15 on GPIO1-GPIO16, and the panel consumes
// them as five bits of one 5-bit channel, six bits of green, then five
// bits of the other 5-bit channel. Which of the two 5-bit channels sits
// at which end is the one thing the schematic's net names do not settle:
// they label the low pins B7..B3 and the high pins R7..R3, but driving
// red onto the low group is what actually produces correct colour on the
// panel, so the bus is consumed in BGR order relative to those names.
// This was established on hardware - with the two groups the other way
// round every colour came out with red and blue exchanged (red bars blue,
// yellow bars cyan, and so on) while green, white, magenta and black,
// which are all invariant under a red/blue swap, looked correct.
//
// Working backwards through byte-swap-then-bit-reverse to find what
// value must be fed in to land each field on its correct pin, the
// required transform collapses to a fixed rearrangement of the RGB565
// fields within the 16-bit word: the 3 low bits of green move to the top
// of the word, the two 5-bit channels swap ends in the middle, and the 3
// high bits of green fill the bottom.
func reorderChannels(c uint16) uint16 {
	r5 := uint16((c >> 11) & 0x1f)
	g6 := uint16((c >> 5) & 0x3f)
	b5 := c & 0x1f
	return (g6&0x7)<<13 | b5<<8 | r5<<3 | (g6>>3)&0x7
}

func startLineDMA(sm pio.StateMachine, line []uint32) {
	waitLineDMA()
	rp.DMA.CH0_CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	rp.DMA.SetCH0_READ_ADDR(uint32(uintptr(unsafe.Pointer(&line[0]))))
	rp.DMA.SetCH0_WRITE_ADDR(uint32(uintptr(unsafe.Pointer(&sm.TxReg().Reg))))
	rp.DMA.SetCH0_TRANS_COUNT_COUNT(uint32(len(line)))
	rp.DMA.CH0_CTRL_TRIG.Set(dmaCtrl(dataDREQ(sm)))
}

func waitLineDMA() {
	for rp.DMA.CH0_CTRL_TRIG.Get()&rp.DMA_CH0_CTRL_TRIG_BUSY != 0 {
	}
	rp.DMA.CH0_CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
}

func dmaCtrl(dreq uint32) uint32 {
	const dmaSize32 = 2
	return rp.DMA_CH0_CTRL_TRIG_EN_Msk |
		rp.DMA_CH0_CTRL_TRIG_INCR_READ_Msk |
		(dmaSize32 << rp.DMA_CH0_CTRL_TRIG_DATA_SIZE_Pos) |
		(dreq << rp.DMA_CH0_CTRL_TRIG_TREQ_SEL_Pos)
}

func dataDREQ(sm pio.StateMachine) uint32 {
	return uint32(sm.PIO().BlockIndex())*8 + uint32(sm.StateMachineIndex())
}

func pinMask(base machine.Pin, count uint8) uint32 {
	var mask uint32
	for i := uint8(0); i < count; i++ {
		mask |= 1 << uint(base+machine.Pin(i))
	}
	return mask
}

func must(action string, err error) {
	if err != nil {
		println(action + ": " + err.Error())
		for {
		}
	}
}
