package viewer

import (
	"container/list"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"onon/internal/config"

	textfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// cachedFontFace caches the per-rune metrics that measuring text looks up:
// a face read from a file would otherwise read it for every rune measured.
type cachedFontFace struct {
	font.Face
	file     *os.File // the face's font, or nil for one in memory
	advances map[rune]glyphAdvance
	kerns    map[[2]rune]fixed.Int26_6
}

type glyphAdvance struct {
	advance fixed.Int26_6
	ok      bool
}

func newCachedFontFace(face font.Face, file *os.File) *cachedFontFace {
	return &cachedFontFace{Face: face, file: file, advances: map[rune]glyphAdvance{}, kerns: map[[2]rune]fixed.Int26_6{}}
}

func (f *cachedFontFace) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	if a, ok := f.advances[r]; ok {
		return a.advance, a.ok
	}
	advance, ok := f.Face.GlyphAdvance(r)
	f.advances[r] = glyphAdvance{advance, ok}
	return advance, ok
}

func (f *cachedFontFace) Kern(r0, r1 rune) fixed.Int26_6 {
	pair := [2]rune{r0, r1}
	if k, ok := f.kerns[pair]; ok {
		return k
	}
	k := f.Face.Kern(r0, r1)
	f.kerns[pair] = k
	return k
}

func (f *cachedFontFace) Close() error {
	var faceErr error
	if closer, ok := f.Face.(interface{ Close() error }); ok {
		faceErr = closer.Close()
	}
	if f.file == nil {
		return faceErr
	}
	fileErr := f.file.Close()
	if faceErr != nil {
		return faceErr
	}
	return fileErr
}

type ttcFontReaderAt struct {
	source    *os.File
	directory []byte
	shift     int64
}

func (r *ttcFontReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("negative font read offset")
	}

	total := 0
	if off < r.shift {
		start := int(off)
		n := len(p)
		if remaining := len(r.directory) - start; n > remaining {
			n = remaining
		}
		copy(p[:n], r.directory[start:start+n])
		total += n
		p = p[n:]
		off += int64(n)
		if len(p) == 0 {
			return total, nil
		}
	}

	n, err := r.source.ReadAt(p, off-r.shift)
	return total + n, err
}

// systemUIFamilies are the platform's interface fonts, tried in order when
// the theme names none, or names one that is not installed.
func systemUIFamilies() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"System Font", "Helvetica Neue"}
	case "windows":
		return []string{"Segoe UI", "Tahoma"}
	default:
		return []string{"Adwaita Sans", "Cantarell", "Noto Sans", "Ubuntu", "DejaVu Sans", "Liberation Sans"}
	}
}

// loadUIFonts loads the theme's UI font at size pixels, and a heavier face
// of it for headings, which is the same face when the font has no heavier
// weight. Without a font path or family it loads the system's interface
// font. warning, if not nil, says why the font is not the one asked for;
// with no font at all the faces are the built-in bitmap font.
func loadUIFonts(f config.ThemeFont, size int) (regular, heading font.Face, warning error) {
	size = max(1, size)
	regular, heading, warning = loadUIFontFaces(f, size)
	// Characters the UI font lacks are drawn in installed fonts that have
	// them, found from the same families.
	families := systemUIFamilies()
	if f.Family != "" {
		families = append([]string{f.Family}, families...)
	}
	aspect := func(weight int) textfont.Aspect {
		a := textfont.Aspect{Style: textfont.StyleNormal, Weight: textfont.Weight(weight), Stretch: textfont.StretchNormal}
		if f.Style == "italic" || f.Style == "oblique" {
			a.Style = textfont.StyleItalic
		}
		return a
	}
	wrapped := newFallbackFace(regular, families, aspect(f.Weight), size)
	if heading == regular {
		return wrapped, wrapped, warning
	}
	return wrapped, newFallbackFace(heading, families, aspect(min(900, f.Weight+200)), size), warning
}

// loadUIFontFaces loads the faces loadUIFonts describes, without falling
// back for characters they lack.
func loadUIFontFaces(f config.ThemeFont, size int) (regular, heading font.Face, warning error) {
	if f.Path != "" {
		face, err := loadFontFile(f.Path, size)
		if err == nil {
			return face, face, nil
		}
		warning = fmt.Errorf("font.path %q: %w", f.Path, err)
	}
	fontMap, err := systemFontMap()
	if err != nil {
		return basicfont.Face7x13, basicfont.Face7x13, fmt.Errorf("listing installed fonts: %w; set onon.theme.font.path", err)
	}
	families := systemUIFamilies()
	if f.Family != "" {
		families = append([]string{f.Family}, families...)
	}
	for _, family := range families {
		regular, location, err := loadInstalledFont(fontMap, family, f.Style, f.Weight, size)
		if err != nil {
			if family == f.Family && warning == nil {
				warning = fmt.Errorf("font.family %q is not installed", family)
			}
			continue
		}
		heading, headingLocation, err := loadInstalledFont(fontMap, family, f.Style, min(900, f.Weight+200), size)
		if err != nil || headingLocation == location {
			if heading != nil {
				closeFontFace(heading)
			}
			heading = regular // the same font, as a variable font gives
		}
		if warning != nil {
			warning = fmt.Errorf("%w; using %s", warning, family)
		}
		return regular, heading, warning
	}
	return basicfont.Face7x13, basicfont.Face7x13, fmt.Errorf("no interface font found (tried %s); set onon.theme.font.family or onon.theme.font.path", strings.Join(families, ", "))
}

