package viewer

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
	"time"

	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// initSDL starts SDL's video subsystem. It may run more than once: SDL counts
// the calls, and Close shuts it down for good.
func initSDL() error {
	// On macOS, prefer normal key repeat over the system press-and-hold accent
	// menu. SDL requires this hint to be set before initialization.
	sdl.SetHint("SDL_MAC_PRESS_AND_HOLD", "0")
	// Keep the floating-point wheel values so continuous devices can scroll by
	// fractional scroll steps. The integer mode would quantize them upstream.
	sdl.SetHint("SDL_MOUSE_INTEGER_MODE", "0")
	// Touch is handled as touch, so a finger pans rather than selects; see
	// handleTouch.
	sdl.SetHint("SDL_TOUCH_MOUSE_EVENTS", "0")
	if !sdl.Init(sdl.InitVideo) {
		return fmt.Errorf("SDL init failed: %s", sdl.GetError())
	}
	return nil
}

func (a *App) Run() error {
	a.logf("init SDL")
	if err := initSDL(); err != nil {
		return err
	}
	sdl.SetHint("SDL_RENDER_SCALE_QUALITY", "2")
	window := sdl.CreateWindow("onon", 1400, 900, sdl.WindowResizable|sdl.WindowHighPixelDensity)
	if window == nil {
		sdl.Quit()
		return fmt.Errorf("SDL window creation failed: %s", sdl.GetError())
	}
	renderer, rendererNote := createRenderer(window, a.config.Renderer)
	if renderer == nil {
		sdl.DestroyWindow(window)
		sdl.Quit()
		return fmt.Errorf("SDL renderer creation failed: %s", sdl.GetError())
	}
	nativeApp = a
	a.configureNativeWindow(window)
	// Frames are presented in step with the display, so animations move
	// once per refresh rather than when a timer happens to fire.
	if !sdl.SetRenderVSync(renderer, 1) {
		a.logf("vsync unavailable: %s", sdl.GetError())
	}
	a.logf("created SDL window 1400x900 renderer=%s", sdl.GetRendererName(renderer))
	a.window = window
	a.renderer = renderer
	a.updateDisplayFrame()
	a.loadUIFont() // again, now the display scale is known
	a.waker = newLoopWaker()
	a.installMenuBar()
	// Only Linux needs the window icon set. macOS takes the app icon from the
	// bundle and Windows from the exe's icon resource, and setting one here
	// would replace them: the Dock's adaptive icon, or the .ico's small sizes.
	if runtime.GOOS == "linux" {
		if rw := sdl.IOFromConstMem(a.iconBytes); rw != nil {
			if icon := sdl.LoadBMPIO(rw, true); icon != nil {
				sdl.SetWindowIcon(window, icon)
				sdl.DestroySurface(icon)
			}
		}
	}
	sdl.SetEventEnabled(sdl.EventDropFile, true)
	a.setWindowTitle()
	sdl.SetRenderDrawBlendMode(a.renderer, sdl.BlendModeBlend)
	sdl.SetDefaultTextureScaleMode(a.renderer, sdl.ScaleModeLinear)
	var outputW, outputH int32
	if sdl.GetRenderOutputSize(a.renderer, &outputW, &outputH) {
		w, h := outputW, outputH
		a.winW, a.winH = int(w), int(h)
	}
	if err := a.openInitialDocument(); err != nil {
		return err
	}
	a.showConfigWarnings()
	if rendererNote != "" {
		a.logf("%s", rendererNote)
		a.message = rendererNote
	}
	a.refreshReducedMotion()
	a.followSystemColors()
	a.recomputeLayout(a.viewportSize())
	a.pendingRedraw = true
	a.syncTextInput()
	if a.runtime != nil {
		a.emitPluginEvent("app_ready", a.documentEventPayload())
		a.flushPendingInstanceCommand()
	}
	defer a.stopTextInput()
	defer a.cancelSmoothScroll()
	defer a.cancelSmoothZoom()
	for !a.quit {
		var event sdl.Event
		for sdl.PollEvent(&event) {
			if err := a.handleSDLEvent(&event); err != nil {
				return err
			}
		}
		a.refreshStaleSelection()
		a.advanceSmoothScroll()
		a.advanceSmoothZoom()
		a.advanceAutoscroll()
		a.advanceTitleBar()
		if a.runtime != nil {
			if a.runtime.PollPluginOperations() {
				a.applyRuntimeChanges("plugin operation")
				a.pendingRedraw = true
			}
		}
		if a.pollInstanceCommands() {
			a.pendingRedraw = true
		}
		if a.pollSystemActions() {
			a.pendingRedraw = true
		}
		a.pollRenderUpdates()
		a.pollMetricUpdates()
		a.flushPendingInstanceJump()
		a.pollSearchUpdates()
		a.pollRemoteTask()
		a.pollRemotePrompts()
		a.pollDocumentUpdate()
		a.expireSequence()
		a.finishKeybindCapture(time.Now())
		a.revealLinkPreview()
		a.pollPrintResult()
		a.prefetchVisiblePages()
		a.adjustRenderBaseScaleForExtremeZoom(a.scale)
		a.emitViewStateEvents()
		if a.pendingRedraw {
			if err := a.drawFrame(); err != nil {
				return err
			}
		}
		if !a.quit {
			var event sdl.Event
			if sdl.WaitEventTimeout(&event, int32(a.eventWaitTimeoutMS())) {
				if err := a.handleSDLEvent(&event); err != nil {
					return err
				}
			}
		}
	}
	a.logf("viewer exiting")
	return nil
}

