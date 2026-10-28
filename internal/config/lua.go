package config

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"onon/internal/actions"
	"onon/internal/keys"

	lua "github.com/yuin/gopher-lua"
)

type luaFunctionSpec struct {
	Signature string
	Function  lua.LGFunction
}

var luaFunctionReferences = map[string]LuaReferenceEntry{}
var luaFunctionReferencesMu sync.Mutex

func (r *Runtime) applyLuaConfig(path string) error {
	L := r.state
	if L == nil {
		L = r.initLuaState()
	}
	prependPackagePath(L, filepath.Dir(path))
	if err := L.DoFile(path); err != nil {
		r.closeLuaState()
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// prependPackagePath makes require search dir first, so a config can
// require the Lua files beside it: require("forest") loads dir/forest.lua
// and require("themes.forest") dir/themes/forest.lua.
func prependPackagePath(L *lua.LState, dir string) {
	packageTable, ok := L.GetGlobal("package").(*lua.LTable)
	if !ok || dir == "" {
		return
	}
	patterns := filepath.Join(dir, "?.lua") + ";" + filepath.Join(dir, "?", "init.lua")
	current := lua.LVAsString(L.GetField(packageTable, "path"))
	if strings.HasPrefix(current, patterns) {
		return
	}
	if current != "" {
		patterns += ";" + current
	}
	L.SetField(packageTable, "path", lua.LString(patterns))
}

func (r *Runtime) initLuaState() *lua.LState {
	if r.state != nil {
		r.closeLuaState()
	}
	L := lua.NewState()
	PreloadPortableLuaModules(L)
	r.state = L
	r.pluginGeneration++
	if r.plugins == nil {
		r.plugins = newPluginState(r)
	}
	installPluginPackagePath(L, r)
	mod := newLuaModule(L, r, &r.cfg)
	L.SetGlobal("onon", mod)
	L.SetGlobal("bind", L.GetField(mod, "bind"))
	L.SetGlobal("unbind", L.GetField(mod, "unbind"))
	L.SetGlobal("bind_mouse", L.GetField(mod, "bind_mouse"))
	L.SetGlobal("unbind_mouse", L.GetField(mod, "unbind_mouse"))
	L.SetGlobal("options", L.GetField(mod, "options"))
	return L
}

func (r *Runtime) updateLuaDocument() {
	if r.state == nil {
		return
	}
	mod, ok := r.state.GetGlobal("onon").(*lua.LTable)
	if ok {
		r.state.SetField(mod, "document", newLuaDocumentTable(r.state, r))
	}
}

func newLuaModule(L *lua.LState, rt *Runtime, cfg *Config) *lua.LTable {
	mod := L.NewTable()
	L.SetField(mod, "document", newLuaDocumentTable(L, rt))
	L.SetField(mod, "cache", newLuaCacheTable(L, rt))
	L.SetField(mod, "ui", newLuaViewAPI(L, rt))
	L.SetField(mod, "plugin", newLuaPluginAPI(L, rt))
	L.SetField(mod, "platform", NewLuaPlatformModule(L))
	L.SetField(mod, "path", NewLuaPathModule(L))
	L.SetField(mod, "json", NewLuaJSONModule(L))
	L.SetField(mod, "clipboard", newLuaClipboardTable(L, rt))
	L.SetField(mod, "formats", newLuaFormatsTable(L, rt))
	registerLuaFunctions(L, mod, "onon.", []luaFunctionSpec{
		{Signature: "onon.bind(key, action)", Function: luaBind(rt)},
		{Signature: "onon.unbind(key)", Function: luaUnbind(rt)},
		{Signature: "onon.bind_mouse(event, action)", Function: luaBindMouse(rt)},
		{Signature: "onon.unbind_mouse(event)", Function: luaUnbindMouse(rt)},
		{Signature: "onon.message([text])", Function: luaMessage(rt, cfg)},
		{Signature: "onon.command(command)", Function: luaCommand(rt)},
		{Signature: "onon.open(path)", Function: luaOpen(rt)},
		{Signature: "onon.register_scheme(name, resolve)", Function: luaRegisterScheme(rt)},
		{Signature: "onon.pick_file(callback)", Function: luaPickFile(rt)},
		{Signature: "onon.pick_directory(callback)", Function: luaPickDirectory(rt)},
		{Signature: "onon.schedule(callback)", Function: luaSchedule(rt)},
		{Signature: "onon.log(level, message)", Function: luaLog(rt)},
		{Signature: "onon.open_external(uri_or_path)", Function: luaOpenExternal(rt)},
		{Signature: "onon.page([page])", Function: luaPage(rt)},
		{Signature: "onon.page_count()", Function: luaPageCount(rt)},
		{Signature: "onon.version()", Function: luaVersion},
		{Signature: "onon.goto_document_point(spec)", Function: luaGotoDocumentPoint(rt)},
		{Signature: "onon.mode()", Function: luaMode(rt)},
		{Signature: "onon.search(query[, backward])", Function: luaSearch(rt)},
		{Signature: "onon.search_query()", Function: luaSearchQuery(rt)},
		{Signature: "onon.search_match_count()", Function: luaSearchMatchCount(rt)},
		{Signature: "onon.search_match_index()", Function: luaSearchMatchIndex(rt)},
		{Signature: "onon.current_count()", Function: luaCurrentCount(rt)},
		{Signature: "onon.pending_keys()", Function: luaPendingKeys(rt)},
		{Signature: "onon.recent_files([limit])", Function: luaRecentFiles(cfg)},
		{Signature: "onon.clear_pending_keys()", Function: luaClearPendingKeys(rt)},
		{Signature: "onon.fit_mode([mode])", Function: luaFitMode(rt, cfg)},
		{Signature: "onon.render_mode([mode])", Function: luaRenderMode(rt, cfg)},
		{Signature: "onon.zoom([scale])", Function: luaZoom(rt)},
		{Signature: "onon.rotation([degrees])", Function: luaRotation(rt)},
		{Signature: "onon.fullscreen([enabled])", Function: luaFullscreen(rt)},
	})
	options := newLuaOptionsTable(L, rt, cfg)
	L.SetField(mod, "options", options)
	L.SetField(mod, "o", options)
	L.SetField(mod, "themes", newLuaThemesTable(L))
	// onon.theme is read and assigned through the metatable, as assigning
	// it sets the theme rather than replacing the field.
	mt := L.NewTable()
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		if L.CheckString(2) == "theme" {
			L.Push(newLuaThemeTable(L, rt, cfg, ""))
			return 1
		}
		L.Push(lua.LNil)
		return 1
	}))
	L.SetField(mt, "__newindex", L.NewFunction(func(L *lua.LState) int {
		key := L.CheckAny(2)
		if key.String() == "theme" {
			if err := rt.setOption("theme", L.CheckAny(3)); err != nil {
				L.RaiseError("onon.theme: %v", err)
			}
			return 0
		}
		L.RawSet(L.CheckTable(1), key, L.CheckAny(3))
		return 0
	}))
	L.SetMetatable(mod, mt)
	for _, action := range actions.Names() {
		name := action
		L.SetField(mod, name, newLuaActionValue(L, rt, name))
	}
	return mod
}

