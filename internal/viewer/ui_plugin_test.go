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

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font/basicfont"
)

func TestLuaUIViewNormalizesInitialFilteredSelection(t *testing.T) {
	app := testLayoutApp(0)
	view := &uiView{
		id:       "filtered",
		owner:    "lua",
		modal:    true,
		query:    "visible",
		selected: 0,
		rows: []uiRow{
			{index: 0, text: "hidden"},
			{index: 1, text: "visible"},
		},
	}
	selected := -1
	view.onSelect = func(_ *App, row uiRow) { selected = row.index }

	app.showUIView(view)
	app.activateUIView(view)

	if view.selected != 1 || selected != 1 {
		t.Fatalf("expected visible row 2 to be selected and activated, selected=%d callback=%d", view.selected, selected)
	}
}

func TestLuaUIEmptyViewHasNoSelection(t *testing.T) {
	app := testLayoutApp(0)
	if err := app.ShowUI(config.UIOverlay{ID: "empty", Searchable: true}); err != nil {
		t.Fatal(err)
	}
	if got := app.UISelected("empty"); got != 0 {
		t.Fatalf("empty view selection = %d, want 0", got)
	}
	app.SetUISelected("empty", 1)
	if got := app.UISelected("empty"); got != 0 {
		t.Fatalf("empty view selection after set = %d, want 0", got)
	}
}

func TestLuaUIRowsPreserveID(t *testing.T) {
	rows := uiRowsFromConfig([]config.UIListRow{{ID: "stable-id", Text: "row"}})
	if len(rows) != 1 || rows[0].id != "stable-id" {
		t.Fatalf("row ID was not preserved: %+v", rows)
	}
}

