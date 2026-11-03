package viewer

import (
	"math"
	"os"
	"slices"

	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// With native tabs, as on macOS, each tab is a window of its own, which the
// system gathers with others into one window's tabs. The tab commands and
// actions then act on the windows of the tab group, through userTabs, as
// they act on a window's own tabs elsewhere.

// userTab is a tab as the user sees it: one of the window's own, or with
// native tabs, a window of its tab group, which has one.
type userTab struct {
	w *App
	t *tab
}

// nativeTabsOn reports whether the system gathers this window into tabs.
func (a *App) nativeTabsOn() bool { return a.nativeTabbing }

// nativeTabApps returns the windows of this window's tab group, in the
// order of their tabs.
func (a *App) nativeTabApps() []*App {
	if a.window == nil {
		return []*App{a}
	}
	var apps []*App
	for _, native := range nativeTabWindows(a.window) {
		for _, w := range a.windowGroup().windows {
			if w.window != nil && cocoaWindow(w.window) == native {
				apps = append(apps, w)
			}
		}
	}
	if !slices.Contains(apps, a) {
		apps = append(apps, a)
	}
	return apps
}

// userTabs returns the tabs the user sees in this window, in order.
func (a *App) userTabs() []userTab {
	if !a.nativeTabsOn() {
		tabs := a.tabList()
		users := make([]userTab, len(tabs))
		for i, t := range tabs {
			users[i] = userTab{a, t}
		}
		return users
	}
	apps := a.nativeTabApps()
	users := make([]userTab, len(apps))
	for i, w := range apps {
		users[i] = userTab{w, w.tab}
	}
	return users
}

// userTabIndex is the index among userTabs of the tab shown.
func (a *App) userTabIndex() int {
	return slices.IndexFunc(a.userTabs(), func(u userTab) bool { return u.w == a && u.t == a.tab })
}

// showTab shows the tab at index i among userTabs.
func (a *App) showTab(i int) {
	tabs := a.userTabs()
	if i < 0 || i >= len(tabs) {
		return
	}
	u := tabs[i]
	if u.w != a {
		selectNativeTab(u.w.window)
		u.w.windowGroup().focus(u.w)
		u.w.loadPendingTab()
		return
	}
	a.selectTab(slices.Index(a.tabList(), u.t))
}

// openNativeTab opens path, or nothing when it is empty, in a new window
// added to this window's tabs, and returns it.
func (a *App) openNativeTab(path string, page int) (*App, error) {
	if path != "" && !remote.IsURI(path) {
		if _, err := os.Stat(path); err != nil {
			return nil, err // no window, rather than one showing the error
		}
	}
	w, err := a.windowGroup().newWindow(a, path, page)
	if err != nil {
		return nil, err
	}
	addNativeTab(a.window, w.window)
	return w, nil
}

// updateNativeInset keeps the document below the system's tab bar, which
// covers the top of the window while it shows.
func (a *App) updateNativeInset() {
	if !a.nativeTabsOn() || a.window == nil {
		return
	}
	density := float64(sdl.GetWindowPixelDensity(a.window))
	if density <= 0 {
		density = 1
	}
	inset := int(math.Round(nativeTabBarInset(a.window) * density))
	if inset != a.nativeInset {
		a.relayoutWithViewportAnchor(func() { a.nativeInset = inset })
		a.pendingRedraw = true
	}
}

// emitTabChanged tells plugins the window shows another tab, previous
// being the index among userTabs of the one shown before, or -1.
func (a *App) emitTabChanged(previous int) {
	if a.runtime == nil {
		return
	}
	payload := a.documentEventPayload()
	payload["index"] = a.userTabIndex() + 1
	payload["previous"] = previous + 1
	a.emitPluginEvent("tab_changed", payload)
}
