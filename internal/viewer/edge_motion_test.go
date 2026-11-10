package viewer

import (
	"testing"
	"time"

	"onon/internal/config"
)

// slowEdges turns motion.edges on at 100ms linear, as if the window had
// been drawn. It goes after setOption, which applies the theme again.
func slowEdges(app *App) {
	app.config.Theme.Motion.Scale = 1
	slide := config.Animation{Duration: 100, Easing: config.Easing{Name: "linear", Curve: [4]float64{0, 0, 1, 1}}, Tracks: []config.Track{{Prop: "slide", Auto: true}}}
	app.config.Theme.Motion.Moves[config.MotionEdges][config.MomentEnter] = slide
	app.config.Theme.Motion.Moves[config.MotionEdges][config.MomentExit] = slide
	app.motion.osReduced.Store(false)
	app.motion.frame = 1
}

// movingEdges is twoTabs with motion.edges on.
func movingEdges(t *testing.T) *App {
	t.Helper()
	app := twoTabs(t)
	slowEdges(app)
	return app
}

// zoomedIn is twoTabs at twice the zoom, scrolled to scrollY.
func zoomedIn(t *testing.T, scrollY float64) *App {
	t.Helper()
	app := twoTabs(t)
	setOption(t, app, "fit_mode", "manual")
	app.relayoutWithViewportAnchor(func() { app.zoom = 2 })
	app.scrollY = scrollY
	app.relayoutWithViewportAnchor(nil)
	return app
}

func stepEdgesAt(app *App, at time.Time) {
	app.motion.now = at
	app.motion.animating = false
	app.stepEdgeMotion()
}

func toggleStatusBar(app *App) {
	app.relayoutWithViewportAnchor(func() { app.statusBarShown = !app.statusBarShown })
}

// pageTop is where the top of page is in the window.
func pageTop(app *App, page int) float32 {
	_, y, _ := app.pageScreenOrigin(page)
	return app.documentRect().Y + float32(y)
}

func TestBarsShownOrHiddenLeaveThePageWhereItIs(t *testing.T) {
	for _, toggle := range []struct {
		name string
		run  func(*App)
	}{{"status bar", toggleStatusBar}, {"tab bar", (*App).toggleTabBar}} {
		app := zoomedIn(t, 600)
		top := pageTop(app, 1)
		toggle.run(app)
		if got := pageTop(app, 1); got != top {
			t.Fatalf("%s hidden: page at %v, was %v: want it left where it was", toggle.name, got, top)
		}
		toggle.run(app)
		if got := pageTop(app, 1); got != top {
			t.Fatalf("%s shown: page at %v, was %v: want it left where it was", toggle.name, got, top)
		}
	}
}

func TestBarShownAtTheDocumentsStartMovesItOutOfTheWay(t *testing.T) {
	app := zoomedIn(t, 0)
	app.toggleTabBar()
	top := pageTop(app, 0)
	app.toggleTabBar()
	bar := app.layoutEdges().tabBar
	if got := pageTop(app, 0); got != top+bar.H {
		t.Fatalf("page at %v, bar %v: want the document's start moved below the bar, to %v", got, bar, top+bar.H)
	}
	app.toggleTabBar()
	if got := pageTop(app, 0); got != top {
		t.Fatalf("page at %v: want it back at %v, nothing left above the document's start", got, top)
	}
}

func TestBarShownAtTheDocumentsEndMovesItOutOfTheWay(t *testing.T) {
	app := zoomedIn(t, 1e9) // the end
	toggleStatusBar(app)    // hidden
	_, h := app.viewportSize()
	end := app.contentH - app.scrollY
	if end > float64(h) {
		t.Fatalf("document's end at %v, window %v: want it in view", end, h)
	}
	toggleStatusBar(app) // shown
	_, h = app.viewportSize()
	if end := app.contentH - app.scrollY; end > float64(h)+0.5 {
		t.Fatalf("document's end at %v, above the bar at %v: want it kept in view", end, h)
	}
}

func TestBarsShownOrHiddenKeepTheFit(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "fit_mode", "page")
	scale := app.scale
	toggleStatusBar(app)
	if app.scale <= scale {
		t.Fatalf("scale %v, was %v: want the page fitted to the taller window", app.scale, scale)
	}
}

