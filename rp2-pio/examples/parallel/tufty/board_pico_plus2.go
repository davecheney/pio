//go:build pico_plus2

package main

import "machine"

// Pimoroni Tufty 2350 LCD pin definitions.
// TinyGo 0.42.0 does not include a Tufty 2350 target; build for the
// underlying Pimoroni Pico Plus 2 module with -target=pico-plus2.
const (
	csPin  = machine.GPIO27 // LCD_CS
	dcPin  = machine.GPIO28 // LCD_RS
	wrPin  = machine.GPIO30 // LCD_WR
	rdPin  = machine.GPIO31 // LCD_RD
	db0Pin = machine.GPIO32 // LCD_DB0..DB7 = GPIO32..GPIO39
	blPin  = machine.GPIO26 // LCD_BACKLIGHT
)

// busBaud controls the PIO parallel bus clock rate driving WR.
//
// At the RP2350 default 150 MHz sysclk, piolib's 8-bit fractional divider
// produces a 15.006 MHz WR rate (66.64 ns write cycle), which remains within
// the ST7789 8080-II timing limit.
const busBaud = 15_000_000

func configureBacklight(pin machine.Pin, on bool) error {
	return configureBacklightPWM(machine.PWM5, pin, on)
}
