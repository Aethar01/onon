package config

import (
	"fmt"
	"log"
	"strings"

	"onon/internal/filepicker"

	lua "github.com/yuin/gopher-lua"
)

// luaBind binds a key sequence to an action or Lua callback.
//
// # Parameters
//
//   - key: A printable key, special key name, modified key, or multi-key
//     sequence, such as `j`, `<PgDn>`, `<C-p>`, or `gg`. See
//     [Key and mouse names](#key-and-mouse-names).
//   - action: A bindable action such as onon.next_page, or a Lua callback.
//
// # Returns
//
// Nothing. A later binding replaces an earlier binding for the same key.
//
// # Errors
//
// Raises an error when the key or action is invalid.
//
// # Example
//
//	onon.bind("<C-p>", function()
//	  onon.page(1)
//	end)
func luaBind(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)
		action := L.CheckAny(2)
		actionName, err := luaActionName(rt, action)
		if err != nil {
			L.RaiseError("bind %q: %v", key, err)
		}
		if err := rt.setKeyBinding(key, actionName); err != nil {
			L.RaiseError("bind %q: %v", key, err)
		}
		return 0
	}
}

// luaUnbind removes a key binding.
//
// # Parameters
//
//   - key: The key or key sequence to remove.
//
// # Returns
//
// Nothing. Removing an unbound key is harmless.
//
// # Errors
//
// Raises an error when the key is invalid.
//
// # Example
//
//	onon.unbind("<C-p>")
func luaUnbind(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		key := L.CheckString(1)
		if err := rt.unbindKey(key); err != nil {
			L.RaiseError("unbind %q: %v", key, err)
		}
		return 0
	}
}

// luaBindMouse binds a mouse event to an action or Lua callback.
//
// # Parameters
//
//   - event: A mouse event such as `wheel_down`, `left_down`, or
//     `<C-wheel_up>`. See [Key and mouse names](#key-and-mouse-names).
//   - action: A bindable action or a Lua callback.
//
// # Returns
//
// Nothing.
//
// # Errors
//
// Raises an error when the event or action is invalid.
//
// # Example
//
//	onon.bind_mouse("<C-wheel_up>", onon.zoom_in)
func luaBindMouse(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		event := L.CheckString(1)
		actionName, err := luaActionName(rt, L.CheckAny(2))
		if err != nil {
			L.RaiseError("bind_mouse %q: %v", event, err)
		}
		if err := rt.setMouseBinding(event, actionName); err != nil {
			L.RaiseError("bind_mouse %q: %v", event, err)
		}
		return 0
	}
}

// luaUnbindMouse removes a mouse binding.
//
// # Parameters
//
//   - event: The mouse event to remove.
//
// # Returns
//
// Nothing. Removing an unbound event is harmless.
//
// # Errors
//
// Raises an error when the event is invalid.
//
// # Example
//
//	onon.unbind_mouse("middle_down")
func luaUnbindMouse(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		event := L.CheckString(1)
		if err := rt.unbindMouse(event); err != nil {
			L.RaiseError("unbind_mouse %q: %v", event, err)
		}
		return 0
	}
}

// luaMessage reads or replaces the viewer's status message.
//
// # Parameters
//
//   - text: Optional replacement status message.
//
// # Returns
//
// The current message as a string.
//
// # Example
//
//	onon.message("ready")
//	local current = onon.message()
func luaMessage(rt *Runtime, cfg *Config) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("message: viewer host unavailable")
			}
			rt.host.SetMessage(L.CheckString(1))
		}
		if rt.host == nil {
			L.Push(lua.LString(cfg.NormalMessage))
			return 1
		}
		L.Push(lua.LString(rt.host.Message()))
		return 1
	}
}

// luaCommand executes a viewer command.
//
// # Parameters
//
//   - command: A viewer command, with or without the leading colon.
//
// # Returns
//
// Nothing.
//
// # Errors
//
// Raises an error when the command is invalid or cannot be executed.
//
// # Example
//
//	onon.command(":fit width")
func luaCommand(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.RaiseError("command: viewer host unavailable")
		}
		if err := rt.host.RunCommand(L.CheckString(1)); err != nil {
			L.RaiseError("command: %v", err)
		}
		return 0
	}
}

