package viewer

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // image formats canvas:image draws
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"runtime"
	"strings"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
	lua "github.com/yuin/gopher-lua"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/font"
	_ "golang.org/x/image/webp"
)

// canvasCache keeps what canvases draw with beyond the UI font: faces at
// other sizes and monospaced, text laid out in lines, and images. Each is
// bounded, emptied when full, in case a plugin draws ever new ones.
type canvasCache struct {
	faces  map[canvasFaceKey]font.Face
	lines  map[canvasLinesKey]canvasLines
	images map[string]*canvasImage
	// thumbs are the pages canvases drew this frame, with the render a
	// sharper thumbnail needs, if one does, and tiles the tiles of parts of
	// pages they drew. Drawing notes them; prefetchVisiblePages asks for
	// them, once it has told the render worker they are wanted.
	thumbs map[int]renderRequest
	tiles  map[tileKey]image.Rectangle
}

const (
	maxCanvasFaces  = 16
	maxCanvasLines  = 512
	maxCanvasImages = 64
)

type canvasFaceKey struct {
	size       int // output pixels
	bold, mono bool
}

type canvasLinesKey struct {
	text  string
	face  font.Face
	width int // output pixels, 0 for no limit
	wrap  bool
}

// canvasLines is text laid out in lines, and the widest's width.
type canvasLines struct {
	lines []string
	width int
}

type canvasImage struct {
	texture *sdl.Texture
	w, h    int
	err     error
	// modTime is the file's when loaded, checked against it at most every
	// imageCheckInterval, the last time at checked.
	modTime, checked time.Time
}

// imageCheckInterval is how often an image's file is looked at for a
// change, which reloads it.
const imageCheckInterval = time.Second

func (c *canvasCache) closeFaces() {
	closed := map[font.Face]bool{}
	for _, face := range c.faces {
		if !closed[face] {
			closeFontFace(face)
			closed[face] = true
		}
	}
	c.faces = nil
	c.lines = nil // keyed by the faces
}

func (c *canvasCache) clearImages() {
	for _, img := range c.images {
		destroyTexture(img.texture)
	}
	c.images = nil
}

// canvasFace is the face text options ask for: the UI font's, or another
// size of it or a monospaced font, loaded when first asked for.
func (a *App) canvasFace(o config.TextOptions) font.Face {
	if a.fontFace == nil {
		return nil
	}
	if o.Size == 0 && !o.Mono {
		if o.Bold {
			return a.headingFont()
		}
		return a.fontFace
	}
	size := int(math.Round(float64(a.config.Theme.Font.Size) * a.uiScale))
	if o.Size > 0 {
		size = int(math.Round(o.Size * float64(a.px(1))))
	}
	size = max(1, size)
	key := canvasFaceKey{size: size, bold: o.Bold, mono: o.Mono}
	if face, ok := a.canvas.faces[key]; ok {
		return face
	}
	if len(a.canvas.faces) >= maxCanvasFaces {
		a.canvas.closeFaces()
	}
	if a.canvas.faces == nil {
		a.canvas.faces = map[canvasFaceKey]font.Face{}
	}
	var regular, heading font.Face
	if o.Mono {
		regular, heading = loadMonoFonts(a.config.Theme.Font, size)
	} else {
		regular, heading, _ = loadUIFonts(a.config.Theme.Font, size)
	}
	a.canvas.faces[canvasFaceKey{size: size, mono: o.Mono}] = regular
	a.canvas.faces[canvasFaceKey{size: size, bold: true, mono: o.Mono}] = heading
	return a.canvas.faces[key]
}

// monoFamilies are the platform's monospaced fonts, tried in order.
func monoFamilies() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"SF Mono", "Menlo", "Monaco", "Courier New"}
	case "windows":
		return []string{"Cascadia Mono", "Consolas", "Courier New"}
	default:
		return []string{"DejaVu Sans Mono", "Noto Sans Mono", "Liberation Mono", "Ubuntu Mono"}
	}
}

