package viewer

import (
	"unsafe"

	"onon/internal/platform"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// The system's own UI, such as the menu bar, the taskbar or a window's
// frame, reaches the viewer through code the system calls on the main
// thread while the event loop waits in its event handling. That code finds
// the app in nativeApp, and queues the actions chosen with
// chooseSystemAction for the event loop, which it wakes.

var (
	// nativeApp is the app the system's UI acts on, once it has a window.
	nativeApp     *App
	systemActions = make(chan string, 16)
)

func init() {
	platform.SetHooks(platform.Hooks{
		OnAction: chooseSystemAction,
		OnMenuItemState: func(action string) (enabled, checked bool) {
			if nativeApp == nil {
				return true, false
			}
			return nativeApp.menuItemState(action)
		},
		OnTitleRowTakesPoint: func(window unsafe.Pointer, x, y float64, anywhere bool) bool {
			if nativeApp == nil {
				return false
			}
			for _, w := range nativeApp.windowGroup().windows {
				if w.window == nil || platform.CocoaWindow(w.window) != window {
					continue
				}
				density := float64(sdl.GetWindowPixelDensity(w.window))
				if density <= 0 {
					density = 1
				}
				if w.titleRowTakes(float32(x*density), float32(y*density), anywhere) {
					return true
				}
			}
			return false
		},
		OnTitleBarMaximizeAt: func(x, y int) bool {
			return nativeApp != nil && nativeApp.titleBarButtonAt(float64(x), float64(y)) == titleButtonMaximize
		},
		OnTitleBarMaximizePress: func(down bool) {
			if nativeApp == nil || !nativeApp.titleBarActive() {
				return
			}
			nativeApp.pressTitleBarButton(titleButtonMaximize, down)
			nativeApp.wakeLoop()
		},
	})
}

// LaunchDocuments returns the documents the system launched onon to open
// without passing them as arguments (Apple Events on macOS).
func LaunchDocuments(verbose bool) []string {
	return platform.LaunchDocuments(initSDL, verbose)
}

func (a *App) configureNativeWindow(window *sdl.Window) {
	if platform.ConfigureWindow(window, a.titleBarHitTest) {
		a.titleBar = &titleBar{}
	}
}

// chooseSystemAction queues an action chosen in the system's UI.
func chooseSystemAction(action string) {
	select {
	case systemActions <- action:
		if nativeApp != nil {
			nativeApp.wakeLoop()
		}
	default: // a burst too quick to keep up with
	}
}

// pollSystemActions runs the actions chosen in the system's UI.
func (a *App) pollSystemActions() bool {
	ran := false
	for {
		select {
		case action := <-systemActions:
			a.runAction(action)
			ran = true
		default:
			return ran
		}
	}
}
