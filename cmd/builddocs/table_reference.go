package main

import (
	"fmt"
	"strings"
)

// luaTable is a table onon hands to Lua code or takes from it, documented
// field by field so a script can tell what it holds without reading the
// source.
type luaTable struct {
	name   string // the heading, which other entries link to
	intro  string
	fields []luaField
}

type luaField struct {
	name        string
	kind        string
	description string
}

type luaTableGroup struct {
	name   string
	intro  string
	tables []luaTable
}

var luaTableGroups = []luaTableGroup{
	{
		name:  "Document and platform",
		intro: "`onon.platform`, `onon.document` and its functions, and a plugin's `document`, give these. Coordinates are in unrotated PDF points from the page's top left.",
		tables: []luaTable{
			{"Platform", "`onon.platform`, also `require(\"onon.platform\")`. Directories are absolute when the system gives one, and empty when it does not.", []luaField{
				{"os", "string", "`linux`, `macos` or `windows`."},
				{"arch", "string", "The processor architecture, as Go names it: `amd64`, `arm64` and so on."},
				{"home_dir", "string", "The user's home directory."},
				{"config_dir", "string", "The directory onon reads `config.lua` from."},
				{"data_dir", "string", "onon's data directory."},
				{"cache_dir", "string", "onon's cache directory."},
				{"temp_dir", "string", "The system's temporary directory."},
			}},
			{"Document", "`onon.document`, refreshed when the document changes. Its functions raise an error when no document is open.", []luaField{
				{"path", "string", "The document's path or URL; empty when none is open."},
				{"name", "string", "Its file name."},
				{"extension", "string", "Its extension, lower case, without the dot."},
				{"exists", "boolean", "Whether the file exists."},
				{"size_bytes", "integer", "The file's size; only set when it exists."},
				{"page_count", "integer", "Its number of pages; only set once the document is open."},
				{"metadata()", "function", "Returns its [Document metadata](#document-metadata)."},
				{"outline()", "function", "Returns its outline: a list of [Outline entry](#outline-entry) tables, empty when it has none."},
				{"page_info(page)", "function", "Returns [Page info](#page-info) for a 1-based page; an out-of-range page raises an error."},
				{"selection()", "function", "Returns the current [Selection](#selection)."},
			}},
			{"Rectangle", "An area of a page.", []luaField{
				{"x0", "number", "Left edge."},
				{"y0", "number", "Top edge."},
				{"x1", "number", "Right edge."},
				{"y1", "number", "Bottom edge."},
			}},
			{"Document metadata", "Every field is a string, empty when the document does not give it.", []luaField{
				{"format", "string", "The document's format, as MuPDF names it."},
				{"encryption", "string", "How it is encrypted, as MuPDF describes it."},
				{"title", "string", "The document's title."},
				{"author", "string", "Its author."},
				{"subject", "string", "Its subject."},
				{"keywords", "string", "Its keywords, as one string."},
				{"creator", "string", "The application that made the original."},
				{"producer", "string", "The application that wrote the file."},
				{"creation_date", "string", "When it was made, as the document writes it."},
				{"modification_date", "string", "When it last changed, as the document writes it."},
			}},
			{"Outline entry", "", []luaField{
				{"title", "string", "The entry's text."},
				{"uri", "string", "Its link target, or empty."},
				{"external", "boolean", "Whether `uri` leads out of the document."},
				{"page", "integer", "The 1-based page it goes to, or 0 when it goes to none."},
				{"x", "number or nil", "Where on the page it goes, when it says."},
				{"y", "number or nil", "Where on the page it goes, when it says."},
				{"children", "[Outline entry](#outline-entry)[]", "The entries under it; empty when it has none."},
			}},
			{"Page info", "", []luaField{
				{"page", "integer", "The 1-based page."},
				{"label", "string", "Its page label, such as `iv`, or empty when it has none."},
				{"width", "number", "Its width."},
				{"height", "number", "Its height."},
				{"bounds", "[Rectangle](#rectangle)", "Its box."},
			}},
			{"Selection", "", []luaField{
				{"active", "boolean", "Whether text is selected."},
				{"text", "string", "The selected text."},
				{"pages", "[Selection page](#selection-page)[]", "Each page the selection covers, in page order; empty when nothing is selected."},
			}},
			{"Selection page", "", []luaField{
				{"page", "integer", "The 1-based page."},
				{"quads", "[Rectangle](#rectangle)[]", "The bounding rectangle of each piece of the selection on it."},
			}},
			{"Page link", "Each of a [Page links result](#page-links-result)'s `links`.", []luaField{
				{"bounds", "[Rectangle](#rectangle)", "The link's area on the page."},
				{"uri", "string", "Its target, or empty."},
				{"external", "boolean", "Whether `uri` leads out of the document."},
				{"page", "integer", "The 1-based page it goes to, or 0 when it goes to none."},
				{"x", "number or nil", "Where on the page it goes, when it says."},
				{"y", "number or nil", "Where on the page it goes, when it says."},
			}},
		},
	},
	{
		name:  "Plugin tables",
		intro: "A plugin describes itself with these, and its module has the functions that take the rest.",
		tables: []luaTable{
			{"Plugin manifest", "`onon-plugin.json` in the plugin's directory, which may be left out. Unrecognised fields are ignored.", []luaField{
				{"id", "string", "The plugin's ID, which `require` loads it by; the directory's name unless given."},
				{"version", "string", "Its version, shown as the module's `version`."},
				{"module", "string", "The module to load: `lua/<module>.lua` or `lua/<module>/init.lua`; the ID unless given."},
				{"dependencies", "string[]", "IDs of plugins it may `require`, loaded before it."},
			}},
			{"Plugin spec", "The second argument of `onon.plugin.register(id[, spec])`.", []luaField{
				{"options", "table", "The plugin's options, by name: each an [Option declaration](#option-declaration). They are set as `plugin-id.name`, with `onon.options` or `:set`, and read and assigned on the module as `plugin.name`."},
			}},
			{"Option declaration", "", []luaField{
				{"type", "string", "`boolean`, `integer`, `number`, `string`, `path`, `command` or `list`; assigned values must be of it. Unless given, `boolean`, `number` or `list` for a default of that type, else `string`."},
				{"default", "any", "Its value until set; `false`, `0`, `\"\"` or `{}`, by its type, unless given."},
				{"description", "string", "What it does."},
			}},
			{"Plugin module", "What `onon.plugin.register` and `require(\"plugin-id\")` return. Its functions are methods, called with `:` as `plugin:on(\"page_changed\", callback)`; the `register_*` functions and `on` work only while the plugin is loading. Fields assigned on it are kept as the plugin's own.", []luaField{
				{"id", "string", "The plugin's ID."},
				{"version", "string", "The manifest's version, or empty."},
				{"actions", "table", "The plugin's actions by name, as `register_action` returned them."},
				{"fs", "table", "`read_dir` and `stat`; see [Plugins](#plugins)."},
				{"timer", "table", "`after` and `every`."},
				{"storage", "table", "`get`, `set`, `delete` and `keys`."},
				{"http", "table", "`request`."},
				{"document", "table", "`page_text` and `page_links`."},
				{"register_action(name[, spec], callback)", "function", "Adds the action `plugin-id.name`, run by `callback()`, with an [Action spec](#action-spec). Returns the action, to pass to `onon.bind`."},
				{"register_command(name[, spec], callback)", "function", "Adds the command `:plugin-id-name`; `name` cannot contain `-`. `callback` is given a [Command context](#command-context). Takes a [Command spec](#command-spec), and returns the command's full name."},
				{"on(event, callback)", "function", "Calls `callback(payload)` for each event; see [Events](#events). Returns the subscription's ID, an integer."},
				{"off(id)", "function", "Removes the subscription `on` returned `id` for."},
				{"job(spec, callback)", "function", "Starts a [Job spec](#job-spec)'s process, calling `callback` with its [Job result](#job-result). Returns a [Handle](#handle)."},
			}},
			{"Action spec", "", []luaField{
				{"description", "string", "What it does; its full name unless given."},
				{"countable", "boolean", "Whether a count can be typed before its keys; false unless set."},
			}},
			{"Command spec", "", []luaField{
				{"description", "string", "Shown in the help menu; `:plugin-id-name` unless given."},
				{"arg_completions", "string[]", "Arguments offered as the command is typed."},
			}},
			{"Command context", "What a command's callback is given.", []luaField{
				{"name", "string", "The command's full name, as `plugin-id-name`."},
				{"raw", "string", "Everything typed after the command."},
				{"args", "string[]", "`raw` split at whitespace."},
			}},
			{"Filesystem options", "The optional `options` of `plugin.fs:read_dir` and `plugin.fs:stat`.", []luaField{
				{"follow_symlinks", "boolean", "Follow a symlink given as `path` instead of describing the link itself; false unless set."},
			}},
			{"HTTP request", "The `spec` of `plugin.http:request`.", []luaField{
				{"url", "string", "Required; `http://` or `https://`."},
				{"method", "string", "`GET` unless given."},
				{"headers", "{ [string]: string }", "Request headers by name."},
				{"body", "string", "The request body."},
				{"timeout_ms", "integer", "Milliseconds before the request is given up; no limit unless greater than 0."},
			}},
			{"Job spec", "The `spec` of `plugin:job`. The command runs without a shell.", []luaField{
				{"command", "string", "Required; the program, a path or a name found on `PATH`."},
				{"args", "string[]", "Its arguments."},
				{"cwd", "string", "The directory it runs in; onon's own unless given."},
				{"env", "{ [string]: string }", "Environment variables added to onon's."},
				{"stdin", "string", "Written to its standard input."},
				{"timeout_ms", "integer", "Milliseconds before it is killed; no limit unless greater than 0."},
			}},
		},
	},
	{
		name:  "Results",
		intro: "Callbacks are given these. The results of a plugin's filesystem, HTTP, document and job calls have the [Operation result](#operation-result)'s fields as well as their own; timers and `onon.schedule` call their callbacks with nothing.",
		tables: []luaTable{
			{"Handle", "Returned by `onon.schedule` and every asynchronous plugin call.", []luaField{
				{"id", "integer", "The operation's ID, which its result carries too."},
				{"cancel()", "function", "Stops the operation without calling its callback. Calling it again does nothing."},
				{"active()", "function", "Returns true until the operation completes or is cancelled; a repeating timer stays active until cancelled."},
			}},
			{"Operation result", "", []luaField{
				{"id", "integer", "The [Handle](#handle)'s ID."},
				{"success", "boolean", "Whether the operation did what it was asked."},
				{"error", "string", "Why it failed, or empty."},
				{"timed_out", "boolean", "Whether its `timeout_ms` ran out."},
				{"cancelled", "boolean", "Always false: a cancelled operation calls nothing."},
			}},
			{"Picker result", "What `onon.pick_file` and `onon.pick_directory` give their callbacks.", []luaField{
				{"success", "boolean", "Whether a path was picked."},
				{"path", "string", "The path picked, or empty."},
				{"cancelled", "boolean", "Whether the picker was closed without picking, which is not an error."},
				{"error", "string", "Why the picker failed, or empty."},
			}},
			{"File info", "A `plugin.fs:stat` result, and each of a `read_dir` result's `entries`.", []luaField{
				{"name", "string", "The file's name."},
				{"path", "string", "Its path."},
				{"type", "string", "`file`, `directory`, `symlink` or `other`."},
				{"size_bytes", "integer", "Its size."},
				{"modified_unix", "integer", "When it last changed, in whole seconds since the Unix epoch."},
				{"error", "string", "In `entries`: why the entry could not be read, its `size_bytes` and `modified_unix` then 0; else empty."},
			}},
			{"Directory result", "A `plugin.fs:read_dir` result.", []luaField{
				{"entries", "[File info](#file-info)[]", "The directory's entries."},
			}},
			{"HTTP response", "A `plugin.http:request` result. An HTTP error status is still a response, and `success` is true.", []luaField{
				{"status", "integer", "The HTTP status code, or 0 when no response came."},
				{"headers", "{ [string]: string[] }", "Response headers by name, each a list of its values."},
				{"body", "string", "The response body."},
			}},
			{"Job result", "A `plugin:job` result. `success` needs the process to exit with 0, in time.", []luaField{
				{"code", "integer", "The exit code, or -1 when there is none."},
				{"stdout", "string", "Its standard output."},
				{"stderr", "string", "Its standard error."},
			}},
			{"Page text result", "A `plugin.document:page_text` result.", []luaField{
				{"page", "integer", "The 1-based page."},
				{"text", "string", "Its text."},
			}},
			{"Page links result", "A `plugin.document:page_links` result.", []luaField{
				{"page", "integer", "The 1-based page."},
				{"links", "[Page link](#page-link)[]", "Its links."},
			}},
		},
	},
	{
		name:  "Events",
		intro: "What `plugin:on(event, callback)` gives its callback. `shutdown` gives an empty table.",
		tables: []luaTable{
			{"Document event", "`app_ready`, `document_opened`, `document_reloaded`, `config_reloaded`, `document_close_pre` and `document_closed`.", []luaField{
				{"document", "table", "`path`, `name`, and `page_count`, an integer, of the document concerned; `path` is empty when none is open."},
				{"generation", "integer", "A number that changes whenever a document is opened or reloaded."},
			}},
			{"Opening event", "`document_open_pre`, before a document opens.", []luaField{
				{"document", "table", "The `path` and `name` of the document opening."},
			}},
			{"Mouse button event", "`mouse_button_pre`, before onon handles a press or release, and `mouse_button`, after. A `mouse_button_pre` callback returning true keeps onon from handling it.", []luaField{
				{"phase", "string", "`down` or `up`."},
				{"button", "string", "`left`, `middle`, `right`, `x1` or `x2`."},
				{"x", "number", "Where the pointer is, from the window's left."},
				{"y", "number", "Where the pointer is, from the window's top."},
				{"modifiers", "table", "Booleans `ctrl`, `shift`, `alt` and `gui`, the last Cmd on macOS and the Super or Windows key elsewhere."},
				{"inside_document", "boolean", "Whether the pointer is over a page."},
				{"page", "integer", "Over a page: the 1-based page."},
				{"page_x", "number", "Over a page: where on it, in points from its left."},
				{"page_y", "number", "Over a page: where on it, in points from its top."},
			}},
			{"Selection event", "`selection_changed`.", []luaField{
				{"active", "boolean", "Whether text is selected."},
				{"text", "string", "The selected text."},
				{"pages", "integer[]", "The 1-based pages the selection covers, in order. [`onon.document.selection()`](#selection) gives where on them."},
			}},
			{"Page event", "`page_changed`.", []luaField{
				{"page", "integer", "The 1-based page now current."},
				{"label", "string", "Its page label, or its number when it has none."},
				{"previous_page", "integer", "The 1-based page current before."},
				{"page_count", "integer", "The document's number of pages."},
			}},
			{"Zoom event", "`zoom_changed`.", []luaField{
				{"scale", "number", "The zoom now, 1 being 100%."},
				{"previous_scale", "number", "The zoom before."},
				{"percent", "number", "The zoom now as a percentage."},
			}},
			{"Option event", "`option_changed`, after Lua code changes options.", []luaField{
				{"source", "string", "What ran the code that changed them: the event's name for an event callback, `ui_select`, `ui_click` and so on for a view's callback, or `action`, `command`, `set`, `theme` or `plugin operation`."},
			}},
		},
	},
}

// renderLuaTables writes each table in luaTableGroups as a heading and a
// table of its fields.
func renderLuaTables(b *strings.Builder) {
	for _, group := range luaTableGroups {
		fmt.Fprintf(b, "\n### %s\n\n", group.name)
		if group.intro != "" {
			b.WriteString(group.intro + "\n")
		}
		for _, table := range group.tables {
			fmt.Fprintf(b, "\n#### %s\n\n", table.name)
			if table.intro != "" {
				b.WriteString(table.intro + "\n\n")
			}
			b.WriteString("| Field | Type | Description |\n|---|---|---|\n")
			for _, field := range table.fields {
				fmt.Fprintf(b, "| %s | %s | %s |\n", markdownCode(field.name), field.kind, field.description)
			}
		}
	}
}