// luaOpen opens another document.
//
// # Parameters
//
//   - path: A document path. Relative paths resolve from the current document directory.
//
// # Returns
//
// Nothing. Opening is applied by the viewer after the current Lua dispatch.
//
// # Errors
//
// Raises an error when the document cannot be opened.
//
// # Example
//
//	onon.open("appendix.pdf")
func luaOpen(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if err := rt.open(L.CheckString(1)); err != nil {
			L.RaiseError("open: %v", err)
		}
		return 0
	}
}

// luaPickFile opens the native document picker, filtered to the formats this
// build can open, and invokes a callback with its result.
//
// # Parameters
//
//   - callback: Receives { success, path, cancelled, error } after the native picker closes.
//
// # Returns
//
// Nothing. The picker and callback complete before this function returns.
//
// # Errors
//
// Raises an error when the callback fails.
//
// # Example
//
//	onon.pick_file(function(result)
//	  if result.success then onon.open(result.path) end
//	end)
func luaPickFile(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		fn, ok := L.Get(1).(*lua.LFunction)
		if !ok {
			L.RaiseError("pick_file: expected callback")
		}
		var extensions []string
		if host, ok := rt.host.(DocumentFormatHost); ok {
			extensions = host.SupportedExtensions()
		}
		path, err := filepicker.PickDocument(extensions)
		result := L.NewTable()
		L.SetField(result, "success", lua.LBool(err == nil && path != ""))
		L.SetField(result, "path", lua.LString(path))
		L.SetField(result, "cancelled", lua.LBool(err == nil && path == ""))
		L.SetField(result, "error", lua.LString(errorString(err)))
		if err := rt.callLua(lua.P{Fn: fn, NRet: 0, Protect: true}, result); err != nil {
			L.RaiseError("pick_file: %v", err)
		}
		return 0
	}
}

// luaPickDirectory opens the native directory picker and invokes a callback with its result.
//
// # Parameters
//
//   - callback: Receives { success, path, cancelled, error } after the native picker closes.
//
// # Returns
//
// Nothing. The callback completes before this function returns.
//
// # Example
//
//	onon.pick_directory(function(result)
//	  if result.success then onon.message(result.path) end
//	end)
func luaPickDirectory(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		fn, ok := L.Get(1).(*lua.LFunction)
		if !ok {
			L.RaiseError("pick_directory: expected callback")
		}
		path := ""
		var err error
		if picker, ok := rt.host.(DirectoryPicker); ok {
			path, err = picker.PickDirectory()
		} else {
			path, err = filepicker.PickDirectory()
		}
		result := luaTableFromMap(L, map[string]any{"success": err == nil && path != "", "path": path, "cancelled": err == nil && path == "", "error": errorString(err)})
		if err := rt.callLua(lua.P{Fn: fn, NRet: 0, Protect: true}, result); err != nil {
			L.RaiseError("pick_directory: %v", err)
		}
		return 0
	}
}

// luaSchedule queues a callback on the main Lua thread after the current dispatch.
//
// # Parameters
//
//   - callback: A zero-argument callback to run on the main Lua thread after the current dispatch.
//
// # Returns
//
// An operation handle with numeric id, cancel(), and active() members.
//
// # Example
//
//	local handle = onon.schedule(function() onon.message("later") end)
func luaSchedule(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		fn, ok := L.Get(1).(*lua.LFunction)
		if !ok {
			L.RaiseError("schedule: expected callback")
		}
		id, err := rt.schedule(fn)
		if err != nil {
			L.RaiseError("schedule: %v", err)
		}
		L.Push(newPluginOperationHandle(L, rt, id))
		return 1
	}
}

// luaLog writes a diagnostic without changing the user-facing message.
//
// # Parameters
//
//   - level: debug, info, warn, or error.
//   - message: Diagnostic text written to the application log, tagged with
//     the plugin responsible, or config outside plugin code.
//
// # Returns
//
// Nothing. The viewer's user-facing message is unchanged.
//
// # Example
//
//	onon.log("info", "plugin loaded")
func luaLog(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		level := strings.ToLower(strings.TrimSpace(L.CheckString(1)))
		if level != "debug" && level != "info" && level != "warn" && level != "error" {
			L.RaiseError("log: invalid level %q", level)
		}
		log.Printf("plugin=%s level=%s %s", rt.diagnosticPluginID(), level, L.CheckString(2))
		return 0
	}
}

