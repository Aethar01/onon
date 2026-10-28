package viewer

import (
	"strings"
	"testing"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font/basicfont"
)

func lineTexts(lines []textLine) string {
	texts := make([]string, len(lines))
	for i, ln := range lines {
		texts[i] = ln.text
	}
	return strings.Join(texts, "|")
}

func TestLayoutLinesWrapAndKeepEveryRune(t *testing.T) {
	face := basicfont.Face7x13 // 7 pixels a character
	lines := layoutLines(face, "hello world foo\nabcdefghijklmnop", 77, true)
	if got, want := lineTexts(lines), "hello world |foo|abcdefghijk|lmnop"; got != want {
		t.Fatalf("lines %q, want %q", got, want)
	}
	// The lines hold every rune once, a newline between paragraphs.
	next := 0
	for _, ln := range lines {
		if ln.start != next && ln.start != next+1 {
			t.Fatalf("line %q starts at %d after %d", ln.text, ln.start, next)
		}
		next = ln.end
	}
	if next != len([]rune("hello world foo\nabcdefghijklmnop")) {
		t.Fatalf("lines end at %d", next)
	}
	if got := lineTexts(layoutLines(face, "one\n\nthree", 100, true)); got != "one||three" {
		t.Fatalf("an empty paragraph is not a line: %q", got)
	}
}

func TestTextLayoutMovesByLine(t *testing.T) {
	face := basicfont.Face7x13
	l := textLayout{face: face, lines: layoutLines(face, "abcdef\nxy\nlonger line", 200, true), lineHeight: 13}
	if got := l.moveLines(4, 1); got != 9 {
		t.Fatalf("down from column 4 of a long line to a short one = %d, want its end, 9", got)
	}
	if got := l.moveLines(12, -1); got != 9 {
		t.Fatalf("up from column 2 of the third line = %d, want 9", got)
	}
	if got := l.moveLines(2, -1); got != 0 {
		t.Fatalf("up from the first line = %d, want the start", got)
	}
	if got := l.runeAt(3*7+1, 13+2); got != 9 {
		t.Fatalf("the pointer on the second line, past its end = %d, want its end, 9", got)
	}
}

func TestMultilineFieldTakesNewlinesAndLines(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
view = onon.ui.create({
  draw = function(canvas, box) canvas:input("note", 0, 0, box.w, { lines = 3 }) end,
  on_submit = function(text) onon.message("submit " .. text) end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	app.typeUIText("one")
	app.viewKey(view, keyDown(sdl.KeycodeReturn, 0))
	app.typeUIText("two")
	f := view.fields["note"]
	if f.input.Value != "one\ntwo" {
		t.Fatalf("Enter in a field of lines typed %q", f.input.Value)
	}
	drawLuaView(t, app, view) // lay out what was typed
	app.viewKey(view, keyDown(sdl.KeycodeUp, 0))
	if f.input.Cursor != 3 {
		t.Fatalf("Up moved to %d, want the end of the first line, 3", f.input.Cursor)
	}
	app.viewKey(view, keyDown(sdl.KeycodeReturn, sdl.KeymodCtrl))
	if app.message != "submit one\ntwo" {
		t.Fatalf("Ctrl-Enter: %q", app.message)
	}
}

func TestFieldDragSelects(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
view = onon.ui.create({ draw = function(canvas, box) canvas:input("name", 0, 0, box.w) end })
view:set_field("name", "abcdefgh")
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	f := view.fields["name"]
	at := func(i int) float32 { x, _ := f.layout.at(i); return x }
	y := f.layout.y + 2
	app.viewMouseButton(view, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: at(2), Y: y})
	app.viewMouseMotion(view, &sdl.MouseMotionEvent{X: at(5), Y: y, State: sdl.ButtonLMask})
	app.viewMouseButton(view, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: at(6), Y: y})
	if text, _ := f.input.SelectedText(); text != "cdef" || f.input.mouseSelecting {
		t.Fatalf("dragged across the field selected %q, still selecting %v", text, f.input.mouseSelecting)
	}
}

func TestCompositionShowsAtTheCursor(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `onon.ui.create({ prompt = "Find", rows = { "a" } }):show()`)
	view := app.activeUIView()
	app.typeUIText("ab")
	view.promptInput.Cursor = 1
	app.composition = "に"
	if shown, start, n := app.compose(&view.promptInput, view.promptInput.Value); shown != "aにb" || start != 1 || n != 1 {
		t.Fatalf("composing shows %q from %d for %d", shown, start, n)
	}
	// Only the text typed into shows it.
	other := textInput{Value: "zz"}
	if shown, _, _ := app.compose(&other, other.Value); shown != "zz" {
		t.Fatalf("another input shows the composition: %q", shown)
	}
	app.handleSDLTextInput(&sdl.TextInputEvent{})
	if maskText("abc") != "•••" {
		t.Fatalf("mask = %q", maskText("abc"))
	}
}
