package viewer

import (
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/testpdf"
)

func testTile(page int, scale float64, x, y, size int) *renderedTile {
	return &renderedTile{key: tileKey{page: page, scale: scale, x: x, y: y}, rect: image.Rect(0, 0, size, size), scale: scale}
}

func TestTileCacheEvictsLeastRecentlyUsedByMemory(t *testing.T) {
	cfg := config.Default()
	cfg.PageCacheMemoryMB = 2
	c := tileCache{byteLimit: pageCacheByteLimit(cfg)}
	// Each 512x512 RGBA tile is exactly 1 MiB.
	a, b, d := testTile(0, 1, 0, 0, 512), testTile(1, 1, 0, 0, 512), testTile(2, 1, 0, 0, 512)
	c.add(a)
	c.add(b)
	c.get(a.key) // a is now more recent than b
	c.add(d)
	c.evict()

	if _, ok := c.entries[b.key]; ok {
		t.Fatal("least recently used tile kept past the memory limit")
	}
	if _, ok := c.entries[a.key]; !ok {
		t.Fatal("recently used tile was evicted")
	}
	if c.bytes != 2<<20 {
		t.Fatalf("cache bytes = %d, want %d", c.bytes, 2<<20)
	}
}

func TestTileCacheKeepsProtectedTilesOverLimit(t *testing.T) {
	c := tileCache{byteLimit: 1 << 20}
	a, b := testTile(0, 1, 0, 0, 512), testTile(1, 1, 0, 0, 512)
	c.add(a)
	c.add(b)
	c.protected = map[tileKey]bool{a.key: true, b.key: true}
	c.evict()
	if len(c.entries) != 2 {
		t.Fatalf("entries = %d, want both protected tiles kept", len(c.entries))
	}
}

func TestTileCacheReplacesAndRemovesByKey(t *testing.T) {
	var c tileCache // the zero value is ready to use
	c.add(testTile(0, 1, 0, 0, 512))
	c.add(testTile(0, 1, 0, 0, 256))
	if len(c.entries) != 1 || c.bytes != 256*256*4 {
		t.Fatalf("after replace: entries=%d bytes=%d", len(c.entries), c.bytes)
	}
	c.clear()
	if len(c.entries) != 0 || len(c.byPage) != 0 || c.bytes != 0 || c.lru.Len() != 0 {
		t.Fatalf("after clear: entries=%d pages=%d bytes=%d lru=%d", len(c.entries), len(c.byPage), c.bytes, c.lru.Len())
	}
}

func TestPageTilesDrawSharpestTilesLast(t *testing.T) {
	var c tileCache
	current := testTile(0, 2, 0, 0, 8)
	sharper := testTile(0, 4, 0, 0, 8)
	blurrier := testTile(0, 1, 0, 0, 8)
	stale := testTile(0, 8, 0, 0, 8)
	stale.key.version.gen = -1
	thumb := &renderedTile{key: thumbnailKey(0), rect: image.Rect(0, 0, 8, 8), scale: 0.5}
	for _, tile := range []*renderedTile{current, stale, sharper, thumb, blurrier, testTile(1, 2, 0, 0, 8)} {
		c.add(tile)
	}
	got := c.pageTiles(0, tileVersion{})
	if want := []*renderedTile{thumb, stale, blurrier, current, sharper}; !slices.Equal(got, want) {
		t.Fatalf("draw order = %v, want thumbnail, stale version, then by resolution", got)
	}
}