func newLuaOptionsTable(L *lua.LState, rt *Runtime, cfg *Config) *lua.LTable {
	tbl := L.NewTable()
	mt := L.NewTable()
	L.SetField(mt, "__newindex", L.NewFunction(func(L *lua.LState) int {
		name := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		value := L.CheckAny(3)
		if err := rt.setOption(name, value); err != nil {
			L.RaiseError("options.%s: %v", name, err)
		}
		return 0
	}))
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		name := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		if name == "theme" {
			L.Push(newLuaThemeTable(L, rt, cfg, ""))
			return 1
		}
		if desc, ok := lookupOption(name); ok {
			L.Push(desc.get(L, cfg))
			return 1
		}
		if value, ok := rt.pluginOption(name); ok {
			L.Push(value.value)
			return 1
		}
		L.RaiseError("options.%s: unknown setting", name)
		return 0
	}))
	L.SetMetatable(tbl, mt)
	return tbl
}

func newLuaCacheTable(L *lua.LState, rt *Runtime) *lua.LTable {
	tbl := L.NewTable()
	registerLuaFunctions(L, tbl, "onon.cache.", []luaFunctionSpec{
		{Signature: "onon.cache.entries()", Function: luaCacheEntries(rt)},
		{Signature: "onon.cache.pending()", Function: luaCachePending(rt)},
		{Signature: "onon.cache.limit([mib])", Function: luaCacheLimit(rt)},
		{Signature: "onon.cache.clear()", Function: luaCacheClear(rt)},
	})
	return tbl
}

