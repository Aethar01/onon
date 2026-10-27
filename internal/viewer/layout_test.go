package viewer

import (
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/testpdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font/basicfont"
)

func TestRecomputeLayoutUsesRotatedPageDimensions(t *testing.T) {
	width, height := rotatedBoundsSize(mupdf.Rect{X1: 100, Y1: 200}, 90)
	assertClose(t, width, 200)
	assertClose(t, height, 100)
	metrics := make([]pageMetrics, 5)
	for i := range metrics {
		metrics[i] = pageMetrics{bounds: mupdf.Rect{X1: 100, Y1: 200}, width: width, height: height}
	}

	app := &App{
		documentState:   documentState{pageCount: 5, page: 0},
		viewStateFields: viewStateFields{zoom: 1, fitMode: fitManual, renderMode: renderContinuous, dualPage: true, firstPageOffset: true},
		config:          config.Config{PageGap: -1, PageGapHorizontal: -1, PageGapVertical: -1, SpreadGap: -1},
		metricsService:  metricsService{pageMetrics: metrics},
	}

	app.recomputeLayout(1000, 1000)

	if len(app.rows) != 3 {
		t.Fatalf("expected first offset row plus two spreads, got %d rows", len(app.rows))
	}
	assertClose(t, app.rows[0].width, 200)
	assertClose(t, app.rows[0].height, 100)
	assertClose(t, app.rows[1].width, 400)
	assertClose(t, app.rows[1].height, 100)
	assertClose(t, app.rows[0].x, 100)
	assertClose(t, app.rows[1].x, 0)
}

func TestNewAllowsBlankViewerWithoutDocument(t *testing.T) {
	rt, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	app, err := New("", rt, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if app.doc != nil || app.docPath != "" || app.docName != "" {
		t.Fatalf("expected blank viewer without document, app=%+v", app)
	}
	if app.pageCount != 0 || len(app.pageMetrics) != 0 || len(app.rows) != 0 {
		t.Fatalf("expected no document layout, pageCount=%d metrics=%d rows=%d", app.pageCount, len(app.pageMetrics), len(app.rows))
	}
	if app.renderWorker != nil || app.searchWorker != nil {
		t.Fatalf("expected no document workers, render=%v search=%v", app.renderWorker, app.searchWorker)
	}
}

func TestSetRotationKeepsCurrentPage(t *testing.T) {
	app := testLayoutApp(5)
	app.renderMode = renderContinuous
	app.page = 3

	if err := app.SetRotation(90); err != nil {
		t.Fatal(err)
	}

	if app.page != 3 {
		t.Fatalf("expected rotation to keep page 3, got %d", app.page)
	}
}

func TestSetRenderModeKeepsCurrentPage(t *testing.T) {
	app := testLayoutApp(5)
	app.renderMode = renderSingle
	app.page = 3
	app.recomputeLayout(1000, 1000)

	if err := app.SetRenderMode("continuous"); err != nil {
		t.Fatal(err)
	}

	if app.page != 3 {
		t.Fatalf("expected render mode change to keep page 3, got %d", app.page)
	}
}

func TestPageBackgroundVerticesUseRotatedPageCorners(t *testing.T) {
	vertices := pageBackgroundVertices(10, 20, mupdf.Rect{X1: 100, Y1: 100}, 1, 45, color.RGBA{R: 1, G: 2, B: 3, A: 4})

	assertClose(t, float64(vertices[0].Position.X), 80.711)
	assertClose(t, float64(vertices[0].Position.Y), 20)
	assertClose(t, float64(vertices[1].Position.X), 151.421)
	assertClose(t, float64(vertices[1].Position.Y), 90.711)
	assertClose(t, float64(vertices[2].Position.X), 10)
	assertClose(t, float64(vertices[2].Position.Y), 90.711)
	assertClose(t, float64(vertices[3].Position.X), 80.711)
	assertClose(t, float64(vertices[3].Position.Y), 161.421)

	if vertices[0].Color != (sdl.FColor{R: 1.0 / 255, G: 2.0 / 255, B: 3.0 / 255, A: 4.0 / 255}) {
		t.Fatalf("unexpected vertex color: %+v", vertices[0].Color)
	}
}

func TestSmoothWheelQueuesPreciseScrollDelta(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollDampening = 0.35
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{X: 0.25, Y: 0.1})

	assertClose(t, app.scrollX, 0)
	assertClose(t, app.scrollY, 100)
	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected smooth scroll target")
	}
	assertClose(t, state.targetX, 16)
	assertClose(t, state.targetY, 93.6)
}

