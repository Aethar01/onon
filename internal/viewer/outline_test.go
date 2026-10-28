package viewer

import (
	"reflect"
	"testing"

	"onon/internal/config"
	"onon/internal/mupdf"

	"golang.org/x/image/font/basicfont"
)

func TestVisibleOutlineIndicesRespectExpandedAncestors(t *testing.T) {
	app := &App{
		documentState: documentState{outline: []mupdf.OutlineItem{
			{Title: "Chapter 1", Page: 0, Parent: -1, HasChildren: true},
			{Title: "Section 1.1", Page: 1, Parent: 0, HasChildren: true},
			{Title: "Topic 1.1.1", Page: 2, Parent: 1},
			{Title: "Chapter 2", Page: 5, Parent: -1},
		}},
		uiState: uiState{outlineMenu: outlineMenuState{expanded: map[int]bool{}}},
	}

	if got, want := app.visibleOutlineIndices(), []int{0, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected collapsed top-level outline %v, got %v", want, got)
	}
	app.outlineMenu.expanded[0] = true
	if got, want := app.visibleOutlineIndices(), []int{0, 1, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected first level expanded outline %v, got %v", want, got)
	}
	app.outlineMenu.expanded[1] = true
	if got, want := app.visibleOutlineIndices(), []int{0, 1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected nested expanded outline %v, got %v", want, got)
	}
}

func TestOutlineIndexForPageUsesNearestPreviousDestination(t *testing.T) {
	app := &App{documentState: documentState{outline: []mupdf.OutlineItem{
		{Title: "Intro", Page: 0},
		{Title: "Chapter", Page: 5},
		{Title: "Appendix", Page: 10},
	}}}

	tests := []struct {
		page int
		want int
	}{
		{page: 0, want: 0},
		{page: 4, want: 0},
		{page: 5, want: 1},
		{page: 9, want: 1},
		{page: 99, want: 2},
	}
	for _, tt := range tests {
		if got := app.outlineIndexForPage(tt.page); got != tt.want {
			t.Fatalf("outlineIndexForPage(%d) = %d, want %d", tt.page, got, tt.want)
		}
	}
}

func TestOutlineSelectionMovementAndExpandCollapse(t *testing.T) {
	app := &App{
		layoutState: layoutState{winW: 800, winH: 600},
		sdlState:    sdlState{fontFace: basicfont.Face7x13},
		config:      config.Default(),
		documentState: documentState{outline: []mupdf.OutlineItem{
			{Title: "Chapter 1", Page: 0, Parent: -1, HasChildren: true},
			{Title: "Section 1.1", Page: 1, Parent: 0},
			{Title: "Chapter 2", Page: 5, Parent: -1},
		}},
		uiState: uiState{outlineMenu: outlineMenuState{expanded: map[int]bool{}}},
	}
	app.outlineMenu.view = app.newOutlineView()
	app.refreshOutlineView()

	app.moveUIViewSelection(app.outlineMenu.view, 1)
	if app.outlineMenu.view.selected != 2 {
		t.Fatalf("expected collapsed outline to move to next visible top-level item, got %d", app.outlineMenu.view.selected)
	}

	app.outlineMenu.view.selected = 0
	app.expandSelectedOutline()
	if !app.outlineMenu.expanded[0] {
		t.Fatal("expected expanding selected parent to reveal children")
	}

	app.moveUIViewSelection(app.outlineMenu.view, 1)
	if app.outlineMenu.view.selected != 1 {
		t.Fatalf("expected expanded outline to move into child item, got %d", app.outlineMenu.view.selected)
	}

	app.collapseSelectedOutline()
	if app.outlineMenu.view.selected != 0 {
		t.Fatalf("expected collapsing child to select its parent, got %d", app.outlineMenu.view.selected)
	}

	app.collapseSelectedOutline()
	if app.outlineMenu.expanded[0] {
		t.Fatal("expected collapsing expanded parent to hide children")
	}
}