func TestPageMovesWithTheBarOnlyAsFarAsItMust(t *testing.T) {
	app := zoomedIn(t, 0)
	app.toggleTabBar()
	slowEdges(app)
	start := pageTop(app, 0)
	app.toggleTabBar()
	m := &app.edgeMotion
	if !m.active || pageTop(app, 0) != start {
		t.Fatal("want the bar to start sliding in, the page still where it was")
	}
	stepEdgesAt(app, m.start.Add(50*time.Millisecond))
	bar := app.layoutEdges().tabBar
	drawnBottom := bar.Y + bar.H*float32(app.edgeIn(edgeItem{kind: edgeTabBar}))
	if top := pageTop(app, 0); top <= start || top < drawnBottom {
		t.Fatalf("page at %v, bar drawn down to %v: want the page moving down with it, never under it", top, drawnBottom)
	}
	stepEdgesAt(app, m.start.Add(150*time.Millisecond))
	if m.active || pageTop(app, 0) != start+bar.H {
		t.Fatalf("active %v, page at %v: want the move over, the page below the bar", m.active, pageTop(app, 0))
	}
}

func TestBarSlidingOverThePageMidDocumentLeavesItStill(t *testing.T) {
	for _, toggle := range []struct {
		name string
		run  func(*App)
	}{{"status bar", toggleStatusBar}, {"tab bar", (*App).toggleTabBar}} {
		for _, shown := range []bool{false, true} {
			app := zoomedIn(t, 600)
			if shown {
				toggle.run(app) // hidden at once, to be shown
			}
			slowEdges(app)
			top := pageTop(app, 1)
			toggle.run(app)
			m := &app.edgeMotion
			for _, ms := range []time.Duration{0, 30, 60, 90, 150} {
				at := ms * time.Millisecond
				stepEdgesAt(app, m.start.Add(at))
				if got := pageTop(app, 1); got != top {
					t.Fatalf("%s shown %v, at %v: page at %v, was %v: want it still as the bar slides", toggle.name, shown, at, got, top)
				}
			}
		}
	}
}

func TestHiddenBarIsLaidOutWhileItSlidesOut(t *testing.T) {
	app := movingEdges(t)
	before := app.layoutEdges()
	toggleStatusBar(app)
	m := &app.edgeMotion
	stepEdgesAt(app, m.start.Add(50*time.Millisecond))
	l := app.layoutEdges()
	if l.status != before.status || !app.motion.animating {
		t.Fatalf("status %v, want it laid out where it was, %v, sliding out", l.status, before.status)
	}
	if l.doc.H <= before.doc.H || l.doc.H >= before.doc.H+before.status.H {
		t.Fatalf("document %v, was %v: want it part way into the bar's room", l.doc, before.doc)
	}
	stepEdgesAt(app, m.start.Add(150*time.Millisecond))
	if l := app.layoutEdges(); m.active || l.status.W != 0 || l.doc.H != before.doc.H+before.status.H {
		t.Fatalf("status %v, document %v: want the bar gone and its room the document's", l.status, l.doc)
	}
}

func TestBarTurnedRoundPartWayGoesBackFromWhereItIs(t *testing.T) {
	app := movingEdges(t)
	toggleStatusBar(app)
	m := &app.edgeMotion
	m.start = time.Now().Add(-50 * time.Millisecond) // half way out
	toggleStatusBar(app)
	s := m.slides[edgeKey{kind: edgeStatus}]
	if s == nil || s.to != 1 || s.from < 0.3 || s.from > 0.7 {
		t.Fatalf("slide %+v: want it coming back in from about half way", s)
	}
}

func TestTabsKeepTheirPlaceAsTheBarSlidesOut(t *testing.T) {
	app := movingEdges(t)
	if !app.tabBarInTitleRow() {
		t.Skip("no tab bar in the title row here")
	}
	shown := app.tabBarLayout()
	app.toggleTabBar()
	stepEdgesAt(app, app.edgeMotion.start.Add(50*time.Millisecond))
	if sliding := app.tabBarLayout(); sliding.area != shown.area {
		t.Fatalf("tabs in %v, were in %v: want them kept beside the window's buttons", sliding.area, shown.area)
	}
	if app.titleRowTakes(shown.area.X+1, shown.area.Y+1, true) {
		t.Fatal("want a bar sliding out to leave presses to the window")
	}
}

func TestResizingABarDoesNotMoveTheEdges(t *testing.T) {
	app := twoTabs(t)
	setOption(t, app, "tab_bar_position", "left")
	slowEdges(app)
	bar := app.layoutEdges().tabBar
	app.resizeTabBar(bar.X + bar.W + 40)
	if app.edgeMotion.active {
		t.Fatal("want a resized bar to follow the pointer, not move")
	}
}

func TestEdgesDoNotMoveWithoutMotion(t *testing.T) {
	app := twoTabs(t)
	app.config.Theme.Motion.Moves[config.MotionEdges][config.MomentEnter].Duration = 0
	app.config.Theme.Motion.Moves[config.MotionEdges][config.MomentExit].Duration = 0
	app.motion.frame = 1
	before := app.documentRect()
	toggleStatusBar(app)
	if app.edgeMotion.active || app.documentRect() == before {
		t.Fatal("want the document laid out at once with motion.edges at none")
	}
}
