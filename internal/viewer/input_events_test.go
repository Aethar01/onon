package viewer

import (
	"testing"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestPasteInputInsertsClipboardAtCursor(t *testing.T) {
	oldGet := sdlGetClipboardText
	t.Cleanup(func() { sdlGetClipboardText = oldGet })
	sdlGetClipboardText = func() string { return "界x" }

	app := &App{tab: &tab{}, inputState: inputState{input: textInput{Value: "ab", Cursor: 1}}}
	app.handleInputEditKey(&sdl.KeyboardEvent{Key: sdl.KeycodeV, Mod: sdl.KeymodCtrl})

	if app.input.Value != "a界xb" || app.input.Cursor != 3 {
		t.Fatalf("expected pasted text at cursor, input=%q cursor=%d", app.input.Value, app.input.Cursor)
	}
}

func TestCommitInputModeExecutesModeSpecificBehavior(t *testing.T) {
	app := testLayoutApp(5)
	app.recomputeLayout(800, 600)
	app.mode = modeGotoPage
	app.input.Set("3")
	app.commitInputMode()
	if app.mode != modeNormal || app.input.Value != "" || app.input.Cursor != 0 || app.page != 2 {
		t.Fatalf("expected goto input to navigate and clear input, mode=%v input=%q cursor=%d page=%d", app.mode, app.input.Value, app.input.Cursor, app.page)
	}

	app = &App{tab: &tab{}, inputState: inputState{mode: modeCommand, input: textInput{Value: "quit", Cursor: 4}}}
	app.commitInputMode()
	if app.mode != modeNormal || !app.quit {
		t.Fatalf("expected command input to run quit and return to normal, mode=%v quit=%v", app.mode, app.quit)
	}

	app = &App{tab: &tab{}, inputState: inputState{mode: modeSearch, input: textInput{Value: "needle", Cursor: 6}, searchInput: searchModeBackward}}
	app.commitInputMode()
	if app.mode != modeNormal || app.search.query != "needle" || app.search.mode != searchModeBackward || app.message != "no document open" {
		t.Fatalf("expected search input to start backward search, mode=%v search=%+v message=%q", app.mode, app.search, app.message)
	}
}
