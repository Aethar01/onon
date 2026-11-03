package viewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/instance"
	"onon/internal/testpdf"
)

// testPages writes a PDF of n pages, each page named for its number, and
// the file for how many have been written, so tabs can be told apart.
func testPages(t *testing.T, n int) string {
	t.Helper()
	pages := make([][]string, n)
	for i := range pages {
		pages[i] = []string{fmt.Sprintf("page %d", i+1)}
	}
	written := testpdf.WritePages(t, pages...)
	testPagesWritten++
	path := filepath.Join(filepath.Dir(written), fmt.Sprintf("doc%d.pdf", testPagesWritten))
	if err := os.Rename(written, path); err != nil {
		t.Fatal(err)
	}
	return path
}

var testPagesWritten int

// tabsApp opens first in a viewer with a configuration runtime, as onon
// starts.
func tabsApp(t *testing.T, first string) *App {
	t.Helper()
	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	app, err := New(first, runtime, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	app.winW, app.winH = 1000, 800
	if err := app.openInitialDocument(); err != nil {
		t.Fatal(err)
	}
	return app
}

func tabNames(a *App) string {
	names := make([]string, len(a.tabList()))
	for i, t := range a.tabList() {
		names[i] = t.tabName()
		if t == a.tab {
			names[i] = "[" + names[i] + "]"
		}
	}
	return strings.Join(names, " ")
}

func TestTabsKeepTheirOwnView(t *testing.T) {
	first, second := testPages(t, 6), testPages(t, 4)
	app := tabsApp(t, first)
	app.alignPageToAnchor(2)
	app.SetZoom(2)

	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	if len(app.tabs) != 2 || app.tabIndex() != 1 || app.docPath != config.AbsoluteDocumentPath(second) {
		t.Fatalf("tabs %s, want the second shown after the first", tabNames(app))
	}
	if app.page != 0 || app.pageCount != 4 {
		t.Fatalf("new tab at page %d of %d, want 1 of 4", app.page+1, app.pageCount)
	}
	app.alignPageToAnchor(3)

	app.runAction("next_tab") // wraps around to the first
	if app.docPath != config.AbsoluteDocumentPath(first) || app.page != 2 || app.zoom != 2 {
		t.Fatalf("first tab shows page %d at zoom %v, want page 3 at 2", app.page+1, app.zoom)
	}
	app.runAction("prev_tab")
	if app.page != 3 {
		t.Fatalf("second tab shows page %d, want 4", app.page+1)
	}
}

func TestSwitchingTabsKeepsTheConfigAndMovesOnonDocument(t *testing.T) {
	first, second := testPages(t, 2), testPages(t, 2)
	app := tabsApp(t, first)
	generation := app.runtime.Generation()
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	check := func(path string) {
		t.Helper()
		if app.runtime.Generation() != generation {
			t.Fatal("the configuration was reloaded")
		}
		if _, err := app.runtime.Eval(fmt.Sprintf("assert(onon.document.path == %q, onon.document.path)", config.AbsoluteDocumentPath(path))); err != nil {
			t.Fatal(err)
		}
	}
	check(second)
	app.selectTab(0)
	check(first)
}

func TestOpenTabShowsTheTabAlreadyShowingIt(t *testing.T) {
	first, second := testPages(t, 3), testPages(t, 3)
	app := tabsApp(t, first)
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	if err := app.OpenTab("file:///" + strings.TrimPrefix(filepath.ToSlash(first), "/") + "#page=2"); err != nil {
		t.Fatal(err)
	}
	if len(app.tabs) != 2 || app.tabIndex() != 0 || app.page != 1 {
		t.Fatalf("tabs %s at page %d, want the first tab shown again at page 2", tabNames(app), app.page+1)
	}
	if err := app.Open(second); err != nil {
		t.Fatal(err)
	}
	if len(app.tabs) != 2 || app.tabIndex() != 1 {
		t.Fatalf(":open of a document in another tab gave %s, want that tab shown", tabNames(app))
	}
}

func TestOpenTabOfAMissingFileAddsNoTab(t *testing.T) {
	app := tabsApp(t, testPages(t, 1))
	app.runCommand(":tabnew " + filepath.Join(t.TempDir(), "missing.pdf"))
	if len(app.tabs) != 1 || app.doc == nil {
		t.Fatalf("tabs %s after a failed :tabnew, want the first alone", tabNames(app))
	}
	if app.message == "" {
		t.Fatal("no error shown")
	}
}

func TestClosingATabWithUnsavedEditsWarnsFirst(t *testing.T) {
	first, second := testPages(t, 2), testPages(t, 2)
	app := tabsApp(t, first)
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	app.markEdited()
	app.runCommand(":q")
	if len(app.tabs) != 2 || app.quit {
		t.Fatalf("closed an edited tab without warning: tabs %s quit %t", tabNames(app), app.quit)
	}
	app.runCommand(":q")
	if len(app.tabs) != 1 || app.quit || app.docPath != config.AbsoluteDocumentPath(first) {
		t.Fatalf("second :q gave tabs %s quit %t, want only the first, still open", tabNames(app), app.quit)
	}
	app.runCommand(":q")
	if !app.quit {
		t.Fatal(":q on the last tab did not close the window")
	}
}

func TestQuitWarnsAboutUnsavedEditsInAnyTab(t *testing.T) {
	first, second := testPages(t, 2), testPages(t, 2)
	app := tabsApp(t, first)
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	app.markEdited()
	app.selectTab(0)
	app.runAction("quit")
	if app.quit || !strings.Contains(app.message, "1 tab has unsaved changes") {
		t.Fatalf("quit=%t message %q, want a warning", app.quit, app.message)
	}
	app.runAction("quit")
	if !app.quit {
		t.Fatal("second quit did not quit")
	}
}

func TestTabCommands(t *testing.T) {
	a, b, c := testPages(t, 1), testPages(t, 1), testPages(t, 1)
	app := tabsApp(t, a)
	for _, path := range []string{b, c} {
		if err := app.OpenTab(path); err != nil {
			t.Fatal(err)
		}
	}
	name := func(path string) string { return filepath.Base(path) }
	want := func(command, order string) {
		t.Helper()
		app.runCommand(command)
		if got := tabNames(app); got != order {
			t.Fatalf("%s gave %s, want %s", command, got, order)
		}
	}
	A, B, C := name(a), name(b), name(c)
	if A == B || B == C {
		t.Fatal("test documents share a name")
	}
	want(":tabfirst", "["+A+"] "+B+" "+C)
	want(":tabnext 3", A+" "+B+" ["+C+"]")
	want(":tabp", A+" ["+B+"] "+C)
	want(":tabmove", A+" "+C+" ["+B+"]")
	want(":tabmove 0", "["+B+"] "+A+" "+C)
	want(":tabmove 2", A+" ["+B+"] "+C)
	want(":tabm +1", A+" "+C+" ["+B+"]")
	want(":tabclose 1", C+" ["+B+"]")
	want(":tabn", "["+C+"] "+B)
	want(":tabonly", "["+C+"]")
}

func TestTabDoRunsInEveryTab(t *testing.T) {
	app := tabsApp(t, testPages(t, 5))
	if err := app.OpenTab(testPages(t, 5)); err != nil {
		t.Fatal(err)
	}
	app.runCommand(":tabdo page 3")
	for _, tab := range app.tabs {
		if tab.page != 2 {
			t.Fatalf("%s at page %d, want 3", tab.tabName(), tab.page+1)
		}
	}
}

func TestWriteAllSavesEveryEditedTab(t *testing.T) {
	first, second := testPages(t, 1), testPages(t, 1)
	app := tabsApp(t, first)
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	app.markEdited()
	app.selectTab(0)
	app.markEdited()
	app.runCommand(":wa")
	if app.unsavedTabs() != 0 {
		t.Fatalf("%d tabs still unsaved: %s", app.unsavedTabs(), app.message)
	}
}

func TestInstanceRequestShowsItsTab(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "onon")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMPDIR", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)

	first, second := testPages(t, 5), testPages(t, 5)
	app := tabsApp(t, first)
	app.EnableSingleInstance()
	t.Cleanup(app.CloseInstanceServer)
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	address, err := instance.AddressFor(config.AbsoluteDocumentPath(first))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := instance.Send(address, instance.Request{Command: "open", Path: config.AbsoluteDocumentPath(first), Page: 4})
		done <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		app.pollInstanceCommands()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if app.tabIndex() != 0 || app.page != 3 {
				t.Fatalf("request for the first tab left tabs %s at page %d", tabNames(app), app.page+1)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the request was not answered")
		}
	}
}

func TestDroppedFileOpensInANewTab(t *testing.T) {
	first, second := testPages(t, 1), testPages(t, 1)
	app := tabsApp(t, first)
	app.handleDroppedFile(second, 1)
	if len(app.tabs) != 2 || app.docPath != config.AbsoluteDocumentPath(second) {
		t.Fatalf("dropping a file gave tabs %s, want it in a new tab", tabNames(app))
	}
}
