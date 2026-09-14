//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

func main() {
	start := time.Now()
	if synced, err := syncClockFromNTP(); err == nil {
		start = synced
		println("ntp: synced to", start.String())
	} else {
		println("ntp: sync failed:", err.Error())
	}
	presto.Run(newRenderer(start))
}
