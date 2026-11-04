package viewer

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/instance"
	"onon/internal/testpdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
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

func TestNewTabOpensAnotherBlankTabEachTime(t *testing.T) {
	app := tabsApp(t, testPages(t, 2))
	for range 3 {
		app.newEmptyTab()
	}
	if len(app.tabList()) != 4 || app.tabIndex() != 3 || !app.tabIsEmpty() {
		t.Fatalf("tabs %q; want the document and three blank tabs, the last shown", tabNames(app))
	}
	second := testPages(t, 2)
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	if len(app.tabList()) != 4 || app.docPath != config.AbsoluteDocumentPath(second) {
		t.Fatalf("tabs %q; want the document in the blank tab shown", tabNames(app))
	}
}

func TestBackgroundTabKeepsItsOwnViewForItsSession(t *testing.T) {
	app := tabsApp(t, testPages(t, 6))
	if err := app.OpenTab(testPages(t, 6)); err != nil {
		t.Fatal(err)
	}
	app.alignPageToAnchor(4)
	second := app.tab
	app.selectTab(0)
	app.alignPageToAnchor(2)
	app.toggleOverview() // the shown tab's, saving page 3 to return to
	var saved viewState
	app.withTab(second, func() { saved = app.captureViewState() })
	if saved.page != 4 {
		t.Fatalf("the background tab would be saved at page %d; want its own, 4", saved.page)
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

// shortRuntimeDir points instance sockets at a directory with a short
// path, as socket paths are limited to about 100 bytes.
func shortRuntimeDir(t *testing.T) {
	t.Helper()
	base := "/tmp" // shorter than macOS's per-user temporary directory
	if runtime.GOOS == "windows" {
		base = "" // the system's, which has no /tmp
	}
	dir, err := os.MkdirTemp(base, "onon")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TMPDIR", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("LOCALAPPDATA", dir) // where sockets go on Windows
}

// sendWhilePolling sends request to address, polling the app's instance
// commands as its event loop would until it is answered.
func sendWhilePolling(t *testing.T, app *App, address string, request instance.Request) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := instance.Send(address, request)
		done <- err
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		app.pollInstanceCommands()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the request was not answered")
		}
	}
}

func TestInstanceRequestShowsItsTab(t *testing.T) {
	shortRuntimeDir(t)
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
	sendWhilePolling(t, app, address, instance.Request{Command: "open", Path: config.AbsoluteDocumentPath(first), Page: 4})
	if app.tabIndex() != 0 || app.page != 3 {
		t.Fatalf("request for the first tab left tabs %s at page %d", tabNames(app), app.page+1)
	}
}

