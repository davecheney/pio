//go:build rp2350 && rp2350b

package main

import (
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

func main() {
	start := time.Now()
	presto.Run(newRenderer(start))
}
