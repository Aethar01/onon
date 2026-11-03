package viewer

import "onon/internal/mupdf"

// tab is one document open in a window: the document itself, how it is
// viewed, its edits and its session. The window shows one tab at a time,
// embedded in App, so the viewer's methods act on the tab shown.
type tab struct {
	documentState
	viewStateFields
	layoutState
	documentWorkers
	renderService
	metricsService
	editState
	selection textSelection
	search    searchState
	jumpBack  []jumpPosition
	jumpAhead []jumpPosition
}

// newTab returns a tab with no document, starting at page.
func newTab(page int) *tab {
	return &tab{
		documentState: documentState{
			page:      page,
			pageLinks: map[int][]mupdf.Link{},
		},
		viewStateFields: viewStateFields{scale: 1},
		renderService:   renderService{minRenderBaseScale: 0.25},
	}
}
