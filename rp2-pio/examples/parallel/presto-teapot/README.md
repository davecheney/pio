# presto-teapot

A rotating Utah teapot wireframe on the Pimoroni Presto (RP2350B, 480x480
ST7701S). It uses the single 8-bit indexed SRAM framebuffer and scanline palette
expansion from `presto-indexed`, and splits the work across both cores with
`-scheduler=cores`.

## Architecture

| Goroutine | Core (expected) | Work |
| --- | --- | --- |
| `main.main` | one core, exclusively | `presto.RunIndexed`: timing DMA/PIO sync, per-row palette expansion into two ping-ponged RGB565 SRAM lines, data DMA, fault detection |
| `render` (the only other goroutine) | the other core | fixed-point 3-axis rotate/perspective of 755 vertices, then at each frame boundary draw the new wireframe and erase only the old pixels it did not reclaim, directly in the framebuffer |

- There is one 230,400 B framebuffer in internal SRAM: no PSRAM and no double
  buffering.
- The renderer writes into the framebuffer while it is being scanned.
  - This is a deliberate, unsynchronized race. Tearing and partially drawn
    edges are accepted.
  - Byte stores are atomic on the bus, so the scanout sees either the old or
    the new index. It never sees a corrupt palette index.
- The renderer never shows an erased teapot (v7). Palette indices
  `colorLineA` (1) and `colorLineB` (3) both map to the same cyan RGB565, and
  frames alternate between them:
  1. Project the new vertices into the inactive point set.
  2. Draw every new edge with this generation's index. Pixels shared with the
     previous wireframe are overwritten and stay lit.
  3. Walk the previous edges and set a pixel to background only if it still
     holds the previous generation's index.
  4. Swap point sets and generation.
  - The screen can briefly show the union of both wireframes, never a gap.
  - There are no masks and no second framebuffer. Memory traffic is close to
    v5: one draw pass plus one read-compare pass over the old edges.
  - Lines clip inside the 2 px border, so the border is never written.
- The renderer is paced by vertical sync.
  - `RunIndexed` stores `presto.FrameCount` (an `atomic.Uint32`, number of
    completed frames) at each IRQ0 frame boundary, which is the first
    vertical-blank line.
  - The renderer waits with `presto.WaitFrame` for the count to change, then
    does exactly one update, starting immediately after the boundary.
  - At most one update is made per display frame. A counter gap means the
    update overran a frame; gaps are summed into "display frames skipped".
  - Vertical blank is only 18 lines (~760 µs), so an update runs into the
    visible area. With draw-first ordering, the beam sees old edges, new
    edges or both, never a missing teapot.
- The renderer spins and never sleeps or yields, so under the `cores`
  scheduler it keeps its core. `main.main` busy-waits for the renderer to
  report its core, prints both cores, and then enters `RunIndexed` without
  yielding. Nothing prints during scanout.
- Neither loop allocates, so the GC never stops the scanline core.
  `TestStepDoesNotAllocate` checks the renderer.
- Rotation is a full 360° about all three axes, derived from the completed
  frame count (`(FrameCount - first) × 21,115 µs`), so it is deterministic and
  independent of renderer speed:
  - yaw (vertical axis) one revolution per 6 s;
  - pitch (horizontal axis) one revolution per 9 s, reversed;
  - roll (view axis) one revolution per 13 s.
  - The periods are coprime in 0.5 s units, so the combined orientation
    repeats only every 234 s. The composite is `Ry·Rx·Rz`.
- The math is Q14 fixed point (the RP2350 TinyGo target is soft-float), with
  focal length 660 px and camera distance 10 model units. That is 150% of the
  hardware-verified v3 size (440 px), about the same center.
  - `projectVertex` guards the near plane.
  - `drawLine` is Bresenham and clips every pixel to the area inside the 2 px
    border.
  - At 150% scale, parts of the teapot can clip at some angles; this is
    accepted. The orange border is never drawn over or erased.

### Startup and contention

Hardware history:

- **v1:** no serial output and no backlight. This was indistinguishable from
  lost output, because TinyGo USB CDC drops all prints until the host asserts
  DTR.
