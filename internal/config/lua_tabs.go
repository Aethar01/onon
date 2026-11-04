package config

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// TabInfo describes one of the window's tabs, as onon.tabs.list gives it.
type TabInfo struct {
	Path      string
	Name      string
	Page      int // 1-based, or 0 before the document opens
	PageCount int
	Modified  bool
	Active    bool
}

// TabsHost is the optional extension to Host for a viewer with tabs.
// Indices count from 0.
type TabsHost interface {
	Tabs() []TabInfo
	SelectTab(index int) error
	OpenTab(path string) error
	CloseTab(index int, force bool) bool
}

func newLuaTabsTable(L *lua.LState, rt *Runtime) *lua.LTable {
	tbl := L.NewTable()
	registerLuaFunctions(L, tbl, "onon.tabs.", []luaFunctionSpec{
		{Signature: "onon.tabs.list()", Function: luaTabsList(rt)},
		{Signature: "onon.tabs.current()", Function: luaTabsCurrent(rt)},
		{Signature: "onon.tabs.select(index)", Function: luaTabsSelect(rt)},
		{Signature: "onon.tabs.open([path])", Function: luaTabsOpen(rt)},
		{Signature: "onon.tabs.close([index[, force]])", Function: luaTabsClose(rt)},
	})
	return tbl
}

func (r *Runtime) tabsHost() (TabsHost, bool) {
	host, ok := r.host.(TabsHost)
	return host, ok
}

// tabIndexArg reads the 1-based tab index at position n, or 0 when it is
// absent, for the tab shown.
func tabIndexArg(L *lua.LState, n int, name string) int {
	if L.Get(n) == lua.LNil {
		return 0
	}
	index := L.CheckInt(n)
	if index < 1 {
		L.RaiseError("tabs.%s: no tab %d", name, index)
	}
	return index
}

// resolveTabIndex counts index, as tabIndexArg reads it, from 0 among the
// tabs open now: when the call is made, which while Lua runs is once it
// returns, so that calls made one after another see each other's tabs.
func resolveTabIndex(index int, tabs []TabInfo) (int, error) {
	if index == 0 {
		for i, tab := range tabs {
			if tab.Active {
				return i, nil
			}
		}
		return 0, nil
	}
	if index > len(tabs) {
		return 0, fmt.Errorf("no tab %d of %d", index, len(tabs))
	}
	return index - 1, nil
}

// luaTabsList returns the window's tabs in order.
//
// # Returns
//
// A list of [Tab](#tab) tables, one for each tab, from the first. Tabs
// that onon.tabs calls made earlier in the same Lua dispatch open, show or
// close are not in it yet, as those calls take effect once it returns.
//
// # Example
//
//	for _, tab in ipairs(onon.tabs.list()) do
//	  onon.log("info", tab.index .. ": " .. tab.name)
//	end
func luaTabsList(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		list := L.NewTable()
		if host, ok := rt.tabsHost(); ok {
			for i, tab := range host.Tabs() {
				list.Append(luaTableFromMap(L, map[string]any{
					"index": i + 1, "path": tab.Path, "name": tab.Name, "page": tab.Page,
					"page_count": tab.PageCount, "modified": tab.Modified, "active": tab.Active,
				}))
			}
		}
		L.Push(list)
		return 1
	}
}

// luaTabsCurrent returns the index of the tab shown.
//
// # Returns
//
// The 1-based index of the tab shown.
//
// # Example
//
//	onon.message("tab " .. onon.tabs.current() .. " of " .. #onon.tabs.list())
func luaTabsCurrent(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		current := 1
		if host, ok := rt.tabsHost(); ok {
			for i, tab := range host.Tabs() {
				if tab.Active {
					current = i + 1
				}
			}
		}
		L.Push(lua.LNumber(current))
		return 1
	}
}

// luaTabsSelect shows the tab at index.
//
// # Parameters
//
//   - index: The 1-based index of the tab to show.
//
// # Returns
//
// Nothing. The tab is shown after the current Lua dispatch, once the calls
// made before it have taken effect, so index counts the tabs they leave.
//
// # Errors
//
// Raises an error when there is no tab at index.
//
// # Example
//
//	onon.tabs.select(1)
func luaTabsSelect(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		host, ok := rt.tabsHost()
		if !ok {
			L.RaiseError("tabs.select: viewer host unavailable")
		}
		index := tabIndexArg(L, 1, "select")
		if err := rt.callHost("tabs.select", func(Host) error {
			i, err := resolveTabIndex(index, host.Tabs())
			if err != nil {
				return err
			}
			return host.SelectTab(i)
		}); err != nil {
			L.RaiseError("tabs.select: %v", err)
		}
		return 0
	}
}

// luaTabsOpen opens a document in a new tab after the one shown, or shows
// the tab that already has it open.
//
// # Parameters
//
//   - path: A document path, URL or ssh host:path, as onon.open takes. Without one, a blank tab opens.
//
// # Returns
//
// Nothing. The tab opens after the current Lua dispatch.
//
// # Errors
//
// Raises an error when the document cannot be opened.
//
// # Example
//
//	onon.tabs.open("appendix.pdf")
func luaTabsOpen(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		host, ok := rt.tabsHost()
		if !ok {
			L.RaiseError("tabs.open: viewer host unavailable")
		}
		path := L.OptString(1, "")
		if err := rt.callHost("tabs.open", func(Host) error { return host.OpenTab(path) }); err != nil {
			L.RaiseError("tabs.open: %v", err)
		}
		return 0
	}
}

// luaTabsClose closes a tab, and the window with its last.
//
// # Parameters
//
//   - index: The 1-based index of the tab to close; the tab shown when omitted.
//   - force: Close it even with unsaved edits, discarding them. Without it, a tab with unsaved edits stays open the first time, with a warning.
//
// # Returns
//
// Nothing. The tab closes after the current Lua dispatch, once the calls
// made before it have taken effect, so index counts the tabs they leave.
//
// # Errors
//
// Raises an error when there is no tab at index.
//
// # Example
//
//	onon.tabs.close(2)
func luaTabsClose(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		host, ok := rt.tabsHost()
		if !ok {
			L.RaiseError("tabs.close: viewer host unavailable")
		}
		index := tabIndexArg(L, 1, "close")
		force := L.OptBool(2, false)
		if err := rt.callHost("tabs.close", func(Host) error {
			i, err := resolveTabIndex(index, host.Tabs())
			if err != nil {
				return err
			}
			host.CloseTab(i, force)
			return nil
		}); err != nil {
			L.RaiseError("tabs.close: %v", err)
		}
		return 0
	}
}
