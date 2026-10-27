package viewer

import (
	"math"
	"testing"

	commandmeta "onon/internal/commands"
	"onon/internal/mupdf"
)

func TestEveryCommandHasAHandler(t *testing.T) {
	// :quit!, :write! and :wq are documented with :quit and :write.
	unlisted := map[string]bool{"quit!": true, "write!": true, "wq": true}
	specs := map[string]bool{}
	for _, name := range commandmeta.Names() {
		specs[name] = true
		if commandHandlers[name] == nil {
			t.Errorf(":%s has no handler", name)
		}
	}
	for name := range commandHandlers {
		if !specs[name] && !unlisted[name] {
			t.Errorf(":%s has no command spec", name)
		}
	}
	for alias, name := range commandAliases {
		if commandHandlers[name] == nil {
			t.Errorf("alias :%s names :%s, which has no handler", alias, name)
		}
	}
}

func TestToggleCommandArguments(t *testing.T) {
	app := testLayoutApp(1)
	app.runCommand(":fullscreen on")
	app.runCommand(":fullscreen on") // already on: stays on
	if !app.fullscreen {
		t.Fatal(":fullscreen on left fullscreen off")
	}
	app.runCommand(":fullscreen")
	if app.fullscreen {
		t.Fatal(":fullscreen did not toggle")
	}
	app.runCommand(":fullscreen maybe")
	if app.fullscreen || app.message != `:fullscreen expects on or off, not "maybe"` {
		t.Fatalf("bad argument: fullscreen=%v message=%q", app.fullscreen, app.message)
	}
}

func TestModeCommands(t *testing.T) {
	app := testLayoutApp(3)
	app.winW, app.winH = 1000, 800
	app.recomputeLayout(app.viewportSize())
	app.runCommand(":overview")
	if app.overview == nil {
		t.Fatal(":overview did not open the overview")
	}
	app.runCommand(":overview")
	app.runCommand(":present")
	if app.overview != nil || app.presentation == nil {
		t.Fatalf(":overview then :present: overview=%v presentation=%v", app.overview != nil, app.presentation != nil)
	}
	app.runCommand(":present")
	app.outline = []mupdf.OutlineItem{{Title: "One", Parent: -1}}
	app.runCommand(":outline")
	if view := app.activeUIView(); view == nil || view != app.outlineMenu.view {
		t.Fatal(":outline did not open the outline")
	}
}

func TestLayoutToggleCommands(t *testing.T) {
	app := testLayoutApp(4)
	app.winW, app.winH = 1000, 800
	app.recomputeLayout(app.viewportSize())
	app.runCommand(":dual on")
	app.runCommand(":cover off")
	app.runCommand(":statusbar off")
	if !app.dualPage || app.firstPageOffset || app.statusBarShown {
		t.Fatalf("dual=%v cover=%v statusbar=%v", app.dualPage, app.firstPageOffset, app.statusBarShown)
	}
	if len(app.rows[0].pages) != 2 {
		t.Fatalf("first row has %d pages with :dual on :cover off", len(app.rows[0].pages))
	}
	app.runCommand(":dual")
	if app.dualPage {
		t.Fatal(":dual did not toggle off")
	}
}

func TestViewCommands(t *testing.T) {
	app := testLayoutApp(5)
	app.config.MinZoom, app.config.MaxZoom = 0.5, 8
	app.winW, app.winH = 1000, 800
	app.recomputeLayout(app.viewportSize())

	app.runCommand(":rotate")
	app.runCommand(":rotate cw")
	if app.rotation != 180 {
		t.Fatalf("rotation = %v after two :rotate", app.rotation)
	}
	app.runCommand(":rotate 270")
	app.runCommand(":rotate 45")
	if app.rotation != 270 || app.message != "usage: :rotate [cw|ccw|0|90|180|270]" {
		t.Fatalf("rotation = %v, message %q", app.rotation, app.message)
	}

	app.runCommand(":zoom 150%")
	if app.fitMode != fitManual || math.Abs(app.zoom-1.5) > 1e-9 {
		t.Fatalf(":zoom 150%%: fit=%q zoom=%v", app.fitMode, app.zoom)
	}
	app.runCommand(":zoom 200")
	if math.Abs(app.zoom-2) > 1e-9 {
		t.Fatalf(":zoom 200: zoom=%v", app.zoom)
	}

	before := app.page
	app.alignPageToAnchor(4)
	app.runCommand(":back")
	if app.page != before {
		t.Fatalf(":back left page %d, want %d", app.page, before)
	}
	app.runCommand(":forward")
	if app.page != 4 {
		t.Fatalf(":forward left page %d", app.page)
	}

	app.search = searchState{query: "x", order: []searchHitRef{{}}}
	app.runCommand(":noh")
	if app.search.query != "" {
		t.Fatal(":noh kept the search")
	}
}

func TestOtherActionCommands(t *testing.T) {
	app := testPrefetchApp(3, 1)
	app.search = searchState{query: "x", current: 0, matches: map[int][]mupdf.SearchHit{0: {{}}, 2: {{}}}, order: []searchHitRef{{page: 0}, {page: 2}}}
	app.runCommand(":next")
	if app.search.current != 1 {
		t.Fatalf(":next left match %d", app.search.current)
	}
	app.runCommand(":prev")
	if app.search.current != 0 {
		t.Fatalf(":prev left match %d", app.search.current)
	}
	app.pageLinks = map[int][]mupdf.Link{0: {{Bounds: mupdf.Rect{X1: 50, Y1: 10}, Page: 1}}, 1: nil, 2: nil}
	app.runCommand(":hints")
	if app.hints == nil {
		t.Fatal(":hints showed no hints")
	}
}
