package viewer

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"onon/internal/config"
	"onon/internal/instance"
	"onon/internal/mupdf"
	"onon/internal/remote"
)

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
	// appliedConfig is the configuration last applied to the tab, as of
	// the window's configGeneration configSeen; a tab in the background
	// catches up with changes when it is shown again.
	appliedConfig config.Config
	configSeen    int
	// pendingPath is the document a tab restored from the last session
	// opens when it is first shown; until then it has no document.
	pendingPath string
}

// newTab returns a tab with no document, starting at page.
func newTab(page int) *tab {
	return &tab{
		documentState: documentState{
			page:      page,
			pageLinks: map[int][]mupdf.Link{},
		},
		viewStateFields: viewStateFields{scale: 1},
		renderService:   renderService{minRenderBaseScale: defaultMinRenderBaseScale},
	}
}

// tabName is what the tab bar and the tabs menu call t.
func (t *tab) tabName() string {
	switch {
	case t.docName != "":
		return t.docName
	case t.pendingPath != "":
		return config.DocumentName(t.pendingPath)
	case t.remoteTask != nil:
		return remote.Name(t.remoteTask.uri)
	}
	return "New Tab"
}

// withTab runs fn with t in place of the tab shown, for work on a tab in
// the background. fn must not show another tab.
func (a *App) withTab(t *tab, fn func()) {
	if t == a.tab {
		fn()
		return
	}
	shown, background := a.tab, a.background
	a.tab, a.background = t, t != shown
	defer func() { a.tab, a.background = shown, background }()
	fn()
}

// tabShowing returns the index of the tab showing path, or -1.
func (a *App) tabShowing(path string) int {
	if path == "" {
		return -1
	}
	key := instance.Key(config.AbsoluteDocumentPath(path))
	return slices.IndexFunc(a.tabList(), func(t *tab) bool {
		path := cmp.Or(t.docPath, t.pendingPath)
		return path != "" && instance.Key(path) == key
	})
}

// tabIndex is the index of the tab shown.
func (a *App) tabIndex() int { return slices.Index(a.tabList(), a.tab) }

// emptyTab makes a tab with nothing open, set up as the configuration
// asks, ready for openDocument.
func (a *App) emptyTab() *tab {
	t := newTab(0)
	t.zoom = a.clampZoom(1)
	t.cache.byteLimit = pageCacheByteLimit(a.config)
	t.appliedConfig, t.configSeen = a.config, a.configGeneration
	a.withTab(t, func() { a.applyConfigState(a.config) })
	return t
}

// insertTab adds t after the tab shown and shows it.
func (a *App) insertTab(t *tab) {
	at := a.tabIndex() + 1
	a.tabs = slices.Insert(a.tabList(), at, t)
	a.selectTab(at)
}

// OpenTab opens path in a new tab after the one shown, as the :tabnew
// command and onon.tabs.open do, or shows the tab that already has it open.
func (a *App) OpenTab(path string) error {
	if path == "" {
		a.newEmptyTab()
		return nil
	}
	path, page, err := a.resolveDocumentArg(path)
	if err != nil {
		return err
	}
	if a.showOpenDocument(path, page) {
		return nil
	}
	if a.tabIsEmpty() {
		return a.openInTab(path, page)
	}
	if a.nativeTabsOn() {
		_, err := a.openNativeTab(path, page)
		return err
	}
	previous := a.tab
	t := a.emptyTab()
	a.insertTab(t)
	if err := a.openInTab(path, page); err != nil {
		if t.doc == nil && t.remoteTask == nil {
			a.removeTab(t, previous) // nothing opened: no tab to keep
		}
		return err
	}
	return nil
}

// openInTab opens path in the tab shown, which has no document, at page
// when page is above 0.
func (a *App) openInTab(path string, page int) error {
	opts := openDocumentOptions{}
	if page > 0 {
		opts.startPage, opts.startPageExplicit = page-1, true
	}
	return a.openDocument(path, opts)
}

// tabIsEmpty reports whether the tab shown has no document and is not
// waiting for one.
func (a *App) tabIsEmpty() bool {
	return a.doc == nil && a.docPath == "" && a.pendingPath == "" && a.remoteTask == nil
}

// showTabFor shows the tab that has path open, at page when page is above
// 0, reporting whether there is one.
func (a *App) showTabFor(path string, page int) bool {
	i := a.tabShowing(path)
	if i < 0 {
		return false
	}
	a.selectTab(i)
	if page > 0 {
		if a.pageCount == 0 {
			a.pendingInstanceJump = &instanceJump{page: page}
		} else if err := a.GotoPage(page); err != nil {
			a.message = err.Error()
		}
	}
	return true
}

