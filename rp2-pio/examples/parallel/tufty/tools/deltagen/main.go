package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
)

const (
	frameWidth  = 320
	frameHeight = 240
	monoStride  = frameWidth / 8
	monoSize    = monoStride * frameHeight
	deltaBlock  = 32
	deltaBlocks = monoSize / deltaBlock
	deltaMapLen = (deltaBlocks + 7) / 8
)

var bayer8 = [8][8]uint8{
	{0, 48, 12, 60, 3, 51, 15, 63},
	{32, 16, 44, 28, 35, 19, 47, 31},
	{8, 56, 4, 52, 11, 59, 7, 55},
	{40, 24, 36, 20, 43, 27, 39, 23},
	{2, 50, 14, 62, 1, 49, 13, 61},
	{34, 18, 46, 30, 33, 17, 45, 29},
	{10, 58, 6, 54, 9, 57, 5, 53},
	{42, 26, 38, 22, 41, 25, 37, 21},
}

type options struct {
	input          string
	output         string
	frames         int
	blackPoint     int
	whitePoint     int
	ditherJitter   int
	copyThreshold  int
	generatedPkg   string
	progressFrames int
	inspectStart   int
	inspectEnd     int
}

type statistics struct {
	changes          []int
	rowSpanBytes     []int
	changedRows      []int
	contiguousRuns   []int
	sparseFrames     int
	fullFrames       int
	encodedBytes     int64
	unpackedBytes    int64
	rowSpanPayload   int64
	rowSpanCallCount int64
	runPayload       int64
	runCallCount     int64
	directBytes      int64
}

func main() {
	var opts options
	flag.StringVar(&opts.input, "input", "input.webm", "input video")
	flag.StringVar(&opts.output, "output", "", "generated Go output (optional)")
	flag.IntVar(&opts.frames, "frames", 0, "maximum frames to process (zero means all)")
	flag.IntVar(&opts.blackPoint, "black-point", 24, "source luma mapped to pure black before downsampling")
	flag.IntVar(&opts.whitePoint, "white-point", 231, "source luma mapped to pure white before downsampling")
	flag.IntVar(&opts.ditherJitter, "dither-jitter", 0, "temporal dither threshold variation in luma levels")
	flag.IntVar(&opts.copyThreshold, "copy-threshold", monoSize, "encoded delta bytes at which to emit a full-frame copy")
	flag.StringVar(&opts.generatedPkg, "package", "main", "package name for generated Go")
	flag.IntVar(&opts.progressFrames, "progress", 300, "print progress every N frames (zero disables)")
	flag.IntVar(&opts.inspectStart, "inspect-start", -1, "first frame to report individually")
	flag.IntVar(&opts.inspectEnd, "inspect-end", -1, "last frame to report individually")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "deltagen:", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	if opts.frames < 0 {
		return errors.New("frames must not be negative")
	}
	if opts.blackPoint < 0 || opts.blackPoint > 254 {
		return errors.New("black-point must be between 0 and 254")
	}
	if opts.whitePoint < 1 || opts.whitePoint > 255 {
		return errors.New("white-point must be between 1 and 255")
	}
	if opts.blackPoint >= opts.whitePoint {
		return errors.New("black-point must be less than white-point")
	}
	if opts.ditherJitter < 0 || opts.ditherJitter > 16 {
		return errors.New("dither-jitter must be between 0 and 16")
	}
	if opts.copyThreshold <= 0 || opts.copyThreshold > monoSize {
		return fmt.Errorf("copy-threshold must be between 1 and %d", monoSize)
	}
	if opts.generatedPkg == "" {
		return errors.New("package must not be empty")
	}

	contrast := fmt.Sprintf(
		"lut=y='clip((val-%d)*255/(%d-%d),0,255)'",
		opts.blackPoint,
		opts.whitePoint,
		opts.blackPoint,
	)
	args := []string{
		"-v", "error",
		"-i", opts.input,
		"-an",
		"-vf", fmt.Sprintf(
			"format=gray,%s,scale=%d:%d:flags=area",
			contrast,
			frameWidth,
			frameHeight,
		),
	}
	if opts.frames > 0 {
		args = append(args, "-frames:v", strconv.Itoa(opts.frames))
	}
	args = append(args, "-f", "rawvideo", "-pix_fmt", "gray", "pipe:1")

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var output *bufio.Writer
	var outputFile *os.File
	if opts.output != "" {
		if err := os.MkdirAll(filepath.Dir(opts.output), 0o755); err != nil {
			return err
		}
		outputFile, err = os.Create(opts.output)
		if err != nil {
			return err
		}
		defer outputFile.Close()
		output = bufio.NewWriterSize(outputFile, 1<<20)
		if err := writeHeader(output, opts.generatedPkg); err != nil {
			return err
		}
	}

	stats, processErr := processFrames(stdout, output, opts)
	waitErr := cmd.Wait()
	if processErr != nil {
		return processErr
	}
	if waitErr != nil {
		return waitErr
	}
	if output != nil {
		if err := writeFooter(output, len(stats.changes)); err != nil {
			return err
		}
		if err := output.Flush(); err != nil {
			return err
		}
	}
	printStatistics(stats, opts.copyThreshold)
	return nil
}

