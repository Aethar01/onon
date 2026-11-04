package viewer

import (
	"math"
	"slices"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// onon draws its own tab bar wherever the system draws no tabs: along an
// edge of the window, as tab_bar_position says, among the other bars and
// docked views there as tab_bar_order says. Along the top or bottom tabs
// sit side by side; at the left or right, one under another.

// unsavedMark is drawn before the name of a tab with unsaved edits.
const unsavedMark = "• "

// tabBarState is the window's tab bar between frames.
type tabBarState struct {
	// shown is whether toggle_tab_bar showed or hid the bar, while
	// tab_bar_visible is still shownFrom.
	shown     *bool
	shownFrom string
	// scroll is how far the tabs are scrolled along the bar, and revealed
	// what the scroll last brought the shown tab into view for.
	scroll   float32
	revealed tabReveal
	// width is what the pointer dragged a bar at the side to, kept while
	// tab_width is still widthFrom.
	width     *config.UISize
	widthFrom config.TabWidth
	resizing  bool
	drag      tabDrag
}

type tabReveal struct {
	t    *tab
	n    int
	room float32
}

// tabDrag is a tab pressed, which moving the pointer drags along the bar,
// the other tabs making way.
type tabDrag struct {
	t      *tab    // nil when none is
	from   float32 // where along the bar it was pressed
	grab   float32 // how far into the tab
	at     float32 // where along the bar the pointer is
	moving bool    // the pointer has gone far enough to drag
}

// tabBarSide is the edge the tab bar is along, the title row's being the
// top.
func (a *App) tabBarSide() string {
	if p := a.config.TabBarPosition; p != "title" && slices.Contains(config.TabBarPositions, p) {
		return p
	}
	return "top"
}

// tabBarWantsTitleRow reports whether the tab bar is to share the row of
// the window's buttons: as tab_bar_position = "title" asks, where there is
// such a row, the system's title band on macOS or the buttons onon draws
// on Windows.
func (a *App) tabBarWantsTitleRow() bool {
	return a.config.TabBarPosition == "title" && (windowTitleBand > 0 || a.titleBarActive())
}

// tabBarVertical reports whether the tabs sit one under another, in a bar
// at the left or right.
func (a *App) tabBarVertical() bool {
	side := a.tabBarSide()
	return side == "left" || side == "right"
}

// tabBarShown reports whether the window draws its own tab bar: as
// tab_bar_visible says, by default while it has more than one tab, and
// not with the system's tabs or in presentation mode.
func (a *App) tabBarShown() bool {
	if a.nativeTabsOn() || a.presentation != nil || a.fontFace == nil {
		return false
	}
	if shown := a.tabBar.shown; shown != nil && a.tabBar.shownFrom == a.config.TabBarVisible {
		return *shown
	}
	switch a.config.TabBarVisible {
	case "never":
		return false
	case "always":
		return true
	}
	return len(a.tabList()) > 1
}

// tabRowHeight is the height of a tab: a line of text and its padding.
func (a *App) tabRowHeight() float32 {
	top, _, bottom, _ := a.insets(a.style(config.ElementTab).Padding.V)
	return float32(a.uiLineHeight()) + top + bottom
}

// tabBarThickness is how deep the tab bar is from its edge of rest, what
// the bars and views of lower order left.
func (a *App) tabBarThickness(rest sdl.FRect) float32 {
	margin := a.tabBarMargin()
	if a.tabBarVertical() {
		width := float32(math.Round(a.uiLength(a.tabBarWidth(), float64(a.winW))))
		width = min(max(width, a.px(60)), float32(math.Round(float64(rest.W)*maxDockShare)))
		return width + float32(margin.Left+margin.Right)
	}
	top, _, bottom, _ := a.insets(a.style(config.ElementTabBar).Padding.V)
	h := a.tabRowHeight() + top + bottom
	if a.tabBarSide() == "top" && rest.Y == 0 {
		if a.tabBarWantsTitleRow() {
			// In the row of the window's buttons, and centred on it.
			return float32(math.Round(float64(max(h, a.titleRowHeight()))))
		}
		h += a.titleRowHeight() // below it
	}
	return float32(math.Round(float64(h) + margin.Top + margin.Bottom))
}

// tabBarMargin is the tab bar's margin in render pixels, turned to its
// side as its border is: its bottom faces the document.
func (a *App) tabBarMargin() config.Insets {
	top, right, bottom, left := a.insets(a.style(config.ElementTabBar).Margin.V)
	m := config.Insets{Top: float64(top), Right: float64(right), Bottom: float64(bottom), Left: float64(left)}
	switch a.tabBarSide() {
	case "bottom":
		m.Top, m.Bottom = m.Bottom, m.Top
	case "left":
		m = config.Insets{Left: m.Top, Right: m.Bottom, Top: m.Left, Bottom: m.Right}
	case "right":
		m = config.Insets{Right: m.Top, Left: m.Bottom, Top: m.Left, Bottom: m.Right}
	}
	return m
}

// tabBarFloats reports whether the tab bar floats over the page.
func (a *App) tabBarFloats() bool { return a.style(config.ElementTabBar).Floating.V }

// titleRowHeight is the height of the row of the window's buttons along
// its top: the system's title band, or the title bar onon draws.
func (a *App) titleRowHeight() float32 {
	h := float32(a.titleBand())
	if a.titleBarActive() {
		h = max(h, float32(a.titleBarLayout().height))
	}
	return h
}

// tabBarWidth is the width of a bar at the side, as wide as its tabs: as
// tab_width says, the tab element's max_width for fit and fill, or as the
// pointer dragged it to since.
func (a *App) tabBarWidth() config.UISize {
	if w := a.tabBar.width; w != nil && a.tabBar.widthFrom == a.config.TabWidth {
		return *w
	}
	if w := a.config.TabWidth; w.Mode == "" && w.Size.Value > 0 {
		return w.Size
	}
	if most := a.style(config.ElementTab).MaxWidth.V; most > 0 {
		return config.UISize{Value: most}
	}
	return config.UISize{Value: 200}
}

// titleBand is the height of the band the system keeps at the top of the
// window for its title bar, in render pixels, which the tabs sit below.
func (a *App) titleBand() int {
	return int(math.Round(windowTitleBand * a.displayScale()))
}

// tabBarBand is the band at the top of bar the tabs keep clear of the
// row of the window's buttons, when the bar reaches the window's top and
// is not in that row.
func (a *App) tabBarBand(bar sdl.FRect) float32 {
	if bar.Y > 0 || a.tabBarInTitleRow() {
		return 0
	}
	return a.titleRowHeight()
}

// tabBarInTitleRow reports whether the tab bar is in the row of the
// window's buttons, as tab_bar_position = "title" asks, which it is while
// nothing of lower order is along the window's top.
func (a *App) tabBarInTitleRow() bool {
	if !a.tabBarWantsTitleRow() || !a.tabBarShown() || a.tabBarSide() != "top" {
		return false
	}
	bar := a.layoutEdges().tabBar
	return bar.H > 0 && bar.Y == 0
}

// tabBarSharesTitleRow reports whether the tab bar is in the row of the
// window's buttons onon draws, which then stay shown.
func (a *App) tabBarSharesTitleRow() bool {
	return a.titleBarActive() && a.tabBarInTitleRow()
}

// tabBarLayout is where the tab bar and each tab are, in render pixels.
type tabBarLayout struct {
	bar      sdl.FRect
	area     sdl.FRect   // the part of the bar the tabs scroll in
	tabs     []sdl.FRect // by tab index, clipped to the area by drawing
	vertical bool
	total    float32 // the tabs' length along the bar, gaps and all
	scroll   float32
}

// along is the start and length of r along the bar.
func (l tabBarLayout) along(r sdl.FRect) (float32, float32) {
	if l.vertical {
		return r.Y, r.H
	}
	return r.X, r.W
}

// at is where along the bar x, y is.
func (l tabBarLayout) at(x, y float32) float32 {
	if l.vertical {
		return y
	}
	return x
}

// moved is r moved to start along the bar.
func (l tabBarLayout) moved(r sdl.FRect, start float32) sdl.FRect {
	if l.vertical {
		r.Y = start
	} else {
		r.X = start
	}
	return r
}

// room is the length of the area the tabs scroll in.
func (l tabBarLayout) room() float32 {
	_, n := l.along(l.area)
	return n
}

func (a *App) tabBarLayout() tabBarLayout {
	strip := a.layoutEdges().tabBar
	l := tabBarLayout{bar: strip, vertical: a.tabBarVertical()}
	tabs := a.tabList()
	if strip.W == 0 || strip.H == 0 || len(tabs) == 0 {
		return l
	}
	barStyle := a.style(config.ElementTabBar)
	top, right, bottom, left := a.insets(barStyle.Padding.V)
	gap := a.px(barStyle.Gap.V)
	inTitleRow := a.tabBarInTitleRow()
	// The bar is its strip less its margin and, for a floating one, the
	// band kept clear of the window's title row; a bar that is not
	// floating fills that band, its tabs below it.
	band := a.tabBarBand(strip)
	m := a.tabBarMargin()
	if inTitleRow {
		m.Top, m.Bottom = 0, 0
	}
	bar := sdl.FRect{X: strip.X + float32(m.Left), Y: strip.Y + float32(m.Top), W: max(strip.W-float32(m.Left+m.Right), 0), H: max(strip.H-float32(m.Top+m.Bottom), 0)}
	floats := a.tabBarFloats()
	if floats {
		bar.Y += band
		bar.H = max(bar.H-band, 0)
		band = 0
	}
	l.bar = bar
	l.area = sdl.FRect{X: bar.X + left, Y: bar.Y + band + top, W: max(bar.W-left-right, 0), H: max(bar.H-band-top-bottom, 0)}
	if inTitleRow && floats {
		// Pills centred on the row of the window's buttons.
		h := a.tabRowHeight()
		l.area.Y, l.area.H = strip.Y+max((a.titleRowHeight()-h)/2, 0), h
	}
	if inTitleRow {
		// Leave the window's buttons their room, and room to drag the
		// window by however many tabs there are.
		lead := a.px(windowButtonsLeading)
		l.area.X += lead
		l.area.W -= lead
		if a.titleBarActive() {
			first := a.titleBarLayout().buttonRect(titleButtonMinimize)
			l.area.W = min(l.area.W, first.X-l.area.X)
		}
		l.area.W = max(l.area.W-a.px(float64(a.config.TabBarDragSpace)), 0)
	}
	l.tabs = make([]sdl.FRect, len(tabs))
	n := float32(len(tabs))
	if l.vertical {
		h := a.tabRowHeight()
		for i := range tabs {
			l.tabs[i] = sdl.FRect{X: l.area.X, Y: l.area.Y + float32(i)*(h+gap), W: l.area.W, H: h}
		}
		l.total = n*h + gap*(n-1)
	} else {
		widths := a.tabWidths(tabs, l.area.W, gap)
		x := l.area.X
		for i, w := range widths {
			l.tabs[i] = sdl.FRect{X: x, Y: l.area.Y, W: w, H: l.area.H}
			x += w + gap
		}
		l.total = x - gap - l.area.X
	}
	a.revealShownTab(&l)
	l.scroll = min(max(a.tabBar.scroll, 0), max(l.total-l.room(), 0))
	for i, r := range l.tabs {
		start, _ := l.along(r)
		l.tabs[i] = l.moved(r, start-l.scroll)
	}
	return l
}

// tabWidths is how wide each tab is in a bar at the top or bottom whose
// tabs have room across, as tab_width says.
func (a *App) tabWidths(tabs []*tab, room, gap float32) []float32 {
	st := a.style(config.ElementTab)
	_, padRight, _, padLeft := a.insets(st.Padding.V)
	least, most := a.px(st.MinWidth.V), a.px(st.MaxWidth.V)
	share := (room - gap*float32(len(tabs)-1)) / float32(len(tabs))
	widths := make([]float32, len(tabs))
	width := a.config.TabWidth
	if width.Mode == "fill" {
		for i := range widths {
			widths[i] = max(share, least)
		}
		return widths
	}
	total := gap * float32(len(tabs)-1)
	for i, t := range tabs {
		w := float32(measureText(a.fontFace, a.tabLabel(t))) + padLeft + padRight
		if width.Mode == "" && width.Size.Value > 0 {
			w = float32(math.Round(a.uiLength(width.Size, float64(a.winW))))
		}
		if most > 0 {
			w = min(w, most)
		}
		widths[i] = max(w, least)
		total += widths[i]
	}
	if total > room {
		// Share the bar, as far as the narrowest a tab may be.
		for i := range widths {
			widths[i] = min(widths[i], max(share, least))
		}
	}
	return widths
}

// revealShownTab scrolls the tabs so the one shown is in view, when it is
// another tab than last time, or the bar has changed.
func (a *App) revealShownTab(l *tabBarLayout) {
	i := a.tabIndex()
	reveal := tabReveal{t: a.tab, n: len(l.tabs), room: l.room()}
	if i < 0 || a.tabBar.revealed == reveal {
		return
	}
	a.tabBar.revealed = reveal
	start, length := l.along(l.tabs[i])
	areaStart, _ := l.along(l.area)
	start -= areaStart // unscrolled, from the area's start
	scroll := a.tabBar.scroll
	if start < scroll {
		scroll = start
	}
	if end := start + length; end > scroll+l.room() {
		scroll = end - l.room()
	}
	a.tabBar.scroll = scroll
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
	p := sdl.FPoint{X: float32(x), Y: float32(y)}
	if !pointIn(p, l.area) {
		return -1
	}
	for i, r := range l.tabs {
		if pointIn(p, r) {
			return i
		}
	}
	return -1
}

func pointIn(p sdl.FPoint, r sdl.FRect) bool {
	return p.X >= r.X && p.X < r.X+r.W && p.Y >= r.Y && p.Y < r.Y+r.H
}

// overTabBar reports whether x, y, in render pixels, is in the tab bar:
// for one floating over the page, on a tab, the page showing between.
func (a *App) overTabBar(x, y float32) bool {
	if !a.tabBarShown() {
		return false
	}
	if a.tabBarFloats() {
		return a.tabAt(float64(x), float64(y)) >= 0
	}
	return pointIn(sdl.FPoint{X: x, Y: y}, a.layoutEdges().tabBar)
}

// tabBarEdgeAt reports whether x, y is at the inner edge of a bar at the
// side that the pointer may drag to resize it.
func (a *App) tabBarEdgeAt(x, y float32) bool {
	if !a.config.TabBarResizable || !a.tabBarVertical() || !a.tabBarShown() {
		return false
	}
	bar := a.layoutEdges().tabBar
	edge := bar.X + bar.W
	if a.tabBarSide() == "right" {
		edge = bar.X
	}
	return abs32(x-edge) <= a.px(4) && y >= bar.Y && y <= bar.Y+bar.H
}

// turnSides turns the sides of a box styled for a bar at the top to the
// bar's side, its bottom facing the document wherever the bar is.
func turnSides(s config.Sides, side string) config.Sides {
	if side == "top" {
		return s
	}
	var turned config.Sides
	for _, pair := range [4][2]config.Sides{
		{config.SideTop, map[string]config.Sides{"bottom": config.SideBottom, "left": config.SideLeft, "right": config.SideRight}[side]},
		{config.SideBottom, map[string]config.Sides{"bottom": config.SideTop, "left": config.SideRight, "right": config.SideLeft}[side]},
		{config.SideLeft, map[string]config.Sides{"bottom": config.SideLeft, "left": config.SideTop, "right": config.SideTop}[side]},
		{config.SideRight, map[string]config.Sides{"bottom": config.SideRight, "left": config.SideBottom, "right": config.SideBottom}[side]},
	} {
		if s&pair[0] != 0 {
			turned |= pair[1]
		}
	}
	return turned
}

// barStyle is element's style, its sides turned to the bar's side.
func (a *App) barStyle(element config.Element) config.Style {
	st := a.style(element)
	if side := a.tabBarSide(); side != "top" {
		st.BorderSides.V = turnSides(st.BorderSides.V, side)
	}
	return st
}

// drawTabBar draws the tab bar, a tab being dragged over the others.
func (a *App) drawTabBar(renderer *sdl.Renderer) {
	if !a.tabBarShown() {
		return
	}
	l := a.tabBarLayout()
	if l.bar.W == 0 || l.bar.H == 0 {
		return
	}
	bar := a.barStyle(config.ElementTabBar)
	if a.drawBoxParts(renderer, &bar, l.bar, boxBody) {
		defer a.drawBoxParts(renderer, &bar, l.bar, boxBorder)
	}
	// Tabs are clipped along the bar only, so the shadows of pills show.
	clip := sdl.FRect{X: l.area.X, W: l.area.W, H: float32(a.winH)}
	if l.vertical {
		clip = sdl.FRect{Y: l.area.Y, W: float32(a.winW), H: l.area.H}
	}
	_ = a.withClip(renderer, clip, func() error {
		dragged := -1
		for i, t := range a.tabList() {
			if a.tabBar.drag.moving && t == a.tabBar.drag.t {
				dragged = i
				continue
			}
			a.drawTab(renderer, t, l.tabs[i])
		}
		if dragged >= 0 {
			a.drawTab(renderer, a.tabList()[dragged], a.draggedTabRect(l, dragged))
		}
		return nil
	})
}

// draggedTabRect is where the tab at i is while the pointer drags it: at
// the pointer, kept in the bar.
func (a *App) draggedTabRect(l tabBarLayout, i int) sdl.FRect {
	r := l.tabs[i]
	_, length := l.along(r)
	areaStart, room := l.along(l.area)
	start := a.tabBar.drag.at - a.tabBar.drag.grab
	return l.moved(r, min(max(start, areaStart), areaStart+max(room-length, 0)))
}

func (a *App) drawTab(renderer *sdl.Renderer, t *tab, r sdl.FRect) {
	element := config.ElementTab
	if t == a.tab {
		element = config.ElementTabSelected
	}
	st := a.barStyle(element)
	a.drawBox(renderer, &st, r)
	_, padRight, _, padLeft := a.insets(st.Padding.V)
	x := int(r.X + padLeft)
	room := int(r.W - padLeft - padRight)
	baseline := int(r.Y) + a.statusBaselineOffset(int(r.H))
	if t.unsaved {
		_ = a.drawTextFace(renderer, unsavedMark, x, baseline, a.textColor(&st, true), st.Bold.V)
		mark := measureText(a.fontFace, unsavedMark)
		x += mark
		room -= mark
	}
	name := truncateText(a.fontFace, t.tabName(), room)
	_ = a.drawTextFace(renderer, name, x, baseline, a.textColor(&st, false), st.Bold.V)
}

// handleTabBarButton takes a press in the tab bar: the left button shows
// the tab and, with tab_drag, starts dragging it; the middle button closes
// it; a double click on the bar between tabs opens a new one; and at the
// inner edge of a bar at the side, the left button resizes the bar. It
// reports whether the press was the tab bar's.
func (a *App) handleTabBarButton(e *sdl.MouseButtonEvent) bool {
	s := &a.tabBar
	if e.Type == sdl.EventMouseButtonUp {
		if e.Button == uint8(sdl.ButtonLeft) && (s.resizing || s.drag.t != nil) {
			s.resizing, s.drag = false, tabDrag{}
			a.pendingRedraw = true
			a.updateCursor()
			return true
		}
		return a.overTabBar(e.X, e.Y)
	}
	if e.Button == uint8(sdl.ButtonLeft) && a.tabBarEdgeAt(e.X, e.Y) {
		s.resizing = true
		return true
	}
	if !a.overTabBar(e.X, e.Y) {
		return false
	}
	i := a.tabAt(float64(e.X), float64(e.Y))
	switch {
	case i < 0:
		if e.Button == uint8(sdl.ButtonLeft) && e.Clicks == 2 {
			a.newEmptyTab()
		}
	case e.Button == uint8(sdl.ButtonLeft):
		a.selectTab(i)
		if a.config.TabDrag {
			l := a.tabBarLayout()
			at := l.at(e.X, e.Y)
			start, _ := l.along(l.tabs[i])
			s.drag = tabDrag{t: a.tab, from: at, grab: at - start, at: at}
		}
	case e.Button == uint8(sdl.ButtonMiddle):
		a.closeTab(i, false, "closing it")
	}
	a.pendingRedraw = true
	return true
}

// handleTabBarMotion resizes the bar or drags a tab as the pointer moves
// to x, y, reporting whether it did either.
func (a *App) handleTabBarMotion(x, y float32) bool {
	s := &a.tabBar
	if s.resizing {
		a.resizeTabBar(x)
		return true
	}
	d := &s.drag
	if d.t == nil {
		return false
	}
	if d.t != a.tab || !a.tabBarShown() {
		*d = tabDrag{}
		return false
	}
	l := a.tabBarLayout()
	d.at = l.at(x, y)
	if !d.moving && abs32(d.at-d.from) < a.px(4) {
		return true
	}
	d.moving = true
	i := a.tabIndex()
	_, length := l.along(l.tabs[i])
	centre := d.at - d.grab + length/2 // where the pointer holds it, past the bar's ends too
	to := i                            // the farthest tab whose middle the dragged one's has passed
	for j, r := range l.tabs {
		otherStart, otherLength := l.along(r)
		switch mid := otherStart + otherLength/2; {
		case j > i && centre > mid:
			to = max(to, j)
		case j < i && centre < mid:
			to = min(to, j)
		}
	}
	if to != i {
		a.moveTab(to)
	}
	a.pendingRedraw = true
	return true
}

// resizeTabBar drags the inner edge of a bar at the side to x.
func (a *App) resizeTabBar(x float32) {
	bar := a.layoutEdges().tabBar
	width := x - bar.X
	if a.tabBarSide() == "right" {
		width = bar.X + bar.W - x
	}
	size := config.UISize{Value: float64(max(width, a.px(60))) / float64(a.px(1))}
	a.relayoutWithViewportAnchor(func() {
		a.tabBar.width, a.tabBar.widthFrom = &size, a.config.TabWidth
		a.views.layoutGen++ // docked views sit beside it
	})
	a.pendingRedraw = true
}

// handleTabBarWheel scrolls the tabs with a wheel over the bar, reporting
// whether it was.
func (a *App) handleTabBarWheel(e *sdl.MouseWheelEvent) bool {
	if !a.overTabBar(e.MouseX, e.MouseY) {
		return false
	}
	dx, dy := e.X, e.Y
	if e.Direction == sdl.MouseWheelFlipped {
		dx, dy = -dx, -dy
	}
	l := a.tabBarLayout()
	amount := -dy
	if !l.vertical {
		amount += dx
	}
	step := a.tabRowHeight()
	a.tabBar.scroll = min(max(l.scroll+amount*step, 0), max(l.total-l.room(), 0))
	a.pendingRedraw = true
	return true
}

// toggleTabBar shows or hides the tab bar of this window: onon's, or with
// native tabs the system's.
func (a *App) toggleTabBar() {
	if a.nativeTabsOn() {
		toggleNativeTabBar(a.window)
		return
	}
	shown := !a.tabBarShown()
	a.relayoutWithViewportAnchor(func() {
		a.tabBar.shown, a.tabBar.shownFrom = &shown, a.config.TabBarVisible
		a.views.layoutGen++ // docked views sit beside it
	})
	a.pendingRedraw = true
}

// tabBarOn reports whether the window shows a tab bar, its own or the
// system's.
func (a *App) tabBarOn() bool {
	if a.nativeTabsOn() {
		return nativeTabBarVisible(a.window)
	}
	return a.tabBarShown()
}

// titleRowTakes reports whether the press at x, y, in render pixels, in
// the row of the window's buttons is the tab bar's: over a tab, or with
// anywhere, anywhere the tabs scroll, rather than room to drag the window.
func (a *App) titleRowTakes(x, y float32, anywhere bool) bool {
	if !a.tabBarInTitleRow() {
		return false
	}
	if anywhere {
		return pointIn(sdl.FPoint{X: x, Y: y}, a.tabBarLayout().area)
	}
	return a.tabAt(float64(x), float64(y)) >= 0
}