// showOpenDocument shows the tab of any window of the process that has
// path open, at page when page is above 0, reporting whether there is one.
func (a *App) showOpenDocument(path string, page int) bool {
	if a.showTabFor(path, page) {
		return true
	}
	for _, w := range a.windowGroup().windows {
		if w != a && w.showTabFor(path, page) {
			w.raiseWindow()
			return true
		}
	}
	return false
}

// newEmptyTab opens a tab with no document, and the recent-files menu to
// choose one from, or the file picker when there are no recent files.
func (a *App) newEmptyTab() {
	if !a.tabIsEmpty() {
		if a.nativeTabsOn() {
			w, err := a.openNativeTab("", 0)
			if err != nil {
				a.message = err.Error()
				return
			}
			a = w // to choose a document in
		} else {
			a.insertTab(a.emptyTab())
		}
	}
	if a.config.SessionDatabase && len(config.RecentFiles(1)) > 0 {
		a.showRecentFiles()
		return
	}
	if err := a.openFilePicker(); err != nil {
		a.message = err.Error()
	}
}

// selectTab shows the tab at index i.
func (a *App) selectTab(i int) {
	if i < 0 || i >= len(a.tabList()) || a.tabList()[i] == a.tab {
		return
	}
	previous := a.tabIndex()
	a.leaveTab()
	a.tab = a.tabList()[i]
	a.enterTab(previous)
}

// cycleTab shows the tab delta tabs on from the one shown, wrapping around.
func (a *App) cycleTab(delta int) {
	n := len(a.userTabs())
	if n < 2 {
		return
	}
	a.showTab(((a.userTabIndex()+delta)%n + n) % n)
}

// leaveTab puts away what the window shows of the tab shown before
// another is: menus and prompts, modes and gestures in progress, and the
// tiles that are not on screen.
func (a *App) leaveTab() {
	if a.tab == nil {
		return
	}
	a.closeViewMode()
	a.closeAllUI()
	a.cancelSmoothZoom()
	a.cancelSmoothScroll()
	a.stopPan()
	a.stopAutoscroll()
	a.hints = nil
	a.closeLinkPreview()
	a.links = linkInputState{}
	a.sequence = nil
	a.pendingCount = ""
	a.pendingMark = ""
	a.cache.keepProtected()
}

// enterTab shows the tab that has just become a.tab, previous being the
// index of the one shown before, or -1.
func (a *App) enterTab(previous int) {
	if a.configSeen != a.configGeneration {
		a.applyTabConfig(a.appliedConfig, a.config, nil)
	}
	a.followSystemColors()
	a.canvas.thumbs, a.canvas.tiles, a.canvas.asked = nil, nil, nil
	a.relayoutWithViewportAnchor(nil)
	if a.runtime != nil {
		a.runtime.SetActiveDocument(a.docPath, a.pageCount)
	}
	a.setWindowTitle()
	a.pendingRedraw = true
	a.loadPendingTab()
	if a.runtime != nil {
		payload := a.documentEventPayload()
		payload["index"] = a.tabIndex() + 1
		payload["previous"] = previous + 1
		a.emitPluginEvent("tab_changed", payload)
	}
}

// CloseTab closes the tab at index i, after a warning when it has unsaved
// edits unless force is set, reporting whether it closed. Closing the last
// tab closes the window.
func (a *App) CloseTab(i int, force bool) bool { return a.closeTab(i, force, ":tabclose") }

// closeTab closes the tab at index i as CloseTab does, its warning naming
// action as what to repeat to discard the edits.
func (a *App) closeTab(i int, force bool, action string) bool {
	users := a.userTabs()
	if i < 0 || i >= len(users) {
		return false
	}
	if a.nativeTabsOn() {
		// The tab is a window, which closes with its one tab.
		w := users[i].w
		if w != a {
			a.showTab(i) // to show what would be lost
		}
		if !force && !w.confirmDiscard(action) {
			return false
		}
		w.quit = true
		return true
	}
	t := a.tabList()[i]
	if !force && t.unsaved {
		if t != a.tab {
			a.selectTab(i) // to show what would be lost
		}
		if !a.confirmDiscard(action) {
			return false
		}
	}
	if len(a.tabList()) == 1 {
		a.quit = true // the window closes, and Close closes the tab
		return true
	}
	next := a.tab
	if t == a.tab {
		next = a.tabList()[min(i+1, len(a.tabList())-1)]
		if next == t {
			next = a.tabList()[i-1]
		}
	}
	a.removeTab(t, next)
	return true
}

