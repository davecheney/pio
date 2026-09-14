package main

const (
	panelWidth  = 480
	panelHeight = 480

	digitWidth  = 90
	colonWidth  = 46
	glyphHeight = 128
	clockGlyphs = 8
	colonIndex  = 10

	scaleNum = 3
	scaleDen = 4

	unscaledClockWidth = digitWidth*6 + colonWidth*2
	clockWidth         = unscaledClockWidth * scaleNum / scaleDen
	clockHeight        = glyphHeight * scaleNum / scaleDen
	clockX             = (panelWidth - clockWidth) / 2
	clockY             = (panelHeight - clockHeight) / 2
)

var glyphOffsets = [clockGlyphs]int{
	0,
	digitWidth,
	digitWidth * 2,
	digitWidth*2 + colonWidth,
	digitWidth*3 + colonWidth,
	digitWidth*4 + colonWidth,
	digitWidth*4 + colonWidth*2,
	digitWidth*5 + colonWidth*2,
}

func glyphWidth(index int) int {
	if index == colonIndex {
		return colonWidth
	}
	return digitWidth
}

func scaleX(x int) int {
	return clockX + x*scaleNum/scaleDen
}

func scaledRow(y int) int {
	return y * scaleDen / scaleNum
}
