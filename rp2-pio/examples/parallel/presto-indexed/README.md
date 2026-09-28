# Presto indexed SRAM framebuffer prototype

This example keeps exactly **one** 480 x 480 8-bit indexed framebuffer in
internal SRAM and applies a fixed palette on the fly, expanding each scanline
into packed RGB565 immediately before DMA scanout. It uses no PSRAM, no double
buffering and no framebuffer writes during scanout: the goal is solely to
prove stable per-scanline palette expansion. It builds with the ordinary
`pico-plus2` target and does not replace the plasma or Dali examples;
`presto.Run` and its streaming `Renderer` keep their original behavior.

**Status: checkpoint, not a production-stable solution.**

## Hardware results

Tested on a physical Pimoroni Presto, with identical source built for
`pico-plus2` using each TinyGo scheduler:

| Scheduler | Result | Stopped at | Expand max |
| --- | --- | --- | ---: |
| `tasks` | Visually stable, then data SM TXSTALL | frame 422, row 134 | 52 us |
| `none` | Data SM TXSTALL | frame 1516, row 44 | 55 us |
| `cores` | Visually stable, then data SM TXSTALL | frame 598, row 211 | 44 us |

In every run the timing SM had no TXSTALL, and frame periods were
21,113-21,116 us. The `tasks` fault reported `FDEBUG 0x01000000`, which is
TXSTALL on PIO1 SM0, the data SM. In that run the average expansion was about
22.7 us per row (202,273 rows, 4,594,394 us).

This shows that per-scanline palette expansion from a single SRAM indexed
framebuffer produces a correct, stable image, and that the DMA-fed timing
path holds frame rate. However, rare latency outliers still exceed the row
deadline and starve the data SM within seconds, regardless of scheduler. The
deadline is about 46-47 us: the 42.4 us line plus roughly 5 us of FIFO and
horizontal-blank slack. The outlier cause is not yet confirmed; interrupts
(such as USB) and flash-cache misses during expansion are the leading
suspects.

## Memory and bandwidth

| Storage | Bytes | Location |
| --- | ---: | --- |
| One 480 x 480 indexed frame | 230,400 (225 KiB) | Internal SRAM |
| Two packed RGB565 scanlines | 1,920 | Internal SRAM |
| 256 prepacked palette entries | 1,024 | Internal SRAM |
| 498 x 4 timing words | 7,968 | Internal SRAM |

The linked ELF places `main.frame` at `0x200039f0`; `_heap_start = 0x2003c6f8`,
`_heap_end = 0x20080000`, leaving **276,744 bytes of initial heap**. An earlier
two-frame build left only 46,344 bytes and produced no serial output or display
on hardware; its failure was not root-caused.

The display setup and PIO programs are shared with the existing examples:
150 MHz CPU, 25 MHz timing SM, 50 MHz data SM, 12.5 MHz dot clock. A line is
530 dots, or **42.4 us / 6,360 CPU cycles**; a frame is 498 lines, 21.1152 ms
(about 47.36 Hz). Scanout reads 480 index bytes per active row (about
10.91 MB/s averaged over a frame), writes 960 bytes into a line buffer and DMA
reads those 960 bytes, all in internal SRAM.

## Scanout

`IndexedFrame.Expand` reads pairs of byte indices and combines two prepacked
palette halfwords into each of the 240 SRAM words. The high halfword is the
first pixel: the existing data PIO reverses ISR before driving the pins.
There is no per-pixel channel conversion and no array-by-value range loop.
The palette stays immutable in SRAM for the entire run.

DMA0 transmits one line while the CPU expands the following row into the other
line buffer. DMA completion releases the source buffer; it does **not** mean
the pixels have left the PIO FIFO. The CPU rearms DMA0 after completion, not
on a software delay or an assumed FIFO depth.

DMA1 continuously feeds the timing SM from the SRAM timing table. At table
end it chains to DMA2, whose single fixed-address write to DMA1's
`AL3_READ_ADDR_TRIG` reloads the table's starting address and transfer count.
DMA2's self `CHAIN_TO` disables further chaining. Both timing channels are
high priority. This avoids CPU-fed timing starvation while expanding/drawing
rows. The RP2350 DMA trigger/count-reload configuration still needs hardware
verification.

PIO IRQ4 grants each visible line to the data SM. A non-waiting timing-table
IRQ0 marks the first blank line **after** the last visible row. The CPU waits
for that hardware boundary, clears it, expands row zero and primes its DMA in
vertical blank. The framebuffer is never written while scanout is active.

DMA channels 0-2 are reserved; do not combine this runner with other DMA
users. Runtime checks verify internal-SRAM placement of the frame, line
buffers, timing table and palette, and the required CPU clock.

## Demonstrator and diagnostics

The static image has eight color bars (white, yellow, cyan, green, magenta,
red, blue, black), a two-pixel white border and a stationary orange 64 x 64
square. It is fully drawn before the display is initialized.

Serial behavior (USB CDC):

1. `indexed: boot marker N ...` twelve times, 250 ms apart, from the start of
   `main`, before any large initialization.
2. `indexed: palette ready...`, `indexed: static image ready`.
3. Stage lines with a microsecond timestamp: `RunIndexed entered`,
   `preconditions ok; initDisplay`, `display initialized; configuring PIO`,
   `priming FIFOs; starting scanout after USB quiet period (serial silent until
   stop)`, followed by a 200 ms busy-wait so queued USB output drains before
   the first line deadlines.
4. No output while scanout is active.
5. After 3,000 frames (about 63 s) the backlight, PIO and DMA are stopped and
   one `indexed: complete frame 2999 row 479 ...` line is printed. A true
   TXSTALL, DMA bus error or 25 ms handshake timeout stops earlier with that
   reason instead. The display going dark at the end is intentional.

A pre-scanout configuration error prints `indexed: FATAL ...` every second
with the display off.

The final line includes FDEBUG, PIO IRQ/PCs, all three DMA control words, the
timing DMA read address, expanded-row count (1,437,000 on a full run),
expansion total and max microseconds, and min/max frame periods (expected
about 21,115 us). Row zero is expanded in vertical blank and excluded.
TIMER0's 1 us quantization is significant at this scale.

The optimized Thumb loop has 13 instructions per pixel pair, an ideal
instruction-only floor of about 20.8 us per row. The measured average on
hardware was about 22.7 us, with worst cases of 44-55 us (see
[Hardware results](#hardware-results)).

## Build and validation

```sh
OUT=/absolute/path/to/artifacts
tinygo build -target=pico-plus2 -scheduler=tasks -size=short \
  -o "$OUT/presto-indexed.uf2" ./rp2-pio/examples/parallel/presto-indexed
tinygo build -target=pico-plus2 -scheduler=tasks \
  -o "$OUT/presto-indexed.elf" ./rp2-pio/examples/parallel/presto-indexed
objdump -t "$OUT/presto-indexed.elf" | grep -E 'main.frame|_heap_(start|end)'
go test -race ./rp2-pio/examples/parallel/internal/presto ./rp2-pio/examples/parallel/presto-indexed
go vet ./rp2-pio/examples/parallel/internal/presto ./rp2-pio/examples/parallel/presto-indexed
git diff --check
```

Tests cover exact sizes, all palette indices and adjacent pixel order, every
row and invalid bounds, row-slice capacity, timing-table geometry/grants, and
full-frame pattern smoke tests. They do not emulate PIO or DMA.
