package viewer

import (
	"encoding/binary"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestOpenInitialDocumentDisplaysOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.pdf")
	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), path)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()

	app, err := New(path, runtime, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if err := app.openInitialDocument(); err != nil {
		t.Fatalf("initial open should not close the viewer: %v", err)
	}
	if app.doc != nil {
		t.Fatal("expected the viewer to remain blank")
	}
	if !strings.Contains(app.message, "open document") || !strings.Contains(app.message, path) {
		t.Fatalf("expected document open error in status message, got %q", app.message)
	}
}

func TestHandleSDLEventTracksSystemFullscreenChanges(t *testing.T) {
	app := &App{tab: &tab{}}

	enter := sdl.Event{}
	binary.NativeEndian.PutUint32(enter[:], uint32(sdl.EventWindowEnterFullscreen))
	if err := app.handleSDLEvent(&enter); err != nil {
		t.Fatalf("handle enter fullscreen event: %v", err)
	}
	if !app.Fullscreen() {
		t.Fatal("expected system fullscreen event to update app state")
	}

	if err := app.ExecuteAction("toggle_fullscreen"); err != nil {
		t.Fatalf("toggle fullscreen: %v", err)
	}
	if app.Fullscreen() {
		t.Fatal("expected app toggle to leave fullscreen after system entered it")
	}

	app.fullscreen = true
	leave := sdl.Event{}
	binary.NativeEndian.PutUint32(leave[:], uint32(sdl.EventWindowLeaveFullscreen))
	if err := app.handleSDLEvent(&leave); err != nil {
		t.Fatalf("handle leave fullscreen event: %v", err)
	}
	if app.Fullscreen() {
		t.Fatal("expected system leave fullscreen event to update app state")
	}
}

func TestHandleSDLEventRedrawsExposedWindow(t *testing.T) {
	app := &App{tab: &tab{}}
	event := sdl.Event{}
	binary.NativeEndian.PutUint32(event[:], uint32(sdl.EventWindowExposed))

	if err := app.handleSDLEvent(&event); err != nil {
		t.Fatalf("handle window exposed event: %v", err)
	}
	if !app.pendingRedraw {
		t.Fatal("expected window exposed event to request a redraw")
	}
}

func TestEventWaitTimeoutUsesConfiguredAnimationFrame(t *testing.T) {
	app := &App{
		tab:        &tab{},
		config:     config.Config{AnimationFrameMS: 24},
		smoothZoom: &smoothZoomState{targetLog: 1, appliedLog: 0},
	}
	app.frameStart = time.Now()
	if got := app.eventWaitTimeoutMS(); got < 20 || got > 24 {
		t.Fatalf("expected what is left of a 24ms frame, got %d", got)
	}
	// A frame that took its whole interval to draw leaves no wait.
	app.frameStart = time.Now().Add(-30 * time.Millisecond)
	if got := app.eventWaitTimeoutMS(); got != 0 {
		t.Fatalf("expected no wait after a slow frame, got %d", got)
	}
	// With no frame drawn since, the next wait is a whole frame, not none.
	if got := app.eventWaitTimeoutMS(); got < 20 || got > 24 {
		t.Fatalf("expected a frame's wait when nothing was drawn, got %d", got)
	}
}

func TestHandleSDLEventFocusLossStopsKeyPanning(t *testing.T) {
	app := &App{tab: &tab{}, interactionState: interactionState{panning: true, panKeycode: sdl.KeycodeSpace}}
	event := sdl.Event{}
	binary.NativeEndian.PutUint32(event[:], uint32(sdl.EventWindowFocusLost))

	if err := app.handleSDLEvent(&event); err != nil {
		t.Fatalf("handle focus lost event: %v", err)
	}
	if app.panning || app.panKeycode != 0 {
		t.Fatalf("expected focus loss to stop key panning, panning=%v panKeycode=%v", app.panning, app.panKeycode)
	}
}

