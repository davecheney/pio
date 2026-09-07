package main

import "github.com/tinygo-org/pio/rp2-pio/picovga"

// painter writes each pixel into a framebuffer of palette indices. The set
// itself is drawn by the shared mandel package, which knows nothing about how a
// pixel is stored; this is the whole of the difference between this demo and
// the direct colour one.
type painter struct{ fb *picovga.Framebuffer }

func (p painter) Paint(x, y, iter, maxIter int) {
	p.fb.SetPixel(x, y, index(iter, maxIter))
}