// loadMonoFonts loads the first installed monospaced font at size pixels,
// with a heavier face for bold, or the UI font if none is installed.
func loadMonoFonts(base config.ThemeFont, size int) (regular, heading font.Face) {
	if fontMap, err := systemFontMap(); err == nil {
		for _, family := range monoFamilies() {
			face, location, err := loadInstalledFont(fontMap, family, "normal", 400, size)
			if err != nil {
				continue
			}
			bold, boldLocation, err := loadInstalledFont(fontMap, family, "normal", 700, size)
			if err != nil || boldLocation == location {
				if bold != nil {
					closeFontFace(bold)
				}
				bold = face
			}
			return face, bold
		}
	}
	regular, heading, _ = loadUIFonts(base, size)
	return regular, heading
}

// faceLineHeight is the height of a line of text in face.
func faceLineHeight(face font.Face) int {
	m := face.Metrics()
	return max(m.Height.Ceil(), m.Ascent.Ceil()+m.Descent.Ceil())
}

// canvasTextLines lays text out in face as o asks: its lines, wrapped or
// truncated to o's width, and the widest's width, in output pixels.
func (a *App) canvasTextLines(face font.Face, text string, o config.TextOptions) canvasLines {
	limit := int(math.Round(o.Width * float64(a.px(1))))
	key := canvasLinesKey{text: text, face: face, width: limit, wrap: o.Wrap}
	if laid, ok := a.canvas.lines[key]; ok {
		return laid
	}
	var laid canvasLines
	for para := range strings.SplitSeq(text, "\n") {
		switch {
		case limit <= 0:
			laid.lines = append(laid.lines, para)
		case o.Wrap:
			// Wrapped as a text field wraps, the spaces a line breaks
			// after not drawn.
			runes := []rune(para)
			for _, seg := range wrapSegments(face, runes, limit) {
				laid.lines = append(laid.lines, strings.TrimRight(string(runes[seg[0]:seg[1]]), " "))
			}
		default:
			laid.lines = append(laid.lines, truncateText(face, para, limit))
		}
	}
	for _, line := range laid.lines {
		laid.width = max(laid.width, measureText(face, line))
	}
	if a.canvas.lines == nil || len(a.canvas.lines) >= maxCanvasLines {
		a.canvas.lines = map[canvasLinesKey]canvasLines{}
	}
	a.canvas.lines[key] = laid
	return laid
}

func (c *elementCanvas) Text(x, y float64, text string, clr config.Color, o config.TextOptions) (float64, float64) {
	return c.text(x, y, text, clr, o, true)
}

func (c *elementCanvas) Measure(text string, o config.TextOptions) (float64, float64) {
	return c.text(0, 0, text, config.Color{}, o, false)
}

// text lays text out as o asks at x, y, drawing it if draw is set, and
// returns the size it takes in logical pixels.
func (c *elementCanvas) text(x, y float64, text string, clr config.Color, o config.TextOptions, draw bool) (float64, float64) {
	a := c.app
	face := a.canvasFace(o)
	if face == nil {
		return 0, 0
	}
	scale := float64(a.px(1))
	regular := o
	regular.Bold = false
	embolden := o.Bold && a.canvasFace(regular) == face
	laid := a.canvasTextLines(face, text, o)
	lineHeight := faceLineHeight(face)
	height := len(laid.lines) * lineHeight
	if draw {
		left, top := float64(c.box.X)+x*scale, float64(c.box.Y)+y*scale
		ascent := face.Metrics().Ascent.Ceil()
		rgba := a.styleColor(clr, c.style.Opacity.V)
		for i, line := range laid.lines {
			lx := left
			if o.Align == "center" || o.Align == "right" {
				free := -float64(measureText(face, line))
				if o.Width > 0 {
					free += o.Width * scale
				}
				if o.Align == "center" {
					free /= 2
				}
				lx += free
			}
			a.drawTextWithFace(c.renderer, line, int(math.Round(lx)), int(math.Round(top))+i*lineHeight+ascent, rgba, face, embolden)
		}
		c.extend(top - float64(c.box.Y) + float64(height))
	}
	return float64(laid.width) / scale, float64(height) / scale
}

// extend notes that the canvas drew down to bottom output pixels below its
// box's top, for a view that scrolls what it draws.
func (c *elementCanvas) extend(bottom float64) {
	c.extent = max(c.extent, bottom)
}

