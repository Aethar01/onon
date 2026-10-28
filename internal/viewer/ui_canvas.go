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
	"unicode/utf8"

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
	// thumbs are the pages canvases drew this frame, which the renderer
	// keeps thumbnails of; see prefetchVisiblePages.
	thumbs map[int]bool
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
}

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
			laid.lines = append(laid.lines, wrapText(face, para, limit)...)
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

// wrapText breaks s into lines at most limit pixels wide in face, between
// words, a word too long for a line broken where it reaches the edge.
func wrapText(face font.Face, s string, limit int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		candidate := word
		if line != "" {
			candidate = line + " " + word
		}
		if measureText(face, candidate) <= limit {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		for measureText(face, word) > limit {
			cut := fittingPrefix(face, word, limit)
			lines = append(lines, word[:cut])
			word = word[cut:]
		}
		line = word
	}
	return append(lines, line)
}

// fittingPrefix is the length in bytes of the longest prefix of s, of at
// least one rune, that fits in limit pixels.
func fittingPrefix(face font.Face, s string, limit int) int {
	_, first := utf8.DecodeRuneInString(s)
	cut := first
	for i := first; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if measureText(face, s[:i+size]) > limit {
			break
		}
		i += size
		cut = i
	}
	return cut
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
		return img
	}
	if len(a.canvas.images) >= maxCanvasImages {
		a.canvas.clearImages()
	}
	if a.canvas.images == nil {
		a.canvas.images = map[string]*canvasImage{}
	}
	img := &canvasImage{}
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

func (c *elementCanvas) Page(page int, x, y, w, h float64) (float64, float64) {
	a := c.app
	if page < 0 || page >= len(a.pageMetrics) {
		return 0, 0
	}
	bounds := a.pageMetrics[page].bounds
	pw, ph := float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0)
	w, h = fitSize(pw, ph, w, h, true)
	dst := c.rect(x, y, w, h)
	a.wantThumbnail(page, float64(dst.W)/math.Max(1, pw))
	if c.renderer == nil {
		return w, h
	}
	if tile, ok := a.cache.get(thumbnailKey(page)); ok {
		alpha := a.styleColor(config.Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1}, c.style.Opacity.V).A
		// The thumbnail is the document's too, so it is left as it was.
		sdl.SetTextureAlphaMod(tile.texture, alpha)
		sdl.RenderTexture(c.renderer, tile.texture, nil, &dst)
		sdl.SetTextureAlphaMod(tile.texture, 0xff)
	} else {
		fillRect(c.renderer, dst, a.styleColor(config.Color{RGB: a.palette().Page, Alpha: 1}, c.style.Opacity.V))
	}
	return w, h
}

// wantThumbnail asks for a thumbnail of page at scale device pixels to
// the point, if the one kept is not as sharp or not current, and keeps it
// wanted while canvases draw it.
func (a *App) wantThumbnail(page int, scale float64) {
	bounds := a.pageMetrics[page].bounds
	scale = math.Min(scale, overviewThumbLongSide/math.Max(1, math.Max(float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0))))
	if a.canvas.thumbs == nil {
		a.canvas.thumbs = map[int]bool{}
	}
	a.canvas.thumbs[page] = true
	version := a.tileVersion(page)
	if thumb, ok := a.cache.get(thumbnailKey(page)); ok && thumb.scale >= scale*renderUpgradeTolerance && thumb.version == version {
		return
	}
	a.queueRender(renderRequest{key: thumbnailKey(page), scale: scale, rect: mupdf.DeviceRect(bounds, scale), version: version}, 0)
}

// addCanvasThumbnails adds the thumbnails canvases drew to those wanted,
// on screen and kept from eviction.
func (a *App) addCanvasThumbnails(wanted, onScreen, protected map[tileKey]bool) {
	for page := range a.canvas.thumbs {
		key := thumbnailKey(page)
		wanted[key], onScreen[key], protected[key] = true, true, true
	}
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
	return c.pointerOver && p.X >= r.X && p.X < r.X+r.W && p.Y >= r.Y && p.Y < r.Y+r.H
}

// regionAt is the id of the region of view at x, y, the one drawn last
// where they overlap, or nil.
func (view *uiView) regionAt(x, y float32) lua.LValue {
	for i := len(view.regions) - 1; i >= 0; i-- {
		r := view.regions[i].rect
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
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
	view.fieldOrder = view.fieldOrder[:0]
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
