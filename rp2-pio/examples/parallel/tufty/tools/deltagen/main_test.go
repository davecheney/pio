package main

import "testing"

func TestPackFrameBitOrder(t *testing.T) {
	gray := make([]byte, frameWidth*frameHeight)
	for x := 0; x < 8; x += 2 {
		gray[x] = 255
	}

	packed := packFrame(gray, 0, 0)
	if got, want := packed[0], byte(0xaa); got != want {
		t.Fatalf("first packed byte = %#02x, want %#02x", got, want)
	}
	for i, b := range packed[1:] {
		if b != 0 {
			t.Fatalf("packed byte %d = %#02x, want zero", i+1, b)
		}
	}
}

func TestChangedOffsets(t *testing.T) {
	previous := []byte{0, 1, 2, 3, 4}
	current := []byte{0, 9, 2, 8, 4}

	got := changedOffsets(previous, current)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("changedOffsets() = %v, want [1 3]", got)
	}
}

func TestChangedRowSpans(t *testing.T) {
	previous := make([]byte, monoSize)
	current := make([]byte, monoSize)
	current[2] = 1
	current[6] = 1
	current[monoStride+10] = 1

	bytes, rows := changedRowSpans(previous, current)
	if bytes != 6 || rows != 2 {
		t.Fatalf("changedRowSpans() = (%d, %d), want (6, 2)", bytes, rows)
	}
}

func TestCountContiguousRuns(t *testing.T) {
	if got := countContiguousRuns([]int{1, 2, 4, 8, 9, 10}); got != 3 {
		t.Fatalf("countContiguousRuns() = %d, want 3", got)
	}
}

func TestBlockDeltaRoundTrip(t *testing.T) {
	previous := make([]byte, monoSize)
	current := make([]byte, monoSize)
	current[0] = 1
	current[31] = 2
	current[32] = 3
	current[monoSize-1] = 4

	delta := encodeBlockDelta(previous, current)
	applyBlockDelta(previous, delta)
	for i := range current {
		if previous[i] != current[i] {
			t.Fatalf("decoded byte %d = %d, want %d", i, previous[i], current[i])
		}
	}
}
