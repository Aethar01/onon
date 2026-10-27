package viewer

import (
	"os"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"
)

const documentReloadRetry = time.Second

type documentSession struct {
	watcher    *documentWatcher
	remote     *remoteWatcher // in place of watcher for a document on an SSH host
	pollRemote bool           // auto_reload: whether remote watches check at all
	delay      time.Duration  // how long writes must settle before a change is reported
	mod        time.Time
	size       int64

	pending     *documentChange
	lastAttempt time.Time
}

type documentChange struct {
	mod  time.Time
	size int64
}

func (s *documentSession) record(path string) {
	s.stopWatching()
	s.pending = nil
	s.lastAttempt = time.Time{}
	watcher, err := newDocumentWatcher(path, s.delay)
	if err != nil {
		// Fall back gracefully if watcher can't be created
		return
	}
	s.watcher = watcher
	s.watcher.record(path)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		s.mod = time.Time{}
		s.size = 0
		return
	}
	s.mod = info.ModTime()
	s.size = info.Size()
}

// recordRemote watches a remote document, as record does a local one.
// Only SSH hosts can be asked whether a file changed; an http(s) document
// is fetched again only when opened again.
func (s *documentSession) recordRemote(info remote.Info) {
	s.pending = nil
	s.lastAttempt = time.Time{}
	s.mod, s.size = info.ModTime, info.Size
	if s.remote != nil && s.remote.uri == info.URI {
		s.remote.setKnown(info) // our own save is not an outside change
		return
	}
	s.stopWatching()
	if remote.IsSFTP(info.URI) {
		s.remote = newRemoteWatcher(info, s.pollRemote)
	}
}

// setRemotePolling turns checking a remote document for changes on or off.
func (s *documentSession) setRemotePolling(on bool) {
	s.pollRemote = on
	if s.remote != nil {
		s.remote.enabled.Store(on)
	}
}

func (s *documentSession) stopWatching() {
	if s.watcher != nil {
		s.watcher.Close()
		s.watcher = nil
	}
	if s.remote != nil {
		s.remote.Close()
		s.remote = nil
	}
}

func (s *documentSession) poll(now time.Time) (documentChange, bool) {
	var source interface {
		waitForChange(time.Duration) (watcherChange, bool)
	}
	switch {
	case s.watcher != nil:
		source = s.watcher
	case s.remote != nil:
		source = s.remote
	default:
		return documentChange{}, false
	}

	if s.pending != nil {
		if !s.lastAttempt.IsZero() && now.Sub(s.lastAttempt) < documentReloadRetry {
			return documentChange{}, false
		}
		s.lastAttempt = now
		return *s.pending, true
	}

	change, ok := source.waitForChange(0)
	if !ok {
		return documentChange{}, false
	}

	s.pending = &documentChange{
		mod:  change.mod,
		size: change.size,
	}
	s.lastAttempt = now
	return *s.pending, true
}

func (s *documentSession) setDelay(delay time.Duration) {
	s.delay = delay
	if s.watcher != nil {
		s.watcher.delay.Store(int64(delay))
	}
}

func (s *documentSession) commit(change documentChange) {
	s.mod = change.mod
	s.size = change.size
	s.pending = nil
	s.lastAttempt = time.Time{}
}

func (s *documentSession) Close() {
	s.stopWatching()
}

type viewState struct {
	page            int
	scrollX         float64
	scrollY         float64
	anchor          viewportAnchor
	zoom            float64
	fitMode         fitMode
	renderMode      renderMode
	rotation        float64
	dualPage        bool
	firstPageOffset bool
	statusBarShown  bool
	altColors       bool
}

// closeViewMode leaves the overview or presentation mode, whichever is on,
// returning to the view from before it. Entering either mode calls it first,
// so at most one is ever on.
func (a *App) closeViewMode() {
	switch {
	case a.overview != nil:
		a.closeOverview(a.overview.selected)
	case a.presentation != nil:
		a.closePresentation()
	}
}

