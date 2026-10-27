package viewer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"
)

type openDocumentOptions struct {
	startPage         int
	startPageExplicit bool
	reloadConfig      bool
	preserveView      *viewState
	// remote is the fetched copy of a remote document; until it is set,
	// opening one fetches it first.
	remote *remote.Info
}

func (a *App) Open(path string) error {
	if path == "" {
		return fmt.Errorf("open: empty path")
	}
	// A scheme from Lua first, as it may name a URL with a #page=N.
	path, _, err := a.runtime.ResolveScheme(path)
	if err != nil {
		return err
	}
	path, page := remote.PageFragment(path)
	if path, err = a.resolveOpenPath(path); err != nil {
		return err
	}
	if !a.confirmDiscard("opening it") {
		return nil
	}
	a.logf("open requested path=%q", remote.Display(path))
	if a.runtime == nil {
		a.pendingOpen = path
		a.quit = true
		return nil
	}
	state := a.captureViewState().atDocumentStart()
	opts := openDocumentOptions{reloadConfig: true, preserveView: &state}
	if page > 0 {
		opts.startPage, opts.startPageExplicit = page-1, true // #page=N
	}
	return a.openDocument(path, opts)
}

func (a *App) resolveOpenPath(path string) (string, error) {
	// A file of the name beside a local document wins over host:path, as
	// one in the working directory does in FromArg.
	if a.docPath != "" && !remote.IsURI(a.docPath) && !filepath.IsAbs(path) {
		beside := filepath.Join(filepath.Dir(a.docPath), path)
		if _, err := os.Lstat(beside); err == nil {
			return config.AbsoluteDocumentPath(beside), nil
		}
	}
	if uri, ok, err := remote.FromArg(path); ok {
		return uri, err
	}
	path = expandHomePath(path)
	if !filepath.IsAbs(path) && remote.IsURI(a.docPath) {
		// Beside a remote document is on its host.
		return remote.Resolve(a.docPath, filepath.ToSlash(path))
	}
	if !filepath.IsAbs(path) {
		if a.docPath != "" {
			dir := filepath.Dir(a.docPath)
			path = filepath.Join(dir, path)
		}
	}
	return config.AbsoluteDocumentPath(path), nil
}

// initMetricLoader loads metrics for pages in the background, with content
// boxes while trimming margins.
func (a *App) initMetricLoader(pages []int) {
	a.logf("start metric loader pages=%d trim=%t", len(pages), a.trimMargins)
	l := &metricLoader{
		workerLifecycle: newWorkerLifecycle(a.wakeLoop),
		updates:         make(chan pageMetricUpdate, 128),
	}
	a.metricLoader = l
	go l.run(a.doc, pages, a.trimMargins)
}

func (a *App) startPendingMetricLoader() {
	if a.metricLoader != nil || !a.pendingLoad || a.pendingPages <= 1 {
		return
	}
	pages := a.pendingPages
	start := a.pendingStart
	a.pendingLoad = false
	a.pendingPages = 0
	a.pendingStart = 0
	a.initMetricLoader(metricPageOrder(pages, clampInt(start, 0, pages-1)))
}

func (a *App) pollMetricUpdates() {
	if a.metricLoader == nil {
		return
	}
	changed := false
	anchor := a.captureViewportAnchor()
	for {
		select {
		case update, ok := <-a.metricLoader.updates:
			if !ok {
				return
			}
			if update.err != nil {
				a.message = update.err.Error()
				a.pendingRedraw = true
				continue
			}
			if update.page >= len(a.pageMetrics) {
				continue
			}
			a.pageMetrics[update.page] = update.metrics
			a.setPageBounds(&a.pageMetrics[update.page])
			changed = true
		default:
			if changed {
				oldX, oldY := a.scrollX, a.scrollY
				a.recomputeLayout(a.viewportSize())
				a.restoreViewportAnchor(anchor)
				a.shiftSmoothScroll(oldX, oldY)
				a.pendingRedraw = true
			}
			return
		}
	}
}

func (a *App) openDocument(path string, opts openDocumentOptions) error {
	return a.openDocumentWithPassword(path, opts, "")
}

