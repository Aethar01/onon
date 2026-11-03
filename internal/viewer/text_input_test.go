package viewer

import (
	"testing"
)

func TestInputEditingKeepsRuneCursorPositions(t *testing.T) {
	app := &App{tab: &tab{}, inputState: inputState{input: textInput{Value: "ab", Cursor: 1}}}
	app.input.InsertRune('界')
	if app.input.Value != "a界b" || app.input.Cursor != 2 {
		t.Fatalf("expected rune inserted at cursor, input=%q cursor=%d", app.input.Value, app.input.Cursor)
	}

	app.input.Move(10)
	if app.input.Cursor != 3 {
		t.Fatalf("expected cursor to clamp to rune length, got %d", app.input.Cursor)
	}
	app.input.Move(-2)
	if app.input.Cursor != 1 {
		t.Fatalf("expected cursor to move left by runes, got %d", app.input.Cursor)
	}

	app.input.Backspace()
	if app.input.Value != "界b" || app.input.Cursor != 0 {
		t.Fatalf("expected backspace to remove previous rune, input=%q cursor=%d", app.input.Value, app.input.Cursor)
	}

	app.input.Backspace()
	if app.input.Value != "界b" || app.input.Cursor != 0 {
		t.Fatalf("expected backspace at start to be a no-op, input=%q cursor=%d", app.input.Value, app.input.Cursor)
	}
}
