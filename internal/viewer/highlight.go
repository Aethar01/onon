package viewer

import (
	"fmt"
	"strconv"

	"onon/internal/config"
	"onon/internal/keys"
)

// Highlighting turns the selection into highlight annotations in a colour
// chosen from annotation_colors.

func (a *App) annotationPalette() [][3]uint8 {
	var palette [][3]uint8
	for _, raw := range a.config.AnnotationColors {
		if c, err := config.ParseColor(raw); err == nil {
			palette = append(palette, c)
		}
	}
	return palette
}

// pickHighlightColor opens the colour picker for highlighting the selection.
func (a *App) pickHighlightColor() {
	if palette, ok := a.highlightReady(); ok {
		a.pickColor("Highlight colour", palette, false, a.highlightSelection)
	}
}

// pickColor offers palette as swatch rows and calls apply with the chosen
// index, over the menu open if over. Digits pick a colour directly; the
// last one used is preselected. Choosing one closes every menu.
func (a *App) pickColor(title string, palette [][3]uint8, over bool, apply func(int)) {
	rows := make([]uiRow, len(palette))
	for i, c := range palette {
		swatch := config.Color{RGB: c, Alpha: 1}
		rows[i] = uiRow{index: i, key: strconv.Itoa(i + 1), text: fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2]), value: strconv.Itoa(i), swatch: &swatch}
	}
	show := a.showRowList
	if over {
		show = a.showRowListOver
	}
	show("color-picker", title, rows, 30, 40, func(view *uiView) {
		view.searchable = false
		view.selected = clampInt(a.highlightColor, 0, len(rows)-1)
		// A colour's digit picks it.
		view.keyHook = func(a *App, key keys.Key, repeat bool) bool {
			n, err := strconv.Atoi(key.String())
			if err != nil || n < 1 || n > len(palette) || repeat {
				return false
			}
			a.closeAllUIWithCallbacks(false)
			apply(n - 1)
			return true
		}
		view.onSelect = func(a *App, row uiRow) {
			a.closeAllUIWithCallbacks(false)
			n, _ := strconv.Atoi(row.value)
			apply(n)
		}
	})
}

// highlightReady reports whether the selection can be highlighted, with the
// palette to pick from, explaining in the status bar when it cannot.
func (a *App) highlightReady() ([][3]uint8, bool) {
	palette := a.annotationPalette()
	switch {
	case a.selection.empty():
		a.message = "select text to highlight"
	case a.doc == nil || !a.doc.IsPDF():
		a.message = "only PDF documents can be annotated"
	case len(palette) == 0:
		a.message = "annotation_colors has no valid colours"
	default:
		return palette, true
	}
	return nil, false
}

// highlightSelection highlights the selection in palette colour index.
func (a *App) highlightSelection(index int) {
	palette, ok := a.highlightReady()
	if !ok {
		return
	}
	index = clampInt(index, 0, len(palette)-1)
	for _, part := range a.selection.parts {
		if err := a.doc.AddHighlight(part.page, part.quads, palette[index]); err != nil {
			a.message = err.Error()
			return
		}
		a.pageEdited(part.page)
	}
	a.highlightColor = index
	a.clearSelection()
	a.message = "highlighted"
}

// pageEdited re-renders an edited page and marks the document unsaved.
func (a *App) pageEdited(page int) {
	a.bumpPageRevision(page)
	a.markEdited()
	a.pendingRedraw = true
}

// bumpPageRevision makes a page's cached tiles stale so it re-renders.
func (a *App) bumpPageRevision(page int) {
	if a.pageRevisions == nil {
		a.pageRevisions = map[int]int{}
	}
	a.pageRevisions[page]++
}
