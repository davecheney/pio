//go:build rp2350 && rp2350b

package presto

import (
	"device/rp"
	"machine"
	"time"
	"unsafe"

	pio "github.com/tinygo-org/pio/rp2-pio"
)

// StopReport, if set, is called once after scanout has stopped and the
// diagnostic line has been printed. Serial output is safe at that point.
var StopReport func()

// usbQuietUS is the busy-wait between the final startup print and priming.
const usbQuietUS = 200_000

var (
	timingBuf    [timingVFront * 4]uint32
	timingStart  uint32
	indexedStats scanoutStats
)

type scanoutStats struct {
	frame, row             uint32
	rows, expandUS         uint64
	maxExpandUS            uint32
	minFrameUS, maxFrameUS uint32
}

// RunIndexed scans out one read-only indexed frame from internal SRAM,
// expanding each row through palette into the two SRAM line buffers.
// It reserves DMA 0 (pixels), 1 (timing) and 2 (timing reload).
// frameLimit == 0 runs indefinitely; otherwise it stops after frameLimit
// frames, disables DMA/PIO/backlight, prints measurements once and halts.
// Faults stop the same way with the fault reason.
func RunIndexed(frame *IndexedFrame, palette *Palette, frameLimit uint32) {
	stage("RunIndexed entered")
	initControlPins()
	if machine.CPUFrequency() != 150_000_000 {
		fatal("presto: indexed prototype requires 150 MHz CPU")
	}
	if frame == nil || palette == nil {
		fatal("presto: nil indexed frame or palette")
	}
	checkSRAM(unsafe.Pointer(frame), unsafe.Sizeof(*frame))
	checkSRAM(unsafe.Pointer(&lineBuf[0]), unsafe.Sizeof(lineBuf))
	checkSRAM(unsafe.Pointer(&timingBuf[0]), unsafe.Sizeof(timingBuf))
	checkSRAM(unsafe.Pointer(&timingStart), unsafe.Sizeof(timingStart))
	checkSRAM(unsafe.Pointer(palette), unsafe.Sizeof(*palette))
	const used = rp.DMA_CH0_CTRL_TRIG_EN_Msk | rp.DMA_CH0_CTRL_TRIG_BUSY
	if (rp.DMA.CH0_CTRL_TRIG.Get()|rp.DMA.CH1_CTRL_TRIG.Get()|rp.DMA.CH2_CTRL_TRIG.Get())&used != 0 {
		fatal("presto: indexed DMA channels already in use")
	}
	stage("preconditions ok; initDisplay")
	initDisplay()
	stage("display initialized; configuring PIO")
	p := pio.PIO1
	dataSM, err := p.ClaimStateMachine()
	must("claim indexed data state machine", err)
	timingSM, err := p.ClaimStateMachine()
	must("claim indexed timing state machine", err)
	configureScanoutPIO(p, dataSM, timingSM)
	indexedTiming(&timingBuf)
	indexedStats = scanoutStats{}

	// This is the last print before the stop report. USB CDC transmits
	// queued output from the USBCTRL interrupt on this core; let it drain
	// before scanout, where a line has only ~5 us of FIFO+HBLANK slack.
	stage("priming FIFOs; starting scanout after USB quiet period (serial silent until stop)")
	for t := micros(); micros()-t < usbQuietUS; {
	}

	frame.Expand(&lineBuf[0], 0, palette)
	startLineDMA(dataSM, lineBuf[0][:])
	startTimingDMA(timingSM)
	start := micros()
	for !dataSM.IsTxFIFOFull() || !timingSM.IsTxFIFOFull() {
		checkIndexed(p, dataSM, timingSM, start)
	}
	p.HW().IRQ.Set(1<<0 | 1<<4)
	dataSM.ClearTxStalled()
	timingSM.ClearTxStalled()
	dataSM.SetEnabled(true)
	timingSM.SetEnabled(true)
	setBacklight(true)

	lastBoundary := uint32(0)
	for n := uint32(0); ; n++ {
		indexedStats.frame = n
		for row := 0; row < Height; row++ {
			indexedStats.row = uint32(row)
			workStart := micros()
			if row+1 < Height {
				frame.Expand(&lineBuf[(row+1)&1], row+1, palette)
				elapsed := micros() - workStart
				indexedStats.rows++
				indexedStats.expandUS += uint64(elapsed)
				indexedStats.maxExpandUS = max(indexedStats.maxExpandUS, elapsed)
			}
			for rp.DMA.CH0_CTRL_TRIG.Get()&rp.DMA_CH0_CTRL_TRIG_BUSY != 0 {
				checkIndexed(p, dataSM, timingSM, workStart)
			}
			checkIndexed(p, dataSM, timingSM, workStart)
			if row+1 < Height {
				startLineDMA(dataSM, lineBuf[(row+1)&1][:])
			}
		}
		// DMA completion means "in FIFO", not "on panel". IRQ0 is executed
		// by the timing SM only after the final active line has ended.
		start = micros()
		for p.HW().IRQ.Get()&1 == 0 {
			checkIndexed(p, dataSM, timingSM, start)
		}
		checkIndexed(p, dataSM, timingSM, start)
		p.HW().IRQ.Set(1)
		now := micros()
		if n != 0 {
			period := now - lastBoundary
			if indexedStats.minFrameUS == 0 || period < indexedStats.minFrameUS {
				indexedStats.minFrameUS = period
			}
			indexedStats.maxFrameUS = max(indexedStats.maxFrameUS, period)
		}
		lastBoundary = now
		FrameCount.Store(n + 1)
		if frameLimit != 0 && n+1 == frameLimit {
			stopIndexed(p, dataSM, timingSM, "complete")
		}
		frame.Expand(&lineBuf[0], 0, palette)
		startLineDMA(dataSM, lineBuf[0][:])
	}
}

