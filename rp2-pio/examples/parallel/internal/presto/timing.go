package presto

const (
	timingVPulse   = 8
	timingVBack    = 5 + timingVPulse
	timingVDisplay = Height + timingVBack
	timingVFront   = 5 + timingVDisplay

	timingHFront   = 4
	timingHPulse   = 16
	timingHBack    = 30
	timingHDisplay = Width

	pioNop  = 0xb042
	pioIRQ0 = 0xd000
	pioIRQ4 = 0xd004
)

func timingWord(hsync, vsync bool, pixelClocks uint16, instr uint16) uint32 {
	word := uint32(pixelClocks-3)<<16 | uint32(instr)
	if hsync {
		word |= 1 << 30
	}
	if vsync {
		word |= 1 << 31
	}
	return word
}

func indexedTiming(dst *[timingVFront * 4]uint32) {
	for row := 0; row < timingVFront; row++ {
		vsync := row >= timingVPulse
		front, back := uint16(pioNop), uint16(pioNop)
		if row == timingVDisplay {
			front = pioIRQ0 // First blank line after the final visible line.
		}
		if row >= timingVBack && row < timingVDisplay {
			back = pioIRQ4
		}
		dst[row*4] = timingWord(true, vsync, timingHFront, front)
		dst[row*4+1] = timingWord(false, vsync, timingHPulse, pioNop)
		dst[row*4+2] = timingWord(true, vsync, timingHBack, back)
		dst[row*4+3] = timingWord(true, vsync, timingHDisplay, pioNop)
	}
}
