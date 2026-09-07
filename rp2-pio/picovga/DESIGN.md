# picovga design

Notes on turning the working demos into a library. Written after five demos
were built and confirmed on hardware; the shape below is what those demos
argued for, not what was planned in advance.

## The hardware constraint that decides everything

The RP2040 VGA reference design, which the Pimoroni Pico VGA Demo Base
implements, gives each colour channel five resistor ladder bits:

    R  GP0-GP4      G  GP6-GP10      B  GP11-GP15
    GP5 is the SD card clock, not video
    HSYNC GP16      VSYNC GP17

The channels sit six pins apart. A PIO `out pins` writes a run of consecutive
pins, and no eight pin window reaches all three channels, so **a pixel costs
sixteen bits on the wire**. That single fact drives most of what follows.

It also explains why PicoVGA's design does not transfer. Its own board is an
eight bit 3-3-2 ladder, so its framebuffer *is* the wire format and DMA can
stream it untouched. Ours is not: we either store sixteen bits per pixel, which
is the wire format at twice the memory, or store eight and convert, which costs
CPU on every scanline.

## The core abstraction is a row

Everything the display needs is a row of pixels ready for DMA:

    contiguous []uint16, length = framebuffer width
    RGB555 values, byte swapped in advance
    even length, so the 32 bit transfers stay aligned
    transfer count = len/2, two pixels to a word

Where the row comes from is not the display's business. It may be

  - a slice of a 16 bit framebuffer, handed over with no copy at all
  - a line expanded from 8 bit palette indices into a scratch buffer
  - a row generated on the spot: text from a character map, a tile map,
    sprites composited per scanline, or anything procedural

The third case is the one worth protecting. A text mode at 640x480 needs a
character map and a font, about 2.4KB, where a framebuffer needs 150KB.

### Scaling is not symmetric

Horizontal scaling is free. The output routine holds each pixel on the pins for
`ClocksPerPixel * HScale` cycles using the instruction delay field, so one
framebuffer pixel is one FIFO word however far it is stretched.

Vertical scaling is not. The same row is sent again for each scanline it covers,
because control words have to be pushed between scanlines and a transfer cannot
span them. So **DMA traffic is framebuffer width times output height**, which is
why 800x600 at 2x moves 29MB/s and 640x480 at 4x moves 9.2MB/s.

## The client owns the loop

A scanline is `sync, back porch, output command, pixels, front porch`, all
through one FIFO, and their order is the scanline. DMA carries only the pixels,
so something must push the four control words around them. That is a loop, and
it occupies a core.

PicoVGA avoids this with two chained DMA channels: a control block channel walks
a list of (count, address) pairs into the data channel's alias registers, and a
whole frame streams with no CPU at all. It was considered and rejected here:

  - it needs DMA interrupts, which TinyGo can reach but which nothing in this
    repository wires today, and which would have to coexist with the cores
    scheduler
  - it needs a per-frame control list rebuilt on every change
  - **it forces a framebuffer.** The data must exist before the frame starts, so
    rows cannot be generated as they are sent

The last point is decisive. Being in the loop is not the price of a simpler
implementation, it is the capability, and it is the one thing this design has
that the original does not.

The eight bit path settles the argument by itself: converting indices to colours
needs CPU on every scanline regardless, so the loop cannot be escaped by taking
the memory saving. The two supposed costs cancel.

## Division of labour

The library owns everything that is derived or subtle:

  - validating a (mode, scale, format) combination
  - deriving clocks per framebuffer pixel and rejecting what will not encode
  - assembling and loading the base program at its required origin
  - claiming a state machine, configuring pins, pin directions and FIFO join
  - sync polarity, including inverting the pad for a negative mode
  - the clock divider
  - claiming a DMA channel and its control word
  - the order of words within a scanline, and vertical sync

The client owns the loop and the content:

```go
d, err := picovga.New(picovga.PimoroniVGA, picovga.Mode640x480, picovga.Scale{H: 2, V: 2})
fb := picovga.NewFramebuffer16(d.Width(), d.Height())
go draw(fb)

d.Start()
for {
    for n := 0; n < d.Scanlines(); n++ {
        var px []uint16
        if row, ok := d.Row(n); ok {
            px = fb.Line(row)
        }
        d.Scanline(n, px) // nil blanks the line
    }
}
```

