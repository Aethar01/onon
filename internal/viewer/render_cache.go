package viewer

import (
	"cmp"
	"container/list"
	"image"
	"math"
	"slices"

	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Pages render as a grid of tiles at the shared render scale, so no texture
// exceeds renderTileSize on a side however far the view is zoomed. Each page
// also keeps a low-resolution thumbnail, painted from its tiles as they
// arrive, that stands in while tiles at a new scale are rendering.
const (
	renderTileSize    = 1024
	thumbnailLongSide = 768
)

// tileKey identifies a cached texture: tile (x, y) of page at scale showing
// content version, or the page's thumbnail when thumb is set. Tiles of an
// older version stay as placeholders until fresh ones replace them.
type tileKey struct {
	page    int
	scale   float64
	x, y    int
	version tileVersion
	thumb   bool
}

// tileVersion is the content a tile shows: the document generation, which
// changes on reload, the page's revision, which changes on each edit, and
// the tile style, which changes with the settings rendered into tiles.
type tileVersion struct{ gen, rev, style int }

func thumbnailKey(page int) tileKey { return tileKey{page: page, thumb: true} }

// renderedTile is a texture holding part of a page rendered at scale; rect
// is the part it covers in device pixels at that scale.
type renderedTile struct {
	key     tileKey
	texture *sdl.Texture
	rect    image.Rectangle
	scale   float64
	lru     *list.Element
	// version is, for a thumbnail, the content of its last whole-page render.
	version tileVersion
}

func (t *renderedTile) bytes() int64 {
	return int64(t.rect.Dx()) * int64(t.rect.Dy()) * 4
}

// tileCache holds rendered tiles and thumbnails in least-recently-used
// order under a byte limit. Its zero value is empty and ready to use.
type tileCache struct {
	entries   map[tileKey]*renderedTile
	byPage    map[int]map[tileKey]*renderedTile
	lru       list.List
	bytes     int64
	byteLimit int64
	// protected tiles are on screen and never evicted.
	protected map[tileKey]bool
}

func (c *tileCache) get(key tileKey) (*renderedTile, bool) {
	tile, ok := c.entries[key]
	if ok {
		c.lru.MoveToBack(tile.lru)
	}
	return tile, ok
}

func (c *tileCache) add(tile *renderedTile) {
	c.remove(tile.key)
	if c.entries == nil {
		c.entries = map[tileKey]*renderedTile{}
		c.byPage = map[int]map[tileKey]*renderedTile{}
	}
	page := c.byPage[tile.key.page]
	if page == nil {
		page = map[tileKey]*renderedTile{}
		c.byPage[tile.key.page] = page
	}
	tile.lru = c.lru.PushBack(tile.key)
	c.entries[tile.key] = tile
	page[tile.key] = tile
	c.bytes += tile.bytes()
}

func (c *tileCache) remove(key tileKey) {
	tile, ok := c.entries[key]
	if !ok {
		return
	}
	c.lru.Remove(tile.lru)
	delete(c.entries, key)
	delete(c.byPage[key.page], key)
	if len(c.byPage[key.page]) == 0 {
		delete(c.byPage, key.page)
	}
	c.bytes -= tile.bytes()
	destroyTexture(tile.texture)
}

// evict drops least recently used tiles until the cache fits its limit,
// skipping protected ones.
func (c *tileCache) evict() {
	if c.byteLimit <= 0 {
		return
	}
	for elem := c.lru.Front(); elem != nil && c.bytes > c.byteLimit; {
		key := elem.Value.(tileKey)
		elem = elem.Next()
		if !c.protected[key] {
			c.remove(key)
		}
	}
}

func (c *tileCache) clear() {
	for key := range c.entries {
		c.remove(key)
	}
}

// dropStale removes a page's tiles of versions other than version.
func (c *tileCache) dropStale(page int, version tileVersion) {
	for key := range c.byPage[page] {
		if !key.thumb && key.version != version {
			c.remove(key)
		}
	}
}

// retainPages removes everything cached for pages at or beyond count.
func (c *tileCache) retainPages(count int) {
	for page, tiles := range c.byPage {
		if page >= count {
			for key := range tiles {
				c.remove(key)
			}
		}
	}
}

// pageTiles returns a page's tiles in drawing order: the thumbnail, tiles
// of older versions, then tiles of the current version, each group from
// lowest to highest resolution. A tile at a scale other than the current one
// shows the same content, so the sharpest available ends up on top, for
// example while the render scale catches up after zooming in.
func (c *tileCache) pageTiles(page int, version tileVersion) []*renderedTile {
	tiles := make([]*renderedTile, 0, len(c.byPage[page]))
	for _, tile := range c.byPage[page] {
		tiles = append(tiles, tile)
	}
	rank := func(t *renderedTile) int {
		switch {
		case t.key.thumb:
			return 0
		case t.key.version != version:
			return 1
		default:
			return 2
		}
	}
	slices.SortFunc(tiles, func(a, b *renderedTile) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.scale, b.scale))
	})
	return tiles
}

func destroyTexture(tex *sdl.Texture) {
	if tex != nil {
		sdl.DestroyTexture(tex)
	}
}

