//go:build rp2040

package main

// The RP2040's core has no wide multiply, so it uses the narrow format and the
// shallow zoom that goes with it, and the iteration limit that suits that depth.
const (
	q       = qNarrow
	maxIter = 96
	// maxZoom never binds here: the format runs out of precision first.
	maxZoom = 1 << 20
)

func escapeCount(cr, ci int32, maxIter int) int { return countNarrow(cr, ci, maxIter) }
