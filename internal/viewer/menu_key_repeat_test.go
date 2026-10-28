package viewer

import (
	"path/filepath"
	"slices"
	"strings"
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

// testKeybindApp opens the keybind editor of an app with the default
// bindings.
func testKeybindApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	rt, err := config.Open(filepath.Join(dir, "config.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	app, err := New("", rt, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app.toggleKeybindMenu()
	return app
}

// chooseAction opens the keys of action in the keybind editor.
func chooseAction(t *testing.T, app *App, action string) *uiView {
	t.Helper()
	view := app.keybindMenu.view
	view.selected = slices.Index(app.keybindMenu.actions, action)
	if view.selected < 0 {
		t.Fatalf("no row for %s", action)
	}
	app.activateUIView(view)
	if keys := app.activeModalUIView(); keys != app.keybindMenu.keysView || keys == nil {
		t.Fatalf("choosing %s did not open its keys", action)
	}
	return app.keybindMenu.keysView
}

func TestKeybindEditorListsActionsWithTheirKeys(t *testing.T) {
	app := testKeybindApp(t)
	view := app.keybindMenu.view
	copy := view.rows[slices.Index(app.keybindMenu.actions, "copy")]
	if want := strings.Join(app.actionKeys()["copy"], "  "); copy.secondary != want || want == "" {
		t.Fatalf("copy row = %+v, want keys %q", copy, want)
	}
	keys := chooseAction(t, app, "copy")
	if !keys.rows[0].button || len(keys.rows) != len(app.actionKeys()["copy"])+1 {
		t.Fatalf("copy's keys = %+v", keys.rows)
	}
	app.runUIViewAction(keys, "close")
	if app.activeModalUIView() != view {
		t.Fatal("closing an action's keys did not return to the actions")
	}
}

func TestKeybindEditorCapturesChords(t *testing.T) {
	app := testKeybindApp(t)
	keys := chooseAction(t, app, "overview")
	keys.selected = 0 // add a key
	app.activateUIView(keys)
	if !strings.Contains(keys.rows[0].secondary, "press keys") {
		t.Fatalf("the row capturing does not say so: %+v", keys.rows[0])
	}

	start := time.Now()
	for _, key := range []sdl.Keycode{sdl.KeycodeZ, sdl.KeycodeX} {
		app.viewKey(keys, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: key})
	}
	app.finishKeybindCapture(start) // still within the pause: nothing bound yet
	if !app.keybindMenu.capturing || keys.rows[0].secondary != "z x …" {
		t.Fatalf("bound before the chord's pause elapsed, row %+v", keys.rows[0])
	}
	app.finishKeybindCapture(start.Add(time.Duration(app.config.SequenceTimeoutMS+1) * time.Millisecond))
	if app.keybindMenu.capturing {
		t.Fatal("still capturing after the pause")
	}
	if got := app.config.KeyBindings["zx"]; got != "overview" {
		t.Fatalf("zx bound to %q, want overview; message %q", got, app.message)
	}
	if !slices.Contains(app.keybindMenu.keys, "zx") {
		t.Fatalf("the keys shown are %q", app.keybindMenu.keys)
	}
}

// The key capture started on is the one rebound, wherever the pointer
// moves the selection while the keys are pressed.
func TestKeybindCaptureKeepsItsRow(t *testing.T) {
	app := testKeybindApp(t)
	keys := chooseAction(t, app, "copy")
	if len(app.keybindMenu.keys) < 2 {
		t.Fatalf("copy has keys %q, want two", app.keybindMenu.keys)
	}
	chosen, other := app.keybindMenu.keys[0], app.keybindMenu.keys[1]
	keys.selected = 1
	app.activateUIView(keys)
	keys.selected = 2 // the pointer passes over another row
	code, name := freshKey(chosen, other)
	start := time.Now()
	app.viewKey(keys, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: code})
	app.finishKeybindCapture(start.Add(time.Duration(app.config.SequenceTimeoutMS+1) * time.Millisecond))
	if got := app.config.KeyBindings[name]; got != "copy" {
		t.Fatalf("%s bound to %q, want copy", name, got)
	}
	if _, kept := app.config.KeyBindings[chosen]; kept {
		t.Fatalf("%s kept its binding: the capture added one rather than rebinding", chosen)
	}
	if got := app.config.KeyBindings[other]; got != "copy" {
		t.Fatalf("the row passed over lost its binding: %s is %q", other, got)
	}
}

// Choosing another row while capturing, such as the button, leaves the
// capture to the key it began on.
func TestKeybindCaptureIgnoresChoosingWhileCapturing(t *testing.T) {
	app := testKeybindApp(t)
	keys := chooseAction(t, app, "copy")
	chosen := app.keybindMenu.keys[0]
	keys.selected = 1
	app.activateUIView(keys)
	keys.selected = 0 // the button
	app.activateUIView(keys)
	code, name := freshKey(chosen)
	start := time.Now()
	app.viewKey(keys, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: code})
	app.finishKeybindCapture(start.Add(time.Duration(app.config.SequenceTimeoutMS+1) * time.Millisecond))
	if app.config.KeyBindings[name] != "copy" {
		t.Fatalf("%s bound to %q, want copy", name, app.config.KeyBindings[name])
	}
	if _, kept := app.config.KeyBindings[chosen]; kept {
		t.Fatalf("%s kept its binding: the capture added one rather than rebinding", chosen)
	}
}

func TestKeybindEditorDeletesAKey(t *testing.T) {
	app := testKeybindApp(t)
	keys := chooseAction(t, app, "copy")
	gone := app.keybindMenu.keys[0]
	keys.selected = 1
	app.viewKey(keys, keyDown(sdl.KeycodeDelete, 0))
	if _, kept := app.config.KeyBindings[gone]; kept || slices.Contains(app.keybindMenu.keys, gone) {
		t.Fatalf("%s is still bound after Delete", gone)
	}
}

// freshKey is a key to capture that none of keys is.
func freshKey(keys ...string) (sdl.Keycode, string) {
	for _, k := range []struct {
		code sdl.Keycode
		name string
	}{{sdl.KeycodeZ, "z"}, {sdl.KeycodeX, "x"}, {sdl.KeycodeC, "c"}} {
		if !slices.Contains(keys, k.name) {
			return k.code, k.name
		}
	}
	panic("no fresh key")
}
