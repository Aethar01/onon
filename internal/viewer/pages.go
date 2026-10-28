package viewer

import (
	"image"
	"image/color"
	"math"
	"slices"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) drawPages(renderer *sdl.Renderer) {
	a.loaderVisible = false
	if a.renderMode == renderSingle {
		a.drawSinglePage(renderer)
		return
	}
	a.drawContinuousPages(renderer)
}

func (a *App) drawContinuousPages(renderer *sdl.Renderer) {
	viewportW, viewportH := a.viewportSize()
	margin := a.renderMargin()
	minY := a.scrollY - margin
	maxY := a.scrollY + float64(viewportH) + margin
	a.forEachContinuousPage(minY, maxY, func(page int, x, y, width, height float64) {
		if x+width < 0 || x > float64(viewportW) || y+height < 0 || y > float64(viewportH) {
			return
		}
		a.drawPage(renderer, page, x, y, width, height)
	})
	a.drawSelection(renderer)
	a.drawOverviewSelection(renderer)
}

func (a *App) drawSinglePage(renderer *sdl.Renderer) {
	if len(a.rows) == 0 || a.page < 0 || a.page >= len(a.pageToRow) {
		return
	}
	viewportW, viewportH := a.viewportSize()
	row := a.rows[a.pageToRow[a.page]]
	for i, page := range row.pages {
		x, y := a.rowPageScreenOrigin(row, i)
		if x+row.pageW[i] < 0 || x > float64(viewportW) || y+row.pageH[i] < 0 || y > float64(viewportH) {
			continue
		}
		a.drawPage(renderer, page, x, y, row.pageW[i], row.pageH[i])
	}
	a.drawSelection(renderer)
}

func (a *App) drawPage(renderer *sdl.Renderer, page int, x, y, width, height float64) {
	// A page turned by quarter turns is styled as the page element; the
	// style's box would not fit one turned otherwise.
	st := a.style(config.ElementPage)
	styled := math.Mod(normalizeRotation(a.rotation), 90) == 0
	box := sdl.FRect{X: float32(x), Y: float32(y), W: float32(width), H: float32(height)}
	if styled {
		fill := st
		fill.Shadow.V, fill.BorderWidth.V = config.Shadow{}, 0
		a.drawBox(renderer, &fill, box)
		defer a.finishPageBox(renderer, &st, box)
	} else {
		_ = a.drawPageBackground(renderer, x, y, page)
	}
	viewportW, viewportH := a.viewportSize()
	tiles := a.cache.pageTiles(page, a.tileVersion(page))
	if a.overview != nil && len(tiles) > 0 {
		tiles = tiles[:1] // the overview shows thumbnails only, which sort first
		if !tiles[0].key.thumb {
			tiles = nil
		}
	}
	for _, tile := range tiles {
		a.drawTile(renderer, tile, x, y, viewportW, viewportH)
	}
	if len(tiles) == 0 && a.config.LoadingIndicator && a.pagePending(page) {
		a.drawInkLoader(renderer, x, y, width, height, time.Since(startTime))
		a.loaderVisible = true
	}
	a.drawSearchHighlightsForPage(renderer, page, x, y)
}

// finishPageBox draws what goes over a page: the canvas over the corners
// its shape leaves, then its shadow, cut away where the page is so it
// falls only beside it, then its border.
func (a *App) finishPageBox(renderer *sdl.Renderer, st *config.Style, rect sdl.FRect) {
	box := pixelRect(rect)
	if box.W <= 0 || box.H <= 0 {
		return
	}
	shape := a.boxShape(st, box)
	if shape.Kind != "rect" || shape.radius != [4]float32{} {
		a.drawMask(renderer, maskSpec{shape: shape, w: box.W, h: box.H, invert: true}, box.X, box.Y, a.backgroundColor())
	}
	layers := st.Shadow.V.List()
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		clr := a.styleColor(layer.Color, st.Opacity.V)
		if clr.A == 0 {
			continue
		}
		dx, dy := int32(math.Round(float64(a.px(layer.X)))), int32(math.Round(float64(a.px(layer.Y))))
		spec := maskSpec{shape: shape, w: box.W, h: box.H, spread: a.px(layer.Spread), blur: a.px(layer.Blur), cut: true, cutX: float32(-dx), cutY: float32(-dy)}
		a.drawMask(renderer, spec, box.X+dx, box.Y+dy, clr)
	}
	a.drawDefaultBox(renderer, st, rect, boxBorder)
}

