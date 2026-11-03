package viewer

import (
	"fmt"
	"strconv"
	"strings"
)

// runTabNewCommand opens a file in a new tab, or with none an empty tab
// to choose one in, as :tabnew does.
func (a *App) runTabNewCommand(path string) {
	if err := a.OpenTab(unescapeCommandArg(path)); err != nil {
		a.message = err.Error()
	}
}

// tabNumber reads a tab given as Vim does, counting from 1: N, $ for the
// last, or +N and -N from the tab shown. It returns the tab's index, which
// may be out of range.
func (a *App) tabNumber(arg string) (int, error) {
	arg = strings.TrimSpace(arg)
	if arg == "$" {
		return len(a.tabList()) - 1, nil
	}
	n, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("not a tab: %s", arg)
	}
	if strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-") {
		return a.tabIndex() + n, nil
	}
	return n - 1, nil
}

// runTabGotoCommand runs :tabnext (dir 1) and :tabprevious (dir -1): with
// no argument to the next or previous tab, wrapping around; :tabnext N to
// tab N, :tabnext +N or -N by N tabs, and :tabprevious N back N tabs.
func (a *App) runTabGotoCommand(args string, dir int) {
	args = strings.TrimSpace(args)
	if args == "" {
		a.cycleTab(dir)
		return
	}
	if dir < 0 {
		n, err := strconv.Atoi(args)
		if err != nil || n < 0 {
			a.message = "usage: :tabprevious [N]"
			return
		}
		a.cycleTab(-n)
		return
	}
	if strings.HasPrefix(args, "+") || strings.HasPrefix(args, "-") {
		n, err := strconv.Atoi(args)
		if err != nil {
			a.message = "usage: :tabnext [N|+N|-N]"
			return
		}
		a.cycleTab(n)
		return
	}
	i, err := a.tabNumber(args)
	if err != nil {
		a.message = "usage: :tabnext [N|+N|-N]"
		return
	}
	if i < 0 || i >= len(a.tabList()) {
		a.message = fmt.Sprintf("no tab %s", args)
		return
	}
	a.selectTab(i)
}

// runTabCloseCommand closes the tab shown, or the one given as tabNumber
// reads it, as :tabclose does.
func (a *App) runTabCloseCommand(args string, force bool) {
	action := ":tabclose"
	i := a.tabIndex()
	if args = strings.TrimSpace(args); args != "" {
		var err error
		if i, err = a.tabNumber(args); err != nil || i < 0 || i >= len(a.tabList()) {
			a.message = "no tab " + args
			return
		}
		action += " " + args
	}
	a.closeTab(i, force, action)
}

// runTabMoveCommand moves the tab shown, as :tabmove does: with no
// argument to the end, with N after tab N, 0 being before the first, and
// with +N or -N by N places.
func (a *App) runTabMoveCommand(args string) {
	args = strings.TrimSpace(args)
	from := a.tabIndex()
	switch {
	case args == "" || args == "$":
		a.moveTab(len(a.tabList()) - 1)
	case strings.HasPrefix(args, "+") || strings.HasPrefix(args, "-"):
		n, err := strconv.Atoi(args)
		if err != nil {
			a.message = "usage: :tabmove [N|+N|-N]"
			return
		}
		a.moveTab(from + n)
	default:
		n, err := strconv.Atoi(args)
		if err != nil || n < 0 {
			a.message = "usage: :tabmove [N|+N|-N]"
			return
		}
		// After tab N: taking this tab out first shifts the later ones back.
		if n > from {
			n--
		}
		a.moveTab(n)
	}
}

// writeAll saves every tab of every window that has unsaved edits, as :wa
// does.
func (a *App) writeAll() {
	wrote := 0
	for _, w := range a.windowGroup().windows {
		for _, t := range w.tabList() {
			if !t.unsaved {
				continue
			}
			w.withTab(t, func() { w.writeDocument("", false) })
			wrote++
		}
	}
	switch {
	case wrote == 0:
		a.message = "no unsaved changes"
	case wrote > 1 && a.unsavedTabs() == 0:
		a.message = fmt.Sprintf("wrote %d documents", wrote)
	}
}

// unsavedTabs counts the tabs of every window with unsaved edits.
func (a *App) unsavedTabs() int {
	n := 0
	for _, w := range a.windowGroup().windows {
		for _, t := range w.tabList() {
			if t.unsaved {
				n++
			}
		}
	}
	return n
}

// toggleTabsMenu opens the list of tabs, to choose one to show, or closes
// it.
func (a *App) toggleTabsMenu() {
	if view := a.views.views["tabs"]; view != nil && view.visible {
		a.closeUIView(view, false)
		return
	}
	rows := make([]uiRow, len(a.tabList()))
	for i, t := range a.tabList() {
		name := t.tabName()
		if t.unsaved {
			name = "• " + name
		}
		rows[i] = uiRow{index: i, text: name, key: strconv.Itoa(i + 1)}
		if t.pageCount > 0 {
			rows[i].secondary = fmt.Sprintf("p. %d/%d", t.page+1, t.pageCount)
		}
	}
	current := a.tabIndex()
	a.showRowList("tabs", "Tabs", rows, 60, 60, func(view *uiView) {
		view.selected = current
		view.onSelect = func(a *App, row uiRow) {
			a.CloseUI("tabs")
			a.selectTab(row.index)
		}
	})
}
