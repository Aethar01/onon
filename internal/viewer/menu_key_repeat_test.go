package viewer

import (
	"math"
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
	app := &App{tab: &tab{}, uiState: uiState{views: uiManager{active: view}}}
	e := sdl.KeyboardEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventKeyDown}, Key: sdl.KeycodeBackspace, Repeat: true}

	app.handleUIViewSearchKey(view, &e)

	if view.query != "ab" {
		t.Fatalf("expected repeated backspace to edit query, got %q", view.query)
	}
}

func TestPluginUIViewSearchRepeatsBackspace(t *testing.T) {
	view := &uiView{visible: true, modal: true, searchable: true, searching: true, query: "abc"}
	app := &App{tab: &tab{}, uiState: uiState{views: uiManager{active: view}}}
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
	// Its own data directory too, on every platform, so the keys one test
	// binds or deletes, kept in the session database, are not the next's.
	dir := t.TempDir()
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(name, dir)
	}
	// Closed before dir is removed, as cleanups run last first: Windows
	// cannot remove a file still open.
	t.Cleanup(config.CloseSessionDatabase)
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
	app.finishKeybindCapture(app.captureDeadline())
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
	// The default bindings differ by system: take an action that has two
	// keys on this one.
	action := ""
	for _, a := range app.keybindMenu.actions {
		if len(app.actionKeys()[a]) >= 2 {
			action = a
			break
		}
	}
	if action == "" {
		t.Fatal("no action has two keys")
	}
	keys := chooseAction(t, app, action)
	chosen, other := app.keybindMenu.keys[0], app.keybindMenu.keys[1]
	keys.selected = 1
	app.activateUIView(keys)
	keys.selected = 2 // the pointer passes over another row
	code, name := freshKey(chosen, other)
	app.viewKey(keys, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: code})
	app.finishKeybindCapture(app.captureDeadline())
	if got := app.config.KeyBindings[name]; got != action {
		t.Fatalf("%s bound to %q, want %s", name, got, action)
	}
	if _, kept := app.config.KeyBindings[chosen]; kept {
		t.Fatalf("%s kept its binding: the capture added one rather than rebinding", chosen)
	}
	if got := app.config.KeyBindings[other]; got != action {
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
	app.viewKey(keys, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: code})
	app.finishKeybindCapture(app.captureDeadline())
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

