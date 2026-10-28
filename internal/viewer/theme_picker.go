package viewer

import "path/filepath"

// showThemePicker lists the themes there are, showing each as it is
// selected: choosing one keeps it, and closing the list otherwise puts
// back the theme before.
func (a *App) showThemePicker() {
	if a.runtime == nil {
		a.message = "no config runtime"
		return
	}
	current := a.runtime.ChosenTheme()
	if current == "" {
		current = a.config.Theme.Base
	}
	before := a.config.Theme
	choices := a.runtime.ThemeChoices()
	rows := make([]uiRow, len(choices))
	selected := 0
	for i, choice := range choices {
		secondary := "built in"
		if choice.Path != "" {
			secondary = filepath.Join("themes", filepath.Base(choice.Path))
		}
		rows[i] = uiRow{index: i, text: choice.Name, value: choice.Name, secondary: secondary}
		if choice.Name == current {
			selected = i
		}
	}
	chosen := false
	view := a.showRowList("themes", "Themes", rows, 50, 60, func(view *uiView) {
		view.selected = selected
		view.onSelect = func(a *App, row uiRow) {
			chosen = true
			a.CloseUI("themes")
			a.chooseTheme(row.value)
		}
		view.onClose = func(a *App) {
			if !chosen {
				a.runtime.RestoreTheme(before)
				a.applyThemePreview()
			}
		}
		view.onSelectionChanged = func(a *App, view *uiView) {
			for _, row := range view.visibleRows() {
				if row.index == view.selected {
					if err := a.runtime.PreviewTheme(row.value); err != nil {
						a.message = err.Error()
					}
					a.applyThemePreview()
					return
				}
			}
		}
	})
	view.notifiedSelected = view.selected
}

// chooseTheme switches to the theme name and keeps it for the next start.
func (a *App) chooseTheme(name string) {
	if a.runtime == nil {
		a.message = "no config runtime"
		return
	}
	if err := a.runtime.ChooseTheme(name); err != nil {
		a.message = err.Error()
		return
	}
	a.applyRuntimeChanges("theme")
	a.message = "theme " + name
	if a.runtime.ConfigSetsTheme() {
		a.message += "; config.lua sets onon.theme, so this lasts until onon restarts"
	}
}

// applyThemePreview shows a theme previewed or put back. Plugins hear of
// the theme chosen, not of each one passed on the way.
func (a *App) applyThemePreview() {
	if dirty, assigned := a.runtime.ConsumeDirty(); dirty {
		a.applyConfig(a.runtime.Config(), assigned)
	}
}

// notifySelectionChange tells the open view and panels their selection
// moved, once whatever moved it, a key, the pointer or the query, is done.
func (a *App) notifySelectionChange() {
	for _, view := range append([]*uiView{a.activeModalUIView()}, a.views.panels...) {
		if view == nil || !view.visible || view.onSelectionChanged == nil || view.notifiedSelected == view.selected {
			continue
		}
		view.notifiedSelected = view.selected
		view.onSelectionChanged(a, view)
	}
}
