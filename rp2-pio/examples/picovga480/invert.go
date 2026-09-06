//go:build rp2040 || rp2350

package main

import (
	"device/rp"
	"machine"
	"runtime/volatile"
	"unsafe"
)

// setPinInvert makes a pad drive the complement of whatever its peripheral
// outputs, or stops it doing so.
//
// IO_BANK0 holds a status and a control register for each GPIO in turn, eight
// bytes apart. The output override field sits at different bit positions on the
// RP2040 and the RP2350 but under the same name, so these constants resolve
// correctly for whichever is being built.
func setPinInvert(p machine.Pin, invert bool) {
	over := uint32(0) // normal
	if invert {
		over = rp.IO_BANK0_GPIO0_CTRL_OUTOVER_INVERT
	}
	base := uintptr(unsafe.Pointer(rp.IO_BANK0))
	reg := (*volatile.Register32)(unsafe.Pointer(base + uintptr(p)*8 + 4))
	v := reg.Get() &^ uint32(rp.IO_BANK0_GPIO0_CTRL_OUTOVER_Msk)
	reg.Set(v | over<<rp.IO_BANK0_GPIO0_CTRL_OUTOVER_Pos)
}
