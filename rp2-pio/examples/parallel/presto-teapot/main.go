//go:build rp2350 && rp2350b

package main

import (
	"machine"
	"sync/atomic"
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

const (
	// frameLimit 0 runs until a fault. Set non-zero only for bounded debug
	// runs; RunIndexed then stops, reports and halts after that many frames.
	frameLimit = 0

	// bootTrace enables the B01–B13 boot breadcrumbs and the pre-scanout
	// renderer self-test. It is off so the demo boots silently and needs no
	// USB host; fault reports from RunIndexed are always printed.
	bootTrace = false

	// Maximum busy-wait for the renderer goroutine to report its core.
	renderWaitUS = 3_000_000
)

var (
	frame   presto.IndexedFrame
	palette presto.Palette
	model   teapot

	// SRAM copies of the flash mesh, so the renderer does not churn the XIP
	// cache that the scanline core shares.
	ramVerts [len(teapotVerts)][3]int16
	ramEdges [len(teapotEdges)][2]uint16

	renderCore    atomic.Int32
	renderStarted atomic.Bool
	renderFrames  atomic.Uint32
	renderMissed  atomic.Uint32 // display frames with no update (renderer overran)
	stepMaxUS     atomic.Uint32 // worst draw-new+erase-old update, from a boundary
)

func main() {
	crumb("B01 main.main reached", 0)
	crumb("B02 main core", uint32(machine.CurrentCore()))
	crumb("B03 CPU MHz", machine.CPUFrequency()/1_000_000)
	crumb("B04 verts", uint32(len(teapotVerts)))
	crumb("B05 edges", uint32(len(teapotEdges)))
	crumb("B06 frame limit", frameLimit)

	palette[colorBackground] = presto.PackPixels(0x0841)
	palette[colorLineA] = presto.PackPixels(lineRGB565)
	palette[colorLineB] = presto.PackPixels(lineRGB565)
	palette[colorBorder] = presto.PackPixels(0xfd20)
	drawBorder(&frame)
	ramVerts, ramEdges = teapotVerts, teapotEdges
	meshVerts, meshEdges = &ramVerts, &ramEdges
	r := rotationAt(0)
	crumb("B07 palette and border ready; first vertex x", uint32(projectVertex(&teapotVerts[0], &r).x))
	crumb("B07a warmup frames before renderer", warmupFrames)
	if bootTrace {
		selfTest()
	}

	presto.StopReport = func() {
		println("teapot: renderer core", renderCore.Load(), "frames drawn", renderFrames.Load(),
			"display frames skipped", renderMissed.Load(), "scanned", presto.FrameCount.Load(),
			"step max us", stepMaxUS.Load())
	}

	crumb("B08 starting renderer goroutine", 0)
	go render()
	// Busy-wait (no Sleep) so this does not depend on the scheduler waking
	// main while the renderer owns the other core.
	t0 := micros()
	for !renderStarted.Load() && micros()-t0 < renderWaitUS {
	}
	if renderStarted.Load() {
		crumb("B09 renderer started after us", micros()-t0)
	} else {
		crumb("B09 RENDERER NOT STARTED after us (continuing)", micros()-t0)
	}
	crumb("B10 scanout core", uint32(machine.CurrentCore()))
	crumb("B11 renderer core", uint32(renderCore.Load()))
	crumb("B12 renderer frames so far (expect 0: warmup gate)", renderFrames.Load())
	crumb("B13 calling RunIndexed", 0)
	presto.RunIndexed(&frame, &palette, frameLimit)
}

// render performs at most one framebuffer update per display frame, starting
// as soon as RunIndexed reports a new frame boundary (start of vertical
// blank). Each update draws the new wireframe first and then erases only the
// old wireframe's pixels not reclaimed by the new one, so there is never an
// erased intermediate teapot. It spins rather than sleeping or yielding, so
// under -scheduler=cores it keeps its core while main.main pumps scanlines on
// the other. Framebuffer access is unsynchronized; an update running past
// vertical blank can briefly show old and new edges together. Drawing starts
// only after warmupFrames static frames.
func render() {
	renderCore.Store(int32(machine.CurrentCore()))
	renderStarted.Store(true)
	last := waitWarmup(&presto.FrameCount)
	first := last
	for {
		t0 := micros()
		model.step(&frame, uint64(last-first)*frameUS)
		storeMax(&stepMaxUS, micros()-t0)
		renderFrames.Add(1)
		next := presto.WaitFrame(&presto.FrameCount, last)
		renderMissed.Add(next - last - 1)
		last = next
	}
}

func storeMax(v *atomic.Uint32, x uint32) {
	if x > v.Load() {
		v.Store(x)
	}
}

// selfTest times one first update and one steady-state update on this core
// before scanout, then erases and checks that only background and border
// remain.
func selfTest() {
	t0 := micros()
	model.step(&frame, 0)
	t1 := micros()
	model.step(&frame, frameUS)
	t2 := micros()
	crumb("B07b self-test first step us", t1-t0)
	crumb("B07c self-test steady step us (draw+erase)", t2-t1)
	model.clear(&frame)
	lit := uint32(0)
	for i := range frame {
		if c := frame[i]; c != colorBackground && c != colorBorder {
			lit++
		}
	}
	crumb("B07d self-test lit pixels after erase (expect 0)", lit)
}

func crumb(msg string, v uint32) {
	if bootTrace {
		println("teapot:", msg, v)
	}
}

func micros() uint32 {
	return uint32(time.Now().UnixMicro())
}