`d.Width()` and `d.Height()` are derived from the mode and scale, so a
framebuffer cannot disagree with the display it is drawn for. That is the class
of mistake the per-demo build tagged size constants kept producing.

An eight bit source needs one extension, not a redesign: splitting `Scanline`
into begin and wait, so the next row can be expanded while DMA sends this one.
Worth leaving room for; not worth building until that mode is wanted.

## Configuration space

Only four things are chosen: mode, horizontal scale, vertical scale and pixel
format. Everything else is derived. Uniform scales that are legal:

    mode      scale  cpp  framebuffer   8bpp   16bpp     DMA
    800x600     2     6     400x300     117K    234K   29.0M
    800x600     4    12     200x150      29K     59K   14.5M
    800x600     5    15     160x120      19K     38K   11.6M
    640x480     2     4     320x240      75K    150K   18.4M
    640x480     4     8     160x120      19K     38K    9.2M
    640x480     5    10     128x96       12K     24K    7.4M
    640x480     8    16      80x60        5K      9K    4.6M

Three constraints produce that list, and the library should enforce all three at
construction rather than letting them appear as a display that will not lock:

  - **divisibility**: the visible area must divide by the scale. This is why a
    3x scale does not exist in either mode: neither 800 nor 640 divides by three
  - **cpp range**: `ClocksPerPixel * HScale` must be 2 to 17, the delay field
    being four bits once a side-set bit is taken. This is what stops 800x600 at
    6x and beyond, and what lets 640x480 reach 8x
  - **memory**: measured, not calculated. An RP2040 runs a 156KB program and
    fails at 198KB, well short of what the linker reports as free

Exactly one combination is RP2350 only: 800x600 at 2x in sixteen bits, 234KB.
The choice of chip is a memory ceiling, not a configuration axis.

A fourth constraint applies only to the palettised format: the expansion must
fit inside the DMA window for a visible line. At 400 pixels wide on an RP2040 it
does not, unless a row is expanded once rather than once per scanline. Reuse
should therefore be the default; expanding per scanline was a demo stress test.

## What consolidation removes

About 370 lines of duplicated infrastructure, and it has begun to drift:

    dma.go                  3 copies in demos, 1 divergent in the library
    640x480 timings         2 copies, one in the library and one in a demo
    sync polarity           2 copies, already differing
    the display loop        4 hand rolled, 3 of them structurally identical

The demo renderers are not duplication and are out of scope.

## Deferred, in order

**Eight bit palettised.** Halves the framebuffer and gives 256 freely chosen
colours out of 32768, which is better than RGB332 at the same size. Costs an
expansion pass per row, which fits the DMA window at 320 pixels wide and needs
row reuse at 400.

**RGB332 and the 3-3-2 board.** Wanted only if such a board is ever built. Note
that about 29% of the current library targets it: the eight bit base program and
all five layer programs, none of which any demo uses.

**Layers.** Parked with the board above, and harder than it looks. The base
program could be widened to sixteen bits because its dark command had a
nineteen bit counter to raid. The layer control words have no such slack: the
key layer packs an eleven bit pixel count, an eight bit key colour and a
thirteen bit delay into thirty two bits, and a sixteen bit colour would leave
five bits of delay where hundreds of cycles are needed. Layers on this hardware
are a redesign, not a port.

## Defects to fix while consolidating

  - **Vertical sync leads the beam.** It is set from where the CPU is in the
    frame, but the CPU runs ahead by whatever is buffered in the FIFO, up to
    about four scanlines. The picture sits low by that much and the bottom rows
    fall off the screen. Today it affects one demo; a shared driver would give
    it to everyone, which is the argument for fixing it during consolidation
    rather than after
  - **`Framebuffer` is documented as holding RGB332 colours** but its only user
    stores palette indices in it. The storage is general, the interpretation is
    not; they should be separated
  - **`dma.go` signatures have diverged.** The row contract says `[]uint16`:
    the length carries the transfer count and keeps alignment and lifetime
    visible

## A rule learned the hard way

Anything the tests must agree with belongs in a file without build tags. Host
tests cannot see a file built only for the target, and three separate faults came
from that: an iteration limit that tests measured at one value and the demo used
at another, letting four unusable destinations through; and twice a signature
change that passed every test and failed only at `tinygo build`, because the
file that calls it is tagged out of the host build.
