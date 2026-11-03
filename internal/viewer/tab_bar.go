package viewer

import (
	"math"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Tab widths in logical pixels: a tab is as wide as its name up to
// tabMaxWidth, and tabs shrink to share a narrow bar down to tabMinWidth,
// past which the bar scrolls to keep the tab shown in view.
const (
	tabMaxWidth = 220
	tabMinWidth = 90
)

// unsavedMark is drawn before the name of a tab with unsaved edits.
const unsavedMark = "• "

// tabBarShown reports whether the window draws its own tab bar: while it
// has more than one tab, and not in presentation mode.
func (a *App) tabBarShown() bool {
	return len(a.tabList()) > 1 && a.presentation == nil
}

// tabBarHeight is the height the tab bar takes from the top of the window,
// or 0 while it is hidden. Where onon draws the window's buttons, the bar
// shares their row, and is at least as tall.
func (a *App) tabBarHeight() int {
	if !a.tabBarShown() || a.fontFace == nil {
		return 0
	}
	barTop, _, barBottom, _ := a.insets(a.style(config.ElementTabBar).Padding.V)
	tabTop, _, tabBottom, _ := a.insets(a.style(config.ElementTab).Padding.V)
	h := a.uiLineHeight() + int(math.Round(float64(barTop+barBottom+tabTop+tabBottom)))
	if a.titleBarActive() {
		h = max(h, int(a.titleBarLayout().height))
	}
	return a.titleBand() + h
}

// titleBand is the height of the band the system keeps at the top of the
// window for its title bar, in render pixels, which the tab bar's tabs sit
// below.
func (a *App) titleBand() int {
	return int(math.Round(windowTitleBand * a.displayScale()))
}

// topInset is the height at the top of the window that the document is
// laid out below.
func (a *App) topInset() int {
	return a.tabBarHeight()
}

// tabBarLayout is where the tab bar and each tab are, in render pixels.
type tabBarLayout struct {
	bar  sdl.FRect
	tabs []sdl.FRect // by tab index, clipped to the bar by drawing
	area sdl.FRect   // the part of the bar the tabs scroll in
}

func (a *App) tabBarLayout() tabBarLayout {
	h := float32(a.tabBarHeight())
	l := tabBarLayout{bar: sdl.FRect{W: float32(a.winW), H: h}}
	if h == 0 {
		return l
	}
	bar := a.style(config.ElementTabBar)
	top, right, bottom, left := a.insets(bar.Padding.V)
	gap := a.px(bar.Gap.V)
	top += float32(a.titleBand())
	l.area = sdl.FRect{X: left, Y: top, W: float32(a.winW) - left - right, H: h - top - bottom}
	if a.titleBarActive() {
		// Leave the window's buttons their room at the right.
		first := a.titleBarLayout().buttonRect(titleButtonMinimize)
		l.area.W = min(l.area.W, first.X-l.area.X)
	}
	tabs := a.tabList()
	tab := a.style(config.ElementTab)
	_, padRight, _, padLeft := a.insets(tab.Padding.V)
	widths := make([]float32, len(tabs))
	total := gap * float32(len(tabs)-1)
	for i, t := range tabs {
		text := measureText(a.fontFace, a.tabLabel(t))
		widths[i] = min(float32(text)+padLeft+padRight, a.px(tabMaxWidth))
		total += widths[i]
	}
	if total > l.area.W {
		// Share the bar, as far as the narrowest a tab may be.
		share := max((l.area.W-gap*float32(len(tabs)-1))/float32(len(tabs)), a.px(tabMinWidth))
		total = gap * float32(len(tabs)-1)
		for i := range widths {
			widths[i] = min(widths[i], share)
			total += widths[i]
		}
	}
	x := l.area.X
	l.tabs = make([]sdl.FRect, len(tabs))
	for i := range tabs {
		l.tabs[i] = sdl.FRect{X: x, Y: l.area.Y, W: widths[i], H: l.area.H}
		x += widths[i] + gap
	}
	// Scroll the tabs so the one shown is in the bar.
	if total > l.area.W {
		shown := l.tabs[max(0, a.tabIndex())]
		shift := float32(0)
		if end := shown.X + shown.W; end > l.area.X+l.area.W {
			shift = end - (l.area.X + l.area.W)
		}
		for i := range l.tabs {
			l.tabs[i].X -= shift
		}
	}
	return l
}

// tabLabel is what a tab shows: its name, marked when it has unsaved
// edits.
func (a *App) tabLabel(t *tab) string {
	if t.unsaved {
		return unsavedMark + t.tabName()
	}
	return t.tabName()
}

// tabAt is the index of the tab at x, y in render pixels, or -1.
func (a *App) tabAt(x, y float64) int {
	l := a.tabBarLayout()
	if l.bar.H == 0 || y < float64(l.area.Y) || y >= float64(l.area.Y+l.area.H) ||
		x < float64(l.area.X) || x >= float64(l.area.X+l.area.W) {
		return -1
	}
	for i, r := range l.tabs {
		if x >= float64(r.X) && x < float64(r.X+r.W) {
			return i
		}
	}
	return -1
}

// overTabBar reports whether y, in render pixels, is in the tab bar.
func (a *App) overTabBar(y float64) bool {
	return y >= 0 && y < float64(a.tabBarHeight())
}

// drawTabBar draws the tab bar across the top of the window.
func (a *App) drawTabBar(renderer *sdl.Renderer) {
	l := a.tabBarLayout()
	if l.bar.H == 0 {
		return
	}
	bar := a.style(config.ElementTabBar)
	parts := boxBody
	if a.drawBoxParts(renderer, &bar, l.bar, parts) {
		defer a.drawBoxParts(renderer, &bar, l.bar, boxBorder)
	}
	clip := sdl.FRect{X: l.area.X, Y: 0, W: l.area.W, H: l.bar.H}
	_ = a.withClip(renderer, clip, func() error {
		baseline := int(l.area.Y) + a.statusBaselineOffset(int(l.area.H))
		for i, t := range a.tabList() {
			r := l.tabs[i]
			element := config.ElementTab
			if t == a.tab {
				element = config.ElementTabSelected
			}
			st := a.style(element)
			a.drawBox(renderer, &st, r)
			_, padRight, _, padLeft := a.insets(st.Padding.V)
			x := int(r.X + padLeft)
			room := int(r.W - padLeft - padRight)
			if t.unsaved {
				_ = a.drawTextFace(renderer, unsavedMark, x, baseline, a.textColor(&st, true), st.Bold.V)
				mark := measureText(a.fontFace, unsavedMark)
				x += mark
				room -= mark
			}
			name := truncateText(a.fontFace, t.tabName(), room)
			_ = a.drawTextFace(renderer, name, x, baseline, a.textColor(&st, false), st.Bold.V)
		}
		return nil
	})
}

// handleTabBarButton shows the tab clicked, or closes the one clicked with
// the middle button, reporting whether the press was the tab bar's.
func (a *App) handleTabBarButton(e *sdl.MouseButtonEvent) bool {
	if !a.overTabBar(float64(e.Y)) {
		return false
	}
	if e.Type != sdl.EventMouseButtonDown {
		return true
	}
	i := a.tabAt(float64(e.X), float64(e.Y))
	if i < 0 {
		return true
	}
	switch e.Button {
	case uint8(sdl.ButtonLeft):
		a.selectTab(i)
	case uint8(sdl.ButtonMiddle):
		a.closeTab(i, false, "closing it")
	}
	a.pendingRedraw = true
	return true
}
