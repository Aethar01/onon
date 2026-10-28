package viewer

import (
	"fmt"
	"strings"
	"testing"

	commandmeta "onon/internal/commands"

	"golang.org/x/image/font/basicfont"
)

func TestHelpRowsCoverEverySection(t *testing.T) {
	app := testLayoutApp(1)
	app.config.KeyBindings = map[string]string{"j": "scroll_down", "<Down>": "scroll_down"}
	rows := app.helpRows()
	find := func(id, text string) (uiRow, bool) {
		for _, row := range rows {
			if row.id == id && row.text == text {
				return row, true
			}
		}
		return uiRow{}, false
	}
	if row, ok := find(helpRowCommand, ":fit width|height|page|manual"); !ok || row.value != "fit " {
		t.Fatalf("fit command row = %+v, %v", row, ok)
	}
	if row, ok := find(helpRowAction, "scroll_down"); !ok || row.secondary != "<Down> j" {
		t.Fatalf("scroll_down row = %+v, %v", row, ok)
	}
	if row, ok := find(helpRowSearch, `/\<…`); !ok || row.value != `\<` {
		t.Fatalf("word-start modifier row = %+v, %v", row, ok)
	}
	if row, ok := find(helpRowCommand, "fit_mode"); !ok || row.value != "set fit_mode=" {
		t.Fatalf("fit_mode option row = %+v, %v", row, ok)
	}
	for i, row := range rows {
		if row.index != i {
			t.Fatalf("row %d has index %d", i, row.index)
		}
	}
}

func TestActivateHelpRowFillsPrompts(t *testing.T) {
	app := testLayoutApp(1)
	app.activateHelpRow(uiRow{id: helpRowCommand, value: "fit "})
	if app.mode != modeCommand || app.input.Value != "fit " {
		t.Fatalf("command row: mode=%v input=%q", app.mode, app.input.Value)
	}
	app.activateHelpRow(uiRow{id: helpRowSearch, value: `\v`})
	if app.mode != modeSearch || app.input.Value != `\v` {
		t.Fatalf("search row: mode=%v input=%q", app.mode, app.input.Value)
	}
}

func TestDocumentedSearchModifiersParse(t *testing.T) {
	for _, m := range commandmeta.SearchModifiers() {
		if p := parseSearchQuery(m.Modifier+"x", 0); strings.ContainsRune(p.source, '\\') {
			t.Errorf("documented modifier %s is not parsed: %+v", m.Modifier, p)
		}
	}
}

func TestListsPageByScreen(t *testing.T) {
	app := testLayoutApp(1)
	app.winW, app.winH = 1000, 800
	app.fontFace = basicfont.Face7x13
	rows := make([]uiRow, 100)
	for i := range rows {
		rows[i] = uiRow{index: i, text: fmt.Sprint(i)}
	}
	view := app.showRowList("test", "Test", rows, 50, 50, func(*uiView) {})
	_, visible := view.frameGeometry(app)
	app.runUIViewAction(view, "next_page")
	if view.selected != visible {
		t.Fatalf("PgDn selected %d, want %d", view.selected, visible)
	}
	app.runUIViewAction(view, "last_page")
	if view.selected != 99 {
		t.Fatalf("G selected %d", view.selected)
	}
	app.runUIViewAction(view, "prev_page")
	app.runUIViewAction(view, "first_page")
	if view.selected != 0 || app.page != 0 {
		t.Fatalf("gg selected %d (document page %d)", view.selected, app.page)
	}
}

func TestHelpOpensKeybindsOverItself(t *testing.T) {
	app := testStyleApp("bar")
	app.toggleHelp()
	help := app.activeModalUIView()
	help.selected = 3
	app.activateHelpRow(uiRow{id: helpRowKeybinds})
	if view := app.activeModalUIView(); view == nil || view.id != "core:keybinds" {
		t.Fatalf("active view = %+v, want the keybind editor", view)
	}
	app.runUIViewAction(app.activeModalUIView(), "close")
	if view := app.activeModalUIView(); view != help || help.selected != 3 {
		t.Fatalf("closing the editor left %+v, want help at row 3", view)
	}
}

func TestPrinterPickerReturnsToTheDialog(t *testing.T) {
	app := testStyleApp("bar")
	app.showPrintDialog(0)
	dialog := app.activeModalUIView()
	app.pickPrinter([]string{"one", "two"}, func() { app.showPrintDialog(0) })
	app.runUIViewAction(app.activeModalUIView(), "close")
	if app.activeModalUIView() != dialog {
		t.Fatal("closing the printer picker did not return to the print dialog")
	}
}
