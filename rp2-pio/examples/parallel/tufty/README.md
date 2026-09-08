# Tufty parallel ST7789 example

This example drives the 320x240 ST7789 display on Pimoroni Tufty boards over
the 8-bit 8080-II parallel interface using RP2 PIO.

## Supported builds

Tufty 2040 has a TinyGo board target:

```sh
tinygo build -target=tufty2040 -o /tmp/tufty2040.uf2 .
```

TinyGo 0.42.0 does not include a Tufty 2350 board target. Build Tufty 2350
firmware for the underlying Pimoroni Pico Plus 2 module:

```sh
tinygo build -target=pico-plus2 -o /tmp/tufty2350.uf2 .
```

The Tufty 2350 build produces a UF2 and links correctly, but it will not drive
the panel at runtime until `piolib.NewParallel` grows RP2350B high-GPIO support.
Tufty 2350 places `LCD_DB0..LCD_DB7` on `GPIO32..GPIO39`; the current parallel
PIO helper uses a `uint32` pin mask and rejects PIO pin bases >= 32. RP2350B
needs `GPIOBASE=16` plus PIO-relative pin indexing to represent these pins.

## Board pin maps

Tufty 2040:

| Signal | GPIO |
| --- | --- |
| LCD_BACKLIGHT | GPIO2 |
| LCD_CS | GPIO10 |
| LCD_DC | GPIO11 |
| LCD_WR | GPIO12 |
| LCD_RD | GPIO13 |
| LCD_DB0..LCD_DB7 | GPIO14..GPIO21 |

Tufty 2350:

| Signal | GPIO |
| --- | --- |
| LCD_BACKLIGHT | GPIO26 |
| LCD_CS | GPIO27 |
| LCD_RS / LCD_DC | GPIO28 |
| LCD_WR | GPIO30 |
| LCD_RD | GPIO31 |
| LCD_DB0..LCD_DB7 | GPIO32..GPIO39 |

## Bus clock

The parallel PIO program has three instructions, so the PIO state machine runs
at `3 * busBaud`. With `busBaud = 15_000_000`, the RP2350 default 150 MHz
sysclk produces divider `3 + 85/256`, an actual WR rate of about 15.006 MHz,
and a write cycle of about 66.64 ns. This remains within the ST7789 8080-II
66 ns minimum write cycle.