// captureViewState reports the view to persist; in the overview or
// presentation mode, that is the view they will return to.
func (a *App) captureViewState() viewState {
	if a.overview != nil {
		return a.overviewReturnState()
	}
	if a.presentation != nil {
		return a.presentationReturnState()
	}
	return a.currentViewState()
}

// currentViewState reports the view as shown, even in a mode that will
// return to another.
func (a *App) currentViewState() viewState {
	return viewState{
		page:            a.page,
		scrollX:         a.scrollX,
		scrollY:         a.scrollY,
		anchor:          a.captureViewportAnchor(),
		zoom:            a.zoom,
		fitMode:         a.fitMode,
		renderMode:      a.renderMode,
		rotation:        a.rotation,
		dualPage:        a.dualPage,
		firstPageOffset: a.firstPageOffset,
		statusBarShown:  a.statusBarShown,
		altColors:       a.altColors,
	}
}

func (a *App) restoreViewState(state viewState) {
	a.zoom = state.zoom
	a.fitMode = state.fitMode
	a.renderMode = state.renderMode
	a.rotation = normalizeRotation(state.rotation)
	a.dualPage = state.dualPage
	a.firstPageOffset = state.firstPageOffset
	a.statusBarShown = state.statusBarShown
	a.altColors = state.altColors
	if a.config.AltColorsSystem {
		a.altColors = a.wantAltColors(a.config) // the OS outranks a remembered mode
	}
	a.updatePageMetricSizes()
	a.page = clampInt(state.page, 0, max(0, a.pageCount-1))
	a.recomputeLayout(a.viewportSize())
	if state.anchor.valid {
		a.restoreViewportAnchor(state.anchor)
	} else {
		a.scrollX = state.scrollX
		a.scrollY = state.scrollY
		a.clampScroll()
	}
	if a.renderMode == renderContinuous {
		a.updateCurrentPageFromScroll()
	}
}

func (state viewState) atDocumentStart() viewState {
	state.page = 0
	state.scrollX = 0
	state.scrollY = 0
	// The anchor names a place in the old document, and restoreViewState
	// prefers it to the scroll position.
	state.anchor = viewportAnchor{}
	return state
}

func (state viewState) documentSession() config.DocumentSession {
	return config.DocumentSession{
		Page:            state.page,
		ScrollX:         state.scrollX,
		ScrollY:         state.scrollY,
		AnchorPage:      state.anchor.page,
		AnchorX:         state.anchor.point.X,
		AnchorY:         state.anchor.point.Y,
		AnchorValid:     state.anchor.valid,
		Zoom:            state.zoom,
		FitMode:         state.fitMode.String(),
		RenderMode:      state.renderMode.String(),
		Rotation:        state.rotation,
		DualPage:        state.dualPage,
		FirstPageOffset: state.firstPageOffset,
		StatusBarShown:  state.statusBarShown,
		AltColors:       state.altColors,
	}
}

func viewStateFromDocumentSession(session config.DocumentSession) viewState {
	return viewState{
		page:    session.Page,
		scrollX: session.ScrollX,
		scrollY: session.ScrollY,
		anchor: viewportAnchor{
			page:  session.AnchorPage,
			point: mupdf.Point{X: session.AnchorX, Y: session.AnchorY},
			valid: session.AnchorValid,
		},
		zoom:            session.Zoom,
		fitMode:         parseFitMode(session.FitMode),
		renderMode:      parseRenderMode(session.RenderMode),
		rotation:        session.Rotation,
		dualPage:        session.DualPage,
		firstPageOffset: session.FirstPageOffset,
		statusBarShown:  session.StatusBarShown,
		altColors:       session.AltColors,
	}
}

