package viewer

import (
	"errors"
	"image"
	"math"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"
)

type renderService struct {
	cache              tileCache
	renderPending      map[tileKey]renderRequest
	renderBaseScale    float64
	renderScaleTarget  float64
	renderScaleReadyAt time.Time
	minRenderBaseScale float64
	renderGeneration   int
	pageRevisions      map[int]int // edits per page, so edited pages re-render
	tileStyle          int         // changes of settings rendered into tiles, so tiles re-render
	loaderVisible      bool        // a loading indicator was drawn this frame
}

func (a *App) initRenderWorker() {
	a.logf("start render worker path=%q", remote.Display(a.docPath))
	a.renderPending = map[tileKey]renderRequest{}
	a.renderWorker = newRenderWorker(a.doc, renderThreadCount(a.config), a.wakeLoop)
	a.renderWorker.SetGeneration(a.renderGeneration)
}

func (a *App) pollRenderUpdates() {
	if a.renderWorker == nil {
		return
	}
	for {
		select {
		case update := <-a.renderWorker.updates:
			if update.rendered != nil {
				defer update.rendered.Close()
			}
			a.acceptRenderUpdate(update)
		default:
			return
		}
	}
}

func (a *App) acceptRenderUpdate(update renderUpdate) {
	req := update.request
	if req.links && update.err == nil {
		a.storeLinks(req.key.page, update.links) // the links do not depend on the render
	}
	if _, pending := a.renderPending[req.key]; !pending || req.generation != a.renderGeneration {
		return
	}
	delete(a.renderPending, req.key)
	if errors.Is(update.err, mupdf.ErrCancelled) {
		return // requested again if still wanted
	}
	if update.err != nil {
		a.logf("render update failed err=%v", update.err)
		a.message = update.err.Error()
		return
	}
	img := update.rendered.Image
	if img.Bounds().Empty() {
		return
	}
	tex, err := textureFromRGBA(a.renderer, img)
	if err != nil {
		a.logf("render texture failed page=%d err=%v", req.key.page+1, err)
		a.message = err.Error()
		return
	}
	origin := image.Pt(update.rendered.X, update.rendered.Y)
	tile := &renderedTile{key: req.key, texture: tex, rect: img.Bounds().Add(origin), scale: req.scale}
	if req.key.thumb {
		a.replaceThumbnail(tile, req.version)
		a.pendingRedraw = true
		return
	}
	a.cache.add(tile)
	a.updateThumbnail(tile)
	if !a.pagePending(req.key.page) {
		a.cache.dropStale(req.key.page, req.key.version)
	}
	a.cache.evict()
	a.startPendingMetricLoader()
	a.pendingRedraw = true
}

// requestTile queues a render of the tile at key unless it is cached or
// already queued, in which case a more urgent priority is kept. It reports
// whether a new render was queued.
func (a *App) requestTile(key tileKey, rect image.Rectangle, priority int) bool {
	if _, ok := a.cache.get(key); ok {
		return false
	}
	return a.queueRender(renderRequest{key: key, scale: key.scale, rect: rect}, priority)
}

// queueRender queues req unless the same key is pending, in which case a
// more urgent priority is kept.
func (a *App) queueRender(req renderRequest, priority int) bool {
	if a.renderWorker == nil {
		return false
	}
	if pending, ok := a.renderPending[req.key]; ok {
		if priority < pending.priority {
			pending.priority = priority
			a.renderPending[req.key] = pending
		}
		return false
	}
	req.generation = a.renderGeneration
	req.priority = priority
	if _, loaded := a.loadedLinks(req.key.page); !loaded && !req.key.thumb {
		req.links = true
	}
	req.altColors = a.altColors
	req.aaLevel = a.config.AntiAliasing
	if req.altColors {
		req.altBackground, req.altForeground = a.config.Theme.Alt.Page, a.config.Theme.Alt.Foreground
		req.keepImages = a.config.AltColorsKeepImages
	}
	if !a.renderWorker.Enqueue(req) {
		a.logf("render enqueue skipped page=%d tile=%d,%d", req.key.page+1, req.key.x, req.key.y)
		return false
	}
	a.renderPending[req.key] = req
	return true
}

func (a *App) tileVersion(page int) tileVersion {
	return tileVersion{gen: a.generation, rev: a.pageRevisions[page], style: a.tileStyle}
}

// restyleTiles re-renders every tile after a setting rendered into them
// changes, such as the alternate colours, keeping the current tiles on
// screen until their replacements arrive.
func (a *App) restyleTiles() {
	a.tileStyle++
	a.invalidateRenderRequests()
	a.pendingRedraw = true
}

func (a *App) pagePending(page int) bool {
	for key := range a.renderPending {
		if key.page == page {
			return true
		}
	}
	return false
}

func (a *App) hasPendingVisibleRender() bool {
	for _, req := range a.renderPending {
		if req.generation == a.renderGeneration && req.priority <= 0 {
			return true
		}
	}
	return false
}

func (a *App) pendingBackgroundRenderCount() int {
	count := 0
	for _, req := range a.renderPending {
		if req.generation == a.renderGeneration && req.priority > 0 {
			count++
		}
	}
	return count
}

func (a *App) invalidateRenderRequests() {
	a.renderGeneration++
	a.renderPending = map[tileKey]renderRequest{}
	if a.renderWorker != nil {
		a.renderWorker.SetGeneration(a.renderGeneration)
	}
}

