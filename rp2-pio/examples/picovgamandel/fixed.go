package main

// The set is iterated in fixed point. Software floating point would cost
// hundreds of cycles an iteration and a picture needs millions of them, and
// TinyGo builds both these chips with the FPU switched off, so floats would be
// software either way.
//
// Two formats are kept, and a build tagged file picks one. They differ only in
// how many fractional bits they carry and therefore how far the view can zoom
// before neighbouring pixels stop differing.
//
// The narrow one keeps every product inside an int32, which the RP2040's core
// needs: it multiplies 32 bits by 32 bits into 32, and a wider product costs a
// library call. Four integer bits and a sign are enough, a point escaping once
// past two and one further iteration carrying a component to about six.
//
// The wide one uses 64 bit products, which the RP2350's core does in a single
// instruction, and spends the extra sixteen bits on precision. Coordinates
// still fit an int32; only the squares need the wider type, and they must have
// it, because a component of six squares to about 1e10 at this scale and would
// overflow an int32 before the escape test could see it.
const (
	qNarrow = 12
	qWide   = 28

	escapeNarrow = int64(4) << qNarrow
	escapeWide   = int64(4) << qWide
)

// stepNarrow advances z by one iteration of z = z*z + c, reporting whether the
// point had already escaped. Every product stays inside an int32.
func stepNarrow(zr, zi, cr, ci int32) (int32, int32, bool) {
	zr2 := (zr * zr) >> qNarrow
	zi2 := (zi * zi) >> qNarrow
	if int64(zr2)+int64(zi2) > escapeNarrow {
		return zr, zi, true
	}
	return zr2 - zi2 + cr, ((zr * zi) >> (qNarrow - 1)) + ci, false
}

// stepWide is stepNarrow with sixteen more fractional bits. The squares are
// held in an int64 because at this scale they outgrow an int32 as soon as a
// point escapes.
func stepWide(zr, zi, cr, ci int32) (int32, int32, bool) {
	r, i := int64(zr), int64(zi)
	zr2 := (r * r) >> qWide
	zi2 := (i * i) >> qWide
	if zr2+zi2 > escapeWide {
		return zr, zi, true
	}
	return int32(zr2 - zi2 + int64(cr)), int32(((r * i) >> (qWide - 1)) + int64(ci)), false
}

// countNarrow runs a point to escape or to the limit in the narrow format.
func countNarrow(cr, ci int32, maxIter int) int {
	var zr, zi int32
	for n := 0; n < maxIter; n++ {
		nzr, nzi, escaped := stepNarrow(zr, zi, cr, ci)
		if escaped {
			return n
		}
		zr, zi = nzr, nzi
	}
	return maxIter
}

// countWide runs a point to escape or to the limit in the wide format.
func countWide(cr, ci int32, maxIter int) int {
	var zr, zi int32
	for n := 0; n < maxIter; n++ {
		nzr, nzi, escaped := stepWide(zr, zi, cr, ci)
		if escaped {
			return n
		}
		zr, zi = nzr, nzi
	}
	return maxIter
}
