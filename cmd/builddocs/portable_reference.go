package main

import (
	"fmt"
	"strings"

	"onon/internal/config"
)

type apiEntry struct {
	name        string
	result      string
	description string
}

var portableModules = []apiEntry{
	{"onon.platform", "[Platform](#platform)", "Also available from `require(\"onon.platform\")`. The system onon runs on and its directories."},
	{"onon.path", "table", "Also available from `require(\"onon.path\")`. Fields: `separator`, `list_separator`; functions: `join(...)`, `clean(path)`, `basename(path)`, `dirname(path)`, `extension(path)`, `is_absolute(path)`, `absolute(path)`, `relative(base, target)`, and `expand_home(path)`. Operations use native platform path rules; `expand_home` expands only `~`, `~/...`, and `~\\...`."},
	{"onon.json", "table", "Also available from `require(\"onon.json\")`. `encode(value)` returns compact JSON and `decode(text)` returns a Lua value. `null` is the stable module-local JSON null sentinel. Arrays require contiguous positive integer keys; objects require string keys. Cycles, sparse or mixed tables, non-finite numbers, unsupported Lua values, and trailing input are errors. Decoded empty arrays and objects retain their shape when re-encoded."},
}

var documentFunctions = []apiEntry{
	{"onon.document.metadata()", "[Document metadata](#document-metadata)", "Return the document's metadata. Absent fields are empty strings."},
	{"onon.document.outline()", "[Outline entry](#outline-entry)[]", "Return the outline as a tree, each entry holding the entries under it in `children`."},
	{"onon.document.page_info(page)", "[Page info](#page-info)", "Return a 1-based page's label and size. Sizes are unrotated PDF points; an out-of-range page raises an error."},
	{"onon.document.selection()", "[Selection](#selection)", "Return the current selection: its text and where it is on each page. `pages` is empty when nothing is selected."},
	{"plugin.document:page_text(page, callback)", "[Handle](#handle)", "Extract a page's text asynchronously, calling `callback` with a [Page text result](#page-text-result)."},
	{"plugin.document:page_links(page, callback)", "[Handle](#handle)", "Read a page's links asynchronously, calling `callback` with a [Page links result](#page-links-result)."},
}

var pluginFunctions = []apiEntry{
	{"plugin.fs:read_dir(path[, options], callback)", "[Handle](#handle)", "Read one directory asynchronously, calling `callback` with a [Directory result](#directory-result). `options` are [Filesystem options](#filesystem-options). Without following, a symlink supplied as `path` is rejected; entries themselves are reported as symlinks."},
	{"plugin.fs:stat(path[, options], callback)", "[Handle](#handle)", "Stat a path asynchronously, calling `callback` with its [File info](#file-info). `options` are [Filesystem options](#filesystem-options)."},
	{"plugin.timer:after(ms, callback)", "[Handle](#handle)", "Call `callback()` once after `ms` milliseconds. `ms` may be zero."},
	{"plugin.timer:every(ms, callback)", "[Handle](#handle)", "Call `callback()` repeatedly, waiting `ms` milliseconds between dispatches. `ms` must be greater than zero."},
	{"plugin.storage:get(key)", "value or nil", "Synchronously return the value stored for this plugin ID, or nil when absent or when no session database is available."},
	{"plugin.storage:set(key, value)", "none", "Synchronously persist a plugin-scoped value. Supports nil, booleans, finite numbers, strings, contiguous arrays, and string-keyed objects; tables cannot be cyclic or mix array and object keys. Raises an error when no session database is available; the value is not silently discarded."},
	{"plugin.storage:delete(key)", "none", "Synchronously delete a plugin-scoped value."},
	{"plugin.storage:keys()", "string[]", "Synchronously return this plugin's keys in lexical order."},
	{"plugin.http:request(spec, callback)", "[Handle](#handle)", "Make an asynchronous [HTTP request](#http-request), calling `callback` with its [HTTP response](#http-response)."},
	{"plugin:job(spec, callback)", "[Handle](#handle)", "Start a subprocess from a [Job spec](#job-spec) without invoking a shell, calling `callback` with its [Job result](#job-result)."},
}

var apiLimits = []apiEntry{
	{"Storage value", "65,536 bytes", "Limit applies to the encoded JSON value for each key."},
	{"HTTP response body", "8 MiB", "A larger response fails the operation; request bodies have no API-specific size limit."},
	{"HTTP redirects", "10", "Following a redirect after the tenth redirect fails the operation."},
	{"Job stdout", "4 MiB", "Captured independently; additional output is discarded."},
	{"Job stderr", "4 MiB", "Captured independently; additional output is discarded."},
}

