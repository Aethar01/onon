package viewer

import (
	"path/filepath"
	"strings"
	"testing"

	"onon/internal/config"
)

func TestJumpsGoBackToTheDocumentOpenedOver(t *testing.T) {
	first, second := testPages(t, 5), testPages(t, 5)
	app := tabsApp(t, first)
	if err := app.GotoPage(3); err != nil {
		t.Fatal(err)
	}
	if err := app.Open(second); err != nil {
		t.Fatal(err)
	}
	if err := app.GotoPage(4); err != nil {
		t.Fatal(err)
	}

	app.jumpBackward() // to the second document's first page
	app.jumpBackward()
	if app.docPath != config.AbsoluteDocumentPath(first) || app.page != 2 {
		t.Fatalf("back went to %s page %d, want the first document at page 3", filepath.Base(app.docPath), app.page+1)
	}
	app.jumpForward()
	if app.docPath != config.AbsoluteDocumentPath(second) || app.page != 0 {
		t.Fatalf("forward went to %s page %d, want the second document at page 1", filepath.Base(app.docPath), app.page+1)
	}
	app.jumpForward()
	if app.page != 3 || len(app.jumpAhead) != 0 {
		t.Fatalf("forward again went to page %d with %d ahead, want page 4 and none", app.page+1, len(app.jumpAhead))
	}
}

func TestJumpBackFromANewTabShowsTheTabItCameFrom(t *testing.T) {
	first := testPages(t, 3)
	app := tabsApp(t, first)
	app.newEmptyTab()
	if !app.tabIsEmpty() {
		t.Fatal("no blank tab")
	}
	app.jumpBackward()
	if app.tabIndex() != 0 || len(app.tabs) != 2 {
		t.Fatalf("back in a blank tab showed tab %d of %s, want the first", app.tabIndex()+1, tabNames(app))
	}
}

func TestBlankWindowJumpsBackToWhereOnonClosed(t *testing.T) {
	first := testPages(t, 5)
	app := tabsApp(t, first)
	if err := app.GotoPage(4); err != nil {
		t.Fatal(err)
	}
	app.rememberJumps()
	app.saveDocumentSession() // as closing does
	t.Cleanup(func() { config.SetJumpList(nil) })

	blank := tabsApp(t, "")
	if !blank.tabIsEmpty() || len(blank.jumpBack) == 0 {
		t.Fatalf("a blank window with %d places to go back to", len(blank.jumpBack))
	}
	blank.jumpBackward()
	if blank.docPath != config.AbsoluteDocumentPath(first) || blank.page != 3 {
		t.Fatalf("back went to %q page %d, want the document closed with at page 4", blank.docPath, blank.page+1)
	}

	// Opened again where it closed, the document is not a jump from itself.
	again := tabsApp(t, first)
	if n := len(again.jumpBack); n != 1 || again.jumpBack[0].page != 0 {
		t.Fatalf("reopened with jumps %v, want only the one to page 1", again.jumpBack)
	}
}

func TestKeybindEditorShowsItsSearch(t *testing.T) {
	app := tabsApp(t, testPages(t, 1))
	app.toggleKeybindMenu()
	view := app.keybindMenu.view
	view.searching, view.query = true, "jump"
	if _, detail := view.headerText(app, 2); !strings.HasPrefix(detail, "/jump") {
		t.Fatalf("header detail %q, want the query", detail)
	}
}

func TestJumpListKeepsToItsLimit(t *testing.T) {
	app := tabsApp(t, testPages(t, 9))
	app.config.JumpListMax = 3
	for page := 2; page <= 8; page++ {
		if err := app.GotoPage(page); err != nil {
			t.Fatal(err)
		}
	}
	if len(app.jumpBack) != 3 || app.jumpBack[0].page != 4 {
		t.Fatalf("jumps %v, want the last three, from page 5", app.jumpBack)
	}
}

func TestQuoteQuoteGoesToThePlaceBeforeTheLastJump(t *testing.T) {
	app := tabsApp(t, testPages(t, 9))
	if err := app.GotoPage(6); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{0, 5, 0} {
		app.handleMarkToken("'")
		app.handleMarkToken("'")
		if app.page != want || len(app.jumpBack) != 1 {
			t.Fatalf("'' went to page %d with %d places back, want page %d and one", app.page+1, len(app.jumpBack), want+1)
		}
	}
}

func TestJumpsMenuGoesStraightToAPlace(t *testing.T) {
	first, second := testPages(t, 9), testPages(t, 9)
	app := tabsApp(t, first)
	if err := app.GotoPage(3); err != nil {
		t.Fatal(err)
	}
	if err := app.Open(second); err != nil {
		t.Fatal(err)
	}
	if err := app.GotoPage(5); err != nil {
		t.Fatal(err)
	}
	app.jumpBackward()

	app.showJumps()
	view := app.activeModalUIView()
	if view == nil || len(view.rows) != 4 || view.selected != 2 || view.rows[2].hint != "here" {
		t.Fatalf("jumps menu %+v, want four places with the third, here, selected", view)
	}
	view.onSelect(app, view.rows[0]) // the first document's first page
	if app.docPath != config.AbsoluteDocumentPath(first) || app.page != 0 || len(app.jumpBack) != 0 || len(app.jumpAhead) != 3 {
		t.Fatalf("went to %s page %d, %d back and %d ahead; want the first document's first page with three ahead", filepath.Base(app.docPath), app.page+1, len(app.jumpBack), len(app.jumpAhead))
	}
	app.jumpForward()
	if app.docPath != config.AbsoluteDocumentPath(first) || app.page != 2 {
		t.Fatalf("forward went to %s page %d, want the first document at page 3", filepath.Base(app.docPath), app.page+1)
	}
}
