package viewer

import (
	"math"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Every view, onon's own and plugins', takes the keys and the pointer the
// same way, through the hooks a view sets for its own behaviour.

// viewKey handles a key pressed while view has the keys. The key goes
// first to the field or prompt typed in, then to a search of the list,
// then to the view's key hook, then to the list's own keys and the
// actions bound. Escape gives a panel's keys back to the document. A held
// key repeats only an action that moves, as held keys do on the page.
func (a *App) viewKey(view *uiView, e *sdl.KeyboardEvent) {
	key, ok := keyToken(e)
	if !ok || e.Type != sdl.EventKeyDown {
		return
	}
	if a.handleUIEditKey(view, key) {
		return
	}
	if view.searchable && a.handleUIViewSearchKey(view, e) {
		return
	}
	if view.keyHook != nil && !view.searching && view.keyHook(a, key, e.Repeat) {
		a.pendingRedraw = true
		return
	}
	if !view.modal && e.Key == sdl.KeycodeEscape {
		a.focusUIView(view, false)
		return
	}
	token := key.String()
	action, bound := a.sequenceLookup[token]
	if e.Repeat && (!bound || !a.isCountableAction(action) || a.hasPrefix(token)) {
		return
	}
	switch e.Key {
	case sdl.KeycodeDown:
		a.moveUIViewSelection(view, 1)
		return
	case sdl.KeycodeUp:
		a.moveUIViewSelection(view, -1)
		return
	}
	if !bound {
		return
	}
	// An action held down, as pan is, ends when this key is let go.
	a.actionKeycode = e.Key
	defer func() { a.actionKeycode = 0 }()
	wasSearching := view.searching
	a.runUIViewAction(view, action)
	if !wasSearching && view.searching {
		a.ignoreKeyText(key)
	}
}

func (a *App) handleUIViewSearchKey(view *uiView, e *sdl.KeyboardEvent) bool {
	if view.searching && e.Key == sdl.KeycodeBackspace {
		if view.query != "" {
			runes := []rune(view.query)
			a.setUIViewQuery(view, string(runes[:len(runes)-1]))
		}
		return true
	}
	if e.Repeat || !view.searching {
		return false
	}
	switch e.Key {
	case sdl.KeycodeEscape:
		view.searching = false
		a.setUIViewQuery(view, "")
		return true
	case sdl.KeycodeReturn, sdl.KeycodeKpEnter:
		view.searching = false
		return true
	}
	if key, ok := keyToken(e); ok {
		if _, ok := key.Text(); ok {
			return true
		}
	}
	return false
}

// runUIViewAction runs an action bound to a key pressed in view: the
// view's own way first, then as moving or scrolling it, then as the
// action does elsewhere.
func (a *App) runUIViewAction(view *uiView, action string) {
	if view == nil || view.onAction != nil && view.onAction(a, view, action) {
		return
	}
	if a.viewScrollAction(view, action) {
		return
	}
	if delta, ok := a.listPageDelta(view, action); ok {
		a.moveUIViewSelection(view, delta)
		return
	}
	switch action {
	case "scroll_down":
		a.moveUIViewSelection(view, 1)
	case "scroll_up":
		a.moveUIViewSelection(view, -1)
	case "scroll_left":
		a.scrollUIViewBy(view, -1)
	case "scroll_right":
		a.scrollUIViewBy(view, 1)
	case "search_prompt", "search_prompt_backward":
		if view.searchable {
			view.searching = true
			a.setUIViewQuery(view, "")
		}
	case "confirm":
		a.activateUIView(view)
	case "close":
		// A search is closed before its list; a prompt's text is the
		// view's own, closed with it.
		if view.prompt == "" && (view.searching || view.query != "") {
			view.searching = false
			a.setUIViewQuery(view, "")
			return
		}
		a.closeUIView(view, true)
	default:
		a.runAction(action)
	}
}

func (a *App) scrollUIViewBy(view *uiView, delta int) {
	if view == nil {
		return
	}
	_, rows := view.contentGeometry(a)
	scrollUIView(view, delta, rows)
	a.pendingRedraw = true
}

func (a *App) activateUIView(view *uiView) {
	if view == nil || view.onSelect == nil {
		return
	}
	for _, row := range view.visibleRows() {
		if row.index == view.selected && !row.disabled {
			view.onSelect(a, row)
			return
		}
	}
}

// viewMouseEvent is the pointer at x, y in the window as a mouse hook is
// told of it, in logical pixels from the top left of what view's body
// draws, scrolled, with the region there, and whether it is in view's
// frame.
func (a *App) viewMouseEvent(view *uiView, x, y float32) (config.UIMouseEvent, bool) {
	frame, _ := view.frameGeometry(a)
	body := a.uiViewBody(view)
	scale := float64(a.px(1))
	ev := config.UIMouseEvent{
		X:      float64(x-body.X) / scale,
		Y:      (float64(y-body.Y) + view.scrollPx) / scale,
		Region: view.regionAt(x, y),
	}
	return ev, pointInRect(int(x), int(y), frame)
}

// viewMouseButton handles a button event for view, which has the pointer
// over it, or after a press on it until the release. A press on a text
// field focuses it. Otherwise the view's mouse hook has the event first,
// then the scrollbar, then the rows; a press outside a menu closes it.
func (a *App) viewMouseButton(view *uiView, e *sdl.MouseButtonEvent) {
	ev, inside := a.viewMouseEvent(view, e.X, e.Y)
	down := e.Type == sdl.EventMouseButtonDown
	left := e.Button == uint8(sdl.ButtonLeft)
	if down && inside && left && a.pressUIField(view, e.X, e.Y) {
		return
	}
	if down && inside || !down && view.pointerDown {
		view.pointerDown = down
		if view.mouseHook != nil {
			ev.Type = "release"
			ev.Button, _ = mouseButtonName(e.Button)
			if down {
				ev.Type, ev.Clicks = "press", int(e.Clicks)
			}
			if view.mouseHook(a, ev) {
				return
			}
		}
	}
	if !left {
		return
	}
	if !down {
		view.draggingScrollbar = false
		return
	}
	if a.viewStartScrollbarDrag(view, e.X, e.Y) {
		return
	}
	if item, ok := a.uiViewIndexAt(view, int(e.X), int(e.Y)); ok {
		if !item.disabled {
			view.selected = item.index
			a.activateUIView(view)
		}
		return
	}
	if !inside && view.modal {
		a.closeUIView(view, true)
	}
}

// viewMouseMotion handles pointer motion for view, reporting whether it
// needs drawing again: as the pointer moves between its rows or regions,
// drags its scrollbar, or leaves it.
func (a *App) viewMouseMotion(view *uiView, e *sdl.MouseMotionEvent) bool {
	ev, inside := a.viewMouseEvent(view, e.X, e.Y)
	leaving := view.pointerOver && !inside && !view.pointerDown
	view.pointerOver = inside
	region := ev.Region
	if !inside {
		region = nil
	}
	redraw := region != view.hoveredRegion
	view.hoveredRegion = region
	if view.mouseHook != nil {
		if inside || view.pointerDown {
			ev.Type = "move"
			if view.mouseHook(a, ev) {
				return true
			}
		} else if leaving {
			ev.Type = "leave"
			view.mouseHook(a, ev)
			redraw = true
		}
	}
	if view.draggingScrollbar {
		a.viewDragScrollbar(view, e.Y)
		return true
	}
	return a.uiViewHover(view, int(e.X), int(e.Y)) || redraw
}

// viewWheel gives a wheel event over view to its mouse hook, then scrolls
// what it draws, reporting whether either took it. A list's rows are left
// to the caller, which scrolls them smoothly where that is on.
func (a *App) viewWheel(view *uiView, e *sdl.MouseWheelEvent) bool {
	ev, inside := a.viewMouseEvent(view, e.MouseX, e.MouseY)
	if !inside {
		return false
	}
	wx, wy := normalizedWheelDeltas(e)
	if view.mouseHook != nil {
		ev.Type, ev.DX, ev.DY = "wheel", float64(wx), -float64(wy)
		if view.mouseHook(a, ev) {
			return true
		}
	}
	return a.scrollViewBodyBy(view, -float64(wy)*float64(3*a.uiLineHeight()))
}

// scrollUIViewByWheel scrolls view's list a row for a wheel event, as a
// list does without smooth scrolling, however far the wheel says.
func (a *App) scrollUIViewByWheel(view *uiView, e *sdl.MouseWheelEvent) {
	if _, wy := normalizedWheelDeltas(e); wy != 0 {
		delta := 1
		if wy > 0 {
			delta = -1
		}
		a.scrollListTo(view, float64(view.scroll+delta))
	}
}

// bodyScrolls reports whether what view's draw function draws is taller
// than its body, so it, not the rows, scrolls.
func (a *App) bodyScrolls(view *uiView) bool {
	return view.drawBody != nil && view.extent > float64(a.uiViewBody(view).H)
}

// scrollViewBodyBy scrolls what view's draw function draws by delta output
// pixels, reporting whether it had that to scroll.
func (a *App) scrollViewBodyBy(view *uiView, delta float64) bool {
	if !a.bodyScrolls(view) {
		return false
	}
	a.setViewScroll(view, view.scrollPx+delta)
	return true
}

// viewScrollAction scrolls what view's draw function draws for the
// actions that move a list, reporting whether action was one.
func (a *App) viewScrollAction(view *uiView, action string) bool {
	if len(view.rows) > 0 {
		return false
	}
	line, page := float64(3*a.uiLineHeight()), float64(a.uiViewBody(view).H)
	delta := map[string]float64{
		"scroll_down": line, "scroll_up": -line, "next_page": page, "prev_page": -page,
		"last_page": view.extent, "first_page": -view.extent,
	}
	d, ok := delta[action]
	return ok && a.scrollViewBodyBy(view, d)
}

// viewScrollRange is how much of view shows and how much there is, and
// how far it is scrolled: in rows for a list, or output pixels for what a
// draw function draws.
func (a *App) viewScrollRange(view *uiView) (shown, total int, offset float64) {
	if a.bodyScrolls(view) {
		return int(a.uiViewBody(view).H), int(math.Ceil(view.extent)), view.scrollPx
	}
	_, rows := view.contentGeometry(a)
	return rows, len(view.visibleRows()), view.offset
}

// setViewScroll scrolls view to offset, in viewScrollRange's units.
func (a *App) setViewScroll(view *uiView, offset float64) {
	if a.bodyScrolls(view) {
		view.scrollPx = clampFloat(offset, 0, view.extent-float64(a.uiViewBody(view).H))
		a.pendingRedraw = true
		return
	}
	a.scrollListTo(view, offset)
}

// viewScrollbar is the track and thumb of view's scrollbar, if it needs
// one.
func (a *App) viewScrollbar(view *uiView) (track, thumb sdl.FRect, ok bool) {
	area := a.uiViewBody(view)
	area.H -= 8
	shown, total, offset := a.viewScrollRange(view)
	return listScrollbarRects(area, shown, total, offset)
}

func (a *App) drawViewScrollbar(renderer *sdl.Renderer, view *uiView) {
	if _, thumb, ok := a.viewScrollbar(view); ok {
		frame, _ := view.frameGeometry(a)
		a.drawScrollbarThumb(renderer, frame, thumb)
	}
}

// viewStartScrollbarDrag starts dragging view's scrollbar at x, y, if it
// is there: from the thumb where it was grabbed, or from its middle,
// jumped to the pointer, when the track was.
func (a *App) viewStartScrollbarDrag(view *uiView, x, y float32) bool {
	track, thumb, ok := a.viewScrollbar(view)
	if !ok || !pointInRect(int(x), int(y), track) {
		return false
	}
	if pointInRect(int(x), int(y), thumb) {
		view.scrollbarDragOffsetY = int(y - thumb.Y)
	} else {
		view.scrollbarDragOffsetY = int(thumb.H / 2)
		a.viewDragScrollbar(view, y)
	}
	view.draggingScrollbar = true
	return true
}

func (a *App) viewDragScrollbar(view *uiView, y float32) {
	track, thumb, ok := a.viewScrollbar(view)
	if !ok {
		return
	}
	shown, total, _ := a.viewScrollRange(view)
	a.setViewScroll(view, modalListScrollbarScrollForY(track, thumb, shown, total, int(y), view.scrollbarDragOffsetY))
}

// panelAt is the topmost open panel at x, y.
func (a *App) panelAt(x, y float32) *uiView {
	panels := a.views.panels
	for i := len(panels) - 1; i >= 0; i-- {
		if rect, _ := panels[i].frameGeometry(a); panels[i].visible && pointInRect(int(x), int(y), rect) {
			return panels[i]
		}
	}
	return nil
}

// handlePanelMouseButton gives a press over a panel to the panel, and the
// release after it, reporting whether a panel took the event. A release
// after a press elsewhere is left to the document, whose drag it ends. A
// press elsewhere takes the keys back from the panel that had them; one
// on a panel that takes keys gives them to it.
func (a *App) handlePanelMouseButton(e *sdl.MouseButtonEvent) bool {
	var view *uiView
	if e.Type == sdl.EventMouseButtonUp {
		view, a.views.pressed = a.views.pressed, nil
	} else {
		view = a.panelAt(e.X, e.Y)
		a.views.pressed = view
		if focused := a.views.focused; focused != nil && focused != view {
			a.focusUIView(focused, false)
		}
		if view != nil && view.takesKeys() {
			a.focusUIView(view, true)
		}
	}
	if view == nil {
		return false
	}
	a.viewMouseButton(view, e)
	return true
}

// handlePanelMouseMotion gives pointer motion to the panel it is over, or
// to the one a button was pressed on, reporting whether a panel took it
// and whether to draw again. The panel the pointer was last over hears of
// it leaving. Motion while a button pressed elsewhere is held, as when
// selecting text, is the document's.
func (a *App) handlePanelMouseMotion(e *sdl.MouseMotionEvent) (taken, redraw bool) {
	view := a.views.pressed
	if view == nil && e.State == 0 {
		view = a.panelAt(e.X, e.Y)
	}
	if old := a.views.hovered; old != nil && old != view && old.visible {
		redraw = a.viewMouseMotion(old, e)
	}
	a.views.hovered = view
	if view == nil {
		return false, redraw
	}
	return true, a.viewMouseMotion(view, e) || redraw
}

// handlePanelMouseWheel gives a wheel event over a panel to the panel,
// reporting whether one took it.
func (a *App) handlePanelMouseWheel(e *sdl.MouseWheelEvent) bool {
	view := a.panelAt(e.MouseX, e.MouseY)
	if view == nil {
		return false
	}
	if !a.viewWheel(view, e) {
		a.scrollUIViewByWheel(view, e)
	}
	return true
}
