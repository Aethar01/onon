//go:build darwin

package viewer

import (
	"testing"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Cocoa's modifier flags, from NSEvent.h and IOLLEvent.h.
const (
	nsShift   = 1 << 17
	nsControl = 1 << 18
	nsOption  = 1 << 19
	nsCommand = 1 << 20

	nsDeviceRCmd = 0x10
	nsDeviceRAlt = 0x40
)

func TestKeyModifiers(t *testing.T) {
	tests := []struct {
		name  string
		mod   sdl.Keymod
		flags uint64
		want  sdl.Keymod
	}{
		{"remapped key drops held Command", sdl.KeymodLGui, 0, 0},
		{"both Commands dropped", sdl.KeymodGui, 0, 0},
		{"held Command kept", sdl.KeymodRGui, nsCommand, sdl.KeymodRGui},
		{"added Command is left", 0, nsCommand, sdl.KeymodLGui},
		{"added right Command", 0, nsCommand | nsDeviceRCmd, sdl.KeymodRGui},
		{"added right Option", 0, nsOption | nsDeviceRAlt, sdl.KeymodRAlt},
		{"remap to Shift arrow", sdl.KeymodLGui, nsShift, sdl.KeymodLShift},
		{"all four", 0, nsShift | nsControl | nsOption | nsCommand,
			sdl.KeymodLShift | sdl.KeymodLCtrl | sdl.KeymodLAlt | sdl.KeymodLGui},
		{"Caps Lock untouched", sdl.KeymodCaps | sdl.KeymodLGui, 0, sdl.KeymodCaps},
	}
	for _, tt := range tests {
		if got := keyModifiers(tt.mod, tt.flags); got != tt.want {
			t.Errorf("%s: keyModifiers(%#x, %#x) = %#x; want %#x", tt.name, tt.mod, tt.flags, got, tt.want)
		}
	}
}

func TestSameKey(t *testing.T) {
	tests := []struct {
		name          string
		raw           uint16
		down          bool
		keyCode       uint16
		keyDown, want bool
	}{
		{"press", 123, true, 123, true, true},
		{"release", 123, false, 123, false, true},
		{"release made during a press", 0, false, 50, true, false},
		{"other key", 38, true, 4, true, false},
		{"ISO swapped key", 10, true, 50, true, true},
		{"ISO swapped back", 50, true, 10, true, true},
	}
	for _, tt := range tests {
		if got := sameKey(tt.raw, tt.down, tt.keyCode, tt.keyDown); got != tt.want {
			t.Errorf("%s: sameKey(%d, %t, %d, %t) = %t; want %t", tt.name, tt.raw, tt.down, tt.keyCode, tt.keyDown, got, tt.want)
		}
	}
}
