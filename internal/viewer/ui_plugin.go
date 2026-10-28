package viewer

import (
	"math"
	"strconv"

	"onon/internal/config"
	"onon/internal/keys"
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
		a.setViewScroll(view, float64(scroll)*float64(a.px(1)))
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
	if view.prompt != "" {
		view.promptInput.Set(query)
	}
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

// createLuaView makes a plugin's view from its spec, its callbacks as the
// view's hooks.
func (a *App) createLuaView(spec config.UIOverlay) *uiView {
	selected := -1
	if len(spec.Rows) > 0 {
		selected = clampInt(spec.Selected-1, 0, len(spec.Rows)-1)
	}
	view := &uiView{
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
		headless:   spec.Draw != "" && spec.Title == "" && spec.Prompt == "",
		element:    config.ElementPanel,
		place:      viewPlacement{width: spec.Width, height: spec.Height, anchor: spec.Anchor, at: spec.At, dock: spec.Dock},
	}
	if view.id == "" {
		view.id = "lua" + strconv.Itoa(len(a.views.views)+1)
	}
	if spec.Empty != "" {
		view.empty = func(*App, *uiView) string { return spec.Empty }
	}
	for id, text := range spec.Fields {
		view.field(id).input.Set(text)
	}
	if spec.Prompt != "" {
		view.prompt = spec.Prompt
		view.promptInput.Set(spec.Query)
		view.searchable = false // typing goes to the prompt, without /
	}
	rt := a.runtime
	if spec.Draw != "" {
		view.drawBody = func(a *App, canvas *elementCanvas, state config.DrawState) (bool, error) {
			return rt.RunUIDraw(spec.Draw, canvas, state)
		}
	}
	if spec.OnKey != "" {
		view.keyHook = func(a *App, key keys.Key, repeat bool) bool {
			handled, err := rt.RunUIKey(spec.OnKey, key.String())
			a.afterLuaViewCallback("ui_key", err)
			return handled
		}
	}
	if spec.OnAction != "" {
		view.onAction = func(a *App, _ *uiView, action string) bool {
			handled, err := rt.RunUIAction(spec.OnAction, action)
			a.afterLuaViewCallback("ui_action", err)
			return handled
		}
	}
	if spec.OnMouse != "" {
		view.mouseHook = func(a *App, ev config.UIMouseEvent) bool {
			handled, err := rt.RunUIMouse(spec.OnMouse, ev)
			a.afterLuaViewCallback("ui_mouse", err)
			return handled
		}
	}
	if spec.OnInput != "" {
		view.onInput = func(a *App, id, text string) {
			a.afterLuaViewCallback("ui_input", rt.RunUIText(spec.OnInput, text, id))
		}
	}
	if spec.OnSubmit != "" {
		view.onSubmit = func(a *App, id, text string) {
			a.afterLuaViewCallback("ui_submit", rt.RunUIText(spec.OnSubmit, text, id))
		}
	}
	if spec.OnSelect != "" {
		view.onSelect = func(a *App, row uiRow) {
			a.afterLuaViewCallback("ui_select", rt.RunUIRow(spec.OnSelect, luaRowRef(row)))
		}
	}
	if spec.OnSelectionChanged != "" {
		view.onSelectionChanged = func(a *App, view *uiView) {
			for _, row := range view.rows {
				if row.index == view.selected {
					a.afterLuaViewCallback("ui_selection_changed", rt.RunUIRow(spec.OnSelectionChanged, luaRowRef(row)))
					return
				}
			}
		}
	}
	if spec.OnQueryChanged != "" {
		view.onQueryChanged = func(a *App, view *uiView) {
			a.afterLuaViewCallback("ui_query_changed", rt.RunUIQuery(spec.OnQueryChanged, view.query))
		}
	}
	if spec.OnClose != "" {
		view.onClose = func(a *App) {
			a.afterLuaViewCallback("ui_close", rt.RunUIClose(spec.OnClose))
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
