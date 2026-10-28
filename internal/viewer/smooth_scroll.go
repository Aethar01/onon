package viewer

import (
	"math"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

const (
	smoothAnimationFrame  = 16 * time.Millisecond
	smoothScrollSnap      = 0.25
	modalSmoothScrollSnap = 0.01
	// trackpadWheelGap is how long after a trackpad's last wheel event a
	// whole-number one is still taken as the trackpad's. Swipes now and then
	// report a whole delta, and reading one as a mouse wheel would invert it
	// or stop animating it mid-swipe.
	trackpadWheelGap = 250 * time.Millisecond
)

type smoothScrollState struct {
	targetX     float64
	targetY     float64
	appliedX    float64
	appliedY    float64
	modalView   *uiView // a list the wheel scrolls, in rows
	targetRow   float64
	appliedRow  float64
	lastAdvance time.Time
}

type smoothInputSource uint8

const (
	smoothInputSourceUnset smoothInputSource = iota
	smoothInputSourceMouse
	smoothInputSourceTrackpad
	smoothInputSourceKeyboard
)

func (a *App) currentSmoothInputSource() smoothInputSource {
	if a.inputSource == smoothInputSourceUnset {
		return smoothInputSourceKeyboard
	}
	return a.inputSource
}

func smoothInputSourceMask(source smoothInputSource) config.SmoothInputSources {
	switch source {
	case smoothInputSourceMouse:
		return config.SmoothInputMouse
	case smoothInputSourceTrackpad:
		return config.SmoothInputTrackpad
	case smoothInputSourceKeyboard:
		return config.SmoothInputKeyboard
	}
	return 0
}

func (a *App) smoothScrollInputEnabled(source smoothInputSource) bool {
	return a.config.SmoothScrollSources.Has(smoothInputSourceMask(source))
}

func (a *App) smoothZoomInputEnabled(source smoothInputSource) bool {
	return a.config.SmoothZoomSources.Has(smoothInputSourceMask(source))
}

func (a *App) scrollInverted(source smoothInputSource) bool {
	return a.config.InvertScrollSources.Has(smoothInputSourceMask(source))
}

func withSmoothInputSource(a *App, source smoothInputSource, run func()) {
	previous := a.inputSource
	a.inputSource = source
	defer func() { a.inputSource = previous }()
	run()
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

func smoothToward(current, target, dampening float64, elapsed, frame time.Duration) float64 {
	if elapsed <= 0 {
		return current
	}
	if frame <= 0 {
		frame = smoothAnimationFrame
	}
	dampening = clampFloat(dampening, 0.01, 1)
	if dampening >= 1 {
		return target
	}
	factor := 1 - math.Pow(1-dampening, float64(elapsed)/float64(frame))
	return current + (target-current)*factor
}

// animationFrameDuration is how often animations draw a frame: the
// display's refresh interval, unless animation_frame_ms fixes it.
func (a *App) animationFrameDuration() time.Duration {
	if a.config.AnimationFrameMS > 0 {
		return time.Duration(a.config.AnimationFrameMS) * time.Millisecond
	}
	if a.displayFrame > 0 {
		return a.displayFrame
	}
	return smoothAnimationFrame
}

// dampingFrame is the frame smoothing's dampening is given per: a fixed
// animation_frame_ms, or else 16ms, so that following a faster display
// draws more often without scrolling any faster.
func (a *App) dampingFrame() time.Duration {
	if a.config.AnimationFrameMS > 0 {
		return time.Duration(a.config.AnimationFrameMS) * time.Millisecond
	}
	return smoothAnimationFrame
}

// updateDisplayFrame reads the refresh rate of the display the window is
// on, which animations follow unless animation_frame_ms fixes their rate.
func (a *App) updateDisplayFrame() {
	if a.window == nil {
		return
	}
	mode := sdl.GetCurrentDisplayMode(sdl.GetDisplayForWindow(a.window))
	if mode == nil || mode.RefreshRate <= 0 {
		a.displayFrame = 0
		return
	}
	a.displayFrame = time.Duration(float64(time.Second) / float64(mode.RefreshRate))
	a.logf("display refresh %.2fHz", mode.RefreshRate)
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

func modalWheelRows(e *sdl.MouseWheelEvent) float64 {
	_, wy := normalizedWheelDeltas(e)
	return -float64(wy)
}

func (a *App) handleAnimatedMouseWheel(e *sdl.MouseWheelEvent) {
	source := a.wheelInputSource(e)
	if !a.smoothScrollInputEnabled(source) {
		a.cancelSmoothScroll()
		withSmoothInputSource(a, source, func() { a.handleSDLMouseWheel(e) })
		a.pendingRedraw = true
		return
	}
	if view := a.activeModalUIView(); view != nil {
		if view.onWheel == nil || !view.onWheel(a, e) {
			a.queueModalSmoothScroll(view, modalWheelRows(e))
		}
		return
	}
	if a.handlePanelMouseWheel(e) {
		a.pendingRedraw = true
		return
	}

	wx, wy := normalizedWheelDeltas(e)
	if sdl.GetModState()&sdl.KeymodCtrl != 0 {
		a.cancelSmoothScroll()
		withSmoothInputSource(a, source, func() { a.handleSDLMouseWheel(e) })
		a.pendingRedraw = true
		return
	}

	if !a.canSmoothWheel(wx, wy) {
		withSmoothInputSource(a, source, func() { a.runDiscreteMouseWheel(wx, wy) })
		return
	}

	wx, wy = invertWheelDeltas(wx, wy, a.scrollInverted(source))
	// SDL wheel units may be fractional for continuous devices. Scaling the
	// float delta keeps the target precise while preserving scroll_step as the
	// distance of one ordinary wheel unit.
	a.queueSmoothScroll(float64(wx)*a.pageStep, -float64(wy)*a.pageStep)
}

func (a *App) runDiscreteMouseWheel(wx, wy float32) {
	a.cancelSmoothScroll()
	wx, wy = invertWheelDeltas(wx, wy, a.scrollInverted(a.currentSmoothInputSource()))
	a.dispatchDiscreteWheel(wx, wy)
	a.pendingRedraw = true
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

func (a *App) modalSmoothScrollBounds(view *uiView) (int, bool) {
	if view == nil || !view.visible || !view.modal {
		return 0, false
	}
	_, rows := view.contentGeometry(a)
	return max(0, len(view.visibleRows())-rows), true
}

// queueModalSmoothScroll moves a list's wheel target by deltaRows; the
// list glides there, resting part-way into a row if the target is.
func (a *App) queueModalSmoothScroll(view *uiView, deltaRows float64) {
	if deltaRows == 0 {
		return
	}
	maxScroll, ok := a.modalSmoothScrollBounds(view)
	if !ok || maxScroll == 0 {
		a.cancelSmoothScroll()
		return
	}
	state := a.smoothScrollState()
	if state == nil || state.modalView != view || state.appliedRow != view.offset {
		state = &smoothScrollState{modalView: view, targetRow: view.offset, appliedRow: view.offset}
		a.smoothScroll = state
	}
	state.targetRow = clampFloat(state.targetRow+deltaRows, 0, float64(maxScroll))
	if state.targetRow == state.appliedRow {
		a.cancelSmoothScroll()
	}
}

func (a *App) queueSmoothScroll(dx, dy float64) {
	state := a.smoothScrollState()
	if state == nil || state.modalView != nil || a.scrollX != state.appliedX || a.scrollY != state.appliedY {
		state = &smoothScrollState{
			targetX:  a.scrollX,
			targetY:  a.scrollY,
			appliedX: a.scrollX,
			appliedY: a.scrollY,
		}
		a.smoothScroll = state
	}

	maxX, maxY := a.maxScrollOffsets()
	state.targetX = clampFloat(state.targetX+dx, 0, maxX)
	state.targetY = clampFloat(state.targetY+dy, 0, maxY)
	if state.targetX == a.scrollX && state.targetY == a.scrollY {
		a.cancelSmoothScroll()
	}
}

func (a *App) advanceSmoothScroll() bool {
	state := a.smoothScrollState()
	if state == nil {
		return false
	}

	now := time.Now()
	elapsed := a.animationFrameDuration()
	if !state.lastAdvance.IsZero() {
		elapsed = now.Sub(state.lastAdvance)
	}
	state.lastAdvance = now
	return a.advanceSmoothScrollBy(elapsed)
}

func (a *App) advanceSmoothScrollBy(elapsed time.Duration) bool {
	state := a.smoothScrollState()
	if state == nil {
		return false
	}
	if state.modalView != nil {
		return a.advanceModalSmoothScrollBy(state, elapsed)
	}

	// Keyboard navigation, panning, page jumps and relayouts take ownership of
	// the viewport immediately. An old wheel target must never pull it back.
	if a.scrollX != state.appliedX || a.scrollY != state.appliedY {
		a.cancelSmoothScroll()
		return false
	}

	nextX := smoothToward(a.scrollX, state.targetX, a.config.SmoothScrollDampening, elapsed, a.dampingFrame())
	nextY := smoothToward(a.scrollY, state.targetY, a.config.SmoothScrollDampening, elapsed, a.dampingFrame())
	if math.Abs(state.targetX-nextX) <= smoothScrollSnap {
		nextX = state.targetX
	}
	if math.Abs(state.targetY-nextY) <= smoothScrollSnap {
		nextY = state.targetY
	}

	oldX, oldY := a.scrollX, a.scrollY
	a.scrollX = nextX
	a.scrollY = nextY
	a.clampScroll()
	if a.renderMode != renderSingle && (a.scrollX != oldX || a.scrollY != oldY) {
		a.followScroll()
	}
	state.appliedX = a.scrollX
	state.appliedY = a.scrollY

	if a.scrollX == state.targetX && a.scrollY == state.targetY {
		a.cancelSmoothScroll()
	}
	if a.scrollX != oldX || a.scrollY != oldY {
		a.pendingRedraw = true
		return true
	}
	return false
}

func (a *App) advanceModalSmoothScrollBy(state *smoothScrollState, elapsed time.Duration) bool {
	view := state.modalView
	maxScroll, ok := a.modalSmoothScrollBounds(view)
	if !ok {
		a.cancelSmoothScroll()
		return false
	}
	if view.offset != state.appliedRow || view.settling {
		// Keyboard selection changes and scrollbar drags take the list
		// over. Do not let an older wheel target pull it back afterward.
		a.cancelSmoothScroll()
		return false
	}
	state.targetRow = clampFloat(state.targetRow, 0, float64(maxScroll))
	next := smoothToward(state.appliedRow, state.targetRow, a.config.SmoothScrollDampening, elapsed, a.dampingFrame())
	if math.Abs(state.targetRow-next) <= modalSmoothScrollSnap {
		next = state.targetRow
	}
	old := view.offset
	a.scrollListTo(view, next)
	state.appliedRow = view.offset
	if state.appliedRow == state.targetRow {
		a.cancelSmoothScroll()
	}
	return view.offset != old
}

// shiftSmoothScroll carries an in-flight wheel animation across a relayout
// that moved the viewport from (oldX, oldY) to keep the same content in view.
// Without it the animation treats the anchor correction as a foreign scroll
// and cancels, dropping the rest of the swipe.
func (a *App) shiftSmoothScroll(oldX, oldY float64) {
	state := a.smoothScrollState()
	if state == nil || state.modalView != nil || state.appliedX != oldX || state.appliedY != oldY {
		return
	}
	maxX, maxY := a.maxScrollOffsets()
	state.targetX = clampFloat(state.targetX+a.scrollX-oldX, 0, maxX)
	state.targetY = clampFloat(state.targetY+a.scrollY-oldY, 0, maxY)
	state.appliedX = a.scrollX
	state.appliedY = a.scrollY
}

func (a *App) smoothScrollState() *smoothScrollState {
	return a.smoothScroll
}

func (a *App) cancelSmoothScroll() {
	a.smoothScroll = nil
}

func (a *App) smoothScrollActive() bool {
	return a.smoothScroll != nil
}

func (a *App) maxScrollOffsets() (float64, float64) {
	viewportW, viewportH := a.viewportSize()
	return math.Max(0, a.contentW-float64(viewportW)), math.Max(0, a.contentH-float64(viewportH))
}
