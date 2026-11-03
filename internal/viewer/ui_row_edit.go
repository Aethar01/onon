package viewer

import (
	"onon/internal/config"
	"onon/internal/keys"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

// A row of a list can take text in place: its value, on the right, becomes
// the text typed, ending where the value ended and growing leftwards over
// an accent rule as it lengthens. Enter keeps it and Escape puts the value
// back; the other keys edit it, rather than moving through the list.

// uiRowEdit is the text being typed into a row of a view.
type uiRowEdit struct {
	row         int    // the row's index
	hint        string // what to type, shown muted after the row's text while there is room
	placeholder string // shown while nothing is typed
	// done has the text typed on Enter, and change each edit of it;
	// cancel runs on Escape, the text dropped.
	done, change func(a *App, text string)
	cancel       func(a *App)
}

// rowEditField is the id of the field a row being edited types into, which
// no plugin's field can be.
const rowEditField = "\x00row"

// editRow starts typing into row of view, from initial, all of it selected
// so that typing replaces it. A row already being edited keeps its text.
func (a *App) editRow(view *uiView, row int, initial string, edit uiRowEdit) {
	if view.rowEdit != nil {
		a.finishRowEdit(view, true)
	}
	edit.row = row
	view.rowEdit = &edit
	f := view.field(rowEditField)
	f.input.Set(initial)
	f.input.SelectAll()
	f.scroll = [2]int{}
	view.focus = rowEditField
	a.syncTextInput()
	a.pendingRedraw = true
}

// finishRowEdit stops typing into view's row, keeping the text, with keep,
// or dropping it.
func (a *App) finishRowEdit(view *uiView, keep bool) {
	edit := view.rowEdit
	if edit == nil {
		return
	}
	text := view.field(rowEditField).input.Value
	view.rowEdit = nil
	delete(view.fields, rowEditField)
	view.focus = ""
	a.syncTextInput()
	a.pendingRedraw = true
	switch {
	case keep && edit.done != nil:
		edit.done(a, text)
	case !keep && edit.cancel != nil:
		edit.cancel(a)
	}
}

// rowEditText is the text typed into view's row being edited.
func (view *uiView) rowEditText() (string, bool) {
	if view == nil || view.rowEdit == nil {
		return "", false
	}
	return view.field(rowEditField).input.Value, true
}

// rowEditKey has the keys while a row of view is being edited: Enter keeps
// the text, Escape drops it, and the rest edit it or are let go unused,
// so that none of them moves through the list or closes it.
func (a *App) rowEditKey(view *uiView, key keys.Key, token string) {
	switch token {
	case "<CR>":
		a.finishRowEdit(view, true)
		return
	case "<Esc>":
		a.finishRowEdit(view, false)
		return
	}
	a.handleUIEditKey(view, key, token)
}

// drawRowEdit draws the text being typed into a row, in place of its value:
// right-aligned at end, from no further left than start, over a rule in
// the cursor's colour, with the hint muted at start while the text leaves
// it room.
func (a *App) drawRowEdit(renderer *sdl.Renderer, view *uiView, st *config.Style, face font.Face, start, end, baseline int) {
	edit := view.rowEdit
	f := view.field(rowEditField)
	display := f.display()
	textColor, mutedColor := a.textColor(st, false), a.textColor(st, true)
	gap, cursorRoom := a.ipx(12), a.ipx(3)
	shown := display
	if shown == "" {
		shown = edit.placeholder
	}
	// The rule is at least a few figures wide, so a field holding one
	// still reads as somewhere to type.
	width := min(max(measureText(face, shown), measureText(face, "0000"))+cursorRoom, max(1, end-start))
	x := end - width
	if hintW := measureText(a.fontFace, edit.hint); edit.hint != "" && start+hintW+gap <= x {
		a.drawText(renderer, edit.hint, start, baseline, mutedColor)
	}
	top, lineHeight := baseline-face.Metrics().Ascent.Ceil(), faceLineHeight(face)
	rule := a.style(config.ElementCursor)
	a.drawBox(renderer, &rule, sdl.FRect{X: float32(x), Y: float32(top+lineHeight) + a.px(1), W: float32(width), H: a.lineWidth(1)})
	if display == "" && edit.placeholder != "" {
		a.drawTextWithFace(renderer, edit.placeholder, end-cursorRoom-measureText(face, edit.placeholder), baseline, mutedColor, face, false)
	}
	// The text ends against the rule's right end, the cursor after it.
	textW := min(measureText(face, display)+cursorRoom, width)
	f.rect = sdl.FRect{X: float32(x), Y: float32(top), W: float32(width), H: float32(lineHeight)}
	f.layout = a.drawEditBox(renderer, editBox{
		input: &f.input, display: display, face: face,
		x: end - textW, y: top, width: max(1, textW), lines: 1, scroll: &f.scroll,
		focused: a.keyView() == view, textColor: textColor, phColor: mutedColor,
		compositionColor: st.Text.V,
		cursor:           tweenKey{kind: "cursor", view: view, text: rowEditField + f.input.Value},
	})
}

// editRowText starts typing into a row that says how with its edit: the
// view's onInput hears each edit and its onSubmit the text kept, by the
// row's id, or its value if it has none.
func (a *App) editRowText(view *uiView, row uiRow) {
	id := row.id
	if id == "" {
		id = row.value
	}
	a.editRow(view, row.index, row.edit.Text, uiRowEdit{
		hint: row.edit.Hint, placeholder: row.edit.Placeholder,
		change: func(a *App, text string) {
			if view.onInput != nil {
				view.onInput(a, id, text)
			}
		},
		done: func(a *App, text string) {
			if view.onSubmit != nil {
				view.onSubmit(a, id, text)
			}
		},
	})
}

// EditUIRow starts typing into row index, 1-based, of view id, if the row
// can be typed into.
func (a *App) EditUIRow(id string, index int) {
	view := a.views.views[id]
	if view == nil || !view.visible {
		return
	}
	for _, row := range view.rows {
		if row.index == index-1 && row.edit != nil && !row.disabled {
			view.selected = row.index
			a.ensureUIViewSelectionVisible(view)
			a.editRowText(view, row)
			return
		}
	}
}
