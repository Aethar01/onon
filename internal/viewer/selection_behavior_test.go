package viewer

import (
	"testing"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestGuiClipboardShortcutsUseDModifierBindings(t *testing.T) {
	tests := []struct {
		key  sdl.Keycode
		want string
	}{
		{key: sdl.KeycodeC, want: "<D-c>"},
		{key: sdl.KeycodeX, want: "<D-x>"},
		{key: sdl.KeycodeV, want: "<D-v>"},
	}
	for _, tt := range tests {
		got, ok := keyToken(&sdl.KeyboardEvent{Key: tt.key, Mod: sdl.KeymodGui})
		if !ok || got.String() != tt.want {
			t.Fatalf("keyToken(%v, Command) = %q, %v; want %s, true", tt.key, got, ok, tt.want)
		}
	}
}

func TestTextInputSelectionMovementAndEditing(t *testing.T) {
	var input textInput
	input.Set("hello world")
	input.SetCursor(5, false)
	input.MoveSelecting(-2, true)
	if got, ok := input.SelectedText(); !ok || got != "lo" {
		t.Fatalf("expected selected text lo, got %q, %v", got, ok)
	}
	input.InsertText("XX")
	if input.Value != "helXX world" || input.Cursor != 5 || input.HasSelection() {
		t.Fatalf("expected insertion to replace selection, got value=%q cursor=%d anchor=%d", input.Value, input.Cursor, input.Anchor)
	}

	input.Set("one two three")
	input.SetCursor(13, false)
	input.MoveWordLeftSelecting(true)
	if got, ok := input.SelectedText(); !ok || got != "three" {
		t.Fatalf("expected shift-word selection three, got %q, %v", got, ok)
	}
	input.Backspace()
	if input.Value != "one two " || input.HasSelection() {
		t.Fatalf("expected backspace to delete selection, got %q", input.Value)
	}
}

func TestClipboardEditingMainTextInputUsesSelection(t *testing.T) {
	withTestClipboard(t)
	app := &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}
	app.input.Set("hello")
	app.input.SetCursor(1, false)
	app.input.SetCursor(4, true)

	if !app.copyActiveTextInputToClipboard() || sdlGetClipboardText() != "ell" {
		t.Fatalf("expected copy to place selected input text on clipboard, got %q", sdlGetClipboardText())
	}
	if app.input.Value != "hello" {
		t.Fatalf("copy changed input: %q", app.input.Value)
	}

	if !app.cutActiveTextInputToClipboard() || app.input.Value != "ho" {
		t.Fatalf("expected cut to delete selection, got %q", app.input.Value)
	}
	if sdlGetClipboardText() != "ell" {
		t.Fatalf("expected cut text on clipboard, got %q", sdlGetClipboardText())
	}

	sdlSetClipboardText("paste")
	app.input.Set("hi")
	app.input.SetCursor(1, false)
	if !app.pasteIntoActiveTextInput() || app.input.Value != "hpastei" {
		t.Fatalf("expected cursor-aware paste, got %q", app.input.Value)
	}

	app.input.Set("abcdef")
	app.input.SetCursor(2, false)
	app.input.SetCursor(5, true)
	sdlSetClipboardText("X")
	if !app.pasteIntoActiveTextInput() || app.input.Value != "abXf" {
		t.Fatalf("expected paste to replace selection, got %q", app.input.Value)
	}
}

func TestClipboardCopyWithoutTextInputSelectionIsNoop(t *testing.T) {
	withTestClipboard(t)
	sdlSetClipboardText("existing")
	app := &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}
	app.input.Set("hello")
	if !app.copyActiveTextInputToClipboard() {
		t.Fatal("expected active input to consume copy")
	}
	if got := sdlGetClipboardText(); got != "existing" {
		t.Fatalf("expected clipboard to remain unchanged without selection, got %q", got)
	}
}

func TestClipboardEditingOutlineSearch(t *testing.T) {
	withTestClipboard(t)
	view := &uiView{visible: true, modal: true, searching: true, query: "needle"}
	app := &App{tab: &tab{}, uiState: uiState{views: uiManager{active: view}}}

	if !app.cutActiveTextInputToClipboard() || view.query != "" {
		t.Fatalf("expected cut to clear outline query, got %q", view.query)
	}
	if sdlGetClipboardText() != "needle" {
		t.Fatalf("expected outline query on clipboard, got %q", sdlGetClipboardText())
	}

	sdlSetClipboardText("filter")
	if !app.pasteIntoActiveTextInput() || view.query != "filter" {
		t.Fatalf("expected paste into outline query, got %q", view.query)
	}
}

func TestClipboardEditingPluginUISearch(t *testing.T) {
	withTestClipboard(t)
	view := &uiView{visible: true, modal: true, searching: true, query: "menu"}
	app := &App{tab: &tab{}, uiState: uiState{views: uiManager{active: view}}}

	if !app.copyActiveTextInputToClipboard() || sdlGetClipboardText() != "menu" {
		t.Fatalf("expected Lua UI query on clipboard, got %q", sdlGetClipboardText())
	}
	sdlSetClipboardText(" item")
	if !app.pasteIntoActiveTextInput() || view.query != "menu item" {
		t.Fatalf("expected paste into plugin UI query, got %q", view.query)
	}
}

func TestCopyOnSelectDisabledKeepsSelectionAfterMouseCopyHook(t *testing.T) {
	app := &App{
		tab: &tab{selection: textSelection{
			text:  "selected text",
			parts: []selectionPart{{quads: []mupdf.Quad{{}}}},
		}},
		config: config.Config{CopyOnSelect: false},
	}

	app.copySelectionToClipboard()

	if app.selection.text != "selected text" || len(app.selection.parts) != 1 {
		t.Fatalf("expected selection to persist, got %+v", app.selection)
	}
}

func TestEscapeClearsPersistentSelection(t *testing.T) {
	app := &App{
		tab: &tab{selection: textSelection{
			text:  "selected text",
			parts: []selectionPart{{quads: []mupdf.Quad{{}}}},
		}},
		config: config.Config{CopyOnSelect: false},
	}

	app.closeActiveUI()

	if app.selection.text != "" || !app.selection.empty() {
		t.Fatalf("expected escape to clear selection, got %+v", app.selection)
	}
}

func withTestClipboard(t *testing.T) {
	t.Helper()
	oldSet := sdlSetClipboardText
	oldGet := sdlGetClipboardText
	clipboard := ""
	sdlSetClipboardText = func(text string) bool {
		clipboard = text
		return true
	}
	sdlGetClipboardText = func() string { return clipboard }
	t.Cleanup(func() {
		sdlSetClipboardText = oldSet
		sdlGetClipboardText = oldGet
	})
}