func TestSmoothWheelPrefersPreciseDeltaOverAccumulatedIntegerTicks(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollDampening = 0.35
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{Y: 0.1, IntegerY: 1})

	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected smooth scroll target")
	}
	assertClose(t, state.targetY, 93.6)
}

func TestSmoothWheelUsesScrollStepForIntegerDelta(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollDampening = 0.35
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{Y: 1, IntegerY: 1})

	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected smooth scroll target")
	}
	assertClose(t, state.targetY, 36)
}

func TestTrackpadSmoothingCanBeSelectedWithoutMouseWheelSmoothing(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollSources = config.SmoothInputTrackpad
	app.config.SmoothScrollDampening = 0.35
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{Y: 0.1})
	if !app.smoothScrollActive() {
		t.Fatal("expected fractional trackpad input to animate")
	}
	app.cancelSmoothScroll()
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{Y: 1})
	assertClose(t, app.scrollY, 36)
	if app.smoothScrollActive() {
		t.Fatal("expected detented mouse-wheel input to remain immediate")
	}
}

func TestInvertScrollInvertsBothTrackpadAxes(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.InvertScrollSources = config.SmoothInputTrackpad
	app.config.SmoothScrollDampening = 0.35
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)
	app.scrollX = 32
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{X: 0.25, Y: 0.5})

	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected smooth scroll target")
	}
	assertClose(t, state.targetX, 16)
	assertClose(t, state.targetY, 132)
}

func TestInvertScrollTreatsMouseAndTrackpadSeparately(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollSources = 0
	app.config.InvertScrollSources = config.SmoothInputMouse
	app.mouseBindings = map[string]string{
		"wheel_up":   "scroll_up",
		"wheel_down": "scroll_down",
	}
	app.recomputeLayout(1000, 100)

	app.scrollY = 100
	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{Y: 1})
	assertClose(t, app.scrollY, 164)

	// A swipe that reports a whole delta mid-gesture is still the trackpad.
	app.scrollY = 100
	swipe := sdl.MouseWheelEvent{Y: 0.5}
	swipe.Timestamp = uint64(time.Second)
	app.handleAnimatedMouseWheel(&swipe)
	assertClose(t, app.scrollY, 36)
	app.scrollY = 100
	swipe = sdl.MouseWheelEvent{Y: 1}
	swipe.Timestamp = uint64(time.Second + 16*time.Millisecond)
	app.handleAnimatedMouseWheel(&swipe)
	assertClose(t, app.scrollY, 36)

	app.scrollY = 100
	click := sdl.MouseWheelEvent{Y: 1}
	click.Timestamp = uint64(2 * time.Second)
	app.handleAnimatedMouseWheel(&click)
	assertClose(t, app.scrollY, 164)
}

func TestDisabledSmoothScrollUsesDiscreteWheelPath(t *testing.T) {
	app := testLayoutApp(5)
	app.config.SmoothScrollSources = 0
	app.pageStep = 64
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	app.handleAnimatedMouseWheel(&sdl.MouseWheelEvent{Y: 1})

	assertClose(t, app.scrollY, 36)
	if app.smoothScrollActive() {
		t.Fatal("expected disabled smooth scrolling to leave no animation state")
	}
}

func TestPanCanBeHeldByKey(t *testing.T) {
	app := testLayoutApp(5)
	app.actionKeycode = sdl.KeycodeSpace

	if err := app.runBuiltinAction("pan"); err != nil {
		t.Fatal(err)
	}

	if !app.panning || app.panKeycode != sdl.KeycodeSpace || app.panButton != 0 {
		t.Fatalf("expected key pan state, panning=%v panKeycode=%v panButton=%d", app.panning, app.panKeycode, app.panButton)
	}
	app.handleSDLKeyUp(&sdl.KeyboardEvent{Key: sdl.KeycodeSpace})
	if app.panning {
		t.Fatal("expected key release to stop panning")
	}
}