func (a *App) openDocumentWithPassword(path string, opts openDocumentOptions, password string) error {
	path = config.AbsoluteDocumentPath(path)
	file := path
	if remote.IsURI(path) {
		if opts.remote == nil {
			a.fetchRemoteDocument(path, opts, password)
			return nil
		}
		file = opts.remote.Local
	}
	a.message = "opening " + remote.Display(path)
	a.logf("opening document path=%q file=%q startPage=%d reloadConfig=%t", remote.Display(path), file, opts.startPage+1, opts.reloadConfig)
	a.emitPluginEvent("document_open_pre", map[string]any{"document": map[string]any{"path": path, "name": config.DocumentName(path)}})
	doc, err := mupdf.Open(file, a.openOptions(password))
	if err != nil {
		a.logf("open document failed path=%q err=%v", remote.Display(path), err)
		if mupdf.IsPasswordError(err) {
			a.promptDocumentPassword(path, opts)
			return nil
		}
		return err
	}
	pages := doc.CachedPageCount()
	startPage := opts.startPage
	if startPage < 0 {
		startPage = 0
	}
	if pages > 0 && startPage >= pages {
		startPage = pages - 1
	}
	savedState, hasSavedState := a.documentSessionViewState(path)
	if !opts.startPageExplicit && hasSavedState {
		startPage = clampInt(savedState.page, 0, max(0, pages-1))
	}

	oldDocumentPayload := a.documentEventPayload()
	hadDocument := a.doc != nil
	if hadDocument && a.runtime != nil {
		a.emitPluginEvent("document_close_pre", oldDocumentPayload)
	}
	a.closeViewMode()
	a.saveDocumentSession()
	a.closeDocumentResources()
	if hadDocument && a.runtime != nil {
		a.emitPluginEvent("document_closed", oldDocumentPayload)
	}
	a.runtime.SetPageCount(pages)

	if opts.remote != nil {
		a.remoteInfo = *opts.remote
		a.document.recordRemote(*opts.remote)
	} else {
		a.remoteInfo = remote.Info{}
		a.document.record(path)
	}
	a.installDocument(doc, path, file, pages, startPage)
	a.rebindInstanceServer()
	a.resetForNewDocument(password)

	a.initDocumentMetrics(doc, pages, startPage)
	a.logf("opened document path=%q pages=%d page=%d", remote.Display(path), pages, startPage+1)

	var configErr error
	if opts.reloadConfig {
		configErr = a.runtime.SetDocument(path, pages)
		a.removeStaleLuaViews(a.runtime.Generation())
	}
	a.applyConfigState(a.runtime.Config())
	a.message = a.config.NormalMessage

	a.setWindowTitle()
	a.initRenderWorker()
	a.initSearch()
	a.recomputeLayout(a.viewportSize())
	if opts.preserveView != nil {
		a.restoreViewState(*opts.preserveView)
		if opts.startPageExplicit {
			a.alignPageToAnchor(startPage)
		}
	} else if !opts.startPageExplicit && hasSavedState {
		a.restoreViewState(savedState)
	} else {
		a.alignPageToAnchor(startPage)
	}
	a.resetRenderScale()
	a.pendingRedraw = true
	if a.runtime != nil {
		a.emitPluginEvent("document_opened", a.documentEventPayload())
	}
	if configErr != nil {
		a.logf("document config reload failed err=%v", configErr)
		a.message = configErr.Error()
		return configErr
	}
	return nil
}

func (a *App) openOptions(password string) mupdf.OpenOptions {
	return mupdf.OpenOptions{Password: password, StoreBytes: int64(a.config.MuPDFStoreMB) << 20}
}

func (a *App) resetForNewDocument(password string) {
	a.cancelSmoothZoom()
	a.docPassword = password
	a.rotation = 0
	a.zoom = a.clampZoom(1)
	a.scale = 1
	a.scrollX = 0
	a.scrollY = 0
	a.search = searchState{}
	a.outlineMenu = outlineMenuState{}
	a.keybindMenu = keybindMenuState{}
	a.closeAllUIViews(true)
	a.completion = completionState{}
	a.mode = modeNormal
	a.input.Reset()
	a.ignoreText = ""
	a.sequence = nil
	a.pendingCount = ""
	a.jumpBack = nil
	a.jumpAhead = nil
	a.pendingOpen = ""
	a.hints = nil
	a.preview = nil
	a.discardWarned, a.editPos, a.editEnd, a.savedPos = false, 0, 0, 0
	a.setUnsaved(false)
}

func (a *App) promptDocumentPassword(path string, opts openDocumentOptions) {
	a.closeAllUI()
	a.mode = modePassword
	a.input.Reset()
	a.passwordPrompt = pendingPasswordPrompt{path: path, opts: opts}
	a.message = "password required: " + config.DocumentName(path)
	a.pendingRedraw = true
}

func (a *App) submitDocumentPassword(password string) {
	prompt := a.passwordPrompt
	a.passwordPrompt = pendingPasswordPrompt{}
	if prompt.path == "" {
		return
	}
	if err := a.openDocumentWithPassword(prompt.path, prompt.opts, password); err != nil {
		a.message = err.Error()
	}
}

