package viewer

import (
	"github.com/jupiterrider/purego-sdl3/sdl"
)

type cursorKind uint8

const (
	cursorDefault cursorKind = iota
	cursorPointer
	cursorGrabbing
	cursorAutoscrollAll
	cursorAutoscrollVertical
	cursorAutoscrollHorizontal
	// The directional autoscroll cursors run clockwise from north.
	cursorAutoscrollN
	cursorAutoscrollNE
	cursorAutoscrollE
	cursorAutoscrollSE
	cursorAutoscrollS
	cursorAutoscrollSW
	cursorAutoscrollW
	cursorAutoscrollNW
	cursorResizeEW // a docked view's edge, dragged across
	cursorResizeNS // and dragged up and down
	cursorText     // over text to type in
)

// Cursors from the CSS set SDL 3.6 adds after SDL_SYSTEM_CURSOR_W_RESIZE,
// which the binding does not name yet. SDL 3.4 accepts these values but
// shows its default arrow for them.
const (
	systemCursorGrabbing  sdl.SystemCursor = 28
	systemCursorAllScroll sdl.SystemCursor = 31
)

func sdlHasCSSCursors() bool {
	major, minor, _ := sdl.GetVersion()
	return major > 3 || major == 3 && minor >= 6
}

// updateCursor shows the cursor for what the pointer does right now.
func (a *App) updateCursor() {
	kind := cursorDefault
	switch {
	case a.autoscroll != nil:
		kind = a.autoscrollCursor()
	case a.panning:
		kind = cursorGrabbing
	case a.links.overLink:
		kind = cursorPointer
	default:
		kind = a.viewCursor()
	}
	a.setCursor(kind)
}

// viewCursor is the cursor for the views under the pointer: across or up
// and down at a docked view's edge, a hand over a button or check box, and
// an I-beam over text to type in.
func (a *App) viewCursor() cursorKind {
	if a.views.active == nil && len(a.views.panels) == 0 {
		return cursorDefault
	}
	x, y := a.pointer.X, a.pointer.Y
	edge := a.views.resizing
	if edge == nil {
		edge = a.dockEdgeAt(x, y)
	}
	if edge != nil {
		if edge.place.dock == "left" || edge.place.dock == "right" {
			return cursorResizeEW
		}
		return cursorResizeNS
	}
	view := a.viewUnderPointer()
	if view == nil {
		return cursorDefault
	}
	switch target, _ := view.controlAt(x, y); target {
	case targetControl:
		return cursorPointer
	case targetField, targetPrompt:
		return cursorText
	}
	return cursorDefault
}

func (a *App) setCursor(kind cursorKind) {
	if a.window == nil || kind == a.cursor {
		return
	}
	cursor, ok := a.cursors[kind]
	if !ok {
		cursor = sdl.CreateSystemCursor(systemCursorFor(kind, sdlHasCSSCursors()))
		if a.cursors == nil {
			a.cursors = make(map[cursorKind]*sdl.Cursor)
		}
		a.cursors[kind] = cursor
	}
	if cursor != nil && sdl.SetCursor(cursor) {
		a.cursor = kind
	}
}

// systemCursorFor picks the system cursor for kind, using the nearest
// older shape where SDL lacks the CSS cursors.
func systemCursorFor(kind cursorKind, css bool) sdl.SystemCursor {
	switch kind {
	case cursorPointer:
		return sdl.SystemCursorPointer
	case cursorGrabbing:
		if css {
			return systemCursorGrabbing
		}
		return sdl.SystemCursorMove
	case cursorAutoscrollAll:
		if css {
			return systemCursorAllScroll
		}
		return sdl.SystemCursorMove
	case cursorAutoscrollVertical:
		return sdl.SystemCursorNSResize
	case cursorAutoscrollHorizontal, cursorResizeEW:
		return sdl.SystemCursorEWResize
	case cursorResizeNS:
		return sdl.SystemCursorNSResize
	case cursorText:
		return sdl.SystemCursorText
	}
	if kind >= cursorAutoscrollN && kind <= cursorAutoscrollNW {
		return []sdl.SystemCursor{
			sdl.SystemCursorNResize, sdl.SystemCursorNEResize, sdl.SystemCursorEResize, sdl.SystemCursorSEResize,
			sdl.SystemCursorSResize, sdl.SystemCursorSWResize, sdl.SystemCursorWResize, sdl.SystemCursorNWResize,
		}[kind-cursorAutoscrollN]
	}
	return sdl.SystemCursorDefault
}

func (s *sdlState) destroyCursors() {
	for kind, cursor := range s.cursors {
		if cursor != nil {
			sdl.DestroyCursor(cursor)
		}
		delete(s.cursors, kind)
	}
}