func newLuaViewAPI(L *lua.LState, rt *Runtime) *lua.LTable {
	tbl := L.NewTable()
	registerLuaFunctions(L, tbl, "onon.ui.", []luaFunctionSpec{
		{Signature: "onon.ui.create(spec)", Function: luaUiCreate(rt)},
	})
	return tbl
}

func uiOverlayFromLuaSpec(L *lua.LState, rt *Runtime, spec *lua.LTable) UIOverlay {
	overlay := UIOverlay{
		ID:         lua.LVAsString(spec.RawGetString("id")),
		Title:      lua.LVAsString(spec.RawGetString("title")),
		Rows:       luaTableUIRows(L, spec.RawGetString("rows")),
		Selected:   1,
		Searchable: true,
		Filter:     true,
		Modal:      true,
		Width:      UISize{Value: 70, Percent: true},
		Height:     UISize{Value: 70, Percent: true},
		Anchor:     "center",
		Generation: rt.pluginGeneration,
	}
	if selected := spec.RawGetString("selected"); selected.Type() == lua.LTNumber {
		overlay.Selected = int(lua.LVAsNumber(selected))
	}
	if scroll := spec.RawGetString("scroll"); scroll.Type() == lua.LTNumber {
		overlay.Scroll = int(lua.LVAsNumber(scroll))
	}
	if query := spec.RawGetString("query"); query.Type() == lua.LTString {
		overlay.Query = query.String()
	}
	for name, field := range map[string]*bool{"searchable": &overlay.Searchable, "filter": &overlay.Filter, "modal": &overlay.Modal, "stack": &overlay.Stack, "resizable": &overlay.Resizable} {
		if value := spec.RawGetString(name); value.Type() == lua.LTBool {
			*field = lua.LVAsBool(value)
		}
	}
	for name, field := range map[string]*UISize{"width": &overlay.Width, "height": &overlay.Height} {
		if value := spec.RawGetString(name); value != lua.LNil {
			size, err := luaUISize(value)
			if err != nil {
				L.RaiseError("ui.create: %s %v", name, err)
			}
			*field = size
		}
	}
	switch anchor := spec.RawGetString("anchor").(type) {
	case *lua.LNilType:
	case lua.LString:
		overlay.Anchor = string(anchor)
		if !slices.Contains(UIAnchors, overlay.Anchor) {
			L.RaiseError("ui.create: unknown anchor %q; use one of %s, or a point", overlay.Anchor, strings.Join(UIAnchors, ", "))
		}
	case *lua.LTable:
		x, xok := anchor.RawGetString("x").(lua.LNumber)
		y, yok := anchor.RawGetString("y").(lua.LNumber)
		page := anchor.RawGetString("page")
		pageNumber, pageok := page.(lua.LNumber)
		corner := lua.LVAsString(anchor.RawGetString("corner"))
		if !xok || !yok || page != lua.LNil && (!pageok || pageNumber < 1) || corner != "" && !slices.Contains(UICorners, corner) {
			L.RaiseError("ui.create: an anchor point is { x = number, y = number [, page = page number] [, corner = %s] }", strings.Join(UICorners, " | "))
		}
		overlay.At = &UIPoint{Page: int(pageNumber), X: float64(x), Y: float64(y), Corner: corner}
	default:
		L.RaiseError("ui.create: anchor must be a place or a point")
	}
	if columns, ok := spec.RawGetString("columns").(*lua.LTable); ok {
		for i := 1; i <= columns.Len(); i++ {
			col, ok := columns.RawGetInt(i).(*lua.LTable)
			if !ok {
				L.RaiseError("ui.create: column %d must be a table", i)
			}
			c := UIColumn{Title: lua.LVAsString(col.RawGetString("title")), Align: lua.LVAsString(col.RawGetString("align")), Fill: true}
			if c.Align != "" && c.Align != "left" && c.Align != "center" && c.Align != "right" {
				L.RaiseError("ui.create: column %d align is left, center or right", i)
			}
			if width := col.RawGetString("width"); width != lua.LNil && lua.LVAsString(width) != "*" {
				size, err := luaUISize(width)
				if err != nil {
					L.RaiseError("ui.create: column %d width %v, or \"*\" for a share of the rest", i, err)
				}
				c.Width, c.Fill = size, false
			}
			overlay.Columns = append(overlay.Columns, c)
		}
	}
	if empty := spec.RawGetString("empty"); empty != lua.LNil {
		overlay.Empty = lua.LVAsString(empty)
	}
	if prompt := spec.RawGetString("prompt"); prompt != lua.LNil {
		if prompt.Type() != lua.LTString {
			L.RaiseError("ui.create: prompt must be a string")
		}
		overlay.Prompt = prompt.String()
	}
	if dock := spec.RawGetString("dock"); dock != lua.LNil {
		overlay.Dock = lua.LVAsString(dock)
		if !slices.Contains(UIDocks, overlay.Dock) {
			L.RaiseError("ui.create: unknown dock %q; use one of %s", overlay.Dock, strings.Join(UIDocks, ", "))
		}
		overlay.Modal = false
	}
	for name, field := range map[string]*string{
		"on_select":            &overlay.OnSelect,
		"on_close":             &overlay.OnClose,
		"on_selection_changed": &overlay.OnSelectionChanged,
		"on_query_changed":     &overlay.OnQueryChanged,
		"on_key":               &overlay.OnKey,
		"on_action":            &overlay.OnAction,
		"on_mouse":             &overlay.OnMouse,
		"draw":                 &overlay.Draw,
		"on_input":             &overlay.OnInput,
		"on_submit":            &overlay.OnSubmit,
		"on_click":             &overlay.OnClick,
	} {
		switch fn := spec.RawGetString(name).(type) {
		case *lua.LFunction:
			*field = rt.registerCallback(fn)
		case *lua.LNilType:
		default:
			L.RaiseError("ui.create: %s must be a function", name)
		}
	}
	return overlay
}

