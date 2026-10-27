package viewer

import (
	"log"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

type App struct {
	runtime *config.Runtime
	config  config.Config
	verbose bool
	// launchArgs are the flags a new window's process inherits.
	launchArgs []string

	documentState
	viewStateFields
	layoutState
	sdlState
	documentWorkers
	renderService
	metricsService
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
}

func New(docPath string, runtime *config.Runtime, startPage int, iconBytes []byte, opts NewOptions) (*App, error) {
	cfg := runtime.Config()
	if startPage < 0 {
		startPage = 0
	}
	app := &App{
		runtime:    runtime,
		verbose:    opts.Verbose,
		launchArgs: opts.LaunchArgs,
		documentState: documentState{
			page:      startPage,
			pageLinks: map[int][]mupdf.Link{},
		},
		viewStateFields: viewStateFields{
			zoom:     cfg.MinZoom,
			scale:    1,
			pageStep: 64,
		},
		sdlState: sdlState{
			iconBytes: iconBytes,
		},
		renderService: renderService{
			cache:              tileCache{byteLimit: pageCacheByteLimit(cfg)},
			minRenderBaseScale: 0.25,
		},
		metricsService: metricsService{},
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
	if a.window == nil {
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
	if a.window != nil {
		file := a.docPath
		if remote.IsURI(file) {
			file = "" // no local file for the proxy icon
		}
		setNativeWindowDocument(a.window, file, a.unsaved)
	}
}

func (a *App) Close() {
	a.cancelRemoteTask()
	if a.remotePrompts != nil {
		remote.SetPrompter(nil)
	}
	hadDocument := a.doc != nil
	payload := a.documentEventPayload()
	if hadDocument && a.runtime != nil {
		a.emitPluginEvent("document_close_pre", payload)
	}
	a.saveDocumentSession()
	a.document.Close()
	a.closeDocumentResources()
	if hadDocument && a.runtime != nil {
		a.emitPluginEvent("document_closed", payload)
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
