package viewer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
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
  width = 20,
  height = 25,
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
