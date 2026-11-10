package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"unicode"

	"onon/internal/config"
)

// loadLuaFunctionDocs reads ordinary Go doc comments from the named Lua
// handlers in internal/config. Handler names are derived from their Lua
// signatures, so the generated reference has one source of truth: the
// documented implementation in Go source, not a second metadata table.
func loadLuaFunctionDocs() (map[string]string, error) {
	files, err := parser.ParseDir(token.NewFileSet(), "internal/config", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse config documentation: %w", err)
	}
	pkg, ok := files["config"]
	if !ok {
		return nil, fmt.Errorf("config package not found while loading Lua documentation")
	}
	docs := map[string]string{}
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil || len(fn.Doc.List) == 0 || !strings.HasPrefix(fn.Name.Name, "lua") {
				continue
			}
			docs[fn.Name.Name] = strings.TrimSpace(fn.Doc.Text())
		}
	}
	return docs, nil
}

func luaDocumentationName(signature string) string {
	name, _, _ := strings.Cut(signature, "(")
	parts := strings.Split(name, ".")
	var b strings.Builder
	b.WriteString("lua")
	for _, part := range parts[1:] {
		for _, word := range strings.FieldsFunc(part, func(r rune) bool { return r == '_' || r == '-' }) {
			if word == "" {
				continue
			}
			runes := []rune(word)
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
			b.WriteString(string(runes))
		}
	}
	return b.String()
}

func validateLuaFunctionDocs(refs []config.LuaReferenceEntry, docs map[string]string) error {
	for _, ref := range refs {
		name := luaDocumentationName(ref.Signature)
		if strings.TrimSpace(docs[name]) == "" {
			return fmt.Errorf("missing Go documentation for Lua API %s (expected %s)", ref.Signature, name)
		}
	}
	return nil
}

func docSummary(signature, doc string) string {
	doc = formatLuaDoc(signature, doc)
	if paragraph, _, ok := strings.Cut(strings.TrimSpace(doc), "\n\n"); ok {
		return strings.ReplaceAll(paragraph, "\n", " ")
	}
	return strings.ReplaceAll(strings.TrimSpace(doc), "\n", " ")
}

func formatLuaDoc(signature, doc string) string {
	doc = strings.TrimSpace(doc)
	handler := luaDocumentationName(signature)
	doc = strings.TrimPrefix(doc, handler+" ")
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		// Sections such as Parameters and Returns are labels, not headings,
		// so the dozens of them stay out of the table of contents.
		if strings.HasPrefix(line, "# ") {
			lines[i] = "**" + strings.TrimPrefix(line, "# ") + "**"
		}
	}
	return strings.Join(lines, "\n")
}

// luaFunctionGroups sorts the Lua functions by what they work on, named
// without their onon. prefix. Every function must be in exactly one.
var luaFunctionGroups = []struct {
	name      string
	functions []string
}{
	{"Opening documents", []string{"open", "open_external", "recent_files", "pick_file", "pick_directory", "register_scheme", "formats.extensions", "formats.supports"}},
	{"Pages and view", []string{"page", "page_count", "goto_document_point", "zoom", "fit_mode", "rotation", "render_mode", "fullscreen"}},
	{"Search", []string{"search", "search_query", "search_match_count", "search_match_index"}},
	{"Tabs", []string{"tabs.list", "tabs.current", "tabs.open", "tabs.select", "tabs.close"}},
	{"Bindings and input", []string{"bind", "unbind", "bind_mouse", "unbind_mouse", "command", "mode", "pending_keys", "current_count", "clear_pending_keys"}},
	{"Messages and interface", []string{"message", "log", "ui.create", "clipboard.get_text", "clipboard.set_text"}},
	{"Scheduling and plugins", []string{"schedule", "plugin.register"}},
	{"Render cache", []string{"cache.limit", "cache.entries", "cache.pending", "cache.clear"}},
	{"Application", []string{"version", "check_updates"}},
}

// luaFunctionName is a signature's name without its arguments, as
// onon.bind for onon.bind(key, action).
func luaFunctionName(signature string) string {
	name, _, _ := strings.Cut(signature, "(")
	return name
}

// headingAnchor is the anchor the site gives a heading: its text lower
// cased, with spaces as hyphens and other punctuation dropped.
func headingAnchor(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// renderLuaAPI writes the index of every Lua function, then each function
// under the group it belongs to, headed by its name alone so the table of
// contents and links stay short.
func renderLuaAPI(b *strings.Builder, refs []config.LuaReferenceEntry, docs map[string]string) {
	bySignature := map[string]config.LuaReferenceEntry{}
	for _, ref := range refs {
		bySignature[strings.TrimPrefix(luaFunctionName(ref.Signature), "onon.")] = ref
	}
	grouped := map[string]bool{}
	for _, group := range luaFunctionGroups {
		for _, name := range group.functions {
			if _, ok := bySignature[name]; !ok {
				fatal(fmt.Errorf("Lua function group %q lists onon.%s, which does not exist", group.name, name))
			}
			if grouped[name] {
				fatal(fmt.Errorf("onon.%s is in more than one Lua function group", name))
			}
			grouped[name] = true
		}
	}
	for _, ref := range refs {
		if name := strings.TrimPrefix(luaFunctionName(ref.Signature), "onon."); !grouped[name] {
			fatal(fmt.Errorf("onon.%s is in no Lua function group; add it to luaFunctionGroups in cmd/builddocs", name))
		}
	}

	b.WriteString("\n## Lua API\n\n")
	// Links made before the section was renamed.
	b.WriteString("<a id=\"lua-functions\"></a>\n\n")
	b.WriteString("Every function is listed below, then described under the group it belongs to. Lua errors are raised as protected-call errors; use `pcall` when a script should handle them without aborting the current dispatch.\n\n")
	b.WriteString("### All functions\n\n| Function | Summary |\n|---|---|\n")
	for _, ref := range refs {
		name := luaFunctionName(ref.Signature)
		fmt.Fprintf(b, "| [%s](#%s) | %s |\n", markdownCode(ref.Signature), headingAnchor(name), docSummary(ref.Signature, docs[luaDocumentationName(ref.Signature)]))
	}
	for _, group := range luaFunctionGroups {
		fmt.Fprintf(b, "\n### %s\n", group.name)
		for _, name := range group.functions {
			ref := bySignature[name]
			doc := formatLuaDoc(ref.Signature, docs[luaDocumentationName(ref.Signature)])
			fmt.Fprintf(b, "\n#### %s\n\n%s %s\n\n", markdownCode(luaFunctionName(ref.Signature)), markdownCode(ref.Signature), doc)
		}
	}
}
