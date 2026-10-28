package viewer

import (
	"testing"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestBuiltinModalSelection(t *testing.T) {
	selected := ""
	app := &App{}
	app.showCoreList("test", "Files", []string{"one.pdf", "two.pdf"}, func(value string) { selected = value })
	app.activeUIView().selected = 1
	app.activateUIView(app.activeUIView())
	if selected != "two.pdf" {
		t.Fatalf("expected built-in selection callback, got %q", selected)
	}
}

func TestKeybindViewHasItsHooks(t *testing.T) {
	app := &App{}
	view := app.newKeybindView()
	if view.onSelect == nil || view.onAction == nil {
		t.Fatalf("expected keybind hooks, view=%+v", view)
	}
}

func TestUIViewSkipsDisabledRows(t *testing.T) {
	selected := ""
	app := testLayoutApp(1)
	view := &uiView{
		visible:  true,
		modal:    true,
		selected: 0,
		rows: []uiRow{
			{index: 0, text: "disabled", value: "disabled", disabled: true},
			{index: 1, text: "enabled", value: "enabled"},
		},
		onSelect: func(_ *App, row uiRow) { selected = row.value },
	}
	app.ensureUIViewSelectionVisible(view)
	if view.selected != 1 {
		t.Fatalf("expected disabled row to be skipped, selected=%d", view.selected)
	}
	view.selected = 0
	app.activateUIView(view)
	if selected != "" {
		t.Fatalf("expected disabled row not to activate, got %q", selected)
	}
}

func TestUIViewSearchBindingIgnoresTriggerText(t *testing.T) {
	app := testLayoutApp(1)
	view := &uiView{visible: true, modal: true, searchable: true}
	app.setKeyBindings(map[string]string{"/": "search_prompt"})
	e := sdl.KeyboardEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventKeyDown}, Key: sdl.KeycodeSlash}
	app.viewKey(view, &e)
	if !view.searching || app.ignoreText != "/" {
		t.Fatalf("expected search mode to ignore trigger text, searching=%t ignore=%q", view.searching, app.ignoreText)
	}
}

func TestUIViewCloseClearsFilterBeforeClosing(t *testing.T) {
	app := testLayoutApp(1)
	view := &uiView{visible: true, modal: true, searching: false, query: "filter"}
	app.runUIViewAction(view, "close")
	if !view.visible || view.query != "" {
		t.Fatalf("expected first close to clear filter, visible=%t query=%q", view.visible, view.query)
	}
}

func TestModalWheelRowsHonorsFlippedDirection(t *testing.T) {
	normal := modalWheelRows(&sdl.MouseWheelEvent{Y: 1})
	flipped := modalWheelRows(&sdl.MouseWheelEvent{Y: 1, Direction: sdl.MouseWheelFlipped})
	if normal != -flipped {
		t.Fatalf("expected flipped modal wheel direction, normal=%v flipped=%v", normal, flipped)
	}
}

func TestModalListRowAtUsesRowsBelowHeaderOnly(t *testing.T) {
	app := &App{}
	rect := sdl.FRect{X: 10, Y: 20, W: 200, H: 160}

	if _, ok := app.modalListRowAt(rect, 3, 30, 30, 50, 25); ok {
		t.Fatal("expected click in modal header to miss rows")
	}
	if got, ok := app.modalListRowAt(rect, 3, 30, 30, 50, 55); !ok || got != 0 {
		t.Fatalf("expected first row hit, got row=%d ok=%v", got, ok)
	}
	if got, ok := app.modalListRowAt(rect, 3, 30, 30, 50, 115); !ok || got != 2 {
		t.Fatalf("expected third row hit, got row=%d ok=%v", got, ok)
	}
	if _, ok := app.modalListRowAt(rect, 3, 30, 30, 50, 145); ok {
		t.Fatal("expected click below configured rows to miss")
	}
	if _, ok := app.modalListRowAt(rect, 3, 30, 30, 500, 55); ok {
		t.Fatal("expected click outside modal bounds to miss")
	}
}

func TestVisibleRowsFollowQueryAndRows(t *testing.T) {
	view := &uiView{rows: []uiRow{{text: "alpha"}, {text: "beta"}, {text: "alphabet"}}}
	if got := len(view.visibleRows()); got != 3 {
		t.Fatalf("unfiltered rows = %d, want 3", got)
	}
	view.query = "ALPHA"
	if got := len(view.visibleRows()); got != 2 {
		t.Fatalf("rows matching alpha = %d, want 2", got)
	}
	view.rows = []uiRow{{text: "alpha"}}
	if got := len(view.visibleRows()); got != 1 {
		t.Fatalf("rows matching alpha after replacing rows = %d, want 1", got)
	}
	view.query = "beta"
	if got := len(view.visibleRows()); got != 0 {
		t.Fatalf("rows matching beta = %d, want 0", got)
	}
}

func TestKeyColumnWidthFollowsTheRows(t *testing.T) {
	app := testStyleApp("bar")
	view := &uiView{}
	rows := []uiRow{{text: "a", key: "gg"}, {text: "b"}}
	if got, want := app.keyColumnWidth(view, rows, 1000), measureText(app.fontFace, "gg"); got != want {
		t.Fatalf("key column = %d, want %d", got, want)
	}
	if got := app.keyColumnWidth(view, rows, 5); got != 5 {
		t.Fatalf("key column = %d, want the limit", got)
	}
	rows = []uiRow{{text: "a", key: "ctrl+shift+x"}, {text: "b"}}
	if got, want := app.keyColumnWidth(view, rows, 1000), measureText(app.fontFace, "ctrl+shift+x"); got != want {
		t.Fatalf("key column after new rows = %d, want %d", got, want)
	}
	if got := app.keyColumnWidth(view, rows[1:], 1000); got != 0 {
		t.Fatalf("key column of rows without keys = %d", got)
	}
}
