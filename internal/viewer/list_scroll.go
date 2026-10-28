package viewer

import (
	"math"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A list's offset is the row shown at its top. The wheel, a pan and
// autoscroll move it themselves, a trackpad leaving it part-way into a
// row; any other change of the list's scroll, such as the selection
// moving out of view, is followed, smoothly when keyboard smooth
// scrolling is on.

// listOffset brings view's offset up to date for a frame of a list rows
// tall holding total rows, and returns it.
func (a *App) listOffset(view *uiView, rows, total int) float64 {
	now := a.motion.now
	if now.IsZero() {
		now = time.Now()
	}
	elapsed := a.animationFrameDuration()
	if !view.offsetAt.IsZero() {
		elapsed = now.Sub(view.offsetAt)
	}
	first := view.offsetAt.IsZero()
	view.offsetAt = now
	wheel := a.smoothScroll != nil && a.smoothScroll.modalView == view
	if view.scroll != view.offsetScroll {
		view.offsetScroll, view.settling = view.scroll, true
		if wheel {
			a.cancelSmoothScroll() // what moved the list outranks the wheel
			wheel = false
		}
	}
	if view.settling && !wheel {
		target := float64(view.scroll)
		if first || !a.config.SmoothScrollSources.Has(config.SmoothInputKeyboard) {
			view.offset = target
		} else {
			// A jump of more than a screen, such as to the end or
			// round from the last row to the first, glides only its
			// last screen.
			view.offset = clampFloat(view.offset, target-float64(rows), target+float64(rows))
			view.offset = smoothToward(view.offset, target, a.config.SmoothScrollDampening, elapsed, a.dampingFrame())
			if math.Abs(view.offset-target) <= modalSmoothScrollSnap {
				view.offset = target
			}
		}
		if view.offset == target {
			view.settling = false
		} else {
			a.motion.animating = true
		}
	}
	view.offset = clampFloat(view.offset, 0, float64(max(0, total-rows)))
	return view.offset
}

// scrollListTo shows view from offset, as a wheel, pan or autoscroll
// does, keeping its scroll the nearest whole row.
func (a *App) scrollListTo(view *uiView, offset float64) {
	_, rows := view.frameGeometry(a)
	maxOffset := max(0, len(view.visibleRows())-rows)
	view.offset = clampFloat(offset, 0, float64(maxOffset))
	view.scroll = clampInt(int(math.Round(view.offset)), 0, maxOffset)
	view.offsetScroll, view.settling = view.scroll, false
	a.pendingRedraw = true
}

// scrollListBy scrolls view by rows, which may be a fraction of a row.
func (a *App) scrollListBy(view *uiView, rows float64) {
	a.scrollListTo(view, view.offset+rows)
}

// settleList has view's offset follow its scroll to a whole row, as after
// the keyboard moves the selection.
func settleList(view *uiView) {
	view.offsetScroll, view.settling = view.scroll, true
}

// handleListScrollButton lets the mouse buttons bound to pan and
// autoscroll scroll a list as they scroll the page. It reports whether
// the button was one of them. The left button stays the list's, to pick
// rows and close it, whatever it is bound to.
func (a *App) handleListScrollButton(e *sdl.MouseButtonEvent) bool {
	if e.Type == sdl.EventMouseButtonUp && a.panning && e.Button == a.panButton {
		a.stopPan()
		return true
	}
	if e.Button == uint8(sdl.ButtonLeft) {
		return false
	}
	event, ok := mouseButtonEvent(e.Button, e.Type)
	if !ok {
		return false
	}
	if action := a.mouseBindings[event]; action != "pan" && action != "autoscroll" {
		return false
	}
	a.mouseButton = e.Button
	defer func() { a.mouseButton = 0 }()
	return a.runMouseBinding(event)
}
