package viewer

import (
	"testing"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font/basicfont"
)

func TestLuaUIButtonsAndCheckboxes(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	app.sequenceLookup = map[string]string{}
	evalLua(t, app, `
agree = false
view = onon.ui.create({
  draw = function(canvas, box)
    local h = canvas:input("name", 0, 0, box.w)
    canvas:checkbox("agree", 0, h + 4, "I agree", agree)
    canvas:button("ok", 0, h + 30, 80, "OK")
  end,
  on_click = function(id)
    if id == "agree" then agree = not agree end
    onon.message(id .. " " .. tostring(agree))
  end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	if got := view.focusOrder; len(got) != 3 || got[0] != "name" || got[1] != "agree" || got[2] != "ok" {
		t.Fatalf("focus order %q", got)
	}
	ok := view.controls["ok"]
	press := func(typ sdl.EventType, r sdl.FRect) {
		app.viewMouseButton(view, &sdl.MouseButtonEvent{Type: typ, Button: uint8(sdl.ButtonLeft), X: r.X + 2, Y: r.Y + 2})
	}
	press(sdl.EventMouseButtonDown, ok)
	press(sdl.EventMouseButtonUp, ok)
	if app.message != "ok false" || view.focus != "ok" {
		t.Fatalf("clicking OK: message %q, focus %q", app.message, view.focus)
	}
	// A release away from the button pressed does not click it.
	app.message = ""
	press(sdl.EventMouseButtonDown, ok)
	press(sdl.EventMouseButtonUp, sdl.FRect{X: 1, Y: 1})
	if app.message != "" {
		t.Fatalf("a release away clicked: %q", app.message)
	}
	// Shift-Tab from OK reaches the check box, which Space checks.
	app.viewKey(view, keyDown(sdl.KeycodeTab, sdl.KeymodShift))
	app.viewKey(view, keyDown(sdl.KeycodeSpace, 0))
	if view.focus != "agree" || app.message != "agree true" {
		t.Fatalf("Space on the check box: focus %q, message %q", view.focus, app.message)
	}
	// Tab round to the field, where Space types.
	app.viewKey(view, keyDown(sdl.KeycodeTab, 0))
	app.viewKey(view, keyDown(sdl.KeycodeTab, 0))
	app.viewKey(view, keyDown(sdl.KeycodeSpace, 0))
	if view.focus != "name" || app.message != "agree true" {
		t.Fatalf("Space in the field clicked something: focus %q, message %q", view.focus, app.message)
	}
	if !app.typeUIText(" ") || view.fields["name"].input.Value != " " {
		t.Fatal("the field focused by Tab did not take typing")
	}
}

func TestLuaUIRowsCheckboxesAndColumns(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
onon.ui.create({
  columns = { { title = "Name" }, { title = "Size", width = 80, align = "right" } },
  rows = {
    { cells = { "a.pdf", "1 MB" }, checked = true },
    { cells = { "b.pdf", "2 MB" }, checked = false },
    { text = "Done", button = true },
  },
  on_select = function(_, value) onon.message("chose " .. value) end,
}):show()`)
	view := app.activeUIView()
	a, b, done := view.rows[0], view.rows[1], view.rows[2]
	if !a.checkbox || !a.checked || !b.checkbox || b.checked || !done.button {
		t.Fatalf("rows %+v", view.rows)
	}
	if a.value != "a.pdf" || len(a.cells) != 2 {
		t.Fatalf("a row of cells %+v", a)
	}
	view.query = "b.p"
	if rows := view.visibleRows(); len(rows) != 1 || rows[0].value != "b.pdf" {
		t.Fatalf("searching cells found %+v", rows)
	}
	view.query = ""
	if head, plain := app.uiViewHeadHeight(view), app.modalListHeadHeight(); head != plain+app.modalListRowHeight() {
		t.Fatalf("head with column titles = %d, without %d", head, plain)
	}
	spans := app.columnSpans(view, 0, 400)
	if spans[1][1] != 80 || spans[0][1] != 400-12-80 || spans[1][0] != 400-80 {
		t.Fatalf("column spans %v", spans)
	}
}