// createRenderer makes the renderer the renderer option names, or SDL's own
// choice for "auto", or when SDL_RENDER_DRIVER in the environment chooses
// one, or when the one named cannot start, which the note then says.
func createRenderer(window *sdl.Window, name string) (*sdl.Renderer, string) {
	if name == "" || name == "auto" || os.Getenv("SDL_RENDER_DRIVER") != "" {
		return sdl.CreateRenderer(window, ""), ""
	}
	if renderer := sdl.CreateRenderer(window, name); renderer != nil {
		return renderer, ""
	}
	reason := sdl.GetError()
	renderer := sdl.CreateRenderer(window, "")
	if renderer == nil {
		return nil, ""
	}
	return renderer, fmt.Sprintf("renderer %q unavailable (%s); using %s", name, reason, sdl.GetRendererName(renderer))
}

func (a *App) openInitialDocument() error {
	if a.initialDocPath == "" {
		return nil
	}
	a.message = "opening " + remote.Display(a.initialDocPath)
	a.pendingRedraw = true
	if err := a.drawFrame(); err != nil {
		return err
	}
	path := a.initialDocPath
	startPage := a.initialStartPage
	pageSet := a.initialPageSet
	a.initialDocPath = ""
	a.initialPageSet = false
	a.logf("open initial document path=%q page=%d", remote.Display(path), startPage+1)
	if err := a.openDocument(path, openDocumentOptions{startPage: startPage, startPageExplicit: pageSet}); err != nil {
		a.message = err.Error()
		a.pendingRedraw = true
	}
	return nil
}

func (a *App) eventWaitTimeoutMS() int {
	if a.smoothScrollActive() || a.smoothZoomAnimating() || a.autoscrollMoving() || a.loaderVisible || a.motion.animating {
		// Wait only what is left of the frame; drawing and presenting,
		// which waits for the display, took the rest. Once the frame is
		// over the next begins, so an animation that draws nothing, such
		// as autoscroll held at the end of the document, still waits a
		// frame between steps rather than spinning.
		left := a.animationFrameDuration() - time.Since(a.frameStart)
		if left <= 0 {
			a.frameStart = time.Now()
			return 0
		}
		return int(left / time.Millisecond)
	}
	// Wake for the earliest pending deadline.
	deadlines := []time.Time{a.captureDeadline(), a.previewDeadline(), a.renderScaleReadyAt}
	if wait := a.titleBarWaitTimeout(); wait > 0 {
		deadlines = append(deadlines, time.Now().Add(wait))
	}
	if len(a.sequence) > 0 {
		deadlines = append(deadlines, a.sequenceAt.Add(time.Duration(a.config.SequenceTimeoutMS)*time.Millisecond))
	}
	var deadline time.Time
	for _, d := range deadlines {
		if !d.IsZero() && (deadline.IsZero() || d.Before(deadline)) {
			deadline = d
		}
	}
	if !deadline.IsZero() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 1
		}
		if remaining < 100*time.Millisecond {
			return max(1, int(remaining/time.Millisecond))
		}
	}
	return 100
}

func (a *App) convertPointerEventToRenderCoordinates(event *sdl.Event) {
	if a.renderer == nil || event == nil {
		return
	}
	switch event.Type() {
	case sdl.EventMouseButtonDown, sdl.EventMouseButtonUp, sdl.EventMouseMotion:
		sdl.ConvertEventToRenderCoordinates(a.renderer, event)
	}
}

