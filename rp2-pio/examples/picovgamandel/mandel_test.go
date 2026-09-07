package main

import (
	"math"
	"testing"

	"github.com/tinygo-org/pio/rp2-pio/picovga"
)

// referenceCount iterates the set in floating point, which the target cannot
// afford but a test can, to check the fixed point arithmetic against.
func referenceCount(cr, ci float64, maxIter int) int {
	var zr, zi float64
	for i := 0; i < maxIter; i++ {
		zr2, zi2 := zr*zr, zi*zi
		if zr2+zi2 > 4 {
			return i
		}
		zr, zi = zr2-zi2+cr, 2*zr*zi+ci
	}
	return maxIter
}

func toFixed(v float64) int32 { return int32(math.Round(v * one)) }

// TestKnownPoints checks points whose membership is not in doubt.
func TestKnownPoints(t *testing.T) {
	const maxIter = 64
	for _, tc := range []struct {
		name   string
		cr, ci float64
		inSet  bool
	}{
		{"origin", 0, 0, true},
		{"minus one", -1, 0, true},
		{"cusp interior", -0.5, 0, true},
		{"main bulb", -0.1, 0.7, true},
		{"far right", 2, 0, false},
		{"far up", 0, 2, false},
		{"outside left", -2.5, 0, false},
	} {
		got := escapeCount(toFixed(tc.cr), toFixed(tc.ci), maxIter)
		if inSet := got >= maxIter; inSet != tc.inSet {
			t.Errorf("%s (%.2f,%.2f): escaped at %d, inSet=%v want %v",
				tc.name, tc.cr, tc.ci, got, inSet, tc.inSet)
		}
	}
}

// TestAgainstFloat checks the fixed point iteration against a floating point
// one over a grid covering the whole view.
//
// The reference is given the same rounded c that the fixed point code works
// from, so what is compared is the arithmetic rather than the quantisation of
// the view. Points that escape quickly are well conditioned and must agree
// closely. Near the set's boundary the iteration is chaotic, and twelve
// fractional bits of rounding will change an escape count there however the
// arithmetic is done, so those are counted rather than asserted individually.
// On screen they amount to a handful of boundary pixels landing in the
// neighbouring colour band.
func TestAgainstFloat(t *testing.T) {
	const (
		maxIter = 64
		steps   = 60
		settled = 12 // counts below this come from points well clear of the edge
	)
	total, diverged, worst := 0, 0, 0
	for iy := 0; iy <= steps; iy++ {
		for ix := 0; ix <= steps; ix++ {
			cr := toFixed(-2.2 + 3.0*float64(ix)/steps)
			ci := toFixed(-1.125 + 2.25*float64(iy)/steps)
			got := escapeCount(cr, ci, maxIter)
			want := referenceCount(float64(cr)/one, float64(ci)/one, maxIter)
			total++
			d := got - want
			if d < 0 {
				d = -d
			}
			if d > worst {
				worst = d
			}
			if want < settled {
				if d > 1 {
					t.Errorf("(%.4f,%.4f) escapes at %d: fixed point says %d",
						float64(cr)/one, float64(ci)/one, want, got)
				}
				continue
			}
			if d > 3 {
				diverged++
			}
		}
	}
	if diverged*100 > total*3 {
		t.Errorf("%d of %d points diverge by more than three iterations, want under 3%%",
			diverged, total)
	}
	t.Logf("%d of %d points diverge near the boundary, worst difference %d",
		diverged, total, worst)
}

// TestNarrowNoOverflow reruns the narrow iteration with 64 bit products and
// requires exact agreement. A disagreement would mean a product had overflowed
// an int32, which is the risk taken by choosing a format small enough to avoid
// wide multiplies on the RP2040.
func TestNarrowNoOverflow(t *testing.T) {
	const iters = 64
	wide := func(cr, ci int32) int {
		var zr, zi int64
		c1, c2 := int64(cr), int64(ci)
		for n := 0; n < iters; n++ {
			zr2, zi2 := (zr*zr)>>qNarrow, (zi*zi)>>qNarrow
			if zr2+zi2 > escapeNarrow {
				return n
			}
			zr, zi = zr2-zi2+c1, ((zr*zi)>>(qNarrow-1))+c2
		}
		return iters
	}
	const steps = 80
	for iy := 0; iy <= steps; iy++ {
		for ix := 0; ix <= steps; ix++ {
			cr := int32((-2.5 + 3.6*float64(ix)/steps) * (1 << qNarrow))
			ci := int32((-1.4 + 2.8*float64(iy)/steps) * (1 << qNarrow))
			if got, want := countNarrow(cr, ci, iters), wide(cr, ci); got != want {
				t.Fatalf("(%d,%d): 32 bit gives %d, 64 bit gives %d, a product overflowed",
					cr, ci, got, want)
			}
		}
	}
}

// TestWideNoOverflow does the same for the wide format, where the squares
// outgrow an int32 as soon as a point escapes and are held in an int64 for that
// reason. Truncating them would show up here.
func TestWideNoOverflow(t *testing.T) {
	const iters = 64
	for _, c := range [][2]float64{{0, 0}, {-1, 0}, {2, 0}, {0.3, 0.5}, {-1.9, 0.1}, {-0.75, 0.11}} {
		cr, ci := int32(c[0]*(1<<qWide)), int32(c[1]*(1<<qWide))
		var zr, zi int32
		for n := 0; n < iters; n++ {
			nzr, nzi, escaped := stepWide(zr, zi, cr, ci)
			if escaped {
				break
			}
			// A point still iterating had not escaped when this step began, so
			// |z| was within two and the square within four; adding c can carry
			// a component to about six and a half. Seven is the bound, and at
			// this scale that is 1.9e9, inside an int32 with room to spare.
			if nzr > 7<<qWide || nzr < -7<<qWide || nzi > 7<<qWide || nzi < -7<<qWide {
				t.Fatalf("(%v): component grew to (%d,%d) at step %d", c, nzr, nzi, n)
			}
			zr, zi = nzr, nzi
		}
	}
}

