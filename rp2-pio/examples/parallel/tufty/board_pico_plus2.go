//go:build pico_plus2

package main

import (
	"machine"
	"time"
)

// Pimoroni Tufty 2350 LCD pin definitions.
// TinyGo 0.42.0 does not include a Tufty 2350 target; build for the
// underlying Pimoroni Pico Plus 2 module with -target=pico-plus2.
const (
	csPin    = machine.GPIO27 // LCD_CS
	dcPin    = machine.GPIO28 // LCD_RS
	wrPin    = machine.GPIO30 // LCD_WR
	rdPin    = machine.GPIO31 // LCD_RD
	db0Pin   = machine.GPIO32 // LCD_DB0..DB7 = GPIO32..GPIO39
	blPin    = machine.GPIO26 // LCD_BACKLIGHT
	powerPin = machine.GPIO41 // POWER_EN
)

var cuePins = [...]machine.Pin{
	machine.GPIO0, // CL0
	machine.GPIO1, // CL1
	machine.GPIO2, // CL2
	machine.GPIO3, // CL3
}

// busBaud controls the PIO parallel bus clock rate driving WR.
//
// The RP2350 default 150 MHz sysclk produces a 15.006 MHz WR rate
// (66.64 ns write cycle), within the ST7789 8080-II timing limit.
const busBaud = 15_000_000

func configureBoard() {
	powerPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
	powerPin.High()
	time.Sleep(500 * time.Millisecond)
}

func configureBacklight(pin machine.Pin, on bool) error {
	return configureBacklightPWM(machine.PWM5, pin, on)
}

func startVideoCue() {
	for _, pin := range cuePins {
		pin.Configure(machine.PinConfig{Mode: machine.PinOutput})
		pin.Low()
	}

	for i := 0; i < 5; i++ {
		cuePins[0].High()
		time.Sleep(500 * time.Millisecond)
		cuePins[0].Low()
		time.Sleep(500 * time.Millisecond)
	}

	for i := 0; i < len(cuePins)-1; i++ {
		cuePins[i].High()
		time.Sleep(time.Second)
	}
	cuePins[len(cuePins)-1].High()
}

func finishVideoCue() {
	for _, pin := range cuePins {
		pin.Low()
	}
}
