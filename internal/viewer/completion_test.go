package viewer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"onon/internal/config"
)

func TestShowCompletionAcceptsUniqueCommand(t *testing.T) {
	a := &App{inputState: inputState{mode: modeCommand, input: textInput{Value: "qui", Cursor: 3}}, config: config.Default()}
	a.showCompletion()

	if a.input.Value != "quit" {
		t.Fatalf("expected quit completion, got %q", a.input.Value)
	}
	if a.completion.view != nil && a.completion.view.visible {
		t.Fatal("expected unique completion to close menu")
	}
}

func TestShowCompletionCyclesCommandMenu(t *testing.T) {
	a := &App{inputState: inputState{mode: modeCommand}, config: config.Default()}
	a.showCompletion()

	if a.completion.view == nil || !a.completion.view.visible {
		t.Fatal("expected command completion menu")
	}
	first := a.completion.items[a.completion.view.selected].value
	a.showCompletion()
	if got := a.completion.items[a.completion.view.selected].value; got == first {
		t.Fatalf("expected show_completion to cycle, still selected %q", got)
	}
	a.moveCompletion(-1)
	if got := a.completion.items[a.completion.view.selected].value; got != first {
		t.Fatalf("expected previous completion %q, got %q", first, got)
	}
}

func TestOpenPathCompletionsUseDocumentDirectory(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "docs"))
	mustWrite(t, filepath.Join(dir, "docs", "book.pdf"))
	mustWrite(t, filepath.Join(dir, "current.pdf"))
	mustWrite(t, filepath.Join(dir, "paper.pdf"))
	mustWrite(t, filepath.Join(dir, "notes.txt"))

	a := &App{documentState: documentState{docPath: filepath.Join(dir, "current.pdf")}}
	items := a.openPathCompletions("")
	got := completionValues(items)
	want := []string{"docs" + pathSeparator(), "current.pdf", "notes.txt", "paper.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	items = a.openPathCompletions("docs" + pathSeparator())
	got = completionValues(items)
	want = []string{filepath.Join("docs", "book.pdf")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestOpenPathCompletionsPreserveRelativePrefixes(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "sub"))
	mustMkdir(t, filepath.Join(dir, "parent"))
	mustWrite(t, filepath.Join(dir, "sub", "file.pdf"))
	mustWrite(t, filepath.Join(dir, "parent", "paper.pdf"))

	a := &App{documentState: documentState{docPath: filepath.Join(dir, "sub", "current.pdf")}}
	items := a.openPathCompletions("." + pathSeparator())
	got := completionValues(items)
	want := []string{"." + pathSeparator() + "file.pdf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	items = a.openPathCompletions(".." + pathSeparator())
	got = completionValues(items)
	want = []string{filepath.Join("..", "parent") + pathSeparator(), filepath.Join("..", "sub") + pathSeparator()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestOpenPathCompletionsEscapeSpaces(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Lancer - Core Book.pdf"))

	a := &App{documentState: documentState{docPath: filepath.Join(dir, "current.pdf")}}
	items := a.openPathCompletions("Lancer")
	got := completionValues(items)
	want := []string{`Lancer\ -\ Core\ Book.pdf`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}

	items = a.openPathCompletions(`Lancer\ `)
	got = completionValues(items)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected escaped prefix to complete %v, got %v", want, got)
	}
}

func TestDotPathCompletionsAddSeparator(t *testing.T) {
	a := &App{}
	if got := completionValues(a.openPathCompletions(".")); !reflect.DeepEqual(got, []string{"." + pathSeparator()}) {
		t.Fatalf("expected ./ completion, got %v", got)
	}
	if got := completionValues(a.openPathCompletions("..")); !reflect.DeepEqual(got, []string{".." + pathSeparator()}) {
		t.Fatalf("expected ../ completion, got %v", got)
	}
}

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home directory unavailable")
	}
	if got := expandHomePath("~"); got != home {
		t.Fatalf("expected %q, got %q", home, got)
	}
	if got := expandHomePath(filepath.Join("~", "paper.pdf")); got != filepath.Join(home, "paper.pdf") {
		t.Fatalf("expected home-relative path, got %q", got)
	}
	abs := filepath.Join(string(filepath.Separator), "tmp", "paper.pdf")
	if got := expandHomePath(abs); got != abs {
		t.Fatalf("expected absolute path unchanged, got %q", got)
	}
}

func TestExpandHomePathAcceptsSlashSeparator(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home directory unavailable")
	}
	if got := expandHomePath(filepath.ToSlash(filepath.Join("~", "paper.pdf"))); got != filepath.Join(home, "paper.pdf") {
		t.Fatalf("expected home-relative path, got %q", got)
	}
}

func TestDeleteInputWord(t *testing.T) {
	a := &App{inputState: inputState{input: textInput{Value: "open ../some file.pdf", Cursor: len([]rune("open ../some file"))}}}
	a.input.DeleteWordLeft()
	if a.input.Value != "open ../some .pdf" || a.input.Cursor != len([]rune("open ../some ")) {
		t.Fatalf("expected previous word deleted, input=%q cursor=%d", a.input.Value, a.input.Cursor)
	}

	a.input.Set("open ../some   ")
	a.input.DeleteWordLeft()
	if a.input.Value != "open " || a.input.Cursor != len([]rune("open ")) {
		t.Fatalf("expected word and trailing spaces deleted, input=%q cursor=%d", a.input.Value, a.input.Cursor)
	}
}