func processFrames(r io.Reader, output *bufio.Writer, opts options) (statistics, error) {
	gray := make([]byte, frameWidth*frameHeight)
	var previous [monoSize]byte
	var stats statistics

	for frame := 0; ; frame++ {
		_, err := io.ReadFull(r, gray)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stats, fmt.Errorf("read frame %d: %w", frame, err)
		}

		current := packFrame(gray, frame, opts.ditherJitter)
		changed := changedOffsets(previous[:], current[:])
		if frame == 0 {
			changed = allOffsets()
		}
		rowBytes, rows := changedRowSpans(previous[:], current[:])
		if frame == 0 {
			rowBytes, rows = monoSize, frameHeight
		}
		runs := countContiguousRuns(changed)
		delta := encodeBlockDelta(previous[:], current[:])
		stats.changes = append(stats.changes, len(changed))
		stats.rowSpanBytes = append(stats.rowSpanBytes, rowBytes)
		stats.changedRows = append(stats.changedRows, rows)
		stats.contiguousRuns = append(stats.contiguousRuns, runs)
		stats.unpackedBytes += monoSize
		stats.rowSpanPayload += int64(rowBytes)
		stats.rowSpanCallCount += int64(rows)
		stats.runPayload += int64(len(changed))
		stats.runCallCount += int64(runs)
		copyFrame := frame == 0 || len(delta) >= opts.copyThreshold
		if copyFrame {
			stats.fullFrames++
			stats.encodedBytes += monoSize
		} else {
			stats.sparseFrames++
			stats.encodedBytes += int64(len(delta))
		}
		directBytes := int64(len(changed) * 5)
		if frame == 0 || directBytes >= monoSize {
			stats.directBytes += monoSize
		} else {
			stats.directBytes += directBytes
		}
		if output != nil {
			if err := writeFrame(output, frame, current[:], delta, copyFrame); err != nil {
				return stats, err
			}
		}
		if opts.inspectStart >= 0 && frame >= opts.inspectStart &&
			(opts.inspectEnd < opts.inspectStart || frame <= opts.inspectEnd) {
			fmt.Fprintf(
				os.Stderr,
				"frame=%d time=%.3fs changed=%d delta_bytes=%d copy=%t\n",
				frame,
				float64(frame)/30,
				len(changed),
				len(delta),
				copyFrame,
			)
		}
		previous = current

		if opts.progressFrames > 0 && (frame+1)%opts.progressFrames == 0 {
			fmt.Fprintf(os.Stderr, "processed %d frames\n", frame+1)
		}
	}
	if len(stats.changes) == 0 {
		return stats, errors.New("ffmpeg produced no frames")
	}
	return stats, nil
}