- **v2:** added DTR-wait breadcrumbs and reverted `.ramfuncs`/the SRAM mesh.
  - It proved boot, display init and core placement all work: main on core 0,
    renderer on core 1.
  - Scanout then stopped immediately with a data-SM `TXSTALL` at frame 0,
    row 1 (expand max 41 µs). The renderer was drawing throughout startup.

- **v3:** gated the renderer until scanout was established and restored the
  SRAM placements below.
  - Visible success on hardware (`i can see the teapot`) at the 440 px focal
    length, with the renderer drawing unpaced.
  - It completed the full 6000-frame run with stable scanout and no underflow:

    ```text
    indexed: complete frame 5999 row 479 FDEBUG 0 IRQ 0 PC data/timing 25 21 DMA ctrl data/timing/reload 1048601 68304923 8273931 timing read 536894264 abort pending 0 rows 2874000 expand total us 69744197 expand max us 38 frame min/max us 21114 21117
    teapot: renderer core 1 frames drawn 27758
    ```

  - Expansion averaged 24.3 µs per row (max 38 µs, inside the ~42 µs row).
  - The renderer averaged 4.63 redraws per display frame, about 4.6 ms per
    erase+redraw. Several updates per scanned frame is the flicker that v4's
    vsync pacing removes.
  - A ~4.6 ms update is longer than the ~760 µs vertical blank, so a paced
    update still overlaps roughly the top 100 rows of the next frame. Tearing
    is confined there and accepted.

- **v4:** 150% scale and vsync pacing (one erase+redraw per frame).
  - First boot: `TXSTALL frame 0 row 1 ... rows 2 expand total us 61 expand
    max us 37`, renderer `frames drawn 0 ... scanned 0`. The renderer had
    not drawn anything, so renderer contention was not the cause.
  - Reflash of the identical UF2: success, visibly stable:

    ```text
    indexed: complete frame 5999 row 479 FDEBUG 0 ... rows 2874000 expand total us 69233459 expand max us 40 frame min/max us 21114 21116
    teapot: renderer core 1 frames drawn 5999 display frames skipped 0 scanned 6000
    ```

  - The display then went dark. That was the intentional 6000-frame limit
    (`RunIndexed` turns off the display and backlight on completion), not a
    crash.
  - So vsync pacing works in steady state, but startup was nondeterministic.

v5:

- **Startup fix.** The frame-0 row-1 stall is the first tight deadline: row 0
  has ~551 µs of vertical-blank slack, but after each line DMA finishes, main
  has only ~5 µs (8-word FIFO + HBLANK) to start the next.
  - With the renderer idle, the remaining core-0 contender was the USB
    interrupt. It transmits the ~1 KB of queued breadcrumbs/stage prints, and
    the last print was issued immediately before enabling the SMs.
  - `RunIndexed` now prints its final stage line *before* priming, then
    busy-waits a fixed 200 ms (`usbQuietUS`, no yield, no print) so the USB
    queue drains. Only then does it expand line 0, start DMA, fill the FIFOs
    and enable data then timing SMs, exactly as before.
  - This is an evidence-based hypothesis, not a proven root cause. There are
    no retries, and `TXSTALL` is neither masked nor cleared after enable; a
    genuine stall on any frame, including frame 0, still stops and reports.
- **Warmup gate.** The renderer waits for `warmupFrames` (4) completed static
  frames (background and border) before its first update, then starts at a
  vertical-blank boundary.
- **3-axis rotation** as described above.
- **Runs indefinitely.** `frameLimit` is 0 by default; the pump stops and
  reports only on a fault (TXSTALL, DMA error, timeout). A non-zero value is
  available for bounded debug runs.
- Hardware: full 3-axis motion and 150% size looked good, but a black
  horizontal band appeared about 1/16 to 3/8 down the screen. The v5 update
  erased all old edges and then drew all new edges, taking 4.6 ms or more,
  while vertical blank is ~760 µs. The beam scanned rows between erase and
  redraw, showing a transient all-background teapot.

v6 (1-bit mask diff):

