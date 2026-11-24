package platform

import "unsafe"

// Hooks connects OS-initiated callbacks (menu bar, native tabs, window title
// bar and taskbar controls) back into the viewer without platform depending on
// viewer types.
type Hooks struct {
	OnAction                func(action string)
	OnMenuItemState         func(action string) (enabled, checked bool)
	OnTitleRowTakesPoint    func(cocoaWindow unsafe.Pointer, x, y float64, anywhere bool) bool
	OnTitleBarMaximizeAt    func(x, y int) bool
	OnTitleBarMaximizePress func(down bool)
}

var hooks Hooks

// SetHooks installs the callbacks invoked from native OS hooks.
func SetHooks(h Hooks) {
	hooks = h
}
