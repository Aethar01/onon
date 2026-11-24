//go:build !darwin

package platform

import (
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// NativeTabsAvailable is whether the system gathers windows into tabs:
// not here, where onon draws a tab bar of its own.
const NativeTabsAvailable = false

func ConfigureTabbing(*sdl.Window, bool)      {}
func AddTab(existing, added *sdl.Window)      {}
func TabWindows(*sdl.Window) []unsafe.Pointer { return nil }
func SelectTab(*sdl.Window)                   {}
func MoveTab(*sdl.Window, int)                {}
func SameTabGroup(a, b *sdl.Window) bool      { return false }
func TabBarInset(*sdl.Window) float64         { return 0 }
func ToggleTabBar(*sdl.Window)                {}
func TabBarVisible(*sdl.Window) bool          { return false }
func CocoaWindow(*sdl.Window) unsafe.Pointer  { return nil }
