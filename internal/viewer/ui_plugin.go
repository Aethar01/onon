package viewer

import (
	"math"
	"strconv"
	"strings"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) ShowUI(spec config.UIOverlay) error {
	if spec.Modal && !spec.Stack {
		callCallbacks := true
		if active := a.activeUIView(); active != nil && active.owner == "lua" && active.generation != spec.Generation {
			callCallbacks = false
		}
		a.closeAllUIWithCallbacks(callCallbacks)
	} else if old := a.views.views[spec.ID]; old != nil {
		a.closeUIView(old, true)
	}
	view := a.createLuaView(spec)
	a.showUIView(view)
	// Opening is not a change of selection, for on_selection_changed.
	view.notifiedSelected = view.selected
	return nil
}

func (a *App) RedrawUI(id string) {
	if view := a.views.views[id]; view != nil && view.visible {
		a.pendingRedraw = true
	}
}

func (a *App) CloseUI(id string) {
	view := a.views.views[id]
	a.closeUIView(view, true)
}

func (a *App) UIVisible(id string) bool {
	view := a.views.views[id]
	return view != nil && view.visible
}

func (a *App) SetUIRows(id string, rows []config.UIListRow) {
	view := a.views.views[id]
	if view == nil {
		return
	}
	a.setUIViewRows(view, uiRowsFromConfig(rows))
}

func (a *App) SetUISelected(id string, selected int) {
	view := a.views.views[id]
	if view == nil {
		return
	}
	if len(view.rows) == 0 {
		view.selected = -1
	} else {
		view.selected = clampInt(selected-1, 0, len(view.rows)-1)
	}
	a.ensureUIViewSelectionVisible(view)
	view.notifiedSelected = view.selected
	a.pendingRedraw = true
}

func (a *App) SetUIScroll(id string, scroll int) {
	view := a.views.views[id]
	if view == nil {
		return
	}
	if view.drawBody != nil && len(view.rows) == 0 {
		// what a view draws scrolls by logical pixels
		view.scrollPx = 0
		a.scrollLuaViewBy(view, float64(scroll)*float64(a.px(1)))
		return
	}
	_, rows := view.contentGeometry(a)
	view.scroll = clampInt(scroll, 0, max(0, len(view.visibleRows())-rows))
	a.pendingRedraw = true
}

// SetUIQuery sets a view's query without telling its on_query_changed,
// which hears only of the user's typing.
func (a *App) SetUIQuery(id string, query string) {
	view := a.views.views[id]
	if view == nil {
		return
	}
	view.query = query
	a.ensureUIViewSelectionVisible(view)
	a.pendingRedraw = true
}

func (a *App) UISelected(id string) int {
	view := a.views.views[id]
	if view == nil {
		return 0
	}
	return view.selected + 1
}

func (a *App) UIScroll(id string) int {
	view := a.views.views[id]
	if view == nil {
		return 0
	}
	if view.drawBody != nil && len(view.rows) == 0 {
		return int(math.Round(view.scrollPx / float64(a.px(1))))
	}
	return view.scroll
}

func (a *App) UIQuery(id string) string {
	view := a.views.views[id]
	if view == nil {
		return ""
	}
	return view.query
}

func (a *App) handleGenericUIViewKey(view *uiView, e *sdl.KeyboardEvent) bool {
	if view == nil || e == nil {
		return true
	}
	if view.searchable && a.handleUIViewSearchKey(view, e) {
		return true
	}
	if e.Type != sdl.EventKeyDown || e.Repeat {
		return true
	}
	if e.Key == sdl.KeycodeDown {
		a.moveUIViewSelection(view, 1)
		return true
	}
	if e.Key == sdl.KeycodeUp {
		a.moveUIViewSelection(view, -1)
		return true
	}
	if key, ok := keyToken(e); ok {
		if action, ok := a.sequenceLookup[key.String()]; ok {
			// An action held down, as pan is, ends when this key is let go.
			a.actionKeycode = e.Key
			defer func() { a.actionKeycode = 0 }()
			wasSearching := view.searching
			a.runUIViewAction(view, action)
			if !wasSearching && view.searching {
				a.ignoreKeyText(key)
			}
		}
	}
	return true
}

