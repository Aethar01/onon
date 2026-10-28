package viewer

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"onon/internal/config"

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
    local _, h = canvas:input("name", 0, 0, box.w)
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

func TestResizableDockFollowsItsEdge(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
dock = onon.ui.create({ dock = "left", width = 200, resizable = true, draw = function() end })
dock:show()`)
	view := app.views.panels[0]
	if app.dockEdgeAt(200, 300) != view || app.dockEdgeAt(260, 300) != nil {
		t.Fatal("the dock's edge is not where it ends")
	}
	if app.viewCursor() != cursorDefault {
		t.Fatal("a cursor other than the default away from the edge")
	}
	app.pointer = sdl.FPoint{X: 201, Y: 300}
	if app.viewCursor() != cursorResizeEW {
		t.Fatal("no resize cursor over the edge")
	}
	if !app.handleDockResizeButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: 201, Y: 300}) {
		t.Fatal("a press on the edge did not start dragging it")
	}
	app.dragDockEdge(320, 300)
	app.handleDockResizeButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: 320, Y: 300})
	if doc := app.documentRect(); doc.X != 320 || app.views.resizing != nil {
		t.Fatalf("dragged to 320, the document starts at %v", doc.X)
	}
	evalLua(t, app, `local w, h = dock:size() onon.message(w .. "x" .. h)`)
	if want := "320x" + strconv.Itoa(int(app.documentRect().H)); app.message != want {
		t.Fatalf("dock:size() = %s, want %s", app.message, want)
	}
	// A dock not resizable has no edge to drag.
	evalLua(t, app, `onon.ui.create({ dock = "right", width = 100, draw = function() end }):show()`)
	if app.dockEdgeAt(1000-100, 300) != nil {
		t.Fatal("a dock not resizable has an edge")
	}
}

func TestViewCursorOverControls(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
onon.ui.create({
  draw = function(canvas, box)
    canvas:input("name", 0, 0, box.w)
    canvas:button("ok", 0, 40, 80, "OK")
  end,
}):show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	field, ok := view.fields["name"].rect, view.controls["ok"]
	app.pointer = sdl.FPoint{X: field.X + 2, Y: field.Y + 2}
	if app.viewCursor() != cursorText {
		t.Fatal("no I-beam over a field")
	}
	app.pointer = sdl.FPoint{X: ok.X + 2, Y: ok.Y + 2}
	if app.viewCursor() != cursorPointer {
		t.Fatal("no hand over a button")
	}
}

func TestCanvasImagesReloadAndPagesTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "icon.png")
	write := func(w, h int) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(4, 2)
	app := testLayoutApp(1)
	if img := app.canvasImage(nil, path); img.w != 4 {
		t.Fatalf("image width %d", img.w)
	}
	write(8, 2)
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if img := app.canvasImage(nil, path); img.w != 4 {
		t.Fatal("the image was looked at again within the interval")
	}
	app.canvas.images[path].checked = time.Time{}
	if img := app.canvasImage(nil, path); img.w != 8 {
		t.Fatalf("the changed image was not reloaded: width %d", img.w)
	}

	// A page turned a quarter fits by its turned size.
	app.rotation = 90
	canvas := &elementCanvas{app: app, style: &config.Style{}}
	if w, h := canvas.Page(0, 0, 0, 50, 0, nil); math.Abs(w-50) > 1e-6 || math.Abs(h-25) > 1e-6 {
		t.Fatalf("a page 100 by 200 turned a quarter, 50 wide, is %vx%v", w, h)
	}
}

func TestCanvasPageAreaAtAZoom(t *testing.T) {
	app := luaUITestApp(t, "")
	pages := testLayoutApp(2)
	app.pageMetrics, app.pageCount = pages.pageMetrics, pages.pageCount
	app.relayoutWithViewportAnchor(nil)
	evalLua(t, app, `
view = onon.ui.create({
  draw = function(canvas) canvas:page(2, 0, 0, 100, 50, { x = 10, y = 20, zoom = 2 }) end,
})
view:show()`)
	drawLuaView(t, app, app.activeUIView())
	want := app.renderScaleFor(app.scale) * 2
	if len(app.canvas.tiles) == 0 {
		t.Fatal("drawing part of a page wanted no tiles")
	}
	for key := range app.canvas.tiles {
		if key.page != 1 || key.scale != want {
			t.Fatalf("wanted tile %+v, want page 2's at scale %v", key, want)
		}
	}
	evalLua(t, app, `view:close()
onon.ui.create({ draw = function(canvas) canvas:page(1, 0, 0, 100, nil, { x = 0, y = 0 }) end }):show()`)
	drawLuaView(t, app, app.activeUIView())
	if !strings.Contains(app.message, "needs a height") {
		t.Fatalf("a page area without a height: %q", app.message)
	}
}

func TestPromptAndFieldsShareFocus(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	app.sequenceLookup = map[string]string{}
	evalLua(t, app, `
view = onon.ui.create({
  prompt = "Find",
  draw = function(canvas, box)
    canvas:input("name", 0, 0, box.w)
    canvas:button("go", 0, 30, 60, "Go")
  end,
  on_click = function(id) onon.message("clicked " .. id) end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	if !app.typeUIText("q") || view.promptInput.Value != "q" {
		t.Fatalf("typing went to %q, not the prompt the view opened on", view.focus)
	}
	app.viewKey(view, keyDown(sdl.KeycodeTab, 0))
	drawLuaView(t, app, view)
	app.typeUIText("n")
	if view.fields["name"].input.Value != "n" {
		t.Fatal("Tab from the prompt did not reach the field")
	}
	// A click back on the prompt keeps it, however the body draws.
	frame, _ := view.frameGeometry(app)
	view.promptRect = sdl.FRect{X: frame.X + 60, Y: frame.Y + 2, W: 100, H: 20} // as the header draws it
	r := view.promptRect
	view.promptLayout = textLayout{face: app.fontFace, lines: layoutLines(app.fontFace, "q", 0, false), x: r.X, y: r.Y, lineHeight: 13}
	app.viewMouseButton(view, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: r.X + 1, Y: r.Y + 1})
	app.viewMouseButton(view, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: r.X + 1, Y: r.Y + 1})
	drawLuaView(t, app, view)
	app.typeUIText("r")
	if view.promptInput.Value != "rq" || view.fields["name"].input.Value != "n" { // the click put the cursor at the start
		t.Fatalf("after clicking the prompt: prompt %q, field %q", view.promptInput.Value, view.fields["name"].input.Value)
	}
	// Space on a focused button clicks it, and types nothing anywhere.
	view.focus = "go"
	app.viewKey(view, keyDown(sdl.KeycodeSpace, 0))
	app.handleSDLTextInput(&sdl.TextInputEvent{})
	if app.typeUIText(" ") || view.promptInput.Value != "rq" || app.message != "clicked go" {
		t.Fatalf("Space on a button: prompt %q, message %q", view.promptInput.Value, app.message)
	}
}