func completionValues(items []completionItem) []string {
	values := make([]string, len(items))
	for i, item := range items {
		values[i] = item.value
	}
	return values
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionAcceptCloseAndRows(t *testing.T) {
	items := []completionItem{
		{display: "one", value: "one"},
		{display: "paper.pdf", value: "paper.pdf"},
	}
	app := &App{
		inputState: inputState{input: textInput{Value: "open par", Cursor: 8}},
		config:     config.Config{CompletionMaxItems: 3},
		uiState: uiState{completion: completionState{
			view:  &uiView{visible: true, selected: 1, rows: completionRowsFor(items)},
			start: 5,
			end:   8,
			items: items,
		}},
	}
	app.acceptCompletion()
	if app.input.Value != "open paper.pdf" || app.input.Cursor != len([]rune("open paper.pdf")) || app.completion.view != nil || !app.pendingRedraw {
		t.Fatalf("expected selected completion to replace range and close menu, input=%q cursor=%d completion=%+v redraw=%v", app.input.Value, app.input.Cursor, app.completion, app.pendingRedraw)
	}

	app.completion = completionState{view: &uiView{visible: true}, items: []completionItem{{display: "a", value: "a"}}}
	app.pendingRedraw = false
	app.closeCompletion()
	if app.completion.view != nil || len(app.completion.items) != 0 || !app.pendingRedraw {
		t.Fatalf("expected closeCompletion to clear menu and request redraw, completion=%+v redraw=%v", app.completion, app.pendingRedraw)
	}

	// Every completion is a row.
	rows := completionRowsFor([]completionItem{{display: "a"}, {display: "b"}, {display: "c"}})
	if got, want := completionRowTexts(rows), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected completion rows %v, got %v", want, got)
	}
	// Recent ones go under their own heading, which cannot be selected.
	rows = completionRowsFor([]completionItem{{display: "old.pdf", recent: true}, {display: "paper.pdf", recent: true}, {display: "docs" + pathSeparator(), value: "docs" + pathSeparator()}})
	if got, want := completionRowTexts(rows), []string{"Recents:", "  old.pdf", "  paper.pdf", "Suggestions:", "  docs" + pathSeparator()}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected recent completion section %v, got %v", want, got)
	}
	if !rows[0].heading || !rows[0].disabled || !rows[3].heading {
		t.Fatalf("headings are not headings: %+v", rows)
	}
	app.completion = completionState{view: &uiView{visible: true, rows: rows, selected: 2}, items: []completionItem{{}, {}, {}}}
	app.moveCompletion(1)
	if app.completion.view.selected != 4 {
		t.Fatalf("moving past a heading selected row %d, want 4", app.completion.view.selected)
	}
	app.moveCompletion(1)
	if app.completion.view.selected != 1 {
		t.Fatalf("moving past the last row selected row %d, want the first item, 1", app.completion.view.selected)
	}
}

func completionRowTexts(rows []uiRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = strings.Repeat("  ", row.depth) + row.text
	}
	return texts
}

// Cycling through completions quickly, as a held Tab does, scrolls the
// popup smoothly along with the selection, never leaving it behind.
func TestCompletionScrollsWithAFastSelection(t *testing.T) {
	app := testStyleApp("bar")
	app.config.CompletionMaxItems = 5
	app.config.ScrollOff = 1
	items := make([]completionItem, 30)
	for i := range items {
		items[i] = completionItem{display: fmt.Sprintf("item %d", i)}
	}
	view := &uiView{visible: true, headless: true, element: config.ElementCompletion, rows: completionRowsFor(items)}
	app.completion = completionState{view: view, items: items}
	app.placeCompletion()
	if _, visible := view.frameGeometry(app); visible != 5 {
		t.Fatalf("completions show %d rows, want completion_max_items, 5", visible)
	}
	start := time.Now()
	for step := range 40 {
		app.motion.now = start.Add(time.Duration(step) * 30 * time.Millisecond) // key repeat
		app.moveCompletion(1)
		_, visible := view.frameGeometry(app)
		offset := app.listOffset(view, visible, len(view.rows))
		// The list glides behind the selection, which stays in view.
		if selected := view.selected; float64(selected+1) <= offset || float64(selected) >= offset+float64(visible) {
			t.Fatalf("step %d: row %d out of view at offset %v", step, selected, offset)
		}
	}
	// Settled, the selection keeps a row of context.
	app.motion.now = app.motion.now.Add(time.Second)
	_, visible := view.frameGeometry(app)
	offset := app.listOffset(view, visible, len(view.rows))
	selected := view.selected
	if offset != math.Round(offset) || float64(selected) < offset+1 && offset > 0 || float64(selected) > offset+float64(visible)-2 && offset+float64(visible) < float64(len(view.rows)) {
		t.Fatalf("settled at offset %v with row %d selected", offset, selected)
	}
}