// stage prints a startup marker. Only call before scanout is enabled.
func stage(msg string) {
	println("indexed: t_us", micros(), msg)
}

// fatal is for pre-scanout configuration errors. The display is not being
// driven, so repeating the message is safe and helps a late-attached monitor.
func fatal(msg string) {
	setBacklight(false)
	for {
		println("indexed: FATAL", msg)
		time.Sleep(time.Second)
	}
}

func startTimingDMA(sm pio.StateMachine) {
	timingStart = uint32(uintptr(unsafe.Pointer(&timingBuf[0])))
	rp.DMA.CH1_READ_ADDR.Set(timingStart)
	rp.DMA.CH1_WRITE_ADDR.Set(uint32(uintptr(unsafe.Pointer(&sm.TxReg().Reg))))
	rp.DMA.CH1_TRANS_COUNT.Set(uint32(len(timingBuf)))

	// A completed channel reloads its programmed count when retriggered.
	// CH2 has fixed source/destination and no chain (CHAIN_TO == itself).
	rp.DMA.CH2_READ_ADDR.Set(uint32(uintptr(unsafe.Pointer(&timingStart))))
	rp.DMA.CH2_WRITE_ADDR.Set(uint32(uintptr(unsafe.Pointer(&rp.DMA.CH1_AL3_READ_ADDR_TRIG.Reg))))
	rp.DMA.CH2_TRANS_COUNT.Set(1)
	const priority = rp.DMA_CH0_CTRL_TRIG_HIGH_PRIORITY_Msk
	rp.DMA.CH2_AL1_CTRL.Set((dmaCtrl(0x3f) &^ rp.DMA_CH0_CTRL_TRIG_INCR_READ_Msk) |
		2<<rp.DMA_CH0_CTRL_TRIG_CHAIN_TO_Pos | priority)
	rp.DMA.CH1_CTRL_TRIG.Set(dmaCtrl(dataDREQ(sm)) |
		2<<rp.DMA_CH0_CTRL_TRIG_CHAIN_TO_Pos | priority)
}

func micros() uint32 {
	return rp.TIMER0.TIMERAWL.Get()
}

func checkIndexed(p *pio.PIO, dataSM, timingSM pio.StateMachine, since uint32) {
	if dataSM.HasTxStalled() || timingSM.HasTxStalled() {
		stopIndexed(p, dataSM, timingSM, "TXSTALL")
	}
	if (rp.DMA.CH0_CTRL_TRIG.Get()|rp.DMA.CH1_CTRL_TRIG.Get()|rp.DMA.CH2_CTRL_TRIG.Get())&rp.DMA_CH0_CTRL_TRIG_AHB_ERROR != 0 {
		stopIndexed(p, dataSM, timingSM, "DMA bus error")
	}
	if micros()-since > 25_000 {
		stopIndexed(p, dataSM, timingSM, "handshake timeout")
	}
}

func stopIndexed(p *pio.PIO, dataSM, timingSM pio.StateMachine, reason string) {
	debug, irq := p.HW().FDEBUG.Get(), p.HW().IRQ.Get()
	stallMask := uint32(1<<dataSM.StateMachineIndex()|1<<timingSM.StateMachineIndex()) << rp.PIO0_FDEBUG_TXSTALL_Pos
	if debug&stallMask != 0 {
		reason = "TXSTALL"
	}
	dataPC, timingPC := dataSM.HW().ADDR.Get(), timingSM.HW().ADDR.Get()
	dataCtrl, timingCtrl, reloadCtrl := rp.DMA.CH0_CTRL_TRIG.Get(), rp.DMA.CH1_CTRL_TRIG.Get(), rp.DMA.CH2_CTRL_TRIG.Get()
	timingRead := rp.DMA.CH1_READ_ADDR.Get()
	setBacklight(false)
	dataSM.SetEnabled(false)
	timingSM.SetEnabled(false)
	rp.DMA.CH0_CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	rp.DMA.CH1_CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	rp.DMA.CH2_CTRL_TRIG.ClearBits(rp.DMA_CH0_CTRL_TRIG_EN_Msk)
	rp.DMA.CHAN_ABORT.Set(7)
	start := micros()
	for rp.DMA.CHAN_ABORT.Get()&7 != 0 && micros()-start < 1000 {
	}
	println("indexed:", reason, "frame", indexedStats.frame, "row", indexedStats.row,
		"FDEBUG", debug, "IRQ", irq, "PC data/timing", dataPC, timingPC,
		"DMA ctrl data/timing/reload", dataCtrl, timingCtrl, reloadCtrl,
		"timing read", timingRead, "abort pending", rp.DMA.CHAN_ABORT.Get()&7,
		"rows", indexedStats.rows, "expand total us", indexedStats.expandUS,
		"expand max us", indexedStats.maxExpandUS,
		"frame min/max us", indexedStats.minFrameUS, indexedStats.maxFrameUS)
	if StopReport != nil {
		StopReport()
	}
	for {
		time.Sleep(time.Second)
	}
}

func checkSRAM(ptr unsafe.Pointer, size uintptr) {
	start := uintptr(ptr)
	if start < 0x20000000 || start+size > 0x20082000 {
		fatal("presto: indexed storage is not in internal SRAM")
	}
}
