//go:build !rp2040 && !rp2350

package main

// Tests run on the host, where they measure the wide format the RP2350 uses.
// The narrow one is covered by comparing the two directly, which needs no
// target.
const (
	q       = qWide
	maxIter = 384
	maxZoom = 4000
)

func escapeCount(cr, ci int32, maxIter int) int { return countWide(cr, ci, maxIter) }
