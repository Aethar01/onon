//go:build darwin

package platform

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

// ConfigureWindow applies macOS window styling (transparent full-size title bar).
// It returns false because macOS draws its own window controls.
func ConfigureWindow(window *sdl.Window, _ func(x, y float64) sdl.HitTestResult) bool {
	if nativeWindow := CocoaWindow(window); nativeWindow != nil {
		C.ononConfigureMacOSWindow(nativeWindow)
	}
	return false
}

// SetWindowDocument gives the window the file it shows, for its proxy icon
// and path menu, and marks it edited while it has unsaved edits.
func SetWindowDocument(window *sdl.Window, path string, edited bool) {
	nativeWindow := CocoaWindow(window)
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

// SetWindowColors does nothing on macOS, whose hidden title bar leaves no frame to colour.
func SetWindowColors(window *sdl.Window, background, border color.RGBA, dark bool) {}

// CocoaWindow returns the NSWindow pointer behind an SDL window.
func CocoaWindow(window *sdl.Window) unsafe.Pointer {
	if window == nil {
		return nil
	}
	properties := sdl.GetWindowProperties(window)
	return sdl.GetPointerProperty(properties, sdl.PropWindowCocoaWindowPointer, nil)
}

// WindowTitleBand is the height, in logical pixels, of the band at the top of
// the window that macOS keeps for its title bar.
const WindowTitleBand = 28

// WindowButtonsLeading is the room, in logical pixels, the close, minimize
// and zoom buttons take at the left of the title band.
const WindowButtonsLeading = 76

//export ononTitleRowTakesPoint
func ononTitleRowTakesPoint(window unsafe.Pointer, x, y C.double, anywhere C.int) C.int {
	if hooks.OnTitleRowTakesPoint != nil && hooks.OnTitleRowTakesPoint(window, float64(x), float64(y), anywhere != 0) {
		return 1
	}
	return 0
}
