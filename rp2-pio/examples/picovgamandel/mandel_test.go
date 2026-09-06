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
	fb := picovga.NewFramebuffer16(64, 48)
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

func TestPalette(t *testing.T) {
	if got := palette(64, 64); got != picovga.Black555 {
		t.Errorf("points in the set should be black, got %#04x", got)
	}
	if got := palette(65, 64); got != picovga.Black555 {
		t.Errorf("counts past the limit should be black, got %#04x", got)
	}
	if palette(0, 64) == picovga.Black555 {
		t.Error("points that escape at once should not be black")
	}
	// A colour must never set bit 5, which drives the SD card clock.
	for i := 0; i < 256; i++ {
		if palette(i, 1000)&(1<<5) != 0 {
			t.Fatalf("palette(%d) sets bit 5", i)
		}
	}
}

// TestZoomResets checks the view returns to the whole set rather than zooming
// past what the fixed point format can resolve.
func TestZoomResets(t *testing.T) {
	fb := picovga.NewFramebuffer16(40, 30)
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
	fb := picovga.NewFramebuffer16(160, 120)
	r := newRenderer(fb, 16)
	d := r.target()
	span, dx, dy := r.xSpan, abs32(r.cx-d.cx), abs32(r.cy-d.cy)
	if dy == 0 {
		t.Fatal("test is meaningless: the home view already sits on the destination")
	}
	startDY := dy
	for i := 0; i < 24; i++ {
		r.zoom()
		if r.dest != 0 {
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
	fb := picovga.NewFramebuffer16(160, 120)
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

// TestDestinationsAreOnTheEdge checks each destination sits on the set's
// boundary, where the interesting structure is, rather than deep inside it or
// out in the empty exterior. A mistyped coordinate would land in one or the
// other and give a cycle that zooms into a featureless field.
func TestDestinationsAreOnTheEdge(t *testing.T) {
	const maxIter = 200
	const step = 150 * one / 10000 // 0.015
	for _, d := range destinations {
		inSet, escaped := 0, 0
		for iy := -2; iy <= 2; iy++ {
			for ix := -2; ix <= 2; ix++ {
				if escapeCount(d.cx+int32(ix)*step, d.cy+int32(iy)*step, maxIter) >= maxIter {
					inSet++
				} else {
					escaped++
				}
			}
		}
		if inSet == 0 || escaped == 0 {
			t.Errorf("%s (%.4f,%.4f): %d of 25 samples inside the set, wanted a mix; the point is not on the edge",
				d.name, float64(d.cx)/one, float64(d.cy)/one, inSet)
		}
	}
}

// TestZoomCyclesDestinations checks each cycle heads somewhere new and that the
// list wraps round.
func TestZoomCyclesDestinations(t *testing.T) {
	fb := picovga.NewFramebuffer16(160, 120)
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
