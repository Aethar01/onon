package viewer

import (
	"math"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A touch screen works the page as touch screens do elsewhere: one finger
// drags it and flicks it on, two pinch to zoom and drag with their middle,
// and a tap is a click. Over a menu or list, a finger is the pointer, so
// rows are tapped and dragged as with a mouse. SDL's own clicks from touch
// are off, as a finger would otherwise select text. Trackpads report their
// fingers too, as touch that is not on the screen, and are left to the
// scrolling and pinching they send besides.

const (
	// touchSlop is how far a finger may move, in logical pixels, and still
	// tap.
	touchSlop = 10
	// touchTapTime is how long a finger may rest and still tap.
	touchTapTime = 400 * time.Millisecond
	// touchFlingSpeed is how fast, in logical pixels a second, a finger
	// must leave the screen to send the page on.
	touchFlingSpeed = 300
	// touchFlingTime is about how long a flick carries the page on: it
	// travels the distance the finger would have in this time.
	touchFlingTime = 0.3
)

type touchState struct {
	order    []sdl.FingerID              // the fingers down, first first
	at       map[sdl.FingerID]sdl.FPoint // where each is, in render pixels
	start    sdl.FPoint                  // where the first came down
	startAt  time.Time
	tap      bool // the first finger may still tap
	asMouse  bool // the first finger is the pointer, over a menu
	pinch    bool
	spread   float64    // how far apart the pinching fingers are
	middle   sdl.FPoint // the point between them
	velocity sdl.FPoint // of a dragging finger, in render pixels a second
	movedAt  time.Time
}

// touchIsScreen reports whether a touch device is a screen, whose fingers
// are where they touch; tests replace it.
var touchIsScreen = func(id sdl.TouchID) bool {
	if sdlGetTouchDeviceType == nil && loadSDLSymbol("SDL_GetTouchDeviceType", &sdlGetTouchDeviceType) != nil {
		return false
	}
	return sdlGetTouchDeviceType(id) == sdl.TouchDeviceDirect
}

var sdlGetTouchDeviceType func(sdl.TouchID) sdl.TouchDeviceType

func (a *App) handleTouch(kind sdl.EventType, e *sdl.TouchFingerEvent) {
	if !touchIsScreen(e.TouchID) {
		return
	}
	t := &a.touch
	if t.at == nil {
		t.at = map[sdl.FingerID]sdl.FPoint{}
	}
	p := sdl.FPoint{X: e.X * float32(a.winW), Y: e.Y * float32(a.winH)}
	switch kind {
	case sdl.EventFingerDown:
		a.touchDown(e.FingerID, p)
	case sdl.EventFingerMotion:
		a.touchMove(e.FingerID, p)
	case sdl.EventFingerUp:
		a.touchUp(e.FingerID, p, true)
	case sdl.EventFingerCanceled:
		a.touchUp(e.FingerID, p, false)
	}
	a.pendingRedraw = true
}

func (a *App) touchDown(id sdl.FingerID, p sdl.FPoint) {
	t := &a.touch
	if _, ok := t.at[id]; ok {
		return
	}
	t.order = append(t.order, id)
	t.at[id] = p
	now := time.Now()
	switch len(t.order) {
	case 1:
		t.start, t.startAt, t.movedAt = p, now, now
		t.tap, t.pinch, t.velocity = true, false, sdl.FPoint{}
		a.cancelSmoothScroll()
		// Over a menu, a panel or a docked view's edge, a finger is the
		// pointer.
		t.asMouse = a.activeModalUIView() != nil || a.panelAt(p.X, p.Y) != nil || a.dockEdgeAt(p.X, p.Y) != nil
		if t.asMouse {
			a.touchPointer(p, sdl.EventMouseButtonDown)
		}
	case 2:
		if t.asMouse {
			return
		}
		t.tap = false
		t.pinch = true
		t.spread, t.middle = a.touchPair()
		a.beginPinch()
	}
}

func (a *App) touchMove(id sdl.FingerID, p sdl.FPoint) {
	t := &a.touch
	last, ok := t.at[id]
	if !ok {
		return
	}
	t.at[id] = p
	if t.asMouse {
		if id == t.order[0] {
			a.touchPointer(p, sdl.EventMouseMotion)
		}
		return
	}
	if t.pinch {
		if len(t.order) < 2 || (id != t.order[0] && id != t.order[1]) {
			return
		}
		spread, middle := a.touchPair()
		if t.spread > 0 && spread > 0 {
			a.updatePinch(spread / t.spread)
		}
		a.scrollBy(float64(t.middle.X-middle.X), float64(t.middle.Y-middle.Y))
		t.spread, t.middle = spread, middle
		return
	}
	if id != t.order[0] {
		return
	}
	slop := touchSlop * a.displayScale()
	if t.tap && math.Hypot(float64(p.X-t.start.X), float64(p.Y-t.start.Y)) < slop {
		return
	}
	if t.tap {
		t.tap = false
		last = t.start // the slop moves the page too, once it is crossed
	}
	a.scrollBy(float64(last.X-p.X), float64(last.Y-p.Y))
	now := time.Now()
	if dt := now.Sub(t.movedAt).Seconds(); dt > 0 {
		// Smoothed, so one uneven step does not set the flick.
		const keep = 0.6
		vx, vy := float64(p.X-last.X)/dt, float64(p.Y-last.Y)/dt
		t.velocity.X = float32(keep*float64(t.velocity.X) + (1-keep)*vx)
		t.velocity.Y = float32(keep*float64(t.velocity.Y) + (1-keep)*vy)
	}
	t.movedAt = now
}

func (a *App) touchUp(id sdl.FingerID, p sdl.FPoint, lifted bool) {
	t := &a.touch
	if _, ok := t.at[id]; !ok {
		return
	}
	first := id == t.order[0]
	delete(t.at, id)
	for i, finger := range t.order {
		if finger == id {
			t.order = append(t.order[:i], t.order[i+1:]...)
			break
		}
	}
	switch {
	case t.asMouse:
		if first {
			a.touchPointer(p, sdl.EventMouseButtonUp)
			t.asMouse = false
		}
	case t.pinch:
		// The finger left goes on dragging, from where it is.
		a.endPinch()
		t.pinch = false
		t.velocity = sdl.FPoint{}
		t.movedAt = time.Now()
	case first && lifted && t.tap && time.Since(t.startAt) < touchTapTime:
		a.touchPointer(t.start, sdl.EventMouseButtonDown)
		a.touchPointer(t.start, sdl.EventMouseButtonUp)
	case first && lifted && !t.tap:
		a.flingTouch()
	}
	if len(t.order) == 0 {
		t.tap = false
	}
}

// flingTouch sends the page on after a flick, unless motion is off.
func (a *App) flingTouch() {
	t := &a.touch
	// A finger that stopped before it lifted sends nothing on.
	if time.Since(t.movedAt) > 100*time.Millisecond || a.motionScale() == 0 {
		return
	}
	speed := math.Hypot(float64(t.velocity.X), float64(t.velocity.Y))
	if speed < touchFlingSpeed*a.displayScale() {
		return
	}
	a.queueSmoothScroll(-float64(t.velocity.X)*touchFlingTime, -float64(t.velocity.Y)*touchFlingTime)
}

// touchPair is how far apart the first two fingers are, and the point
// between them.
func (a *App) touchPair() (float64, sdl.FPoint) {
	t := &a.touch
	p, q := t.at[t.order[0]], t.at[t.order[1]]
	return math.Hypot(float64(q.X-p.X), float64(q.Y-p.Y)), sdl.FPoint{X: (p.X + q.X) / 2, Y: (p.Y + q.Y) / 2}
}

// touchPointer moves the pointer to p and, for a button kind, presses or
// releases the left button there, as a mouse would.
func (a *App) touchPointer(p sdl.FPoint, kind sdl.EventType) {
	if kind == sdl.EventMouseMotion {
		a.handleMouseMotionEvent(&sdl.MouseMotionEvent{CommonEvent: sdl.CommonEvent{Type: kind}, X: p.X, Y: p.Y, State: sdl.MouseButtonFlags(sdl.ButtonLMask)})
		return
	}
	if kind == sdl.EventMouseButtonDown {
		a.handleMouseMotionEvent(&sdl.MouseMotionEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventMouseMotion}, X: p.X, Y: p.Y})
	}
	e := sdl.MouseButtonEvent{CommonEvent: sdl.CommonEvent{Type: kind}, Button: uint8(sdl.ButtonLeft), Down: kind == sdl.EventMouseButtonDown, Clicks: 1, X: p.X, Y: p.Y}
	a.handleMouseButtonEvent(&e)
}
