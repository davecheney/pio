//go:build rp2350 && rp2350b

package main

import (
	"math"

	"github.com/tinygo-org/pio/rp2-pio/examples/parallel/internal/presto"
)

const (
	white = 0xffff

	borderWidth = 2

	plasmaScale   = 4
	plasmaSamples = presto.Width / plasmaScale
)

type plasmaRenderer struct {
	sinTab    [256]uint8
	palette   [256]uint32
	whiteWord uint32
	t         uint8
}

func main() {
	renderer := newPlasmaRenderer()
	presto.Run(renderer)
}

func newPlasmaRenderer() *plasmaRenderer {
	r := &plasmaRenderer{}
	r.buildTables()
	return r
}

func (r *plasmaRenderer) BeginFrame(frame uint32) {
	r.t = uint8(frame)
}

func (r *plasmaRenderer) RenderLine(dst *presto.Line, row int) {
	if row < borderWidth || row >= presto.Height-borderWidth {
		for i := range dst {
			dst[i] = r.whiteWord
		}
		return
	}

	vertical := r.sinTab[uint8(row)+2*r.t]

	for i := 0; i < plasmaSamples; i++ {
		x := i * plasmaScale
		v := vertical + r.sinTab[uint8(x)+r.t] + r.sinTab[uint8((x+row)>>1)+r.t]
		w := r.palette[v]
		dst[2*i] = w
		dst[2*i+1] = w
	}

	dst[0] = r.whiteWord
	dst[presto.Width/2-1] = r.whiteWord
}

func (r *plasmaRenderer) buildTables() {
	for i := range r.sinTab {
		theta := 2 * math.Pi * float64(i) / 256
		r.sinTab[i] = uint8(127.5 + 127.5*math.Sin(theta))
	}

	for i := range r.palette {
		theta := 2 * math.Pi * float64(i) / 256
		red := uint8(127.5 + 127.5*math.Sin(theta))
		green := uint8(127.5 + 127.5*math.Sin(theta+2*math.Pi/3))
		blue := uint8(127.5 + 127.5*math.Sin(theta+4*math.Pi/3))
		r.palette[i] = presto.PackPixels(presto.RGB565(red, green, blue))
	}

	r.whiteWord = presto.PackPixels(white)
}