func renderPortablePluginReference(b *strings.Builder) {
	b.WriteString("\n### Portable API\n\n")
	b.WriteString("The `onon` tables below are available to configuration and plugin Lua code.\n\n")
	renderAPITable(b, "Module", portableModules)

	b.WriteString("\n#### Document inspection\n\n")
	b.WriteString("These read the open document. The [Document](#document) table's fields are refreshed when the document changes. The functions below raise an error when no document is open. Rectangles are `x0`, `y0`, `x1`, `y1` in unrotated PDF points. [Lua tables](#lua-tables) lists the fields of each table they return.\n\n")
	renderAPITable(b, "Function", documentFunctions)

	b.WriteString("\n### Plugins\n\n")
	b.WriteString("Plugins are discovered without being executed. Enable one explicitly with `local plugin = require(\"plugin-id\")`; only the required plugin and its declared dependencies execute. The entrypoint must call `onon.plugin.register(\"plugin-id\"[, spec])`; `require` returns the registered module.\n\n")
	b.WriteString("A plugin may describe itself in a [Plugin manifest](#plugin-manifest), `onon-plugin.json`, and declares its options in a [Plugin spec](#plugin-spec). The [Plugin module](#plugin-module) `register` returns has the functions below, and those that register actions, commands and event callbacks. Registered actions use `plugin-id.action`; commands use `:plugin-id-command`, where the command name cannot contain `-`; options use `plugin-id.option`.\n\n")
	renderAPITable(b, "Function", pluginFunctions)

	b.WriteString("\n#### Handles and results\n\n")
	b.WriteString("Every `schedule`, filesystem, timer, HTTP, document-extraction, and job call returns a handle with numeric `id`, `cancel()`, and `active()`. `active()` becomes false before a one-shot completion callback runs. Repeating timers remain active between callbacks until cancelled. `cancel()` is idempotent, removes the operation, and does not invoke its callback, so delivered result tables always have `cancelled=false`. Callbacks run serially on the main Lua thread, never on worker goroutines. [Results](#results) lists each result's fields.\n")

	b.WriteString("\n#### Limits\n\n")
	renderAPITable(b, "Resource", apiLimits)

	b.WriteString("\n#### Lifecycle\n\n")
	b.WriteString("Each plugin has an isolated local-module cache and search root. It may require another discovered plugin by ID only when that ID is declared in `dependencies`. Dependencies load first, and events are delivered in activation order, so dependencies receive an event before their dependents. Registration of actions, commands, and event subscriptions is allowed only while the plugin entrypoint is loading; `off(id)`, storage calls, timers, filesystem and HTTP operations, and jobs remain available afterward.\n\n")
	b.WriteString("A successful `:reload-config` replaces the Lua state and cancels all handles and jobs from the old generation without invoking their callbacks. If reload fails, the previous state and its operations remain active. A failed plugin load rolls back that plugin and cancels work it started. Viewer shutdown emits `shutdown` before runtime close; close then cancels remaining operations and jobs. Storage is keyed by plugin ID in the session database and survives reloads and application restarts.\n\n")
	fmt.Fprintf(b, "Supported events are %s. ", codeList(config.PluginEvents))
	b.WriteString("Each callback is given a table, listed in [Events](#events). `page_changed` and `zoom_changed` are emitted once per frame with the settled value, so a continuous gesture reports where it came to rest. Event callbacks run in subscription order within each plugin; returning true marks an event consumed where the host supports consumption but does not stop later callbacks.\n\n")
	b.WriteString("Plugins are found in every one of the platform plugin directories, and rescanned by `:reload-config`: on Linux `/usr/lib/onon/plugins`, `$XDG_DATA_HOME/onon/plugins` (or `~/.local/share/onon/plugins`), `$XDG_CONFIG_HOME/onon/plugins`, `~/.config/onon/plugins` and each `$XDG_CONFIG_DIRS/onon/plugins`; on macOS `~/Library/Application Support/onon/plugins` and `~/.config/onon/plugins`; on Windows `%LOCALAPPDATA%\\onon\\plugins` and `%APPDATA%\\onon\\plugins`. Where two have a plugin with the same id, the first wins. Disable plugin discovery with `--no-plugins`, or start from built-in defaults with `--no-config`, which also skips the generated settings file.\n\n#### Single instance\n\nInstances are per document. Each window listens on a socket named after the document it is showing, and the address follows the document when the window opens another one. `--unique` hands the request to the window already showing that document and exits; with no such window, this process opens it. `--goto PAGE` opens at a page and `--goto PAGE:X:Y` at a point on it, overriding any remembered session position, whether the work is done here or by an existing window. `--command TEXT` runs a viewer command there, as typed after `:`, including any command registered by a plugin. An unrecognised command exits non-zero. `X` and `Y` are points measured from the page's top-left corner, which is also SyncTeX's convention. Sockets live under `$XDG_RUNTIME_DIR/onon` on Linux, the per-user temporary directory on macOS, and `%LOCALAPPDATA%\\onon` on Windows. They are mode 0600 and named by a hash of the document path, which keeps them within the platform socket length limit. A socket left by a crashed process is reclaimed on the next start.\n")
}

func renderAPITable(b *strings.Builder, firstColumn string, entries []apiEntry) {
	fmt.Fprintf(b, "| %s | Returns / fields | Description |\n|---|---|---|\n", firstColumn)
	for _, entry := range entries {
		fmt.Fprintf(b, "| %s | %s | %s |\n", markdownCode(entry.name), entry.result, entry.description)
	}
}

// codeList formats names as code in a sentence's list: `a`, `b`, and `c`.
func codeList(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = "`" + name + "`"
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + ", and " + quoted[len(quoted)-1]
}
