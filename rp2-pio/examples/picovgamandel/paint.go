package main

import "github.com/tinygo-org/pio/rp2-pio/picovga"

// painter writes each pixel straight into a framebuffer of colours. The set
// itself is drawn by the shared mandel package, which knows nothing about how a
// pixel is stored; this is the whole of the difference between this demo and
// the palettised one.
type painter struct{ fb *picovga.Framebuffer16 }

func (p painter) Paint(x, y, iter, maxIter int) {
	p.fb.SetPixel(x, y, palette(iter, maxIter))
}

// palette maps an escape count to a colour. Points that never escape are left
// black, and the rest cycle through a ramp so that the bands stay distinct
// however high the count goes.
func palette(iter, maxIter int) picovga.Colour {
	if iter >= maxIter {
		return picovga.Black555
	}
	v := uint8(iter & 31)
	switch (iter >> 5) % 3 {
	case 0:
		return picovga.RGB555(v, 0, 31-v)
	case 1:
		return picovga.RGB555(31-v, v, 0)
	default:
		return picovga.RGB555(0, 31-v, v)
	}
}