func (state viewState) documentMark() config.DocumentMark {
	return config.DocumentMark{
		Page:        state.page,
		ScrollX:     state.scrollX,
		ScrollY:     state.scrollY,
		AnchorPage:  state.anchor.page,
		AnchorX:     state.anchor.point.X,
		AnchorY:     state.anchor.point.Y,
		AnchorValid: state.anchor.valid,
	}
}

func viewportAnchorFromDocumentMark(mark config.DocumentMark) viewportAnchor {
	return viewportAnchor{page: mark.AnchorPage, point: mupdf.Point{X: mark.AnchorX, Y: mark.AnchorY}, valid: mark.AnchorValid}
}

func (a *App) saveDocumentSession() {
	if a == nil || !a.config.SessionDatabase || a.docPath == "" || a.pageCount == 0 {
		return
	}
	_ = config.SetDocumentSession(a.docPath, a.captureViewState().documentSession())
}

func (a *App) recordRecentFile(path string) {
	if a == nil || !a.config.SessionDatabase || path == "" {
		return
	}
	_ = config.RecordRecentFile(path, a.config.RecentFilesMax)
	if !remote.IsURI(path) {
		noteSystemRecentDocument(path) // the system's lists hold local files
	}
}

func (a *App) handleMarkToken(token string) bool {
	if a.pendingMark != "" {
		if token == "<Esc>" {
			a.pendingMark = ""
			a.message = ""
			return true
		}
		mode := a.pendingMark
		a.pendingMark = ""
		if !isMarkName(token) {
			a.message = "mark must be a letter"
			return true
		}
		if mode == "set" {
			a.setDocumentMark(token)
		} else {
			a.jumpDocumentMark(token)
		}
		return true
	}
	if token != "\"" && token != "'" {
		// SDL key-repeat events intentionally don't enter pushToken because a
		// repeated prefix can accidentally turn into a multi-key sequence. Run
		// unambiguous countable single-key actions here instead. This handles
		// both the initial keydown and repeats without changing sequence parsing;
		// pending numeric counts still flow through handleCountToken below.
		if a.pendingCount == "" {
			if action, ok := a.sequenceLookup[token]; ok && a.isCountableAction(action) && !a.hasPrefix(token) {
				a.runAction(action)
				return true
			}
		}
		return false
	}
	if !a.config.SessionDatabase {
		a.message = "marks require session_database"
		return true
	}
	if a.docPath == "" {
		a.message = "no document open"
		return true
	}
	if token == "\"" {
		a.pendingMark = "set"
		a.message = "mark: choose letter"
	} else {
		a.pendingMark = "jump"
		a.message = "jump mark: choose letter"
	}
	return true
}

func isMarkName(token string) bool {
	if len(token) != 1 {
		return false
	}
	r := token[0]
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func (a *App) setDocumentMark(name string) {
	if err := config.SetDocumentMark(a.docPath, name, a.captureViewState().documentMark()); err != nil {
		a.message = err.Error()
		return
	}
	a.message = "set mark " + name
}

func (a *App) jumpDocumentMark(name string) {
	mark, ok := config.GetDocumentMark(a.docPath, name)
	if !ok {
		a.message = "mark not set: " + name
		return
	}
	a.recordJump()
	a.page = clampInt(mark.Page, 0, max(0, a.pageCount-1))
	if mark.AnchorValid {
		a.restoreViewportAnchor(viewportAnchorFromDocumentMark(mark))
	} else {
		a.scrollX = mark.ScrollX
		a.scrollY = mark.ScrollY
		a.clampScroll()
	}
	if a.renderMode == renderContinuous {
		a.updateCurrentPageFromScroll()
	}
	a.message = "jumped to mark " + name
	a.pendingRedraw = true
}

func (a *App) documentSessionViewState(path string) (viewState, bool) {
	if a == nil || !a.config.SessionDatabase || path == "" {
		return viewState{}, false
	}
	session, ok := config.GetDocumentSession(path)
	if !ok {
		return viewState{}, false
	}
	return viewStateFromDocumentSession(session), true
}