func packFrame(gray []byte, frame, jitter int) [monoSize]byte {
	var packed [monoSize]byte
	// This four-frame sequence moves the threshold gently around its static
	// Bayer value. Pure black and white remain pinned by the contrast stage.
	phase := [...]int{0, -jitter, 0, jitter}[frame&3]
	for y := 0; y < frameHeight; y++ {
		for x := 0; x < frameWidth; x++ {
			// Matrix values are 0..63. Centre each of the 64 thresholds in
			// its four-level-wide grayscale bucket.
			threshold := int(bayer8[y&7][x&7])*4 + 2 + phase
			if threshold < 0 {
				threshold = 0
			} else if threshold > 254 {
				threshold = 254
			}
			if int(gray[y*frameWidth+x]) > threshold {
				packed[y*monoStride+x/8] |= 1 << (7 - uint(x&7))
			}
		}
	}
	return packed
}

func changedOffsets(previous, current []byte) []int {
	changed := make([]int, 0, len(current)/8)
	for i := range current {
		if previous[i] != current[i] {
			changed = append(changed, i)
		}
	}
	return changed
}

func allOffsets() []int {
	offsets := make([]int, monoSize)
	for i := range offsets {
		offsets[i] = i
	}
	return offsets
}

func changedRowSpans(previous, current []byte) (bytes, rows int) {
	for y := 0; y < frameHeight; y++ {
		start := y * monoStride
		first := -1
		last := -1
		for x := 0; x < monoStride; x++ {
			offset := start + x
			if previous[offset] != current[offset] {
				if first < 0 {
					first = x
				}
				last = x
			}
		}
		if first >= 0 {
			bytes += last - first + 1
			rows++
		}
	}
	return bytes, rows
}

func countContiguousRuns(changed []int) int {
	if len(changed) == 0 {
		return 0
	}
	runs := 1
	for i := 1; i < len(changed); i++ {
		if changed[i] != changed[i-1]+1 {
			runs++
		}
	}
	return runs
}

func encodeBlockDelta(previous, current []byte) []byte {
	delta := make([]byte, deltaMapLen)
	for block := 0; block < deltaBlocks; block++ {
		start := block * deltaBlock
		var mask uint32
		for i := 0; i < deltaBlock; i++ {
			if previous[start+i] != current[start+i] {
				mask |= 1 << uint(i)
			}
		}
		if mask == 0 {
			continue
		}
		delta[block/8] |= 1 << uint(block&7)
		delta = append(delta, byte(mask), byte(mask>>8), byte(mask>>16), byte(mask>>24))
		for i := 0; i < deltaBlock; i++ {
			if mask&(1<<uint(i)) != 0 {
				delta = append(delta, current[start+i])
			}
		}
	}
	return delta
}

func applyBlockDelta(frame []byte, delta []byte) {
	cursor := deltaMapLen
	for block := 0; block < deltaBlocks; block++ {
		if delta[block/8]&(1<<uint(block&7)) == 0 {
			continue
		}
		mask := uint32(delta[cursor]) |
			uint32(delta[cursor+1])<<8 |
			uint32(delta[cursor+2])<<16 |
			uint32(delta[cursor+3])<<24
		cursor += 4
		for bit := 0; mask != 0; bit++ {
			if mask&1 != 0 {
				frame[block*deltaBlock+bit] = delta[cursor]
				cursor++
			}
			mask >>= 1
		}
	}
}

func writeHeader(w io.Writer, packageName string) error {
	_, err := fmt.Fprintf(w, `// Code generated by deltagen; DO NOT EDIT.

package %s

func applyVideoDelta(frame *[%d]byte, delta string) {
	const mapLen = %d
	cursor := mapLen
	for block := 0; block < %d; block++ {
		if delta[block/8]&(1<<uint(block&7)) == 0 {
			continue
		}
		mask := uint32(delta[cursor]) |
			uint32(delta[cursor+1])<<8 |
			uint32(delta[cursor+2])<<16 |
			uint32(delta[cursor+3])<<24
		cursor += 4
		for bit := 0; mask != 0; bit++ {
			if mask&1 != 0 {
				frame[block*%d+bit] = delta[cursor]
				cursor++
			}
			mask >>= 1
		}
	}
}

`, packageName, monoSize, deltaMapLen, deltaBlocks, deltaBlock)
	return err
}