// luaUISize reads a size: a number of logical pixels, or a string such as
// "30%" for a percentage of the window.
func luaUISize(value lua.LValue) (UISize, error) {
	switch v := value.(type) {
	case lua.LNumber:
		if v > 0 {
			return UISize{Value: float64(v)}, nil
		}
	case lua.LString:
		if n, ok := strings.CutSuffix(strings.TrimSpace(string(v)), "%"); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil && f > 0 && f <= 100 {
				return UISize{Value: f, Percent: true}, nil
			}
		}
	}
	return UISize{}, fmt.Errorf("must be logical pixels, more than 0, or a percentage such as \"30%%\"")
}

func newLuaView(L *lua.LState, rt *Runtime, overlay UIOverlay) *lua.LTable {
	view := L.NewTable()
	// While the view is shown its state is the host's. Once it closes the
	// host hands back what it ended with, final, which is read until it is
	// shown again from its spec; reading it from on_close sees what was
	// typed.
	var final *UIViewState
	shown := func() bool { return rt.host != nil && rt.host.UIVisible(overlay.ID) }
	L.SetField(view, "id", lua.LString(overlay.ID))
	L.SetField(view, "title", lua.LString(overlay.Title))
	L.SetField(view, "show", L.NewFunction(func(L *lua.LState) int {
		if rt.host == nil {
			L.RaiseError("ui.view.show: viewer host unavailable")
		}
		final = nil
		rt.openViews[overlay.ID] = func(state UIViewState) { final = &state }
		if err := rt.host.ShowUI(overlay); err != nil {
			delete(rt.openViews, overlay.ID)
			L.RaiseError("ui.view.show: %v", err)
		}
		return 0
	}))
	L.SetField(view, "close", L.NewFunction(func(L *lua.LState) int {
		if shown() {
			rt.host.CloseUI(overlay.ID)
		}
		return 0
	}))
	L.SetField(view, "set_rows", L.NewFunction(func(L *lua.LState) int {
		overlay.Rows = luaTableUIRows(L, L.CheckAny(2))
		if shown() {
			rt.host.SetUIRows(overlay.ID, overlay.Rows)
		}
		return 0
	}))
	L.SetField(view, "set_selected", L.NewFunction(func(L *lua.LState) int {
		overlay.Selected = L.CheckInt(2)
		if shown() {
			rt.host.SetUISelected(overlay.ID, overlay.Selected)
		} else if final != nil {
			final.Selected = overlay.Selected
		}
		return 0
	}))
	L.SetField(view, "set_scroll", L.NewFunction(func(L *lua.LState) int {
		overlay.Scroll = L.CheckInt(2)
		if shown() {
			rt.host.SetUIScroll(overlay.ID, overlay.Scroll)
		} else if final != nil {
			final.Scroll = overlay.Scroll
		}
		return 0
	}))
	L.SetField(view, "set_query", L.NewFunction(func(L *lua.LState) int {
		overlay.Query = L.CheckString(2)
		if shown() {
			rt.host.SetUIQuery(overlay.ID, overlay.Query)
		} else if final != nil {
			final.Query = overlay.Query
		}
		return 0
	}))
	L.SetField(view, "field", L.NewFunction(func(L *lua.LState) int {
		field := L.CheckString(2)
		switch {
		case shown():
			L.Push(lua.LString(rt.host.UIField(overlay.ID, field)))
		case final != nil:
			L.Push(lua.LString(final.Fields[field]))
		default:
			L.Push(lua.LString(overlay.Fields[field]))
		}
		return 1
	}))
	L.SetField(view, "set_field", L.NewFunction(func(L *lua.LState) int {
		field, text := L.CheckString(2), L.CheckString(3)
		if overlay.Fields == nil {
			overlay.Fields = map[string]string{}
		}
		overlay.Fields[field] = text
		if shown() {
			rt.host.SetUIField(overlay.ID, field, text)
		} else if final != nil {
			final.Fields[field] = text
		}
		return 0
	}))
	L.SetField(view, "focus", L.NewFunction(func(L *lua.LState) int {
		if shown() {
			rt.host.FocusUI(overlay.ID, true)
		}
		return 0
	}))
	L.SetField(view, "blur", L.NewFunction(func(L *lua.LState) int {
		if shown() {
			rt.host.FocusUI(overlay.ID, false)
		}
		return 0
	}))
	L.SetField(view, "focused", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LBool(shown() && rt.host.UIFocused(overlay.ID)))
		return 1
	}))
	L.SetField(view, "size", L.NewFunction(func(L *lua.LState) int {
		w, h := 0.0, 0.0
		if shown() {
			w, h = rt.host.UIViewSize(overlay.ID)
		} else if final != nil {
			w, h = final.W, final.H
		}
		L.Push(lua.LNumber(w))
		L.Push(lua.LNumber(h))
		return 2
	}))
	L.SetField(view, "redraw", L.NewFunction(func(L *lua.LState) int {
		if shown() {
			rt.host.RedrawUI(overlay.ID)
		}
		return 0
	}))
	L.SetField(view, "visible", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LBool(shown()))
		return 1
	}))
	L.SetField(view, "selected", L.NewFunction(func(L *lua.LState) int {
		switch {
		case shown():
			L.Push(lua.LNumber(rt.host.UISelected(overlay.ID)))
		case final != nil:
			L.Push(lua.LNumber(final.Selected))
		default:
			L.Push(lua.LNumber(overlay.Selected))
		}
		return 1
	}))
	L.SetField(view, "scroll", L.NewFunction(func(L *lua.LState) int {
		switch {
		case shown():
			L.Push(lua.LNumber(rt.host.UIScroll(overlay.ID)))
		case final != nil:
			L.Push(lua.LNumber(final.Scroll))
		default:
			L.Push(lua.LNumber(overlay.Scroll))
		}
		return 1
	}))
	L.SetField(view, "query", L.NewFunction(func(L *lua.LState) int {
		switch {
		case shown():
			L.Push(lua.LString(rt.host.UIQuery(overlay.ID)))
		case final != nil:
			L.Push(lua.LString(final.Query))
		default:
			L.Push(lua.LString(overlay.Query))
		}
		return 1
	}))
	return view
}

