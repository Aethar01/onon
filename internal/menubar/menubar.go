// Package menubar describes the menu bar shown on systems that have one:
// which actions it offers, in which menus and in what order. Each system's
// own code draws it, showing beside each item a key bound to its action,
// and runs the action when the item is chosen.
package menubar

import (
	"slices"
	"strings"

	"onon/internal/keys"
)

// Role names a menu or item the system provides, such as the Services menu
// or Hide, rather than one that runs an action. A system leaves out the
// roles it has no counterpart for.
type Role string

const (
	// RoleApp is the menu named for the application.
	RoleApp Role = "app"
	// RoleWindow and RoleHelp are the menus the system adds its own window
	// list and help search to.
	RoleWindow Role = "window"
	RoleHelp   Role = "help"

	RoleAbout      Role = "about"
	RoleServices   Role = "services"
	RoleHide       Role = "hide"
	RoleHideOthers Role = "hide_others"
	RoleShowAll    Role = "show_all"
	RoleMinimize   Role = "minimize"
	RoleZoom       Role = "zoom"
	RoleFront      Role = "front"
	// The tab items macOS gives windows that it gathers into tabs.
	RoleMergeWindows    Role = "merge_windows"
	RoleMoveTabToWindow Role = "move_tab_to_window"
	RoleTabOverview     Role = "tab_overview"
	RoleTabBar          Role = "tab_bar"

	// RoleRecentDocuments is the system's list of documents opened lately.
	RoleRecentDocuments Role = "recent_documents"
)

// Item is one entry in a menu: an action, a system item, or, with neither,
// a separator. "{app}" in a title stands for the application's name.
type Item struct {
	Title  string
	Action string
	Role   Role
	// Key is shown in preference to the action's other keys while it is
	// bound to the action, for items that share one, as Close and Quit do.
	Key string
}

// Separator is a line between groups of items.
var Separator = Item{}

// IsSeparator reports whether the item is a separator.
func (i Item) IsSeparator() bool {
	return i.Action == "" && i.Role == ""
}

// Menu is a titled menu in the menu bar.
type Menu struct {
	Title string
	Role  Role
	Items []Item
}

