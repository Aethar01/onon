package viewer

import (
	"onon/internal/config"
	"onon/internal/mupdf"
)

// Clicking a form field edits it: text fields open a prompt with the value,
// check boxes and radio buttons toggle, and choice fields open a list.

// clickFormField handles a press at a screen point, reporting whether it
// landed on an editable kind of form field.
func (a *App) clickFormField(sx, sy float64) bool {
	if a.doc == nil {
		return false
	}
	page, point, ok := a.pagePointAtScreen(sx, sy)
	if !ok {
		return false
	}
	w, found, err := a.doc.WidgetAt(page, point)
	if err != nil {
		a.logf("find form field page=%d err=%v", page+1, err)
		return false
	}
	if !found || w.Kind == mupdf.WidgetOther {
		return false
	}
	if w.ReadOnly {
		a.message = "read-only field"
		return true
	}
	switch w.Kind {
	case mupdf.WidgetText:
		a.askPrompt("Field", w.Value, func(value string) {
			a.editPage(page, func() error { return a.doc.SetWidgetValue(page, w.Index, value) })
		})
	case mupdf.WidgetCheckbox, mupdf.WidgetRadio:
		a.editPage(page, func() error { return a.doc.ToggleWidget(page, w.Index) })
	case mupdf.WidgetChoice:
		a.pickFormChoice(page, w)
	}
	return true
}

func (a *App) pickFormChoice(page int, w mupdf.Widget) {
	options, err := a.doc.WidgetOptions(page, w.Index)
	if err != nil || len(options) == 0 {
		a.message = "no choices for this field"
		return
	}
	a.closeAllUI()
	rows := make([]uiRow, len(options))
	selected := 0
	for i, option := range options {
		rows[i] = uiRow{index: i, text: option, value: option}
		if option == w.Value {
			selected = i
		}
	}
	a.showRowList("form-choice", "Choose", rows, 40, 50, func(view *uiView) {
		b := w.Bounds // a dropdown under the field
		view.place = view.place.belowBox(config.UIBox{Page: page + 1, X: float64(b.X0), Y: float64(b.Y0), W: float64(b.X1 - b.X0), H: float64(b.Y1 - b.Y0)})
		view.selected = selected
		view.onSelect = func(a *App, row uiRow) {
			a.closeUIView(view, false)
			a.editPage(page, func() error { return a.doc.SetWidgetValue(page, w.Index, row.value) })
		}
	})
}
