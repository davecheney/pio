# Pimoroni Presto Dali Clock example

This example drives the Pimoroni Presto's 480 x 480 ST7701S display as a
classic Dali Clock: a landscape-style, horizontally centered, morphing
24-hour `HH:MM:SS` display.

It intentionally does not replace the neighbouring `../presto` plasma demo.
Both examples share the same hardware-verified Presto scanout package in
`../internal/presto`.

## Original source and license

The digit morphology and font data are derived from Jamie Zawinski's
XDaliClock 2.48:

<https://www.jwz.org/xdaliclock/xdaliclock-2.48.tar.gz>

Original notice retained as required:

```text
xdaliclock - a melting digital clock
Copyright © 1991-2022 Jamie Zawinski <jwz@jwz.org>

Permission to use, copy, modify, distribute, and sell this software and its
documentation for any purpose is hereby granted without fee, provided that
the above copyright notice appear in all copies and that both that
copyright notice and this permission notice appear in supporting
documentation.  No representations are made about the suitability of this
software for any purpose.  It is provided "as is" without express or
implied warranty.
```

`font_d.go` is generated from upstream `font/{zero,one,two,three,four,five,
six,seven,eight,nine,colon}D.xbm` by scanning each XBM row in source bit order
and storing compact filled `[left,right)` endpoint segments. No bitmap or media
assets are committed. Font D's six `90x128` digits plus two `46x128` colons
form a `632x128` unscaled `HH:MM:SS` layout; applying a uniform `3/4` scale
produces exactly `474x96`, centered at `x=3`, `y=192`.

## Clock behavior

The display starts at `09:41:00` on every boot and then free-runs from TinyGo's
monotonic elapsed time. The Wi‑Fi/NTP branch installs a one-time sync step using
`cyw43439` and `seqs` so a real NTP server can seed the clock before the renderer
starts.

`wifi.go` embeds
`rp2-pio/examples/parallel/presto-daliclock/wifi_creds.json` at build time via
`go:embed`, so the file must exist and is committed with placeholder values
(`YOUR_WIFI_SSID` / `YOUR_WIFI_PASSWORD`). With placeholders in place,
`loadWiFiConfig` rejects the config and the example falls back to the fixed
`09:41:00` start with no network attempt, unchanged from the base behavior.

To sync from a real NTP server, edit the file locally with a real
SSID/password, a reachable IPv4 NTP server, and a fixed `utcOffsetMinutes`
(see below), then run
`git update-index --skip-worktree rp2-pio/examples/parallel/presto-daliclock/wifi_creds.json`
so your local edit is not accidentally committed or pushed. (Undo with
`--no-skip-worktree` if you need to commit an intentional change to the
template.) With valid credentials, the example joins Wi‑Fi, obtains a DHCP
lease, resolves the local gateway's hardware address via ARP (required since
the NTP server is off-LAN), and queries the configured NTP server before
running the Dali clock.

NTP always returns UTC. The device has no way to sense timezone or DST, so
`wifi_creds.json` has a `utcOffsetMinutes` field (e.g. `600` for UTC+10,
`-300` for UTC-5) that is added to the synced time before it seeds the clock.
It defaults to `0` (UTC) if omitted.

Each glyph row follows XDaliClock's scanline-segment model. A row is represented
as filled `[left,right)` segments; during each second, matching segment endpoints
interpolate linearly from the current digit to the next digit. The interpolation
position is multiplied by `1.2` and clamped to the end position, reproducing the
original roughly 100 ms end-of-cycle linger.

Foreground and background colours cycle smoothly in Dali Clock style. Colours
are computed once per frame and prepacked to the display's RGB565 bus format,
not recomputed per pixel.

## Rendering architecture

The shared `internal/presto` package owns:

- ST7701S command initialization.
- Pimoroni's PIO timing and RGB565 data programs.
- The verified clock chain: 25 MHz timing state machine, 50 MHz data state
  machine, 12.5 MHz dot clock, and about 47.3 Hz frame rate at the default
  150 MHz RP2350 clock.
- RGB565 channel packing for the Presto panel wiring.
- DMA scanout and two ping-ponged scanline buffers.

The Dali renderer receives one active scanline at a time. It fills the line with
the prepacked background word, then draws at most two prepacked foreground
segments for each visible glyph row. This keeps the work within the roughly
42 us line budget without a framebuffer.

The FIFO starvation fix from the plasma demo is preserved in the shared package:
the scanout loop never waits for the DMA transfer it has just started. Rendering
overlaps the previous line's DMA, and the DMA wait happens only when starting
the next line.

RAM use remains small: two `480`-pixel RGB565 scanlines are stored as `240`
packed `uint32` words each, plus the compact font endpoint table and renderer
state. A full 480 x 480 framebuffer is not allocated.

## Build and flash

TinyGo 0.42.0 does not include a dedicated `presto` target. Use
`-target pico-plus2`, which selects the RP2350B build tags used by this example.

```sh
tinygo build -target pico-plus2 -size short -o build/presto-daliclock.uf2 ./rp2-pio/examples/parallel/presto-daliclock
```

Flash only when a Presto is attached in BOOTSEL mode and hardware flashing has
been explicitly authorized:

```sh
tinygo flash -target pico-plus2 ./rp2-pio/examples/parallel/presto-daliclock
```

## Validation

Host-side tests cover the compact font table, exact `474x96` centered layout,
endpoint interpolation and linger, fixed boot epoch, monotonic conversion, and
rollovers such as `09:41:59 -> 09:42:00` and `23:59:59 -> 00:00:00`.

Software validation for this change should include:

```sh
gofmt -w ./rp2-pio/examples/parallel/internal/presto ./rp2-pio/examples/parallel/presto ./rp2-pio/examples/parallel/presto-daliclock
go test ./rp2-pio/examples/parallel/presto-daliclock
go test ./rp2-pio
tinygo build -target pico-plus2 -size short -o build/presto.uf2 ./rp2-pio/examples/parallel/presto
tinygo build -target pico-plus2 -size short -o build/presto-daliclock.uf2 ./rp2-pio/examples/parallel/presto-daliclock
git diff --check
```

Hardware verification is intentionally not claimed until the example has been
flashed and visually inspected on a physical Pimoroni Presto.