// tileRect is the device-pixel rect of tile (x, y) within a page's device
// rect at the same scale.
func tileRect(page image.Rectangle, x, y int) image.Rectangle {
	min := page.Min.Add(image.Pt(x*renderTileSize, y*renderTileSize))
	return image.Rectangle{Min: min, Max: min.Add(image.Pt(renderTileSize, renderTileSize))}.Intersect(page)
}

// tilesCovering returns the tile coordinates whose rects intersect area,
// both in device pixels relative to the same page rect.
func tilesCovering(page, area image.Rectangle) []image.Point {
	area = area.Intersect(page)
	if area.Empty() {
		return nil
	}
	x0 := (area.Min.X - page.Min.X) / renderTileSize
	y0 := (area.Min.Y - page.Min.Y) / renderTileSize
	x1 := (area.Max.X - page.Min.X + renderTileSize - 1) / renderTileSize
	y1 := (area.Max.Y - page.Min.Y + renderTileSize - 1) / renderTileSize
	tiles := make([]image.Point, 0, (x1-x0)*(y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			tiles = append(tiles, image.Pt(x, y))
		}
	}
	return tiles
}

// pageDeviceArea maps a screen rect onto page, whose screen origin is
// (x, y), and returns it in device pixels at scale.
func (a *App) pageDeviceArea(page int, x, y float64, screen sdl.FRect, scale float64) image.Rectangle {
	return pageDeviceAreaAt(a.pageTransform(page), x, y, screen, scale)
}

// pageDeviceAreaAt is pageDeviceArea for a page drawn by t.
func pageDeviceAreaAt(t pageTransform, x, y float64, screen sdl.FRect, scale float64) image.Rectangle {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, corner := range [][2]float64{
		{float64(screen.X), float64(screen.Y)},
		{float64(screen.X + screen.W), float64(screen.Y)},
		{float64(screen.X), float64(screen.Y + screen.H)},
		{float64(screen.X + screen.W), float64(screen.Y + screen.H)},
	} {
		p := t.toPage(corner[0]-x, corner[1]-y)
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}
	return image.Rect(int(math.Floor(minX*scale)), int(math.Floor(minY*scale)), int(math.Ceil(maxX*scale)), int(math.Ceil(maxY*scale)))
}

// updateThumbnail paints a newly rendered tile into its page's thumbnail.
// The thumbnail is recreated, keeping its old content, when a tile offers a
// sharper scale or the page changed size.
func (a *App) updateThumbnail(tile *renderedTile) {
	if a.renderer == nil || tile.key.thumb {
		return
	}
	page := tile.key.page
	bounds := a.pageMetrics[page].bounds
	longSide := math.Max(float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0))
	if longSide <= 0 {
		return
	}
	want := math.Min(thumbnailLongSide/longSide, tile.scale)
	thumb, ok := a.cache.get(thumbnailKey(page))
	current := ok && thumb.rect == mupdf.DeviceRect(bounds, thumb.scale)
	if !current || thumb.scale < want {
		next := a.newThumbnail(page, mupdf.DeviceRect(bounds, want), want)
		if next == nil {
			return
		}
		if current {
			a.drawIntoThumbnail(next, thumb)
			next.version = thumb.version
		}
		a.cache.add(next)
		thumb = next
	}
	a.drawIntoThumbnail(thumb, tile)
}

// replaceThumbnail makes a whole-page render the page's thumbnail.
func (a *App) replaceThumbnail(render *renderedTile, version tileVersion) {
	defer destroyTexture(render.texture)
	thumb := a.newThumbnail(render.key.page, render.rect, render.scale)
	if thumb == nil {
		return
	}
	a.drawIntoThumbnail(thumb, render)
	thumb.version = version
	a.cache.add(thumb)
	a.cache.evict()
}

func (a *App) newThumbnail(page int, rect image.Rectangle, scale float64) *renderedTile {
	tex := sdl.CreateTexture(a.renderer, sdl.PixelFormatRGBA32, sdl.TextureAccessTarget, int32(rect.Dx()), int32(rect.Dy()))
	if tex == nil {
		return nil
	}
	cleared := a.withRenderTarget(tex, func() {
		sdl.SetRenderDrawColor(a.renderer, 0, 0, 0, 0)
		sdl.RenderClear(a.renderer)
	})
	if !cleared {
		destroyTexture(tex)
		return nil
	}
	return &renderedTile{key: thumbnailKey(page), texture: tex, rect: rect, scale: scale}
}

func (a *App) drawIntoThumbnail(thumb, src *renderedTile) {
	ratio := thumb.scale / src.scale
	dst := sdl.FRect{
		X: float32(float64(src.rect.Min.X)*ratio - float64(thumb.rect.Min.X)),
		Y: float32(float64(src.rect.Min.Y)*ratio - float64(thumb.rect.Min.Y)),
		W: float32(float64(src.rect.Dx()) * ratio),
		H: float32(float64(src.rect.Dy()) * ratio),
	}
	a.withRenderTarget(thumb.texture, func() { sdl.RenderTexture(a.renderer, src.texture, nil, &dst) })
}

func (a *App) withRenderTarget(tex *sdl.Texture, draw func()) bool {
	old := sdl.GetRenderTarget(a.renderer)
	if !sdl.SetRenderTarget(a.renderer, tex) {
		return false
	}
	draw()
	return sdl.SetRenderTarget(a.renderer, old)
}