func TestReloadPlaceholderCleanup(t *testing.T) {
	var c tileCache
	stale, fresh, thumb, gone := testTile(0, 1, 0, 0, 8), testTile(0, 1, 0, 0, 8), &renderedTile{key: thumbnailKey(0)}, testTile(2, 1, 0, 0, 8)
	stale.key.version.rev, fresh.key.version.rev = 0, 1
	for _, tile := range []*renderedTile{stale, fresh, thumb, gone} {
		c.add(tile)
	}

	c.retainPages(2) // the reloaded document has two pages
	if _, ok := c.entries[gone.key]; ok {
		t.Fatal("tile for a page past the new end survived")
	}
	c.dropStale(0, tileVersion{rev: 1})
	if _, ok := c.entries[stale.key]; ok {
		t.Fatal("stale placeholder survived")
	}
	if _, ok := c.entries[fresh.key]; !ok {
		t.Fatal("fresh tile was dropped")
	}
	if _, ok := c.entries[thumb.key]; !ok {
		t.Fatal("thumbnail was dropped; it is repainted rather than replaced")
	}
}

func TestTilesCoveringClipsToPage(t *testing.T) {
	page := image.Rect(10, 20, 2510, 1120) // 2500x1100: 3x2 tiles
	if got := tilesCovering(page, image.Rect(-100, -100, 5000, 5000)); len(got) != 6 {
		t.Fatalf("whole page covered by %d tiles, want 6", len(got))
	}
	got := tilesCovering(page, image.Rect(1100, 30, 1200, 40))
	if want := []image.Point{{1, 0}}; !slices.Equal(got, want) {
		t.Fatalf("tiles = %v, want %v", got, want)
	}
	if r := tileRect(page, 2, 1); r != image.Rect(2058, 1044, 2510, 1120) {
		t.Fatalf("edge tile rect = %v", r)
	}
	if got := tilesCovering(page, image.Rect(3000, 3000, 3100, 3100)); got != nil {
		t.Fatalf("area off the page covered by %v", got)
	}
}

func TestRequestTilePromotesPendingRequest(t *testing.T) {
	key := tileKey{page: 0, scale: 1}
	app := &App{
		tab: &tab{
			documentWorkers: documentWorkers{renderWorker: &renderWorker{}},
			renderService:   renderService{renderPending: map[tileKey]renderRequest{key: {key: key, priority: 10}}},
		},
	}
	if app.requestTile(key, image.Rect(0, 0, 1, 1), 0) {
		t.Fatal("pending request should be promoted rather than enqueued again")
	}
	if got := app.renderPending[key].priority; got != 0 {
		t.Fatalf("pending request priority = %d, want 0", got)
	}
}

func testPrefetchApp(pages int, zoom float64) *App {
	app := testLayoutApp(pages)
	app.winW, app.winH = 1000, 800
	app.zoom = zoom
	app.renderBaseScale = zoom
	app.renderPending = map[tileKey]renderRequest{}
	app.renderWorker = &renderWorker{requests: make(chan renderRequest, 128)}
	app.recomputeLayout(app.viewportSize())
	return app
}

func TestPrefetchQueuesVisibleTilesBeforeLookahead(t *testing.T) {
	app := testPrefetchApp(20, 1)
	app.pageStep = 400 // widens the prefetch margin to several pages
	app.prefetchVisiblePages()
	if len(app.renderPending) == 0 {
		t.Fatal("no visible tiles requested")
	}
	for key, req := range app.renderPending {
		if req.priority != 0 {
			t.Fatalf("queued background render before visible tiles completed: %#v", req)
		}
		app.cache.add(&renderedTile{key: key, rect: req.rect, scale: key.scale})
	}
	app.renderPending = map[tileKey]renderRequest{}
	app.renderWorker.requests = make(chan renderRequest, 128)

	app.prefetchVisiblePages()
	if got := app.pendingBackgroundRenderCount(); got != maxPendingPrefetchRenders {
		t.Fatalf("pending prefetch renders = %d, want %d", got, maxPendingPrefetchRenders)
	}
}

func TestPrefetchStartsMetricLoaderWhenNothingVisibleCanRender(t *testing.T) {
	app := testPrefetchApp(20, 1)
	for i := range app.pageMetrics {
		app.pageMetrics[i].bounds = mupdf.Rect{} // laid out before their metrics load
	}
	app.pendingLoad, app.pendingPages = true, 20
	app.prefetchVisiblePages()
	if app.metricLoader == nil {
		t.Fatal("metric loader still waits for a tile no visible page can give")
	}
	app.metricLoader.Close()
}

