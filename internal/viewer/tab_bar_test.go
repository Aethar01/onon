package viewer

import (
	"testing"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// twoTabs is a window with two tabs and the UI font, its status bar shown.
func twoTabs(t *testing.T) *App {
	t.Helper()
	app := tabsApp(t, testPages(t, 2))
	if app.fontFace == nil {
		t.Skip("no UI font")
	}
	if err := app.OpenTab(testPages(t, 2)); err != nil {
		t.Fatal(err)
	}
	app.statusBarShown = true
	return app
}

// setOption sets an option as :set does.
func setOption(t *testing.T, app *App, name, value string) {
	t.Helper()
	if err := app.runtime.SetOption(name, value); err != nil {
		t.Fatal(err)
	}
	app.applyRuntimeChanges("set")
}

func TestTabBarAtTheSideAndTheStatusBarTakeTurnsByOrder(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_bar_position", "left")
	l := app.layoutEdges()
	if l.tabBar.X != 0 || l.tabBar.W <= 0 || l.doc.X != l.tabBar.W {
		t.Fatalf("bar %v, document %v: want the bar at the left and the document beside it", l.tabBar, l.doc)
	}
	// The status bar comes first by default, spanning the window below.
	if l.status.X != 0 || int(l.status.W) != app.winW || l.tabBar.Y+l.tabBar.H != l.status.Y {
		t.Fatalf("status %v, bar %v: want the status bar across the window, the bar above it", l.status, l.tabBar)
	}
	setOption(t, app, "tab_bar_order", "9")
	l = app.layoutEdges()
	if int(l.tabBar.H) != app.winH || l.status.X != l.tabBar.W {
		t.Fatalf("status %v, bar %v: want the bar the window's height, the status bar beside it", l.status, l.tabBar)
	}
	if w, _ := app.viewportSize(); w != int(l.doc.W) {
		t.Fatalf("viewport %d wide, want the document's %v", w, l.doc.W)
	}
}

func TestTabBarAtTheBottomSitsAboveTheStatusBar(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_bar_position", "bottom")
	l := app.layoutEdges()
	if l.tabBar.Y+l.tabBar.H != l.status.Y || l.doc.Y != 0 || l.doc.Y+l.doc.H != l.tabBar.Y {
		t.Fatalf("doc %v, bar %v, status %v: want the bar between the document and the status bar", l.doc, l.tabBar, l.status)
	}
	setOption(t, app, "tab_bar_order", "0")
	l = app.layoutEdges()
	if int(l.tabBar.Y+l.tabBar.H) != app.winH || l.status.Y+l.status.H != l.tabBar.Y {
		t.Fatalf("bar %v, status %v: want the bar below the status bar", l.tabBar, l.status)
	}
}

func TestTabBarVisible(t *testing.T) {
	app := tabsApp(t, testPages(t, 1))
	if app.fontFace == nil {
		t.Skip("no UI font")
	}
	if app.tabBarShown() {
		t.Fatal("a bar for one tab by default")
	}
	setOption(t, app, "tab_bar_visible", "always")
	if !app.tabBarShown() || app.tabBarThick() == 0 {
		t.Fatal("no bar for one tab with tab_bar_visible = always")
	}
	if err := app.OpenTab(testPages(t, 1)); err != nil {
		t.Fatal(err)
	}
	setOption(t, app, "tab_bar_visible", "false")
	if app.tabBarShown() || app.tabBarThick() != 0 {
		t.Fatal("a bar for two tabs with tab_bar_visible = never")
	}
}

func TestFillingTabsShareTheBar(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_width", "fill")
	l := app.tabBarLayout()
	a, b := l.tabs[0], l.tabs[1]
	if a.W != b.W || b.X+b.W < l.area.X+l.area.W-0.5 {
		t.Fatalf("tabs %v and %v in %v: want two equal tabs filling it", a, b, l.area)
	}
	setOption(t, app, "tab_width", "fit")
	l = app.tabBarLayout()
	if r := l.tabs[1]; r.X+r.W > l.area.X+l.area.W/2 {
		t.Fatalf("fitted tabs reach %v in %v, want them as wide as their names", r.X+r.W, l.area)
	}
}

func TestTabsAtTheSideStackAndScroll(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_bar_position", "right")
	for range 40 {
		if err := app.OpenTab(testPages(t, 1)); err != nil {
			t.Fatal(err)
		}
	}
	l := app.tabBarLayout()
	if l.bar.X+l.bar.W != float32(app.winW) {
		t.Fatalf("bar %v, want it at the right", l.bar)
	}
	shown := l.tabs[app.tabIndex()]
	if shown.Y < l.area.Y || shown.Y+shown.H > l.area.Y+l.area.H+0.5 || l.tabs[0].X != shown.X {
		t.Fatalf("shown tab %v outside the area %v, or not under the others", shown, l.area)
	}
	e := sdl.MouseWheelEvent{MouseX: l.bar.X + 5, MouseY: l.bar.Y + l.bar.H/2, Y: 1000}
	if !app.handleTabBarWheel(&e) || app.tabBarLayout().scroll != 0 {
		t.Fatal("the wheel over the bar did not scroll it to the first tab")
	}
}

func TestDraggingATabMovesIt(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_width", "fill")
	first := app.tabList()[0]
	l := app.tabBarLayout()
	a, b := l.tabs[0], l.tabs[1]
	y := a.Y + a.H/2
	button := func(kind sdl.EventType, x float32) {
		e := sdl.MouseButtonEvent{Type: kind, Button: uint8(sdl.ButtonLeft), X: x, Y: y, Clicks: 1}
		if !app.handleTabBarButton(&e) {
			t.Fatal("the tab bar did not take the button")
		}
	}
	button(sdl.EventMouseButtonDown, a.X+10)
	if !app.handleTabBarMotion(b.X+b.W*0.75, y) {
		t.Fatal("the bar did not take the drag")
	}
	button(sdl.EventMouseButtonUp, b.X+b.W*0.75)
	if app.tabList()[1] != first || app.tab != first {
		t.Fatalf("dragging the first tab past the second left %s", tabNames(app))
	}

	setOption(t, app, "tab_drag", "false")
	button(sdl.EventMouseButtonDown, b.X+10)
	if app.handleTabBarMotion(a.X+10, y) {
		t.Fatal("a tab dragged with tab_drag off")
	}
	button(sdl.EventMouseButtonUp, a.X+10)
	if app.tabList()[1] != first {
		t.Fatalf("tab_drag off still moved tabs: %s", tabNames(app))
	}
}

func TestDraggingTheSideBarsEdgeResizesIt(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_bar_position", "left")
	bar := app.layoutEdges().tabBar
	edge, y := bar.X+bar.W, bar.Y+bar.H/2
	if !app.tabBarEdgeAt(edge, y) {
		t.Fatal("no edge to drag at the bar's inner side")
	}
	e := sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: edge, Y: y}
	app.handleTabBarButton(&e)
	app.handleTabBarMotion(edge+100, y)
	e.Type = sdl.EventMouseButtonUp
	app.handleTabBarButton(&e)
	if got := app.layoutEdges().tabBar.W; got != bar.W+100 {
		t.Fatalf("bar %v wide after dragging, want %v", got, bar.W+100)
	}
	setOption(t, app, "tab_bar_resizable", "false")
	if app.tabBarEdgeAt(edge+100, y) {
		t.Fatal("an edge to drag with tab_bar_resizable off")
	}
}