// luaOpenExternal asks the operating system to open a URI or path.
//
// # Parameters
//
//   - uri_or_path: A URI or filesystem path for the operating system's default application.
//
// # Returns
//
// Nothing.
//
// # Errors
//
// Raises an error when the operating-system opener cannot be started.
//
// # Example
//
//	onon.open_external("https://example.com")
func luaOpenExternal(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		opener, ok := rt.host.(ExternalOpener)
		if !ok {
			L.RaiseError("open_external: viewer host unavailable")
		}
		if err := opener.OpenExternal(L.CheckString(1)); err != nil {
			L.RaiseError("open_external: %v", err)
		}
		return 0
	}
}

// luaPage reads or changes the current physical page.
//
// # Parameters
//
//   - page: Optional 1-based physical page number to navigate to.
//
// # Returns
//
// The current 1-based page number, or nil when no document is open.
//
// # Example
//
//	local page = onon.page()
//	onon.page(page + 1)
func luaPage(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("page: viewer host unavailable")
			}
			if err := rt.host.GotoPage(L.CheckInt(1)); err != nil {
				L.RaiseError("page: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(rt.host.Page()))
		return 1
	}
}

// luaPageCount returns the current document's page count.
//
// # Returns
//
// The number of pages, or nil when no document is open.
//
// # Example
//
//	if (onon.page_count() or 0) > 100 then onon.message("long document") end
func luaPageCount(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(rt.host.PageCount()))
		return 1
	}
}

// luaGotoDocumentPoint moves to a page coordinate in document space.
//
// # Parameters
//
//   - spec: A table containing 1-based integer page and numeric x and y coordinates.
//
// # Returns
//
// Nothing. Coordinates are measured from the page's top-left corner in document points.
//
// # Example
//
//	onon.goto_document_point({ page = 3, x = 72, y = 144 })
func luaGotoDocumentPoint(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.RaiseError("goto_document_point: viewer host unavailable")
		}
		spec, ok := L.CheckAny(1).(*lua.LTable)
		if !ok {
			L.RaiseError("goto_document_point: expected table")
		}
		if err := rt.host.GotoDocumentPoint(int(lua.LVAsNumber(spec.RawGetString("page"))), float64(lua.LVAsNumber(spec.RawGetString("x"))), float64(lua.LVAsNumber(spec.RawGetString("y")))); err != nil {
			L.RaiseError("goto_document_point: %v", err)
		}
		return 0
	}
}

// luaVersion returns onon's version.
//
// # Returns
//
// The release tag onon was built from, such as v0.7.0, or dev for a build not
// made from a tag.
//
// # Example
//
//	onon.log("info", "running onon " .. onon.version())
func luaVersion(L *lua.LState) int {
	L.Push(lua.LString(Version))
	return 1
}

// luaMode returns the current viewer input mode.
//
// # Returns
//
// The current input mode: normal, command, goto, or search; nil without a viewer host.
//
// # Example
//
//	if onon.mode() == "normal" then onon.message("ready") end
func luaMode(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LString(rt.host.Mode()))
		return 1
	}
}

// luaSearch starts an asynchronous document search.
//
// # Parameters
//
//   - query: Search text, which may contain search modifiers such as `\v` or `\<`.
//   - backward: Optional boolean selecting backward search.
//
// # Returns
//
// Nothing. Matching runs asynchronously.
//
// # Example
//
//	onon.search("-iw introduction")
func luaSearch(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.RaiseError("search: viewer host unavailable")
		}
		backward := false
		if L.GetTop() >= 2 {
			backward = lua.LVAsBool(L.CheckAny(2))
		}
		if err := rt.host.Search(L.CheckString(1), backward); err != nil {
			L.RaiseError("search: %v", err)
		}
		return 0
	}
}

