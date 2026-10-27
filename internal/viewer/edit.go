package viewer

import (
	"fmt"
	"path/filepath"

	"onon/internal/remote"
)

// Edits such as highlights and form values stay in memory until :write.

// Edits are counted along the undo history: editPos is the number of edits
// applied, editEnd the number redo can reach, and savedPos the count when
// the file was last written, or -1 once that state can no longer be reached
// by undo and redo.

// markEdited records a new edit.
func (a *App) markEdited() {
	if a.editPos < a.savedPos {
		a.savedPos = -1 // the edit discards the redo history holding the saved state
	}
	a.moveEditPos(1)
	a.editEnd = a.editPos
}

func (a *App) moveEditPos(delta int) {
	a.editPos += delta
	a.setUnsaved(a.editPos != a.savedPos)
	a.discardWarned = false
}

// setUnsaved records whether the document has edits not yet written, and
// shows it on the window where the system marks edited documents.
func (a *App) setUnsaved(unsaved bool) {
	a.unsaved = unsaved
	a.showWindowDocument()
}

// undoEdit undoes (or redoes) an edit and re-renders the pages on screen.
func (a *App) undoEdit(redo bool) {
	if a.doc == nil {
		a.message = "no document open"
		return
	}
	undo, delta, done := a.doc.Undo, -1, "undone"
	if redo {
		undo, delta, done = a.doc.Redo, 1, "redone"
	}
	if err := undo(); err != nil {
		a.message = err.Error()
		return
	}
	a.moveEditPos(delta)
	for page := range a.cache.byPage {
		a.bumpPageRevision(page)
	}
	a.pendingRedraw = true
	a.message = done
}

// confirmDiscard reports whether an action that would lose unsaved edits
// may go ahead: at once when nothing is unsaved, otherwise on the second
// attempt, after a warning.
func (a *App) confirmDiscard(action string) bool {
	if !a.unsaved || a.discardWarned {
		return true
	}
	a.discardWarned = true
	a.message = fmt.Sprintf("unsaved changes: :w saves them, %s again discards them", action)
	return false
}

// writeDocument saves the document to path, or over itself when path is
// empty. A remote document is saved back to its host, which force does even
// if the file there changed since it was opened.
func (a *App) writeDocument(path string, force bool) {
	if a.doc == nil {
		a.message = "no document open"
		return
	}
	if !a.doc.IsPDF() {
		a.message = "only PDF documents can be saved"
		return
	}
	target := a.docPath
	if path != "" {
		var err error
		if target, err = a.resolveOpenPath(path); err != nil {
			a.message = err.Error()
			return
		}
	}
	if remote.IsURI(target) {
		a.writeRemoteDocument(target, force)
		return
	}
	if err := a.doc.Save(target, a.docFile); err != nil {
		a.message = err.Error()
		return
	}
	if target == a.docPath {
		a.savedPos = a.editPos
		a.setUnsaved(false)
		a.document.record(a.docPath) // our own write is not an outside change
	}
	a.message = "wrote " + filepath.Base(target)
}