func TestOutlineRunsItsOwnActionsThroughTheGenericList(t *testing.T) {
	app := &App{
		layoutState: layoutState{winW: 800, winH: 600},
		sdlState:    sdlState{fontFace: basicfont.Face7x13},
		config:      config.Default(),
		documentState: documentState{outline: []mupdf.OutlineItem{
			{Title: "Chapter 1", Page: 0, Parent: -1, HasChildren: true},
			{Title: "Section 1.1", Page: 1, Parent: 0},
		}},
		uiState: uiState{outlineMenu: outlineMenuState{expanded: map[int]bool{}}},
	}
	view := app.newOutlineView()
	app.outlineMenu.view = view
	app.showUIView(view)
	app.refreshOutlineView()

	app.runUIViewAction(view, "scroll_right")
	if !app.outlineMenu.expanded[0] {
		t.Fatal("scroll_right did not expand the selected entry")
	}
	app.runUIViewAction(view, "scroll_left")
	if app.outlineMenu.expanded[0] {
		t.Fatal("scroll_left did not collapse the selected entry")
	}
	app.runUIViewAction(view, "outline")
	if view.visible {
		t.Fatal("the outline toggle did not close the outline")
	}
}

func TestOutlinePreviewsTheEntrySelected(t *testing.T) {
	app := testLayoutApp(6)
	app.winW, app.winH = 1000, 300
	app.recomputeLayout(app.viewportSize())
	app.outline = []mupdf.OutlineItem{
		{Title: "Chapter 1", Page: 0, Parent: -1},
		{Title: "Chapter 2", Page: 4, Parent: -1},
	}
	open := func() *uiView {
		app.toggleOutlineMenu()
		return app.outlineMenu.view
	}

	// Moving the selection shows the entry, and closing goes back.
	view := open()
	app.moveUIViewSelection(view, 1)
	app.notifySelectionChange()
	if app.page != 4 || len(app.jumpBack) != 0 {
		t.Fatalf("previewing chapter 2: page %d, jumps %v", app.page+1, app.jumpBack)
	}
	app.runUIViewAction(view, "close")
	if app.page != 0 || app.scrollY != 0 {
		t.Fatalf("closing left page %d at %v", app.page+1, app.scrollY)
	}

	// The pointer passing over a row does not move the document.
	view = open()
	view.selected, view.pointerSelected = 1, true
	app.notifySelectionChange()
	if app.page != 0 {
		t.Fatalf("hovering moved the document to page %d", app.page+1)
	}

	// Choosing an entry keeps it, one jump from where the outline opened.
	view.pointerSelected = false
	app.activateUIView(view)
	if app.page != 4 || len(app.jumpBack) != 1 || app.jumpBack[0].page != 0 {
		t.Fatalf("choosing chapter 2: page %d, jumps %v", app.page+1, app.jumpBack)
	}
}

func TestOutlineKeepsItsWidthWhileFilteringAndExpanding(t *testing.T) {
	app := testStyleApp("bar")
	app.winW, app.winH = 1000, 800
	app.outline = []mupdf.OutlineItem{
		{Title: "A", Page: 0, Parent: -1, HasChildren: true},
		{Title: "A long and deeply set section title that is wider than the rest", Page: 1, Parent: 0, Depth: 1},
		{Title: "B", Page: 2, Parent: -1},
	}
	app.toggleOutlineMenu()
	view := app.outlineMenu.view
	width := func() float32 { r, _ := view.frameGeometry(app); return r.W }
	before := width()
	app.collapseSelectedOutline()
	app.setUIViewQuery(view, "B")
	if after := width(); after != before {
		t.Fatalf("the outline went from %v to %v wide", before, after)
	}
}

func TestOutlineGoingUpToAParentPreviewsIt(t *testing.T) {
	app := testLayoutApp(6)
	app.winW, app.winH = 1000, 300
	app.recomputeLayout(app.viewportSize())
	app.outline = []mupdf.OutlineItem{
		{Title: "Chapter 1", Page: 0, Parent: -1},
		{Title: "Chapter 2", Page: 3, Parent: -1, HasChildren: true},
		{Title: "Section 2.1", Page: 5, Parent: 1, Depth: 1},
	}
	app.config.OutlineInitialDepth = 1
	app.toggleOutlineMenu()
	view := app.outlineMenu.view
	view.selected, view.pointerSelected = 2, true // the pointer left it on the section
	app.notifySelectionChange()
	app.collapseSelectedOutline() // h, up to its chapter
	app.notifySelectionChange()
	if view.selected != 1 || app.page != 3 {
		t.Fatalf("going up to chapter 2 selected %d and showed page %d", view.selected, app.page+1)
	}
}