func TestHandleSDLEventPinchUpdatesZoom(t *testing.T) {
	app := &App{
		tab:    &tab{viewStateFields: viewStateFields{zoom: 2, fitMode: fitManual}},
		config: config.Config{PinchSensitivity: 2, MinZoom: 0.5, MaxZoom: 8, SmoothZoomSources: config.SmoothInputAll, SmoothZoomDampening: 0.35},
	}
	begin := sdl.Event{}
	binary.NativeEndian.PutUint32(begin[:], uint32(sdl.EventPinchBegin))
	if err := app.handleSDLEvent(&begin); err != nil {
		t.Fatalf("handle pinch begin: %v", err)
	}
	event := sdl.Event{}
	binary.NativeEndian.PutUint32(event[:], uint32(sdl.EventPinchUpdate))
	binary.NativeEndian.PutUint32(event[16:], math.Float32bits(1.25))

	if err := app.handleSDLEvent(&event); err != nil {
		t.Fatalf("handle pinch event: %v", err)
	}
	if app.zoom != 2 {
		t.Fatalf("expected pinch update to defer zoom until an animation frame, got %v", app.zoom)
	}
	if !app.smoothZoomAnimating() {
		t.Fatal("expected pinch target animation")
	}
	if !app.advanceSmoothZoomBy(smoothAnimationFrame) {
		t.Fatal("expected pinch animation frame to change zoom")
	}
	if app.zoom <= 2 || app.zoom >= 3.125 {
		t.Fatalf("expected pinch frame to scale zoom between 2 and 3.125, got %v", app.zoom)
	}
	if app.fitMode != fitManual {
		t.Fatalf("expected pinch animation to switch to manual zoom, got %q", app.fitMode)
	}
	end := sdl.Event{}
	binary.NativeEndian.PutUint32(end[:], uint32(sdl.EventPinchEnd))
	if err := app.handleSDLEvent(&end); err != nil {
		t.Fatalf("handle pinch end: %v", err)
	}
	if app.zoom >= 3.125 {
		t.Fatalf("expected pinch end to keep animating instead of jumping, got %v", app.zoom)
	}
	for app.smoothZoomAnimating() {
		app.advanceSmoothZoomBy(smoothAnimationFrame)
	}
	if math.Abs(app.zoom-3.125) > 0.0001 {
		t.Fatalf("expected pinch animation to settle at zoom 3.125, got %v", app.zoom)
	}
}

func TestHandleSDLEventPinchOutDoesNotReverseDirection(t *testing.T) {
	app := &App{tab: &tab{viewStateFields: viewStateFields{zoom: 2, fitMode: fitManual}}, config: config.Config{MinZoom: 0.5, MaxZoom: 8, SmoothZoomSources: config.SmoothInputAll, SmoothZoomDampening: 0.35}}
	for _, scale := range []float32{0.98, 0.99, 0.97} {
		event := sdl.Event{}
		binary.NativeEndian.PutUint32(event[:], uint32(sdl.EventPinchUpdate))
		binary.NativeEndian.PutUint32(event[16:], math.Float32bits(scale))
		if err := app.handleSDLEvent(&event); err != nil {
			t.Fatalf("handle pinch update: %v", err)
		}
	}
	if !app.advanceSmoothZoomBy(smoothAnimationFrame) {
		t.Fatal("expected pinch-out animation frame to change zoom")
	}
	if app.zoom >= 2 {
		t.Fatalf("expected pinch out to reduce zoom, got %v", app.zoom)
	}
}

func TestZoomActionAnimatesTowardTarget(t *testing.T) {
	app := &App{
		tab:    &tab{viewStateFields: viewStateFields{zoom: 2, fitMode: fitManual}},
		config: config.Config{MinZoom: 0.5, MaxZoom: 8, SmoothZoomSources: config.SmoothInputAll, SmoothZoomDampening: 0.35},
	}

	if err := app.runBuiltinAction("zoom_in"); err != nil {
		t.Fatal(err)
	}
	if app.zoom != 2 {
		t.Fatalf("expected zoom action to defer zoom until an animation frame, got %v", app.zoom)
	}
	if !app.smoothZoomAnimating() {
		t.Fatal("expected zoom animation target")
	}

	if !app.advanceSmoothZoomBy(smoothAnimationFrame) {
		t.Fatal("expected zoom animation frame to change zoom")
	}
	if app.zoom <= 2 || app.zoom >= 2*1.15 {
		t.Fatalf("expected zoom frame between 2 and 2.3, got %v", app.zoom)
	}
}