func (c *elementCanvas) Clip(x, y, w, h float64) {
	scale := float64(c.app.px(1))
	left, top := math.Floor(float64(c.box.X)+x*scale), math.Floor(float64(c.box.Y)+y*scale)
	c.clip = sdl.Rect{
		X: int32(left), Y: int32(top),
		W: int32(math.Ceil(float64(c.box.X)+(x+w)*scale) - left),
		H: int32(math.Ceil(float64(c.box.Y)+(y+h)*scale) - top),
	}
	if n := len(c.app.clips); n > 0 {
		c.clip = intersectRects(c.clip, c.app.clips[n-1])
	}
	c.clipped = true
	if c.renderer != nil {
		sdl.SetRenderClipRect(c.renderer, &c.clip)
	}
}

func (c *elementCanvas) Unclip() {
	if !c.clipped {
		return
	}
	c.clipped = false
	if c.renderer == nil {
		return
	}
	if n := len(c.app.clips); n > 0 {
		sdl.SetRenderClipRect(c.renderer, &c.app.clips[n-1])
	} else {
		sdl.SetRenderClipRect(c.renderer, nil)
	}
}

// canvasImage is the image file at path, decoded and made a texture once.
func (a *App) canvasImage(renderer *sdl.Renderer, path string) *canvasImage {
	if img, ok := a.canvas.images[path]; ok {
		if time.Since(img.checked) < imageCheckInterval {
			return img
		}
		img.checked = time.Now()
		if info, err := os.Stat(path); err != nil || info.ModTime().Equal(img.modTime) {
			return img
		}
		destroyTexture(img.texture)
		delete(a.canvas.images, path)
	}
	if len(a.canvas.images) >= maxCanvasImages {
		a.canvas.clearImages()
	}
	if a.canvas.images == nil {
		a.canvas.images = map[string]*canvasImage{}
	}
	img := &canvasImage{checked: time.Now()}
	if info, err := os.Stat(path); err == nil {
		img.modTime = info.ModTime()
	}
	img.texture, img.w, img.h, img.err = loadImageTexture(renderer, path)
	a.canvas.images[path] = img
	return img
}

func loadImageTexture(renderer *sdl.Renderer, path string) (*sdl.Texture, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer file.Close()
	decoded, _, err := image.Decode(file)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	b := decoded.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), decoded, b.Min, draw.Src)
	if renderer == nil {
		return nil, b.Dx(), b.Dy(), nil
	}
	tex, err := textureFromRGBA(renderer, rgba)
	if err != nil {
		return nil, 0, 0, err
	}
	// image.RGBA's pixels are premultiplied.
	if !sdl.SetTextureBlendMode(tex, sdl.BlendModeBlendPremultiplied) {
		sdl.DestroyTexture(tex)
		return nil, 0, 0, sdlError("set texture blend mode")
	}
	return tex, b.Dx(), b.Dy(), nil
}

func (c *elementCanvas) Image(path string, x, y, w, h float64) (float64, float64, error) {
	img := c.app.canvasImage(c.renderer, path)
	if img.err != nil {
		return 0, 0, img.err
	}
	w, h = fitSize(float64(img.w), float64(img.h), w, h, false)
	if dst := c.rect(x, y, w, h); img.texture != nil {
		a := c.app.styleColor(config.Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1}, c.style.Opacity.V).A
		tintText(img.texture, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: a})
		sdl.RenderTexture(c.renderer, img.texture, nil, &dst)
	}
	return w, h, nil
}

// fitSize is the size to draw something naturally w0 by h0 at, given a
// width and height either of which may be 0 to keep its proportions. With
// both given, contain fits it within them, keeping its proportions.
func fitSize(w0, h0, w, h float64, contain bool) (float64, float64) {
	if w0 <= 0 || h0 <= 0 {
		return w, h
	}
	switch {
	case w == 0 && h == 0:
		return w0, h0
	case w == 0:
		return h * w0 / h0, h
	case h == 0:
		return w, w * h0 / w0
	case contain && w/h > w0/h0:
		return h * w0 / h0, h
	case contain:
		return w, w * h0 / w0
	}
	return w, h
}

// rect is a rect of the canvas in output pixels, noting how far down it
// reaches.
func (c *elementCanvas) rect(x, y, w, h float64) sdl.FRect {
	scale := float64(c.app.px(1))
	r := sdl.FRect{X: float32(float64(c.box.X) + x*scale), Y: float32(float64(c.box.Y) + y*scale), W: float32(w * scale), H: float32(h * scale)}
	c.extend((y + h) * scale)
	return r
}