// drawTile draws a tile of the page whose screen origin is (x, y). The
// tile is rotated about its own centre, placed where that centre falls on
// the rotated page.
func (a *App) drawTile(renderer *sdl.Renderer, tile *renderedTile, x, y float64, viewportW, viewportH int) {
	dst, ok := a.tileRect(tile, x, y, viewportW, viewportH)
	if !ok {
		return
	}
	if normalizeRotation(a.rotation) == 0 {
		sdl.RenderTexture(renderer, tile.texture, nil, &dst)
		return
	}
	sdl.RenderTextureRotated(renderer, tile.texture, nil, &dst, a.rotation, nil, sdl.FlipNone)
}

// tileRect is where a tile of the page whose screen origin is (x, y) is
// drawn before it is rotated, and false if it is off screen.
func (a *App) tileRect(tile *renderedTile, x, y float64, viewportW, viewportH int) (sdl.FRect, bool) {
	return tileRectAt(tile, a.pageTransform(tile.key.page), a.scale, x, y, viewportW, viewportH)
}

// tileRectAt is tileRect for a page drawn by t at scale.
func tileRectAt(tile *renderedTile, t pageTransform, scale, x, y float64, viewportW, viewportH int) (sdl.FRect, bool) {
	drawScale := scale / tile.scale
	drawW := float64(tile.rect.Dx()) * drawScale
	drawH := float64(tile.rect.Dy()) * drawScale
	pageX := (float64(tile.rect.Min.X) + float64(tile.rect.Dx())/2) / tile.scale
	pageY := (float64(tile.rect.Min.Y) + float64(tile.rect.Dy())/2) / tile.scale
	dx, dy := t.toScreen(pageX, pageY)
	centerX, centerY := x+dx, y+dy
	if radius := math.Max(drawW, drawH) / 2; centerX+radius < 0 || centerY+radius < 0 || centerX-radius > float64(viewportW) || centerY-radius > float64(viewportH) {
		return sdl.FRect{}, false
	}
	return sdl.FRect{
		X: float32(centerX - drawW/2),
		Y: float32(centerY - drawH/2),
		W: float32(drawW),
		H: float32(drawH),
	}, true
}

func (a *App) drawPageBackground(renderer *sdl.Renderer, x, y float64, page int) error {
	clr := a.pageBackgroundColor()
	if normalizeRotation(a.rotation) == 0 {
		m := a.pageMetrics[page]
		return fillRect(renderer, sdl.FRect{X: float32(x), Y: float32(y), W: float32(m.width * a.scale), H: float32(m.height * a.scale)}, clr)
	}
	return renderBool(sdl.RenderGeometry(renderer, nil, pageBackgroundVertices(x, y, a.pageMetrics[page].bounds, a.scale, a.rotation, clr), []int32{0, 1, 2, 1, 3, 2}), "render geometry")
}

func pageBackgroundVertices(x, y float64, bounds mupdf.Rect, scale, rotation float64, clr color.RGBA) []sdl.Vertex {
	t := newPageTransform(bounds, scale, rotation)
	points := []mupdf.Point{
		{X: float64(bounds.X0), Y: float64(bounds.Y0)},
		{X: float64(bounds.X1), Y: float64(bounds.Y0)},
		{X: float64(bounds.X0), Y: float64(bounds.Y1)},
		{X: float64(bounds.X1), Y: float64(bounds.Y1)},
	}
	vertices := make([]sdl.Vertex, len(points))
	color := sdl.FColor{R: float32(clr.R) / 255, G: float32(clr.G) / 255, B: float32(clr.B) / 255, A: float32(clr.A) / 255}
	for i, point := range points {
		dx, dy := t.toScreen(point.X, point.Y)
		vertices[i] = sdl.Vertex{
			Position: sdl.FPoint{X: float32(x + dx), Y: float32(y + dy)},
			Color:    color,
		}
	}
	return vertices
}

// tileCandidate is a tile that the current view wants rendered.
type tileCandidate struct {
	key      tileKey
	rect     image.Rectangle
	distance int // device pixels from the viewport; 0 when on screen
}