func systemFontMap() (*fontscan.FontMap, error) {
	fontMap := fontscan.NewFontMap(log.New(io.Discard, "", 0))
	if err := fontMap.UseSystemFonts(""); err != nil {
		return nil, err
	}
	return fontMap, nil
}

// loadInstalledFont loads the installed family closest to style and weight.
// It fails rather than substitute another family.
func loadInstalledFont(fontMap *fontscan.FontMap, family, style string, weight, size int) (font.Face, fontscan.Location, error) {
	aspect := textfont.Aspect{Style: textfont.StyleNormal, Weight: textfont.Weight(weight), Stretch: textfont.StretchNormal}
	if style == "italic" || style == "oblique" {
		aspect.Style = textfont.StyleItalic
	}
	fontMap.SetQuery(fontscan.Query{Families: []string{family}, Aspect: aspect})
	face := fontMap.ResolveFace('M')
	if face == nil || face.Font == nil || !strings.EqualFold(face.Describe().Family, family) {
		return nil, fontscan.Location{}, fmt.Errorf("no installed font matches %q", family)
	}
	location := fontMap.FontLocation(face.Font)
	if location.File == "" {
		return nil, fontscan.Location{}, fmt.Errorf("no installed font location for %q", family)
	}
	loaded, err := loadFontFileAt(location.File, size, int(location.Index))
	return loaded, location, err
}

func loadFontFile(path string, size int) (font.Face, error) {
	return loadFontFileAt(path, size, 0)
}

func loadFontFileAt(path string, size, collectionIndex int) (font.Face, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	reader, err := openTypeFontReaderAt(file, collectionIndex)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	fnt, err := opentype.ParseReaderAt(reader)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
		Size: float64(size),
		DPI:  72,
	})
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return newCachedFontFace(face, file), nil
}

func openTypeFontReaderAt(file *os.File, collectionIndex int) (io.ReaderAt, error) {
	if collectionIndex < 0 {
		return nil, fmt.Errorf("negative font collection index")
	}
	var header [12]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return nil, err
	}
	if string(header[:4]) != "ttcf" {
		if collectionIndex != 0 {
			return nil, fmt.Errorf("font is not a collection")
		}
		return file, nil
	}
	numFonts := int(binary.BigEndian.Uint32(header[8:12]))
	if numFonts == 0 {
		return nil, fmt.Errorf("font collection contains no fonts")
	}
	if collectionIndex >= numFonts {
		return nil, fmt.Errorf("font collection index %d out of range %d", collectionIndex, numFonts)
	}
	var offsetBytes [4]byte
	if _, err := file.ReadAt(offsetBytes[:], int64(12+collectionIndex*4)); err != nil {
		return nil, err
	}
	return newTTCFontReaderAt(file, int64(binary.BigEndian.Uint32(offsetBytes[:])))
}

func newTTCFontReaderAt(file *os.File, fontOffset int64) (io.ReaderAt, error) {
	var header [12]byte
	if _, err := file.ReadAt(header[:], fontOffset); err != nil {
		return nil, err
	}

	numTables := int(binary.BigEndian.Uint16(header[4:6]))
	directorySize := 12 + 16*numTables
	directory := make([]byte, directorySize)
	if _, err := file.ReadAt(directory, fontOffset); err != nil {
		return nil, err
	}

	shift := uint32(directorySize)
	for i := 0; i < numTables; i++ {
		offsetPos := 12 + i*16 + 8
		offset := binary.BigEndian.Uint32(directory[offsetPos : offsetPos+4])
		if offset > ^uint32(0)-shift {
			return nil, fmt.Errorf("font table offset overflow")
		}
		binary.BigEndian.PutUint32(directory[offsetPos:offsetPos+4], offset+shift)
	}

	return &ttcFontReaderAt{
		source:    file,
		directory: directory,
		shift:     int64(shift),
	}, nil
}

