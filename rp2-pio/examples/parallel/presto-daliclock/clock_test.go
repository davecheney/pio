package main

import (
	"testing"
	"time"
)

func TestFontDimensionsAndSegments(t *testing.T) {
	if len(fontD) != 11 {
		t.Fatalf("fontD has %d glyphs, want 11", len(fontD))
	}
	for glyph, data := range fontD {
		width := glyphWidth(glyph)
		if len(data) != glyphHeight*maxSegments*2 {
			t.Fatalf("glyph %d data has %d bytes, want %d", glyph, len(data), glyphHeight*maxSegments*2)
		}
		for y := 0; y < glyphHeight; y++ {
			row := fontRow(glyph, y)
			if len(row) != maxSegments {
				t.Fatalf("glyph %d row %d has %d segments, want %d", glyph, y, len(row), maxSegments)
			}
			realSegments := 0
			for _, seg := range row {
				if int(seg.left) > int(seg.right) {
					t.Fatalf("glyph %d row %d has inverted segment [%d,%d)", glyph, y, seg.left, seg.right)
				}
				if int(seg.right) > width {
					t.Fatalf("glyph %d row %d segment right %d exceeds glyph width %d", glyph, y, seg.right, width)
				}
				if seg.left != seg.right {
					realSegments++
				}
			}
			if realSegments > maxSegments {
				t.Fatalf("glyph %d row %d has %d real segments, want <= %d", glyph, y, realSegments, maxSegments)
			}
		}
	}
}

func TestLayoutDimensionsAndCentering(t *testing.T) {
	if unscaledClockWidth != 632 {
		t.Fatalf("unscaled width = %d, want 632", unscaledClockWidth)
	}
	if clockWidth != 474 {
		t.Fatalf("scaled width = %d, want 474", clockWidth)
	}
	if clockHeight != 96 {
		t.Fatalf("scaled height = %d, want 96", clockHeight)
	}
	if clockX != 3 {
		t.Fatalf("clock x = %d, want 3", clockX)
	}
	if clockY != 192 {
		t.Fatalf("clock y = %d, want 192", clockY)
	}
	if got := scaleX(unscaledClockWidth); got != clockX+clockWidth {
		t.Fatalf("right edge = %d, want %d", got, clockX+clockWidth)
	}
}

func TestInterpolationEndpointsMidpointAndLinger(t *testing.T) {
	if got := interpolateEndpoint(10, 30, 0); got != 10 {
		t.Fatalf("start interpolation = %d, want 10", got)
	}
	if got := interpolateEndpoint(10, 30, 500); got != 20 {
		t.Fatalf("midpoint interpolation = %d, want 20", got)
	}
	if got := interpolateEndpoint(10, 30, 1000); got != 30 {
		t.Fatalf("end interpolation = %d, want 30", got)
	}
	if got := morphMillis(833 * time.Millisecond); got != 999 {
		t.Fatalf("833ms morph = %d, want 999", got)
	}
	if got := morphMillis(834 * time.Millisecond); got != 1000 {
		t.Fatalf("834ms morph = %d, want 1000", got)
	}
	if got := morphMillis(950 * time.Millisecond); got != 1000 {
		t.Fatalf("950ms morph = %d, want 1000", got)
	}
}

func TestBootEpochAndMonotonicConversion(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		want    clockTime
	}{
		{"boot", 0, clockTime{9, 41, 0}},
		{"same second", 999 * time.Millisecond, clockTime{9, 41, 0}},
		{"next second", time.Second, clockTime{9, 41, 1}},
		{"one day", 24 * time.Hour, clockTime{9, 41, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := timeFromElapsed(tt.elapsed); got != tt.want {
				t.Fatalf("timeFromElapsed(%s) = %+v, want %+v", tt.elapsed, got, tt.want)
			}
		})
	}
}

func TestRollovers(t *testing.T) {
	if got := nextSecond(clockTime{9, 41, 59}); got != (clockTime{9, 42, 0}) {
		t.Fatalf("09:41:59 rollover = %+v, want 09:42:00", got)
	}
	if got := nextSecond(clockTime{23, 59, 59}); got != (clockTime{0, 0, 0}) {
		t.Fatalf("23:59:59 rollover = %+v, want 00:00:00", got)
	}
	if got := clockDigits(clockTime{9, 42, 0}); got != ([clockGlyphs]int{0, 9, colonIndex, 4, 2, colonIndex, 0, 0}) {
		t.Fatalf("09:42:00 digits = %+v", got)
	}
}
