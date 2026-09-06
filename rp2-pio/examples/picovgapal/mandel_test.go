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

// TestNoOverflow reruns the iteration in 64 bit arithmetic and checks it agrees
// with the 32 bit one. A disagreement would mean a product had overflowed,
// which is the risk taken by choosing a fixed point format small enough to
// avoid 64 bit multiplies.
func TestNoOverflow(t *testing.T) {
	const maxIter = 64
	wide := func(cr, ci int32) int {
		var zr, zi int64
		c1, c2 := int64(cr), int64(ci)
		for i := 0; i < maxIter; i++ {
			zr2, zi2 := (zr*zr)>>q, (zi*zi)>>q
			if zr2+zi2 > escape {
				return i
			}
			zr, zi = zr2-zi2+c1, ((zr*zi)>>(q-1))+c2
		}
		return maxIter
	}
	const steps = 80
	for iy := 0; iy <= steps; iy++ {
		for ix := 0; ix <= steps; ix++ {
			cr := toFixed(-2.5 + 3.6*float64(ix)/steps)
			ci := toFixed(-1.4 + 2.8*float64(iy)/steps)
			if got, want := escapeCount(cr, ci, maxIter), wide(cr, ci); got != want {
				t.Fatalf("(%d,%d): 32 bit gives %d, 64 bit gives %d, a product overflowed", cr, ci, got, want)
			}
		}
	}
}

func TestRenderFillsFramebuffer(t *testing.T) {
	fb := picovga.NewFramebuffer(64, 48)
	r := newRenderer(fb, 64)
	r.draw()
	black, other := 0, 0
	for i := range fb.Pix {
		if fb.Pix[i] == 0 {
			black++
		} else {
			other++
		}
	}
	// The view holds the whole set, so there must be a substantial interior and
	// a substantial exterior.
	if black < 100 || other < 100 {
		t.Errorf("picture looks wrong: %d black pixels, %d coloured", black, other)
	}
}

// TestZoomResets checks the view returns to the whole set rather than zooming
// past what the fixed point format can resolve.
func TestZoomResets(t *testing.T) {
	fb := picovga.NewFramebuffer(40, 30)
	r := newRenderer(fb, 32)
	start := r.xSpan
	zoomed := false
	for i := 0; i < 200; i++ {
		r.zoom()
		if r.xSpan < r.minSpan() {
			t.Fatalf("zoomed to a span of %d, past the %d limit", r.xSpan, r.minSpan())
		}
		if r.xSpan < start {
			zoomed = true
		}
		if r.xSpan == start && zoomed {
			return // came home
		}
	}
	t.Error("zoom never returned to the whole set")
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestZoomApproachesTarget checks the view closes in on the destination.
//
// It did not: setView pinned the vertical centre to zero and ignored the
// target's imaginary part, so the zoom ran towards a point on the real axis
// instead. Constants may go unused without complaint, so nothing caught it.
func TestZoomApproachesTarget(t *testing.T) {
	fb := picovga.NewFramebuffer(160, 120)
	r := newRenderer(fb, 16)
	// Head for a destination off the real axis: one on it would be reached even
	// by a zoom that ignored the imaginary part, which is the fault this is
	// watching for.
	for i, c := range destinations {
		if c.cy != 0 {
			r.dest = i
			break
		}
	}
	d := r.target()
	span, dx, dy := r.xSpan, abs32(r.cx-d.cx), abs32(r.cy-d.cy)
	if dy == 0 {
		t.Fatal("no destination lies off the real axis, so this cannot test the vertical pan")
	}
	startDY := dy
	dest := r.dest
	for i := 0; i < 24; i++ {
		r.zoom()
		if r.dest != dest {
			t.Fatalf("step %d: cycle ended sooner than expected", i)
		}
		if r.xSpan >= span {
			t.Fatalf("step %d: span went from %d to %d, wanted it to narrow", i, span, r.xSpan)
		}
		ndx, ndy := abs32(r.cx-d.cx), abs32(r.cy-d.cy)
		if ndx > dx || ndy > dy {
			t.Fatalf("step %d: centre moved away from %s, (%d,%d) to (%d,%d)",
				i, d.name, dx, dy, ndx, ndy)
		}
		span, dx, dy = r.xSpan, ndx, ndy
	}
	if r.cy == 0 {
		t.Error("the centre's imaginary part never left zero")
	}
	if dy*4 > startDY {
		t.Errorf("centre is still %d from %s vertically, expected it much closer", dy, d.name)
	}
}

// TestDestinationsInHomeView checks every destination is somewhere the zoom can
// actually set off towards, rather than off the edge of the opening picture.
func TestDestinationsInHomeView(t *testing.T) {
	fb := picovga.NewFramebuffer(160, 120)
	r := newRenderer(fb, 16)
	xLo, xHi := r.xMin, r.xMin+r.xSpan
	yLo, yHi := r.yMin, r.yMin+r.ySpan
	for _, d := range destinations {
		if d.cx < xLo || d.cx > xHi || d.cy < yLo || d.cy > yHi {
			t.Errorf("%s (%.4f,%.4f) lies outside the home view x[%.3f,%.3f] y[%.3f,%.3f]",
				d.name, float64(d.cx)/one, float64(d.cy)/one,
				float64(xLo)/one, float64(xHi)/one, float64(yLo)/one, float64(yHi)/one)
		}
	}
}

// finalView returns the last and closest view of the cycle heading for
// destination i, which is the one most likely to have closed in on solid black.
func finalView(t *testing.T, fb *picovga.Framebuffer, i int) *renderer {
	t.Helper()
	r := newRenderer(fb, maxIter)
	r.dest = i
	cx, cy, span := r.cx, r.cy, r.xSpan
	for n := 0; ; n++ {
		was := r.dest
		cx, cy, span = r.cx, r.cy, r.xSpan
		r.zoom()
		if r.dest != was {
			break
		}
		if n > 1000 {
			t.Fatalf("destination %d: cycle never reached its zoom limit", i)
		}
	}
	r.dest = i
	r.setView(cx, cy, span)
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
		fb := picovga.NewFramebuffer(160, 120)
		r := finalView(t, fb, i)
		r.draw()
		black, seen := 0, map[uint8]bool{}
		for _, p := range fb.Pix {
			if p == 0 {
				black++
			}
			seen[p] = true
		}
		pct := 100 * float64(black) / float64(len(fb.Pix))
		if pct > 70 {
			t.Errorf("%s: closest view is %.1f%% black, nothing left to look at", d.name, pct)
		}
		if pct < 2 {
			t.Errorf("%s: closest view is only %.1f%% black, the set is out of frame", d.name, pct)
		}
		if len(seen) < 20 {
			t.Errorf("%s: closest view uses only %d palette entries", d.name, len(seen))
		}
		t.Logf("%-20s %5.1f%% black, %3d colours", d.name, pct, len(seen))
	}
}

// TestZoomCyclesDestinations checks each cycle heads somewhere new and that the
// list wraps round.
func TestZoomCyclesDestinations(t *testing.T) {
	fb := picovga.NewFramebuffer(160, 120)
	r := newRenderer(fb, 16)
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
