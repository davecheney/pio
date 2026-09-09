package main

import "testing"

func TestExpandMono(t *testing.T) {
	mono := []byte{0x81, 0x7e}
	rgb565 := make([]byte, len(mono)*16)

	expandMono(rgb565, mono)

	for pixel := 0; pixel < 16; pixel++ {
		want := byte(0)
		if pixel == 0 || pixel == 7 || (pixel >= 9 && pixel <= 14) {
			want = 0xff
		}
		if got := rgb565[pixel*2]; got != want {
			t.Fatalf("pixel %d high byte = %#02x, want %#02x", pixel, got, want)
		}
		if got := rgb565[pixel*2+1]; got != want {
			t.Fatalf("pixel %d low byte = %#02x, want %#02x", pixel, got, want)
		}
	}
}
