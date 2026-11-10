package config

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// BackgroundDocumentInfo is how a document opened without a tab is: its
// State is opening, open, failed (with Error) or closed.
type BackgroundDocumentInfo struct {
	State     string
	PageCount int
	Error     string
}

// BackgroundDocumentHost is the optional extension to Host for opening
// documents without a tab.
type BackgroundDocumentHost interface {
	// OpenBackgroundDocument starts opening path, returning its id and a
	// channel closed once it opened or failed.
	OpenBackgroundDocument(path string) (int, <-chan struct{}, error)
	// BackgroundDocument says how the document id is, and returns what
	// answers the Document*Host calls about it, from any goroutine: nil
	// once it is closed.
	BackgroundDocument(id int) (BackgroundDocumentInfo, any)
	CloseBackgroundDocument(id int)
}

const luaBackgroundDocumentType = "onon.background_document"

type luaBackgroundDocument struct {
	id    int
	path  string
	owner string // the plugin that loaded it, or "" for config.lua
}

func (r *Runtime) backgroundHost() (BackgroundDocumentHost, bool) {
	host, ok := r.host.(BackgroundDocumentHost)
	return host, ok
}

// closeBackgroundDocuments closes the background documents of a Lua state
// that is going.
func (r *Runtime) closeBackgroundDocuments(open map[int]bool) {
	host, ok := r.backgroundHost()
	for id := range open {
		if ok {
			host.CloseBackgroundDocument(id)
		}
		delete(open, id)
	}
}

// luaLoadDocument loads a local document in the background, without a tab,
// to read it or draw its pages.
//
// # Parameters
//
//   - path: A local document path; ~ is not expanded.
//   - callback: Optional; called with a [Background document result](#background-document-result) once the document opened or failed.
//
// # Returns
//
// A [Background document](#background-document), opening.
//
// # Errors
//
// Raises an error for a remote URI.
//
// # Example
//
//	local doc
//	doc = onon.load_document("paper.pdf", function(result)
//	  if result.success then onon.message(doc:metadata().title) end
//	end)
func luaLoadDocument(r *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		path := AbsoluteDocumentPath(L.CheckString(1))
		callback, _ := L.Get(2).(*lua.LFunction)
		if L.Get(2) != lua.LNil && callback == nil {
			L.ArgError(2, "expected a function")
		}
		host, ok := r.backgroundHost()
		if !ok {
			L.RaiseError("load_document: viewer host unavailable")
		}
		id, opened, err := host.OpenBackgroundDocument(path)
		if err != nil {
			L.RaiseError("load_document: %v", err)
		}
		if r.backgroundDocuments == nil {
			r.backgroundDocuments = map[int]bool{}
		}
		r.backgroundDocuments[id] = true
		// Its callbacks belong to the plugin that loaded it, or config.lua.
		owner := r.owningPluginID()
		if callback != nil {
			r.startPluginOperation(owner, "load_document", callback, func(ctx context.Context) map[string]any {
				select {
				case <-opened:
				case <-ctx.Done():
					return operationError(ctx.Err())
				}
				info, _ := host.BackgroundDocument(id)
				return map[string]any{"success": info.State == "open", "error": info.Error, "page_count": info.PageCount}
			})
		}
		ud := L.NewUserData()
		ud.Value = &luaBackgroundDocument{id: id, path: path, owner: owner}
		L.SetMetatable(ud, luaBackgroundDocumentMetatable(L, r))
		L.Push(ud)
		return 1
	}
}

// luaBackgroundDocumentMetatable gives a background document its fields and
// methods: those of onon.document that read it, metadata, outline and
// page_info, and those of plugin.document, page_text and page_links.
func luaBackgroundDocumentMetatable(L *lua.LState, r *Runtime) *lua.LTable {
	mt := L.NewTypeMetatable(luaBackgroundDocumentType)
	if mt.RawGetString("__index") != lua.LNil {
		return mt
	}
	// lookup is the document's state, and what answers for it while it is
	// this Lua state's to ask about.
	lookup := func(d *luaBackgroundDocument) (BackgroundDocumentInfo, any) {
		host, ok := r.backgroundHost()
		if !ok || !r.backgroundDocuments[d.id] {
			return BackgroundDocumentInfo{State: "closed"}, nil
		}
		return host.BackgroundDocument(d.id)
	}
	answerer := func(L *lua.LState, name string) any {
		d := checkLuaBackgroundDocument(L, 1)
		info, answers := lookup(d)
		if answers == nil || info.State != "open" {
			L.RaiseError("document:%s: document %s", name, info.State)
		}
		return answers
	}
	methods := map[string]lua.LGFunction{
		"state": func(L *lua.LState) int {
			info, _ := lookup(checkLuaBackgroundDocument(L, 1))
			L.Push(lua.LString(info.State))
			return 1
		},
		"page_count": func(L *lua.LState) int {
			info, _ := lookup(checkLuaBackgroundDocument(L, 1))
			L.Push(lua.LNumber(info.PageCount))
			return 1
		},
		"error": func(L *lua.LState) int {
			info, _ := lookup(checkLuaBackgroundDocument(L, 1))
			L.Push(lua.LString(info.Error))
			return 1
		},
		"close": func(L *lua.LState) int {
			d := checkLuaBackgroundDocument(L, 1)
			if r.backgroundDocuments[d.id] {
				delete(r.backgroundDocuments, d.id)
				if host, ok := r.backgroundHost(); ok {
					host.CloseBackgroundDocument(d.id)
				}
			}
			return 0
		},
		"metadata": func(L *lua.LState) int {
			value, err := answerer(L, "metadata").(DocumentMetadataHost).Metadata()
			if err != nil {
				L.RaiseError("document:metadata: %v", err)
			}
			L.Push(luaDocumentMetadata(L, value))
			return 1
		},
		"outline": func(L *lua.LState) int {
			value, err := answerer(L, "outline").(DocumentOutlineHost).Outline()
			if err != nil {
				L.RaiseError("document:outline: %v", err)
			}
			L.Push(luaOutlineItems(L, value))
			return 1
		},
		"page_info": func(L *lua.LState) int {
			value, err := answerer(L, "page_info").(DocumentPageInfoHost).PageInfo(L.CheckInt(2))
			if err != nil {
				L.RaiseError("document:page_info: %v", err)
			}
			L.Push(luaPageInfo(L, value))
			return 1
		},
		"page_text": func(L *lua.LState) int {
			d := checkLuaBackgroundDocument(L, 1)
			return luaPageTextQuery(r, d.owner, "document:page_text", 2, func() any { return answerer(L, "page_text") })(L)
		},
		"page_links": func(L *lua.LState) int {
			d := checkLuaBackgroundDocument(L, 1)
			return luaPageLinksQuery(r, d.owner, "document:page_links", 2, func() any { return answerer(L, "page_links") })(L)
		},
	}
	table := L.NewTable()
	for name, fn := range methods {
		L.SetField(table, name, L.NewFunction(fn))
	}
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		d := checkLuaBackgroundDocument(L, 1)
		switch key := L.CheckString(2); key {
		case "path":
			L.Push(lua.LString(d.path))
		case "id":
			L.Push(lua.LNumber(d.id))
		default:
			L.Push(table.RawGetString(key))
		}
		return 1
	}))
	return mt
}

func checkLuaBackgroundDocument(L *lua.LState, n int) *luaBackgroundDocument {
	if ud, ok := L.Get(n).(*lua.LUserData); ok {
		if d, ok := ud.Value.(*luaBackgroundDocument); ok {
			return d
		}
	}
	L.ArgError(n, "expected a background document, as onon.load_document returns")
	return nil
}
