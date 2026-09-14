package main

import (
	"math"
	"time"
)

// The colour cycling and HSV conversion in this file are derived from
// xdaliclock 2.48:
// Copyright © 1991-2022 Jamie Zawinski <jwz@jwz.org>
//
// Permission to use, copy, modify, distribute, and sell this software and its
// documentation for any purpose is hereby granted without fee, provided that
// the above copyright notice appear in all copies and that both that
// copyright notice and this permission notice appear in supporting
// documentation.  No representations are made about the suitability of this
// software for any purpose.  It is provided "as is" without express or
// implied warranty.

func colorRGB(elapsed time.Duration) (fr, fg, fb, br, bg, bb uint8) {
	const scale = 3
	tick := int(elapsed / (50 * time.Millisecond))
	fgHue := (tick * 2) % (360 * scale)
	bgHue := (fgHue + 180*scale + tick) % (360 * scale)
	fr, fg, fb = hsvToRGB(float64(fgHue)/scale, 1, 1)
	br, bg, bb = hsvToRGB(float64(bgHue)/scale, 1, 0.4)
	return
}

func hsvToRGB(h, s, v float64) (uint8, uint8, uint8) {
	for h > 360 {
		h -= 360
	}
	for h < 0 {
		h += 360
	}

	h /= 60
	i := math.Floor(h)
	f := h - i
	p1 := v * (1 - s)
	p2 := v * (1 - s*f)
	p3 := v * (1 - s*(1-f))

	var r, g, b float64
	switch int(i) {
	case 0:
		r, g, b = v, p3, p1
	case 1:
		r, g, b = p2, v, p1
	case 2:
		r, g, b = p1, v, p3
	case 3:
		r, g, b = p1, p2, v
	case 4:
		r, g, b = p3, p1, v
	default:
		r, g, b = v, p1, p2
	}
	return uint8(r * 255), uint8(g * 255), uint8(b * 255)
}