func (a *App) handleSDLEvent(event *sdl.Event) error {
	if a.isWakeEvent(event) {
		return nil // the loop polls the workers' channels after events
	}
	a.convertPointerEventToRenderCoordinates(event)
	defer a.syncTextInput()
	defer a.notifySelectionChange()
	if event.Type() != sdl.EventMouseMotion {
		a.refreshStaleSelection() // what follows may read the selection
	}

	redraw := true
	switch event.Type() {
	case sdl.EventQuit:
		a.quit = a.confirmDiscard("closing")
		redraw = false
	case sdl.EventWindowExposed:
		redraw = true
	case sdl.EventWindowDisplayScaleChanged:
		a.relayoutWithViewportAnchor(a.loadUIFont)
	case sdl.EventWindowResized, sdl.EventWindowPixelSizeChanged:
		e := event.Window()
		a.relayoutWithViewportAnchor(func() {
			a.winW = int(e.Data1)
			a.winH = int(e.Data2)
		})
	case sdl.EventWindowEnterFullscreen:
		a.fullscreen = true
		redraw = false
	case sdl.EventWindowLeaveFullscreen:
		a.fullscreen = false
		redraw = false
	case sdl.EventWindowDisplayChanged, sdl.EventDisplayCurrentModeChanged:
		a.updateDisplayFrame()
		redraw = false
	case sdl.EventSystemThemeChanged:
		a.followSystemColors()
	case sdl.EventWindowFocusGained:
		a.refreshReducedMotion()
		redraw = false
	case sdl.EventWindowFocusLost:
		a.stopPan()
		a.stopAutoscroll()
		redraw = false
	case sdl.EventKeyUp:
		e := event.Key()
		a.handleSDLKeyUp(&e)
		redraw = false
	case sdl.EventKeyDown:
		a.preview = nil
		e := event.Key()
		if a.handleAutoscrollKey(&e) {
			break
		}
		a.handleSDLKeyDown(&e)
	case sdl.EventTextInput:
		e := event.Text()
		a.composition = ""
		a.handleSDLTextInput(&e)
	case sdl.EventTextEditing:
		e := event.Edit()
		a.handleSDLTextEditing(&e)
	case sdl.EventMouseWheel:
		a.preview = nil
		if a.autoscroll != nil {
			a.stopAutoscroll()
			break
		}
		e := event.Wheel()
		a.handleAnimatedMouseWheel(&e)
		redraw = false
	case sdl.EventPinchBegin:
		a.beginPinch()
		redraw = false
	case sdl.EventPinchUpdate:
		a.updatePinch(float64(pinchEventScale(event)))
		redraw = false
	case sdl.EventPinchEnd:
		a.endPinch()
		redraw = false
	case sdl.EventMouseButtonDown, sdl.EventMouseButtonUp:
		a.preview = nil
		e := event.Button()
		a.handleMouseButtonEvent(&e)
	case sdl.EventMouseMotion:
		e := event.Motion()
		redraw = a.handleMouseMotionEvent(&e)
	case sdl.EventFingerDown, sdl.EventFingerMotion, sdl.EventFingerUp, sdl.EventFingerCanceled:
		e := event.TFinger()
		a.handleTouch(event.Type(), &e)
	case sdl.EventDropFile:
		e := event.Drop()
		a.handleDroppedFile(e.Data(), e.WindowID)
		redraw = e.Data() != ""
	default:
		redraw = false
	}
	if redraw {
		a.pendingRedraw = true
	}
	return nil
}

// handleMouseMotionEvent moves the pointer, reporting whether the window
// needs drawing again.
func (a *App) handleMouseMotionEvent(e *sdl.MouseMotionEvent) bool {
	a.noteTitleBarPointer(float64(e.X), float64(e.Y), true)
	if a.handleInputMouseMotion(e) {
		return true
	}
	return a.handleSDLMouseMotion(e)
}

func (a *App) handleMouseButtonEvent(e *sdl.MouseButtonEvent) {
	a.pointer = sdl.FPoint{X: e.X, Y: e.Y}
	if a.handleTitleBarButton(e) {
		return
	}
	if a.handleAutoscrollButton(e) {
		return
	}
	if a.runtime == nil {
		if !a.handleInputMouseButton(e) {
			a.handleSDLMouseButton(e)
		}
		return
	}
	payload := a.pluginMouseEventPayload(e)
	consumed := a.emitPluginEvent("mouse_button_pre", payload)
	if !consumed {
		consumed = a.handleInputMouseButton(e)
	}
	if !consumed {
		a.handleSDLMouseButton(e)
	}
	a.emitPluginEvent("mouse_button", payload)
}

