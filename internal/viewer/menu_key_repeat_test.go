package viewer

import (
	"path/filepath"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/keys"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// heldMenu is an open menu of rows and app with bindings, for keys held
// down in it.
func heldMenu(bindings map[string]string) (*App, *uiView) {
	app := testLayoutApp(5)
	app.winW, app.winH = 1000, 800
	view := &uiView{visible: true, modal: true, place: percentPlace(70, 70), rows: uiRowsFromStrings([]string{"a", "b", "c", "d"})}
	app.views.active = view
	app.setKeyBindings(bindings)
	return app, view
}

func TestHeldNavigationRepeatsInMenus(t *testing.T) {
	app, view := heldMenu(map[string]string{"<Down>": "scroll_down", "j": "scroll_down"})
	app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeDown, Repeat: true})
	app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeJ, Repeat: true})
	if view.selected != 2 {
		t.Fatalf("held navigation moved to row %d, want 2", view.selected)
	}
}

func TestHeldPrefixDoesNotRepeatInMenus(t *testing.T) {
	app, view := heldMenu(map[string]string{"j": "scroll_down", "jj": "first_page"})
	app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeJ, Repeat: true})
	if view.selected != 0 {
		t.Fatalf("a held prefix moved to row %d", view.selected)
	}
}

func TestHeldConfirmDoesNotRepeatInMenus(t *testing.T) {
	app, view := heldMenu(map[string]string{"<CR>": "confirm"})
	chosen := 0
	view.onSelect = func(*App, uiRow) { chosen++ }
	app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeReturn, Repeat: true})
	if chosen != 0 {
		t.Fatal("a held Enter chose a row")
	}
}

func TestHeldKeysAreNotCapturedAsKeybinds(t *testing.T) {
	app, view := heldMenu(map[string]string{"<Down>": "scroll_down"})
	app.keybindMenu = keybindMenuState{view: view, capturing: true}
	view.keyHook = func(a *App, key keys.Key, repeat bool) bool { return a.keybindMenuKey(key, repeat) }
	app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeDown, Repeat: true})
	if len(app.keybindMenu.captured) != 0 || view.selected != 0 {
		t.Fatalf("a held key while capturing: captured %q, selected %d", app.keybindMenu.captured, view.selected)
	}
}

func TestUIViewSearchRepeatsBackspace(t *testing.T) {
	view := &uiView{visible: true, modal: true, searchable: true, searching: true, query: "abc"}
	app := &App{uiState: uiState{views: uiManager{active: view}}}
	e := sdl.KeyboardEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventKeyDown}, Key: sdl.KeycodeBackspace, Repeat: true}

	app.handleUIViewSearchKey(view, &e)

	if view.query != "ab" {
		t.Fatalf("expected repeated backspace to edit query, got %q", view.query)
	}
}

func TestPluginUIViewSearchRepeatsBackspace(t *testing.T) {
	view := &uiView{visible: true, modal: true, searchable: true, searching: true, query: "abc"}
	app := &App{uiState: uiState{views: uiManager{active: view}}}
	e := sdl.KeyboardEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventKeyDown}, Key: sdl.KeycodeBackspace, Repeat: true}

	app.viewKey(view, &e)

	if view.query != "ab" {
		t.Fatalf("expected repeated plugin UI backspace to edit query, got %q", view.query)
	}
}

func TestKeybindEditorCapturesChords(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	rt, err := config.Open(filepath.Join(dir, "config.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app, err := New("", rt, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app.toggleKeybindMenu()
	view := app.keybindMenu.view
	if !view.rows[0].button {
		t.Fatalf("the first row is not the new keybind button: %+v", view.rows[0])
	}
	view.selected = 0
	app.activateUIView(view)
	if !app.keybindMenu.selectingAction {
		t.Fatal("the button did not offer the actions")
	}
	for i, row := range view.rows {
		if row.value == "overview" {
			view.selected = i
		}
	}
	app.activateUIView(view)

	start := time.Now()
	for _, key := range []sdl.Keycode{sdl.KeycodeZ, sdl.KeycodeX} {
		app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: key})
	}
	app.finishKeybindCapture(start) // still within the pause: nothing bound yet
	if !app.keybindMenu.capturing {
		t.Fatal("bound before the chord's pause elapsed")
	}
	app.finishKeybindCapture(start.Add(time.Duration(app.config.SequenceTimeoutMS+1) * time.Millisecond))
	if app.keybindMenu.capturing {
		t.Fatal("still capturing after the pause")
	}
	if got := app.config.KeyBindings["zx"]; got != "overview" {
		t.Fatalf("zx bound to %q, want overview; message %q", got, app.message)
	}
}
