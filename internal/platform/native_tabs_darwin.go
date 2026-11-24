//go:build darwin

package platform

/*
#cgo LDFLAGS: -framework Cocoa
void ononConfigureTabbing(void *window, int enabled);
void ononAddTabbedWindow(void *existing, void *added);
int ononTabbedWindows(void *window, void **out, int max);
void ononSelectTabbedWindow(void *window);
void ononMoveTabbedWindow(void *window, int index);
int ononSameTabGroup(void *a, void *b);
double ononTabBarInset(void *window);
void ononToggleTabBar(void *window);
int ononTabBarVisible(void *window);
*/
import "C"

import (
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// NativeTabsAvailable is whether the system gathers windows into tabs.
const NativeTabsAvailable = true

//export ononNewWindowForTab
func ononNewWindowForTab() {
	if hooks.OnAction != nil {
		hooks.OnAction("new_tab")
	}
}

func ConfigureTabbing(window *sdl.Window, enabled bool) {
	flag := C.int(0)
	if enabled {
		flag = 1
	}
	C.ononConfigureTabbing(CocoaWindow(window), flag)
}

func AddTab(existing, added *sdl.Window) {
	C.ononAddTabbedWindow(CocoaWindow(existing), CocoaWindow(added))
}

func TabWindows(window *sdl.Window) []unsafe.Pointer {
	var buf [64]unsafe.Pointer
	n := int(C.ononTabbedWindows(CocoaWindow(window), (*unsafe.Pointer)(unsafe.Pointer(&buf[0])), C.int(len(buf))))
	return append([]unsafe.Pointer(nil), buf[:min(n, len(buf))]...)
}

func SelectTab(window *sdl.Window) {
	C.ononSelectTabbedWindow(CocoaWindow(window))
}

func MoveTab(window *sdl.Window, index int) {
	C.ononMoveTabbedWindow(CocoaWindow(window), C.int(index))
}

func SameTabGroup(a, b *sdl.Window) bool {
	return C.ononSameTabGroup(CocoaWindow(a), CocoaWindow(b)) != 0
}

func ToggleTabBar(window *sdl.Window) {
	C.ononToggleTabBar(CocoaWindow(window))
}

func TabBarVisible(window *sdl.Window) bool {
	return C.ononTabBarVisible(CocoaWindow(window)) != 0
}

func TabBarInset(window *sdl.Window) float64 {
	return float64(C.ononTabBarInset(CocoaWindow(window)))
}