func (c *elementCanvas) Page(page int, x, y, w, h float64, area *config.PageArea) (float64, float64) {
	a := c.app
	if page < 0 || page >= len(a.pageMetrics) {
		return 0, 0
	}
	if area != nil {
		c.pageArea(page, c.rect(x, y, w, h), *area)
		return w, h
	}
	// The page is drawn turned as the document's are, fitted by the box
	// the turned page takes.
	bounds := a.pageMetrics[page].bounds
	pw, ph := float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0)
	minX, minY, maxX, maxY := rotatedBounds(bounds, a.rotation)
	w, h = fitSize(maxX-minX, maxY-minY, w, h, true)
	box := c.rect(x, y, w, h)
	scale := float64(box.W) / math.Max(1, maxX-minX)
	a.wantThumbnail(page, scale)
	if c.renderer == nil {
		return w, h
	}
	dst := sdl.FRect{W: float32(pw * scale), H: float32(ph * scale)}
	dst.X, dst.Y = box.X+(box.W-dst.W)/2, box.Y+(box.H-dst.H)/2
	if tile, ok := a.cache.get(thumbnailKey(page)); ok {
		alpha := a.styleColor(config.Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1}, c.style.Opacity.V).A
		// The thumbnail is the document's too, so it is left as it was.
		sdl.SetTextureAlphaMod(tile.texture, alpha)
		sdl.RenderTextureRotated(c.renderer, tile.texture, nil, &dst, a.rotation, nil, sdl.FlipNone)
		sdl.SetTextureAlphaMod(tile.texture, 0xff)
	} else {
		fillRect(c.renderer, box, a.styleColor(config.Color{RGB: a.palette().Page, Alpha: 1}, c.style.Opacity.V))
	}
	return w, h
}

// wantThumbnail notes a thumbnail of page at scale device pixels to the
// point, kept wanted while canvases draw it, and rendered if the one kept
// is not as sharp or not current.
func (a *App) wantThumbnail(page int, scale float64) {
	bounds := a.pageMetrics[page].bounds
	scale = math.Min(scale, overviewThumbLongSide/math.Max(1, math.Max(float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0))))
	if a.canvas.thumbs == nil {
		a.canvas.thumbs = map[int]renderRequest{}
	}
	version := a.tileVersion(page)
	if thumb, ok := a.cache.get(thumbnailKey(page)); ok && thumb.scale >= scale*renderUpgradeTolerance && thumb.version == version {
		a.canvas.thumbs[page] = renderRequest{}
		return
	}
	if req := a.canvas.thumbs[page]; req.scale < scale {
		a.canvas.thumbs[page] = renderRequest{key: thumbnailKey(page), scale: scale, rect: mupdf.DeviceRect(bounds, scale), version: version}
	}
}

// requestCanvasRenders asks for the thumbnails and tiles canvases drew that
// are not yet kept, once the render worker knows they are wanted.
func (a *App) requestCanvasRenders() {
	for _, req := range a.canvas.thumbs {
		if req.scale > 0 {
			a.queueRender(req, 0)
		}
	}
	for key, rect := range a.canvas.tiles {
		a.requestTile(key, rect, 0)
	}
}

// canvasRendersUnasked reports whether canvases drew a thumbnail or tile
// that is neither kept nor asked for.
func (a *App) canvasRendersUnasked() bool {
	for _, req := range a.canvas.thumbs {
		if _, pending := a.renderPending[req.key]; req.scale > 0 && !pending {
			return true
		}
	}
	for key := range a.canvas.tiles {
		_, cached := a.cache.get(key)
		_, pending := a.renderPending[key]
		if !cached && !pending {
			return true
		}
	}
	return false
}

// addCanvasRenders adds the thumbnails and tiles canvases drew to those
// wanted, on screen and kept from eviction.
func (a *App) addCanvasRenders(wanted, onScreen, protected map[tileKey]bool) {
	for page := range a.canvas.thumbs {
		key := thumbnailKey(page)
		wanted[key], onScreen[key], protected[key] = true, true, true
	}
	for key := range a.canvas.tiles {
		wanted[key], onScreen[key], protected[key] = true, true, true
	}
}