func TestPrefetchAtHighZoomRequestsBoundedTiles(t *testing.T) {
	app := testPrefetchApp(3, 40) // a 100x200pt page becomes 4000x8000px
	app.prefetchVisiblePages()
	if len(app.renderPending) == 0 {
		t.Fatal("no tiles requested")
	}
	for key, req := range app.renderPending {
		if req.rect.Dx() > renderTileSize || req.rect.Dy() > renderTileSize {
			t.Fatalf("tile %v is %v, larger than %d", key, req.rect.Size(), renderTileSize)
		}
		if key.scale != 40 {
			t.Fatalf("tile %v not at the render scale", key)
		}
	}
	// A 1000x800 viewport needs at most 2x2 tiles, plus one per axis
	// when they straddle tile boundaries.
	if n := len(app.renderPending); n > 9 {
		t.Fatalf("requested %d visible tiles for a 1000x800 viewport", n)
	}
}

func TestVisibleRequestPreemptsPreviouslyVisibleRender(t *testing.T) {
	old, visible := tileKey{page: 0}, tileKey{page: 1}
	worker := &renderWorker{slots: []*renderSlot{{}}}
	worker.slots[0].rendering.Store(&old)
	app := &App{
		tab: &tab{
			documentWorkers: documentWorkers{renderWorker: worker},
			renderService: renderService{
				renderPending: map[tileKey]renderRequest{
					old:     {generation: 2, key: old},
					visible: {generation: 2, key: visible},
				},
				renderGeneration: 2,
			},
		},
	}

	app.preemptNonVisibleRender(map[tileKey]bool{visible: true})
	if _, ok := app.renderPending[old]; ok {
		t.Fatal("preempted render remained pending")
	}
	if _, ok := app.renderPending[visible]; !ok {
		t.Fatal("visible render was removed")
	}
}

