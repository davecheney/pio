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

	logicalWidth  = width / 2
	logicalHeight = height / 2

	lcdD0     = machine.GPIO1
	lcdHSync  = machine.GPIO19
	lcdVSync  = machine.GPIO20
	lcdDE     = machine.GPIO21
	lcdDotClk = machine.GPIO22

	lcdSCK       = machine.GPIO26
	lcdDAT       = machine.GPIO27
	lcdCS        = machine.GPIO28
	lcdBacklight = machine.GPIO45

	timingPIOHz = 8_000_000
	dataPIOHz   = timingPIOHz * 2
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

var (
	framebuffer [logicalWidth * logicalHeight / 8]byte
	scanline    [width / 2]uint32
	palette     = [...]uint16{black, white, red, green, blue, yellow, cyan, magenta}
)

func main() {
	time.Sleep(2 * time.Second)

	initControlPins()
	initDisplay()

	p := pio.PIO1
	dataSM, err := p.ClaimStateMachine()
	must("claim data state machine", err)
	timingSM, err := p.ClaimStateMachine()
	must("claim timing state machine", err)

	initScanoutPIO(p, dataSM, timingSM)
	setBacklight(true)

	var frame uint32
	for {
		drawPattern(frame)
		scanFrame(timingSM, dataSM, frame)
		frame++
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
	command(cmdCOLMOD, 0x55)
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
	asm := pio.AssemblerV0{SidesetBits: 1}

	dataProgram := [...]uint16{
		asm.Mov(pio.MovDestX, pio.MovSrcY).Side(1).Encode(),
		asm.WaitIRQ(true, false, 4).Side(1).Encode(),
		asm.Out(pio.OutDestISR, 32).Side(1).Encode(),
		asm.MovReverse(pio.MovDestPins, pio.MovSrcISR).Side(1).Delay(1).Encode(),
		asm.In(pio.InSrcNull, 16).Side(1).Delay(1).Encode(),
		asm.MovReverse(pio.MovDestPins, pio.MovSrcISR).Side(1).Delay(1).Encode(),
		asm.Jmp(pio.JmpXNZeroDec, 2).Side(1).Encode(),
		asm.Mov(pio.MovDestPins, pio.MovSrcNull).Side(0).Encode(),
	}
	dataOffset, err := p.AddProgram(dataProgram[:], -1)
	must("add data PIO program", err)
	dataCfg := asm.DefaultStateMachineConfig(dataOffset, dataProgram[:])
	dataCfg.SetOutPins(lcdD0, 16)
	dataCfg.SetSidesetPins(lcdDE)
	dataCfg.SetOutShift(true, true, 32)
	dataCfg.SetInShift(false, false, 32)
	dataCfg.SetFIFOJoin(pio.FifoJoinTx)
	setClkDiv(&dataCfg, dataPIOHz)

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
	setClkDiv(&timingCfg, timingPIOHz)

	pinCfg := machine.PinConfig{Mode: p.PinMode()}
	for pin := lcdD0; pin <= machine.GPIO18; pin++ {
		pin.Configure(pinCfg)
	}
	for pin := lcdHSync; pin <= lcdDotClk; pin++ {
		pin.Configure(pinCfg)
	}

	dataSM.Init(dataOffset, dataCfg)
	dataSM.SetPinsMasked(0, pinMask(lcdD0, 18)|pinMask(lcdDE, 1))
	dataSM.SetPindirsMasked(pinMask(lcdD0, 18)|pinMask(lcdDE, 1), pinMask(lcdD0, 18)|pinMask(lcdDE, 1))
	dataSM.SetY(width/2 - 1)

	timingSM.Init(timingOffset, timingCfg)
	timingSM.SetPinsMasked(0, pinMask(lcdHSync, 4))
	timingSM.SetPindirsMasked(pinMask(lcdHSync, 4), pinMask(lcdHSync, 4))

	dataSM.SetEnabled(true)
	timingSM.SetEnabled(true)
}

func setClkDiv(cfg *pio.StateMachineConfig, hz uint32) {
	whole, frac, err := pio.ClkDivFromFrequency(hz, machine.CPUFrequency())
	must("calculate PIO clock divider", err)
	cfg.SetClkDivIntFrac(whole, frac)
}

func scanFrame(timingSM, dataSM pio.StateMachine, frame uint32) {
	for row := 0; row < timingVFront; row++ {
		active := row >= timingVBack && row < timingVDisplay
		sourceRow := (row - timingVBack) / 2

		putTiming(timingSM, true, true, timingHFront, pioNop)
		putTiming(timingSM, false, true, timingHPulse, pioNop)

		if active {
			expandScanline(sourceRow, frame)
			startLineDMA(dataSM, scanline[:])
		}

		instr := uint16(pioNop)
		if active {
			instr = pioIRQ4
		}
		putTiming(timingSM, true, true, timingHBack, instr)
		putTiming(timingSM, true, true, timingHDisplay, pioNop)

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

func drawPattern(frame uint32) {
	for y := 0; y < logicalHeight; y++ {
		for x := 0; x < logicalWidth; x++ {
			zone := x / (logicalWidth / 8)
			checker := (((x+int(frame))>>4)+(y>>4))&1 == 0
			on := checker || x == y || x == logicalWidth-1-y ||
				x == int(frame)%logicalWidth || y == int(frame/2)%logicalHeight
			setPixel(x, y, on && zone != 0)
		}
	}
}

func setPixel(x, y int, on bool) {
	i := y*logicalWidth + x
	mask := byte(1 << uint(7-(i&7)))
	if on {
		framebuffer[i>>3] |= mask
	} else {
		framebuffer[i>>3] &^= mask
	}
}

func expandScanline(sourceRow int, frame uint32) {
	for x := 0; x < logicalWidth; x++ {
		i := sourceRow*logicalWidth + x
		on := framebuffer[i>>3]&(1<<uint(7-(i&7))) != 0
		c := uint16(black)
		if on {
			c = palette[(x/(logicalWidth/8)+int(frame/16))&7]
		}
		encoded := uint32(bits.Reverse16(c))
		scanline[x] = encoded<<16 | encoded
	}
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