// removeTab closes t's document and drops it, showing next if t was shown.
func (a *App) removeTab(t, next *tab) {
	shown := t == a.tab
	if shown {
		a.leaveTab()
	}
	a.withTab(t, a.closeTabDocument)
	a.tabs = slices.DeleteFunc(a.tabList(), func(other *tab) bool { return other == t })
	if shown {
		a.tab = next
		a.enterTab(-1)
	} else {
		a.relayoutWithViewportAnchor(nil) // the tab bar may have gone
	}
	a.pendingRedraw = true
}

// closeTabDocument closes the document of the tab shown for good, saving
// its session, as when its tab or the window closes.
func (a *App) closeTabDocument() {
	a.cancelRemoteTask()
	hadDocument := a.doc != nil
	payload := a.documentEventPayload()
	if hadDocument && a.runtime != nil {
		a.emitPluginEvent("document_close_pre", payload)
	}
	a.saveDocumentSession()
	a.document.Close()
	a.closeDocumentResources()
	if a.releaseCopy != nil {
		a.releaseCopy()
		a.releaseCopy = nil
	}
	if a.instanceServer != nil {
		a.instanceServer.Close()
		a.instanceServer = nil
		a.instanceAddress = ""
	}
	if hadDocument && a.runtime != nil {
		a.emitPluginEvent("document_closed", payload)
	}
}

// closeOtherTabs closes every tab but the one shown, keeping those with
// unsaved edits unless force is set.
func (a *App) closeOtherTabs(force bool) {
	kept := 0
	if a.nativeTabsOn() {
		for _, u := range a.userTabs() {
			switch {
			case u.w == a:
			case u.w.unsaved && !force:
				kept++
			default:
				u.w.quit = true
			}
		}
		if kept > 0 {
			a.message = fmt.Sprintf("%s unsaved changes: :tabonly! discards them", countTabs(kept, "has", "have"))
		}
		return
	}
	for _, t := range slices.Clone(a.tabList()) {
		if t == a.tab {
			continue
		}
		if t.unsaved && !force {
			kept++
			continue
		}
		a.removeTab(t, a.tab)
	}
	if kept > 0 {
		a.message = fmt.Sprintf("%s unsaved changes: :tabonly! discards them", countTabs(kept, "has", "have"))
	}
}

// moveTab moves the tab shown to index to, clamped to the tabs.
func (a *App) moveTab(to int) {
	if a.nativeTabsOn() {
		moveNativeTab(a.window, clampInt(to, 0, len(a.userTabs())-1))
		return
	}
	from := a.tabIndex()
	to = clampInt(to, 0, len(a.tabList())-1)
	if from == to {
		return
	}
	t := a.tab
	a.tabs = slices.Delete(a.tabList(), from, from+1)
	a.tabs = slices.Insert(a.tabList(), to, t)
	a.pendingRedraw = true
}

// confirmQuit reports whether closing the window may go ahead: at once
// when no tab has unsaved edits, otherwise on the second attempt, after a
// warning.
func (a *App) confirmQuit(action string) bool {
	return a.confirmTabs(a.tabList(), action)
}

// confirmTabs reports whether closing tabs may go ahead, as confirmQuit
// does for the window's.
func (a *App) confirmTabs(tabs []*tab, action string) bool {
	if len(tabs) == 1 && tabs[0] == a.tab {
		return a.confirmDiscard(action)
	}
	unwarned := 0
	unsaved := 0
	for _, t := range tabs {
		if t.unsaved {
			unsaved++
			if !t.discardWarned {
				unwarned++
			}
		}
	}
	if unwarned == 0 {
		return true
	}
	for _, t := range tabs {
		if t.unsaved {
			t.discardWarned = true
		}
	}
	a.message = fmt.Sprintf("%s unsaved changes: :wa saves them, %s again discards them", countTabs(unsaved, "has", "have"), action)
	return false
}

// countTabs says how many tabs, with the verb that agrees with them.
func countTabs(n int, one, many string) string {
	if n == 1 {
		return "1 tab " + one
	}
	return fmt.Sprintf("%d tabs %s", n, many)
}

// tabDo runs a command in every tab, as :tabdo does, showing each while
// it runs and ending on the last.
func (a *App) tabDo(command string) {
	if strings.TrimSpace(command) == "" {
		a.message = "usage: :tabdo <command>"
		return
	}
	for _, u := range a.userTabs() {
		i := slices.Index(a.userTabs(), u)
		if i < 0 {
			continue // an earlier command closed it
		}
		a.showTab(i)
		u.w.runCommand(command)
		if u.w.quit || a.quit {
			return
		}
	}
}

