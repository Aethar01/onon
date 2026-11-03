//go:build !darwin

package viewer

import (
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// nativeTabsAvailable is whether the system gathers windows into tabs:
// not here, where onon draws a tab bar of its own.
const nativeTabsAvailable = false

func configureNativeTabbing(*sdl.Window, bool)      {}
func addNativeTab(existing, added *sdl.Window)      {}
func nativeTabWindows(*sdl.Window) []unsafe.Pointer { return nil }
func selectNativeTab(*sdl.Window)                   {}
func moveNativeTab(*sdl.Window, int)                {}
func sameNativeTabGroup(a, b *sdl.Window) bool      { return false }
func nativeTabBarInset(*sdl.Window) float64         { return 0 }
func cocoaWindow(*sdl.Window) unsafe.Pointer        { return nil }
