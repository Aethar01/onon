package viewer

import "onon/internal/menubar"

// On systems with a menu bar, the viewer shows the one menubar describes,
// each item with a key bound to its action. setNativeMenuBar draws it again
// whenever the bindings change, and the items chosen go through
// chooseSystemAction. When a menu opens, the system's menu code asks
// menuItemState how to show each item.

func (a *App) installMenuBar() {
	a.menuBarShown = true
	a.updateMenuBar()
}

func (a *App) updateMenuBar() {
	if a.menuBarShown {
		setNativeMenuBar(menubar.Menus(), a.sequenceLookup)
	}
}

// menuItemState reports whether an item running action can be chosen now,
// and whether it is checked, as a setting that is on or a choice in effect.
func (a *App) menuItemState(action string) (enabled, checked bool) {
	enabled = a.pageCount > 0 || menuItemsWithoutDocument[action]
	if can, ok := menuItemEnabled[action]; ok {
		enabled = enabled && can(a)
	}
	for _, toggle := range toggleCommands {
		if toggle.action == action {
			return enabled, toggle.on(a)
		}
	}
	if on, ok := menuItemChecked[action]; ok {
		checked = on(a)
	}
	return enabled, checked
}

// menuItemsWithoutDocument are the actions that do something with no
// document open; the others are disabled then.
var menuItemsWithoutDocument = map[string]bool{
	"quit":              true,
	"new_tab":           true,
	"close_tab":         true,
	"next_tab":          true,
	"prev_tab":          true,
	"tabs_menu":         true,
	"open_file_picker":  true,
	"show_recent_files": true,
	"help":              true,
	"keybinds":          true,
	"options_menu":      true,
	"open_config":       true,
	"cut":               true,
	"copy":              true,
	"paste":             true,
	"toggle_fullscreen": true,
	"toggle_status_bar": true,
	"toggle_tab_bar":    true,
	"toggle_alt_colors": true,
}

// menuItemEnabled holds the actions that have something to act on only
// some of the time.
var menuItemEnabled = map[string]func(*App) bool{
	"save":           func(a *App) bool { return a.unsaved },
	"next_tab":       func(a *App) bool { return len(a.userTabs()) > 1 },
	"prev_tab":       func(a *App) bool { return len(a.userTabs()) > 1 },
	"undo":           func(a *App) bool { return a.editPos > 0 },
	"redo":           func(a *App) bool { return a.editPos < a.editEnd },
	"search_next":    func(a *App) bool { return len(a.search.order) > 0 },
	"search_prev":    func(a *App) bool { return len(a.search.order) > 0 },
	"search_matches": func(a *App) bool { return len(a.search.order) > 0 },
	"jump_backward":  func(a *App) bool { return len(a.jumpBack) > 0 },
	"jump_forward":   func(a *App) bool { return len(a.jumpAhead) > 0 },
}

// menuItemChecked holds the actions, besides those with a toggle command,
// that turn something on or pick one of several.
var menuItemChecked = map[string]func(*App) bool{
	"toggle_render_mode": func(a *App) bool { return a.renderMode == renderContinuous },
	"toggle_alt_colors":  func(a *App) bool { return a.altColors },
	"fit_width":          func(a *App) bool { return a.fitMode == fitWidth },
	"fit_page":           func(a *App) bool { return a.fitMode == fitPage },
	"fit_height":         func(a *App) bool { return a.fitMode == fitHeight },
	"overview":           func(a *App) bool { return a.overview != nil },
	"presentation":       func(a *App) bool { return a.presentation != nil },
}