// UIViewClosed hands a view's state as it closed back to the plugin's view
// object, which reads it until the view is shown again; the host need keep
// nothing of a view once it closes.
func (r *Runtime) UIViewClosed(id string, state UIViewState) {
	if r == nil {
		return
	}
	if closed, ok := r.openViews[id]; ok {
		delete(r.openViews, id)
		closed(state)
	}
}

func registerLuaFunctions(L *lua.LState, table *lua.LTable, prefix string, functions []luaFunctionSpec) {
	registered := make(map[string]lua.LGFunction, len(functions))
	for _, spec := range functions {
		name, ok := strings.CutPrefix(spec.Signature, prefix)
		if !ok {
			panic("Lua function signature has wrong prefix: " + spec.Signature)
		}
		name, _, ok = strings.Cut(name, "(")
		if !ok || name == "" {
			panic("invalid Lua function metadata: " + spec.Signature)
		}
		if _, exists := registered[name]; exists {
			panic("duplicate Lua function: " + prefix + name)
		}
		if spec.Function == nil {
			panic("missing Lua function implementation: " + spec.Signature)
		}
		registered[name] = spec.Function
		luaFunctionReferencesMu.Lock()
		luaFunctionReferences[spec.Signature] = LuaReferenceEntry{Signature: spec.Signature}
		luaFunctionReferencesMu.Unlock()
	}
	L.SetFuncs(table, registered)
}

