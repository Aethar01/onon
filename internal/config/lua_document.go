package config

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

func newLuaDocumentTable(L *lua.LState, rt *Runtime) *lua.LTable {
	document := L.NewTable()
	L.SetField(document, "path", lua.LString(rt.docPath))
	L.SetField(document, "name", lua.LString(rt.docName))
	L.SetField(document, "extension", lua.LString(rt.docMeta.ext))
	L.SetField(document, "exists", lua.LBool(rt.docMeta.exists))
	if rt.docMeta.exists {
		L.SetField(document, "size_bytes", lua.LNumber(rt.docMeta.sizeBytes))
	}
	if rt.docMeta.hasPages {
		L.SetField(document, "page_count", lua.LNumber(rt.docMeta.pageCount))
	}
	L.SetField(document, "metadata", L.NewFunction(func(L *lua.LState) int {
		host, ok := rt.host.(DocumentMetadataHost)
		if !ok {
			L.RaiseError("document.metadata: viewer host unavailable")
		}
		value, err := host.Metadata()
		if err != nil {
			L.RaiseError("document.metadata: %v", err)
		}
		L.Push(luaDocumentMetadata(L, value))
		return 1
	}))
	L.SetField(document, "outline", L.NewFunction(func(L *lua.LState) int {
		host, ok := rt.host.(DocumentOutlineHost)
		if !ok {
			L.RaiseError("document.outline: viewer host unavailable")
		}
		value, err := host.Outline()
		if err != nil {
			L.RaiseError("document.outline: %v", err)
		}
		L.Push(luaOutlineItems(L, value))
		return 1
	}))
	L.SetField(document, "selection", L.NewFunction(func(L *lua.LState) int {
		host, ok := rt.host.(DocumentSelectionHost)
		if !ok {
			L.RaiseError("document.selection: viewer host unavailable")
		}
		value, err := host.Selection()
		if err != nil {
			L.RaiseError("document.selection: %v", err)
		}
		L.Push(luaDocumentSelection(L, value))
		return 1
	}))
	L.SetField(document, "page_info", L.NewFunction(func(L *lua.LState) int {
		host, ok := rt.host.(DocumentPageInfoHost)
		if !ok {
			L.RaiseError("document.page_info: viewer host unavailable")
		}
		value, err := host.PageInfo(L.CheckInt(1))
		if err != nil {
			L.RaiseError("document.page_info: %v", err)
		}
		L.Push(luaPageInfo(L, value))
		return 1
	}))
	return document
}

func newLuaPluginDocument(L *lua.LState, instance *pluginInstance) *lua.LTable {
	document := L.NewTable()
	host := func() any { return instance.runtime.documentHost() }
	r, owner := instance.runtime, instance.manifest.ID
	L.SetField(document, "page_text", L.NewFunction(luaPageTextQuery(r, owner, "document.page_text", 2, host)))
	L.SetField(document, "page_links", L.NewFunction(luaPageLinksQuery(r, owner, "document.page_links", 2, host)))
	return document
}

// luaPageTextQuery and luaPageLinksQuery read a page's text and links on
// an operation of owner's, page and callback at arg and arg+1, of the
// document host gives as they are called: plugin.document's, or a
// background document's.
func luaPageTextQuery(r *Runtime, owner, name string, arg int, host func() any) lua.LGFunction {
	return func(L *lua.LState) int {
		page := L.CheckInt(arg)
		callback, ok := L.Get(arg + 1).(*lua.LFunction)
		if !ok {
			L.RaiseError("%s: expected callback", name)
		}
		h, ok := host().(DocumentPageTextHost)
		if !ok {
			L.RaiseError("%s: viewer host unavailable", name)
		}
		id := r.startPluginOperation(owner, name, callback, func(ctx context.Context) map[string]any {
			if err := ctx.Err(); err != nil {
				return operationError(err)
			}
			text, err := h.PageText(page)
			if err != nil {
				return operationError(err)
			}
			return map[string]any{"success": true, "error": "", "page": page, "text": text}
		})
		L.Push(newPluginOperationHandle(L, r, id))
		return 1
	}
}

func luaPageLinksQuery(r *Runtime, owner, name string, arg int, host func() any) lua.LGFunction {
	return func(L *lua.LState) int {
		page := L.CheckInt(arg)
		callback, ok := L.Get(arg + 1).(*lua.LFunction)
		if !ok {
			L.RaiseError("%s: expected callback", name)
		}
		h, ok := host().(DocumentPageLinksHost)
		if !ok {
			L.RaiseError("%s: viewer host unavailable", name)
		}
		id := r.startPluginOperation(owner, name, callback, func(ctx context.Context) map[string]any {
			if err := ctx.Err(); err != nil {
				return operationError(err)
			}
			links, err := h.PageLinks(page)
			if err != nil {
				return operationError(err)
			}
			values := make([]map[string]any, len(links))
			for i, link := range links {
				values[i] = pageLinkMap(link)
			}
			return map[string]any{"success": true, "error": "", "page": page, "links": values}
		})
		L.Push(newPluginOperationHandle(L, r, id))
		return 1
	}
}

func luaDocumentMetadata(L *lua.LState, value DocumentMetadata) *lua.LTable {
	return luaTableFromMap(L, map[string]any{"format": value.Format, "encryption": value.Encryption, "title": value.Title, "author": value.Author, "subject": value.Subject, "keywords": value.Keywords, "creator": value.Creator, "producer": value.Producer, "creation_date": value.CreationDate, "modification_date": value.ModificationDate})
}

func luaOutlineItems(L *lua.LState, items []DocumentOutlineItem) *lua.LTable {
	table := L.NewTable()
	for _, item := range items {
		value := luaTableFromMap(L, map[string]any{"title": item.Title, "uri": item.URI, "external": item.External, "page": item.Page})
		setOptionalNumber(L, value, "x", item.X)
		setOptionalNumber(L, value, "y", item.Y)
		L.SetField(value, "children", luaOutlineItems(L, item.Children))
		table.Append(value)
	}
	return table
}

func luaDocumentSelection(L *lua.LState, value DocumentSelection) *lua.LTable {
	pages := make([]any, len(value.Pages))
	for i, page := range value.Pages {
		quads := make([]any, len(page.Quads))
		for j, quad := range page.Quads {
			quads[j] = rectMap(quad)
		}
		pages[i] = map[string]any{"page": page.Page, "quads": quads}
	}
	return luaTableFromMap(L, map[string]any{"active": value.Active, "text": value.Text, "pages": pages})
}

func luaPageInfo(L *lua.LState, value DocumentPageInfo) *lua.LTable {
	return luaTableFromMap(L, map[string]any{"page": value.Page, "label": value.Label, "width": value.Width, "height": value.Height, "bounds": rectMap(value.Bounds)})
}

func pageLinkMap(value DocumentPageLink) map[string]any {
	result := map[string]any{"bounds": rectMap(value.Bounds), "uri": value.URI, "external": value.External, "page": value.Page}
	if value.X != nil {
		result["x"] = *value.X
	}
	if value.Y != nil {
		result["y"] = *value.Y
	}
	return result
}

func rectMap(value DocumentRect) map[string]any {
	return map[string]any{"x0": value.X0, "y0": value.Y0, "x1": value.X1, "y1": value.Y1}
}

func setOptionalNumber(L *lua.LState, table *lua.LTable, key string, value *float64) {
	if value != nil {
		L.SetField(table, key, lua.LNumber(*value))
	}
}