func TestLuaUISelectionCallbackReceivesRowID(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	source := `
bind("u", function()
  local view = onon.ui.create({
    rows = { { id = "stable-id", text = "row", value = "value" } },
    on_select = function(index, value, text, id)
      onon.message(tostring(index) .. ":" .. value .. ":" .. text .. ":" .. id)
    end,
  })
  view:show()
end)
`
	if err := os.WriteFile(configPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Open(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	app := testLayoutApp(0)
	app.runtime = runtime
	runtime.AttachHost(app)
	if handled, _, err := runtime.RunAction(runtime.Config().KeyBindings["u"]); !handled || err != nil {
		t.Fatalf("show view action: handled=%v err=%v", handled, err)
	}
	app.activateUIView(app.activeUIView())
	if app.message != "1:value:row:stable-id" {
		t.Fatalf("selection callback arguments = %q", app.message)
	}
}

func TestLuaUIClosePathsInvokeCallback(t *testing.T) {
	app := testLayoutApp(0)
	closed := 0
	view := &uiView{id: "view", owner: "lua", modal: true, visible: true, onClose: func(*App) { closed++ }}
	app.views.show(view)
	app.CloseUI("view")
	if closed != 1 {
		t.Fatalf("programmatic close callbacks = %d, want 1", closed)
	}

	view.visible = true
	app.views.active = view
	app.closeAllUI()
	if closed != 2 {
		t.Fatalf("teardown close callbacks = %d, want 2", closed)
	}
}

func TestLuaUIViewGenerationSurvivesSuccessfulReload(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(configPath, []byte(`-- initial config`), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Open(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	app := testLayoutApp(0)
	app.runtime = runtime
	runtime.AttachHost(app)
	if _, err := runtime.Eval(`local view = onon.ui.create({ title = "old", rows = { "old" } }); view:show()`); err != nil {
		t.Fatal(err)
	}
	old := app.activeUIView()

	if err := os.WriteFile(configPath, []byte(`local view = onon.ui.create({ title = "new", rows = { "new" } }); view:show()`), 0o644); err != nil {
		t.Fatal(err)
	}
	app.reloadConfig()
	active := app.activeUIView()
	if active == nil || active.title != "new" || active.generation != runtime.Generation() {
		t.Fatalf("new-generation view was not retained: %+v", active)
	}
	if _, exists := app.views.views[old.id]; exists {
		t.Fatal("old-generation view was retained")
	}
}

func TestLuaUIViewGenerationRollsBackAfterFailedReload(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(configPath, []byte(`-- initial config`), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Open(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	app := testLayoutApp(0)
	app.runtime = runtime
	runtime.AttachHost(app)
	if _, err := runtime.Eval(`local view = onon.ui.create({ title = "old", rows = { "old" } }); view:show()`); err != nil {
		t.Fatal(err)
	}
	old := app.activeUIView()

	broken := `local view = onon.ui.create({ title = "broken", rows = { "broken" } }); view:show(); error("reload failed")`
	if err := os.WriteFile(configPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	app.reloadConfig()
	if active := app.activeUIView(); active != old {
		t.Fatalf("failed reload did not restore old view: %+v", active)
	}
	for _, view := range app.views.views {
		if view.title == "broken" {
			t.Fatal("failed-generation view was retained")
		}
	}
	if !strings.Contains(app.message, "reload failed") {
		t.Fatalf("expected reload error, got %q", app.message)
	}
}

func TestMouseButtonPreCanConsumeButtonUp(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "mouse")
	if err := os.MkdirAll(filepath.Join(pluginDir, "lua", "mouse"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "onon-plugin.json"), []byte(`{"id":"mouse"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin := `
local M = onon.plugin.register("mouse")
M:on("mouse_button_pre", function(event)
  if event.phase == "up" then
    onon.message("consumed up")
    return true
  end
end)
return M
`
	if err := os.WriteFile(filepath.Join(pluginDir, "lua", "mouse", "init.lua"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(configPath, []byte(`require("mouse")`), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := config.OpenWithOptions(configPath, "", config.OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	app := testLayoutApp(1)
	app.runtime = runtime
	app.config.MouseTextSelect = true
	app.selection.active = true
	runtime.AttachHost(app)

	event := &sdl.MouseButtonEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventMouseButtonUp}, Button: uint8(sdl.ButtonLeft)}
	app.handleMouseButtonEvent(event)

	if !app.selection.active {
		t.Fatal("consumed button-up reached normal selection handling")
	}
	if app.message != "consumed up" {
		t.Fatalf("pre-event callback did not run, message=%q", app.message)
	}
}

// luaUITestApp is an app whose runtime has run source, a config.
func luaUITestApp(t *testing.T, source string) *App {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(configPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Open(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	app := testLayoutApp(0)
	app.winW, app.winH = 1000, 800
	app.runtime = runtime
	runtime.AttachHost(app)
	return app
}

func evalLua(t *testing.T, app *App, code string) {
	t.Helper()
	if _, err := app.runtime.Eval(code); err != nil {
		t.Fatal(err)
	}
}

func TestLuaUIRowsCarryStyleAndSource(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
onon.ui.create({
  rows = {
    { text = "Section", heading = true },
    { text = "a", key = "gg", swatch = "accent", marker = "* ", data = 7 },
  },
  on_select = function(index, value, text, id, row)
    onon.message(index .. ":" .. row.data)
  end,
}):show()`)
	view := app.activeUIView()
	heading, row := view.rows[0], view.rows[1]
	if !heading.heading || !heading.disabled {
		t.Fatalf("heading row = %+v", heading)
	}
	if row.key != "gg" || row.marker != "* " || row.swatch == nil || row.swatch.Name != "accent" {
		t.Fatalf("styled row = %+v", row)
	}
	if view.selected != 1 {
		t.Fatalf("selected = %d, want the row after the heading", view.selected)
	}
	app.activateUIView(view)
	if app.message != "2:7" {
		t.Fatalf("on_select message = %q", app.message)
	}
}

func TestLuaUIKeyAndActionCallbacks(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
onon.ui.create({
  rows = { "a", "b", "c" },
  on_key = function(key)
    if key == "x" then onon.message("key " .. key) return true end
  end,
  on_action = function(action)
    if action == "scroll_down" then onon.message("action " .. action) return true end
  end,
}):show()`)
	view := app.activeUIView()
	view.onKey(app, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeX})
	if app.message != "key x" {
		t.Fatalf("on_key message = %q", app.message)
	}
	// Keys on_key leaves go to the list.
	view.onKey(app, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeDown})
	if view.selected != 1 {
		t.Fatalf("selected after Down = %d, want 1", view.selected)
	}
	app.runUIViewAction(view, "scroll_down")
	if app.message != "action scroll_down" || view.selected != 1 {
		t.Fatalf("on_action: message %q selected %d", app.message, view.selected)
	}
	app.runUIViewAction(view, "scroll_up")
	if view.selected != 0 {
		t.Fatalf("selected after an action on_action left = %d, want 0", view.selected)
	}
}

func TestLuaUISelectionAndQueryCallbacks(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
local view
view = onon.ui.create({
  rows = { "first" },
  filter = false,
  on_query_changed = function(query)
    view:set_rows({ "x", "y" })
  end,
  on_selection_changed = function(index, value)
    onon.message("selected " .. index .. ":" .. value)
  end,
})
view:show()`)
	view := app.activeUIView()
	app.notifySelectionChange()
	if app.message != "" {
		t.Fatalf("opening told on_selection_changed: %q", app.message)
	}
	app.setUIViewQuery(view, "zz")
	if got := len(view.visibleRows()); got != 2 {
		t.Fatalf("rows set for the query, unfiltered = %d, want 2", got)
	}
	app.moveUIViewSelection(view, 1)
	app.notifySelectionChange()
	if app.message != "selected 2:y" {
		t.Fatalf("on_selection_changed message = %q", app.message)
	}
	// A query the plugin sets is not the user's typing.
	app.message = ""
	app.SetUIQuery(view.id, "set")
	if view.query != "set" || len(view.rows) != 2 {
		t.Fatalf("set_query: query %q rows %d", view.query, len(view.rows))
	}
}

func TestLuaUIPanelStaysOpenBesideMenus(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
local log = ""
panel = onon.ui.create({
  title = "Panel",
  modal = false,
  anchor = "bottom_right",
  width = "20%",
  height = "25%",
  draw = function(canvas, box) end,
  on_mouse = function(ev)
    log = log .. ev.type .. (ev.button and (" " .. ev.button) or "") .. ";"
    onon.message(log)
    return ev.type ~= "move"
  end,
})
panel:show()`)
	var panel *uiView
	for _, view := range app.views.views {
		panel = view
	}
	if app.activeUIView() != nil || !panel.visible {
		t.Fatal("a panel opened as a menu")
	}
	rect, _ := panel.frameGeometry(app)
	viewportW, viewportH := app.viewportSize()
	gap := float32(app.ipx(12))
	if rect.W != float32(viewportW*20/100) || rect.H != float32(viewportH*25/100) ||
		rect.X+rect.W != float32(viewportW)-gap || rect.Y+rect.H != float32(viewportH)-gap {
		t.Fatalf("panel at %+v in %dx%d", rect, viewportW, viewportH)
	}

	app.showCoreList("menu", "Menu", []string{"a"}, nil)
	app.closeAllUI()
	if !panel.visible {
		t.Fatal("a menu closed the panel")
	}

	inX, inY := rect.X+rect.W/2, rect.Y+rect.H/2
	if !app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: inX, Y: inY}) {
		t.Fatal("the panel did not take a press over it")
	}
	if app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: 1, Y: 1}) {
		t.Fatal("the panel took a press outside it")
	}
	// The release of the press over it is the panel's, wherever it is.
	app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: inX, Y: inY})
	if !app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: 1, Y: 1}) {
		t.Fatal("the panel did not take the release after its press")
	}
	if taken, _ := app.handlePanelMouseMotion(&sdl.MouseMotionEvent{X: inX, Y: inY}); !taken {
		t.Fatal("the panel did not take motion over it")
	}
	if taken, _ := app.handlePanelMouseMotion(&sdl.MouseMotionEvent{X: 1, Y: 1}); taken {
		t.Fatal("the panel took motion outside it")
	}
	if want := "press left;press left;release left;move;leave;"; app.message != want {
		t.Fatalf("on_mouse events = %q, want %q", app.message, want)
	}

	evalLua(t, app, `panel:close()`)
	if panel.visible || len(app.views.panels) != 0 {
		t.Fatal("close left the panel open")
	}
}

func TestLuaUIMouseCoordinatesAreTheBody(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
onon.ui.create({
  draw = function() end,
  on_mouse = function(ev) onon.message(ev.x .. "," .. ev.y) return true end,
}):show()`)
	view := app.activeUIView()
	if !view.headless {
		t.Fatal("a view that draws, without a title, has a header")
	}
	body := app.uiViewBody(view)
	view.onMouseButton(app, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: body.X + 10, Y: body.Y + 4})
	if app.message != "10,4" {
		t.Fatalf("on_mouse at %q, want 10,4", app.message)
	}
}

func TestLuaUIDrawFunction(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
calls = 0
onon.ui.create({
  title = "Drawn",
  rows = { "a" },
  draw = function(canvas, box, state)
    calls = calls + 1
    onon.message(state.element .. " " .. box.w .. " " .. calls)
    if calls > 1 then error("boom") end
  end,
}):show()`)
	view := app.activeUIView()
	body := app.uiViewBody(view)
	rowsDrawn := 0
	drawRows := func() error { rowsDrawn++; return nil }
	if err := view.drawBody(app, nil, body, drawRows); err != nil {
		t.Fatal(err)
	}
	if want := "panel " + strconv.Itoa(int(body.W)) + " 1"; app.message != want || rowsDrawn != 0 {
		t.Fatalf("first draw: message %q rows %d, want %q and no rows", app.message, rowsDrawn, want)
	}
	// A failing function is reported, and the rows drawn in its place.
	if err := view.drawBody(app, nil, body, drawRows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(app.message, "boom") || rowsDrawn != 1 {
		t.Fatalf("failed draw: message %q rows %d", app.message, rowsDrawn)
	}
	app.message = ""
	view.drawBody(app, nil, body, drawRows)
	if app.message != "" || rowsDrawn != 2 {
		t.Fatalf("a failed function ran again: message %q rows %d", app.message, rowsDrawn)
	}
}

func TestLuaUIDocksTakeTheirSideFromTheDocument(t *testing.T) {
	app := luaUITestApp(t, "")
	full := app.documentRect()
	evalLua(t, app, `
left = onon.ui.create({ dock = "left", width = 200, draw = function() end })
top = onon.ui.create({ dock = "top", height = "10%", draw = function() end })
left:show()
top:show()`)
	doc := app.documentRect()
	if doc.X != 200 || doc.Y != 80 || doc.W != full.W-200 || doc.H != full.H-80 {
		t.Fatalf("document beside docks = %+v, full %+v", doc, full)
	}
	if w, h := app.viewportSize(); w != int(doc.W) || h != int(doc.H) {
		t.Fatalf("viewport %dx%d, document %+v", w, h, doc)
	}
	var left, top *uiView
	for _, view := range app.views.panels {
		if view.place.dock == "left" {
			left = view
		} else {
			top = view
		}
	}
	// The top dock opened second, so it spans what the left one left.
	if rect, _ := left.frameGeometry(app); rect != (sdl.FRect{W: 200, H: full.H}) {
		t.Fatalf("left dock at %+v", rect)
	}
	if rect, _ := top.frameGeometry(app); rect != (sdl.FRect{X: 200, W: full.W - 200, H: 80}) {
		t.Fatalf("top dock at %+v", rect)
	}
	// A view anchored to a corner sits in the document's part of the window.
	evalLua(t, app, `onon.ui.create({ anchor = "top_left", width = 100, height = 50, draw = function() end }):show()`)
	gap := float32(app.ipx(12))
	if rect, _ := app.activeUIView().frameGeometry(app); rect.X != 200+gap || rect.Y != 80+gap {
		t.Fatalf("anchored view at %+v", rect)
	}
	app.closeAllUI()
	evalLua(t, app, `left:close() top:close()`)
	if app.documentRect() != full {
		t.Fatalf("closing the docks left %+v, want %+v", app.documentRect(), full)
	}
}

func TestLuaUIDockMovesPageHitTesting(t *testing.T) {
	app := luaUITestApp(t, "")
	pages := testLayoutApp(1)
	app.pageMetrics, app.pageCount = pages.pageMetrics, pages.pageCount
	app.relayoutWithViewportAnchor(nil)
	x, y, _ := app.pageScreenOrigin(0)
	before, _, _, ok := app.pageGeometryAtScreen(x+1, y+1)
	if !ok || before != 0 {
		t.Fatal("no page under its own corner")
	}
	evalLua(t, app, `onon.ui.create({ dock = "left", width = 300, draw = function() end }):show()`)
	x, y, _ = app.pageScreenOrigin(0)
	// Page origins are in the document's part of the window, which the dock
	// moved right; the window point is that far further right.
	if _, _, _, ok := app.pageGeometryAtScreen(x+1, y+1); ok {
		t.Fatal("the page was hit at its corner unmoved by the dock")
	}
	if page, _, _, ok := app.pageGeometryAtScreen(300+x+1, y+1); !ok || page != 0 {
		t.Fatalf("the page's corner, moved by the dock, did not hit it")
	}
}

func TestLuaUIStackedMenuReturnsToTheOneUnder(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
closed = ""
onon.ui.create({ title = "under", rows = { "a" }, on_close = function() closed = closed .. "under;" end }):show()
onon.ui.create({ title = "over", stack = true, rows = { "b" }, on_close = function() closed = closed .. "over;" onon.message(closed) end }):show()`)
	over := app.activeUIView()
	if over.title != "over" || len(app.views.stack) != 1 || !app.views.stack[0].visible {
		t.Fatalf("stacked menu: active %q, stack %d", over.title, len(app.views.stack))
	}
	app.runUIViewAction(over, "close")
	if active := app.activeUIView(); active == nil || active.title != "under" || app.message != "over;" {
		t.Fatalf("after closing the stacked menu: active %+v, message %q", active, app.message)
	}
	evalLua(t, app, `onon.ui.create({ title = "over", stack = true, rows = { "b" } }):show()`)
	app.closeAllUI()
	if app.activeUIView() != nil || len(app.views.stack) != 0 {
		t.Fatal("closing all left a stacked menu")
	}
}

func TestLuaUIAnchorPoints(t *testing.T) {
	app := luaUITestApp(t, "")
	pages := testLayoutApp(20)
	app.pageMetrics, app.pageCount = pages.pageMetrics, pages.pageCount
	app.relayoutWithViewportAnchor(nil)
	evalLua(t, app, `
onon.ui.create({ modal = false, anchor = { x = 30, y = 40 }, width = 50, height = 20, draw = function() end }):show()
onon.ui.create({ modal = false, anchor = { page = 2, x = 10, y = 20 }, width = 50, height = 20, draw = function() end }):show()`)
	window, page := app.views.panels[0], app.views.panels[1]
	if rect, _ := window.frameGeometry(app); rect != (sdl.FRect{X: 30, Y: 40, W: 50, H: 20}) {
		t.Fatalf("view at a window point: %+v", rect)
	}
	before, _ := page.frameGeometry(app)
	x, y, _ := app.pageScreenOrigin(1)
	dx, dy := app.pageTransform(1).toScreen(10, 20)
	if before.X != float32(math.Round(x+dx)) || before.Y != float32(math.Round(y+dy)) {
		t.Fatalf("view pinned to page 2 at %+v, page point at %.0f,%.0f", before, x+dx, y+dy)
	}
	app.scrollBy(0, 30)
	if after, _ := page.frameGeometry(app); after.Y != before.Y-30 {
		t.Fatalf("pinned view did not scroll with its page: %v then %v", before.Y, after.Y)
	}
}

func TestCanvasTextLines(t *testing.T) {
	app := testLayoutApp(0)
	face := basicfont.Face7x13 // 7 pixels a character
	laid := app.canvasTextLines(face, "hello world foo\nabcdefghijklmnop", config.TextOptions{Width: 77, Wrap: true})
	want := []string{"hello world", "foo", "abcdefghijk", "lmnop"}
	if strings.Join(laid.lines, "|") != strings.Join(want, "|") || laid.width != 77 {
		t.Fatalf("wrapped lines %q width %d, want %q", laid.lines, laid.width, want)
	}
	laid = app.canvasTextLines(face, "hello world foo", config.TextOptions{Width: 77})
	if len(laid.lines) != 1 || !strings.HasSuffix(laid.lines[0], "...") || laid.width > 77 {
		t.Fatalf("truncated line %q width %d", laid.lines, laid.width)
	}
}

// drawLuaView draws app's open view's body without a renderer, as a frame
// would.
func drawLuaView(t *testing.T, app *App, view *uiView) {
	t.Helper()
	if err := view.drawBody(app, nil, app.uiViewBody(view), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestLuaUIDrawnBodyScrolls(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
view = onon.ui.create({
  width = 200, height = 100,
  draw = function(canvas, box, state)
    local w, h = canvas:measure("a\nb", { width = 50 })
    assert(h == 26, "two lines of 13: " .. h)
    canvas:rect(0, 0, box.w, 500, "accent")
  end,
  on_mouse = function(ev) onon.message(ev.type .. " " .. ev.y) end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	body := app.uiViewBody(view)
	if view.extent != 500 {
		t.Fatalf("extent = %v, want 500", view.extent)
	}
	view.onWheel(app, &sdl.MouseWheelEvent{Y: -1, MouseX: body.X + 5, MouseY: body.Y + 5})
	if view.scrollPx <= 0 {
		t.Fatal("the wheel did not scroll the drawn body")
	}
	evalLua(t, app, `view:set_scroll(1000)`)
	if want := 500 - float64(body.H); view.scrollPx != want {
		t.Fatalf("scrolled past the end: %v, want %v", view.scrollPx, want)
	}
	evalLua(t, app, `onon.message(tostring(view:scroll()))`)
	if want := strconv.Itoa(int(500 - body.H)); app.message != want {
		t.Fatalf("view:scroll() = %s, want %s", app.message, want)
	}
	view.onMouseButton(app, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: body.X + 5, Y: body.Y + 5})
	if want := "press " + strconv.Itoa(int(500-body.H)+5); app.message != want {
		t.Fatalf("on_mouse in scrolled content: %q, want %q", app.message, want)
	}
	app.runUIViewAction(view, "first_page")
	if view.scrollPx != 0 {
		t.Fatalf("first_page left the body scrolled %v", view.scrollPx)
	}
}

func TestLuaUIRegionsAndAnimation(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
hovered = nil
view = onon.ui.create({
  width = 200, height = 100,
  draw = function(canvas, box)
    canvas:region("back", 0, 0, box.w, box.h)
    hovered = canvas:region("button", 10, 10, 50, 20)
    return true
  end,
  on_mouse = function(ev) onon.message(ev.type .. " " .. tostring(ev.region)) return true end,
})
view:show()`)
	view := app.activeUIView()
	body := app.uiViewBody(view)
	app.pointer = sdl.FPoint{X: body.X + 20, Y: body.Y + 15}
	app.pendingRedraw = false
	drawLuaView(t, app, view)
	if !app.pendingRedraw {
		t.Fatal("a draw function returning true did not ask for the next frame")
	}
	evalLua(t, app, `onon.message(tostring(hovered))`)
	if app.message != "true" {
		t.Fatalf("region under the pointer hovered = %s", app.message)
	}
	view.onMouseButton(app, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: body.X + 20, Y: body.Y + 15})
	if app.message != "press button" {
		t.Fatalf("press over the button: %q", app.message)
	}
	view.onMouseButton(app, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: body.X + 150, Y: body.Y + 50})
	if app.message != "press back" {
		t.Fatalf("press beside the button: %q", app.message)
	}
}

func TestLuaUIImagesAndPages(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	app := luaUITestApp(t, "")
	pages := testLayoutApp(2)
	app.pageMetrics, app.pageCount = pages.pageMetrics, pages.pageCount
	evalLua(t, app, `
dir = `+strconv.Quote(dir)+`
view = onon.ui.create({
  draw = function(canvas)
    local w, h = canvas:image(dir .. "/icon.png", 0, 0, 20)
    local pw, ph = canvas:page(2, 0, 0, 50)
    onon.message(w .. "x" .. h .. " " .. pw .. "x" .. ph)
  end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	if app.message != "20x10 50x100" {
		t.Fatalf("image and page sizes: %q", app.message)
	}
	if !app.canvas.thumbs[1] {
		t.Fatal("drawing page 2 did not want its thumbnail")
	}
	wanted, onScreen, protected := map[tileKey]bool{}, map[tileKey]bool{}, map[tileKey]bool{}
	app.addCanvasThumbnails(wanted, onScreen, protected)
	if !wanted[thumbnailKey(1)] || !protected[thumbnailKey(1)] {
		t.Fatal("the drawn page's thumbnail is not kept wanted")
	}
	evalLua(t, app, `view:close()
onon.ui.create({ draw = function(canvas) canvas:image(dir .. "/missing.png", 0, 0) end }):show()`)
	drawLuaView(t, app, app.activeUIView())
	if !strings.Contains(app.message, "missing.png") {
		t.Fatalf("a missing image was not reported: %q", app.message)
	}
}

func keyDown(code sdl.Keycode, mod sdl.Keymod) *sdl.KeyboardEvent {
	return &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: code, Mod: mod}
}

func TestLuaUIPromptTypesTheQuery(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
onon.ui.create({
  prompt = "Open",
  rows = { "alpha", "beta", "bell" },
  on_query_changed = function(q) onon.message("query " .. q) end,
  on_select = function(_, value) onon.message("chose " .. value) end,
}):show()`)
	view := app.activeUIView()
	if !app.typeUIText("bet") || view.query != "bet" || app.message != "query bet" {
		t.Fatalf("typing into the prompt: query %q message %q", view.query, app.message)
	}
	if got := len(view.visibleRows()); got != 1 {
		t.Fatalf("rows matching the prompt = %d, want 1", got)
	}
	view.onKey(app, keyDown(sdl.KeycodeBackspace, 0))
	if view.query != "be" || len(view.visibleRows()) != 2 {
		t.Fatalf("backspace in the prompt: query %q", view.query)
	}
	// A key that types is the prompt's, not a binding's.
	app.sequenceLookup = map[string]string{"j": "scroll_down", "<CR>": "confirm"}
	view.onKey(app, keyDown(sdl.KeycodeJ, 0))
	if view.selected != 1 {
		t.Fatalf("a typing key moved the selection to %d", view.selected)
	}
	view.onKey(app, keyDown(sdl.KeycodeDown, 0))
	view.onKey(app, keyDown(sdl.KeycodeReturn, 0))
	if app.message != "chose bell" {
		t.Fatalf("Enter in a prompt without on_submit: %q", app.message)
	}
	app.SetUIQuery(view.id, "al")
	if view.promptInput.Value != "al" {
		t.Fatalf("set_query left the prompt showing %q", view.promptInput.Value)
	}
	// Escape closes a prompt with text in it, as it would its list.
	app.sequenceLookup["<Esc>"] = "close"
	view.onKey(app, keyDown(sdl.KeycodeEscape, 0))
	if view.visible {
		t.Fatal("Escape left a prompt with text open")
	}
}

func TestLuaUIFields(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
view = onon.ui.create({
  title = "Sign in",
  draw = function(canvas, box)
    local h = canvas:input("name", 0, 0, box.w, { placeholder = "Name" })
    canvas:input("password", 0, h + 4, box.w, { secret = true })
  end,
  on_input = function(text, id) onon.message("input " .. id .. "=" .. text) end,
  on_submit = function(text, id) onon.message("submit " .. id .. "=" .. text .. " name=" .. view:field("name")) end,
})
view:set_field("name", "al")
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	if view.focusField != "name" || strings.Join(view.fieldOrder, ",") != "name,password" {
		t.Fatalf("fields %q, focused %q", view.fieldOrder, view.focusField)
	}
	app.typeUIText("ice")
	if app.message != "input name=alice" {
		t.Fatalf("typing into the first field: %q", app.message)
	}
	view.onKey(app, keyDown(sdl.KeycodeTab, 0))
	app.typeUIText("pw")
	view.onKey(app, keyDown(sdl.KeycodeReturn, 0))
	if app.message != "submit password=pw name=alice" {
		t.Fatalf("Enter in the second field: %q", app.message)
	}
	view.onKey(app, keyDown(sdl.KeycodeTab, sdl.KeymodShift))
	if view.focusField != "name" {
		t.Fatalf("Shift-Tab focused %q", view.focusField)
	}
	if f := view.fields["password"]; f.display() != "••" {
		t.Fatalf("a secret field shows %q", f.display())
	}
	// A click on a field focuses it.
	pass := view.fields["password"].rect
	view.onMouseButton(app, &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: pass.X + 2, Y: pass.Y + 2})
	if view.focusField != "password" {
		t.Fatalf("clicking the password field focused %q", view.focusField)
	}
}

func TestLuaUIPanelFocus(t *testing.T) {
	app := luaUITestApp(t, "")
	app.fontFace = basicfont.Face7x13
	evalLua(t, app, `
panel = onon.ui.create({
  modal = false, anchor = "top_right", width = 200, height = 80,
  draw = function(canvas, box) canvas:input("note", 0, 0, box.w) end,
  on_input = function(text) onon.message(text) end,
})
panel:show()`)
	view := app.views.panels[0]
	drawLuaView(t, app, view)
	if app.typeUIText("x") {
		t.Fatal("a panel took typing without the keys")
	}
	rect := view.fields["note"].rect
	app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: rect.X + 2, Y: rect.Y + 2})
	app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: rect.X + 2, Y: rect.Y + 2})
	if app.keyView() != view || !app.typeUIText("hi") || app.message != "hi" {
		t.Fatalf("clicking the panel's field did not give it the keys: message %q", app.message)
	}
	view.onKey(app, keyDown(sdl.KeycodeEscape, 0))
	if app.keyView() != nil {
		t.Fatal("Escape left the panel the keys")
	}
	evalLua(t, app, `panel:focus() onon.message(tostring(panel:focused()))`)
	if app.message != "true" {
		t.Fatalf("panel:focus() then focused() = %s", app.message)
	}
	app.handlePanelMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: 1, Y: 700})
	if app.keyView() != nil {
		t.Fatal("a press on the document left the panel the keys")
	}
}