func luaTableStrings(value lua.LValue) []string {
	tbl, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}
	values := make([]string, 0, tbl.Len())
	for i := 1; i <= tbl.Len(); i++ {
		values = append(values, lua.LVAsString(tbl.RawGetInt(i)))
	}
	return values
}

func luaTableUIRows(L *lua.LState, value lua.LValue) []UIListRow {
	tbl, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}
	rows := make([]UIListRow, 0, tbl.Len())
	for i := 1; i <= tbl.Len(); i++ {
		item := tbl.RawGetInt(i)
		if text, ok := item.(lua.LString); ok {
			rows = append(rows, UIListRow{Text: string(text), Value: string(text), Lua: item})
			continue
		}
		spec, ok := item.(*lua.LTable)
		if !ok {
			continue
		}
		text := lua.LVAsString(spec.RawGetString("text"))
		value := lua.LVAsString(spec.RawGetString("value"))
		if value == "" {
			value = text
		}
		row := UIListRow{
			Text:      text,
			Value:     value,
			ID:        lua.LVAsString(spec.RawGetString("id")),
			Secondary: lua.LVAsString(spec.RawGetString("secondary")),
			Depth:     int(lua.LVAsNumber(spec.RawGetString("depth"))),
			Disabled:  lua.LVAsBool(spec.RawGetString("disabled")),
			Key:       lua.LVAsString(spec.RawGetString("key")),
			Marker:    lua.LVAsString(spec.RawGetString("marker")),
			Heading:   lua.LVAsBool(spec.RawGetString("heading")),
			Button:    lua.LVAsBool(spec.RawGetString("button")),
			Lua:       item,
		}
		if checked := spec.RawGetString("checked"); checked != lua.LNil {
			row.Checkbox, row.Checked = true, lua.LVAsBool(checked)
		}
		if cells, ok := spec.RawGetString("cells").(*lua.LTable); ok {
			for j := 1; j <= cells.Len(); j++ {
				row.Cells = append(row.Cells, lua.LVAsString(cells.RawGetInt(j)))
			}
			if row.Text == "" {
				row.Text = strings.Join(row.Cells, " ")
			}
			if row.Value == "" && len(row.Cells) > 0 {
				row.Value = row.Cells[0]
			}
		}
		if swatch := spec.RawGetString("swatch"); swatch != lua.LNil {
			c, err := colorFromLua(swatch)
			if err != nil {
				L.RaiseError("ui rows: row %d swatch: %v", i, err)
			}
			row.Swatch = &c
		}
		rows = append(rows, row)
	}
	return rows
}

