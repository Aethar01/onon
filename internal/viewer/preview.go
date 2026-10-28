package viewer

import "slices"

// A menu that previews moves the document behind it to the place its
// selected row names as the keys or the query move the selection, not as
// the pointer passes over rows on its way to one, and leaves the jump list
// alone while it does. Choosing a row keeps the document there, one jump
// from where it was; closing the menu otherwise puts the document back.
type documentPreview struct {
	from                jumpPosition
	jumpBack, jumpAhead []jumpPosition
	moved               bool
}

// previewDocument makes view preview, show moving the document to what a
// row names. It takes the view's onClose.
func (a *App) previewDocument(view *uiView, show func(a *App, row uiRow)) *documentPreview {
	p := &documentPreview{from: a.currentJumpPosition(), jumpBack: slices.Clone(a.jumpBack), jumpAhead: slices.Clone(a.jumpAhead)}
	view.notifiedSelected = view.selected
	view.onSelectionChanged = func(a *App, view *uiView) {
		if view.pointerSelected {
			return
		}
		for _, row := range view.visibleRows() {
			if row.index == view.selected {
				show(a, row)
				p.moved = true
				a.jumpBack, a.jumpAhead = slices.Clone(p.jumpBack), slices.Clone(p.jumpAhead)
				a.pendingRedraw = true
				return
			}
		}
	}
	view.onClose = func(a *App) { p.restore(a) }
	return p
}

// restore puts the document and the jump list back as they were before
// the preview: on closing, and before keeping a row, so the jump to it is
// made from there.
func (p *documentPreview) restore(a *App) {
	a.jumpBack, a.jumpAhead = slices.Clone(p.jumpBack), slices.Clone(p.jumpAhead)
	if p.moved {
		a.restoreJump(p.from)
		p.moved = false
	}
}