func TestRepeatedZoomActionsAccumulateTarget(t *testing.T) {
	app := &App{
		tab:    &tab{viewStateFields: viewStateFields{zoom: 2, fitMode: fitManual}},
		config: config.Config{MinZoom: 0.5, MaxZoom: 8, SmoothZoomSources: config.SmoothInputAll, SmoothZoomDampening: 0.35},
	}

	if err := app.runBuiltinAction("zoom_in"); err != nil {
		t.Fatal(err)
	}
	if err := app.runBuiltinAction("zoom_in"); err != nil {
		t.Fatal(err)
	}

	state := app.smoothZoom
	if state == nil {
		t.Fatal("expected zoom animation target")
	}
	assertClose(t, math.Exp(state.targetLog), 2*1.15*1.15)
}

func TestHandleDroppedFileQueuesOpenWithoutRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dropped.pdf")
	app := &App{tab: &tab{}}

	app.handleDroppedFile(path, 1)

	if app.pendingOpen != path {
		t.Fatalf("expected dropped path %q to be queued, got %q", path, app.pendingOpen)
	}
	if !app.quit {
		t.Fatal("expected drop open without runtime to quit for restart")
	}
}

func TestHandleDroppedFileOpensSystemRequestsInNewWindow(t *testing.T) {
	var started [][]string
	startProcess = func(cmd *exec.Cmd) error {
		started = append(started, cmd.Args[1:])
		return nil
	}
	t.Cleanup(func() { startProcess = (*exec.Cmd).Start })
	path := filepath.Join(t.TempDir(), "requested.pdf")
	tests := []struct {
		name      string
		windowID  sdl.WindowID
		option    bool
		docPath   string
		newWindow bool
	}{
		{"system request", 0, true, "/shown.pdf", true},
		{"drop on the window", 1, true, "/shown.pdf", false},
		{"option off", 0, false, "/shown.pdf", false},
		{"empty window", 0, true, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started = nil
			app := &App{tab: &tab{}, launchArgs: []string{"-config=/c.lua"}}
			app.config.OpenInNewWindow = tt.option
			app.docPath = tt.docPath

			app.handleDroppedFile(path, tt.windowID)

			if !tt.newWindow {
				if len(started) != 0 || app.pendingOpen != path {
					t.Fatalf("expected %q to open here, started %q, queued %q", path, started, app.pendingOpen)
				}
				return
			}
			want := []string{"-config=/c.lua", "--unique", "--", path}
			if len(started) != 1 || !slices.Equal(started[0], want) {
				t.Fatalf("expected one new window with %q, got %q", want, started)
			}
			if app.pendingOpen != "" {
				t.Fatalf("expected this window to keep its document, queued %q", app.pendingOpen)
			}
		})
	}
}

func TestTextInputNeededOnlyForActiveTextEntry(t *testing.T) {
	modalApp := func(searching bool) *App {
		view := &uiView{visible: true, modal: true, searching: searching}
		return &App{tab: &tab{}, uiState: uiState{views: uiManager{active: view}}}
	}
	// App carries a mutex, so cases hold pointers rather than copies.
	tests := []struct {
		name string
		app  *App
		want bool
	}{
		{name: "normal", app: &App{tab: &tab{}}, want: false},
		{name: "command prompt", app: &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}, want: true},
		{name: "goto prompt", app: &App{tab: &tab{}, inputState: inputState{mode: modeGotoPage}}, want: true},
		{name: "search prompt", app: &App{tab: &tab{}, inputState: inputState{mode: modeSearch}}, want: true},
		{name: "password prompt", app: &App{tab: &tab{}, inputState: inputState{mode: modePassword}}, want: true},
		{name: "modal menu", app: modalApp(false), want: false},
		{name: "modal search", app: modalApp(true), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.app.textInputNeeded(); got != tt.want {
				t.Fatalf("textInputNeeded() = %t, want %t", got, tt.want)
			}
		})
	}
}
