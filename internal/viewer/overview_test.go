package viewer

import (
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func testOverviewApp() *App {
	app := testLayoutApp(20)
	app.winW, app.winH = 1000, 800
	app.fitMode, app.renderMode = fitPage, renderSingle
	app.page = 5
	app.recomputeLayout(app.viewportSize())
	return app
}

func TestOverviewGridNavigationAndConfirm(t *testing.T) {
	app := testOverviewApp()
	app.toggleOverview()
	columns := app.overviewColumns()
	if columns != 1000/(app.config.OverviewThumbWidth+app.config.OverviewGap) {
		t.Fatalf("columns = %d", columns)
	}
	if len(app.rows[0].pages) != columns || app.renderMode != renderContinuous {
		t.Fatalf("first row has %d pages, mode %q", len(app.rows[0].pages), app.renderMode)
	}
	if state := app.captureViewState(); state.renderMode != renderSingle || state.page != 5 {
		t.Fatalf("persisted state = %+v, want the view from before the overview", state)
	}

	app.runAction("scroll_down") // one row down
	app.runAction("scroll_right")
	if want := 5 + columns + 1; app.overview.selected != want {
		t.Fatalf("selected = %d, want %d", app.overview.selected, want)
	}
	app.runAction("zoom_out") // one more column
	if app.overviewColumns() != columns+1 {
		t.Fatalf("zoom_out left %d columns", app.overviewColumns())
	}

	app.runAction("confirm")
	if app.overview != nil || app.renderMode != renderSingle || app.fitMode != fitPage {
		t.Fatalf("after confirm: overview=%v mode=%q fit=%q", app.overview, app.renderMode, app.fitMode)
	}
	if app.page != 5+columns+1 {
		t.Fatalf("page = %d, want the selected page", app.page)
	}
}

func TestOverviewCloseReturnsToOriginalPage(t *testing.T) {
	app := testOverviewApp()
	app.toggleOverview()
	app.runAction("last_page")
	app.runAction("close")
	if app.overview != nil || app.page != 5 {
		t.Fatalf("after close: overview=%v page=%d, want page 5", app.overview, app.page)
	}
}

func TestOverviewPagesByScreenAndKeepsScrollOff(t *testing.T) {
	app := testLayoutApp(200)
	app.winW, app.winH = 1000, 2400 // several rows of the tall test pages
	app.config.ScrollOff = 1
	app.recomputeLayout(app.viewportSize())
	app.toggleOverview()
	columns, rows := app.overviewColumns(), app.overviewVisibleRows()
	if rows < 2 {
		t.Fatalf("visible rows = %d", rows)
	}
	app.runAction("next_page")
	if app.overview.selected != app.overview.saved.page+rows*columns {
		t.Fatalf("PgDn moved to %d, want %d", app.overview.selected, app.overview.saved.page+rows*columns)
	}
	// The row after the selection stays on screen.
	next := app.rows[app.pageToRow[app.overview.selected]+1]
	_, viewportH := app.viewportSize()
	if next.y+next.height > app.scrollY+float64(viewportH) {
		t.Fatalf("scroll_off row below the selection is off screen")
	}
	app.runAction("prev_page")
	if app.overview.selected != app.overview.saved.page {
		t.Fatalf("PgUp moved to %d", app.overview.selected)
	}
}

func TestOverviewLargeScrollOffCentresSelection(t *testing.T) {
	app := testLayoutApp(200)
	app.winW, app.winH = 1000, 1500
	app.config.ScrollOff = 8
	app.recomputeLayout(app.viewportSize())
	app.toggleOverview()
	rows := app.overviewVisibleRows()
	if rows < 3 {
		t.Fatalf("visible rows = %d, want at least 3 for the test", rows)
	}
	for range 10 {
		app.runAction("scroll_down")
	}
	first := app.rowIndexAtContentY(app.scrollY + float64(app.config.OverviewGap))
	if got, want := app.pageToRow[app.overview.selected]-first, (rows-1)/2; got != want {
		t.Fatalf("selected row is %d rows below the top, want %d (centred)", got, want)
	}
}

func TestPromptOverOverviewReceivesEnter(t *testing.T) {
	app := testOverviewApp()
	app.applyConfigState(config.Default()) // default key bindings
	app.toggleOverview()
	app.runAction("command_mode")
	app.input.Set("colors alt")
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeReturn})
	if app.mode != modeNormal || app.overview == nil {
		t.Fatalf("Enter went to the overview: mode=%v overview=%v", app.mode, app.overview != nil)
	}
	if !app.altColors {
		t.Fatal("command not run")
	}
}

