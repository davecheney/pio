package main

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

func TestMeshIndicesInBounds(t *testing.T) {
	if len(teapotVerts) == 0 || len(teapotEdges) == 0 {
		t.Fatal("empty mesh")
	}
	for i, e := range teapotEdges {
		if int(e[0]) >= len(teapotVerts) || int(e[1]) >= len(teapotVerts) || e[0] == e[1] {
			t.Fatalf("edge %d = %v invalid", i, e)
		}
	}
}

func identity() orientation {
	return orientation{{1 << fracBits, 0, 0}, {0, 1 << fracBits, 0}, {0, 0, 1 << fracBits}}
}

func TestProjectOriginIsCentered(t *testing.T) {
	var origin [3]int16
	for us := uint64(0); us < 20_000_000; us += 333_333 {
		r := rotationAt(us)
		if p := projectVertex(&origin, &r); p.x != centerX || p.y != centerY {
			t.Fatalf("t=%dus: origin projects to %v", us, p)
		}
	}
}

func TestProjectionIs150PercentOfV3(t *testing.T) {
	// v3 (hardware-verified) used a 440 px focal length; v4+ draw 3/2 larger
	// about the same center.
	if focalLength*2 != 440*3 {
		t.Fatalf("focalLength = %d, want 660", focalLength)
	}
	r := identity()
	v := [3]int16{4096, 2048, 0}
	p := projectVertex(&v, &r)
	if want := int16(centerX + 4096*focalLength/cameraDistance); p.x != want {
		t.Fatalf("x = %d, want %d", p.x, want)
	}
	if want := int16(centerY - 2048*focalLength/cameraDistance); p.y != want {
		t.Fatalf("y = %d, want %d", p.y, want)
	}
}

func TestRotationAtZeroIsIdentity(t *testing.T) {
	if r := rotationAt(0); r != identity() {
		t.Fatalf("rotationAt(0) = %v", r)
	}
	// All three periods divide 234 s, so the full orientation repeats there.
	if r := rotationAt(234_000_000); r != identity() {
		t.Fatalf("rotationAt(234s) = %v", r)
	}
}

// Each axis must pass through all four cardinal orientations (0, 90, 180,
// 270 degrees) and return to identity at its full period.
func TestEachAxisCompletesFullRotation(t *testing.T) {
	const one = 1 << fracBits
	for _, tc := range []struct {
		name     string
		periodUS uint64
		angle    func(a uint32) orientation
		apply    func(r *orientation) [3]int32 // image of a probe vector
		quarter  [4][3]int32                   // expected images at 0, 90, 180, 270 degrees
	}{
		{"yaw", yawPeriodUS, func(a uint32) orientation { return rotation(a, 0, 0) },
			func(r *orientation) [3]int32 { return [3]int32{r[0][0], r[1][0], r[2][0]} }, // x axis image
			[4][3]int32{{one, 0, 0}, {0, 0, -one}, {-one, 0, 0}, {0, 0, one}}},
		{"pitch", pitchPeriodUS, func(a uint32) orientation { return rotation(0, a, 0) },
			func(r *orientation) [3]int32 { return [3]int32{r[0][1], r[1][1], r[2][1]} }, // y axis image
			[4][3]int32{{0, one, 0}, {0, 0, one}, {0, -one, 0}, {0, 0, -one}}},
		{"roll", rollPeriodUS, func(a uint32) orientation { return rotation(0, 0, a) },
			func(r *orientation) [3]int32 { return [3]int32{r[0][0], r[1][0], r[2][0]} }, // x axis image
			[4][3]int32{{one, 0, 0}, {0, one, 0}, {-one, 0, 0}, {0, -one, 0}}},
	} {
		for i := uint64(0); i < 4; i++ {
			a := axisAngle(i*tc.periodUS/4, tc.periodUS)
			if a != uint32(i)*angleSteps/4 {
				t.Fatalf("%s: angle at %d/4 period = %d", tc.name, i, a)
			}
			r := tc.angle(a)
			if got := tc.apply(&r); got != tc.quarter[i] {
				t.Fatalf("%s at %d degrees: %v, want %v", tc.name, i*90, got, tc.quarter[i])
			}
		}
		if axisAngle(tc.periodUS, tc.periodUS) != 0 {
			t.Fatalf("%s does not wrap at its period", tc.name)
		}
	}
}

