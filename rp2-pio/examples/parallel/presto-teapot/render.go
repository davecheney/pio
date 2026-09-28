package main

import (
	"sync/atomic"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

const (
	colorBackground = 0
	colorLineA      = 1 // colorLineA and colorLineB map to the same RGB565
	colorBorder     = 2
	colorLineB      = 3
	lineRGB565      = 0x07ff // cyan

	angleSteps = 1024 // len(sinTable); checked by TestSinTable
	fracBits   = 14

	cameraDistance = 10 * 2048 // model units, same x2048 scale as the mesh
	focalLength    = 660       // pixels; 150% of the v3 440 px teapot
	centerX        = presto.Width / 2
	centerY        = presto.Height / 2

	// warmupFrames complete scanout frames must succeed with the static
	// border-only image before the renderer starts writing the framebuffer.
	warmupFrames = 4
)

// waitWarmup blocks until count reports warmupFrames completed frames and
// returns that count. The frame boundary that reaches it is the start of a
// vertical blank, so the first update begins there.
func waitWarmup(count *atomic.Uint32) uint32 {
	last := count.Load()
	for last < warmupFrames {
		last = presto.WaitFrame(count, last)
	}
	return last
}

// sinTable (Q14, one revolution in angleSteps) is a generated constant in
// teapot_mesh.go, so this package has no runtime init.

// The renderer reads the mesh through these; on hardware main points them at
// SRAM copies before scanout so it does not churn the XIP cache.
var (
	meshVerts = &teapotVerts
	meshEdges = &teapotEdges
)

// Full 360 degree rotation periods on all three axes. The periods are
// pairwise coprime in 0.5 s units, so the combined orientation only repeats
// every 234 s. Pitch runs backwards.
const (
	yawPeriodUS   = 6_000_000
	pitchPeriodUS = 9_000_000
	rollPeriodUS  = 13_000_000

	// frameUS is the nominal scanout period (498 lines x 530 dots at
	// 12.5 MHz = 21115.2 us); the renderer derives time from FrameCount.
	frameUS = 21115
)

type point struct{ x, y int16 }

// orientation is a Q14 rotation matrix.
type orientation [3][3]int32

// axisAngle maps elapsed time to 0..angleSteps-1 over one full period.
func axisAngle(us uint64, periodUS uint64) uint32 {
	return uint32(us % periodUS * angleSteps / periodUS)
}

func sinCos(angle uint32) (s, c int32) {
	return sinTable[angle%angleSteps], sinTable[(angle+angleSteps/4)%angleSteps]
}

// rotationAt returns yaw (about y) * pitch (about x) * roll (about z) at time
// us, with pitch reversed.
func rotationAt(us uint64) orientation {
	return rotation(axisAngles(us))
}

func axisAngles(us uint64) (yaw, pitch, roll uint32) {
	return axisAngle(us, yawPeriodUS),
		(angleSteps - axisAngle(us, pitchPeriodUS)) % angleSteps,
		axisAngle(us, rollPeriodUS)
}

func q(a, b int32) int32 { return a * b >> fracBits }

func rotation(yaw, pitch, roll uint32) orientation {
	sy, cy := sinCos(yaw)
	sp, cp := sinCos(pitch)
	sr, cr := sinCos(roll)
	// R = Ry(yaw) * Rx(pitch) * Rz(roll)
	return orientation{
		{q(cy, cr) + q(q(sy, sp), sr), q(q(sy, sp), cr) - q(cy, sr), q(sy, cp)},
		{q(cp, sr), q(cp, cr), -sp},
		{q(q(cy, sp), sr) - q(sy, cr), q(sy, sr) + q(q(cy, sp), cr), q(cy, cp)},
	}
}

// projectVertex rotates v by r and applies a perspective projection centered
// on the display. The result may lie off screen; drawLine clips.
func projectVertex(v *[3]int16, r *orientation) point {
	x, y, z := int32(v[0]), int32(v[1]), int32(v[2])
	x1 := (r[0][0]*x + r[0][1]*y + r[0][2]*z) >> fracBits
	y1 := (r[1][0]*x + r[1][1]*y + r[1][2]*z) >> fracBits
	z1 := (r[2][0]*x + r[2][1]*y + r[2][2]*z) >> fracBits

	den := z1 + cameraDistance
	if den < cameraDistance/4 {
		den = cameraDistance / 4 // never closer than the model allows; guards division
	}
	return point{
		x: int16(centerX + x1*focalLength/den),
		y: int16(centerY - y1*focalLength/den),
	}
}

// Lines are clipped to the area inside the 2 px border, so a clipped teapot
// never draws over or erases the border.
const (
	clipMin = 2
	clipMax = presto.Width - 2
)

// drawLine draws a Bresenham line into fb. When erase is false every pixel
// becomes color. When erase is true a pixel becomes colorBackground only if it
// still holds color, so pixels already claimed by another generation survive.
// Pixels outside the area inside the 2 px border are skipped, so the border is
// never touched.
func drawLine(fb *presto.IndexedFrame, a, b point, color uint8, erase bool) {
	x0, y0, x1, y1 := int(a.x), int(a.y), int(b.x), int(b.y)
	dx, sx := x1-x0, 1
	if dx < 0 {
		dx, sx = -dx, -1
	}
	dy, sy := y1-y0, 1
	if dy < 0 {
		dy, sy = -dy, -1
	}
	err := dx - dy
	for {
		if uint(x0-clipMin) < clipMax-clipMin && uint(y0-clipMin) < clipMax-clipMin {
			i := y0*presto.Width + x0
			if !erase {
				fb[i] = color
			} else if fb[i] == color {
				fb[i] = colorBackground
			}
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// lineColors are two palette indices with identical RGB565 values. Frames
// alternate between them so the previous wireframe can be erased after the
// new one is drawn without clearing pixels the new one shares.
var lineColors = [2]uint8{colorLineA, colorLineB}

// teapot updates the framebuffer in place with no intermediate erased state:
// it draws the new wireframe first with this generation's line index, then
// erases the previous wireframe only where pixels still hold the previous
// generation's index. The screen briefly shows the union of both frames, never
// a gap. All storage is fixed; step does not allocate.
type teapot struct {
	pts     [2][len(teapotVerts)]point
	cur     int // index into pts and lineColors of the shown wireframe
	hasPrev bool
}

func (t *teapot) step(fb *presto.IndexedFrame, us uint64) {
	r := rotationAt(us)
	nxt := t.cur ^ 1
	next := &t.pts[nxt]
	verts := meshVerts
	for i := range verts {
		next[i] = projectVertex(&verts[i], &r)
	}
	t.drawEdges(fb, next, lineColors[nxt], false)
	if t.hasPrev {
		t.drawEdges(fb, &t.pts[t.cur], lineColors[t.cur], true)
	}
	t.cur = nxt
	t.hasPrev = true
}

// clear erases the shown wireframe.
func (t *teapot) clear(fb *presto.IndexedFrame) {
	if t.hasPrev {
		t.drawEdges(fb, &t.pts[t.cur], lineColors[t.cur], true)
		t.hasPrev = false
	}
}

func (t *teapot) drawEdges(fb *presto.IndexedFrame, pts *[len(teapotVerts)]point, color uint8, erase bool) {
	edges := meshEdges
	for i := range edges {
		e := &edges[i]
		drawLine(fb, pts[e[0]], pts[e[1]], color, erase)
	}
}

func drawBorder(fb *presto.IndexedFrame) {
	for i := 0; i < presto.Width; i++ {
		for _, j := range [...]int{0, 1, presto.Height - 2, presto.Height - 1} {
			fb[j*presto.Width+i] = colorBorder
			fb[i*presto.Width+j] = colorBorder
		}
	}
}