func TestOtherButtonsDoNotEndAControlsPress(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
onon.ui.create({
  draw = function(canvas) canvas:button("ok", 0, 0, 80, "OK") end,
  on_click = function(id) onon.message("clicked " .. id) end,
}):show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	r := view.controls["ok"]
	button := func(typ sdl.EventType, b sdl.MouseButtonFlags) {
		app.viewMouseButton(view, &sdl.MouseButtonEvent{Type: typ, Button: uint8(b), X: r.X + 2, Y: r.Y + 2})
	}
	button(sdl.EventMouseButtonDown, sdl.ButtonLeft)
	button(sdl.EventMouseButtonDown, sdl.ButtonRight)
	button(sdl.EventMouseButtonUp, sdl.ButtonRight)
	if !view.pressTaken {
		t.Fatal("a right click ended the left press on the button")
	}
	button(sdl.EventMouseButtonUp, sdl.ButtonLeft)
	if view.pressTaken || view.pressedControl != "" || app.message != "clicked ok" {
		t.Fatalf("the left release: taken %v, pressed %q, message %q", view.pressTaken, view.pressedControl, app.message)
	}
	// The prompt draws its cursor only while it has the focus.
	view.prompt, view.focus = "Find", "ok"
	if view.promptFocused() {
		t.Fatal("the prompt counts as focused with a button focused")
	}
}

