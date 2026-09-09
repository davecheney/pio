package main

import "time"

const (
	videoWidth       = 320
	videoHeight      = 240
	videoStride      = videoWidth / 8
	videoFrameBytes  = videoStride * videoHeight
	videoFramePeriod = time.Second / 30
)

var (
	monoFramebuffer [videoFrameBytes]byte
	monoExpand      [256][16]byte
)

func init() {
	for value := range monoExpand {
		for pixel := 0; pixel < 8; pixel++ {
			if value&(1<<uint(7-pixel)) != 0 {
				monoExpand[value][pixel*2] = 0xff
				monoExpand[value][pixel*2+1] = 0xff
			}
		}
	}
}

func runVideo(st *ST7789) {
	if st.width != videoWidth || st.height != videoHeight {
		panic("video: unexpected display dimensions")
	}

	clear(st.fb)
	if err := st.Display(); err != nil {
		panic(err.Error())
	}
	startVideoCue()
	nextFrame := time.Now()
	var stats videoStats
	firstFrame := true
	for {
		for _, applyFrame := range videoFrames {
			frameStart := time.Now()

			start := time.Now()
			applyFrame(&monoFramebuffer)
			stats.apply += time.Since(start)

			start = time.Now()
			expandMono(st.fb, monoFramebuffer[:])
			stats.expand += time.Since(start)

			start = time.Now()
			if err := st.Display(); err != nil {
				panic(err.Error())
			}
			stats.display += time.Since(start)
			if firstFrame {
				finishVideoCue()
				firstFrame = false
			}

			elapsed := time.Since(frameStart)
			if elapsed > stats.maxFrame {
				stats.maxFrame = elapsed
			}
			stats.frames++

			nextFrame = nextFrame.Add(videoFramePeriod)
			if delay := time.Until(nextFrame); delay > 0 {
				time.Sleep(delay)
			} else {
				stats.overruns++
				nextFrame = time.Now()
			}

			if stats.frames%300 == 0 {
				stats.report()
				stats = videoStats{}
			}
		}
	}
}

func expandMono(rgb565, mono []byte) {
	output := 0
	for _, packed := range mono {
		copy(rgb565[output:output+16], monoExpand[packed][:])
		output += 16
	}
}

type videoStats struct {
	frames   int
	overruns int
	apply    time.Duration
	expand   time.Duration
	display  time.Duration
	maxFrame time.Duration
}

func (s videoStats) report() {
	if s.frames == 0 {
		return
	}
	println(
		"video",
		"frames=", s.frames,
		"apply_us=", int(s.apply/time.Microsecond)/s.frames,
		"expand_us=", int(s.expand/time.Microsecond)/s.frames,
		"display_us=", int(s.display/time.Microsecond)/s.frames,
		"max_frame_us=", int(s.maxFrame/time.Microsecond),
		"overruns=", s.overruns,
	)
}
