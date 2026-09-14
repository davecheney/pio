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

	start := time.Now()
	println("boot: attempting NTP sync")
	if synced, err := syncClockFromNTP(); err == nil {
		start = synced
		println("ntp: synced to", start.String())
	} else {
		println("ntp: sync failed:", err.Error())
	}
	println("boot: starting renderer")
	presto.Run(newRenderer(start))
}