// Menus returns the menu bar, left to right.
func Menus() []Menu {
	return []Menu{
		{Title: "{app}", Role: RoleApp, Items: []Item{
			{Title: "About {app}", Role: RoleAbout},
			Separator,
			{Title: "Settings…", Action: "open_config"},
			Separator,
			{Title: "Services", Role: RoleServices},
			Separator,
			{Title: "Hide {app}", Role: RoleHide},
			{Title: "Hide Others", Role: RoleHideOthers},
			{Title: "Show All", Role: RoleShowAll},
			Separator,
			{Title: "Quit {app}", Action: "quit", Key: "<D-q>"},
		}},
		{Title: "File", Items: []Item{
			{Title: "New Tab", Action: "new_tab"},
			{Title: "Open…", Action: "open_file_picker"},
			{Title: "Open Recent", Role: RoleRecentDocuments},
			{Title: "Recent Files…", Action: "show_recent_files"},
			Separator,
			{Title: "Save", Action: "save"},
			{Title: "Print…", Action: "print"},
		}},
		{Title: "Edit", Items: []Item{
			{Title: "Undo", Action: "undo"},
			{Title: "Redo", Action: "redo"},
			Separator,
			{Title: "Cut", Action: "cut"},
			{Title: "Copy", Action: "copy"},
			{Title: "Paste", Action: "paste"},
			Separator,
			{Title: "Highlight Selection", Action: "highlight_selection"},
			Separator,
			{Title: "Find…", Action: "search_prompt"},
			{Title: "Find Next", Action: "search_next"},
			{Title: "Find Previous", Action: "search_prev"},
			{Title: "All Matches", Action: "search_matches"},
		}},
		{Title: "View", Items: []Item{
			{Title: "Zoom In", Action: "zoom_in"},
			{Title: "Zoom Out", Action: "zoom_out"},
			{Title: "Actual Size", Action: "reset_zoom"},
			Separator,
			{Title: "Fit Width", Action: "fit_width"},
			{Title: "Fit Page", Action: "fit_page"},
			{Title: "Fit Height", Action: "fit_height"},
			Separator,
			{Title: "Rotate Right", Action: "rotate_cw"},
			{Title: "Rotate Left", Action: "rotate_ccw"},
			Separator,
			{Title: "Dual Page", Action: "toggle_dual_page"},
			{Title: "Continuous", Action: "toggle_render_mode"},
			{Title: "Trim Margins", Action: "toggle_trim_margins"},
			{Title: "Alternate Colors", Action: "toggle_alt_colors"},
			{Title: "Status Bar", Action: "toggle_status_bar"},
			{Title: "Tab Bar", Action: "toggle_tab_bar"},
			Separator,
			{Title: "Outline", Action: "outline"},
			{Title: "Page Overview", Action: "overview"},
			{Title: "Presentation", Action: "presentation"},
			{Title: "Full Screen", Action: "toggle_fullscreen"},
		}},
		{Title: "Go", Items: []Item{
			{Title: "Next Page", Action: "next_page"},
			{Title: "Previous Page", Action: "prev_page"},
			{Title: "First Page", Action: "first_page"},
			{Title: "Last Page", Action: "last_page"},
			{Title: "Go to Page…", Action: "goto_page_prompt"},
			Separator,
			{Title: "Back", Action: "jump_backward"},
			{Title: "Forward", Action: "jump_forward"},
			Separator,
			{Title: "Follow Link…", Action: "follow_link"},
		}},
		{Title: "Window", Role: RoleWindow, Items: []Item{
			{Title: "Close Tab", Action: "close_tab", Key: "<D-w>"},
			{Title: "Minimize", Role: RoleMinimize},
			{Title: "Zoom", Role: RoleZoom},
			Separator,
			{Title: "Show Previous Tab", Action: "prev_tab"},
			{Title: "Show Next Tab", Action: "next_tab"},
			{Title: "Tabs…", Action: "tabs_menu"},
			{Title: "Show All Tabs", Role: RoleTabOverview},
			{Title: "Move Tab to New Window", Role: RoleMoveTabToWindow},
			{Title: "Merge All Windows", Role: RoleMergeWindows},
			Separator,
			{Title: "Bring All to Front", Role: RoleFront},
		}},
		{Title: "Help", Role: RoleHelp, Items: []Item{
			{Title: "{app} Help", Action: "help"},
			{Title: "Key Bindings", Action: "keybinds"},
			{Title: "Options", Action: "options_menu"},
		}},
	}
}

// Shortcut returns the key to show beside item, of those bound to its
// action alone: the item's Key while it is bound, else one with all of
// prefer's modifiers if there is one, as a system shows its own shortcuts
// with, then the one with the fewest modifiers. Sequences of more than one
// key cannot be shown.
func Shortcut(bindings map[string]string, item Item, prefer keys.Mod) (keys.Key, bool) {
	if seq, err := keys.ParseSequence(item.Key); err == nil && len(seq) == 1 && bindings[item.Key] == item.Action {
		return seq[0], true
	}
	var candidates []keys.Key
	for binding, bound := range bindings {
		if bound != item.Action {
			continue
		}
		if seq, err := keys.ParseSequence(binding); err == nil && len(seq) == 1 {
			candidates = append(candidates, seq[0])
		}
	}
	if len(candidates) == 0 {
		return keys.Key{}, false
	}
	rank := func(k keys.Key) (bool, int) {
		return k.Mod&prefer != prefer, modCount(k.Mod)
	}
	return slices.MinFunc(candidates, func(x, y keys.Key) int {
		xMissing, xMods := rank(x)
		yMissing, yMods := rank(y)
		switch {
		case xMissing != yMissing:
			if xMissing {
				return 1
			}
			return -1
		case xMods != yMods:
			return xMods - yMods
		}
		return strings.Compare(x.String(), y.String())
	}), true
}

func modCount(m keys.Mod) int {
	n := 0
	for ; m != 0; m &= m - 1 {
		n++
	}
	return n
}
