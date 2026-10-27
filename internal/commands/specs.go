package commands

import "strings"

type Spec struct {
	Name           string
	ArgCompletions []string
	Help           string
}

type CommandReferenceEntry struct {
	Command     string
	Description string
}

var specs = []Spec{
	{Name: "back", Help: ":back - Jump back to the previous position"},
	{Name: "colors", ArgCompletions: []string{"alt", "normal"}, Help: ":colors normal|alt - Set color mode"},
	{Name: "config", Help: ":config - Open the config file in a text editor, creating it if there is none"},
	{Name: "copy", Help: ":copy - Copy the selection"},
	{Name: "cover", ArgCompletions: []string{"on", "off"}, Help: ":cover [on|off] - Show the first page alone in dual-page mode"},
	{Name: "dual", ArgCompletions: []string{"on", "off"}, Help: ":dual [on|off] - Toggle or set dual-page mode"},
	{Name: "fit", ArgCompletions: []string{"height", "manual", "page", "width"}, Help: ":fit width|height|page|manual - Set fit mode"},
	{Name: "forward", Help: ":forward - Jump forward again after :back"},
	{Name: "fullscreen", ArgCompletions: []string{"on", "off"}, Help: ":fullscreen [on|off] - Toggle or set fullscreen"},
	{Name: "help", Help: ":help - Show commands, key bindings, search modifiers and options"},
	{Name: "highlight", Help: ":highlight - Highlight the selection in the last colour used"},
	{Name: "hints", Help: ":hints - Label links on screen to follow one by typing its hint"},
	{Name: "keybinds", Help: ":keybinds - Toggle the keybinds menu"},
	{Name: "lua", Help: ":lua <code> - Execute Lua code inline"},
	{Name: "matches", Help: ":matches - List every match of the current search"},
	{Name: "mode", ArgCompletions: []string{"continuous", "single"}, Help: ":mode continuous|single - Set render mode"},
	{Name: "next", Help: ":next - Go to the next search match"},
	{Name: "noh", Help: ":noh - Clear the search highlights"},
	{Name: "open", Help: ":open <filename> - Open another document relative to the current one, or a URL or ssh host:path"},
	{Name: "open_file_picker", Help: ":open_file_picker - Open the PDF file picker"},
	{Name: "outline", Help: ":outline - Open the document outline"},
	{Name: "overview", Help: ":overview - Toggle the page overview grid"},
	{Name: "page", Help: ":page PAGE, :p PAGE, :N - Jump to a page number or label"},
	{Name: "present", Help: ":present - Toggle presentation mode"},
	{Name: "prev", Help: ":prev - Go to the previous search match"},
	{Name: "print", Help: ":print [lp options] - Print the saved document from a dialog, or directly with lp options such as -d office -P 1-3"},
	{Name: "quit", Help: ":quit, :q - Exit; :q! discards unsaved edits"},
	{Name: "recent", Help: ":recent - Open the recent-files menu"},
	{Name: "redo", Help: ":redo - Reapply the last undone edit"},
	{Name: "reload-config", Help: ":reload-config - Reload the config file"},
	{Name: "rotate", ArgCompletions: []string{"cw", "ccw", "0", "90", "180", "270"}, Help: ":rotate [cw|ccw|DEGREES] - Rotate by a quarter turn, or to 0, 90, 180 or 270"},
	{Name: "search", Help: ":search <text> - Search document text"},
	{Name: "set", Help: ":set [option[?]|option!|option=value] - Inspect or change options"},
	{Name: "statusbar", ArgCompletions: []string{"on", "off"}, Help: ":statusbar [on|off] - Show or hide the status bar"},
	{Name: "theme", Help: ":theme [name] - Pick a theme, seeing each as it is selected, or switch to one"},
	{Name: "trim", ArgCompletions: []string{"on", "off"}, Help: ":trim [on|off] - Lay pages out by their content, trimming margins"},
	{Name: "undo", Help: ":undo - Undo the last edit"},
	{Name: "version", Help: ":version - Show the onon version"},
	{Name: "write", Help: ":write [path], :w - Save edits, or a copy to path; :wq saves and exits; :w! saves over a remote file that changed"},
	{Name: "zoom", ArgCompletions: []string{"in", "out", "reset"}, Help: ":zoom in|out|reset|PERCENT - Zoom a step, reset, or set e.g. :zoom 150"},
}

func All() []Spec {
	result := make([]Spec, len(specs))
	copy(result, specs)
	return result
}

// Names lists the built-in commands, which plugins may not reuse.
func Names() []string {
	names := make([]string, len(specs))
	for i, spec := range specs {
		names[i] = spec.Name
	}
	return names
}

func ArgCompletionValues(name string) []string {
	for _, spec := range specs {
		if spec.Name == name {
			return spec.ArgCompletions
		}
	}
	return nil
}

type SearchModifier struct {
	Modifier    string
	Description string
}

// SearchModifiers are the vim-style modifiers a search query may contain, such as \v.
func SearchModifiers() []SearchModifier {
	return []SearchModifier{
		{Modifier: `\v`, Description: "regular expression from here on"},
		{Modifier: `\V`, Description: "literal text from here on (the default)"},
		{Modifier: `\c`, Description: "ignore case (the default)"},
		{Modifier: `\C`, Description: "match case"},
		{Modifier: `\<`, Description: "start of a word"},
		{Modifier: `\>`, Description: "end of a word"},
		{Modifier: `\%.p`, Description: "current page only"},
		{Modifier: `\%3p`, Description: "page 3 only"},
		{Modifier: `\%<3p`, Description: "pages before page 3"},
		{Modifier: `\%>3p`, Description: "pages after page 3"},
	}
}

func CommandReferences() []CommandReferenceEntry {
	refs := make([]CommandReferenceEntry, 0, len(specs))
	for _, spec := range specs {
		command, description, _ := strings.Cut(spec.Help, " - ")
		refs = append(refs, CommandReferenceEntry{Command: command, Description: description})
	}
	return refs
}