// pageArea draws the part of page area names in box: from the tiles the
// document is drawn from at its zoom, or sharper ones rendered for the
// area's, asked for as they are first drawn. In a panel with round
// corners, the page is cut to them, unless it is turned other than not at
// all.
func (c *elementCanvas) pageArea(page int, box sdl.FRect, area config.PageArea) {
	a := c.app
	scale := a.scale * area.Zoom
	renderScale := a.renderScaleFor(a.scale)
	if area.Zoom != 1 {
		renderScale *= area.Zoom
	}
	t := newPageTransform(a.pageMetrics[page].bounds, scale, a.rotation)
	dx, dy := t.toScreen(area.X, area.Y)
	x, y := float64(box.X)-dx, float64(box.Y)-dy
	version := a.tileVersion(page)
	pageRect := mupdf.DeviceRect(a.pageMetrics[page].bounds, renderScale)
	if a.canvas.tiles == nil {
		a.canvas.tiles = map[tileKey]image.Rectangle{}
	}
	for _, pt := range tilesCovering(pageRect, pageDeviceAreaAt(t, x, y, box, renderScale)) {
		a.canvas.tiles[tileKey{page: page, scale: renderScale, x: pt.X, y: pt.Y, version: version}] = tileRect(pageRect, pt.X, pt.Y)
	}
	if c.renderer == nil {
		return
	}
	var outline []point32
	if c.view != nil && normalizeRotation(a.rotation) == 0 {
		frame, _ := c.view.frameGeometry(a)
		st := a.style(c.view.element)
		if shape := a.boxShape(&st, pixelRect(frame)); shape.radius != [4]float32{} {
			var radius config.Corners
			for i, r := range shape.radius {
				radius[i] = float64(r)
			}
			pr := pixelRect(frame)
			outline = flattenPath(config.RoundedRectPath(float64(pr.X), float64(pr.Y), float64(pr.W), float64(pr.H), radius), pathPlacer(0, 0, 0, 0, 1))[0]
		}
	}
	alpha := a.styleColor(config.Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1}, c.style.Opacity.V).A
	a.withClip(c.renderer, box, func() error {
		for _, tile := range a.cache.pageTiles(page, version) {
			dst, ok := tileRectAt(tile, t, scale, x, y, a.winW, a.winH)
			if !ok {
				continue
			}
			sdl.SetTextureAlphaMod(tile.texture, alpha)
			switch {
			case outline != nil:
				drawClippedTexture(c.renderer, tile.texture, dst, outline)
			case normalizeRotation(a.rotation) == 0:
				sdl.RenderTexture(c.renderer, tile.texture, nil, &dst)
			default:
				sdl.RenderTextureRotated(c.renderer, tile.texture, nil, &dst, a.rotation, nil, sdl.FlipNone)
			}
			sdl.SetTextureAlphaMod(tile.texture, 0xff)
		}
		return nil
	})
	c.restoreClip()
}

// uiRegion is a rect a view's canvas named, in the window.
type uiRegion struct {
	id   lua.LValue
	rect sdl.FRect
}

func (c *elementCanvas) Region(id lua.LValue, x, y, w, h float64) bool {
	if c.view == nil {
		return false
	}
	r := c.rect(x, y, w, h)
	c.view.regions = append(c.view.regions, uiRegion{id: id, rect: r})
	p := c.app.pointer
	return c.pointerOver && pointInFRect(p.X, p.Y, r)
}

// regionAt is the id of the region of view at x, y, the one drawn last
// where they overlap, or nil.
func (view *uiView) regionAt(x, y float32) lua.LValue {
	for i := len(view.regions) - 1; i >= 0; i-- {
		r := view.regions[i].rect
		if pointInFRect(x, y, r) {
			return view.regions[i].id
		}
	}
	return nil
}

// viewUnderPointer is the view the pointer is over: the menu, when one is
// open, or the topmost panel.
func (a *App) viewUnderPointer() *uiView {
	if view := a.activeModalUIView(); view != nil {
		if rect, _ := view.frameGeometry(a); pointInRect(int(a.pointer.X), int(a.pointer.Y), rect) {
			return view
		}
		return nil
	}
	return a.panelAt(a.pointer.X, a.pointer.Y)
}