func TestRenderWorkerPoolRendersEveryRequest(t *testing.T) {
	pages := make([][]string, 6)
	for i := range pages {
		pages[i] = []string{"page"}
	}
	doc, err := mupdf.Open(testpdf.WritePages(t, pages...), mupdf.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	w := newRenderWorker(doc, 3, nil)
	defer w.Close()
	if len(w.slots) != 3 {
		t.Fatalf("slots = %d, want 3", len(w.slots))
	}
	for page := range pages {
		if !w.Enqueue(renderRequest{key: tileKey{page: page, scale: 0.5}, scale: 0.5, rect: image.Rect(0, 0, renderTileSize, renderTileSize)}) {
			t.Fatalf("enqueue page %d failed", page)
		}
	}
	seen := map[int]bool{}
	timeout := time.After(5 * time.Second)
	for len(seen) < len(pages) {
		select {
		case update := <-w.updates:
			if update.err != nil {
				t.Fatal(update.err)
			}
			update.rendered.Close()
			if seen[update.request.key.page] {
				t.Fatalf("page %d rendered twice", update.request.key.page)
			}
			seen[update.request.key.page] = true
		case <-timeout:
			t.Fatalf("rendered %d of %d pages", len(seen), len(pages))
		}
	}
}

func TestRenderWorkerFreesUncollectedTilesOnClose(t *testing.T) {
	pages := make([][]string, 2*maxPendingPrefetchRenders)
	for i := range pages {
		pages[i] = []string{"page"}
	}
	doc, err := mupdf.Open(testpdf.WritePages(t, pages...), mupdf.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	w := newRenderWorker(doc, 2, nil)
	for page := range pages {
		w.Enqueue(renderRequest{key: tileKey{page: page, scale: 0.5}, scale: 0.5, rect: image.Rect(0, 0, renderTileSize, renderTileSize)})
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(w.updates) < cap(w.updates) {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d updates queued", len(w.updates), cap(w.updates))
		}
		time.Sleep(time.Millisecond)
	}
	w.Close()
	<-w.done
	if n := len(w.updates); n != 0 {
		t.Fatalf("%d rendered tiles left in the closed worker's queue", n)
	}
}

func TestCancelNotVisibleChecksEverySlot(t *testing.T) {
	visible, offscreen := tileKey{page: 0}, tileKey{page: 4}
	w := &renderWorker{slots: []*renderSlot{{}, {}, {}}}
	w.slots[0].rendering.Store(&visible)
	w.slots[1].rendering.Store(&offscreen)
	if got := w.CancelNotVisible(map[tileKey]bool{visible: true}); !slices.Equal(got, []tileKey{offscreen}) {
		t.Fatalf("cancelled = %v, want [%v]", got, offscreen)
	}
}

func TestRenderScalePolicy(t *testing.T) {
	if validRenderScale(0) || validRenderScale(math.NaN()) || validRenderScale(math.Inf(1)) {
		t.Fatal("expected zero, NaN, and infinity to be invalid render scales")
	}
	if !validRenderScale(0.5) {
		t.Fatal("expected positive finite scale to be valid")
	}

	app := &App{tab: &tab{renderService: renderService{minRenderBaseScale: math.NaN()}}, config: config.Config{RenderOversample: math.NaN()}}
	assertClose(t, app.renderScaleFloor(), defaultMinRenderBaseScale)
	assertClose(t, app.renderOversampleFactor(), defaultRenderOversample)
	assertClose(t, app.oversampledRenderScale(math.NaN()), 1)

	app = &App{tab: &tab{viewStateFields: viewStateFields{scale: 1, zoom: 1, fitMode: fitManual}, renderService: renderService{minRenderBaseScale: 0.25, renderBaseScale: 2, renderPending: map[tileKey]renderRequest{{page: 1}: {key: tileKey{page: 1}}}}}, config: config.Config{RenderOversample: 1}}
	if !app.applyRenderBaseScaleTarget(app.oversampledRenderScale(4)) {
		t.Fatal("expected target above tolerance to upgrade render base scale")
	}
	assertClose(t, app.renderBaseScale, 4)
	if app.renderGeneration != 1 || len(app.renderPending) != 0 {
		t.Fatalf("expected upgrade to invalidate render requests, generation=%d pending=%d", app.renderGeneration, len(app.renderPending))
	}

	app.settleRenderScale() // back to the view's own scale, 1
	assertClose(t, app.renderBaseScale, 1)
	if app.renderGeneration != 2 {
		t.Fatalf("expected downgrade to invalidate render requests, generation=%d", app.renderGeneration)
	}
}

func TestRenderScaleTargetDowngradesAfterModestZoomOut(t *testing.T) {
	app := &App{tab: &tab{viewStateFields: viewStateFields{scale: 1, zoom: 1, fitMode: fitManual}, renderService: renderService{minRenderBaseScale: 0.25, renderBaseScale: 1.8}}, config: config.Config{RenderOversample: 1}}

	app.scheduleRenderScaleTarget(1.3)
	app.renderScaleReadyAt = time.Now().Add(-time.Millisecond)
	if !app.applyScheduledRenderScaleTarget() {
		t.Fatal("expected zooming out to 1.3 from 1.8 to re-render")
	}
	assertClose(t, app.renderBaseScale, 1.3)

	app.scheduleRenderScaleTarget(1.25) // within the headroom, so the tiles stay
	app.renderScaleReadyAt = time.Now().Add(-time.Millisecond)
	if app.applyScheduledRenderScaleTarget() {
		t.Fatal("expected a slight zoom out to keep the render scale")
	}
	assertClose(t, app.renderBaseScale, 1.3)
}

func TestRenderBaseScaleOversamplesOnce(t *testing.T) {
	app := &App{tab: &tab{viewStateFields: viewStateFields{scale: 1, zoom: 1, fitMode: fitManual}, renderService: renderService{minRenderBaseScale: 0.25}}, config: config.Config{RenderOversample: 2}}
	app.ensureRenderBaseScale()
	assertClose(t, app.renderBaseScale, 2)
}

func TestResetRenderScaleUsesRestoredView(t *testing.T) {
	app := &App{tab: &tab{viewStateFields: viewStateFields{scale: 1, zoom: 1, fitMode: fitManual}, renderService: renderService{minRenderBaseScale: 0.25}}, config: config.Config{RenderOversample: 1}}
	app.scheduleRenderScaleTarget(1) // fixes the base before the saved view is restored
	app.scale, app.zoom = 3, 3
	app.scheduleRenderScaleTarget(3)
	app.resetRenderScale()
	assertClose(t, app.renderBaseScale, 3)
	if !app.renderScaleReadyAt.IsZero() {
		t.Fatal("expected the pending upgrade to be dropped")
	}
}

func TestRenderScaleForAllowsLowZoomUndersampling(t *testing.T) {
	app := &App{tab: &tab{renderService: renderService{minRenderBaseScale: 0.25, renderBaseScale: 1}}, config: config.Config{RenderOversample: 1}}

	assertClose(t, app.renderScaleFor(1), 1)
	assertClose(t, app.renderScaleFor(0.2), 0.4)
	assertClose(t, app.renderScaleFor(0.05), 0.25)
}

func TestRenderScaleTargetDebouncesFastZoom(t *testing.T) {
	app := &App{tab: &tab{viewStateFields: viewStateFields{scale: 1, zoom: 1, fitMode: fitManual}, renderService: renderService{minRenderBaseScale: 0.25, renderBaseScale: 1, renderPending: map[tileKey]renderRequest{{page: 1}: {key: tileKey{page: 1}}}}}, config: config.Config{RenderOversample: 1}}

	app.scheduleRenderScaleTarget(2)
	app.scheduleRenderScaleTarget(3)
	if app.applyScheduledRenderScaleTarget() {
		t.Fatal("render scale target applied before settle delay")
	}
	assertClose(t, app.renderBaseScale, 1)
	if app.renderGeneration != 0 || len(app.renderPending) != 1 {
		t.Fatalf("unexpected early invalidation generation=%d pending=%d", app.renderGeneration, len(app.renderPending))
	}

	app.renderScaleReadyAt = time.Now().Add(-time.Millisecond)
	if !app.applyScheduledRenderScaleTarget() {
		t.Fatal("expected settled render scale target to apply")
	}
	assertClose(t, app.renderBaseScale, 3)
	if app.renderGeneration != 1 || len(app.renderPending) != 0 {
		t.Fatalf("expected settled target to invalidate once, generation=%d pending=%d", app.renderGeneration, len(app.renderPending))
	}
}

func TestRenderWorkerPrioritizesVisibleRequests(t *testing.T) {
	w := &renderWorker{}
	w.generation.Store(2)
	queue := []renderRequest{
		{generation: 2, key: tileKey{page: 10}, priority: 10},
		{generation: 1, key: tileKey{page: 1}, priority: 0},
		{generation: 2, key: tileKey{page: 3}, priority: 0},
	}

	req, queue, ok := w.popNextRequest(queue)
	if !ok || req.key.page != 3 {
		t.Fatalf("expected current-generation visible request, got %#v ok=%v", req, ok)
	}
	req, queue, ok = w.popNextRequest(queue)
	if !ok || req.key.page != 10 {
		t.Fatalf("expected prefetch request after visible request, got %#v ok=%v", req, ok)
	}
	_, _, ok = w.popNextRequest(queue)
	if ok {
		t.Fatal("expected stale-only queue to have no request")
	}
}

func TestRenderWorkerPromotesVisiblePrefetchRequest(t *testing.T) {
	w := &renderWorker{}
	w.generation.Store(2)
	w.SetVisible(map[tileKey]bool{{page: 10}: true})
	queue := []renderRequest{
		{generation: 2, key: tileKey{page: 3}, priority: 0},
		{generation: 2, key: tileKey{page: 10}, priority: 10},
	}

	req, _, ok := w.popNextRequest(queue)
	if !ok || req.key.page != 10 {
		t.Fatalf("expected visible prefetch request to be promoted, got %#v ok=%v", req, ok)
	}
}

func TestRenderWorkerSkipsUnwantedRequests(t *testing.T) {
	w := &renderWorker{}
	w.generation.Store(2)
	w.SetWanted(map[tileKey]bool{{page: 5}: true})
	queue := []renderRequest{
		{generation: 2, key: tileKey{page: 3}, priority: 0},
		{generation: 2, key: tileKey{page: 5}, priority: 10},
	}

	req, queue, ok := w.popNextRequest(queue)
	if !ok || req.key.page != 5 {
		t.Fatalf("expected only wanted page to render, got %#v ok=%v", req, ok)
	}
	_, _, ok = w.popNextRequest(queue)
	if ok {
		t.Fatal("expected unwanted page to be skipped")
	}
}

func TestAltColorsToggleKeepsTilesUntilReplaced(t *testing.T) {
	app := testPrefetchApp(1, 1)
	app.prefetchVisiblePages()
	for key, req := range app.renderPending {
		app.cache.add(&renderedTile{key: key, rect: req.rect, scale: key.scale})
	}
	shown := len(app.cache.pageTiles(0, app.tileVersion(0)))

	app.setAltColors(true)
	if got := len(app.cache.pageTiles(0, app.tileVersion(0))); got != shown || shown == 0 {
		t.Fatalf("page draws %d tiles after the toggle, want the %d from before", got, shown)
	}
	app.renderWorker.requests = make(chan renderRequest, 128)
	app.prefetchVisiblePages()
	if len(app.renderPending) != shown {
		t.Fatalf("%d renders queued, want %d to replace the old tiles", len(app.renderPending), shown)
	}
	for key, req := range app.renderPending {
		if key.version != app.tileVersion(0) || !req.altColors {
			t.Fatalf("queued %+v, want the new colours", req)
		}
	}
}

func TestRenderWorkerDeliversPageLinksWithTile(t *testing.T) {
	path := testpdf.WriteObjects(t, []string{
		"<</Type/Catalog/Pages 2 0 R>>",
		"<</Type/Pages/Kids[3 0 R]/Count 1>>",
		"<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Annots[4 0 R]>>",
		"<</Type/Annot/Subtype/Link/Rect[72 700 200 720]/A<</S/URI/URI(https://example.com)>>>>",
	})
	doc, err := mupdf.Open(path, mupdf.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	w := newRenderWorker(doc, 1, nil)
	defer w.Close()

	app := &App{tab: &tab{documentWorkers: documentWorkers{renderWorker: w}, renderService: renderService{renderPending: map[tileKey]renderRequest{}}}}
	key := tileKey{scale: 0.5}
	app.queueRender(renderRequest{key: key, scale: 0.5, rect: image.Rect(0, 0, 306, 396)}, 0)
	if !app.renderPending[key].links {
		t.Fatal("first tile of a page did not ask for its links")
	}
	select {
	case update := <-w.updates:
		defer update.rendered.Close()
		if len(update.links) != 1 || update.links[0].URI != "https://example.com" {
			t.Fatalf("links = %+v, want the page's one link", update.links)
		}
		app.storeLinks(0, update.links)
	case <-time.After(5 * time.Second):
		t.Fatal("no render update")
	}
	next := tileKey{scale: 1}
	app.queueRender(renderRequest{key: next, scale: 1, rect: image.Rect(0, 0, 612, 792)}, 0)
	if app.renderPending[next].links {
		t.Fatal("links asked for again once loaded")
	}
}