// TestNarrowAgainstFloat checks the narrow format the RP2040 uses against a
// floating point reference, the wide one being covered by TestAgainstFloat.
//
// The two formats are not compared with each other: they round c itself to
// different grids, 1/4096 against 1/2^28, so they iterate slightly different
// points and need not agree even where both are well conditioned.
func TestNarrowAgainstFloat(t *testing.T) {
	const (
		iters   = 64
		steps   = 60
		settled = 12
	)
	total, diverged := 0, 0
	for iy := 0; iy <= steps; iy++ {
		for ix := 0; ix <= steps; ix++ {
			cr := int32(math.Round((-2.2 + 3.0*float64(ix)/steps) * (1 << qNarrow)))
			ci := int32(math.Round((-1.125 + 2.25*float64(iy)/steps) * (1 << qNarrow)))
			got := countNarrow(cr, ci, iters)
			want := referenceCount(float64(cr)/(1<<qNarrow), float64(ci)/(1<<qNarrow), iters)
			total++
			d := got - want
			if d < 0 {
				d = -d
			}
			if want < settled {
				if d > 1 {
					t.Errorf("(%.4f,%.4f) escapes at %d: narrow says %d",
						float64(cr)/(1<<qNarrow), float64(ci)/(1<<qNarrow), want, got)
				}
				continue
			}
			if d > 3 {
				diverged++
			}
		}
	}
	if diverged*100 > total*3 {
		t.Errorf("%d of %d points diverge by more than three iterations", diverged, total)
	}
	t.Logf("narrow: %d of %d points diverge near the boundary", diverged, total)
}

// finalView returns the last and closest view of the cycle heading for
// destination i, which is the one most likely to have closed in on solid black.
func finalView(t *testing.T, fb *picovga.Framebuffer16, i int) *renderer {
	t.Helper()
	r := newRenderer(fb)
	r.dest = i
	cx, cy, span, step := r.cx, r.cy, r.xSpan, r.step
	for n := 0; ; n++ {
		was := r.dest
		cx, cy, span, step = r.cx, r.cy, r.xSpan, r.step
		r.zoom()
		if r.dest != was {
			break
		}
		if n > 1000 {
			t.Fatalf("destination %d: cycle never reached its zoom limit", i)
		}
	}
	// Ending the cycle sent the renderer home, which reset the zoom count. The
	// iteration limit is derived from that count, so restoring the view without
	// it would draw the closest picture at the opening picture's limit.
	r.dest = i
	r.setView(cx, cy, span)
	r.step = step
	return r
}

// TestDestinationsLookInteresting renders the closest view of every cycle and
// checks there is still something to see in it.
//
// A destination can sit right on the set's edge in the opening picture and
// still finish wholly inside it, every pixel reaching the iteration limit and
// the screen going black. That is what the famous valleys and junctions do
// here: their detail lies far deeper than this fixed point format reaches.
//
// The limit used is the one the demo renders at, which matters: an earlier
// version of this test sampled at a higher limit, counted points that escape
// late as exterior, and passed destinations that come out black on hardware.
func TestDestinationsLookInteresting(t *testing.T) {
	for i, d := range destinations {
		fb := picovga.NewFramebuffer16(160, 120)
		r := finalView(t, fb, i)
		r.draw()
		black, seen := 0, map[uint16]bool{}
		for _, p := range fb.Pix {
			if p == 0 {
				black++
			}
			seen[p] = true
		}
		pct := 100 * float64(black) / float64(len(fb.Pix))
		if pct > 90 {
			t.Errorf("%s: closest view is %.1f%% black, nothing left to look at", d.name, pct)
		}
		if len(seen) < 20 {
			t.Errorf("%s: closest view uses only %d colours", d.name, len(seen))
		}
		t.Logf("%-20s %5.1f%% black, %3d colours", d.name, pct, len(seen))
	}
}

// TestZoomCyclesDestinations checks each cycle heads somewhere new and that the
// list wraps round.
func TestZoomCyclesDestinations(t *testing.T) {
	fb := picovga.NewFramebuffer16(160, 120)
	r := newRenderer(fb)
	visited := []int{r.dest}
	for range destinations {
		for i := 0; ; i++ {
			was := r.dest
			r.zoom()
			if r.dest != was {
				visited = append(visited, r.dest)
				break
			}
			if i > 2000 {
				t.Fatal("a cycle never reached its zoom limit")
			}
		}
	}
	for i, got := range visited {
		if want := i % len(destinations); got != want {
			t.Fatalf("visit %d went to destination %d, want %d", i, got, want)
		}
	}
}

func TestDestinationsNamed(t *testing.T) {
	if len(destinations) < 5 {
		t.Errorf("only %d destinations", len(destinations))
	}
	seen := map[string]bool{}
	for _, d := range destinations {
		if d.name == "" {
			t.Error("a destination has no name")
		}
		if seen[d.name] {
			t.Errorf("duplicate destination %q", d.name)
		}
		seen[d.name] = true
	}
}