// drawViewBody draws view's body with its draw function, the canvas's
// default drawing its rows. What it draws below the body scrolls into it.
// If the function fails, the error is reported once, the rows are drawn in
// its place, and it is not called again until the configuration changes.
// A function that returns true is drawn again on the next frame.
func (a *App) drawViewBody(renderer *sdl.Renderer, view *uiView, body sdl.FRect, drawRows func() error) error {
	if a.themeErrors[view] {
		return drawRows()
	}
	// The body is drawn at full strength, the panel's opacity being for
	// the panel's own box.
	st := a.style(view.element)
	st.Opacity.V = 1
	var rowsErr error
	box := pixelRect(body)
	box.Y -= int32(math.Round(view.scrollPx))
	canvas := &elementCanvas{
		app: a, renderer: renderer, style: &st, box: box, drawDefault: func() { rowsErr = drawRows() },
		view: view, pointerOver: a.viewUnderPointer() == view,
	}
	view.regions = view.regions[:0]
	view.focusOrder = view.focusOrder[:0]
	clear(view.controls)
	scale := float64(a.px(1))
	state := config.DrawState{
		Element: view.element,
		X:       float64(body.X) / scale, Y: float64(body.Y) / scale,
		W: float64(body.W) / scale, H: float64(body.H) / scale,
		Alt:    a.altColors,
		Time:   time.Since(startTime).Seconds(),
		Scroll: view.scrollPx / scale,
	}
	err := a.withClip(renderer, body, func() error {
		again, err := view.drawBody(a, canvas, state)
		canvas.Unclip()
		if again {
			a.pendingRedraw = true
		}
		return err
	})
	if err != nil {
		a.reportThemeError(view, "ui draw", err)
		canvas.Default()
	}
	view.extent = canvas.extent
	view.scrollPx = clampFloat(view.scrollPx, 0, math.Max(0, view.extent-float64(body.H)))
	return rowsErr
}

// drawClippedTexture draws texture over dst, only where it falls inside
// the convex polygon outline.
func drawClippedTexture(renderer *sdl.Renderer, texture *sdl.Texture, dst sdl.FRect, outline []point32) {
	polygon := clipConvex([]point32{{dst.X, dst.Y}, {dst.X + dst.W, dst.Y}, {dst.X + dst.W, dst.Y + dst.H}, {dst.X, dst.Y + dst.H}}, outline)
	if len(polygon) < 3 {
		return
	}
	white := sdl.FColor{R: 1, G: 1, B: 1, A: 1}
	vertices := make([]sdl.Vertex, len(polygon))
	for i, p := range polygon {
		vertices[i] = sdl.Vertex{Position: sdl.FPoint{X: p.x, Y: p.y}, Color: white, TexCoord: sdl.FPoint{X: (p.x - dst.X) / dst.W, Y: (p.y - dst.Y) / dst.H}}
	}
	indices := make([]int32, 0, 3*(len(polygon)-2))
	for i := 1; i+1 < len(polygon); i++ {
		indices = append(indices, 0, int32(i), int32(i+1))
	}
	sdl.RenderGeometry(renderer, texture, vertices, indices)
}

// clipConvex is the part of the convex polygon subject inside the convex
// polygon clip, by Sutherland and Hodgman's method.
func clipConvex(subject, clip []point32) []point32 {
	// Which side is inside depends on which way clip is wound.
	var area float32
	for i, p := range clip {
		q := clip[(i+1)%len(clip)]
		area += p.x*q.y - q.x*p.y
	}
	side := func(a, b, p point32) float32 {
		d := (b.x-a.x)*(p.y-a.y) - (b.y-a.y)*(p.x-a.x)
		if area < 0 {
			return -d
		}
		return d
	}
	out := subject
	for i, a := range clip {
		b := clip[(i+1)%len(clip)]
		if a == b {
			continue
		}
		in := out
		out = nil
		for j, p := range in {
			q := in[(j+1)%len(in)]
			sp, sq := side(a, b, p), side(a, b, q)
			if sp >= 0 {
				out = append(out, p)
			}
			if (sp >= 0) != (sq >= 0) {
				t := sp / (sp - sq)
				out = append(out, point32{p.x + (q.x-p.x)*t, p.y + (q.y-p.y)*t})
			}
		}
		if len(out) == 0 {
			return nil
		}
	}
	return out
}
