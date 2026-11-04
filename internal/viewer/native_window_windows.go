//go:build windows

package viewer

/*
#cgo LDFLAGS: -lcomctl32
void ononSubclassTitleBar(void *hwnd);
*/
import "C"

import (
	"image/color"
	"unsafe"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/sys/windows"
)

var (
	dwmapi                           = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")

	procSetAppUserModelID = windows.NewLazySystemDLL("shell32.dll").NewProc("SetCurrentProcessExplicitAppUserModelID")
)

// appUserModelID ties onon's windows, taskbar button, jump list and
// shortcuts together. The installer gives its shortcuts and file types
// the same one.
const appUserModelID = "com.ononreader.onon"

// The ID must be set before the first window is made.
func init() {
	if id, err := windows.UTF16PtrFromString(appUserModelID); err == nil {
		procSetAppUserModelID.Call(uintptr(unsafe.Pointer(id)))
	}
}

// dwmMargins is the MARGINS of DwmExtendFrameIntoClientArea.
type dwmMargins struct {
	left, right, top, bottom int32
}

const dwmCornerRound = 2 // DWMWCP_ROUND

// configureNativeWindow drops the system title bar, keeping the window's
// frame for snapping and the system menu, and draws onon's own controls
// in its place.
func (a *App) configureNativeWindow(window *sdl.Window) {
	if window == nil || !sdl.SetWindowBordered(window, false) {
		return
	}
	a.titleBar = &titleBar{}
	// The binding makes a callback on every call, and never frees one.
	sdl.SetWindowHitTest(window, func(win *sdl.Window, point *sdl.Point, _ unsafe.Pointer) sdl.HitTestResult {
		density := float64(sdl.GetWindowPixelDensity(win))
		if density <= 0 {
			density = 1
		}
		return a.titleBarHitTest(float64(point.X)*density, float64(point.Y)*density)
	}, nil)

	hwnd := win32Window(window)
	if hwnd == 0 {
		return
	}
	C.ononSubclassTitleBar(win32WindowPointer(window))
	setJumpList()
	// Extending the frame into the window brings back the shadow a window
	// without a frame loses. Windows 10 has no rounded corners and fails
	// the second call, which is fine.
	margins := dwmMargins{top: 1}
	procDwmExtendFrameIntoClientArea.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&margins)))
	corner := uint32(dwmCornerRound)
	_ = windows.DwmSetWindowAttribute(hwnd, windows.DWMWA_WINDOW_CORNER_PREFERENCE, unsafe.Pointer(&corner), uint32(unsafe.Sizeof(corner)))
}

func setNativeWindowDocument(window *sdl.Window, path string, edited bool) {}

// DWM window attributes Windows 11 reads; Windows 10 ignores the border.
const (
	dwmUseImmersiveDarkMode = 20 // DWMWA_USE_IMMERSIVE_DARK_MODE
	dwmBorderColor          = 34 // DWMWA_BORDER_COLOR
)

// setNativeWindowColors draws the window's frame dark behind a dark theme,
// so its shadow and edges match, and its border in the theme's colours.
func setNativeWindowColors(window *sdl.Window, background, border color.RGBA) {
	hwnd := win32Window(window)
	if hwnd == 0 {
		return
	}
	dark := uint32(0)
	if !isLight(background) {
		dark = 1
	}
	_ = windows.DwmSetWindowAttribute(hwnd, dwmUseImmersiveDarkMode, unsafe.Pointer(&dark), uint32(unsafe.Sizeof(dark)))
	colorRef := uint32(border.R) | uint32(border.G)<<8 | uint32(border.B)<<16
	_ = windows.DwmSetWindowAttribute(hwnd, dwmBorderColor, unsafe.Pointer(&colorRef), uint32(unsafe.Sizeof(colorRef)))
}

func win32Window(window *sdl.Window) windows.HWND {
	return windows.HWND(uintptr(win32WindowPointer(window)))
}

// win32WindowPointer is the window's HWND as SDL gives it, for C.
func win32WindowPointer(window *sdl.Window) unsafe.Pointer {
	if window == nil {
		return nil
	}
	properties := sdl.GetWindowProperties(window)
	return sdl.GetPointerProperty(properties, sdl.PropWindowWin32HwndPointer, nil)
}

// ononTitleBarMaximizeAt reports whether the maximize button is shown at
// x, y, in the window's pixels.
//
//export ononTitleBarMaximizeAt
func ononTitleBarMaximizeAt(x, y C.int) C.int {
	if a := nativeApp; a != nil && a.titleBarButtonAt(float64(x), float64(y)) == titleButtonMaximize {
		return 1
	}
	return 0
}

// ononTitleBarMaximizePress presses or releases the maximize button,
// which Windows reports as its own once it is told the pointer is there.
//
//export ononTitleBarMaximizePress
func ononTitleBarMaximizePress(down C.int) {
	a := nativeApp
	if a == nil || !a.titleBarActive() {
		return
	}
	a.pressTitleBarButton(titleButtonMaximize, down != 0)
	a.wakeLoop()
}

// windowTitleBand is the height, in logical pixels, of a title bar band
// inside the window: none, as onon's own buttons share the tab bar's row.
const windowTitleBand = 0

// windowButtonsLeading is the room the window's buttons take at the left
// of its title row: none, here.
const windowButtonsLeading = 0
