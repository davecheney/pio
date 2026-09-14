//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

type renderer struct {
	start       time.Time
	fromDigits  [clockGlyphs]int
	toDigits    [clockGlyphs]int
	msec        int
	foreground  uint32
	background  uint32
	currentTime clockTime
}

func newRenderer(start time.Time) *renderer {
	return &renderer{start: start}
}

func (r *renderer) BeginFrame(frame uint32) {
	elapsed := time.Since(r.start)
	r.currentTime = timeFromElapsed(elapsed)
	r.fromDigits = clockDigits(r.currentTime)
	r.toDigits = clockDigits(nextSecond(r.currentTime))
	r.msec = morphMillis(elapsed)
	r.foreground, r.background = colorWords(elapsed)
}

func (r *renderer) RenderLine(dst *presto.Line, row int) {
	renderClockLine(dst[:], row, r.fromDigits, r.toDigits, r.msec, r.foreground, r.background)
}
