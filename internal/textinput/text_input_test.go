package textinput

import "testing"

func TestInputEditingKeepsRuneCursorPositions(t *testing.T) {
	in := Input{Value: "ab", Cursor: 1}
	in.InsertRune('界')
	if in.Value != "a界b" || in.Cursor != 2 {
		t.Fatalf("expected rune inserted at cursor, input=%q cursor=%d", in.Value, in.Cursor)
	}

	in.Move(10)
	if in.Cursor != 3 {
		t.Fatalf("expected cursor to clamp to rune length, got %d", in.Cursor)
	}
	in.Move(-2)
	if in.Cursor != 1 {
		t.Fatalf("expected cursor to move left by runes, got %d", in.Cursor)
	}

	in.Backspace()
	if in.Value != "界b" || in.Cursor != 0 {
		t.Fatalf("expected backspace to remove previous rune, input=%q cursor=%d", in.Value, in.Cursor)
	}

	in.Backspace()
	if in.Value != "界b" || in.Cursor != 0 {
		t.Fatalf("expected backspace at start to be a no-op, input=%q cursor=%d", in.Value, in.Cursor)
	}
}
