//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

func main() {
	// Give the USB CDC serial monitor time to attach after reset so the
	// boot trace below isn't lost.
	time.Sleep(2 * time.Second)
	println("boot: presto-daliclock starting")

	epochSeconds := bootSeconds
	println("boot: attempting NTP sync")
	if synced, err := syncClockFromNTP(); err == nil {
		epochSeconds = synced.Hour()*3600 + synced.Minute()*60 + synced.Second()
		println("ntp: synced, starting clock at", synced.String())
	} else {
		println("ntp: sync failed:", err.Error())
	}
	// start is captured from the device's own (uncorrected) clock right
	// before the render loop begins, so that time.Since(start) inside the
	// renderer is always a small, correct, monotonically-increasing
	// duration -- regardless of whether epochSeconds came from NTP or the
	// fixed 09:41:00 default.
	start := time.Now()
	println("boot: starting renderer")
	presto.Run(newRenderer(start, epochSeconds))
}
