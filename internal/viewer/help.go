package viewer

import (
	"math"
	"slices"
	"strings"

	commandmeta "onon/internal/commands"
	"onon/internal/config"
)

// Help rows carry what Enter does in their id, with the payload in value.
const (
	helpRowCommand  = "command"  // open the command prompt with value
	helpRowAction   = "action"   // run action value
	helpRowSearch   = "search"   // open the search prompt with value
	helpRowKeybinds = "keybinds" // open the keybinding editor
)

const helpViewID = "help"

// toggleHelp shows a searchable overview of commands, key bindings, search
// flags and options. Choosing a row starts using it.
func (a *App) toggleHelp() {
	if view := a.views.views[helpViewID]; view != nil && view.visible {
		a.closeUIView(view, false)
		return
	}
	a.showRowList(helpViewID, "Help", a.helpRows(), 70, 80, func(view *uiView) {
		view.onSelect = func(a *App, row uiRow) { a.activateHelpRow(row) }
		view.selected = firstEnabledRow(view.rows)
		a.helpColumns(view)
	})
}

// refreshHelp builds help's rows again while it is open, as under the
// keybind editor, whose bindings it lists.
func (a *App) refreshHelp() {
	view := a.views.views[helpViewID]
	if view == nil || !view.visible {
		return
	}
	a.setUIViewRows(view, a.helpRows())
	a.helpColumns(view)
}

// helpColumns lays help's rows out in two columns, each name and what it
// does, its keys or its value beside it, the first as wide as the widest
// name.
func (a *App) helpColumns(view *uiView) {
	if a.fontFace == nil {
		return
	}
	widest := 0
	for i, row := range view.rows {
		if row.heading {
			continue
		}
		widest = max(widest, measureText(a.fontFace, row.text))
		view.rows[i].cells = []string{row.text, row.secondary}
	}
	view.columns = []uiColumn{
		{width: config.UISize{Value: math.Ceil(float64(widest) / float64(a.px(1)))}},
		{fill: true},
	}
}

func (a *App) activateHelpRow(row uiRow) {
	if row.id == helpRowKeybinds {
		a.openKeybindMenu(true) // over help, which Esc returns to
		return
	}
	a.closeAllUI()
	switch row.id {
	case helpRowCommand:
		a.mode = modeCommand
		a.input.Set(row.value)
	case helpRowSearch:
		a.mode = modeSearch
		a.searchInput = searchModeForward
		a.input.Set(row.value)
	case helpRowAction:
		a.runAction(row.value)
	}
}

func (a *App) helpRows() []uiRow {
	var rows []uiRow
	section := func(title string) {
		rows = append(rows, uiRow{text: title, disabled: true, heading: true})
	}
	add := func(id, text, secondary, value string) {
		rows = append(rows, uiRow{id: id, text: text, secondary: secondary, value: value})
	}

	section("Commands")
	commands := commandmeta.CommandReferences()
	if a.runtime != nil {
		for _, help := range a.runtime.CommandHelpRows() {
			command, description, _ := strings.Cut(help, " - ")
			commands = append(commands, commandmeta.CommandReferenceEntry{Command: command, Description: description})
		}
	}
	for _, c := range commands {
		name, _, _ := strings.Cut(strings.TrimPrefix(c.Command, ":"), " ")
		add(helpRowCommand, c.Command, c.Description, name+" ")
	}

	section("Keys")
	add(helpRowKeybinds, "Edit key bindings…", ":keybinds", "")
	for _, b := range a.bindingsByAction() {
		add(helpRowAction, b.action, strings.Join(b.keys, " "), b.action)
	}

	section("Search modifiers")
	for _, m := range commandmeta.SearchModifiers() {
		add(helpRowSearch, "/"+m.Modifier+"…", m.Description, m.Modifier)
	}

	section("Options")
	names := config.OptionNames()
	if a.runtime != nil {
		names = a.runtime.OptionNames()
	}
	for _, name := range names {
		value := ""
		if a.runtime != nil {
			value, _ = a.runtime.OptionValue(name)
		}
		add(helpRowCommand, name, value, "set "+name+"="+value)
	}

	for i := range rows {
		rows[i].index = i
	}
	return rows
}

type actionBindings struct {
	action string
	keys   []string
}

// bindingsByAction lists each bound action with its keys and mouse
// bindings, sorted by action name.
func (a *App) bindingsByAction() []actionBindings {
	keys := a.actionKeys()
	for button, action := range a.mouseBindings {
		keys[action] = append(keys[action], button)
	}
	result := make([]actionBindings, 0, len(keys))
	for action, bound := range keys {
		slices.Sort(bound)
		result = append(result, actionBindings{action: action, keys: bound})
	}
	slices.SortFunc(result, func(x, y actionBindings) int { return strings.Compare(x.action, y.action) })
	return result
}

func firstEnabledRow(rows []uiRow) int {
	for _, row := range rows {
		if !row.disabled {
			return row.index
		}
	}
	return -1
}
