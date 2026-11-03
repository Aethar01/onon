package viewer

import (
	"log"

	"onon/internal/config"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

type App struct {
	runtime *config.Runtime
	config  config.Config
	verbose bool
	// launchArgs are the flags a new window's process inherits.
	launchArgs []string

	// tab is the document shown; tabs holds every document open in the
	// window, in order, this one among them.
	*tab
	tabs []*tab

	windowState
	sdlState
	inputState
	interactionState
	uiState
	navigationState
}

type NewOptions struct {
	Verbose           bool
	StartPageExplicit bool
	// LaunchArgs are the flags a new window's process inherits, such as
	// --config.
	LaunchArgs []string
	// RestoreTabs are the tabs to open around the document, as the window
	// had them when it last closed.
	RestoreTabs config.WindowTabs
}

func New(docPath string, runtime *config.Runtime, startPage int, iconBytes []byte, opts NewOptions) (*App, error) {
	cfg := runtime.Config()
	if startPage < 0 {
		startPage = 0
	}
	first := newTab(startPage)
	first.zoom = cfg.MinZoom
	first.cache = tileCache{byteLimit: pageCacheByteLimit(cfg)}
	app := &App{
		runtime:     runtime,
		verbose:     opts.Verbose,
		launchArgs:  opts.LaunchArgs,
		tab:         first,
		tabs:        []*tab{first},
		windowState: windowState{pageStep: 64},
		sdlState: sdlState{
			iconBytes: iconBytes,
		},
		inputState: inputState{
			mouseBindings:  map[string]string{},
			sequenceLookup: map[string]string{},
		},
	}
	app.logf("create viewer doc=%q startPage=%d", remote.Display(docPath), startPage+1)
	app.remotePrompts = make(chan *remotePrompt, 4)
	remote.SetPrompter(app.askRemote)
	runtime.AttachHost(app)
	runtime.SetWake(app.wakeLoop)
	app.applyConfigState(cfg)
	app.message = cfg.NormalMessage
	if docPath != "" {
		app.initialDocPath = docPath
		app.initialStartPage = startPage
		app.initialPageSet = opts.StartPageExplicit
		app.initialTabs = opts.RestoreTabs
	}
	app.recomputeLayout(1400, 900-app.statusReservedHeight())
	if app.doc != nil {
		app.ensureRenderBaseScale()
		app.alignPageToAnchor(startPage)
	}
	return app, nil
}

func (a *App) logf(format string, args ...any) {
	if a != nil && a.verbose {
		log.Printf(format, args...)
	}
}

func (a *App) setWindowTitle() {
	if a.window == nil || a.background {
		return
	}
	a.showWindowDocument()
	if a.docName == "" {
		sdl.SetWindowTitle(a.window, "onon")
		return
	}
	sdl.SetWindowTitle(a.window, a.docName+" - onon")
}

// showWindowDocument tells the system which file the window shows and
// whether it has unsaved edits, on systems that show them.
func (a *App) showWindowDocument() {
	if a.window != nil && !a.background {
		file := a.docPath
		if remote.IsURI(file) {
			file = "" // no local file for the proxy icon
		}
		setNativeWindowDocument(a.window, file, a.unsaved)
	}
}

func (a *App) Close() {
	if a.window != nil {
		a.rememberTabs() // only a window that was shown, not one a test made
	}
	for _, t := range a.tabList() {
		a.withTab(t, a.closeTabDocument)
	}
	if a.remotePrompts != nil {
		remote.SetPrompter(nil)
	}
	if a.runtime != nil {
		a.emitPluginEvent("shutdown", map[string]any{})
	}
	a.stopTextInput()
	a.sdlState.Close()
}

func (a *App) emitPluginEvent(event string, payload map[string]any) bool {
	if a.runtime == nil {
		return false
	}
	consumed := a.runtime.EmitPluginEvent(event, payload)
	a.applyRuntimeChanges(event)
	return consumed
}

func (a *App) applyRuntimeChanges(source string) {
	if a.runtime == nil {
		return
	}
	dirty, assigned := a.runtime.ConsumeDirty()
	if !dirty {
		return
	}
	a.applyConfig(a.runtime.Config(), assigned)
	if source == "option_changed" {
		return
	}
	a.runtime.EmitPluginEvent("option_changed", map[string]any{"source": source})
	if dirty, assigned := a.runtime.ConsumeDirty(); dirty {
		a.applyConfig(a.runtime.Config(), assigned)
	}
}

func (a *App) closeDocumentResources() {
	a.closeDocument()
	a.clearCache()
}

// closeDocument stops the document's workers and closes it, leaving
// rendered tiles cached.
func (a *App) closeDocument() {
	a.closeDocumentWorkers()
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	if a.doc != nil {
		a.doc.Close()
		a.doc = nil
	}
}

func (a *App) closeDocumentWorkers() {
	if a.renderWorker != nil {
		a.renderWorker.Close()
		a.renderWorker = nil
	}
	if a.metricLoader != nil {
		a.logf("close metric loader")
		a.metricLoader.Close()
		a.metricLoader = nil
	}
	if a.searchWorker != nil {
		a.logf("close search worker")
		a.searchWorker.Close()
		a.searchWorker = nil
	}
	a.pendingLoad = false
	a.pendingPages = 0
	a.pendingStart = 0
}
