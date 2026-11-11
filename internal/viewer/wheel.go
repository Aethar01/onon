package viewer

import (
	"math"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// wheelInput is a wheel event resolved once, where it arrives, so that
// whatever it scrolls reads the same deltas.
type wheelInput struct {
	// dx and dy are what to scroll by: the wheel's own direction, whatever
	// the system's natural scrolling made of it, with invert_scroll applied.
	dx, dy float32
	x, y   float32 // where the pointer is
	source smoothInputSource
}

func (a *App) resolveWheel(e *sdl.MouseWheelEvent) wheelInput {
	source := a.wheelInputSource(e)
	wx, wy := normalizedWheelDeltas(e)
	w := wheelInput{x: e.MouseX, y: e.MouseY, source: source}
	w.dx, w.dy = invertWheelDeltas(wx, wy, a.scrollInverted(source))
	return w
}

func (a *App) wheelInputSource(e *sdl.MouseWheelEvent) smoothInputSource {
	followsTrackpad := a.lastTrackpadWheel != 0 && e.Timestamp-a.lastTrackpadWheel < uint64(trackpadWheelGap)
	if hasFractionalWheelDelta(e.X) || hasFractionalWheelDelta(e.Y) || followsTrackpad {
		a.lastTrackpadWheel = e.Timestamp
		return smoothInputSourceTrackpad
	}
	return smoothInputSourceMouse
}

func hasFractionalWheelDelta(delta float32) bool {
	return delta != 0 && float64(delta) != math.Trunc(float64(delta))
}

func normalizedWheelDeltas(e *sdl.MouseWheelEvent) (float32, float32) {
	wx, wy := e.X, e.Y
	if wx == 0 {
		wx = float32(e.IntegerX)
	}
	if wy == 0 {
		wy = float32(e.IntegerY)
	}
	if e.Direction == sdl.MouseWheelFlipped {
		wx = -wx
		wy = -wy
	}
	return wx, wy
}

func invertWheelDeltas(wx, wy float32, invert bool) (float32, float32) {
	if invert {
		return -wx, -wy
	}
	return wx, wy
}

// handleMouseWheel is the one place a wheel event is handled: it resolves
// the event, then gives it to whatever is under it.
func (a *App) handleMouseWheel(e *sdl.MouseWheelEvent) {
	w := a.resolveWheel(e)
	withSmoothInputSource(a, w.source, func() { a.routeWheel(w) })
}

func (a *App) routeWheel(w wheelInput) {
	if a.handleTabBarWheel(w) {
		return
	}
	smooth := a.smoothScrollInputEnabled(w.source)
	if !smooth {
		a.cancelSmoothScroll()
	}
	if view := a.activeModalUIView(); view != nil {
		a.wheelView(view, w, smooth)
		return
	}
	if view := a.panelAt(w.x, w.y); view != nil {
		// A panel's list moves a row an event either way.
		a.wheelView(view, w, false)
		return
	}
	if sdl.GetModState()&sdl.KeymodCtrl != 0 {
		a.cancelSmoothScroll()
		if w.dy > 0 {
			a.runMouseBinding("<C-wheel_up>")
		}
		if w.dy < 0 {
			a.runMouseBinding("<C-wheel_down>")
		}
		a.pendingRedraw = true
		return
	}
	if smooth && a.canSmoothWheel(w.dx, w.dy) {
		// SDL wheel units may be fractional for continuous devices. Scaling
		// the float delta keeps the target precise while preserving
		// scroll_step as the distance of one ordinary wheel unit.
		a.queueSmoothScroll(float64(w.dx)*a.pageStep, -float64(w.dy)*a.pageStep)
		return
	}
	a.cancelSmoothScroll()
	a.dispatchDiscreteWheel(w.dx, w.dy)
	a.pendingRedraw = true
}

// wheelView scrolls view with the wheel: what its mouse hook or drawn body
// takes, else its list, gliding if smooth and otherwise a row an event.
func (a *App) wheelView(view *uiView, w wheelInput, smooth bool) {
	a.pendingRedraw = true
	if a.viewWheel(view, w) {
		return
	}
	if smooth {
		a.queueModalSmoothScroll(view, -float64(w.dy))
		return
	}
	a.scrollListByWheel(view, w)
}

func (a *App) dispatchDiscreteWheel(wx, wy float32) {
	if wy > 0 {
		a.runMouseBinding("wheel_up")
	}
	if wy < 0 {
		a.runMouseBinding("wheel_down")
	}
	if wx > 0 {
		a.runMouseBinding("wheel_right")
	}
	if wx < 0 {
		a.runMouseBinding("wheel_left")
	}
}

func (a *App) canSmoothWheel(wx, wy float32) bool {
	if wx == 0 && wy == 0 {
		return true
	}
	if wy > 0 && a.mouseBindings["wheel_up"] != "scroll_up" {
		return false
	}
	if wy < 0 && a.mouseBindings["wheel_down"] != "scroll_down" {
		return false
	}
	if wx > 0 && a.mouseBindings["wheel_right"] != "scroll_right" {
		return false
	}
	if wx < 0 && a.mouseBindings["wheel_left"] != "scroll_left" {
		return false
	}
	return true
}
