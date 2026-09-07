package mandel

import (
	"math"
	"testing"
)

// counts records escape counts, so a test can look at a picture without a
// framebuffer or a colour scheme.
type counts struct {
	w, h int
	iter []int
	max  int
}

func newCounts(w, h int) *counts { return &counts{w: w, h: h, iter: make([]int, w*h)} }

func (c *counts) Paint(x, y, iter, maxIter int) {
	c.iter[y*c.w+x] = iter
	c.max = maxIter
}

// inSet is how many pixels never escaped, which is what is drawn black.
func (c *counts) inSet() int {
	n := 0
	for _, v := range c.iter {
		if v >= c.max {
			n++
		}
	}
	return n
}

// distinct is how many different escape counts appear, which stands for how
// much structure a picture has.
func (c *counts) distinct() int {
	seen := map[int]bool{}
	for _, v := range c.iter {
		seen[v] = true
	}
	return len(seen)
}

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
func finalView(t *testing.T, w, h int, p Painter, i int) *Renderer {
	t.Helper()
	r := New(w, h, p)
	r.dest = i
	cx, cy, span, step := r.cx, r.cy, r.xSpan, r.step
	for n := 0; ; n++ {
		was := r.dest
		cx, cy, span, step = r.cx, r.cy, r.xSpan, r.step
		r.Zoom()
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
		c := newCounts(160, 120)
		r := finalView(t, 160, 120, c, i)
		r.Draw()
		pct := 100 * float64(c.inSet()) / float64(len(c.iter))
		if pct > 90 {
			t.Errorf("%s: closest view is %.1f%% inside the set, nothing left to look at", d.name, pct)
		}
		if n := c.distinct(); n < 20 {
			t.Errorf("%s: closest view has only %d different escape counts", d.name, n)
		}
		t.Logf("%-20s %5.1f%% in set, %3d escape counts", d.name, pct, c.distinct())
	}
}

func TestZoomCyclesDestinations(t *testing.T) {
	c := newCounts(160, 120)
	r := New(160, 120, c)
	visited := []int{r.dest}
	for range destinations {
		for i := 0; ; i++ {
			was := r.dest
			r.Zoom()
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

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestDestinationsInHomeView checks every destination is somewhere the zoom can
// actually set off towards, rather than off the edge of the opening picture.
func TestDestinationsInHomeView(t *testing.T) {
	r := New(160, 120, newCounts(160, 120))
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

// TestRenderFillsFramebuffer checks a whole picture gets drawn, and that the
// opening view has a substantial interior and a substantial exterior.
func TestRenderFillsFramebuffer(t *testing.T) {
	c := newCounts(64, 48)
	New(64, 48, c).Draw()
	in := c.inSet()
	out := len(c.iter) - in
	if in < 100 || out < 100 {
		t.Errorf("picture looks wrong: %d pixels in the set, %d outside", in, out)
	}
}

// TestZoomResets checks the view returns to the whole set rather than zooming
// past what the fixed point format can resolve.
func TestZoomResets(t *testing.T) {
	r := New(160, 120, newCounts(160, 120))
	start := r.xSpan
	zoomed := false
	for i := 0; i < 500; i++ {
		r.Zoom()
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

// TestZoomApproachesTarget checks the view closes in on the destination.
//
// It did not once: setView pinned the vertical centre to zero and ignored the
// destination's imaginary part, so the zoom ran towards a point on the real
// axis instead. Constants may go unused without complaint, so nothing else
// caught it.
func TestZoomApproachesTarget(t *testing.T) {
	r := New(160, 120, newCounts(160, 120))
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
	startDY, dest := dy, r.dest
	for i := 0; i < 24; i++ {
		r.Zoom()
		if r.dest != dest {
			t.Fatalf("step %d: cycle ended sooner than expected", i)
		}
		if r.xSpan >= span {
			t.Fatalf("step %d: span went from %d to %d, wanted it to narrow", i, span, r.xSpan)
		}
		ndx, ndy := abs32(r.cx-d.cx), abs32(r.cy-d.cy)
		if ndx > dx || ndy > dy {
			t.Fatalf("step %d: centre moved away from %s", i, d.name)
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
