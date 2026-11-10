package viewer

import (
	"encoding/binary"
	"slices"

	"onon/internal/instance"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A process's windows share its Lua runtime and its event loop, which
// hands each SDL event to the window it is for. On macOS a process opens
// every window itself, so that the system can gather them into tabs; on
// other systems a new window is a process of its own.

// windowGroup is the windows of a process.
type windowGroup struct {
	windows []*App
	// focused is the window the Lua runtime, the menu bar and other
	// processes act on: the one focused last.
	focused *App
	waker   *loopWaker
	// server is where other processes ask for a document to open in a new
	// tab of the focused window.
	server *instance.Server
	// nativeTabs is whether the system gathers the windows into tabs, as
	// native_tabs said when the first opened.
	nativeTabs *bool
	// background is the documents plugins opened without a tab.
	background backgroundDocuments
}

// windowGroup returns the group a belongs to, a group of its own for an
// App made alone, as tests make them.
func (a *App) windowGroup() *windowGroup {
	if a.group == nil {
		a.group = &windowGroup{windows: []*App{a}, focused: a}
	}
	return a.group
}

// focus makes w the window the process acts on.
func (g *windowGroup) focus(w *App) {
	g.focused = w
	nativeApp = w
	if w.runtime != nil {
		w.runtime.AttachHost(w)
		w.runtime.SetActiveDocument(w.docPath, w.pageCount)
	}
	if w.remotePrompts != nil {
		remote.SetPrompter(w.askRemote)
	}
}

// windowFor is the window an event with id is for, or the focused window
// when it is for none of them.
func (g *windowGroup) windowFor(id sdl.WindowID) *App {
	if id != 0 {
		for _, w := range g.windows {
			if w.window != nil && sdl.GetWindowID(w.window) == id {
				return w
			}
		}
	}
	return g.focused
}

// eventWindowID is the window an event happened in, or 0 for an event of
// no window, such as a document the system sent.
func eventWindowID(event *sdl.Event) sdl.WindowID {
	switch t := event.Type(); {
	case t >= sdl.EventWindowFirst && t <= sdl.EventWindowLast:
		return event.Window().WindowID
	case t == sdl.EventKeyDown || t == sdl.EventKeyUp:
		return event.Key().WindowID
	case t == sdl.EventTextInput:
		return event.Text().WindowID
	case t == sdl.EventTextEditing:
		return event.Edit().WindowID
	case t == sdl.EventMouseMotion:
		return event.Motion().WindowID
	case t == sdl.EventMouseButtonDown || t == sdl.EventMouseButtonUp:
		return event.Button().WindowID
	case t == sdl.EventMouseWheel:
		return event.Wheel().WindowID
	case t == sdl.EventFingerDown || t == sdl.EventFingerMotion || t == sdl.EventFingerUp || t == sdl.EventFingerCanceled:
		return event.TFinger().WindowID
	case t == sdl.EventDropFile || t == sdl.EventDropText || t == sdl.EventDropBegin || t == sdl.EventDropComplete || t == sdl.EventDropPosition:
		return event.Drop().WindowID
	case t >= sdl.EventPinchBegin && t <= sdl.EventPinchEnd:
		// SDL_PinchFingerEvent: the common header, the scale, then the window.
		const offset = 16 + 4
		return sdl.WindowID(binary.NativeEndian.Uint32(event[offset:]))
	}
	return 0
}

// handleEvent hands an SDL event to the window it is for, or to every
// window when it is about the system rather than one window.
func (g *windowGroup) handleEvent(event *sdl.Event) error {
	if g.waker != nil && event.Type() == g.waker.eventType {
		g.waker.queued.Store(false)
		return nil // the loop polls the workers' channels after events
	}
	switch event.Type() {
	case sdl.EventQuit:
		g.quitAll("closing")
		return nil
	case sdl.EventSystemThemeChanged, sdl.EventDisplayCurrentModeChanged:
		for _, w := range g.windows {
			if err := w.handleSDLEvent(event); err != nil {
				return err
			}
		}
		return nil
	}
	w := g.windowFor(eventWindowID(event))
	if w == nil {
		return nil
	}
	if event.Type() == sdl.EventWindowFocusGained {
		if previous := g.focused; w != previous {
			g.focus(w)
			if w.nativeTabsOn() && previous != nil && previous.window != nil && sameNativeTabGroup(previous.window, w.window) {
				defer w.emitTabChanged(slices.IndexFunc(w.userTabs(), func(u userTab) bool { return u.w == previous }))
			}
		}
		w.loadPendingTab() // a restored tab, shown at last
	}
	return w.handleSDLEvent(event)
}

// run is the event loop, until every window has closed.
func (g *windowGroup) run() error {
	defer g.closeAll()
	for !g.allQuit() {
		var event sdl.Event
		for sdl.PollEvent(&event) {
			if err := g.handleEvent(&event); err != nil {
				return err
			}
		}
		if f := g.focused; f != nil && f.runtime != nil && f.runtime.PollPluginOperations() {
			f.applyRuntimeChanges("plugin operation")
			g.redrawAll()
		}
		if f := g.focused; f != nil && f.pollSystemActions() {
			f.pendingRedraw = true
		}
		for _, w := range slices.Clone(g.windows) {
			if err := w.tick(); err != nil {
				return err
			}
		}
		g.closeQuitWindows()
		if g.allQuit() {
			break
		}
		wait := g.windows[0].eventWaitTimeoutMS()
		for _, w := range g.windows[1:] {
			wait = min(wait, w.eventWaitTimeoutMS())
		}
		if sdl.WaitEventTimeout(&event, int32(wait)) {
			if err := g.handleEvent(&event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *windowGroup) redrawAll() {
	for _, w := range g.windows {
		w.pendingRedraw = true
	}
}

// allQuit reports whether every window has asked to close, which ends the
// loop and leaves them to Close.
func (g *windowGroup) allQuit() bool {
	for _, w := range g.windows {
		if !w.quit {
			return false
		}
	}
	return true
}

// closeQuitWindows closes the windows that asked to while others stay.
func (g *windowGroup) closeQuitWindows() {
	if g.allQuit() {
		return
	}
	for _, w := range slices.Clone(g.windows) {
		if !w.quit {
			continue
		}
		var peer *App // a tab of the same tab group, which tells plugins
		index := -1
		if w.nativeTabsOn() {
			index = w.userTabIndex()
			for _, other := range w.nativeTabApps() {
				if other != w && !other.quit {
					peer = other
					break
				}
			}
		}
		g.windows = slices.DeleteFunc(g.windows, func(other *App) bool { return other == w })
		w.closeWindow()
		if peer != nil {
			defer peer.emitTabListChanged("closed", index)
		}
		if g.focused == w {
			g.focus(g.windows[len(g.windows)-1])
		}
	}
}

// closeAll ends the loop's work on the windows still open at exit.
func (g *windowGroup) closeAll() {
	for _, w := range g.windows {
		w.stopTextInput()
		w.cancelSmoothScroll()
		w.cancelSmoothZoom()
	}
}

// quitAll quits the process once no tab of any window has unsaved edits,
// or on the second attempt after a warning, as confirmQuit does for one.
func (g *windowGroup) quitAll(action string) {
	f := g.focused
	if f == nil {
		return
	}
	var tabs []*tab
	for _, w := range g.windows {
		tabs = append(tabs, w.tabList()...)
	}
	if !f.confirmTabs(tabs, action) {
		f.pendingRedraw = true
		return
	}
	for _, w := range g.windows {
		w.quit = true
	}
}

// documentsOpen counts the documents open in every window of the process.
func (g *windowGroup) documentsOpen() int {
	n := 0
	for _, w := range g.windows {
		n += len(w.tabList())
	}
	return n
}

// newWindow opens path, at page when it is above 0, or nothing when path
// is empty, in a new window of this process, sharing from's runtime.
func (g *windowGroup) newWindow(from *App, path string, page int) (*App, error) {
	w := newApp(from.runtime, from.iconBytes, NewOptions{Verbose: from.verbose, LaunchArgs: from.launchArgs})
	w.group = g
	w.singleInstance = from.singleInstance
	w.initialDocPath = path
	if page > 0 {
		w.initialStartPage, w.initialPageSet = page-1, true
	}
	g.windows = append(g.windows, w)
	if err := w.openWindow(); err != nil {
		g.windows = slices.DeleteFunc(g.windows, func(other *App) bool { return other == w })
		g.focus(from)
		return nil, err
	}
	return w, nil
}
