package main

// The scanline segment rendering in this file is derived from xdaliclock 2.48:
// Copyright © 1991-2022 Jamie Zawinski <jwz@jwz.org>
//
// Permission to use, copy, modify, distribute, and sell this software and its
// documentation for any purpose is hereby granted without fee, provided that
// the above copyright notice appear in all copies and that both that
// copyright notice and this permission notice appear in supporting
// documentation.  No representations are made about the suitability of this
// software for any purpose.  It is provided "as is" without express or
// implied warranty.

func renderClockLine(dst []uint32, row int, fromDigits, toDigits [clockGlyphs]int, msec int, foreground, background uint32) {
	for i := range dst {
		dst[i] = background
	}

	if row < clockY || row >= clockY+clockHeight {
		return
	}

	sourceRow := scaledRow(row - clockY)
	for pos := 0; pos < clockGlyphs; pos++ {
		from := fontRow(fromDigits[pos], sourceRow)
		to := fontRow(toDigits[pos], sourceRow)
		offset := glyphOffsets[pos]
		width := glyphWidth(fromDigits[pos])
		for seg := 0; seg < maxSegments; seg++ {
			left := interpolateEndpoint(int(from[seg].left), int(to[seg].left), msec)
			right := interpolateEndpoint(int(from[seg].right), int(to[seg].right), msec)
			if left < 0 {
				left = 0
			}
			if right > width {
				right = width
			}
			if right <= left {
				continue
			}
			fillWords(dst, scaleX(offset+left), scaleX(offset+right), foreground)
		}
	}
}

func fillWords(dst []uint32, left, right int, word uint32) {
	if right <= left {
		return
	}
	if left < 0 {
		left = 0
	}
	if right > panelWidth {
		right = panelWidth
	}
	start := (left + 1) / 2
	end := right / 2
	for i := start; i < end; i++ {
		dst[i] = word
	}
}