func (a *App) renderScaleFor(layoutScale float64) float64 {
	if a.renderBaseScale <= 0 {
		a.ensureRenderBaseScale()
	}
	if a.renderBaseScale <= 0 {
		return math.Max(1, layoutScale)
	}
	if layoutScale < 0.1 {
		return math.Max(layoutScale, a.minRenderBaseScale)
	}
	if layoutScale < a.renderBaseScale/4 {
		return math.Max(layoutScale*2, a.minRenderBaseScale)
	}
	return a.renderBaseScale
}

func (a *App) clearCache() {
	a.cache.clear()
	a.invalidateRenderRequests()
}

const (
	defaultMinRenderBaseScale = 0.25
	defaultRenderOversample   = 1
	maxPendingPrefetchRenders = 4
	renderPrefetchPriority    = 10
	renderUpgradeTolerance    = 0.95
	// renderDowngradeHeadroom is how far tiles may be scaled down before they
	// are re-rendered; linear filtering has no mipmaps, so shrinking much
	// past this aliases thin strokes such as text diagonals.
	renderDowngradeHeadroom = 1.05
	renderScaleSettleDelay  = 75 * time.Millisecond
)

func validRenderScale(v float64) bool {
	return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func pageCacheByteLimit(cfg config.Config) int64 {
	return int64(cfg.PageCacheMemoryMB) << 20
}

func (rs *renderService) renderScaleFloor() float64 {
	if validRenderScale(rs.minRenderBaseScale) {
		return rs.minRenderBaseScale
	}
	return defaultMinRenderBaseScale
}

func (a *App) renderOversampleFactor() float64 {
	if validRenderScale(a.config.RenderOversample) {
		return a.config.RenderOversample
	}
	return defaultRenderOversample
}

func (a *App) oversampledRenderScale(scale float64) float64 {
	if !validRenderScale(scale) {
		scale = 1
	}
	return math.Max(scale*a.renderOversampleFactor(), a.renderScaleFloor())
}

func (a *App) currentRenderTarget() float64 {
	target := a.scale
	if !validRenderScale(target) {
		target = 1
	}
	if a.fitMode != fitManual && validRenderScale(a.zoom) {
		target = math.Max(target, a.zoom)
	}
	return a.oversampledRenderScale(target)
}

func (a *App) ensureRenderBaseScale() {
	floor := a.renderScaleFloor()
	if validRenderScale(a.renderBaseScale) {
		if a.renderBaseScale < floor {
			a.renderBaseScale = floor
		}
		return
	}
	a.renderBaseScale = a.currentRenderTarget()
}

// resetRenderScale renders at the current view's scale from now on, for a
// document just installed with no tiles to keep consistent with; called once
// the view is restored so the first frames do not render at a stale scale.
func (a *App) resetRenderScale() {
	a.renderScaleTarget, a.renderScaleReadyAt = 0, time.Time{}
	a.renderBaseScale = a.currentRenderTarget()
}

func (a *App) scheduleRenderScaleTarget(target float64) {
	a.ensureRenderBaseScale()
	if !validRenderScale(target) {
		return
	}
	target = a.oversampledRenderScale(target)
	if target <= a.renderBaseScale*renderUpgradeTolerance && target*renderDowngradeHeadroom >= a.renderBaseScale {
		a.renderScaleTarget = 0
		a.renderScaleReadyAt = time.Time{}
		return
	}
	if math.Abs(target-a.renderScaleTarget) < 0.01 && !a.renderScaleReadyAt.IsZero() {
		return
	}
	a.renderScaleTarget = target
	a.renderScaleReadyAt = time.Now().Add(renderScaleSettleDelay)
	a.wakeAfter(renderScaleSettleDelay)
}

func (a *App) applyScheduledRenderScaleTarget() bool {
	if !validRenderScale(a.renderScaleTarget) || a.renderScaleReadyAt.IsZero() || time.Now().Before(a.renderScaleReadyAt) {
		return false
	}
	target := a.renderScaleTarget
	a.renderScaleTarget = 0
	a.renderScaleReadyAt = time.Time{}
	return a.applyRenderBaseScaleTarget(target)
}

func (a *App) applyRenderBaseScaleTarget(target float64) bool {
	a.ensureRenderBaseScale()
	if !validRenderScale(target) {
		return false
	}
	floor := a.renderScaleFloor()
	if target > a.renderBaseScale*renderUpgradeTolerance {
		next := math.Max(target, floor)
		if next > a.renderBaseScale+0.01 {
			a.renderBaseScale = next
			a.logf("upgrade render scale target=%.3f base=%.3f", target, next)
			a.invalidateRenderRequests()
			return true
		}
	}
	if target*renderDowngradeHeadroom < a.renderBaseScale {
		next := math.Max(target, floor)
		if next < a.renderBaseScale {
			a.renderBaseScale = next
			a.logf("downgrade render scale target=%.3f base=%.3f", target, next)
			a.invalidateRenderRequests()
			return true
		}
	}
	return false
}

// settleRenderScale applies the render scale for the current view at once,
// skipping the delay that lets a gradual zoom settle first; for jumps in
// scale such as entering or leaving the overview.
func (a *App) settleRenderScale() {
	a.renderScaleTarget, a.renderScaleReadyAt = 0, time.Time{}
	a.applyRenderBaseScaleTarget(a.currentRenderTarget())
}

func (a *App) adjustRenderBaseScaleForExtremeZoom(layoutScale float64) {
	if a.overview != nil {
		return // the overview draws thumbnails, so the reading view's scale stays
	}
	a.scheduleRenderScaleTarget(layoutScale)
	if a.applyScheduledRenderScaleTarget() {
		return
	}
}
