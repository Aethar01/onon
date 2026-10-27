package config

import (
	"fmt"
	"path/filepath"
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
		Rows:       luaTableUIRows(spec.RawGetString("rows")),
		Selected:   1,
		Searchable: true,
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
	if searchable := spec.RawGetString("searchable"); searchable.Type() == lua.LTBool {
		overlay.Searchable = lua.LVAsBool(searchable)
	}
	if fn, ok := spec.RawGetString("on_select").(*lua.LFunction); ok {
		overlay.OnSelect = rt.registerCallback(fn)
	}
	if fn, ok := spec.RawGetString("on_close").(*lua.LFunction); ok {
		overlay.OnClose = rt.registerCallback(fn)
	}
	return overlay
}

func newLuaView(L *lua.LState, rt *Runtime, overlay UIOverlay) *lua.LTable {
	view := L.NewTable()
	L.SetField(view, "id", lua.LString(overlay.ID))
	L.SetField(view, "title", lua.LString(overlay.Title))
	L.SetField(view, "show", L.NewFunction(func(L *lua.LState) int {
		if rt.host == nil {
			L.RaiseError("ui.view.show: viewer host unavailable")
		}
		if err := rt.host.ShowUI(overlay); err != nil {
			L.RaiseError("ui.view.show: %v", err)
		}
		return 0
	}))
	L.SetField(view, "close", L.NewFunction(func(L *lua.LState) int {
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			rt.host.CloseUI(overlay.ID)
		}
		return 0
	}))
	L.SetField(view, "set_rows", L.NewFunction(func(L *lua.LState) int {
		overlay.Rows = luaTableUIRows(L.CheckAny(2))
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			rt.host.SetUIRows(overlay.ID, overlay.Rows)
		}
		return 0
	}))
	L.SetField(view, "set_selected", L.NewFunction(func(L *lua.LState) int {
		overlay.Selected = L.CheckInt(2)
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			rt.host.SetUISelected(overlay.ID, overlay.Selected)
		}
		return 0
	}))
	L.SetField(view, "set_scroll", L.NewFunction(func(L *lua.LState) int {
		overlay.Scroll = L.CheckInt(2)
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			rt.host.SetUIScroll(overlay.ID, overlay.Scroll)
		}
		return 0
	}))
	L.SetField(view, "set_query", L.NewFunction(func(L *lua.LState) int {
		overlay.Query = L.CheckString(2)
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			rt.host.SetUIQuery(overlay.ID, overlay.Query)
		}
		return 0
	}))
	L.SetField(view, "visible", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LBool(rt.host != nil && rt.host.UIVisible(overlay.ID)))
		return 1
	}))
	L.SetField(view, "selected", L.NewFunction(func(L *lua.LState) int {
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			L.Push(lua.LNumber(rt.host.UISelected(overlay.ID)))
		} else {
			L.Push(lua.LNumber(overlay.Selected))
		}
		return 1
	}))
	L.SetField(view, "scroll", L.NewFunction(func(L *lua.LState) int {
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			L.Push(lua.LNumber(rt.host.UIScroll(overlay.ID)))
		} else {
			L.Push(lua.LNumber(overlay.Scroll))
		}
		return 1
	}))
	L.SetField(view, "query", L.NewFunction(func(L *lua.LState) int {
		if rt.host != nil && rt.host.UIVisible(overlay.ID) {
			L.Push(lua.LString(rt.host.UIQuery(overlay.ID)))
		} else {
			L.Push(lua.LString(overlay.Query))
		}
		return 1
	}))
	return view
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

func luaTableUIRows(value lua.LValue) []UIListRow {
	tbl, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}
	rows := make([]UIListRow, 0, tbl.Len())
	for i := 1; i <= tbl.Len(); i++ {
		item := tbl.RawGetInt(i)
		if text, ok := item.(lua.LString); ok {
			rows = append(rows, UIListRow{Text: string(text), Value: string(text)})
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
		rows = append(rows, UIListRow{
			Text:      text,
			Value:     value,
			ID:        lua.LVAsString(spec.RawGetString("id")),
			Secondary: lua.LVAsString(spec.RawGetString("secondary")),
			Depth:     int(lua.LVAsNumber(spec.RawGetString("depth"))),
			Disabled:  lua.LVAsBool(spec.RawGetString("disabled")),
		})
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