func TestCanvasRenderRequests(t *testing.T) {
	app := testPrefetchApp(5, 1)
	// A smaller draw of a page whose thumbnail is sharp enough for it
	// leaves a sharper one asked for in the same frame.
	key := thumbnailKey(2)
	app.cache.add(&renderedTile{key: key, scale: 1, version: app.tileVersion(2)})
	app.wantThumbnail(2, 2)
	app.wantThumbnail(2, 0.5)
	if req := app.canvas.thumbs[2]; req.scale != 2 {
		t.Fatalf("the sharper thumbnail asked for became %+v", req)
	}
	// A render asked for once and failed is not asked for at once on
	// every frame.
	app.canvas.tiles = map[tileKey]image.Rectangle{{page: 1, scale: 1}: image.Rect(0, 0, 10, 10)}
	app.canvas.thumbs = nil
	app.requestCanvasRenders()
	app.renderPending = map[tileKey]renderRequest{} // as when it fails
	if app.canvasRendersUnasked() {
		t.Fatal("a failed render is asked for again every frame")
	}
	// A panel not docked moves no other view.
	gen := app.views.layoutGen
	app.views.show(&uiView{id: "float", place: viewPlacement{anchor: "center"}})
	if app.views.layoutGen != gen {
		t.Fatal("a floating panel changed every view's layout")
	}
}

// A view can draw everything it holds: what is out of sight costs no
// drawing and no render.
func TestCanvasCullsWhatIsOutOfSight(t *testing.T) {
	app := luaUITestApp(t, "")
	pages := testLayoutApp(200)
	app.pageMetrics, app.pageCount = pages.pageMetrics, pages.pageCount
	evalLua(t, app, `
onon.ui.create({
  dock = "left", width = 120,
  draw = function(canvas, box)
    local y = 0
    for page = 1, onon.page_count() do
      local _, h = canvas:page(page, 10, y, 100)
      y = y + h + 10
    end
  end,
}):show()`)
	view := app.views.panels[0]
	drawLuaView(t, app, view)
	if n := len(app.canvas.thumbs); n == 0 || n > 6 {
		t.Fatalf("drawing 200 pages wanted %d thumbnails, want only the few in sight", n)
	}
	if _, ok := app.canvas.thumbs[0]; !ok {
		t.Fatal("the first page, in sight, was not wanted")
	}
	if view.extent < 200*200 {
		t.Fatalf("what is out of sight still scrolls into it: extent %v", view.extent)
	}
	clear(app.canvas.thumbs)
	view.scrollPx = 210 * 50 // to page 51
	drawLuaView(t, app, view)
	if _, ok := app.canvas.thumbs[50]; !ok || len(app.canvas.thumbs) > 6 {
		t.Fatalf("scrolled to page 51, wanted %v", app.canvas.thumbs)
	}
}

func TestCanvasColumnLaysAFormOut(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
view = onon.ui.create({
  title = "Form",
  draw = function(canvas, box)
    local col = canvas:column({ x = 10, y = 10 })
    col:text("Name")
    col:input("name", { placeholder = "Name" })
    col:checkbox("agree", "I agree", true)
    local w, h = col:button("save", "Save")
    sizes = w .. "x" .. h .. " at " .. col:y() .. " of " .. box.w
  end,
  on_submit = function() local f = view:fields() onon.message("submit " .. f.name) end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	if got := strings.Join(view.focusOrder, ","); got != "name,agree,save" {
		t.Fatalf("the column drew %q", got)
	}
	name, agree, save := view.fields["name"].rect, view.controls["agree"], view.controls["save"]
	body := app.uiViewBody(view)
	if name.X != body.X+10 || name.W != body.W-20 || save.W != body.W-20 {
		t.Fatalf("the column's things are not its width, 10 in from each side: field %+v, button %+v, body %+v", name, save, body)
	}
	if !(name.Y > body.Y+10 && agree.Y >= name.Y+name.H+8 && save.Y >= agree.Y+agree.H+8) {
		t.Fatalf("the column did not stack its things, 8 apart: %v, %v, %v", name.Y, agree.Y, save.Y)
	}
	evalLua(t, app, `onon.message(sizes)`)
	if !strings.HasPrefix(app.message, strconv.Itoa(int(body.W-20))+"x") {
		t.Fatalf("the button's size and the column's next place: %q", app.message)
	}
	app.typeUIText("alice")
	app.viewKey(view, keyDown(sdl.KeycodeReturn, 0))
	if app.message != "submit alice" {
		t.Fatalf("on_submit reading view:fields(): %q", app.message)
	}
	evalLua(t, app, `view:set_title("Edit")`)
	if view.title != "Edit" {
		t.Fatalf("set_title left %q", view.title)
	}
	// A column used after its draw function has run cannot draw.
	evalLua(t, app, `onon.ui.create({ draw = function(canvas) kept = canvas:column() end }):show()`)
	drawLuaView(t, app, app.activeUIView())
	if _, err := app.runtime.Eval(`kept:text("late")`); err == nil || !strings.Contains(err.Error(), "while it draws") {
		t.Fatalf("a column kept past its draw function drew: %v", err)
	}
}