func luaStringsTable(L *lua.LState, values []string) *lua.LTable {
	tbl := L.NewTable()
	for i, value := range values {
		tbl.RawSetInt(i+1, lua.LString(value))
	}
	return tbl
}

func newLuaActionValue(L *lua.LState, rt *Runtime, action string) *lua.LTable {
	tbl := L.NewTable()
	L.SetField(tbl, "__onon_action", lua.LString(action))
	mt := L.NewTable()
	L.SetField(mt, "__call", L.NewFunction(func(L *lua.LState) int {
		if err := rt.executeAction(action); err != nil {
			L.RaiseError("%s: %v", action, err)
		}
		return 0
	}))
	L.SetField(mt, "__tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString(action))
		return 1
	}))
	L.SetMetatable(tbl, mt)
	return tbl
}

func luaActionName(rt *Runtime, value lua.LValue) (string, error) {
	if fn, ok := value.(*lua.LFunction); ok {
		return rt.registerCallback(fn), nil
	}
	if tbl, ok := value.(*lua.LTable); ok {
		if action := tbl.RawGetString("__onon_action"); action.Type() == lua.LTString {
			return action.String(), nil
		}
	}
	if value.Type() != lua.LTString {
		return "", fmt.Errorf("expected action string, action value, or function")
	}
	action := value.String()
	if rt.actionExists(action) {
		return action, nil
	}
	return "", fmt.Errorf("unknown action %q", action)
}

func (r *Runtime) registerCallback(fn *lua.LFunction) string {
	r.callbackSeq++
	id := fmt.Sprintf("__lua_callback_%d", r.callbackSeq)
	r.callbacks[id] = fn
	return id
}

// setKeyBinding stores a binding under the key's canonical name, so every
// spelling of a key replaces, and unbindKey removes, the same entry.
func (r *Runtime) setKeyBinding(key, action string) error {
	seq, err := keys.ParseSequence(key)
	if err != nil {
		return err
	}
	if err := checkFirstKey(seq[0]); err != nil {
		return err
	}
	r.cfg.KeyBindings[keys.Format(seq)] = action
	r.dirty = true
	return nil
}

// checkFirstKey refuses the keys the viewer reads before any binding: digits
// 1 to 9 start a count, and ' and " jump to and set marks. A sequence that
// began with one could never run.
func checkFirstKey(key keys.Key) error {
	text, _ := key.Text()
	switch {
	case len(text) == 1 && text >= "1" && text <= "9":
		return fmt.Errorf("digits 1 to 9 start a count, so a binding cannot begin with one")
	case text == "'" || text == "\"":
		return fmt.Errorf("%s is used for marks, so a binding cannot begin with it", text)
	}
	return nil
}

func (r *Runtime) unbindKey(key string) error {
	key, err := keys.Normalize(key)
	if err != nil {
		return err
	}
	delete(r.cfg.KeyBindings, key)
	r.dirty = true
	return nil
}

func (r *Runtime) setMouseBinding(event, action string) error {
	event, err := keys.NormalizeMouseEvent(event)
	if err != nil {
		return err
	}
	r.cfg.MouseBindings[event] = action
	r.dirty = true
	return nil
}

func (r *Runtime) unbindMouse(event string) error {
	event, err := keys.NormalizeMouseEvent(event)
	if err != nil {
		return err
	}
	delete(r.cfg.MouseBindings, event)
	r.dirty = true
	return nil
}

func (r *Runtime) setOption(name string, value lua.LValue) error {
	name = normalizeOptionName(name)
	if isThemeOption(name) {
		r.recordThemeOp(themeOp{name: name, value: value})
	}
	if desc, ok := lookupOption(name); ok {
		if err := desc.apply(&r.cfg, value); err != nil && !isSkipped(err) {
			return err
		} else if err != nil {
			r.warn(err)
		}
		r.markAssigned(name)
	} else {
		if _, ok := r.pluginOption(name); !ok {
			if err := movedOptionError(name); err != nil {
				return err
			}
			return fmt.Errorf("unknown setting")
		}
		if err := r.setPluginOptionValue(name, value); err != nil {
			return err
		}
	}
	r.dirty = true
	return nil
}