// luaSearchQuery returns the active search query.
//
// # Returns
//
// The active query as typed, modifiers included, or an empty string when search is inactive.
//
// # Example
//
//	local query = onon.search_query()
func luaSearchQuery(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LString(""))
			return 1
		}
		L.Push(lua.LString(rt.host.SearchQuery()))
		return 1
	}
}

// luaSearchMatchCount returns the number of discovered search matches.
//
// # Returns
//
// The number of matches discovered so far. Search may still be running.
//
// # Example
//
//	onon.message(onon.search_match_count() .. " matches")
func luaSearchMatchCount(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LNumber(0))
			return 1
		}
		L.Push(lua.LNumber(rt.host.SearchMatchCount()))
		return 1
	}
}

// luaSearchMatchIndex returns the current search match index.
//
// # Returns
//
// The current 1-based match index, or nil when no match is focused.
//
// # Example
//
//	local index = onon.search_match_index()
func luaSearchMatchIndex(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LNil)
			return 1
		}
		index := rt.host.SearchMatchIndex()
		if index <= 0 {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(index))
		return 1
	}
}

// luaCurrentCount returns the pending numeric action count.
//
// # Returns
//
// The pending numeric prefix as a string, or an empty string when no count is pending.
//
// # Example
//
//	onon.message("count: " .. onon.current_count())
func luaCurrentCount(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LString(""))
			return 1
		}
		L.Push(lua.LString(rt.host.CurrentCount()))
		return 1
	}
}

// luaPendingKeys returns pending key-sequence tokens.
//
// # Returns
//
// An array of pending key-sequence tokens.
//
// # Example
//
//	for _, key in ipairs(onon.pending_keys()) do onon.log("debug", key) end
func luaPendingKeys(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(L.NewTable())
			return 1
		}
		L.Push(luaStringsTable(L, rt.host.PendingKeys()))
		return 1
	}
}

// luaRecentFiles returns recent document paths.
//
// # Parameters
//
//   - limit: Optional maximum number of paths. Defaults to recent_files_max.
//
// # Returns
//
// An array of document paths, or an empty array when session storage is disabled.
func luaRecentFiles(cfg *Config) lua.LGFunction {
	return func(L *lua.LState) int {
		if !cfg.SessionDatabase {
			L.Push(L.NewTable())
			return 1
		}
		limit := cfg.RecentFilesMax
		if L.GetTop() > 0 {
			limit = L.CheckInt(1)
		}
		L.Push(luaStringsTable(L, RecentFiles(limit)))
		return 1
	}
}

// luaClearPendingKeys clears pending key, mark, and numeric-count input.
//
// # Returns
//
// Nothing.
//
// # Example
//
//	onon.clear_pending_keys()
func luaClearPendingKeys(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host != nil {
			rt.host.ClearPendingKeys()
		}
		return 0
	}
}

// luaFitMode reads or changes the fit mode. Valid modes are manual, page, width,
// and height.
//
// # Parameters
//
//   - mode: Optional manual, page, width, or height value. Omit it to read the current mode.
//
// # Returns
//
// The active fit mode.
//
// # Errors
//
// Raises an error for an invalid mode or when setting it without a viewer host.
//
// # Example
//
//	onon.fit_mode("width")
func luaFitMode(rt *Runtime, cfg *Config) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("fit_mode: viewer host unavailable")
			}
			if err := rt.host.SetFitMode(L.CheckString(1)); err != nil {
				L.RaiseError("fit_mode: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LString(cfg.FitMode))
			return 1
		}
		L.Push(lua.LString(rt.host.FitMode()))
		return 1
	}
}

// luaRenderMode reads or changes the render mode. Valid modes are continuous and single.
//
// # Parameters
//
//   - mode: Optional continuous or single value. Omit it to read the current mode.
//
// # Returns
//
// The active render mode.
//
// # Errors
//
// Raises an error for an invalid mode or when setting it without a viewer host.
//
// # Example
//
//	onon.render_mode("single")
func luaRenderMode(rt *Runtime, cfg *Config) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("render_mode: viewer host unavailable")
			}
			if err := rt.host.SetRenderMode(L.CheckString(1)); err != nil {
				L.RaiseError("render_mode: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LString(cfg.RenderMode))
			return 1
		}
		L.Push(lua.LString(rt.host.RenderMode()))
		return 1
	}
}