// closeUIFonts closes the UI faces, the heading face being the regular one
// when the font has no heavier weight.
func (s *sdlState) closeUIFonts() {
	s.canvas.closeFaces()
	if s.headingFace != s.fontFace {
		closeFontFace(s.headingFace)
	}
	closeFontFace(s.fontFace)
	s.fontFace, s.headingFace = nil, nil
}

func closeFontFace(face font.Face) {
	if closer, ok := face.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func textureFromRGBA(renderer *sdl.Renderer, rgba *image.RGBA) (*sdl.Texture, error) {
	return textureFromPixels(renderer, rgba.Bounds().Dx(), rgba.Bounds().Dy(), rgba.Pix, rgba.Stride)
}

func textureFromNRGBA(renderer *sdl.Renderer, img *image.NRGBA) (*sdl.Texture, error) {
	return textureFromPixels(renderer, img.Bounds().Dx(), img.Bounds().Dy(), img.Pix, img.Stride)
}

func textureFromPixels(renderer *sdl.Renderer, w, h int, pix []byte, stride int) (*sdl.Texture, error) {
	tex := sdl.CreateTexture(renderer, sdl.PixelFormatRGBA32, sdl.TextureAccessStatic, int32(w), int32(h))
	if tex == nil {
		return nil, sdlError("create texture")
	}
	if len(pix) > 0 {
		if !sdl.UpdateTexture(tex, nil, unsafe.Pointer(&pix[0]), int32(stride)) {
			sdl.DestroyTexture(tex)
			return nil, sdlError("update texture")
		}
	}
	if !sdl.SetTextureBlendMode(tex, sdl.BlendModeBlend) {
		sdl.DestroyTexture(tex)
		return nil, sdlError("set texture blend mode")
	}
	return tex, nil
}

func measureText(face font.Face, s string) int {
	if s == "" {
		return 0
	}
	var d font.Drawer
	d.Face = face
	return d.MeasureString(s).Ceil()
}

// textTexture draws s in white, to be tinted as it is drawn.
func textTexture(renderer *sdl.Renderer, face font.Face, s string) (*sdl.Texture, int, int, int, error) {
	width := measureText(face, s)
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	height := metrics.Height.Ceil()
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	d := &font.Drawer{
		Dst:  img,
		Src:  image.White,
		Face: face,
		Dot:  fixed.P(0, ascent),
	}
	d.DrawString(s)
	tex, err := textureFromRGBA(renderer, img)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	// The drawer leaves premultiplied pixels, which blended as straight
	// alpha would darken every anti-aliased edge a second time.
	if !sdl.SetTextureBlendMode(tex, sdl.BlendModeBlendPremultiplied) {
		sdl.DestroyTexture(tex)
		return nil, 0, 0, 0, sdlError("set texture blend mode")
	}
	return tex, width, height, ascent, nil
}

const maxTextTextureCacheEntries = 512

// textTextureKey is a string as drawn: only its text and face, since
// it is tinted to its colour and opacity as it is drawn.
type textTextureKey struct {
	text string
	face font.Face
}

type cachedTextTexture struct {
	texture *sdl.Texture
	width   int
	height  int
	ascent  int
}

func (a *App) drawText(renderer *sdl.Renderer, s string, x, baselineY int, clr color.Color) error {
	return a.drawTextFace(renderer, s, x, baselineY, clr, false)
}

// drawHeading draws s in the heavier heading face.
func (a *App) drawHeading(renderer *sdl.Renderer, s string, x, baselineY int, clr color.Color) error {
	return a.drawTextFace(renderer, s, x, baselineY, clr, true)
}

func (a *App) drawTextFace(renderer *sdl.Renderer, s string, x, baselineY int, clr color.Color, heading bool) error {
	face := a.fontFace
	if heading {
		face = a.headingFont()
	}
	// A font with no heavier weight, as a variable font is to x/image, is
	// emboldened.
	return a.drawTextWithFace(renderer, s, x, baselineY, clr, face, heading && a.headingFont() == a.fontFace)
}

// drawTextWithFace draws s in face with its baseline at baselineY, with
// embolden drawing it again a pixel over.
func (a *App) drawTextWithFace(renderer *sdl.Renderer, s string, x, baselineY int, clr color.Color, face font.Face, embolden bool) error {
	entry, err := a.cachedTextTexture(renderer, s, face)
	if err != nil {
		return err
	}
	tintText(entry.texture, clr)
	dst := sdl.FRect{X: float32(x), Y: float32(baselineY - entry.ascent), W: float32(entry.width), H: float32(entry.height)}
	if err := renderBool(sdl.RenderTexture(renderer, entry.texture, nil, &dst), "render text"); err != nil {
		return err
	}
	if embolden {
		dst.X += float32(max(1, a.ipx(0.5)))
		return renderBool(sdl.RenderTexture(renderer, entry.texture, nil, &dst), "render text")
	}
	return nil
}

// headingFont is the face drawHeading draws in.
func (a *App) headingFont() font.Face {
	if a.headingFace != nil {
		return a.headingFace
	}
	return a.fontFace
}

// tintText sets a text texture to draw in clr, whose components are
// straight. The texture's pixels are white and premultiplied, so its
// colour is premultiplied by its alpha too.
func tintText(tex *sdl.Texture, clr color.Color) {
	r, g, b, a := clr.RGBA()
	alpha := uint16(a >> 8)
	premultiply := func(c uint32) uint8 { return uint8((uint16(c>>8)*alpha + 127) / 255) }
	sdl.SetTextureColorMod(tex, premultiply(r), premultiply(g), premultiply(b))
	sdl.SetTextureAlphaMod(tex, uint8(alpha))
}

func (a *App) cachedTextTexture(renderer *sdl.Renderer, s string, face font.Face) (cachedTextTexture, error) {
	key := textTextureKey{text: s, face: face}
	if entry, ok := a.textCache.get(key); ok {
		return entry, nil
	}
	tex, w, h, ascent, err := textTexture(renderer, face, s)
	if err != nil {
		return cachedTextTexture{}, err
	}
	entry := cachedTextTexture{texture: tex, width: w, height: h, ascent: ascent}
	a.textCache.add(key, entry)
	return entry, nil
}

// textTextureCache keeps rendered strings, evicting the least recently used
// once full. Its zero value is empty and ready to use.
type textTextureCache struct {
	entries map[textTextureKey]*list.Element // values are textCacheEntry
	order   list.List
}

type textCacheEntry struct {
	key textTextureKey
	tex cachedTextTexture
}

func (c *textTextureCache) get(key textTextureKey) (cachedTextTexture, bool) {
	elem, ok := c.entries[key]
	if !ok {
		return cachedTextTexture{}, false
	}
	c.order.MoveToBack(elem)
	return elem.Value.(textCacheEntry).tex, true
}

func (c *textTextureCache) add(key textTextureKey, tex cachedTextTexture) {
	if c.entries == nil {
		c.entries = map[textTextureKey]*list.Element{}
	}
	if len(c.entries) >= maxTextTextureCacheEntries {
		oldest := c.order.Front()
		entry := c.order.Remove(oldest).(textCacheEntry)
		delete(c.entries, entry.key)
		destroyTexture(entry.tex.texture)
	}
	c.entries[key] = c.order.PushBack(textCacheEntry{key: key, tex: tex})
}

func (c *textTextureCache) clear() {
	for _, elem := range c.entries {
		destroyTexture(elem.Value.(textCacheEntry).tex.texture)
	}
	c.entries = nil
	c.order.Init()
}

func (s *sdlState) clearTextTextureCache() {
	s.textCache.clear()
}

func (s *sdlState) Close() {
	s.clearTextTextureCache()
	s.canvas.clearImages()
	s.masks.clear()
	s.closeUIFonts()
	s.destroyCursors()
	if s.autoscrollMarker != nil {
		sdl.DestroyTexture(s.autoscrollMarker)
		s.autoscrollMarker = nil
	}
	if s.renderer != nil {
		sdl.DestroyRenderer(s.renderer)
		s.renderer = nil
	}
	if s.window != nil {
		sdl.DestroyWindow(s.window)
		s.window = nil
	}
	sdl.Quit()
}

func fillRect(renderer *sdl.Renderer, rect sdl.FRect, clr color.RGBA) error {
	if !sdl.SetRenderDrawColor(renderer, clr.R, clr.G, clr.B, clr.A) {
		return sdlError("set draw color")
	}
	return renderBool(sdl.RenderFillRect(renderer, &rect), "fill rect")
}

func strokeRect(renderer *sdl.Renderer, rect sdl.FRect, clr color.RGBA, width int) error {
	if width < 1 {
		width = 1
	}
	if !sdl.SetRenderDrawColor(renderer, clr.R, clr.G, clr.B, clr.A) {
		return sdlError("set draw color")
	}
	for i := 0; i < width; i++ {
		inset := float32(i)
		r := sdl.FRect{X: rect.X + inset, Y: rect.Y + inset, W: rect.W - inset*2, H: rect.H - inset*2}
		if r.W <= 0 || r.H <= 0 {
			break
		}
		if !sdl.RenderRect(renderer, &r) {
			return sdlError("draw rect")
		}
	}
	return nil
}

func renderBool(ok bool, op string) error {
	if !ok {
		return sdlError(op)
	}
	return nil
}

func sdlError(op string) error {
	if err := sdl.GetError(); err != "" {
		return fmt.Errorf("SDL %s failed: %s", op, err)
	}
	return fmt.Errorf("SDL %s failed", op)
}