func (a *App) handleUIViewSearchKey(view *uiView, e *sdl.KeyboardEvent) bool {
	if e.Type != sdl.EventKeyDown {
		return true
	}
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

func (a *App) runUIViewAction(view *uiView, action string) {
	if view == nil || view.onAction != nil && view.onAction(a, view, action) {
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
		if view.searching || view.query != "" {
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

func (a *App) handleGenericUIViewMouseButton(view *uiView, e *sdl.MouseButtonEvent) bool {
	if view == nil || e == nil {
		return true
	}
	if e.Type == sdl.EventMouseButtonUp && e.Button == uint8(sdl.ButtonLeft) {
		view.draggingScrollbar = false
		return true
	}
	if e.Type != sdl.EventMouseButtonDown || e.Button != uint8(sdl.ButtonLeft) {
		return true
	}
	if a.uiViewStartScrollbarDrag(view, int(e.X), int(e.Y)) {
		return true
	}
	if item, ok := a.uiViewIndexAt(view, int(e.X), int(e.Y)); ok {
		if item.disabled {
			return true
		}
		view.selected = item.index
		a.activateUIView(view)
		return true
	}
	rect, _ := view.frameGeometry(a)
	if !pointInRect(int(e.X), int(e.Y), rect) {
		a.closeUIView(view, true)
	}
	return true
}

func (a *App) handleGenericUIViewMouseMotion(view *uiView, e *sdl.MouseMotionEvent) bool {
	if view == nil || e == nil {
		return true
	}
	if view.draggingScrollbar {
		old := view.scroll
		a.uiViewDragScrollbar(view, int(e.Y))
		return old != view.scroll
	}
	return a.uiViewHover(view, int(e.X), int(e.Y))
}

func (a *App) createLuaView(spec config.UIOverlay) *uiView {
	selected := -1
	if len(spec.Rows) > 0 {
		selected = clampInt(spec.Selected-1, 0, len(spec.Rows)-1)
	}
	view := &uiView{
		takesKeys:  spec.OnKey != "" || spec.Prompt != "",
		id:         spec.ID,
		owner:      "lua",
		generation: spec.Generation,
		title:      spec.Title,
		rows:       uiRowsFromConfig(spec.Rows),
		selected:   selected,
		scroll:     max(0, spec.Scroll),
		query:      spec.Query,
		searchable: spec.Searchable,
		noFilter:   !spec.Filter,
		modal:      spec.Modal,
		stack:      spec.Modal && spec.Stack,
		headless:   spec.Draw != "" && spec.Title == "",
		place:      viewPlacement{width: spec.Width, height: spec.Height, anchor: spec.Anchor, at: spec.At, dock: spec.Dock},
	}
	if view.id == "" {
		view.id = "lua" + strconv.Itoa(len(a.views.views)+1)
	}
	// A list fits its panel to its rows; a view that draws keeps its size.
	view.geometry = func(a *App) (sdl.FRect, int) { return a.luaViewGeometry(view, spec.Draw == "") }
	// Keys go first to the field or prompt typed in, then to on_key, then
	// to the list. Escape gives a panel's keys back to the document.
	view.onKey = func(a *App, e *sdl.KeyboardEvent) bool {
		key, ok := keyToken(e)
		if !ok || e.Type != sdl.EventKeyDown {
			return a.handleGenericUIViewKey(view, e)
		}
		if a.handleUIEditKey(view, key) {
			return true
		}
		if spec.OnKey != "" && !view.searching {
			handled, err := a.runtime.RunUIKey(spec.OnKey, key.String())
			if a.afterLuaViewCallback("ui_key", err); handled {
				return true
			}
		}
		if !view.modal && e.Key == sdl.KeycodeEscape {
			a.focusUIView(view, false)
			return true
		}
		return a.handleGenericUIViewKey(view, e)
	}
	if spec.OnInput != "" {
		view.onInput = func(a *App, id, text string) {
			a.afterLuaViewCallback("ui_input", a.runtime.RunUIText(spec.OnInput, text, id))
		}
	}
	if spec.OnSubmit != "" {
		view.onSubmit = func(a *App, id, text string) {
			a.afterLuaViewCallback("ui_submit", a.runtime.RunUIText(spec.OnSubmit, text, id))
		}
	}
	for id, text := range spec.Fields {
		view.field(id).input.Set(text)
	}
	if spec.Prompt != "" {
		view.prompt = spec.Prompt
		view.promptInput.Set(spec.Query)
		view.searchable = false // typing goes to the prompt, without /
	}
	a.setLuaViewMouse(view, spec.OnMouse)
	view.onAction = func(a *App, view *uiView, action string) bool {
		if spec.OnAction != "" {
			handled, err := a.runtime.RunUIAction(spec.OnAction, action)
			if a.afterLuaViewCallback("ui_action", err); handled {
				return true
			}
		}
		return a.luaViewScrollAction(view, action)
	}
	if spec.OnSelect != "" {
		view.onSelect = func(a *App, row uiRow) {
			a.afterLuaViewCallback("ui_select", a.runtime.RunUIRow(spec.OnSelect, luaRowRef(row)))
		}
	}
	if spec.OnSelectionChanged != "" {
		view.onSelectionChanged = func(a *App, view *uiView) {
			for _, row := range view.rows {
				if row.index == view.selected {
					a.afterLuaViewCallback("ui_selection_changed", a.runtime.RunUIRow(spec.OnSelectionChanged, luaRowRef(row)))
					return
				}
			}
		}
	}
	if spec.OnQueryChanged != "" {
		view.onQueryChanged = func(a *App, view *uiView) {
			a.afterLuaViewCallback("ui_query_changed", a.runtime.RunUIQuery(spec.OnQueryChanged, view.query))
		}
	}
	if spec.OnClose != "" {
		view.onClose = func(a *App) {
			a.afterLuaViewCallback("ui_close", a.runtime.RunUIClose(spec.OnClose))
		}
	}
	if spec.Draw != "" {
		view.drawBody = func(a *App, renderer *sdl.Renderer, body sdl.FRect, drawRows func() error) error {
			return a.drawLuaViewBody(view, spec.Draw, renderer, body, drawRows)
		}
	}
	return view
}

func luaRowRef(row uiRow) config.UIRowRef {
	return config.UIRowRef{Index: row.index + 1, Value: row.value, Text: row.text, ID: row.id, Lua: row.lua}
}

// afterLuaViewCallback shows a view callback's error, if it failed, and
// applies what it changed. A view's callbacks often change what it
// draws, so it is drawn again.
func (a *App) afterLuaViewCallback(source string, err error) {
	if err != nil {
		a.message = err.Error()
	}
	a.applyRuntimeChanges(source)
	a.pendingRedraw = true
}

// viewPlacement is where a plugin's view sits: its size, and an anchor,
// a point it is pinned to, or a side of the window it is docked to.
type viewPlacement struct {
	width, height config.UISize
	anchor        string
	at            *config.UIPoint
	dock          string
}

// maxDockShare is the most of the document's part of the window a docked
// view takes.
const maxDockShare = 0.8

// uiLength is size in output pixels, a percentage being of whole.
func (a *App) uiLength(size config.UISize, whole float64) float64 {
	if size.Percent {
		return whole * size.Value / 100
	}
	return size.Value * float64(a.px(1))
}

// documentRect is the document's part of the window: above the status
// bar, and beside the docked views.
func (a *App) documentRect() sdl.FRect {
	doc, _ := a.dockLayout(nil)
	return doc
}

// dockLayout lays out the docked views in the order they opened, each
// taking its width or height from its side of what those before it left.
// It returns what is left, the document's part of the window, and
// target's place.
func (a *App) dockLayout(target *uiView) (doc, rect sdl.FRect) {
	h := a.winH
	if a.statusVisible() {
		h -= a.statusReservedHeight()
	}
	doc = sdl.FRect{W: float32(max(a.winW, 1)), H: float32(max(h, 1))}
	for _, view := range a.views.panels {
		if view.place.dock == "" || !view.visible {
			continue
		}
		var r sdl.FRect
		switch view.place.dock {
		case "left", "right":
			r = sdl.FRect{X: doc.X, Y: doc.Y, H: doc.H}
			r.W = float32(math.Round(min(a.uiLength(view.place.width, float64(a.winW)), float64(doc.W)*maxDockShare)))
			if view.place.dock == "right" {
				r.X = doc.X + doc.W - r.W
			} else {
				doc.X += r.W
			}
			doc.W -= r.W
		default:
			r = sdl.FRect{X: doc.X, Y: doc.Y, W: doc.W}
			r.H = float32(math.Round(min(a.uiLength(view.place.height, float64(a.winH)), float64(doc.H)*maxDockShare)))
			if view.place.dock == "bottom" {
				r.Y = doc.Y + doc.H - r.H
			} else {
				doc.Y += r.H
			}
			doc.H -= r.H
		}
		if view == target {
			rect = r
		}
	}
	return doc, rect
}

// hasDock reports whether a view is docked, moving the document from the
// window's corner.
func (a *App) hasDock() bool {
	for _, view := range a.views.panels {
		if view.place.dock != "" && view.visible {
			return true
		}
	}
	return false
}

// withDocumentViewport draws with the document's part of the window as
// the renderer's viewport while views are docked, so the document is
// drawn as it is laid out, from its own corner, and not under them.
func (a *App) withDocumentViewport(renderer *sdl.Renderer, draw func()) {
	if !a.hasDock() {
		draw()
		return
	}
	r := a.documentRect()
	rect := sdl.Rect{X: int32(r.X), Y: int32(r.Y), W: int32(r.W), H: int32(r.H)}
	sdl.SetRenderViewport(renderer, &rect)
	defer sdl.SetRenderViewport(renderer, nil)
	draw()
}

// uiPointOnScreen is where pt is in the window, and false when it is on a
// page not shown.
func (a *App) uiPointOnScreen(pt config.UIPoint) (float64, float64, bool) {
	if pt.Page == 0 {
		scale := float64(a.px(1))
		return pt.X * scale, pt.Y * scale, true
	}
	page := pt.Page - 1
	if page >= len(a.pageMetrics) || page >= len(a.pageToRow) {
		return 0, 0, false
	}
	if a.renderMode == renderSingle && (a.page < 0 || a.page >= len(a.pageToRow) || a.pageToRow[page] != a.pageToRow[a.page]) {
		return 0, 0, false
	}
	x, y, ok := a.pageScreenOrigin(page)
	if !ok {
		return 0, 0, false
	}
	dx, dy := a.pageTransform(page).toScreen(pt.X, pt.Y)
	ox, oy := a.viewportOrigin()
	return ox + x + dx, oy + y + dy, true
}

// luaViewGeometry places a plugin's view. A docked view fills its place.
// Another takes its size, at most the document's part of the window, and
// sits at its anchor there, a gap from the edges it is anchored to, or
// with its top left at the point it is pinned to. A list fits its panel
// to its rows, keeping the edge it is anchored to, or a centred one its
// top, so filtering leaves the header in place.
func (a *App) luaViewGeometry(view *uiView, fit bool) (sdl.FRect, int) {
	p := &view.place
	rowHeight, head, bottom := a.modalListRowHeight(), a.uiViewHeadHeight(view), a.modalListBottomPadding()
	if p.dock != "" {
		_, rect := a.dockLayout(view)
		return rect, max(1, (int(rect.H)-head-bottom)/rowHeight)
	}
	area := a.documentRect()
	gap := 0
	if p.at == nil && p.anchor != "center" {
		gap = a.ipx(12)
	}
	areaW, areaH := int(area.W)-2*gap, int(area.H)-2*gap
	w := clampInt(int(math.Round(a.uiLength(p.width, float64(a.winW)))), 1, max(1, areaW))
	h := clampInt(int(math.Round(a.uiLength(p.height, float64(a.winH)))), 1, max(1, areaH))
	if fit {
		// a list keeps the least size of a menu
		w = max(w, min(260, areaW))
		h = max(h, min(160, areaH))
	}
	rows := max(1, (h-head-bottom)/rowHeight)
	full := h
	if fit {
		full = min(h, head+rows*rowHeight+bottom)
		rows = min(rows, max(1, len(view.visibleRows())))
		h = min(full, head+rows*rowHeight+bottom)
	}
	if p.at != nil {
		x, y, ok := a.uiPointOnScreen(*p.at)
		if !ok {
			x, y = -1e6, -1e6 // out of sight, with its page
		}
		return sdl.FRect{X: float32(math.Round(x)), Y: float32(math.Round(y)), W: float32(w), H: float32(h)}, rows
	}
	x, y := int(area.X)+(int(area.W)-w)/2, int(area.Y)+(int(area.H)-full)/2
	if strings.HasSuffix(p.anchor, "left") {
		x = int(area.X) + gap
	} else if strings.HasSuffix(p.anchor, "right") {
		x = int(area.X+area.W) - w - gap
	}
	if strings.HasPrefix(p.anchor, "top") {
		y = int(area.Y) + gap
	} else if strings.HasPrefix(p.anchor, "bottom") {
		y = int(area.Y+area.H) - h - gap
	}
	return sdl.FRect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, rows
}

// drawLuaViewBody draws a view's body with its plugin's draw function,
// canvas:default() drawing its rows. What it draws below the body scrolls
// into it. If the function fails, the error is reported once, the rows
// are drawn in its place, and it is not called again until the
// configuration changes. A function that returns true is drawn again on
// the next frame.
func (a *App) drawLuaViewBody(view *uiView, callback string, renderer *sdl.Renderer, body sdl.FRect, drawRows func() error) error {
	if a.themeErrors[view] {
		return drawRows()
	}
	// The body is drawn at full strength, the panel's opacity being for
	// the panel's own box.
	st := a.style(config.ElementPanel)
	st.Opacity.V = 1
	var rowsErr error
	box := pixelRect(body)
	box.Y -= int32(math.Round(view.scrollPx))
	canvas := &elementCanvas{
		app: a, renderer: renderer, style: &st, box: box, drawDefault: func() { rowsErr = drawRows() },
		view: view, pointerOver: a.viewUnderPointer() == view,
	}
	view.regions = view.regions[:0]
	view.fieldOrder = view.fieldOrder[:0]
	scale := float64(a.px(1))
	state := config.DrawState{
		Element: config.ElementPanel,
		X:       float64(body.X) / scale, Y: float64(body.Y) / scale,
		W: float64(body.W) / scale, H: float64(body.H) / scale,
		Alt:    a.altColors,
		Time:   time.Since(startTime).Seconds(),
		Scroll: view.scrollPx / scale,
	}
	err := a.withClip(renderer, body, func() error {
		again, err := a.runtime.RunUIDraw(callback, canvas, state)
		canvas.Unclip()
		if again {
			a.pendingRedraw = true
		}
		return err
	})
	if err != nil {
		a.reportThemeError(view, "ui draw", err)
		canvas.Default()
	}
	view.extent = canvas.extent
	if overflow := view.extent - float64(body.H); overflow > 0 {
		view.scrollPx = clampFloat(view.scrollPx, 0, overflow)
		thumb := sdl.FRect{Y: body.Y + float32(view.scrollPx/view.extent)*body.H, H: body.H * body.H / float32(view.extent)}
		a.drawScrollbarThumb(renderer, body, thumb)
	} else {
		view.scrollPx = 0
	}
	return rowsErr
}

// scrollLuaViewBy scrolls what a view's draw function draws by delta
// output pixels, reporting whether it had that to scroll.
func (a *App) scrollLuaViewBy(view *uiView, delta float64) bool {
	overflow := view.extent - float64(a.uiViewBody(view).H)
	if view.drawBody == nil || overflow <= 0 {
		return false
	}
	view.scrollPx = clampFloat(view.scrollPx+delta, 0, overflow)
	a.pendingRedraw = true
	return true
}

// luaViewScrollAction scrolls a view's drawn body for the actions that
// move a list, reporting whether action was one.
func (a *App) luaViewScrollAction(view *uiView, action string) bool {
	if len(view.rows) > 0 {
		return false
	}
	line, page := float64(3*a.uiLineHeight()), float64(a.uiViewBody(view).H)
	delta := map[string]float64{
		"scroll_down": line, "scroll_up": -line, "next_page": page, "prev_page": -page,
		"last_page": view.extent, "first_page": -view.extent,
	}
	d, ok := delta[action]
	return ok && a.scrollLuaViewBy(view, d)
}

// setLuaViewMouse gives view's pointer events in its frame to the plugin's
// on_mouse first, and those after a press in it until the release. The
// view hears when the pointer leaves it, and events on_mouse does not
// take go to the list, or scroll what the view draws. A view that draws
// is drawn again as the pointer moves over it, for its regions.
func (a *App) setLuaViewMouse(view *uiView, callback string) {
	pressed, hovering := false, false
	run := func(a *App, ev config.UIMouseEvent) bool {
		if callback == "" {
			return false
		}
		handled, err := a.runtime.RunUIMouse(callback, ev)
		a.afterLuaViewCallback("ui_mouse", err)
		return handled
	}
	view.onMouseButton = func(a *App, e *sdl.MouseButtonEvent) bool {
		ev, inside := a.luaViewMouseEvent(view, e.X, e.Y)
		down := e.Type == sdl.EventMouseButtonDown
		if down && inside && e.Button == uint8(sdl.ButtonLeft) && a.pressUIField(view, e.X, e.Y) {
			return true
		}
		if down && inside || !down && pressed {
			pressed = down
			ev.Type = "release"
			ev.Button, _ = mouseButtonName(e.Button)
			if down {
				ev.Type, ev.Clicks = "press", int(e.Clicks)
			}
			if run(a, ev) {
				return true
			}
		}
		return a.handleGenericUIViewMouseButton(view, e)
	}
	view.onMouseMotion = func(a *App, e *sdl.MouseMotionEvent) bool {
		ev, inside := a.luaViewMouseEvent(view, e.X, e.Y)
		left := hovering && !inside && !pressed
		hovering = inside
		redraw := view.drawBody != nil && (inside || left)
		if inside || pressed {
			ev.Type = "move"
			if run(a, ev) {
				return true
			}
		} else if left {
			ev.Type = "leave"
			run(a, ev)
		}
		return a.handleGenericUIViewMouseMotion(view, e) || redraw
	}
	view.onWheel = func(a *App, e *sdl.MouseWheelEvent) bool {
		ev, inside := a.luaViewMouseEvent(view, e.MouseX, e.MouseY)
		if !inside {
			return false
		}
		wx, wy := normalizedWheelDeltas(e)
		ev.Type, ev.DX, ev.DY = "wheel", float64(wx), -float64(wy)
		return run(a, ev) || a.scrollLuaViewBy(view, -float64(wy)*float64(3*a.uiLineHeight()))
	}
}

// luaViewMouseEvent is the pointer at x, y in the window as on_mouse is
// told of it, in logical pixels from the top left of what view's body
// draws, scrolled, with the region there, and whether it is in view's
// frame.
func (a *App) luaViewMouseEvent(view *uiView, x, y float32) (config.UIMouseEvent, bool) {
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
// after a press elsewhere is left to the document, whose drag it ends.
func (a *App) handlePanelMouseButton(e *sdl.MouseButtonEvent) bool {
	var view *uiView
	if e.Type == sdl.EventMouseButtonUp {
		view, a.views.pressed = a.views.pressed, nil
	} else {
		view = a.panelAt(e.X, e.Y)
		a.views.pressed = view
		// A press elsewhere takes the keys back from the panel that had
		// them; one on a panel that takes keys gives them to it.
		if focused := a.views.focused; focused != nil && focused != view {
			a.focusUIView(focused, false)
		}
		if view != nil && (view.takesKeys || len(view.fieldOrder) > 0) {
			a.focusUIView(view, true)
		}
	}
	if view == nil {
		return false
	}
	if view.onMouseButton != nil {
		view.onMouseButton(a, e)
	}
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
	if old := a.views.hovered; old != nil && old != view && old.visible && old.onMouseMotion != nil {
		redraw = old.onMouseMotion(a, e)
	}
	a.views.hovered = view
	if view == nil {
		return false, redraw
	}
	if view.onMouseMotion != nil {
		redraw = view.onMouseMotion(a, e) || redraw
	}
	return true, redraw
}

// handlePanelMouseWheel gives a wheel event over a panel to the panel,
// reporting whether one took it.
func (a *App) handlePanelMouseWheel(e *sdl.MouseWheelEvent) bool {
	view := a.panelAt(e.MouseX, e.MouseY)
	if view == nil {
		return false
	}
	if view.onWheel == nil || !view.onWheel(a, e) {
		a.scrollUIViewByWheel(view, e)
	}
	return true
}
