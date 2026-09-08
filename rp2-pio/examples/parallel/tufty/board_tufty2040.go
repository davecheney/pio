//go:build tufty2040

package main

import "machine"

// Pimoroni Tufty 2040 LCD pin definitions.
// https://tinygo.org/docs/reference/microcontrollers/tufty2040/
const (
	csPin  = machine.GPIO10 // LCD_CS
	dcPin  = machine.GPIO11 // LCD_DC
	wrPin  = machine.GPIO12 // LCD_WR
	rdPin  = machine.GPIO13 // LCD_RD
	db0Pin = machine.GPIO14 // LCD_DB0..DB7 = GPIO14..GPIO21
	blPin  = machine.GPIO2  // LCD_BACKLIGHT
)

func configureBacklight(pin machine.Pin, on bool) error {
	return configureBacklightPWM(machine.PWM1, pin, on)
}
