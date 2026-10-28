package viewer

import (
	"fmt"
	"strconv"
	"strings"

	"onon/internal/mupdf"
)

// showSearchMatches lists every match of the current search with the line
// it is on; choosing one makes it the current match.
func (a *App) showSearchMatches() {
	if len(a.search.order) == 0 {
		a.message = a.searchStatusMessage()
		if a.search.query == "" {
			a.message = "no active search"
		}
		return
	}
	rows := make([]uiRow, len(a.search.order))
	for i, ref := range a.search.order {
		rows[i] = uiRow{index: i, text: a.searchMatchContext(ref), secondary: "p. " + a.pageLabel(ref.page), value: strconv.Itoa(i)}
	}
	current := a.search.current
	view := a.showRowList("matches", fmt.Sprintf("Matches for /%s", a.search.query), rows, 80, 40, func(view *uiView) {
		preview := a.previewDocument(view, func(a *App, row uiRow) { a.previewSearchMatch(view, row.index) })
		view.onClose = func(a *App) {
			preview.restore(a)
			a.search.current = current
			a.message = a.searchStatusMessage()
		}
		view.onSelect = func(a *App, row uiRow) {
			preview.restore(a)
			a.closeUIView(view, false)
			a.search.current, _ = strconv.Atoi(row.value)
			a.focusSearchCurrent()
			a.message = a.searchStatusMessage()
		}
		view.selected = max(0, a.search.current)
	})
	view.notifiedSelected = view.selected
}

// previewSearchMatch makes match i the current one and shows it in the
// middle of the larger part of the document view leaves, above it or
// below it.
func (a *App) previewSearchMatch(view *uiView, i int) {
	a.search.current = i
	a.focusSearchCurrent() // which centres it in the viewport
	rect, _ := view.frameGeometry(a)
	_, viewportH := a.viewportSize()
	h := float64(viewportH)
	top := float64(rect.Y - a.documentRect().Y)
	bottom := top + float64(rect.H)
	middle := top / 2
	if h-bottom > top {
		middle = (bottom + h) / 2
	}
	a.scrollBy(0, h/2-middle)
	a.message = a.searchStatusMessage()
}

// searchMatchContext returns the text of the line a match is on.
func (a *App) searchMatchContext(ref searchHitRef) string {
	quads := a.search.matches[ref.page][ref.hit].Quads
	if len(quads) == 0 {
		return ""
	}
	first, last := quads[0], quads[len(quads)-1]
	sel, err := a.doc.ExtractSelection(ref.page, first.UL, last.LR, mupdf.SelectLines)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(sel.Text), " ")
}