func TestAxisAnglesFollowPeriodsAndDirections(t *testing.T) {
	for i := uint64(0); i <= 4; i++ {
		want := uint32(i%4) * angleSteps / 4
		if y, _, _ := axisAngles(i * yawPeriodUS / 4); y != want {
			t.Fatalf("yaw at %d/4 period = %d, want %d", i, y, want)
		}
		if _, p, _ := axisAngles(i * pitchPeriodUS / 4); p != (angleSteps-want)%angleSteps {
			t.Fatalf("pitch (reversed) at %d/4 period = %d, want %d", i, p, (angleSteps-want)%angleSteps)
		}
		if _, _, r := axisAngles(i * rollPeriodUS / 4); r != want {
			t.Fatalf("roll at %d/4 period = %d, want %d", i, r, want)
		}
	}
}

func TestRotationIsOrthonormal(t *testing.T) {
	for us := uint64(0); us < 234_000_000; us += 1_234_567 {
		r := rotationAt(us)
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				dot := int64(0)
				for k := 0; k < 3; k++ {
					dot += int64(r[i][k]) * int64(r[j][k])
				}
				want := int64(0)
				if i == j {
					want = 1 << (2 * fracBits)
				}
				if d := dot - want; d > 1<<(2*fracBits-8) || d < -(1<<(2*fracBits-8)) {
					t.Fatalf("t=%dus rows %d,%d dot %d want %d", us, i, j, dot, want)
				}
			}
		}
	}
}

func TestProjectedMeshBoundedAtAllOrientations(t *testing.T) {
	// Occasional clipping at 150% is accepted; most of the teapot must stay
	// visible and coordinates must stay far from int16 overflow.
	for us := uint64(0); us < 234_000_000; us += 250_000 {
		r := rotationAt(us)
		off := 0
		for i := range teapotVerts {
			p := projectVertex(&teapotVerts[i], &r)
			if p.x < -presto.Width || p.x >= 2*presto.Width || p.y < -presto.Height || p.y >= 2*presto.Height {
				t.Fatalf("t=%dus vertex %d wildly off screen: %v", us, i, p)
			}
			if p.x < 0 || p.x >= presto.Width || p.y < 0 || p.y >= presto.Height {
				off++
			}
		}
		if off*5 > len(teapotVerts) {
			t.Fatalf("t=%dus: %d of %d vertices off screen", us, off, len(teapotVerts))
		}
	}
}

func TestProjectGuardsNearPlane(t *testing.T) {
	v := [3]int16{32767, 32767, 32767}
	for us := uint64(0); us < 20_000_000; us += 100_000 {
		r := rotationAt(us)
		projectVertex(&v, &r) // must not divide by zero or panic
	}
}

func TestWarmupGate(t *testing.T) {
	var count atomic.Uint32
	done := make(chan uint32)
	go func() { done <- waitWarmup(&count) }()
	for n := uint32(1); n < warmupFrames; n++ {
		count.Store(n)
		select {
		case got := <-done:
			t.Fatalf("released after %d frames (%d), want %d", n, got, warmupFrames)
		case <-time.After(5 * time.Millisecond):
		}
	}
	count.Store(warmupFrames)
	if got := <-done; got != warmupFrames {
		t.Fatalf("waitWarmup = %d, want %d", got, warmupFrames)
	}
	if warmupFrames < 1 {
		t.Fatal("renderer must wait for at least one completed frame")
	}
}

func TestRendererTimeDerivedFromFrames(t *testing.T) {
	// 498 lines x 530 dots at 12.5 MHz.
	if frameUS != 498*530*1000/12_500 {
		t.Fatalf("frameUS = %d", frameUS)
	}
}