func TestTurnSidesFacesTheDocument(t *testing.T) {
	for side, want := range map[string]config.Sides{"top": config.SideBottom, "bottom": config.SideTop, "left": config.SideRight, "right": config.SideLeft} {
		if got := turnSides(config.SideBottom, side); got != want {
			t.Errorf("a bar at the %s turned bottom to %v, want %v", side, got, want)
		}
	}
}

func TestTabWidthSetsTabsAndTheSideBar(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_width", "150")
	l := app.tabBarLayout()
	if want := app.px(150); l.tabs[0].W != want || l.tabs[1].W != want {
		t.Fatalf("tabs %v and %v wide, want %v", l.tabs[0].W, l.tabs[1].W, want)
	}
	setOption(t, app, "tab_bar_position", "left")
	if got, want := app.layoutEdges().tabBar.W, app.px(150); got != want {
		t.Fatalf("side bar %v wide with tab_width = 150, want %v", got, want)
	}
	setOption(t, app, "tab_width", "fit")
	if got, want := app.layoutEdges().tabBar.W, app.px(app.style(config.ElementTab).MaxWidth.V); got != want {
		t.Fatalf("side bar %v wide with tab_width = fit, want max_width %v", got, want)
	}
	setOption(t, app, "tab_width", `"25%"`)
	if got := app.layoutEdges().tabBar.W; int(got) != app.winW/4 {
		t.Fatalf("side bar %v wide with tab_width = 25%%, want %d", got, app.winW/4)
	}
}

func TestTabsInTheTitleRowLeaveRoomToDragTheWindow(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_bar_position", "title")
	if !app.tabBarWantsTitleRow() {
		t.Skip("no title row of onon's here")
	}
	for range 30 {
		if err := app.OpenTab(testPages(t, 1)); err != nil {
			t.Fatal(err)
		}
	}
	l := app.tabBarLayout()
	if l.bar.Y != 0 || !app.tabBarInTitleRow() || l.area.Y > l.bar.Y+app.px(8) {
		t.Fatalf("bar %v, area %v: want the tabs in the title row", l.bar, l.area)
	}
	space := app.px(app.config.TabBarDragSpace)
	grab := l.area.X + l.area.W + space/2
	y := l.area.Y + l.area.H/2
	if app.titleRowTakes(grab, y, true) || !app.titleRowTakes(l.tabs[app.tabIndex()].X+2, y, false) {
		t.Fatal("the drag space takes presses, or the shown tab does not")
	}
	if l.area.X < app.px(windowButtonsLeading) {
		t.Fatalf("tabs start at %v, over the window's buttons", l.area.X)
	}
	setOption(t, app, "tab_bar_drag_space", "0")
	if wider := app.tabBarLayout().area.W; wider-l.area.W != space {
		t.Fatalf("no drag space widened the tabs by %v, want %v", wider-l.area.W, space)
	}
	setOption(t, app, "tab_bar_position", "top")
	if top := app.tabBarLayout(); app.tabBarInTitleRow() || top.area.Y < app.titleRowHeight() {
		t.Fatalf("tabs at %v with tab_bar_position = top, want them below the title row", top.area)
	}
}

func TestTabBarToggles(t *testing.T) {
	app := twoTabs(t)
	app.runCommand(":tabbar off")
	if app.tabBarShown() || app.tabBarThick() != 0 {
		t.Fatal(":tabbar off left the bar")
	}
	app.runAction("toggle_tab_bar")
	if !app.tabBarShown() {
		t.Fatal("toggle_tab_bar did not show the bar again")
	}
	app.runCommand(":tabclose")
	app.runCommand(":tabbar on")
	if !app.tabBarShown() {
		t.Fatal(":tabbar on hid the bar of one tab")
	}
	setOption(t, app, "tab_bar_visible", "never")
	if app.tabBarShown() {
		t.Fatal("tab_bar_visible changed, but the toggle still decides")
	}
}
