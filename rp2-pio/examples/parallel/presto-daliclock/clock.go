package main

import "time"

// The morph timing in this file is derived from xdaliclock 2.48:
// Copyright © 1991-2022 Jamie Zawinski <jwz@jwz.org>
//
// Permission to use, copy, modify, distribute, and sell this software and its
// documentation for any purpose is hereby granted without fee, provided that
// the above copyright notice appear in all copies and that both that
// copyright notice and this permission notice appear in supporting
// documentation.  No representations are made about the suitability of this
// software for any purpose.  It is provided "as is" without express or
// implied warranty.

const (
	bootHour   = 9
	bootMinute = 41
	bootSecond = 0

	secondsPerDay = 24 * 60 * 60
	bootSeconds   = bootHour*60*60 + bootMinute*60 + bootSecond

	digitGlyph = iota
	colonGlyph
)

type clockTime struct {
	hour, minute, second int
}

func timeFromElapsed(elapsed time.Duration) clockTime {
	total := (bootSeconds + int(elapsed/time.Second)) % secondsPerDay
	if total < 0 {
		total += secondsPerDay
	}
	return clockTime{
		hour:   total / 3600,
		minute: total / 60 % 60,
		second: total % 60,
	}
}

func nextSecond(t clockTime) clockTime {
	total := (t.hour*3600 + t.minute*60 + t.second + 1) % secondsPerDay
	return clockTime{
		hour:   total / 3600,
		minute: total / 60 % 60,
		second: total % 60,
	}
}

func clockDigits(t clockTime) [clockGlyphs]int {
	return [clockGlyphs]int{
		t.hour / 10,
		t.hour % 10,
		colonIndex,
		t.minute / 10,
		t.minute % 10,
		colonIndex,
		t.second / 10,
		t.second % 10,
	}
}

func morphMillis(elapsed time.Duration) int {
	ms := int((elapsed / time.Millisecond) % 1000)
	morphed := ms * 12 / 10
	if morphed > 1000 {
		return 1000
	}
	return morphed
}

func interpolateEndpoint(from, to, msec int) int {
	return from + (to-from)*msec/1000
}