func TestDrawLineClips(t *testing.T) {
	var fb presto.IndexedFrame
	drawLine(&fb, point{-1000, -1000}, point{1000, 1000}, 7, false)
	for i := 0; i < presto.Width; i++ {
		want := uint8(0)
		if i >= clipMin && i < clipMax {
			want = 7
		}
		if fb[i*presto.Width+i] != want {
			t.Fatalf("diagonal pixel %d = %d, want %d", i, fb[i*presto.Width+i], want)
		}
	}
	var off presto.IndexedFrame
	drawLine(&off, point{-5, -5}, point{-100, 600}, 7, false)
	drawLine(&off, point{480, 0}, point{900, 479}, 7, false)
	drawLine(&off, point{0, 0}, point{479, 1}, 7, false)     // inside the border rows
	drawLine(&off, point{478, 0}, point{479, 479}, 7, false) // inside the border columns
	for i, c := range off {
		if c != 0 {
			t.Fatalf("off-screen line wrote pixel %d", i)
		}
	}
}

func TestDrawLineEndpoints(t *testing.T) {
	var fb presto.IndexedFrame
	drawLine(&fb, point{10, 20}, point{30, 25}, 1, false)
	if fb[20*presto.Width+10] != 1 || fb[25*presto.Width+30] != 1 {
		t.Fatal("endpoints not drawn")
	}
	if n := count(&fb, 1); n != 21 {
		t.Fatalf("drew %d pixels, want 21", n)
	}
}

func TestLinePaletteEntriesIdentical(t *testing.T) {
	if colorLineA == colorLineB {
		t.Fatal("generation indices must differ")
	}
	for _, c := range [...]uint8{colorLineA, colorLineB} {
		if c == colorBackground || c == colorBorder {
			t.Fatalf("line index %d collides with background or border", c)
		}
	}
	if presto.PackPixels(lineRGB565) != presto.PackPixels(lineRGB565) || lineColors != [2]uint8{colorLineA, colorLineB} {
		t.Fatal("line palette entries differ")
	}
	var pal presto.Palette
	pal[colorLineA] = presto.PackPixels(lineRGB565)
	pal[colorLineB] = presto.PackPixels(lineRGB565)
	var fb presto.IndexedFrame
	fb[0], fb[1] = colorLineA, colorLineB
	var line presto.Line
	fb.Expand(&line, 0, &pal)
	if line[0] != lineRGB565<<16|lineRGB565 {
		t.Fatalf("adjacent A/B pixels expand to %#x", line[0])
	}
}

// Conditional erase clears only pixels still holding the old generation.
func TestEraseSparesReclaimedPixels(t *testing.T) {
	var fb presto.IndexedFrame
	drawBorder(&fb)
	old := [2]point{{10, 100}, {200, 100}} // horizontal
	nw := [2]point{{100, 20}, {100, 300}}  // vertical, crosses old at 100,100
	drawLine(&fb, old[0], old[1], colorLineA, false)
	before := fb
	drawLine(&fb, nw[0], nw[1], colorLineB, false)
	drawLine(&fb, old[0], old[1], colorLineA, true)
	if fb[100*presto.Width+100] != colorLineB {
		t.Fatal("shared pixel was cleared")
	}
	if fb[100*presto.Width+50] != colorBackground {
		t.Fatal("removed old pixel not cleared")
	}
	if fb[250*presto.Width+100] != colorLineB {
		t.Fatal("added pixel not lit")
	}
	for i := range fb {
		if before[i] == colorBorder && fb[i] != colorBorder {
			t.Fatal("border damaged")
		}
	}
}

