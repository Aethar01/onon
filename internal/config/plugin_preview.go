package config

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// DocumentPreviewInfo is how a document opened without a tab is: its State
// is opening, open, failed (with Error) or closed.
type DocumentPreviewInfo struct {
	State     string
	PageCount int
	Error     string
}

// DocumentPreviewHost is the optional extension to Host for opening
// documents without a tab, for plugins to draw pages of. Info may be called
// from any goroutine.
type DocumentPreviewHost interface {
	// OpenDocumentPreview starts opening path, returning its id and a
	// channel closed once it opened or failed.
	OpenDocumentPreview(path string) (int, <-chan struct{}, error)
	DocumentPreviewInfo(id int) DocumentPreviewInfo
	CloseDocumentPreview(id int)
}

const luaPreviewType = "onon.document_preview"

type luaPreview struct {
	id   int
	path string
}

func (r *Runtime) previewHost() (DocumentPreviewHost, bool) {
	host, ok := r.host.(DocumentPreviewHost)
	return host, ok
}

// closePreviews closes the previews of a Lua state that is going.
func (r *Runtime) closePreviews(previews map[int]bool) {
	host, ok := r.previewHost()
	for id := range previews {
		if ok {
			host.CloseDocumentPreview(id)
		}
		delete(previews, id)
	}
}

// openDocument is plugin:open_document(path[, callback]): it opens path
// without a tab and returns a [Document preview](#document-preview),
// calling callback with a [Document preview result](#document-preview-result)
// once it opened or failed.
func (instance *pluginInstance) openDocument(L *lua.LState) int {
	r := instance.runtime
	id := instance.manifest.ID
	path := L.CheckString(2)
	callback, _ := L.Get(3).(*lua.LFunction)
	if L.Get(3) != lua.LNil && callback == nil {
		L.ArgError(3, "expected a function")
	}
	host, ok := r.previewHost()
	if !ok {
		L.RaiseError("plugin %s open_document: viewer host unavailable", id)
	}
	path = AbsoluteDocumentPath(path)
	previewID, opened, err := host.OpenDocumentPreview(path)
	if err != nil {
		L.RaiseError("plugin %s open_document: %v", id, err)
	}
	if r.previews == nil {
		r.previews = map[int]bool{}
	}
	r.previews[previewID] = true
	if callback != nil {
		r.startPluginOperation(id, "open_document", callback, func(ctx context.Context) map[string]any {
			select {
			case <-opened:
			case <-ctx.Done():
				return operationError(ctx.Err())
			}
			info := host.DocumentPreviewInfo(previewID)
			return map[string]any{"success": info.State == "open", "error": info.Error, "page_count": info.PageCount}
		})
	}
	L.Push(newLuaPreview(L, r, previewID, path))
	return 1
}

func newLuaPreview(L *lua.LState, r *Runtime, id int, path string) *lua.LUserData {
	ud := L.NewUserData()
	ud.Value = &luaPreview{id: id, path: path}
	mt := L.NewTypeMetatable(luaPreviewType)
	if mt.RawGetString("__index") == lua.LNil {
		methods := L.NewTable()
		info := func(L *lua.LState) DocumentPreviewInfo {
			p := checkLuaPreview(L, 1)
			if host, ok := r.previewHost(); ok && r.previews[p.id] {
				return host.DocumentPreviewInfo(p.id)
			}
			return DocumentPreviewInfo{State: "closed"}
		}
		L.SetField(methods, "state", L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString(info(L).State))
			return 1
		}))
		L.SetField(methods, "page_count", L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LNumber(info(L).PageCount))
			return 1
		}))
		L.SetField(methods, "error", L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString(info(L).Error))
			return 1
		}))
		L.SetField(methods, "close", L.NewFunction(func(L *lua.LState) int {
			p := checkLuaPreview(L, 1)
			if r.previews[p.id] {
				delete(r.previews, p.id)
				if host, ok := r.previewHost(); ok {
					host.CloseDocumentPreview(p.id)
				}
			}
			return 0
		}))
		L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
			p := checkLuaPreview(L, 1)
			switch key := L.CheckString(2); key {
			case "path":
				L.Push(lua.LString(p.path))
			case "id":
				L.Push(lua.LNumber(p.id))
			default:
				L.Push(methods.RawGetString(key))
			}
			return 1
		}))
	}
	L.SetMetatable(ud, mt)
	return ud
}

func checkLuaPreview(L *lua.LState, n int) *luaPreview {
	if ud, ok := L.Get(n).(*lua.LUserData); ok {
		if p, ok := ud.Value.(*luaPreview); ok {
			return p
		}
	}
	L.ArgError(n, "expected a document preview, as plugin:open_document returns")
	return nil
}