func (a *App) initDocumentMetrics(doc *mupdf.Document, pages int, startPage int) {
	defaultW, defaultH := 612.0, 792.0
	if pages > 0 {
		if m, err := loadPageMetrics(doc, startPage, a.trimMargins); err == nil {
			a.setPageBounds(&m)
			a.pageMetrics[startPage] = m
			defaultW, defaultH = m.width, m.height
		}
	}
	if defaultW == 0 || defaultH == 0 {
		defaultW, defaultH = 612.0, 792.0
	}
	for i := range a.pageMetrics {
		if !a.pageMetrics[i].loaded {
			a.pageMetrics[i].width = defaultW
			a.pageMetrics[i].height = defaultH
		}
	}

	if pages > 1 {
		a.logf("queue metric loader pages=%d startPage=%d", pages, startPage+1)
		a.pendingLoad = true
		a.pendingPages = pages
		a.pendingStart = startPage
	}
}

// installDocument makes doc, read from file, the document known as path.
func (a *App) installDocument(doc *mupdf.Document, path, file string, pages, startPage int) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	a.generation++
	a.docPath = path
	a.docFile = file
	// The new copy is held before the old is let go, which may be the same.
	release := a.releaseCopy
	a.releaseCopy = nil
	if remote.IsURI(path) {
		a.releaseCopy = remote.Hold(file)
	}
	if release != nil {
		release()
	}
	a.docName = config.DocumentName(path)
	a.recordRecentFile(path)
	a.doc = doc
	a.pageCount = pages
	a.page = startPage
	a.pageMetrics = make([]pageMetrics, pages)
	a.rows = nil
	a.pageToRow = nil
	a.contentW = 0
	a.contentH = 0
	a.renderBaseScale = 0
	a.pageLinks = map[int][]mupdf.Link{}
	a.pageRevisions = nil
	a.outline = nil
	a.viewEvents = viewStateEvents{}
	a.selection = textSelection{}
}

func (a *App) pollDocumentUpdate() {
	if !a.config.AutoReload {
		return // a change stays pending until auto_reload is turned on
	}
	if a.remoteTask != nil {
		return // a change waits for the fetch or save under way
	}
	change, ok := a.document.poll(time.Now())
	if !ok {
		return
	}
	if a.unsaved {
		a.document.commit(change)
		a.message = a.docName + " changed on disk; keeping unsaved edits"
		return
	}
	a.logf("document changed size=%d mod=%s", change.size, change.mod.Format(time.RFC3339Nano))
	if err := a.reloadUpdatedDocument(change); err != nil {
		a.logf("document reload failed err=%v", err)
		a.message = err.Error()
	}
}

func (a *App) reloadUpdatedDocument(change documentChange) error {
	if a.overview != nil {
		a.closeOverview(a.overview.selected)
	}
	if remote.IsURI(a.docPath) {
		a.refetchRemoteDocument(change)
		return nil
	}
	// Presentation mode stays on, so reload the view it shows rather than
	// the one it returns to.
	state := a.currentViewState()
	if err := a.softReloadDocument(a.docPath, a.docFile, state); err != nil {
		return err
	}
	a.document.commit(change)
	a.message = "reloaded " + a.docName
	a.pendingRedraw = true
	return nil
}

// softReloadDocument reads path again, from file, keeping the view state.
func (a *App) softReloadDocument(path, file string, state viewState) error {
	a.logf("soft reload document path=%q file=%q", remote.Display(path), file)
	doc, err := mupdf.Open(file, a.openOptions(a.docPassword))
	if err != nil {
		return err
	}
	pages := doc.CachedPageCount()
	startPage := clampInt(state.page, 0, max(0, pages-1))

	if a.runtime != nil {
		a.runtime.SetPageCount(pages)
	}
	// Keep the old tiles on screen as placeholders until fresh ones arrive.
	a.closeDocument()
	a.cache.retainPages(pages)
	a.invalidateRenderRequests()

	a.installDocument(doc, path, file, pages, startPage)
	a.rebindInstanceServer()

	a.initDocumentMetrics(doc, pages, startPage)
	a.logf("soft reloaded document path=%q pages=%d page=%d", remote.Display(path), pages, startPage+1)
	a.setWindowTitle()
	a.initRenderWorker()
	a.initSearch()
	a.recomputeLayout(a.viewportSize())
	a.restoreViewState(state)
	a.resetRenderScale()
	a.pendingRedraw = true
	if a.runtime != nil {
		a.emitPluginEvent("document_reloaded", a.documentEventPayload())
	}
	return nil
}

func (a *App) documentEventPayload() map[string]any {
	return map[string]any{
		"document": map[string]any{
			"path":       a.docPath,
			"name":       a.docName,
			"page_count": a.pageCount,
		},
		"generation": a.generation,
	}
}
