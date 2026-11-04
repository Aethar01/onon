package actions

import "runtime"

// Platform is an operating system whose users expect keys of their own,
// such as Cmd shortcuts on macOS. Its keys are bound there on top of each
// action's Keys, never in their place, so the keys onon has everywhere
// keep working on every system.
type Platform struct {
	GOOS string              // as runtime.GOOS names it
	Name string              // as the reference names it
	Keys map[string][]string // by action name
}

// platforms lists the systems with keys of their own. Another system's
// keys go in a table like macOSKeys and an entry here.
var platforms = []Platform{
	{GOOS: "darwin", Name: "macOS", Keys: macOSKeys},
	{GOOS: "windows", Name: "Windows", Keys: desktopKeys},
	{GOOS: "linux", Name: "Linux", Keys: desktopKeys},
}

// macOSKeys are the Cmd shortcuts macOS applications share.
var macOSKeys = map[string][]string{
	"open_file_picker":  {"<D-o>"},
	"save":              {"<D-s>"},
	"print":             {"<D-p>"},
	"quit":              {"<D-q>"},
	"new_tab":           {"<D-t>"},
	"close_tab":         {"<D-w>"},
	"next_tab":          {"<D-S-]>"},
	"prev_tab":          {"<D-S-[>"},
	"open_config":       {"<D-,>"},
	"undo":              {"<D-z>"},
	"redo":              {"<D-S-z>"},
	"cut":               {"<D-x>"},
	"copy":              {"<D-c>"},
	"paste":             {"<D-v>"},
	"search_prompt":     {"<D-f>"},
	"search_next":       {"<D-g>"},
	"search_prev":       {"<D-S-g>"},
	"zoom_in":           {"<D-=>", "<D-S-=>"},
	"zoom_out":          {"<D-->"},
	"reset_zoom":        {"<D-0>"},
	"first_page":        {"<D-Up>"},
	"last_page":         {"<D-Down>"},
	"jump_backward":     {"<D-[>"},
	"jump_forward":      {"<D-]>"},
	"toggle_fullscreen": {"<C-D-f>"},
}

// desktopKeys are the Ctrl and function-key shortcuts Windows and Linux
// desktop applications share. Ctrl-O stays Vim's jump back.
var desktopKeys = map[string][]string{
	"quit":              {"<C-q>"},
	"new_tab":           {"<C-t>"},
	"close_tab":         {"<C-w>"},
	"undo":              {"<C-z>"},
	"redo":              {"<C-y>", "<C-S-z>"},
	"search_prompt":     {"<C-f>"},
	"search_next":       {"<F3>"},
	"search_prev":       {"<S-F3>"},
	"zoom_in":           {"<C-=>", "<C-S-=>", "<C-kPlus>"},
	"zoom_out":          {"<C-->", "<C-kMinus>"},
	"reset_zoom":        {"<C-0>"},
	"first_page":        {"<C-Home>"},
	"last_page":         {"<C-End>"},
	"jump_backward":     {"<A-Left>"},
	"jump_forward":      {"<A-Right>"},
	"toggle_fullscreen": {"<F11>"},
}

// Platforms returns the systems with keys of their own.
func Platforms() []Platform {
	return append([]Platform(nil), platforms...)
}

// DefaultBindings returns the default key bindings on this system.
func DefaultBindings() map[string]string {
	return DefaultBindingsFor(runtime.GOOS)
}

// DefaultBindingsFor returns the default key bindings on goos: every
// action's Keys, and the system's own keys when it has some.
func DefaultBindingsFor(goos string) map[string]string {
	bindings := map[string]string{}
	for _, action := range registry {
		for _, key := range action.Keys {
			bindings[key] = action.Name
		}
	}
	for _, platform := range platforms {
		if platform.GOOS != goos {
			continue
		}
		for name, keys := range platform.Keys {
			for _, key := range keys {
				bindings[key] = name
			}
		}
	}
	return bindings
}
