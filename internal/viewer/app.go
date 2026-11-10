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
	if startPage < 0 {
		startPage = 0
	}
	app := newApp(runtime, iconBytes, opts)
	app.logf("create viewer doc=%q startPage=%d", remote.Display(docPath), startPage+1)
	app.page = startPage
	remote.SetPrompter(app.askRemote)
	runtime.AttachHost(app)
	runtime.SetWake(app.wakeLoop)
	if docPath != "" {
		app.initialDocPath = docPath
		app.initialStartPage = startPage
		app.initialPageSet = opts.StartPageExplicit
		app.initialTabs = opts.RestoreTabs
	}
	app.recomputeLayout(1400, 900-app.statusReservedHeight())
	return app, nil
}

// newApp makes a window, with one empty tab, set up as runtime's
// configuration asks.
func newApp(runtime *config.Runtime, iconBytes []byte, opts NewOptions) *App {
	cfg := runtime.Config()
	first := newTab(0)
	first.zoom = cfg.MinZoom
	first.cache = tileCache{byteLimit: pageCacheByteLimit(cfg)}
	app := &App{
		runtime:     runtime,
		verbose:     opts.Verbose,
		launchArgs:  opts.LaunchArgs,
		tab:         first,
		tabs:        []*tab{first},
		windowState: windowState{pageStep: 64, remotePrompts: make(chan *remotePrompt, 4)},
		sdlState: sdlState{
			iconBytes: iconBytes,
		},
		inputState: inputState{
			mouseBindings:  map[string]string{},
			sequenceLookup: map[string]string{},
		},
	}
	app.applyConfigState(cfg)
	app.message = cfg.NormalMessage
	return app
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
	name := a.docName
	if name == "" && a.pendingPath != "" {
		name = config.DocumentName(a.pendingPath)
	}
	switch {
	case a.nativeTabsOn():
		sdl.SetWindowTitle(a.window, a.tab.tabName()) // as its tab shows it
	case name == "":
		sdl.SetWindowTitle(a.window, "onon")
	default:
		sdl.SetWindowTitle(a.window, name+" - onon")
	}
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

// Close closes every window of the process still open, and the process's
// share of the viewer: the shutdown event, and SDL.
func (a *App) Close() {
	g := a.windowGroup()
	if f := g.focused; f != nil && f.window != nil {
		f.rememberTabs() // only a window that was shown, not one a test made
	}
	a.closeBackgroundDocuments()
	for _, w := range g.windows {
		w.closeWindow()
	}
	g.windows = nil
	g.server.Close()
	g.server = nil
	if a.remotePrompts != nil {
		remote.SetPrompter(nil)
	}
	if a.runtime != nil {
		a.emitPluginEvent("shutdown", map[string]any{})
	}
	if g.waker != nil {
		sdl.Quit()
	}
}

// closeWindow closes a window's tabs, and the window itself.
func (a *App) closeWindow() {
	for _, t := range a.tabList() {
		a.withTab(t, a.closeTabDocument)
	}
	a.dropBackgroundPages()
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
