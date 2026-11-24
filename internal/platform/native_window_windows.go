//go:build windows

package platform

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

type dwmMargins struct {
	left, right, top, bottom int32
}

const dwmCornerRound = 2 // DWMWCP_ROUND

// ConfigureWindow drops the system title bar on Windows, keeping the window's
// frame for snapping and the system menu. It returns true when the viewer
// should draw its own title bar controls.
func ConfigureWindow(window *sdl.Window, hitTest func(x, y float64) sdl.HitTestResult) bool {
	if window == nil || !sdl.SetWindowBordered(window, false) {
		return false
	}
	// The binding makes a callback on every call, and never frees one.
	sdl.SetWindowHitTest(window, func(win *sdl.Window, point *sdl.Point, _ unsafe.Pointer) sdl.HitTestResult {
		density := float64(sdl.GetWindowPixelDensity(win))
		if density <= 0 {
			density = 1
		}
		return hitTest(float64(point.X)*density, float64(point.Y)*density)
	}, nil)

	hwnd := win32Window(window)
	if hwnd == 0 {
		return true
	}
	C.ononSubclassTitleBar(win32WindowPointer(window))
	setJumpList()
	margins := dwmMargins{top: 1}
	procDwmExtendFrameIntoClientArea.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&margins)))
	corner := uint32(dwmCornerRound)
	_ = windows.DwmSetWindowAttribute(hwnd, windows.DWMWA_WINDOW_CORNER_PREFERENCE, unsafe.Pointer(&corner), uint32(unsafe.Sizeof(corner)))
	return true
}

func SetWindowDocument(window *sdl.Window, path string, edited bool) {}

const (
	dwmUseImmersiveDarkMode = 20 // DWMWA_USE_IMMERSIVE_DARK_MODE
	dwmBorderColor          = 34 // DWMWA_BORDER_COLOR
)

// SetWindowColors draws the window's frame dark behind a dark theme and its
// border in the theme's colours.
func SetWindowColors(window *sdl.Window, background, border color.RGBA, dark bool) {
	hwnd := win32Window(window)
	if hwnd == 0 {
		return
	}
	darkAttr := uint32(0)
	if dark {
		darkAttr = 1
	}
	_ = windows.DwmSetWindowAttribute(hwnd, dwmUseImmersiveDarkMode, unsafe.Pointer(&darkAttr), uint32(unsafe.Sizeof(darkAttr)))
	colorRef := uint32(border.R) | uint32(border.G)<<8 | uint32(border.B)<<16
	_ = windows.DwmSetWindowAttribute(hwnd, dwmBorderColor, unsafe.Pointer(&colorRef), uint32(unsafe.Sizeof(colorRef)))
}

func win32Window(window *sdl.Window) windows.HWND {
	return windows.HWND(uintptr(win32WindowPointer(window)))
}

func win32WindowPointer(window *sdl.Window) unsafe.Pointer {
	if window == nil {
		return nil
	}
	properties := sdl.GetWindowProperties(window)
	return sdl.GetPointerProperty(properties, sdl.PropWindowWin32HwndPointer, nil)
}

//export ononTitleBarMaximizeAt
func ononTitleBarMaximizeAt(x, y C.int) C.int {
	if hooks.OnTitleBarMaximizeAt != nil && hooks.OnTitleBarMaximizeAt(int(x), int(y)) {
		return 1
	}
	return 0
}

//export ononTitleBarMaximizePress
func ononTitleBarMaximizePress(down C.int) {
	if hooks.OnTitleBarMaximizePress != nil {
		hooks.OnTitleBarMaximizePress(down != 0)
	}
}

const WindowTitleBand = 0
const WindowButtonsLeading = 0
