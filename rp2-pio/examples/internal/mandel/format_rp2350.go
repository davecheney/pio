//go:build rp2350

package mandel

// The RP2350's core multiplies 32 bits by 32 into 64 in one instruction, so it
// uses the wide format. Sixteen more fractional bits put the zoom limit far
// beyond anything worth waiting for, so the depth is chosen rather than
// inherited, and the iteration limit is raised to resolve detail at it.
const (
	q       = qWide
	maxZoom = 4000
)

func escapeCount(cr, ci int32, maxIter int) int { return countWide(cr, ci, maxIter) }
