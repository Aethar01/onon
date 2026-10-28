package viewer

import (
	"strings"

	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Annotations under the pointer can be deleted with x, or deleted and
// recoloured from a right-click menu.

type annotationTarget struct {
	page int
	mupdf.Annotation
}

// annotationAtScreen returns the annotation under a screen point, if any.
func (a *App) annotationAtScreen(sx, sy float64) (annotationTarget, bool) {
	if a.doc == nil {
		return annotationTarget{}, false
	}
	page, point, ok := a.pagePointAtScreen(sx, sy)
	if !ok {
		return annotationTarget{}, false
	}
	annot, found, err := a.doc.AnnotationAt(page, point)
	if err != nil {
		a.logf("find annotation page=%d err=%v", page+1, err)
	}
	return annotationTarget{page: page, Annotation: annot}, found
}

// deleteAnnotationUnderPointer removes the annotation the pointer is over.
func (a *App) deleteAnnotationUnderPointer() {
	target, ok := a.annotationAtScreen(float64(a.pointer.X), float64(a.pointer.Y))
	if !ok {
		a.message = "no annotation under the pointer"
		return
	}
	a.deleteAnnotation(target)
}

func (a *App) deleteAnnotation(target annotationTarget) {
	a.editPage(target.page, func() error { return a.doc.DeleteAnnotation(target.page, target.Index) })
	if a.unsaved {
		a.message = "deleted " + strings.ToLower(target.Type)
	}
}

// editPage applies an edit to page, then re-renders it and marks the
// document unsaved.
func (a *App) editPage(page int, edit func() error) {
	if err := edit(); err != nil {
		a.message = err.Error()
		return
	}
	a.pageEdited(page)
}

// openAnnotationMenu offers actions for the annotation under a right click,
// reporting whether there was one.
func (a *App) openAnnotationMenu(e *sdl.MouseButtonEvent) bool {
	target, ok := a.annotationAtScreen(float64(e.X), float64(e.Y))
	if !ok {
		return false
	}
	rows := []uiRow{{index: 0, text: "Delete", value: "delete"}, {index: 1, text: "Change colour…", value: "colour"}}
	a.showRowList("annotation-menu", target.Type, rows, 25, 20, func(view *uiView) {
		view.searchable = false
		view.onSelect = func(a *App, row uiRow) {
			switch row.value {
			case "delete":
				a.closeUIView(view, false)
				a.deleteAnnotation(target)
			case "colour":
				palette := a.annotationPalette()
				a.pickColor("Annotation colour", palette, true, func(i int) {
					a.editPage(target.page, func() error { return a.doc.RecolorAnnotation(target.page, target.Index, palette[i]) })
					a.highlightColor = i
				})
			}
		}
	})
	return true
}