// luaZoom reads or changes the zoom and clamps values to the configured range.
//
// # Parameters
//
//   - scale: Optional manual zoom scale. Omit it to read the current scale.
//
// # Returns
//
// The current zoom scale, or nil without a viewer host.
//
// # Errors
//
// Raises an error when the scale is invalid or cannot be set.
//
// # Example
//
//	onon.zoom(1.5)
func luaZoom(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("zoom: viewer host unavailable")
			}
			if err := rt.host.SetZoom(float64(L.CheckNumber(1))); err != nil {
				L.RaiseError("zoom: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(rt.host.Zoom()))
		return 1
	}
}

// luaRotation reads or changes clockwise rotation in degrees.
//
// # Parameters
//
//   - degrees: Optional clockwise rotation. Omit it to read the current rotation.
//
// # Returns
//
// The normalized clockwise rotation, or nil without a viewer host.
//
// # Example
//
//	onon.rotation(90)
func luaRotation(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("rotation: viewer host unavailable")
			}
			if err := rt.host.SetRotation(float64(L.CheckNumber(1))); err != nil {
				L.RaiseError("rotation: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LNumber(rt.host.Rotation()))
		return 1
	}
}

// luaFullscreen reads or changes fullscreen state.
//
// # Parameters
//
//   - enabled: Optional boolean. Omit it to read the current state.
//
// # Returns
//
// The current fullscreen state.
//
// # Example
//
//	onon.fullscreen(true)
func luaFullscreen(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("fullscreen: viewer host unavailable")
			}
			if err := rt.host.SetFullscreen(lua.LVAsBool(L.CheckAny(1))); err != nil {
				L.RaiseError("fullscreen: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LFalse)
			return 1
		}
		L.Push(lua.LBool(rt.host.Fullscreen()))
		return 1
	}
}

// luaCacheEntries returns the number of cached page tiles and thumbnails.
//
// # Returns
//
// The number of rendered textures currently held in the render cache.
//
// # Example
//
//	onon.log("debug", tostring(onon.cache.entries()))
func luaCacheEntries(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LNumber(0))
			return 1
		}
		L.Push(lua.LNumber(rt.host.CacheEntries()))
		return 1
	}
}

// luaCachePending returns the number of queued tile renders.
//
// # Returns
//
// The number of tile renders waiting to be processed.
//
// # Example
//
//	if onon.cache.pending() > 0 then onon.message("rendering") end
func luaCachePending(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host == nil {
			L.Push(lua.LNumber(0))
			return 1
		}
		L.Push(lua.LNumber(rt.host.CachePending()))
		return 1
	}
}

// luaCacheLimit reads or changes the render cache memory limit in MiB.
//
// An omitted limit reads the current limit; supplying an integer changes it and
// returns the resulting limit. 0 removes the limit.
func luaCacheLimit(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if L.GetTop() > 0 {
			if rt.host == nil {
				L.RaiseError("cache.limit: viewer host unavailable")
			}
			if err := rt.host.SetCacheLimit(L.CheckInt(1)); err != nil {
				L.RaiseError("cache.limit: %v", err)
			}
		}
		if rt.host == nil {
			L.Push(lua.LNumber(0))
			return 1
		}
		L.Push(lua.LNumber(rt.host.CacheLimit()))
		return 1
	}
}

// luaCacheClear drops all rendered tiles and thumbnails.
//
// # Returns
//
// Nothing.
//
// # Example
//
//	onon.cache.clear()
func luaCacheClear(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if rt.host != nil {
			rt.host.ClearCache()
		}
		return 0
	}
}

