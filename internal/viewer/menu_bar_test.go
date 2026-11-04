package viewer

import (
	"testing"

	"onon/internal/menubar"
)

func TestMenuItemStateFollowsTheViewer(t *testing.T) {
	app := &App{tab: &tab{}}
	if enabled, _ := app.menuItemState("zoom_in"); enabled {
		t.Error("Zoom In is enabled with no document open")
	}
	if enabled, _ := app.menuItemState("open_file_picker"); !enabled {
		t.Error("Open is disabled with no document open")
	}

	app = testLayoutApp(3)
	app.fitMode = fitWidth
	app.dualPage = true
	app.editPos, app.editEnd = 1, 2
	for action, want := range map[string][2]bool{
		"fit_width":        {true, true},
		"fit_page":         {true, false},
		"toggle_dual_page": {true, true},
		"undo":             {true, false},
		"redo":             {true, false},
		"save":             {false, false},
		"search_next":      {false, false},
		"jump_backward":    {false, false},
	} {
		enabled, checked := app.menuItemState(action)
		if enabled != want[0] || checked != want[1] {
			t.Errorf("%s: enabled=%t checked=%t, want %t %t", action, enabled, checked, want[0], want[1])
		}
	}
}

// Each item's state comes from tables keyed by action name, so a renamed
// action would quietly lose its state.
func TestMenuItemStateNamesMenuActions(t *testing.T) {
	inMenus := map[string]bool{}
	for _, menu := range menubar.Menus() {
		for _, item := range menu.Items {
			inMenus[item.Action] = true
		}
	}
	tables := []map[string]bool{menuItemsWithoutDocument}
	for _, names := range []map[string]func(*App) bool{menuItemEnabled, menuItemChecked} {
		set := map[string]bool{}
		for name := range names {
			set[name] = true
		}
		tables = append(tables, set)
	}
	for _, table := range tables {
		for name := range table {
			if !inMenus[name] {
				t.Errorf("menu item state for %q, which no menu item runs", name)
			}
		}
	}
}