func TestOverviewRendersThumbnailsOnly(t *testing.T) {
	app := testOverviewApp()
	app.fitMode = fitWidth // the reading view renders at scale 10
	app.recomputeLayout(app.viewportSize())
	app.config.RenderOversample = 1
	app.ensureRenderBaseScale()
	app.renderPending = map[tileKey]renderRequest{}
	app.renderWorker = &renderWorker{requests: make(chan renderRequest, 128)}
	base := app.renderBaseScale

	app.toggleOverview()
	app.adjustRenderBaseScaleForExtremeZoom(app.scale)
	app.prefetchVisiblePages()
	if app.renderBaseScale != base {
		t.Fatalf("overview changed the render scale %.3f -> %.3f", base, app.renderBaseScale)
	}
	if len(app.renderPending) == 0 {
		t.Fatal("no thumbnails requested")
	}
	for key, req := range app.renderPending {
		if !key.thumb || math.Abs(req.scale-app.scale) > 1e-9 {
			t.Fatalf("overview requested %+v at scale %.3f, want thumbnails at the grid scale %.3f", key, req.scale, app.scale)
		}
	}

	// A current thumbnail at the grid's scale needs no new render.
	page := app.overview.selected
	app.renderPending = map[tileKey]renderRequest{}
	app.cache.add(&renderedTile{key: thumbnailKey(page), rect: image.Rect(0, 0, 10, 10), scale: app.scale, version: app.tileVersion(page)})
	app.prefetchVisiblePages()
	if _, ok := app.renderPending[thumbnailKey(page)]; ok {
		t.Fatal("re-requested a thumbnail that is already sharp enough")
	}
	app.runAction("confirm")
	if app.renderBaseScale != base {
		t.Fatalf("render scale %.3f after the overview, want %.3f", app.renderBaseScale, base)
	}
}

func TestOverviewGridFitsWidthAndAlignsLastRow(t *testing.T) {
	app := testLayoutApp(13)         // 5 columns leave a short last row
	for i := range app.pageMetrics { // Letter pages, so thumbnails are below 1x
		app.pageMetrics[i] = newPageMetrics(mupdf.PageInfo{Bounds: mupdf.Rect{X1: 612, Y1: 792}})
	}
	app.winW, app.winH = 1300, 800
	app.recomputeLayout(app.viewportSize())
	app.toggleOverview()
	app.overview.columns = 5
	app.relayoutOverview()
	viewportW, _ := app.viewportSize()
	for i, row := range app.rows {
		last := len(row.pages) - 1
		if right := row.pageX[last] + row.pageW[last] + float64(app.config.OverviewGap); right > float64(viewportW)+0.5 {
			t.Fatalf("row %d ends at %.1f, past the %dpx viewport", i, right, viewportW)
		}
	}
	first, lastRow := app.rows[0], app.rows[len(app.rows)-1]
	if len(lastRow.pages) == 5 || lastRow.pageX[0] != first.pageX[0] {
		t.Fatalf("last row starts at %.1f, first row at %.1f", lastRow.pageX[0], first.pageX[0])
	}
}