// Delete pressed in place of a key, after choosing it, deletes it rather
// than binding Delete.
func TestKeybindEditorDeletesAKeyBeingRebound(t *testing.T) {
	app := testKeybindApp(t)
	keys := chooseAction(t, app, "copy")
	gone := app.keybindMenu.keys[0]
	keys.selected = 1
	app.activateUIView(keys)
	app.viewKey(keys, keyDown(sdl.KeycodeBackspace, 0))
	if _, kept := app.config.KeyBindings[gone]; kept || app.keybindMenu.capturing {
		t.Fatalf("%s still bound (capturing %v) after Backspace in its place", gone, app.keybindMenu.capturing)
	}
	if _, bound := app.config.KeyBindings["<BS>"]; bound {
		t.Fatalf("Backspace was bound in place of %s", gone)
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

func TestKeybindKeysOpenBesideTheirAction(t *testing.T) {
	app := testKeybindApp(t)
	app.winW, app.winH = 1400, 900
	keys := chooseAction(t, app, "help")
	actions := app.keybindMenu.view
	parent, _ := actions.frameGeometry(app)
	frame, _ := keys.frameGeometry(app)
	if frame.X < parent.X+parent.W {
		t.Fatalf("keys at x %v, want right of the actions, which end at %v", frame.X, parent.X+parent.W)
	}
	i := slices.Index(app.keybindMenu.actions, "help")
	row := float64(parent.Y) + float64(app.uiViewHeadHeight(actions)) + (float64(i)-actions.offset)*float64(app.modalListRowHeight())
	first := float64(frame.Y) + float64(app.uiViewHeadHeight(keys))
	if math.Abs(first-row) > 1 {
		t.Fatalf("keys' first row at y %v, want level with help's row at %v", first, row)
	}

	// With no room on the right, they open on the left.
	app.config.Menus = map[string]config.MenuPlacement{"keybinds": {Anchor: "right"}}
	app.placeMenu(actions)
	parent, _ = actions.frameGeometry(app)
	frame, _ = keys.frameGeometry(app)
	if frame.X+frame.W > parent.X {
		t.Fatalf("keys end at x %v, want left of the actions, which start at %v", frame.X+frame.W, parent.X)
	}
}

func TestAttachedViewsTakeTheSideWithRoom(t *testing.T) {
	app := testLayoutApp(5)
	app.winW, app.winH = 1000, 800
	area := app.documentRect()
	place := func(box config.UIBox, side string, w, h int) sdl.FRect {
		rect, ok := app.attachedRect(attachment{box: box, side: side}, area, 0, w, h)
		if !ok {
			t.Fatalf("no place attached to %+v", box)
		}
		return rect
	}
	gap := app.px(6)
	W, H := float64(area.W), float64(area.H)
	if r := place(config.UIBox{X: 100, Y: 100, W: 50, H: 20}, "below", 80, 60); r.Y != 120+gap || r.X != 100 {
		t.Fatalf("below a box with room: %+v", r)
	}
	// Near the bottom, a dropdown opens above its box.
	if r := place(config.UIBox{X: 100, Y: H - 30, W: 50, H: 20}, "below", 80, 60); r.Y+r.H != area.H-30-gap {
		t.Fatalf("below a box near the bottom: %+v, want above it", r)
	}
	// Near the right, a submenu opens on the left.
	if r := place(config.UIBox{X: W - 100, Y: 100, W: 90, H: 20}, "right", 200, 60); r.X+r.W != area.W-100-gap {
		t.Fatalf("right of a box near the right: %+v, want left of it", r)
	}
	// At a point near the corner, a context menu is kept in the window.
	if r := place(config.UIBox{X: W - 10, Y: H - 10}, "below", 200, 100); r.X+r.W != area.W || r.Y+r.H != area.H-10 {
		t.Fatalf("at a point by the corner: %+v", r)
	}
	// With room on neither side, the side with more, kept in the window.
	if r := place(config.UIBox{X: 300, Y: 100, W: 300, H: 20}, "right", 500, 60); r.X+r.W != float32(W) {
		t.Fatalf("with room on neither side: %+v, want on the right, the roomier, at the edge", r)
	}
}

func TestMenusOverMenusOpenBesideTheirRow(t *testing.T) {
	app := testStyleApp("bar")
	app.winW, app.winH = 1200, 800
	under := app.showRowList("print", "Print", uiRowsFromStrings([]string{"Printer", "Pages", "Print"}), 30, 40, func(*uiView) {})
	under.selected = 0
	over := app.showRowListOver("printers", "Printer", uiRowsFromStrings([]string{"a", "b"}), 30, 40, func(*uiView) {})
	u, _ := under.frameGeometry(app)
	o, _ := over.frameGeometry(app)
	if o.X < u.X+u.W {
		t.Fatalf("a menu over a menu at %+v, want beside it at %+v", o, u)
	}

	// The menus option can put a menu opened over another beside it, or
	// back in the window.
	app.config.Menus = map[string]config.MenuPlacement{"printers": {Anchor: "center"}}
	over = app.showRowListOver("printers", "Printer", uiRowsFromStrings([]string{"a", "b"}), 30, 40, func(*uiView) {})
	if over.place.attach != nil || over.place.anchor != "center" {
		t.Fatalf("printers with anchor center placed %+v", over.place)
	}
	app.closeUIView(over, false)
	app.config.Menus = map[string]config.MenuPlacement{"keybinds": {Anchor: config.MenuBeside}}
	app.openKeybindMenu(true)
	if att := app.keybindMenu.view.place.attach; att == nil || att.row != under {
		t.Fatalf("keybinds with anchor beside, over print, placed %+v", app.keybindMenu.view.place)
	}
}

// The keys are wide enough for the hint the selected one shows, which
// is not cut short.
func TestKeybindKeysFitTheirHint(t *testing.T) {
	app := testKeybindApp(t)
	app.winW, app.winH = 1400, 900
	keys := chooseAction(t, app, "help")
	row := keys.rows[1]
	if row.hint == "" {
		t.Fatal("a key row has no hint")
	}
	if got, want := app.listContentWidth(keys), measureText(app.fontFace, row.text)+measureText(app.fontFace, row.hint); got < want {
		t.Fatalf("keys %d wide, under the %d their text and hint take", got, want)
	}
}