func (a *App) pluginMouseEventPayload(e *sdl.MouseButtonEvent) map[string]any {
	button, _ := mouseButtonName(e.Button)
	phase := "up"
	if e.Type == sdl.EventMouseButtonDown {
		phase = "down"
	}
	mod := sdl.GetModState()
	page, point, ok := a.pagePointAtScreen(float64(e.X), float64(e.Y))
	payload := map[string]any{
		"phase":  phase,
		"button": button,
		"x":      float64(e.X),
		"y":      float64(e.Y),
		"modifiers": map[string]any{
			"ctrl":  mod&sdl.KeymodCtrl != 0,
			"shift": mod&sdl.KeymodShift != 0,
			"alt":   mod&sdl.KeymodAlt != 0,
			"gui":   mod&sdl.KeymodGui != 0,
		},
		"inside_document": ok,
	}
	if ok {
		payload["page"] = page + 1
		payload["page_x"] = point.X
		payload["page_y"] = point.Y
	}
	return payload
}

func (a *App) textInputNeeded() bool {
	if _, ok := a.uiEditTarget(); ok {
		return true
	}
	return a.mode != modeNormal ||
		func() bool {
			view := a.activeModalUIView()
			return view != nil && view.searching
		}()
}

func (a *App) syncTextInput() {
	if a.window == nil {
		return
	}
	want := a.textInputNeeded()
	if !want {
		a.composition = ""
	}
	if want == sdl.TextInputActive(a.window) {
		return
	}
	if want {
		if !sdl.StartTextInput(a.window) {
			a.logf("start text input failed: %s", sdl.GetError())
		}
		return
	}
	if !sdl.StopTextInput(a.window) {
		a.logf("stop text input failed: %s", sdl.GetError())
	}
}

func (a *App) stopTextInput() {
	if a.window == nil || !sdl.TextInputActive(a.window) {
		return
	}
	if !sdl.StopTextInput(a.window) {
		a.logf("stop text input failed: %s", sdl.GetError())
	}
}

// The SDL binding currently does not expose Event.Pinch(). SDL_Event stores
// the pinch scale immediately after the common event header.
func pinchEventScale(event *sdl.Event) float32 {
	const commonEventSize = 16 // type, reserved, timestamp
	return math.Float32frombits(binary.NativeEndian.Uint32(event[commonEventSize:]))
}

// handleDroppedFile opens a file dropped on the window, or one the system asked
// a running onon to open: on macOS, Finder and `open` send that to this process
// as a drop on no window. With open_in_new_window, that one gets a window of its
// own, as it would on other platforms, unless this window is still empty.
func (a *App) handleDroppedFile(path string, windowID sdl.WindowID) {
	if path == "" {
		return
	}
	if windowID == 0 && a.config.OpenInNewWindow && a.docPath != "" {
		if err := a.openInNewWindow(path); err != nil {
			a.message = err.Error()
		}
		return
	}
	if err := a.Open(path); err != nil {
		a.message = err.Error()
	}
}

func (a *App) drawFrame() error {
	if a.renderer == nil {
		return nil
	}
	var w, h int32
	if sdl.GetRenderOutputSize(a.renderer, &w, &h) {
		a.winW, a.winH = int(w), int(h)
	}
	bg := a.backgroundColor()
	a.showWindowColors(bg)
	if !sdl.SetRenderDrawColor(a.renderer, bg.R, bg.G, bg.B, bg.A) {
		return fmt.Errorf("SDL draw color failed: %s", sdl.GetError())
	}
	if !sdl.RenderClear(a.renderer) {
		return fmt.Errorf("SDL clear failed: %s", sdl.GetError())
	}
	a.frameStart = time.Now()
	a.beginMotionFrame()
	a.drawing = true
	defer func() { a.drawing, a.frameStatus = false, nil }()
	message := a.message
	a.withDocumentViewport(a.renderer, func() {
		a.drawPages(a.renderer)
		// An on-screen loader animates, so it asks for the next frame.
		a.pendingRedraw = a.loaderVisible
		a.drawLinkHints(a.renderer)
		a.drawLinkPreview(a.renderer)
	})
	if a.statusVisible() {
		if err := a.drawStatusBar(a.renderer); err != nil {
			return err
		}
	}
	if err := a.drawUIViews(a.renderer); err != nil {
		return err
	}
	// A view's text fields are known once drawn, and typing into them
	// needs text input on before the first key.
	a.syncTextInput()
	a.drawAutoscrollMarker(a.renderer) // over a menu it scrolls
	if err := a.drawTitleBar(a.renderer); err != nil {
		return err
	}
	// A transition still on its way asks for the next frame, as does a
	// message set while drawing, such as a theme's error, which the status
	// bar has already been drawn without.
	if a.motion.animating || a.message != message {
		a.pendingRedraw = true
	}
	sdl.RenderPresent(a.renderer)
	return nil
}