// prefetchVisiblePages requests the tiles covering the viewport, then the
// nearest tiles within the prefetch margin, and tells the render worker
// which tiles are still wanted.
func (a *App) prefetchVisiblePages() {
	if len(a.rows) == 0 {
		return
	}
	if a.overview != nil {
		a.prefetchOverviewThumbnails()
		return
	}
	viewportW, viewportH := a.viewportSize()
	viewport := sdl.FRect{W: float32(viewportW), H: float32(viewportH)}
	margin := 0.0
	if a.renderMode != renderSingle {
		margin = math.Max(a.renderMargin()*2, float64(viewportH))
	}
	area := sdl.FRect{Y: float32(-margin), W: viewport.W, H: viewport.H + float32(2*margin)}
	scale := a.renderScaleFor(a.scale)

	var visible, prefetch []tileCandidate
	a.forEachDisplayedPage(margin, func(page int, x, y float64) {
		pageRect := mupdf.DeviceRect(a.pageMetrics[page].bounds, scale)
		onScreen := a.pageDeviceArea(page, x, y, viewport, scale)
		for _, pt := range tilesCovering(pageRect, a.pageDeviceArea(page, x, y, area, scale)) {
			rect := tileRect(pageRect, pt.X, pt.Y)
			c := tileCandidate{key: tileKey{page: page, scale: scale, x: pt.X, y: pt.Y, version: a.tileVersion(page)}, rect: rect, distance: rectDistance(rect, onScreen)}
			if c.distance == 0 {
				visible = append(visible, c)
			} else {
				prefetch = append(prefetch, c)
			}
		}
	})

	wanted := make(map[tileKey]bool, len(visible)+len(prefetch))
	onScreen := make(map[tileKey]bool, len(visible))
	protected := make(map[tileKey]bool, 2*len(visible))
	for _, c := range visible {
		wanted[c.key], onScreen[c.key] = true, true
		protected[c.key], protected[thumbnailKey(c.key.page)] = true, true
	}
	for _, c := range prefetch {
		wanted[c.key] = true
	}
	a.addCanvasRenders(wanted, onScreen, protected)
	a.cache.protected = protected
	if a.renderWorker != nil {
		a.renderWorker.SetWanted(wanted)
		a.renderWorker.SetVisible(onScreen)
		a.renderWorker.DrainUnwanted(a.renderGeneration)
	}
	for key := range a.renderPending {
		if !wanted[key] {
			delete(a.renderPending, key)
		}
	}

	for _, c := range visible {
		a.requestTile(c.key, c.rect, 0)
	}
	a.requestCanvasRenders()
	if a.hasPendingVisibleRender() {
		a.preemptNonVisibleRender(onScreen)
		return
	}
	slices.SortStableFunc(prefetch, func(x, y tileCandidate) int { return x.distance - y.distance })
	remaining := maxPendingPrefetchRenders - a.pendingBackgroundRenderCount()
	for i, c := range prefetch {
		if remaining <= 0 {
			break
		}
		if a.requestTile(c.key, c.rect, renderPrefetchPriority+i) {
			remaining--
		}
	}
}

// forEachDisplayedPage visits the pages shown in the viewport extended
// vertically by margin, with their screen origins.
func (a *App) forEachDisplayedPage(margin float64, visit func(page int, x, y float64)) {
	if a.renderMode == renderSingle {
		if a.page < 0 || a.page >= len(a.pageToRow) {
			return
		}
		row := a.rows[a.pageToRow[a.page]]
		for i, page := range row.pages {
			x, y := a.rowPageScreenOrigin(row, i)
			visit(page, x, y)
		}
		return
	}
	_, viewportH := a.viewportSize()
	a.forEachContinuousPage(a.scrollY-margin, a.scrollY+float64(viewportH)+margin, func(page int, x, y, _, _ float64) {
		visit(page, x, y)
	})
}

// rectDistance is the gap between two rects, summed over both axes.
func rectDistance(a, b image.Rectangle) int {
	gap := func(a0, a1, b0, b1 int) int { return max(0, b0-a1, a0-b1) }
	return gap(a.Min.X, a.Max.X, b.Min.X, b.Max.X) + gap(a.Min.Y, a.Max.Y, b.Min.Y, b.Max.Y)
}

func (a *App) preemptNonVisibleRender(visible map[tileKey]bool) {
	if a.renderWorker == nil {
		return
	}
	for _, key := range a.renderWorker.CancelNotVisible(visible) {
		delete(a.renderPending, key)
	}
}
