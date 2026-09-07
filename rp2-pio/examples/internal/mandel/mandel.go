package mandel

// The set is iterated in fixed point. The RP2040 has no floating point unit, so
// software floats would cost hundreds of cycles an iteration, and a picture of
// this size needs millions of them.
//
// q fractional bits leave 32-q for the integer part and sign. A point escapes
// once |z| exceeds two, and one more iteration can carry a component to about
// six, so four integer bits are enough. That keeps every value inside sixteen
// bits, and the product of two of them inside an int32, so the iteration needs
// only 32 bit multiplies rather than the 64 bit ones a larger q would force.
// one is the fixed point scale of whichever format was selected.
const one = 1 << q

// The view is held as a centre and a width, all in fixed point.
const (
	homeX     = -7 * one / 10 // -0.7, the set centred
	homeY     = 0
	homeXSpan = 3 * one

	// The view narrows by this much each time the picture is redrawn. Closer to
	// one gives a slower, smoother zoom and more pictures to get there.
	zoomNum, zoomDen = 15, 16

	// The centre moves this fraction of the way to the target each time, so the
	// view drifts onto the point of interest instead of jumping to it.
	panDivisor = 8
)

// destination is a place on the set's edge worth zooming into. Every cycle
// starts from the whole set and closes in on the next of these in turn.
//
// The coordinates are given to four decimal places, which is as much as the
// fixed point format can carry and far more than the shallowest zoom here can
// resolve. The famous deep zoom coordinates for these places run to fifteen
// figures; those are for zooms thousands of times closer than this can reach.
type destination struct {
	name   string // not displayed, but says what the coordinates are
	cx, cy int32
	// iter is the iteration limit not to exceed here. The limit climbs with
	// the zoom, and this caps where it ends up; a place whose detail needs
	// more than the others get says so.
	iter int
}

// The places the zoom heads for, given to nine decimal places.
//
// Each is written as a count of billionths scaled by one, so the same table
// serves either fixed point format: the wide one carries all nine digits, the
// narrow one rounds them to its own grid, and both land on the same place.
//
// Precision is what makes these worth having. At the narrow format's depth,
// about forty times, the famous valleys and junctions are a thread in a black
// field and had to be thrown out; the wide format reaches four thousand times,
// where their detail opens up. Seahorse valley finishes 65% black at the
// shallow depth and 3% at the deep one.
//
// Every entry was measured at both depths, at the iteration limit each build
// uses. TestDestinationsLookInteresting does that measurement and fails if a
// coordinate is changed for one that comes out flat.
var destinations = []destination{
	{"mini mandelbrot", -1749204600 * one / 1000000000, 0, 256},
	{"misiurewicz point", -775683770 * one / 1000000000, 136467370 * one / 1000000000, 256},
	{"feigenbaum point", -1401155189 * one / 1000000000, 0, 384},
	{"north spiral", -160701350 * one / 1000000000, 1037566500 * one / 1000000000, 256},
	{"upper filament", -235125000 * one / 1000000000, 827215000 * one / 1000000000, 256},
	{"deep spiral", 1643722 * one / 1000000000, -822467633 * one / 1000000000, 256},
	{"elephant valley", 292575500 * one / 1000000000, -14997700 * one / 1000000000, 256},
	{"seahorse valley", -743643887 * one / 1000000000, 131825904 * one / 1000000000, 256},
}

// The iteration limit follows the zoom. Measured against these destinations,
// thirty two iterations are enough to draw the whole set, forty eight still
// suffice at a hundred times, and four thousand times needs a hundred and
// sixty. Little detail waits to be resolved early in a cycle, so holding the
// deepest limit throughout would spend most of a cycle's time on nothing.
//
// The limit is quadratic in the number of zooms taken, which is itself the
// logarithm of the depth, and lands at about twice what was measured to be
// necessary. At the narrow format's limit it comes to about ninety, which is
// what that build used as a fixed value before.
const (
	iterBase   = 48
	iterSpread = 70
)