- Two 28,800 B 1-bit masks: rasterize the next frame into the hidden mask
  between boundaries, then at the boundary XOR the masks and write only
  changed pixels.
- Self-test on core 0 before scanout: prepare 5,982 µs, apply 1,743 µs,
  13,805 pixels.
- Hardware failed with a genuine data-SM underflow:

  ```text
  indexed: TXSTALL frame 60 row 25 FDEBUG 16777216 ... rows 28766 expand total us 693277 expand max us 26 frame min/max us 21113 21116
  teapot: renderer core 1 frames drawn 56 display frames skipped 0 scanned 60 apply max us 1840 prepare max us 5597
  ```

- Expansion itself stayed fast (max 26 µs). The extra SRAM traffic from
  clearing and rasterizing 28.8 KB masks every frame, on top of the
  framebuffer writes, most likely contended with the scanline core and its DMA enough to
  miss a ~5 µs line deadline. The masks were removed.

v7 (current):

- Generation-index update, as described above. There is exactly one 8-bit
  indexed framebuffer and no masks.
- 150% scale, 3-axis rotation, 4-frame warmup, indefinite run, the startup
  USB quiet period, once-per-vsync pacing and all scanout diagnostics are
  unchanged.
- Before starting the renderer, `main` self-tests on core 0 (B07b–B07d). It
  times a first update and a steady-state draw+erase update, then erases and
  checks that only background and border remain.
- The renderer records its worst update time, printed only in the fault stop
  report as `step max us`.
- Hardware telemetry:
  - self-test first step 4,974 µs, steady-state draw+erase 9,607 µs, and 0 lit
    pixels after erase;
  - main/scanout on core 0 and the renderer on core 1;
  - startup passed, and there was no serial fault after a 90-second continuous
    soak.
  - A 9.6 ms update fits inside the 21.1 ms frame period, so it still runs
    once per vsync, but it extends well past vertical blank into the visible
    area. Draw-first ordering means that region shows old edges, new edges or
    both.
- **Visual verification is pending.** The user could not observe the display
  during this run, so it is not yet confirmed on hardware that the v5 black
  band is gone. Algorithmically, v7 has no all-black intermediate state, and
  the host tests cover that.
- `(*presto.IndexedFrame).Expand` is `//go:section .ramfuncs`. TinyGo's
  `arm.ld` copies it with `.data`, so it executes from SRAM.
- `main` copies the mesh tables into SRAM before starting the renderer, so the
  renderer's steady-state reads do not touch XIP.
- The sin table is a generated constant, so there is no runtime
  `init`.
- `main.main` blinks the backlight at 4 Hz while waiting up to 30 s for DTR,
  then prints the B01–B13 breadcrumbs.
- Interrupts are not masked, and real underflows are neither hidden nor
  cleared.

## Model provenance and license

- The data is the original non-rational Bezier teapot; the source file credits
  Martin Newell and Jim Blinn.
