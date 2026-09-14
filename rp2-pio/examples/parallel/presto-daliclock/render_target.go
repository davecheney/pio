//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

type renderer struct {
	start        time.Time
	epochSeconds int
	fromDigits   [clockGlyphs]int
	toDigits     [clockGlyphs]int
	msec         int
	foreground   uint32
	background   uint32
	currentTime  clockTime
}

// newRenderer displays epochSeconds (a second-of-day) at the moment start
// was captured, then free-runs forward using time.Since(start). start and
// epochSeconds must be measured/derived consistently: start should come
// from the device's own time.Now() (its monotonic reading is always
// self-consistent), never from an NTP-corrected absolute time.Time, since
// the device's wall clock is not itself corrected and comparing it against
// a corrected time.Time via time.Since would yield a huge bogus duration.
func newRenderer(start time.Time, epochSeconds int) *renderer {
	return &renderer{start: start, epochSeconds: epochSeconds}
}

func (r *renderer) BeginFrame(frame uint32) {
	elapsed := time.Since(r.start)
	r.currentTime = timeFromEpochAndElapsed(r.epochSeconds, elapsed)
	r.fromDigits = clockDigits(r.currentTime)
	r.toDigits = clockDigits(nextSecond(r.currentTime))
	r.msec = morphMillis(elapsed)
	r.foreground, r.background = colorWords(elapsed)
}

func (r *renderer) RenderLine(dst *presto.Line, row int) {
	renderClockLine(dst[:], row, r.fromDigits, r.toDigits, r.msec, r.foreground, r.background)
}