func writeFrame(w io.Writer, frame int, current, delta []byte, full bool) error {
	if _, err := fmt.Fprintf(w, "func videoFrame%d(frame *[%d]byte) {\n", frame, monoSize); err != nil {
		return err
	}
	if full {
		if _, err := io.WriteString(w, "\tcopy(frame[:], "); err != nil {
			return err
		}
		if err := writeQuotedBytes(w, current); err != nil {
			return err
		}
		if _, err := io.WriteString(w, ")\n"); err != nil {
			return err
		}
	} else {
		if _, err := io.WriteString(w, "\tapplyVideoDelta(frame, "); err != nil {
			return err
		}
		if err := writeQuotedBytes(w, delta); err != nil {
			return err
		}
		if _, err := io.WriteString(w, ")\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}\n\n")
	return err
}

func writeQuotedBytes(w io.Writer, data []byte) error {
	if _, err := io.WriteString(w, "\""); err != nil {
		return err
	}
	for _, b := range data {
		if _, err := fmt.Fprintf(w, "\\x%02x", b); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\"")
	return err
}

func writeFooter(w io.Writer, frames int) error {
	if _, err := fmt.Fprintf(w, "var videoFrames = [%d]func(*[%d]byte){\n", frames, monoSize); err != nil {
		return err
	}
	for frame := 0; frame < frames; frame++ {
		if _, err := fmt.Fprintf(w, "\tvideoFrame%d,\n", frame); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}\n")
	return err
}

func printStatistics(stats statistics, copyThreshold int) {
	changes := append([]int(nil), stats.changes...)
	sort.Ints(changes)
	rowSpanBytes := append([]int(nil), stats.rowSpanBytes...)
	sort.Ints(rowSpanBytes)
	changedRows := append([]int(nil), stats.changedRows...)
	sort.Ints(changedRows)
	contiguousRuns := append([]int(nil), stats.contiguousRuns...)
	sort.Ints(contiguousRuns)
	frameCount := len(changes)
	totalChanges := int64(0)
	for _, changed := range changes {
		totalChanges += int64(changed)
	}
	percentile := func(p int) int {
		index := (frameCount - 1) * p / 100
		return changes[index]
	}
	ratio := float64(stats.encodedBytes) * 100 / float64(stats.unpackedBytes)
	rowSpanEstimate := stats.rowSpanPayload + stats.rowSpanCallCount*16
	runEstimate := stats.runPayload + stats.runCallCount*16

	fmt.Printf("frames=%d mono_bytes_per_frame=%d\n", frameCount, monoSize)
	fmt.Printf("changed_bytes min=%d p50=%d p90=%d p95=%d p99=%d max=%d mean=%.1f\n",
		changes[0], percentile(50), percentile(90), percentile(95), percentile(99),
		changes[frameCount-1], float64(totalChanges)/float64(frameCount))
	fmt.Printf("copy_threshold=%d delta_frames=%d full_frames=%d\n",
		copyThreshold, stats.sparseFrames, stats.fullFrames)
	fmt.Printf("block_bitmap_bytes=%d uncompressed_bytes=%d ratio=%.2f%%\n",
		stats.encodedBytes, stats.unpackedBytes, ratio)
	fmt.Printf("direct_assignment_estimate=%d ratio=%.2f%%\n",
		stats.directBytes, float64(stats.directBytes)*100/float64(stats.unpackedBytes))
	fmt.Printf("row_spans bytes_p50=%d bytes_p95=%d rows_p50=%d rows_p95=%d payload=%d calls=%d estimated_bytes=%d ratio=%.2f%%\n",
		percentileOf(rowSpanBytes, 50), percentileOf(rowSpanBytes, 95),
		percentileOf(changedRows, 50), percentileOf(changedRows, 95),
		stats.rowSpanPayload, stats.rowSpanCallCount, rowSpanEstimate,
		float64(rowSpanEstimate)*100/float64(stats.unpackedBytes))
	fmt.Printf("contiguous_runs p50=%d p95=%d payload=%d calls=%d estimated_bytes=%d ratio=%.2f%%\n",
		percentileOf(contiguousRuns, 50), percentileOf(contiguousRuns, 95),
		stats.runPayload, stats.runCallCount, runEstimate,
		float64(runEstimate)*100/float64(stats.unpackedBytes))
}

func percentileOf(sorted []int, p int) int {
	return sorted[(len(sorted)-1)*p/100]
}