// Across real consecutive frames: every pixel lit in both frames holds a line
// index at every point of the update (it is overwritten, never cleared), and
// after the update exactly the new wireframe is lit.
func TestStepNeverClearsPersistentPixels(t *testing.T) {
	var fb presto.IndexedFrame
	drawBorder(&fb)
	var tp teapot
	tp.step(&fb, 0)
	for f := uint64(1); f < 40; f++ {
		var prev presto.IndexedFrame
		drawBorder(&prev)
		(&teapot{}).step(&prev, (f-1)*frameUS)
		var want presto.IndexedFrame
		drawBorder(&want)
		(&teapot{}).step(&want, f*frameUS)

		// Replay step's two phases to observe the intermediate state.
		oldCur := tp.cur
		mid := fb
		nxt := tp.cur ^ 1
		r := rotationAt(f * frameUS)
		for i := range meshVerts {
			tp.pts[nxt][i] = projectVertex(&meshVerts[i], &r)
		}
		tp.drawEdges(&mid, &tp.pts[nxt], lineColors[nxt], false)
		for i := range mid {
			if prev[i] != colorBackground && prev[i] != colorBorder && mid[i] == colorBackground {
				t.Fatalf("frame %d: old pixel %d vanished before erase", f, i)
			}
		}
		tp.cur = oldCur
		tp.step(&fb, f*frameUS)
		shared := 0
		for i := range fb {
			lit := fb[i] == colorLineA || fb[i] == colorLineB
			wantLit := want[i] == colorLineA || want[i] == colorLineB
			if lit != wantLit || (fb[i] == colorBorder) != (want[i] == colorBorder) {
				t.Fatalf("frame %d pixel %d = %d, want lit %v", f, i, fb[i], wantLit)
			}
			if lit && fb[i] != lineColors[tp.cur] {
				t.Fatalf("frame %d pixel %d has stale generation", f, i)
			}
			if wantLit && (prev[i] == colorLineA || prev[i] == colorLineB) {
				shared++
			}
		}
		if shared == 0 {
			t.Fatalf("frame %d: no shared pixels exercised", f)
		}
	}
}

func TestClearErases(t *testing.T) {
	var fb presto.IndexedFrame
	drawBorder(&fb)
	want := fb
	var tp teapot
	tp.step(&fb, 0)
	tp.step(&fb, 3_000_000)
	tp.clear(&fb)
	if fb != want {
		t.Fatal("clear left pixels or damaged the border")
	}
}

func TestStepErasesPreviousFrame(t *testing.T) {
	var fb presto.IndexedFrame
	drawBorder(&fb)
	var tp teapot
	tp.step(&fb, 0)
	lit := count(&fb, colorLineA) + count(&fb, colorLineB)
	if lit == 0 {
		t.Fatal("first frame drew nothing")
	}
	for a := uint32(1); a < 50; a++ {
		tp.step(&fb, uint64(a)*1_700_000)
	}
	var want presto.IndexedFrame
	drawBorder(&want)
	(&teapot{}).step(&want, 49*1_700_000)
	for i := range want {
		if want[i] == lineColors[1] {
			want[i] = lineColors[tp.cur] // generation parity differs
		}
	}
	if fb != want {
		t.Fatal("incremental redraw left stale pixels")
	}
	if count(&fb, colorBorder) != count(&want, colorBorder) {
		t.Fatal("border damaged")
	}
}

func TestStepDoesNotAllocate(t *testing.T) {
	var fb presto.IndexedFrame
	tp := new(teapot)
	us := uint64(0)
	if n := testing.AllocsPerRun(10, func() { us += frameUS; tp.step(&fb, us) }); n != 0 {
		t.Fatalf("step allocated %v times", n)
	}
}

func TestScanlineExpansionOfRenderedRow(t *testing.T) {
	var fb presto.IndexedFrame
	var pal presto.Palette
	pal[colorBackground] = presto.PackPixels(0x0841)
	pal[colorLineA] = presto.PackPixels(lineRGB565)
	pal[colorLineB] = presto.PackPixels(lineRGB565)
	(&teapot{}).step(&fb, 1_000_000)
	var line presto.Line
	row := fb.Row(centerY)
	fb.Expand(&line, centerY, &pal)
	for i := 0; i < presto.Width; i += 2 {
		want := pal[row[i]]&0xffff0000 | pal[row[i+1]]&0x0000ffff
		if line[i/2] != want {
			t.Fatalf("word %d = %#x want %#x", i/2, line[i/2], want)
		}
	}
}

func count(fb *presto.IndexedFrame, c uint8) int {
	n := 0
	for _, v := range fb {
		if v == c {
			n++
		}
	}
	return n
}

func TestSinTable(t *testing.T) {
	if len(sinTable) != angleSteps || sinTable[0] != 0 || sinTable[angleSteps/4] != 1<<fracBits || sinTable[3*angleSteps/4] != -1<<fracBits {
		t.Fatal("sinTable does not match angleSteps/Q14")
	}
}