// tabList returns the window's tabs: tabs, or the tab shown alone for an
// App made without a list, as tests make them.
func (a *App) tabList() []*tab {
	if len(a.tabs) == 0 && a.tab != nil {
		a.tabs = []*tab{a.tab}
	}
	return a.tabs
}

// pollBackgroundSaves finishes saves to remote hosts that tabs in the
// background started, as :wa does, so their edits count as saved. Other
// remote work waits for its tab to be shown, as it changes the window.
func (a *App) pollBackgroundSaves() {
	for _, t := range a.tabList() {
		if t != a.tab && t.remoteTask != nil && t.remoteTask.kind == remoteSave {
			a.withTab(t, a.pollRemoteTask)
		}
	}
}

// loadPendingTab opens the document of a tab restored from the last
// session, now that it is shown.
func (a *App) loadPendingTab() {
	path := a.pendingPath
	if path == "" {
		return
	}
	a.pendingPath = ""
	if err := a.openInTab(path, 0); err != nil {
		a.message = err.Error()
	}
}

// restoreTabs adds the tabs the window had when it last closed, around
// the tab shown, which has the one that was shown then open. Each opens
// its document when first shown.
func (a *App) restoreTabs(w config.WindowTabs) {
	if !a.config.RestoreTabs || len(w.Paths) < 2 || w.Active < 0 || w.Active >= len(w.Paths) {
		return
	}
	if a.nativeTabsOn() {
		a.restoreNativeTabs(w)
		return
	}
	shown := a.tab
	tabs := make([]*tab, 0, len(w.Paths))
	for i, path := range w.Paths {
		if i == w.Active {
			tabs = append(tabs, shown)
			continue
		}
		if a.tabShowing(path) >= 0 || slices.ContainsFunc(tabs, func(t *tab) bool { return t.pendingPath == path }) {
			continue
		}
		t := a.emptyTab()
		t.pendingPath = path
		a.withTab(t, a.rebindInstanceServer) // --unique finds it before it loads
		tabs = append(tabs, t)
	}
	a.tabs = tabs
	a.relayoutWithViewportAnchor(nil) // below the tab bar
}

// restoreNativeTabs restores tabs as restoreTabs does, each a window the
// system gathers into this window's tabs, which opens its document when
// first shown.
func (a *App) restoreNativeTabs(w config.WindowTabs) {
	order := make([]*App, 0, len(w.Paths))
	for i, path := range w.Paths {
		if i == w.Active {
			order = append(order, a)
			continue
		}
		tab, err := a.windowGroup().newWindow(a, "", 0)
		if err != nil {
			a.logf("cannot restore a tab for %q: %v", path, err)
			continue
		}
		tab.pendingPath = path
		tab.withTab(tab.tab, tab.rebindInstanceServer)
		tab.setWindowTitle()
		addNativeTab(a.window, tab.window)
		order = append(order, tab)
	}
	for i, tab := range order {
		moveNativeTab(tab.window, i)
	}
	selectNativeTab(a.window)
	a.windowGroup().focus(a)
}

// rememberTabs records the window's tabs as it closes, for restoreTabs:
// with native tabs, the windows of its tab group.
func (a *App) rememberTabs() {
	if !a.config.SessionDatabase {
		return
	}
	w := config.WindowTabs{}
	for _, u := range a.userTabs() {
		path := cmp.Or(u.t.docPath, u.t.pendingPath)
		if path == "" {
			continue
		}
		if u.w == a && u.t == a.tab {
			w.Active = len(w.Paths)
		}
		w.Paths = append(w.Paths, path)
	}
	if err := config.SetLastWindowTabs(w); err != nil {
		a.logf("cannot record the tabs: %v", err)
	}
}

// Tabs describes the window's tabs, for onon.tabs.
func (a *App) Tabs() []config.TabInfo {
	tabs := a.userTabs()
	infos := make([]config.TabInfo, len(tabs))
	for i, u := range tabs {
		t := u.t
		infos[i] = config.TabInfo{
			Path: cmp.Or(t.docPath, t.pendingPath), Name: t.tabName(), PageCount: t.pageCount,
			Modified: t.unsaved, Active: u.w == a && t == a.tab,
		}
		if t.pageCount > 0 {
			infos[i].Page = t.page + 1
		}
	}
	return infos
}

// SelectTab shows the tab at index, for onon.tabs.select.
func (a *App) SelectTab(index int) error {
	if index < 0 || index >= len(a.userTabs()) {
		return fmt.Errorf("no tab %d", index+1)
	}
	a.showTab(index)
	return nil
}
