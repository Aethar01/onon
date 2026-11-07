//go:build darwin

package viewer

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdbool.h>
#include <stdint.h>
bool ononKeyFilter(void *userdata, void *event);
uint16_t ononKeyModifiers(uint16_t mod, unsigned long flags);
bool ononSameKey(uint16_t raw, bool down, unsigned short keyCode, bool keyDown);
*/
import "C"

import (
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// watchKeyModifiers gives key events the modifiers of the key presses they
// came from; see key_modifiers_darwin.m. It must run after SDL starts, and
// again if SDL restarts, as quitting SDL clears the filter.
func watchKeyModifiers() {
	sdl.SetEventFilter(sdl.EventFilter(unsafe.Pointer(C.ononKeyFilter)), nil)
}

// keyModifiers returns mod with the modifiers of Cocoa modifier flags.
func keyModifiers(mod sdl.Keymod, flags uint64) sdl.Keymod {
	return sdl.Keymod(C.ononKeyModifiers(C.uint16_t(mod), C.ulong(flags)))
}

// sameKey reports whether an SDL key event came from a Cocoa key event.
func sameKey(raw uint16, down bool, keyCode uint16, keyDown bool) bool {
	return bool(C.ononSameKey(C.uint16_t(raw), C.bool(down), C.ushort(keyCode), C.bool(keyDown)))
}
