//go:build darwin

package viewer

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void ononConfigureMacOSWindow(void *window);
void ononSetWindowDocument(void *window, const char *path, int edited);
*/
import "C"

import (
	"image/color"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) configureNativeWindow(window *sdl.Window) {
	if nativeWindow := cocoaWindow(window); nativeWindow != nil {
		C.ononConfigureMacOSWindow(nativeWindow)
	}
}

// setNativeWindowDocument gives the window the file it shows, for its
// proxy icon and path menu, and marks it edited while it has unsaved edits.
func setNativeWindowDocument(window *sdl.Window, path string, edited bool) {
	nativeWindow := cocoaWindow(window)
	if nativeWindow == nil {
		return
	}
	text := C.CString(path)
	defer C.free(unsafe.Pointer(text))
	flag := C.int(0)
	if edited {
		flag = 1
	}
	C.ononSetWindowDocument(nativeWindow, text, flag)
}

// setNativeWindowColors does nothing on macOS, whose hidden title bar
// leaves no frame to colour.
func setNativeWindowColors(window *sdl.Window, background, border color.RGBA) {}

func cocoaWindow(window *sdl.Window) unsafe.Pointer {
	if window == nil {
		return nil
	}
	properties := sdl.GetWindowProperties(window)
	return sdl.GetPointerProperty(properties, sdl.PropWindowCocoaWindowPointer, nil)
}

// windowTitleBand is the height, in logical pixels, of the band at the
// top of the window that macOS keeps for its title bar, whose buttons onon
// shows there and which drags the window: the tab bar is drawn below it,
// or in it with tab_bar_position = "title".
const windowTitleBand = 28

// windowButtonsLeading is the room, in logical pixels, the close, minimize
// and zoom buttons take at the left of the title band, which tabs in that
// row start after.
const windowButtonsLeading = 76

// ononTitleRowTakesPoint answers whether a press at x, y, in points from
// the window's top left, in the title band is for the tab bar there rather
// than dragging the window: over a tab, or with anywhere set, anywhere the
// tabs are.
//
//export ononTitleRowTakesPoint
func ononTitleRowTakesPoint(window unsafe.Pointer, x, y C.double, anywhere C.int) C.int {
	if nativeApp == nil {
		return 0
	}
	for _, w := range nativeApp.windowGroup().windows {
		if w.window == nil || cocoaWindow(w.window) != window {
			continue
		}
		density := float64(sdl.GetWindowPixelDensity(w.window))
		if density <= 0 {
			density = 1
		}
		if w.titleRowTakes(float32(float64(x)*density), float32(float64(y)*density), anywhere != 0) {
			return 1
		}
	}
	return 0
}