func TestOverviewGroupsSpreadsInDualMode(t *testing.T) {
	app := testLayoutApp(9)
	app.winW, app.winH = 1000, 800
	app.dualPage, app.firstPageOffset = true, true
	app.recomputeLayout(app.viewportSize())
	app.alignPageToAnchor(4) // in the spread 3-4
	app.toggleOverview()
	if app.overview.selected != 3 {
		t.Fatalf("selected = %d, want the spread's first page 3", app.overview.selected)
	}
	row := app.rows[app.pageToRow[3]]
	i := slices.Index(row.pages, 3)
	if row.pages[i+1] != 4 || row.pageX[i+1] != row.pageX[i]+row.pageW[i] {
		t.Fatalf("spread 3-4 is not edge to edge: %v at %v", row.pages, row.pageX)
	}
	cover := app.rows[0]
	if cover.pageX[1]-(cover.pageX[0]+cover.pageW[0]) != float64(app.config.OverviewGap) {
		t.Fatal("no gap between the cover and the next spread")
	}
	// Two spreads per row: columns line up, the cover on its slot's right.
	x := func(page int) float64 {
		return app.rows[app.pageToRow[page]].pageX[slices.Index(app.rows[app.pageToRow[page]].pages, page)]
	}
	if app.overviewColumns() != 2 || x(1) != x(5) {
		t.Fatalf("columns=%d; spread 1-2 at x=%.1f, spread 5-6 at x=%.1f", app.overviewColumns(), x(1), x(5))
	}
	if math.Abs(x(0)+cover.pageW[0]-(x(4)+cover.pageW[0])) > 1e-9 {
		t.Fatalf("cover ends at %.1f, the spread below it at %.1f", x(0)+cover.pageW[0], x(4)+cover.pageW[0])
	}
	app.runAction("scroll_right")
	if app.overview.selected != 5 {
		t.Fatalf("moving right selected %d, want the next spread 5", app.overview.selected)
	}
	app.runAction("last_page")
	if app.overview.selected != 7 {
		t.Fatalf("last spread starts at %d, want 7", app.overview.selected)
	}
}

func TestOverviewSelectionFollowsHover(t *testing.T) {
	app := testLayoutApp(9)
	app.winW, app.winH = 1000, 800
	app.dualPage, app.firstPageOffset = true, true
	app.recomputeLayout(app.viewportSize())
	app.toggleOverview()
	row := app.rows[app.pageToRow[4]]
	i := slices.Index(row.pages, 4)
	x, y := app.rowPageScreenOrigin(row, i)
	scrollY := app.scrollY
	if !app.handleSDLMouseMotion(&sdl.MouseMotionEvent{X: float32(x + row.pageW[i]/2), Y: float32(y + row.pageH[i]/2)}) {
		t.Fatal("hovering another spread reported no change")
	}
	if app.overview.selected != 3 || app.scrollY != scrollY {
		t.Fatalf("selected = %d, scrollY %.1f -> %.1f; want spread 3 and no scroll", app.overview.selected, scrollY, app.scrollY)
	}
	if app.handleSDLMouseMotion(&sdl.MouseMotionEvent{X: float32(x + 1), Y: float32(y + 1)}) {
		t.Fatal("moving within the selected spread reported a change")
	}
}

func TestOverviewAutoscrollsAndSelectionFollowsPointer(t *testing.T) {
	app := testLayoutApp(200)
	app.winW, app.winH = 1000, 800
	app.config.AutoscrollSpeedFactor, app.config.AutoscrollMaxSpeed = 1, 20000
	app.mouseBindings = map[string]string{"middle_down": "autoscroll"}
	app.recomputeLayout(app.viewportSize())
	app.toggleOverview()
	row := app.rows[0]
	x, y := app.rowPageScreenOrigin(row, 1)
	px, py := float32(x+row.pageW[1]/2), float32(y+row.pageH[1]/2)
	app.handleSDLMouseMotion(&sdl.MouseMotionEvent{X: px, Y: py})
	app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonMiddle), X: px, Y: py})
	if app.autoscroll == nil {
		t.Fatal("middle click did not start autoscroll in the overview")
	}
	app.pointer.Y += autoscrollDeadZone + 100
	app.advanceAutoscrollBy(time.Second)
	if app.scrollY == 0 {
		t.Fatal("autoscroll did not scroll the overview")
	}
	page, _, ok := app.pagePointAtScreen(float64(app.pointer.X), float64(app.pointer.Y))
	if !ok || app.overview.selected != page || app.page != page {
		t.Fatalf("selected %d, page %d; want the page under the pointer %d", app.overview.selected, app.page, page)
	}
}