func TestPanEndsWhenKeyIsReleasedWithOtherModifiers(t *testing.T) {
	app := testLayoutApp(5)
	app.setKeyBindings(map[string]string{"<Space>": "pan"})

	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeSpace})
	if !app.panning || app.actionKeycode != 0 {
		t.Fatalf("expected Space to start panning and clear the action key, panning=%v actionKeycode=%v", app.panning, app.actionKeycode)
	}
	app.handleSDLKeyUp(&sdl.KeyboardEvent{Key: sdl.KeycodeSpace, Mod: sdl.KeymodShift})
	if app.panning {
		t.Fatal("expected releasing Space with Shift held to stop panning")
	}
}

func TestOpenCommandKeepsSpacesInPath(t *testing.T) {
	app := &App{}
	path := filepath.Join(t.TempDir(), "Lancer - Core Book.pdf")

	app.runCommand(":open " + path)

	if app.pendingOpen != path {
		t.Fatalf("expected pending open path %q, got %q", path, app.pendingOpen)
	}

	app = &App{}
	app.runCommand(":open " + escapeCommandPath(path))
	if app.pendingOpen != path {
		t.Fatalf("expected escaped pending open path %q, got %q", path, app.pendingOpen)
	}
}

func escapeCommandPath(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(path, `\`, `\\`), " ", `\ `)
}

func TestRenderTargetOversamplesZoom(t *testing.T) {
	app := testLayoutApp(1)
	app.scale = 2
	app.zoom = 2
	app.minRenderBaseScale = 0.25
	app.config.RenderOversample = 1

	assertClose(t, app.currentRenderTarget(), 2)
}

func TestRenderTargetAllowsUndersampling(t *testing.T) {
	app := testLayoutApp(1)
	app.scale = 2
	app.zoom = 2
	app.minRenderBaseScale = 0.25
	app.config.RenderOversample = 0.75

	assertClose(t, app.currentRenderTarget(), 1.5)
}

func TestRowPageScreenOrigin(t *testing.T) {
	row := rowLayout{x: 20, y: 30, width: 200, height: 300, pageX: []float64{45}, pageY: []float64{65}}
	app := testLayoutApp(1)
	app.rows = []rowLayout{row}
	app.pageToRow = []int{0}
	app.winW = 500
	app.winH = 600
	app.contentW = 300
	app.contentH = 700
	app.scrollX = 10
	app.scrollY = 15

	x, y := app.rowPageScreenOrigin(row, 0)
	assertClose(t, x, 135)
	assertClose(t, y, 50)

	app.renderMode = renderSingle
	x, y = app.rowPageScreenOrigin(row, 0)
	assertClose(t, x, 165)
	assertClose(t, y, 170)
}

func testLayoutApp(pageCount int) *App {
	metrics := make([]pageMetrics, pageCount)
	for i := range metrics {
		metrics[i] = pageMetrics{bounds: mupdf.Rect{X1: 100, Y1: 200}, width: 100, height: 200}
	}
	return &App{
		documentState:   documentState{pageCount: pageCount},
		viewStateFields: viewStateFields{zoom: 1, fitMode: fitManual, renderMode: renderContinuous, firstPageOffset: true},
		config: config.Config{
			PageGap: -1, PageGapHorizontal: -1, PageGapVertical: -1, SpreadGap: -1, SmoothScrollSources: config.SmoothInputAll,
			OverviewThumbWidth: 220, OverviewMaxColumns: 12, OverviewGap: 16,
		},
		metricsService: metricsService{pageMetrics: metrics},
	}
}

func assertClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("got %.3f, want %.3f", got, want)
	}
}

func TestActivateLinkHonorsDestinationPoint(t *testing.T) {
	app := testLayoutApp(5)
	app.recomputeLayout(1000, 100)
	app.activateLink(mupdf.Link{Page: 3, Y: 150, HasY: true})

	want := testLayoutApp(5)
	want.recomputeLayout(1000, 100)
	want.alignPageToDocumentPoint(3, 50, 150)

	assertClose(t, app.scrollY, want.scrollY)
	if app.page != 3 {
		t.Fatalf("page = %d, want 3", app.page)
	}

	top := testLayoutApp(5)
	top.recomputeLayout(1000, 100)
	top.alignPageToAnchor(3)
	if math.Abs(app.scrollY-top.scrollY) < 1 {
		t.Fatalf("link jump ignored destination y: scrollY=%.1f matches page top", app.scrollY)
	}
}

func TestLinkSchemeAllowed(t *testing.T) {
	schemes := config.Default().LinkSchemes
	for uri, want := range map[string]bool{
		"https://example.com":   true,
		"HTTP://example.com":    true,
		"mailto:a@example.com":  true,
		"file:///etc/passwd":    false,
		"javascript:alert(1)":   false,
		"relative/path.pdf":     false,
		"launch:calc.exe":       false,
		"https://[invalid-host": false,
	} {
		if got := linkSchemeAllowed(uri, schemes); got != want {
			t.Errorf("linkSchemeAllowed(%q) = %t, want %t", uri, got, want)
		}
	}
}

func TestLinkFollowsOnReleaseOverSameLink(t *testing.T) {
	app := testLayoutApp(3)
	app.recomputeLayout(1000, 1000)
	app.pageLinks = map[int][]mupdf.Link{0: {{Bounds: mupdf.Rect{X1: 100, Y1: 50}, Page: 2}}}
	x, y := app.rowPageScreenOrigin(app.rows[0], 0)
	click := func(kind sdl.EventType, dx, dy float64) {
		app.handleLinkButton(&sdl.MouseButtonEvent{Type: kind, Button: uint8(sdl.ButtonLeft), X: float32(x + dx), Y: float32(y + dy)})
	}

	click(sdl.EventMouseButtonDown, 10, 10)
	if app.page != 0 {
		t.Fatal("link followed on press")
	}
	click(sdl.EventMouseButtonUp, 400, 400)
	if app.page != 0 {
		t.Fatal("link followed after releasing elsewhere")
	}
	click(sdl.EventMouseButtonDown, 10, 10)
	click(sdl.EventMouseButtonUp, 20, 20)
	if app.page != 2 {
		t.Fatalf("page = %d, want link target 2", app.page)
	}
}

func TestHoveredLinkShowsTargetAndRestoresMessage(t *testing.T) {
	app := testLayoutApp(3)
	app.message = "ready"
	link := mupdf.Link{External: true, URI: "https://example.com"}

	if !app.setHoveredLink(link, true) || app.message != link.URI {
		t.Fatalf("hover message = %q, want %q", app.message, link.URI)
	}
	if app.setHoveredLink(link, true) {
		t.Fatal("re-hovering the same link reported a change")
	}
	app.setHoveredLink(mupdf.Link{}, false)
	if app.message != "ready" {
		t.Fatalf("message after leaving = %q, want restored %q", app.message, "ready")
	}

	app.setHoveredLink(mupdf.Link{Page: 1}, true)
	if app.message != "page 2" {
		t.Fatalf("internal link message = %q", app.message)
	}
	app.message = "copied 3 chars"
	app.setHoveredLink(mupdf.Link{}, false)
	if app.message != "copied 3 chars" {
		t.Fatalf("leaving a link clobbered a newer message: %q", app.message)
	}
}

func TestFitModesScaleToViewport(t *testing.T) {
	app := testLayoutApp(3) // 100x200pt pages
	for mode, want := range map[fitMode]float64{fitWidth: 10, fitHeight: 4, fitPage: 4} {
		app.fitMode = mode
		app.recomputeLayout(1000, 800)
		if math.Abs(app.scale-want) > 1e-9 {
			t.Errorf("fit %s: scale = %v, want %v", mode, app.scale, want)
		}
	}
}

func TestToggleTrimMarginsLaysPagesOutByContent(t *testing.T) {
	doc, err := mupdf.Open(testpdf.WriteImage(t), mupdf.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	app := testLayoutApp(1)
	app.doc = doc
	info, _ := doc.PageInfo(0)
	app.pageMetrics[0] = newPageMetrics(info)
	app.config.TrimPadding = 8
	app.recomputeLayout(1000, 1000)

	app.toggleTrimMargins()
	for deadline := time.Now().Add(2 * time.Second); !app.contentBoxesLoaded() && time.Now().Before(deadline); {
		app.pollMetricUpdates()
		time.Sleep(time.Millisecond)
	}
	want := mupdf.Rect{X0: 100 - 8, Y0: 100 - 8, X1: 200 + 8, Y1: 200 + 8}
	if got := app.pageMetrics[0].bounds; got != want {
		t.Fatalf("trimmed bounds = %v, want %v", got, want)
	}
	assertClose(t, app.pageMetrics[0].width, 116)

	cfg := app.config
	cfg.TrimPadding = 0
	app.applyConfig(cfg, nil)
	if got := app.pageMetrics[0].bounds; got != (mupdf.Rect{X0: 100, Y0: 100, X1: 200, Y1: 200}) {
		t.Fatalf("bounds after trim_padding=0 = %v, want the content box", got)
	}

	app.toggleTrimMargins()
	if got := app.pageMetrics[0].bounds; got != info.Bounds {
		t.Fatalf("untrimmed bounds = %v, want the full page", got)
	}
}

func TestFitWidthKeepsSpreadGapUnscaled(t *testing.T) {
	app := testLayoutApp(3)
	for i := range app.pageMetrics { // Letter pages: the spread fits below 1x
		app.pageMetrics[i] = newPageMetrics(mupdf.PageInfo{Bounds: mupdf.Rect{X1: 612, Y1: 792}})
	}
	app.dualPage, app.firstPageOffset = true, false
	app.config.PageGapHorizontal = 40
	app.fitMode = fitWidth
	app.recomputeLayout(1000, 800)
	row := app.rows[0]
	if gap := row.pageX[1] - (row.pageX[0] + row.pageW[0]); math.Abs(gap-40) > 0.01 {
		t.Fatalf("spread gap = %.2f px, want 40", gap)
	}
	if right := row.pageX[1] + row.pageW[1] + 40; math.Abs(right-1000) > 0.01 {
		t.Fatalf("spread ends at %.2f px with its margin, want the 1000px viewport edge", right)
	}
}

func TestViewportAndContentOffsets(t *testing.T) {
	app := &App{layoutState: layoutState{winW: 800, winH: 600}, sdlState: sdlState{fontFace: basicfont.Face7x13}, viewStateFields: viewStateFields{statusBarShown: true}}
	w, h := app.viewportSize()
	if w != 800 || h != 583 {
		t.Fatalf("expected status bar to reduce viewport height, got %dx%d", w, h)
	}

	app.statusBarShown = false
	app.mode = modeCommand
	w, h = app.viewportSize()
	if w != 800 || h != 583 {
		t.Fatalf("expected input mode to reserve status bar height, got %dx%d", w, h)
	}

	app.mode = modeNormal
	app.contentW = 400
	app.contentH = 200
	app.renderMode = renderSingle
	x, y := app.contentViewportOffset()
	assertClose(t, x, 200)
	assertClose(t, y, 200)

	app.renderMode = renderContinuous
	x, y = app.contentViewportOffset()
	assertClose(t, x, 200)
	assertClose(t, y, 0)
}

func TestTransformAndInverseTransformRoundTrip(t *testing.T) {
	for _, rotation := range []float64{0, 45, 90, 180, 270, -90} {
		x, y := transformPoint(12, 34, 1.5, rotation)
		gotX, gotY := inverseTransformPoint(x, y, 1.5, rotation)
		assertClose(t, gotX, 12)
		assertClose(t, gotY, 34)
	}
}

func TestLayoutGapAndRotationEdgeCases(t *testing.T) {
	app := &App{config: config.Config{PageGap: 12, PageGapVertical: -1, SpreadGap: 34, PageGapHorizontal: -1}}
	if app.verticalGap() != 12 || app.horizontalGap() != 34 {
		t.Fatalf("expected fallback gaps from page/spread gap, got vertical=%d horizontal=%d", app.verticalGap(), app.horizontalGap())
	}
	app.config.PageGapVertical = 7
	app.config.PageGapHorizontal = 9
	if app.verticalGap() != 7 || app.horizontalGap() != 9 {
		t.Fatalf("expected explicit gaps to win, got vertical=%d horizontal=%d", app.verticalGap(), app.horizontalGap())
	}

	for _, tt := range []struct {
		input float64
		want  float64
	}{
		{input: -90, want: 270},
		{input: 450, want: 90},
		{input: 720, want: 0},
	} {
		assertClose(t, normalizeRotation(tt.input), tt.want)
	}
}

func TestViewportAnchorRowIndexAndCurrentPageFollowScroll(t *testing.T) {
	app := testLayoutApp(4)
	app.winW = 100
	app.winH = 100
	app.config.PageGapVertical = 10
	app.recomputeLayout(app.viewportSize())

	app.renderMode = renderContinuous
	app.scrollY = app.rows[2].y - 1
	if got := app.viewportAnchorRowIndex(); got != 2 {
		t.Fatalf("expected viewport midpoint in row 2, got row %d", got)
	}
	app.updateCurrentPageFromScroll()
	if app.page != app.rows[2].pages[0] {
		t.Fatalf("expected current page to follow row 2, got page %d", app.page)
	}

	app.renderMode = renderSingle
	app.page = 3
	if got := app.viewportAnchorRowIndex(); got != app.pageToRow[3] {
		t.Fatalf("expected single-page row from current page, got %d want %d", got, app.pageToRow[3])
	}
}

func TestViewportAnchorRowIndexUsesDocumentEdgesWhenClamped(t *testing.T) {
	for _, anchor := range []string{"top", "center", "bottom"} {
		t.Run(anchor, func(t *testing.T) {
			app := testLayoutApp(5)
			app.winW = 300
			app.winH = 500
			app.config.AnchorPosition = anchor
			app.recomputeLayout(app.viewportSize())

			app.scrollY = 0
			if got := app.viewportAnchorRowIndex(); got != 0 {
				t.Fatalf("expected document start to anchor row 0, got %d", got)
			}

			app.scrollY = app.clampedScrollY(app.contentH)
			if got := app.viewportAnchorRowIndex(); got != len(app.rows)-1 {
				t.Fatalf("expected document end to anchor last row, got %d", got)
			}
		})
	}
}

func TestViewportAnchorRowIndexUsesConfiguredAnchorPosition(t *testing.T) {
	app := testLayoutApp(4)
	app.winW = 100
	app.winH = 100
	app.config.PageGapVertical = 10
	app.recomputeLayout(app.viewportSize())
	app.renderMode = renderContinuous
	app.scrollY = app.rows[2].y - 1

	app.config.AnchorPosition = "top"
	if got := app.viewportAnchorRowIndex(); got != 1 {
		t.Fatalf("expected top anchor to use row 1 sliver, got row %d", got)
	}
	app.config.AnchorPosition = "bottom"
	if got := app.viewportAnchorRowIndex(); got != 2 {
		t.Fatalf("expected bottom anchor to use row 2, got row %d", got)
	}
}

func TestResizeKeepsConfiguredViewportAnchorPosition(t *testing.T) {
	app := testLayoutApp(4)
	app.winW = 220
	app.winH = 300
	app.fitMode = fitWidth
	app.config.AnchorPosition = "top"
	app.recomputeLayout(app.viewportSize())
	app.scrollY = app.rows[1].pageY[0] - 24
	anchor := app.captureViewportAnchor()

	app.winW = 420
	app.recomputeLayout(app.viewportSize())
	app.restoreViewportAnchor(anchor)

	x, y, ok := app.pageScreenOrigin(anchor.page)
	if !ok {
		t.Fatal("expected anchored page to remain placed")
	}
	tx, ty := transformPoint(anchor.point.X, anchor.point.Y, app.scale, app.rotation)
	originX, originY := rotatedBoundsOrigin(app.pageMetrics[anchor.page].bounds, app.scale, app.rotation)
	targetX, targetY := app.viewportAnchorScreenPoint()
	assertClose(t, x+tx-originX, targetX)
	assertClose(t, y+ty-originY, targetY)
}

func TestPageTransformRoundTripsAndStartsAtTheDrawnCorner(t *testing.T) {
	bounds := mupdf.Rect{X1: 100, Y1: 200}
	for _, rotation := range []float64{0, 90, 180, 270} {
		tr := newPageTransform(bounds, 1.5, rotation)
		minX, minY := math.Inf(1), math.Inf(1)
		for _, corner := range []mupdf.Point{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 0, Y: 200}, {X: 100, Y: 200}} {
			dx, dy := tr.toScreen(corner.X, corner.Y)
			minX, minY = math.Min(minX, dx), math.Min(minY, dy)
			if back := tr.toPage(dx, dy); math.Abs(back.X-corner.X) > 1e-9 || math.Abs(back.Y-corner.Y) > 1e-9 {
				t.Fatalf("rotation %v: %v came back as %v", rotation, corner, back)
			}
		}
		if math.Abs(minX) > 1e-9 || math.Abs(minY) > 1e-9 {
			t.Fatalf("rotation %v: drawn page starts at (%v, %v), want its top-left at 0", rotation, minX, minY)
		}
	}
}