func TestLastWindowOpensDocumentsInNewTabs(t *testing.T) {
	shortRuntimeDir(t)
	first, second := testPages(t, 5), testPages(t, 5)
	app := tabsApp(t, first)
	app.EnableSingleInstance()
	t.Cleanup(app.CloseInstanceServer)
	last, ok := instance.ReadLastWindow()
	if !ok {
		t.Fatal("the window was not recorded as the last")
	}
	sendWhilePolling(t, app, last.Address, instance.Request{Command: "open-tab", Path: config.AbsoluteDocumentPath(second), Page: 2})
	if len(app.tabs) != 2 || app.docPath != config.AbsoluteDocumentPath(second) || app.page != 1 {
		t.Fatalf("open-tab gave tabs %s at page %d, want the second at page 2", tabNames(app), app.page+1)
	}
	sendWhilePolling(t, app, last.Address, instance.Request{Command: "run", Text: "tabfirst"})
	if app.tabIndex() != 0 {
		t.Fatalf("run tabfirst left %s", tabNames(app))
	}
	// --tab without --unique: a new tab even though one shows the document.
	sendWhilePolling(t, app, last.Address, instance.Request{Command: "open-tab", Path: config.AbsoluteDocumentPath(second)})
	if len(app.tabs) != 3 || app.tabIndex() != 1 || app.docPath != config.AbsoluteDocumentPath(second) {
		t.Fatalf("open-tab of an open document gave tabs %s, want a second tab of it", tabNames(app))
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

func TestTabBarShowsWithTwoTabsAndTakesItsHeight(t *testing.T) {
	app := tabsApp(t, testPages(t, 2))
	if app.fontFace == nil {
		t.Skip("no UI font")
	}
	_, alone := app.viewportSize()
	if app.tabBarThick() != 0 {
		t.Fatal("a tab bar with one tab")
	}
	if err := app.OpenTab(testPages(t, 2)); err != nil {
		t.Fatal(err)
	}
	bar := app.tabBarThick()
	if bar <= 0 {
		t.Fatal("no tab bar with two tabs")
	}
	if _, h := app.viewportSize(); h != alone-bar {
		t.Fatalf("viewport %d high with the bar, want %d", h, alone-bar)
	}
	if _, y := app.viewportOrigin(); y != float64(bar) {
		t.Fatalf("document starts at %v, want below the bar at %d", y, bar)
	}
	app.runCommand(":tabclose")
	if _, h := app.viewportSize(); h != alone || app.tabBarThick() != 0 {
		t.Fatalf("viewport %d high after closing back to one tab, want %d", h, alone)
	}
}

func TestClickingATabShowsIt(t *testing.T) {
	first, second := testPages(t, 1), testPages(t, 1)
	app := tabsApp(t, first)
	if app.fontFace == nil {
		t.Skip("no UI font")
	}
	if err := app.OpenTab(second); err != nil {
		t.Fatal(err)
	}
	l := app.tabBarLayout()
	r := l.tabs[0]
	x, y := float64(r.X+r.W/2), float64(r.Y+r.H/2)
	if app.tabAt(x, y) != 0 || app.tabAt(x, float64(app.tabBarThick()+5)) != -1 {
		t.Fatal("tabAt does not find the first tab, or finds one below the bar")
	}
	press := func(button uint8) {
		e := sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: button, X: float32(x), Y: float32(y)}
		if !app.handleTabBarButton(&e) {
			t.Fatal("the tab bar did not take the click")
		}
	}
	press(uint8(sdl.ButtonLeft))
	if app.tabIndex() != 0 {
		t.Fatalf("clicking the first tab left %s", tabNames(app))
	}
	press(uint8(sdl.ButtonMiddle))
	if len(app.tabs) != 1 || app.docPath != config.AbsoluteDocumentPath(second) {
		t.Fatalf("middle-clicking the first tab left %s", tabNames(app))
	}
}

func TestTabBarSqueezesTabsAndKeepsTheShownOneInView(t *testing.T) {
	app := tabsApp(t, testPages(t, 1))
	if app.fontFace == nil {
		t.Skip("no UI font")
	}
	for range 11 {
		if err := app.OpenTab(testPages(t, 1)); err != nil {
			t.Fatal(err)
		}
	}
	app.winW = int(app.px(app.style(config.ElementTab).MinWidth.V) * 4)
	l := app.tabBarLayout()
	shown := l.tabs[app.tabIndex()]
	if shown.X < l.area.X || shown.X+shown.W > l.area.X+l.area.W+0.5 {
		t.Fatalf("shown tab at %v..%v, outside the bar %v..%v", shown.X, shown.X+shown.W, l.area.X, l.area.X+l.area.W)
	}
	tabStyle := app.style(config.ElementTab)
	_, padRight, _, padLeft := app.insets(tabStyle.Padding.V)
	for i, r := range l.tabs {
		natural := float32(measureText(app.fontFace, app.tabLabel(app.tabs[i]))) + padLeft + padRight
		if least := app.px(app.style(config.ElementTab).MinWidth.V); r.W < least-0.5 {
			t.Fatalf("a tab %v wide, under the least %v (%v natural)", r.W, least, natural)
		}
	}
}

func TestRestoredTabsOpenWhenFirstShown(t *testing.T) {
	a, b, c := testPages(t, 2), testPages(t, 2), testPages(t, 2)
	paths := []string{config.AbsoluteDocumentPath(a), config.AbsoluteDocumentPath(b), config.AbsoluteDocumentPath(c)}
	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	app, err := New(paths[1], runtime, 0, nil, NewOptions{RestoreTabs: config.WindowTabs{Paths: paths, Active: 1}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	app.winW, app.winH = 1000, 800
	if err := app.openInitialDocument(); err != nil {
		t.Fatal(err)
	}
	if len(app.tabs) != 3 || app.tabIndex() != 1 || app.docPath != paths[1] {
		t.Fatalf("restored %s, want three tabs with the second shown", tabNames(app))
	}
	if app.tabs[0].doc != nil || app.tabs[2].doc != nil {
		t.Fatal("a tab in the background was opened before it was shown")
	}
	if app.tabShowing(c) != 2 {
		t.Fatal("the third tab does not answer for its document before it opens")
	}
	app.selectTab(0)
	if app.doc == nil || app.docPath != paths[0] {
		t.Fatalf("showing the first tab left %q open", app.docPath)
	}

	app.rememberTabs()
	w, ok := config.LastWindowTabs()
	if !ok || w.Active != 0 || !slices.Equal(w.Paths, paths) {
		t.Fatalf("remembered %+v, want %q with the first shown", w, paths)
	}

	// A blank tab is not kept, and the one before it is shown in its place.
	app.selectTab(2)
	app.newEmptyTab()
	app.rememberTabs()
	if w, _ := config.LastWindowTabs(); w.Active != 2 || !slices.Equal(w.Paths, paths) {
		t.Fatalf("remembered %+v with a blank tab shown last, want the third shown", w)
	}
}

func TestLuaTabsAPI(t *testing.T) {
	first, second := testPages(t, 3), testPages(t, 2)
	app := tabsApp(t, first)
	eval := func(code string) {
		t.Helper()
		if _, err := app.runtime.Eval(code); err != nil {
			t.Fatal(err)
		}
	}
	eval(fmt.Sprintf("onon.tabs.open(%q)", second))
	if len(app.tabs) != 2 || app.tabIndex() != 1 {
		t.Fatalf("onon.tabs.open gave %s", tabNames(app))
	}
	eval(`
local tabs = onon.tabs.list()
assert(#tabs == 2, "two tabs")
assert(tabs[1].index == 1 and tabs[1].page_count == 3 and not tabs[1].active)
assert(tabs[2].active and tabs[2].page == 1 and tabs[2].page_count == 2)
assert(onon.tabs.current() == 2)
assert(not pcall(onon.tabs.select, 0), "there is no tab 0")
onon.tabs.select(1)`)
	if app.tabIndex() != 0 {
		t.Fatalf("onon.tabs.select(1) left %s", tabNames(app))
	}
	if _, err := app.runtime.Eval(`onon.tabs.select(3)`); err == nil {
		t.Fatal("selecting a tab that is not there did not fail")
	}
	// Calls take effect in order, each counting the tabs the last left.
	eval(fmt.Sprintf("onon.tabs.open(%q); onon.tabs.select(1); onon.tabs.select(3)", testPages(t, 2)))
	if len(app.tabs) != 3 || app.tabIndex() != 2 {
		t.Fatalf("selecting the tab just opened left %s", tabNames(app))
	}
	eval(`onon.tabs.close(1); onon.tabs.close(1)`)
	if len(app.tabs) != 1 || app.docPath != app.tabs[0].docPath || app.tabIndex() != 0 {
		t.Fatalf("closing tab 1 twice left %s", tabNames(app))
	}
}

// tabBarThick is how deep the tab bar is from its edge, 0 while hidden.
func (a *App) tabBarThick() int {
	r := a.layoutEdges().tabBar
	if a.tabBarVertical() {
		return int(r.W)
	}
	return int(r.H)
}

// new_tab and close_tab repeat while their keys are held, but a held
// close_tab stops at unsaved edits and at the window's last tab.
func TestHeldTabKeysRepeat(t *testing.T) {
	app := tabsApp(t, testPages(t, 2))
	app.config.KeyBindings = map[string]string{"t": "new_tab", "w": "close_tab"} // kept by each new tab
	app.setKeyBindings(app.config.KeyBindings)
	press := func(key sdl.Keycode, repeats int) {
		app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: key})
		for range repeats {
			app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: key, Repeat: true})
		}
	}

	press(sdl.KeycodeT, 2)
	if len(app.tabList()) != 4 || app.tabIndex() != 3 {
		t.Fatalf("holding new_tab gave %s, want four tabs, the last shown", tabNames(app))
	}

	app.selectTab(1)
	app.unsaved = true
	app.selectTab(3)
	press(sdl.KeycodeW, 5)
	if len(app.tabList()) != 2 || app.tabIndex() != 1 || !app.discardWarned {
		t.Fatalf("holding close_tab gave %s, want it stopped at the unsaved tab with a warning", tabNames(app))
	}

	press(sdl.KeycodeW, 3) // a fresh press discards, the held one stops at the last tab
	if len(app.tabList()) != 1 || app.quit {
		t.Fatalf("held close_tab gave %s, quit %v; want the last tab kept", tabNames(app), app.quit)
	}
	press(sdl.KeycodeW, 0)
	if !app.quit {
		t.Fatal("a fresh close_tab on the last tab did not close the window")
	}
}

func TestHeldNextTabCycles(t *testing.T) {
	app := tabsApp(t, testPages(t, 2))
	for range 2 {
		app.newEmptyTab()
	}
	app.selectTab(0)
	app.config.KeyBindings = map[string]string{"<C-Tab>": "next_tab"}
	app.setKeyBindings(app.config.KeyBindings)
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeTab, Mod: sdl.KeymodLCtrl})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeTab, Mod: sdl.KeymodLCtrl, Repeat: true})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeTab, Mod: sdl.KeymodLCtrl, Repeat: true})
	if app.tabIndex() != 0 {
		t.Fatalf("holding next_tab through three tabs showed tab %d, want back to 0", app.tabIndex())
	}
}

// A new tab keeps the window's UI font rather than loading it again,
// which is slow enough to lag a held new_tab.
func TestNewTabKeepsUIFont(t *testing.T) {
	app := tabsApp(t, testPages(t, 2))
	face := app.fontFace
	if face == nil {
		t.Fatal("no UI font loaded")
	}
	app.newEmptyTab()
	if app.fontFace != face {
		t.Fatal("new_tab loaded the UI font again")
	}
}