- It comes from the University of Utah Model Repository
  (<https://users.cs.utah.edu/~dejohnso/models/teapot_bezier>), transcribed by
  Thomas V Thompson II (2000). That version has 28 patches (448 control
  points): the classic 32 minus the four bottom patches.
- The Utah teapot data is widely distributed as freely usable/public-domain
  reference geometry.
- Only derived tables are committed. `gen_teapot.go` (`//go:build ignore`)
  does the following:
  - evaluates each bicubic patch on a 6x6 grid;
  - centers the result and scales it ×2048 to `int16`;
  - dedupes vertices and grid edges;
  - emits `teapot_mesh.go`.
- The result has 755 vertices and 1,456 edges.

Regenerate:

```sh
curl -O https://users.cs.utah.edu/~dejohnso/models/teapot_bezier
go run gen_teapot.go teapot_bezier > teapot_mesh.go
```

## Memory budget (TinyGo `pico-plus2`, `-scheduler=cores`)

| Item | Bytes | Location |
| --- | ---: | --- |
| Indexed framebuffer | 230,400 | SRAM `.bss` |
| Palette (256 × uint32) | 1,024 | SRAM |
| Two RGB565 scanlines | 1,920 | SRAM |
| Timing table (498 × 4 words) | 7,968 | SRAM |
| Projected points (2 × 755 × 4 B) | 6,040 | SRAM |
| Mesh copy (verts 4,530 + edges 5,824) | 10,354 | SRAM (also in flash) |
| Sin table (1024 × int32) | 4,096 | flash |
| Total RAM per `-size=short` | 263,904 | heap starts `0x20043048`, ~244 KiB free |

## Build and flash

From `rp2-pio/examples/parallel`:

```sh
tinygo build -target=pico-plus2 -scheduler=cores -o presto-teapot.uf2 ./presto-teapot
# or: tinygo flash -target=pico-plus2 -scheduler=cores ./presto-teapot
tinygo monitor
```

## Expected serial output

```text
(backlight blinks at 4 Hz until the monitor opens, up to 30 s)
teapot: B01 main.main reached; waited ms <n>
teapot: B02 main core <m>
teapot: B03 CPU MHz 150
teapot: B04 verts 755
teapot: B05 edges 1456
teapot: B06 frame limit 0
teapot: B07 palette and border ready; first vertex x <x>
teapot: B07a warmup frames before renderer 4
teapot: B07b self-test first step us <f>
teapot: B07c self-test steady step us (draw+erase) <s>
teapot: B07d self-test lit pixels after erase (expect 0) 0
teapot: B08 starting renderer goroutine 0
teapot: B09 renderer started after us <t>     (or: B09 RENDERER NOT STARTED ...)
teapot: B10 scanout core <m>
teapot: B11 renderer core <n>                  (expect n != m)
teapot: B12 renderer frames so far (expect 0: warmup gate) 0
teapot: B13 calling RunIndexed 0
indexed: t_us ... RunIndexed entered / preconditions ok; initDisplay /
         display initialized; configuring PIO /
         priming FIFOs; starting scanout after USB quiet period (serial silent until stop)
    ... silence indefinitely while the teapot rotates ...
```

Normal operation prints nothing further. Any later `indexed:` line
(`TXSTALL`, DMA error, timeout) followed by
`teapot: renderer core 1 frames drawn <d> display frames skipped <s> scanned <n> step max us <u>`
is a genuine fault.
FDEBUG bit 24 means the data SM stalled; bit 25 means the timing SM stalled.

## Validation

- `go test -race ./presto-teapot ./internal/presto ./presto-indexed` covers:
  - mesh index bounds;
  - projection centering and the exact 150% focal scale;
  - each axis passing through 0/90/180/270° and wrapping at its period, pitch
    reversed, identity at 0 s and 234 s, and orthonormal rotations;
  - int16-safe, mostly-on-screen projection over the whole 234 s cycle, and
    the near-plane guard;
  - the warmup gate not releasing before 4 frames;
  - Bresenham clipping, endpoints and pixel count;
  - the two line palette indices are distinct from background/border and
    expand to identical RGB565;
  - conditional erase: a pixel reclaimed by the new generation survives,
    removed old pixels clear, added pixels are lit, and the border is intact;
  - over 40 real consecutive frames, no old pixel disappears before erase,
    the result is exactly the new wireframe, and every lit pixel holds the
    current generation;
  - `clear` restores background and border only;
  - incremental consistency against a fresh render;
  - zero allocations per step;
  - palette expansion of a rendered row.
- `internal/presto` tests `WaitFrame`: it blocks until the count changes and
  reports skipped frames as a gap.
- TinyGo builds: `presto-teapot` (cores), `presto`, `presto-daliclock`,
  `presto-indexed`.
- Hardware:
  - v3 visibly rendered the teapot and completed 6000 frames with FDEBUG 0.
  - v4 (150% + vsync) failed at frame 0 row 1 on one boot and completed 6000
    frames with 0 skipped on the next.
  - v5: 3-axis motion and size looked good, but showed a black band from
    erase-then-draw.
  - v6 (mask diff): TXSTALL at frame 60 row 25, attributed to mask SRAM traffic.
  - v7 (generation indices): startup passed and a 90 s soak had no serial
    fault; steady update 9,607 µs. Visual confirmation that the band is gone
    is pending.
