package viewer

import (
	"testing"

	"onon/internal/keys"
)

func TestTextEditKeysAreCanonical(t *testing.T) {
	tables := map[string]map[string]textEdit{"every system": textEditKeys}
	for goos, table := range platformTextEditKeys {
		tables[goos] = table
	}
	for name, table := range tables {
		for key := range table {
			if got, err := keys.Normalize(key); err != nil || got != key {
				t.Errorf("%s edits with %q, which normalizes to %q, %v", name, key, got, err)
			}
		}
	}
}

func TestTextEditKeysPerSystem(t *testing.T) {
	tests := []struct {
		goos, key   string
		value, want string
		cursor      int
	}{
		{goos: "linux", key: "<C-Left>", value: "one two", cursor: 7, want: "one |two"},
		{goos: "linux", key: "<Home>", value: "one two", cursor: 7, want: "|one two"},
		{goos: "linux", key: "<C-Del>", value: "one two", cursor: 0, want: "| two"},
		{goos: "darwin", key: "<A-Left>", value: "one two", cursor: 7, want: "one |two"},
		{goos: "darwin", key: "<D-Left>", value: "one two", cursor: 5, want: "|one two"},
		{goos: "darwin", key: "<D-Right>", value: "one two", cursor: 0, want: "one two|"},
		{goos: "darwin", key: "<A-BS>", value: "one two", cursor: 7, want: "one |"},
		{goos: "darwin", key: "<D-BS>", value: "one two", cursor: 4, want: "|two"},
		{goos: "darwin", key: "<C-k>", value: "one two", cursor: 3, want: "one|"},
		{goos: "darwin", key: "<C-Left>", value: "one two", cursor: 7, want: "one |two"},
		{goos: "darwin", key: "<C-a>", value: "one two", cursor: 4, want: "|one two"},
	}
	for _, tt := range tests {
		edit, ok := textEditFor(tt.goos, tt.key)
		if !ok {
			t.Errorf("%s has no edit for %s", tt.goos, tt.key)
			continue
		}
		app := &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}
		app.input.Set(tt.value)
		app.input.SetCursor(tt.cursor, false)
		edit(app)
		left, right := splitAtRune(app.input.Value, app.input.Cursor)
		if got := left + "|" + right; got != tt.want {
			t.Errorf("%s %s on %q: got %q, want %q", tt.goos, tt.key, tt.value, got, tt.want)
		}
	}
	for goos, key := range map[string]string{"darwin": "<D-a>", "windows": "<C-a>", "linux": "<C-a>"} {
		edit, ok := textEditFor(goos, key)
		if !ok {
			t.Errorf("%s has no select all on %s", goos, key)
			continue
		}
		app := &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}
		app.input.Set("one two")
		edit(app)
		if got, ok := app.input.SelectedText(); !ok || got != "one two" {
			t.Errorf("%s %s selects %q", goos, key, got)
		}
	}
	for _, key := range []string{"<A-Left>", "<D-BS>", "<C-k>"} {
		if _, ok := textEditFor("linux", key); ok {
			t.Errorf("Linux edits with the macOS key %s", key)
		}
	}
}