func TestOverviewPansWithTheMouse(t *testing.T) {
	app := testLayoutApp(200)
	app.winW, app.winH = 1000, 800
	app.recomputeLayout(app.viewportSize())
	app.toggleOverview()
	app.actionKeycode = sdl.KeycodeSpace
	app.runAction("pan")
	app.actionKeycode = 0
	if !app.panning {
		t.Fatal("pan did not start in the overview")
	}
	selected := app.overview.selected
	app.handleSDLMouseMotion(&sdl.MouseMotionEvent{X: 500, Y: 400, Yrel: -300})
	if app.scrollY != 300 || app.overview.selected != selected {
		t.Fatalf("scrollY = %.1f, selected %d -> %d; want 300 and the same selection", app.scrollY, selected, app.overview.selected)
	}
	app.handleSDLKeyUp(&sdl.KeyboardEvent{Key: sdl.KeycodeSpace})
	if app.panning {
		t.Fatal("releasing the key did not end the pan")
	}
}

func TestSettingsChangedInOverviewCarryOver(t *testing.T) {
	app := testOverviewApp()
	app.toggleOverview()
	app.runAction("toggle_dual_page")
	app.runAction("toggle_alt_colors")
	if state := app.captureViewState(); !state.dualPage || state.renderMode != renderSingle {
		t.Fatalf("session state %+v, want dual on with the pre-overview render mode", state)
	}
	app.runAction("close")
	if !app.dualPage || !app.altColors {
		t.Fatalf("after the overview: dual=%v alt=%v, want both kept", app.dualPage, app.altColors)
	}
	if app.renderMode != renderSingle || app.fitMode != fitPage {
		t.Fatalf("overview's own settings not restored: mode=%q fit=%q", app.renderMode, app.fitMode)
	}
}

func TestOverviewColumnsFollowTheOverviewOptions(t *testing.T) {
	app := testOverviewApp()
	app.config.OverviewThumbWidth = 480
	app.toggleOverview()
	if got := app.overviewColumns(); got != 1000/(480+16) {
		t.Fatalf("columns = %d with overview_thumb_width 480", got)
	}
	app.config.OverviewThumbWidth, app.config.OverviewMaxColumns = 10, 3
	if got := app.overviewColumns(); got != 3 {
		t.Fatalf("columns = %d, want overview_max_columns 3", got)
	}
}

func TestOverviewThumbnailSizeDoesNotDependOnPageCount(t *testing.T) {
	long := testOverviewApp()
	long.toggleOverview()
	short := testLayoutApp(1)
	short.winW, short.winH = 1000, 800
	short.recomputeLayout(short.viewportSize())
	short.toggleOverview()
	if got, want := short.rows[0].pageW[0], long.rows[0].pageW[0]; math.Abs(got-want) > 0.01 {
		t.Fatalf("one-page overview thumbnail is %.1fpx wide, want %.1fpx as with 20 pages", got, want)
	}
	if want := float64(short.config.OverviewThumbWidth); short.rows[0].pageW[0] > 2*want {
		t.Fatalf("thumbnail is %.1fpx wide, far past overview_thumb_width %v", short.rows[0].pageW[0], want)
	}
}

func TestOverviewOpensWithNoDocument(t *testing.T) {
	app := testLayoutApp(0)
	app.relayoutWithViewportAnchor(nil) // as a change of display scale does
	if app.page != 0 {
		t.Fatalf("page = %d with no document, want 0", app.page)
	}
	app.toggleOverview()
	app.runAction("scroll_down")
	app.toggleOverview()
}