// Painter receives each pixel as the picture is drawn: where it is, how many
// iterations it took to escape, and the limit it was drawn at. What a pixel is
// stored as is the caller's business, which is what lets one renderer serve a
// framebuffer of colours and a framebuffer of palette indices.
type Painter interface {
	Paint(x, y, iter, maxIter int)
}

// Renderer draws the Mandelbrot set, zooming from one place to the next.
type Renderer struct {
	w, h int
	p    Painter

	// The region being drawn: a centre, a width, and the derived top left
	// corner and height.
	cx, cy, xSpan, ySpan int32
	xMin, yMin           int32

	// dest is the destination the current cycle is heading for, and step how
	// many zooms have been taken towards it.
	dest, step int
}

// New returns a renderer drawing a picture w by h pixels into p.
func New(w, h int, p Painter) *Renderer {
	r := &Renderer{w: w, h: h, p: p}
	r.home()
	return r
}

// home returns the view to the whole set.
func (r *Renderer) home() {
	r.step = 0
	r.setView(homeX, homeY, homeXSpan)
}

// iterFor is the iteration limit for the view now in frame.
func (r *Renderer) iterFor() int {
	n := iterBase + r.step*r.step/iterSpread
	if cap := r.target().iter; n > cap {
		return cap
	}
	return n
}

// setView frames a region of the given width about a centre.
func (r *Renderer) setView(cx, cy, xSpan int32) {
	r.cx, r.cy, r.xSpan = cx, cy, xSpan
	// Framebuffer pixels are square on screen, so the vertical span follows the
	// framebuffer's shape.
	r.ySpan = int32(int64(xSpan) * int64(r.h) / int64(r.w))
	r.xMin = cx - xSpan/2
	r.yMin = cy - r.ySpan/2
}

// minSpan is the narrowest view the fixed point format can still resolve. Below
// about two units of the last fractional bit per pixel, neighbouring pixels stop
// differing and the picture goes blocky, so this depends on how wide the
// framebuffer is.
func (r *Renderer) minSpan() int32 {
	floor := int32(2 * r.w) // what the format can still tell apart
	chosen := int32(homeXSpan / maxZoom)
	if chosen > floor {
		return chosen
	}
	return floor
}

// target is the place the current cycle is heading for.
func (r *Renderer) target() destination { return destinations[r.dest] }

// zoom narrows the view a little and eases its centre towards the current
// destination. Once the fixed point values can no longer tell neighbouring
// pixels apart the cycle ends, and the next one starts from the whole set again
// and heads somewhere else.
// Zoom narrows the view towards the current destination, moving on to the next
// place once it can go no closer.
func (r *Renderer) Zoom() {
	span := int32(int64(r.xSpan) * zoomNum / zoomDen)
	if span < r.minSpan() || span == r.xSpan {
		r.dest = (r.dest + 1) % len(destinations)
		r.home()
		return
	}
	d := r.target()
	r.step++
	r.setView(r.cx+(d.cx-r.cx)/panDivisor, r.cy+(d.cy-r.cy)/panDivisor, span)
}

// draw renders the whole picture, left to right and top to bottom.
//
// It runs on a core of its own, so it never has to stop and let anything else
// happen and needs no dividing up. The display reads the framebuffer while this
// is filling it in, with no locking between them, so the picture is watched
// being painted; a half drawn frame is the worst that can be seen.
// Draw renders the whole picture.
func (r *Renderer) Draw() {
	iter := r.iterFor()
	for py := 0; py < r.h; py++ {
		ci := r.yMin + int32(int64(py)*int64(r.ySpan)/int64(r.h))
		for px := 0; px < r.w; px++ {
			cr := r.xMin + int32(int64(px)*int64(r.xSpan)/int64(r.w))
			r.p.Paint(px, py, escapeCount(cr, ci, iter), iter)
		}
	}
}