// luaUiCreate creates a view: a searchable list, or a panel a plugin
// draws itself.
//
// The spec may contain id, title, rows, selected, scroll, query, searchable,
// filter, modal, width, height, anchor, draw, and the callbacks on_select,
// on_selection_changed, on_query_changed, on_key, on_action, on_mouse, and
// on_close. Rows may be strings or row tables with text, value, id,
// secondary, depth, disabled, key, marker, heading, and swatch fields. See
// UI views below.
//
// # Returns
//
// A view object with show, close, visible, redraw, row/query setters, and state accessors.
//
// # Example
//
//	local view = onon.ui.create({
//	  title = "Recent files",
//	  rows = onon.recent_files(10),
//	  on_select = function(_, path) onon.open(path) end,
//	})
//	view:show()
func luaUiCreate(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		spec, ok := L.CheckAny(1).(*lua.LTable)
		if !ok {
			L.RaiseError("ui.create: expected table")
		}
		overlay := uiOverlayFromLuaSpec(L, rt, spec)
		rt.uiSeq++
		overlay.ID = fmt.Sprintf("lua:%s:%d", overlay.ID, rt.uiSeq)
		L.Push(newLuaView(L, rt, overlay))
		return 1
	}
}

// luaClipboardGetText returns the current clipboard contents as UTF-8 text.
//
// # Returns
//
// The current clipboard contents as UTF-8 text.
//
// # Errors
//
// Raises an error when clipboard access is unavailable.
func luaClipboardGetText(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		getter, ok := rt.host.(ClipboardGetter)
		if !ok {
			L.RaiseError("clipboard.get_text: viewer host unavailable")
		}
		L.Push(lua.LString(getter.GetClipboard()))
		return 1
	}
}

// luaClipboardSetText replaces the clipboard with UTF-8 text.
//
// # Parameters
//
//   - text: UTF-8 text to place on the clipboard.
//
// # Returns
//
// Nothing.
func luaClipboardSetText(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		setter, ok := rt.host.(ClipboardSetter)
		if !ok {
			L.RaiseError("clipboard.set_text: viewer host unavailable")
		}
		if err := setter.SetClipboard(L.CheckString(1)); err != nil {
			L.RaiseError("clipboard.set_text: %v", err)
		}
		return 0
	}
}

// luaFormatsExtensions returns extensions supported by the linked document engine.
//
// # Returns
//
// An alphabetically sorted array of lower-case extensions without leading dots.
//
// # Example
//
//	for _, extension in ipairs(onon.formats.extensions()) do
//	  onon.log("debug", extension)
//	end
func luaFormatsExtensions(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		host, ok := rt.host.(DocumentFormatHost)
		if !ok {
			L.RaiseError("formats.extensions: document engine unavailable")
		}
		result := L.NewTable()
		for _, ext := range host.SupportedExtensions() {
			result.Append(lua.LString(ext))
		}
		L.Push(result)
		return 1
	}
}

// luaFormatsSupports checks whether a path extension names a supported format.
//
// # Parameters
//
//   - path: A path whose extension should be checked.
//
// # Returns
//
// True when the extension names a format supported by the linked engine.
//
// # Behavior
//
// This is an extension hint. Opening also sniffs content, so a missing or misleading extension may still open.
//
// # Example
//
//	if onon.formats.supports("book.epub") then onon.open("book.epub") end
func luaFormatsSupports(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		host, ok := rt.host.(DocumentFormatHost)
		if !ok {
			L.RaiseError("formats.supports: document engine unavailable")
		}
		L.Push(lua.LBool(host.SupportsPath(L.CheckString(1))))
		return 1
	}
}

// luaPluginRegister registers and returns a lazily loaded plugin module.
//
// # Parameters
//
//   - id: The plugin ID currently being loaded by require.
//   - spec: Optional plugin option declarations.
//
// # Returns
//
// The plugin module table.
//
// # Errors
//
// Raises an error when the ID is invalid, undiscovered, disabled, or does not
// match the plugin currently being loaded.
//
// # Example
//
//	local plugin = onon.plugin.register("my-plugin")
func luaPluginRegister(runtime *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		if runtime == nil {
			L.RaiseError("plugin.register: runtime unavailable")
		}
		id := L.CheckString(1)
		var spec *lua.LTable
		if value, ok := L.Get(2).(*lua.LTable); ok {
			spec = value
		}
		module, err := runtime.registerPlugin(L, id, spec)
		if err != nil {
			L.RaiseError("plugin.register: %v", err)
		}
		L.Push(module)
		return 1
	}
}
