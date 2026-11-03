//go:build darwin

package viewer

/*
#cgo LDFLAGS: -framework Cocoa
void ononConfigureTabbing(void *window, int enabled);
void ononAddTabbedWindow(void *existing, void *added);
int ononTabbedWindows(void *window, void **out, int max);
void ononSelectTabbedWindow(void *window);
void ononMoveTabbedWindow(void *window, int index);
int ononSameTabGroup(void *a, void *b);
double ononTabBarInset(void *window);
*/
import "C"

import (
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// nativeTabsAvailable is whether the system gathers windows into tabs.
const nativeTabsAvailable = true

// ononNewWindowForTab answers the tab bar's +.
//
//export ononNewWindowForTab
func ononNewWindowForTab() {
	chooseSystemAction("new_tab")
}

func configureNativeTabbing(window *sdl.Window, enabled bool) {
	flag := C.int(0)
	if enabled {
		flag = 1
	}
	C.ononConfigureTabbing(cocoaWindow(window), flag)
}

// addNativeTab makes added a tab of existing's window, and shows it.
func addNativeTab(existing, added *sdl.Window) {
	C.ononAddTabbedWindow(cocoaWindow(existing), cocoaWindow(added))
}

// nativeTabWindows returns the Cocoa windows of window's tabs, in order.
func nativeTabWindows(window *sdl.Window) []unsafe.Pointer {
	var buf [64]unsafe.Pointer
	n := int(C.ononTabbedWindows(cocoaWindow(window), (*unsafe.Pointer)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
	return append([]unsafe.Pointer(nil), buf[:min(n, len(buf))]...)
}

func selectNativeTab(window *sdl.Window) {
	C.ononSelectTabbedWindow(cocoaWindow(window))
}

func moveNativeTab(window *sdl.Window, index int) {
	C.ononMoveTabbedWindow(cocoaWindow(window), C.int(index))
}

func sameNativeTabGroup(a, b *sdl.Window) bool {
	return C.ononSameTabGroup(cocoaWindow(a), cocoaWindow(b)) != 0
}

// nativeTabBarInset is how far the system's tab bar, and the title bar
// above it, reach over the window's content, in points.
func nativeTabBarInset(window *sdl.Window) float64 {
	return float64(C.ononTabBarInset(cocoaWindow(window)))
}
