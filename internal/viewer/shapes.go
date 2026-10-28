package viewer

import (
	"container/list"
	"image"
	"image/color"
	"log"
	"math"
	"slices"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/vector"
)

// px converts logical pixels, as the theme measures, to output pixels at
// the scale the UI font was loaded for.
func (a *App) px(v float64) float32 {
	scale := a.uiScale
	if scale <= 0 {
		scale = 1
	}
	return float32(v * scale)
}

// ipx is px rounded to whole output pixels.
func (a *App) ipx(v float64) int {
	return int(math.Round(float64(a.px(v))))
}

// hairline is the width of a one-logical-pixel line, never under one
// output pixel.
func (a *App) hairline() float32 {
	return max(1, float32(math.Round(float64(a.px(1)))))
}

// style is how an element is drawn in the current theme. The theme's
// styles are resolved on first use and kept until the config changes,
// as a frame asks for them many times over.
func (a *App) style(e config.Element) config.Style {
	if a.styles == nil {
		a.styles = new([config.ElementCount]config.Style)
		for i := range a.styles {
			a.styles[i] = a.config.Theme.Style(config.Element(i))
		}
	}
	return a.styles[e]
}

// lineWidth is a border or line width in output pixels: none for 0, and
// otherwise never under one pixel, so hairlines stay visible.
func (a *App) lineWidth(v float64) float32 {
	if v <= 0 {
		return 0
	}
	return max(1, a.px(v))
}

// styleColor is c in the current color mode at opacity.
func (a *App) styleColor(c config.Color, opacity float64) color.RGBA {
	alpha := c.Alpha * opacity * (1 - a.motion.fade)
	rgb := c.RGB
	switch c.Name {
	case "":
	case "shadow":
		// Shadows need more strength to show over a dark background.
		rgb = [3]uint8{}
		if isLight(a.backgroundColor()) {
			alpha *= 0x22 / 255.0
		} else {
			alpha *= 0x55 / 255.0
		}
	default:
		rgb, _ = a.palette().Lookup(c.Name)
	}
	return color.RGBA{R: rgb[0], G: rgb[1], B: rgb[2], A: uint8(math.Round(255 * min(1, max(0, alpha))))}
}

// textColor is the style's text colour, or its secondary colour.
func (a *App) textColor(st *config.Style, secondary bool) color.RGBA {
	if secondary {
		return a.styleColor(st.Secondary.V, st.Opacity.V)
	}
	return a.styleColor(st.Text.V, st.Opacity.V)
}

// insets converts a style's insets to output pixels.
func (a *App) insets(in config.Insets) (top, right, bottom, left float32) {
	return a.px(in.Top), a.px(in.Right), a.px(in.Bottom), a.px(in.Left)
}

// boxParts selects the parts of a box to draw.
type boxParts int

const (
	boxBody   boxParts = 1 << iota // shadows and fill
	boxBorder                      // the border
	boxAll    = boxBody | boxBorder
)

// drawBox draws the box of an element styled st over rect: its shadows,
// then its fill and its border.
func (a *App) drawBox(renderer *sdl.Renderer, st *config.Style, rect sdl.FRect) {
	a.drawBoxParts(renderer, st, rect, boxAll)
}

// drawBoxParts draws parts of a box. A box holding other elements has its
// border drawn after them, so that they cannot cover it. A style's draw
// function draws its box when the body is drawn; the border is still to
// be drawn after only if the function drew the default box, which the
// result reports.
func (a *App) drawBoxParts(renderer *sdl.Renderer, st *config.Style, rect sdl.FRect, parts boxParts) bool {
	if fn := st.Draw.V; fn != nil && parts&boxBody != 0 && !a.themeErrors[fn] {
		return a.runDrawFunction(renderer, st, rect, parts)
	}
	a.drawDefaultBox(renderer, st, rect, parts)
	return true
}

// drawDefaultBox draws parts of a box as its style describes.
func (a *App) drawDefaultBox(renderer *sdl.Renderer, st *config.Style, rect sdl.FRect, parts boxParts) {
	box := pixelRect(rect)
	if box.W <= 0 || box.H <= 0 {
		return
	}
	opacity := st.Opacity.V
	shape := a.boxShape(st, box)
	var layers []config.ShadowLayer
	if parts&boxBody != 0 {
		layers = st.Shadow.V.List()
	}
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		clr := a.styleColor(layer.Color, opacity)
		if clr.A == 0 {
			continue
		}
		mask := maskSpec{shape: shape, w: box.W, h: box.H, spread: a.px(layer.Spread), blur: a.px(layer.Blur)}
		a.drawMask(renderer, mask, box.X+int32(math.Round(float64(a.px(layer.X)))), box.Y+int32(math.Round(float64(a.px(layer.Y)))), clr)
	}
	if clr := a.styleColor(st.Fill.V, opacity); clr.A > 0 && parts&boxBody != 0 {
		a.drawMask(renderer, maskSpec{shape: shape, w: box.W, h: box.H}, box.X, box.Y, clr)
	}
	if width := a.lineWidth(st.BorderWidth.V); width > 0 && parts&boxBorder != 0 {
		if clr := a.styleColor(st.BorderColor.V, opacity); clr.A > 0 {
			mask := maskSpec{shape: shape, w: box.W, h: box.H, border: width, sides: st.BorderSides.V}
			a.drawMask(renderer, mask, box.X, box.Y, clr)
		}
	}
}

// pixelRect rounds rect's edges to whole pixels, which the shape masks are
// drawn at.
func pixelRect(rect sdl.FRect) sdl.Rect {
	x0, y0 := math.Round(float64(rect.X)), math.Round(float64(rect.Y))
	x1, y1 := math.Round(float64(rect.X+rect.W)), math.Round(float64(rect.Y+rect.H))
	return sdl.Rect{X: int32(x0), Y: int32(y0), W: int32(x1 - x0), H: int32(y1 - y0)}
}

// boxShape is st's shape for a box, its radii in output pixels and fitted
// to the box as CSS fits them.
func (a *App) boxShape(st *config.Style, box sdl.Rect) boxShape {
	shape := boxShape{Shape: st.Shape.V}
	w, h := float64(box.W), float64(box.H)
	switch shape.Kind {
	case "pill":
		r := float32(min(w, h) / 2)
		shape.Kind, shape.radius = "rect", [4]float32{r, r, r, r}
	case "path":
		shape.scale = a.px(1)
	default:
		var r [4]float64
		for i, v := range st.Radius.V {
			r[i] = float64(a.px(v))
		}
		fit := min(1, w/max(r[0]+r[1], 1e-9), w/max(r[3]+r[2], 1e-9), h/max(r[0]+r[3], 1e-9), h/max(r[1]+r[2], 1e-9))
		for i := range r {
			shape.radius[i] = float32(r[i] * fit)
		}
	}
	return shape
}

// boxShape is a shape ready to rasterise: a rect with radii in output
// pixels, or a path at a scale from logical to output pixels.
type boxShape struct {
	config.Shape
	radius [4]float32
	scale  float32
	ops    string // a draw function's path, as packPath packs it, in place of Path
}

// maskSpec is a mask of a shape on a w by h box: its fill, its border
// border pixels wide on sides, its shadow, grown by spread and blurred, or
// for a path a line stroke pixels wide along it.
type maskSpec struct {
	shape        boxShape
	w, h         int32
	border       float32
	sides        config.Sides
	spread, blur float32
	stroke       float32
	// invert takes the mask's complement within the box, as the corners
	// beyond a rounded shape; cut takes the shape out of it, offset by
	// cutX and cutY, as a shadow drawn over what casts it.
	invert     bool
	cut        bool
	cutX, cutY float32
}

// mask is a rasterised shape as a texture, white with the shape's
// coverage as alpha. Drawn, its origin sits at the box's top left less
// pad; sliceX and sliceY, when not zero, are the widths of the edges kept
// as they are when the middle stretches to the box's size.
type mask struct {
	texture        *sdl.Texture
	w, h           int32
	pad            int32
	sliceX, sliceY int32
	// hollow is a sliced mask whose middle is empty, as a shadow cut
	// away under its page is, so the middle is not drawn.
	hollow bool
}

// blurMargin is how far a blur reaches beyond a shape.
func blurMargin(blur float32) int32 {
	if blur <= 0 {
		return 0
	}
	return int32(math.Ceil(1.5*float64(blur))) + 1 // three standard deviations
}

// drawMask draws spec's mask with its box at x, y in clr.
func (a *App) drawMask(renderer *sdl.Renderer, spec maskSpec, x, y int32, clr color.RGBA) {
	m, ok := a.shapeMask(renderer, spec)
	if !ok {
		return
	}
	sdl.SetTextureColorMod(m.texture, clr.R, clr.G, clr.B)
	sdl.SetTextureAlphaMod(m.texture, clr.A)
	full := sdl.Rect{X: x - m.pad, Y: y - m.pad, W: spec.w + 2*m.pad, H: spec.h + 2*m.pad}
	// The mask's columns and rows: the left and right slices as they are,
	// and the middle stretched to the rest.
	spans := func(size, full, slice int32) [][2][2]int32 { // {src start, len}, {dst start, len}
		if slice == 0 {
			return [][2][2]int32{{{0, size}, {0, full}}}
		}
		return [][2][2]int32{
			{{0, slice}, {0, slice}},
			{{slice, size - 2*slice}, {slice, full - 2*slice}},
			{{size - slice, slice}, {full - slice, slice}},
		}
	}
	for i, col := range spans(m.w, full.W, m.sliceX) {
		for j, row := range spans(m.h, full.H, m.sliceY) {
			if col[1][1] <= 0 || row[1][1] <= 0 || m.hollow && i == 1 && j == 1 {
				continue
			}
			// The rects are the app's own, which SDL is given without each
			// taking an allocation.
			src, dst := &a.drawRects[0], &a.drawRects[1]
			*src = sdl.FRect{X: float32(col[0][0]), Y: float32(row[0][0]), W: float32(col[0][1]), H: float32(row[0][1])}
			*dst = sdl.FRect{X: float32(full.X + col[1][0]), Y: float32(full.Y + row[1][0]), W: float32(col[1][1]), H: float32(row[1][1])}
			sdl.RenderTexture(renderer, m.texture, src, dst)
		}
	}
}

// shapeMask returns spec's mask, rasterising it on first use.
func (a *App) shapeMask(renderer *sdl.Renderer, spec maskSpec) (mask, bool) {
	pad := blurMargin(spec.blur)
	key, sliceX, sliceY := maskSlices(spec, pad)
	if m, ok := a.masks.get(key); ok {
		return m, true
	}
	img, pad := a.rasterMask(key, pad)
	if img == nil {
		return mask{}, false
	}
	pix := make([]byte, 4*len(img.Pix))
	for i, v := range img.Pix {
		pix[4*i], pix[4*i+1], pix[4*i+2], pix[4*i+3] = 0xff, 0xff, 0xff, v
	}
	tex, err := textureFromPixels(renderer, img.Rect.Dx(), img.Rect.Dy(), pix, 4*img.Rect.Dx())
	if err != nil {
		log.Printf("shape mask: %v", err)
		return mask{}, false
	}
	sdl.SetTextureScaleMode(tex, sdl.ScaleModeNearest)
	m := mask{texture: tex, w: int32(img.Rect.Dx()), h: int32(img.Rect.Dy()), pad: pad, sliceX: sliceX, sliceY: sliceY}
	m.hollow = hollowMiddle(img, sliceX, sliceY)
	a.masks.add(key, m)
	return m, true
}

// maskSlices is the mask spec is drawn from, which is its key in the
// cache, and the size of its slices. A rect only changes at its corners,
// so its mask needs to be no bigger than its corners, plus a middle to
// stretch.
func maskSlices(spec maskSpec, pad int32) (key maskSpec, sliceX, sliceY int32) {
	key = spec
	if spec.shape.Kind == "rect" {
		reach := max(spec.shape.radius[0], spec.shape.radius[1], spec.shape.radius[2], spec.shape.radius[3], spec.border) + max(0, spec.spread) + max(abs32(spec.cutX), abs32(spec.cutY))
		corner := int32(math.Ceil(float64(reach))) + 2*pad + 1
		if spec.w+2*pad > 2*corner+2 {
			sliceX, key.w = corner, 2*(corner-pad)+2
		}
		if spec.h+2*pad > 2*corner+2 {
			sliceY, key.h = corner, 2*(corner-pad)+2
		}
	}
	return key, sliceX, sliceY
}

// hollowMiddle reports whether the middle of a mask sliced at sliceX
// and sliceY is transparent throughout.
func hollowMiddle(img *image.Alpha, sliceX, sliceY int32) bool {
	if sliceX == 0 || sliceY == 0 {
		return false
	}
	for y := int(sliceY); y < img.Rect.Dy()-int(sliceY); y++ {
		for x := int(sliceX); x < img.Rect.Dx()-int(sliceX); x++ {
			if img.AlphaAt(x, y).A != 0 {
				return false
			}
		}
	}
	return true
}

// rasterMask rasterises spec's mask with at least pad pixels around the
// box, and more for a path reaching past it, returning the pad it used.
func (a *App) rasterMask(spec maskSpec, pad int32) (*image.Alpha, int32) {
	img, pad := a.rasterShapeMask(spec, pad)
	if img == nil {
		return nil, 0
	}
	stride := img.Stride
	if spec.invert {
		box := image.Rect(int(pad), int(pad), int(pad+spec.w), int(pad+spec.h))
		for y := range img.Rect.Dy() {
			for x := range img.Rect.Dx() {
				i := y*stride + x
				if image.Pt(x, y).In(box) {
					img.Pix[i] = 255 - img.Pix[i]
				} else {
					img.Pix[i] = 0
				}
			}
		}
	}
	if spec.cut {
		hole := a.rasterShape(spec.shape, img.Rect.Dx(), img.Rect.Dy(), float32(pad)+spec.cutX, float32(pad)+spec.cutY, float32(spec.w), float32(spec.h), 0)
		for i, v := range hole.Pix {
			img.Pix[i] = uint8(int(img.Pix[i]) * (255 - int(v)) / 255)
		}
	}
	return img, pad
}

func abs32(v float32) float32 { return max(v, -v) }

// rasterShapeMask rasterises the fill, border, stroke or shadow of
// spec's shape, as rasterMask describes.
func (a *App) rasterShapeMask(spec maskSpec, pad int32) (*image.Alpha, int32) {
	var ops []config.PathOp
	if spec.shape.Kind == "path" {
		var err error
		if ops, err = a.shapePath(spec.shape, float64(spec.w), float64(spec.h)); err != nil {
			spec.shape = boxShape{Shape: config.Shape{Kind: "rect"}} // a plain rect in place of a broken shape
		} else {
			pad += pathOverflow(ops, spec.w, spec.h, spec.shape.scale) + int32(math.Ceil(float64(max(0, spec.spread)+spec.stroke/2)))
		}
	}
	w, h := int(spec.w+2*pad), int(spec.h+2*pad)
	if w <= 0 || h <= 0 || w*h > 1<<26 {
		return nil, 0
	}
	fx, fy, fw, fh := float32(pad), float32(pad), float32(spec.w), float32(spec.h)
	switch {
	case spec.stroke > 0 && ops != nil:
		img := image.NewAlpha(image.Rect(0, 0, w, h))
		r := vector.NewRasterizer(w, h)
		strokePath(r, flattenPath(ops, pathPlacer(fx, fy, fw, fh, spec.shape.scale)), spec.stroke)
		r.Draw(img, img.Bounds(), image.Opaque, image.Point{})
		return img, pad
	case spec.border > 0:
		img := a.rasterShape(spec.shape, w, h, fx, fy, fw, fh, 0)
		// The border is what the shape covers beyond the same shape inset
		// on the bordered sides.
		var in [4]float32 // top, right, bottom, left
		for i, side := range []config.Sides{config.SideTop, config.SideRight, config.SideBottom, config.SideLeft} {
			if spec.sides&side != 0 {
				in[i] = spec.border
			}
		}
		inner := spec.shape
		for i, sides := range [4][2]int{{0, 3}, {0, 1}, {2, 1}, {2, 3}} { // each corner's two sides
			inner.radius[i] = max(0, inner.radius[i]-max(in[sides[0]], in[sides[1]]))
		}
		hole := a.rasterShape(inner, w, h, fx+in[3], fy+in[0], fw-in[1]-in[3], fh-in[0]-in[2], 0)
		for i, v := range hole.Pix {
			img.Pix[i] = uint8(max(0, int(img.Pix[i])-int(v)))
		}
		return img, pad
	case spec.spread != 0 || spec.blur > 0:
		img := a.rasterShape(spec.shape, w, h, fx-spec.spread, fy-spec.spread, fw+2*spec.spread, fh+2*spec.spread, spec.spread)
		blurAlpha(img, float64(spec.blur)/2)
		return img, pad
	}
	return a.rasterShape(spec.shape, w, h, fx, fy, fw, fh, 0), pad
}

// pathOverflow is how far ops, laid over a w by h box, reach past it on
// any side.
func pathOverflow(ops []config.PathOp, w, h int32, scale float32) int32 {
	at := pathPlacer(0, 0, float32(w), float32(h), scale)
	var over float32
	for _, op := range ops {
		for _, p := range op.Pts {
			x, y := at(p)
			over = max(over, -x, -y, x-float32(w), y-float32(h))
		}
	}
	return int32(math.Ceil(float64(over)))
}

// pathPlacer places path points over the box at x, y, w by h; scale
// converts their pixels to output pixels.
func pathPlacer(x, y, w, h, scale float32) func(config.PathPoint) (float32, float32) {
	return func(p config.PathPoint) (float32, float32) {
		return x + float32(p.X.Frac)*w + float32(p.X.Px)*scale, y + float32(p.Y.Frac)*h + float32(p.Y.Px)*scale
	}
}

// rasterShape draws shape over the box at x, y, bw by bh, in a w by h
// mask; grow is how far the box was grown, which grows a rect's corners.
func (a *App) rasterShape(shape boxShape, w, h int, x, y, bw, bh, grow float32) *image.Alpha {
	img := image.NewAlpha(image.Rect(0, 0, w, h))
	if bw <= 0 || bh <= 0 {
		return img
	}
	r := vector.NewRasterizer(w, h)
	if shape.Kind == "path" {
		ops, err := a.shapePath(shape, float64(bw), float64(bh))
		if err == nil {
			tracePath(r, ops, x, y, bw, bh, shape.scale)
			r.Draw(img, img.Bounds(), image.Opaque, image.Point{})
			return img
		}
		shape.Kind = "rect" // draw a plain rect in place of a broken shape
	}
	var radius config.Corners
	for i, v := range shape.radius {
		radius[i] = float64(max(0, v+grow))
	}
	tracePath(r, config.RoundedRectPath(0, 0, float64(bw), float64(bh), radius), x, y, bw, bh, 1)
	r.Draw(img, img.Bounds(), image.Opaque, image.Point{})
	return img
}

// shapePath is the path of a path shape for a box bw by bh output pixels,
// from its text or its Lua function. An error is reported once per shape.
func (a *App) shapePath(shape boxShape, bw, bh float64) ([]config.PathOp, error) {
	var ops []config.PathOp
	var err error
	if shape.ops != "" {
		return unpackPath(shape.ops), nil
	} else if shape.Func != nil {
		ops, err = a.runtime.ShapePath(shape.Func, bw/float64(shape.scale), bh/float64(shape.scale))
	} else {
		ops, err = config.ParsePath(shape.Path)
	}
	if err != nil {
		a.reportThemeError(shape.Shape, "theme shape", err)
	}
	return ops, err
}

// reportThemeError reports err, from a shape or draw function the theme
// gave, once for each such key until the config changes.
func (a *App) reportThemeError(key any, what string, err error) {
	if a.themeErrors[key] {
		return
	}
	if a.themeErrors == nil {
		a.themeErrors = map[any]bool{}
	}
	a.themeErrors[key] = true
	log.Printf("%s: %v", what, err)
	a.message = what + ": " + err.Error()
}

// tracePath adds ops to r with the box at x, y, w by h; scale converts the
// ops' pixels to output pixels.
func tracePath(r *vector.Rasterizer, ops []config.PathOp, x, y, w, h, scale float32) {
	at := pathPlacer(x, y, w, h, scale)
	for _, op := range ops {
		switch op.Op {
		case 'M':
			r.MoveTo(at(op.Pts[0]))
		case 'L':
			r.LineTo(at(op.Pts[0]))
		case 'Q':
			cx, cy := at(op.Pts[0])
			px, py := at(op.Pts[1])
			r.QuadTo(cx, cy, px, py)
		case 'C':
			c1x, c1y := at(op.Pts[0])
			c2x, c2y := at(op.Pts[1])
			px, py := at(op.Pts[2])
			r.CubeTo(c1x, c1y, c2x, c2y, px, py)
		case 'Z':
			r.ClosePath()
		}
	}
	r.ClosePath()
}

// blurAlpha blurs img with a gaussian of standard deviation sigma pixels,
// approximated by three box blurs each way.
func blurAlpha(img *image.Alpha, sigma float64) {
	if sigma <= 0 {
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	buf := make([]float32, w*h)
	for i, v := range img.Pix {
		buf[i] = float32(v)
	}
	tmp := make([]float32, max(w, h))
	for _, radius := range boxBlurRadii(sigma) {
		for y := range h {
			boxBlurLine(buf[y*w:(y+1)*w], 1, tmp[:w], radius)
		}
		for x := range w {
			boxBlurLine(buf[x:], w, tmp[:h], radius)
		}
	}
	for i, v := range buf {
		img.Pix[i] = uint8(min(255, max(0, v+0.5)))
	}
}

// boxBlurRadii are the radii of three box blurs that together approximate
// a gaussian of standard deviation sigma.
func boxBlurRadii(sigma float64) [3]int {
	ideal := math.Sqrt(12*sigma*sigma/3 + 1)
	lower := int(math.Floor(ideal))
	if lower%2 == 0 {
		lower--
	}
	upper := lower + 2
	m := int(math.Round((12*sigma*sigma - 3*float64(lower*lower) - 12*float64(lower) - 9) / (-4*float64(lower) - 4)))
	var radii [3]int
	for i := range radii {
		size := upper
		if i < m {
			size = lower
		}
		radii[i] = max(0, (size-1)/2)
	}
	return radii
}

// boxBlurLine blurs the n = len(tmp) values of line, stride apart, by
// averaging each with radius values either side; beyond the ends is 0.
func boxBlurLine(line []float32, stride int, tmp []float32, radius int) {
	n := len(tmp)
	for i := range n {
		tmp[i] = line[i*stride]
	}
	var sum float32
	for i := 0; i < min(radius, n); i++ {
		sum += tmp[i]
	}
	scale := 1 / float32(2*radius+1)
	for i := range n {
		if j := i + radius; j < n {
			sum += tmp[j]
		}
		line[i*stride] = sum * scale
		if j := i - radius; j >= 0 {
			sum -= tmp[j]
		}
	}
}

// maskCache keeps shape masks, evicting the least recently used once
// full. Its zero value is empty and ready to use.
type maskCache struct {
	entries map[maskSpec]*list.Element // values are maskCacheEntry
	order   list.List
	bytes   int // the size of the masks' textures
}

// A mask the size of its box, as a path shape's is, takes a texture the
// size of a panel, so the cache is limited in bytes as well as entries.
const (
	maxMaskCacheEntries = 256
	maxMaskCacheBytes   = 64 << 20
)

type maskCacheEntry struct {
	key  maskSpec
	mask mask
}

func (c *maskCache) get(key maskSpec) (mask, bool) {
	elem, ok := c.entries[key]
	if !ok {
		return mask{}, false
	}
	c.order.MoveToBack(elem)
	return elem.Value.(maskCacheEntry).mask, true
}

func (c *maskCache) add(key maskSpec, m mask) {
	if c.entries == nil {
		c.entries = map[maskSpec]*list.Element{}
	}
	c.entries[key] = c.order.PushBack(maskCacheEntry{key: key, mask: m})
	c.bytes += m.bytes()
	for c.order.Len() > 1 && (len(c.entries) > maxMaskCacheEntries || c.bytes > maxMaskCacheBytes) {
		entry := c.order.Remove(c.order.Front()).(maskCacheEntry)
		delete(c.entries, entry.key)
		c.bytes -= entry.mask.bytes()
		destroyTexture(entry.mask.texture)
	}
}

func (m mask) bytes() int { return 4 * int(m.w) * int(m.h) }

func (c *maskCache) clear() {
	for _, elem := range c.entries {
		destroyTexture(elem.Value.(maskCacheEntry).mask.texture)
	}
	c.entries = nil
	c.order.Init()
	c.bytes = 0
}

// drawTextHighlight marks rect on a page the way a highlighter would: on a
// light page it multiplies, so the text under it stays dark, and on a dark
// page it adds, so light text stays light.
func (a *App) drawTextHighlight(renderer *sdl.Renderer, rect sdl.FRect, clr color.RGBA) {
	mode := sdl.BlendMode(sdl.BlendModeMod)
	if !isLight(a.pageBackgroundColor()) {
		mode = sdl.BlendModeAdd
		clr.A = 0xa0
	}
	sdl.SetRenderDrawBlendMode(renderer, mode)
	fillRect(renderer, rect, clr)
	sdl.SetRenderDrawBlendMode(renderer, sdl.BlendModeBlend)
}

// withClip runs draw with drawing limited to rect, inside any clip already
// in place, so content such as scrolled text stays within its box.
func (a *App) withClip(renderer *sdl.Renderer, rect sdl.FRect, draw func() error) error {
	clip := sdl.Rect{
		X: int32(math.Floor(float64(rect.X))),
		Y: int32(math.Floor(float64(rect.Y))),
		W: int32(math.Ceil(float64(rect.X+rect.W))) - int32(math.Floor(float64(rect.X))),
		H: int32(math.Ceil(float64(rect.Y+rect.H))) - int32(math.Floor(float64(rect.Y))),
	}
	if n := len(a.clips); n > 0 {
		clip = intersectRects(clip, a.clips[n-1])
	}
	a.clips = append(a.clips, clip)
	sdl.SetRenderClipRect(renderer, &clip)
	defer func() {
		a.clips = a.clips[:len(a.clips)-1]
		if n := len(a.clips); n > 0 {
			sdl.SetRenderClipRect(renderer, &a.clips[n-1])
		} else {
			sdl.SetRenderClipRect(renderer, nil)
		}
	}()
	return draw()
}

func intersectRects(a, b sdl.Rect) sdl.Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	return sdl.Rect{X: x0, Y: y0, W: max(0, x1-x0), H: max(0, y1-y0)}
}

// flattenPath turns ops into polylines through the points at places,
// curves divided into short lines. A closed subpath ends at its start.
func flattenPath(ops []config.PathOp, at func(config.PathPoint) (float32, float32)) [][]point32 {
	var lines [][]point32
	var line []point32
	var start, current point32
	pt := func(p config.PathPoint) point32 {
		x, y := at(p)
		return point32{x, y}
	}
	flush := func() {
		if len(line) > 1 {
			lines = append(lines, line)
		}
		line = nil
	}
	const steps = 16
	for _, op := range ops {
		switch op.Op {
		case 'M':
			flush()
			start = pt(op.Pts[0])
			current = start
			line = []point32{start}
		case 'L':
			current = pt(op.Pts[0])
			line = append(line, current)
		case 'Q':
			c, end := pt(op.Pts[0]), pt(op.Pts[1])
			for i := 1; i <= steps; i++ {
				t := float32(i) / steps
				u := 1 - t
				line = append(line, point32{u*u*current.x + 2*u*t*c.x + t*t*end.x, u*u*current.y + 2*u*t*c.y + t*t*end.y})
			}
			current = end
		case 'C':
			c1, c2, end := pt(op.Pts[0]), pt(op.Pts[1]), pt(op.Pts[2])
			for i := 1; i <= steps; i++ {
				t := float32(i) / steps
				u := 1 - t
				a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				line = append(line, point32{a*current.x + b*c1.x + c*c2.x + d*end.x, a*current.y + b*c1.y + c*c2.y + d*end.y})
			}
			current = end
		case 'Z':
			line = append(line, start)
			current = start
			flush()
			line = []point32{start}
		}
	}
	flush()
	return lines
}

type point32 struct{ x, y float32 }

// strokePath adds a line width pixels wide along each polyline to r, with
// round joins where the line turns. Every piece is wound the same way, so
// where pieces overlap their coverage adds up rather than cancelling.
func strokePath(r *vector.Rasterizer, lines [][]point32, width float32) {
	half := width / 2
	for _, line := range lines {
		closed := line[0] == line[len(line)-1]
		for i := 0; i+1 < len(line); i++ {
			p, q := line[i], line[i+1]
			dx, dy := q.x-p.x, q.y-p.y
			length := float32(math.Hypot(float64(dx), float64(dy)))
			if length == 0 {
				continue
			}
			nx, ny := -dy/length*half, dx/length*half
			addPolygon(r, []point32{{p.x + nx, p.y + ny}, {q.x + nx, q.y + ny}, {q.x - nx, q.y - ny}, {p.x - nx, p.y - ny}})
		}
		for i, p := range line {
			var prev, next point32
			switch {
			case i > 0 && i+1 < len(line):
				prev, next = line[i-1], line[i+1]
			case closed && len(line) > 2:
				prev, next = line[len(line)-2], line[1]
			default:
				continue // an open end is cut square
			}
			if turns(prev, p, next) {
				addPolygon(r, disc(p, half))
			}
		}
	}
}

// turns reports whether a line through a, b and c bends at b by more
// than a curve's divisions do.
func turns(a, b, c point32) bool {
	ux, uy, vx, vy := b.x-a.x, b.y-a.y, c.x-b.x, c.y-b.y
	lu, lv := math.Hypot(float64(ux), float64(uy)), math.Hypot(float64(vx), float64(vy))
	if lu == 0 || lv == 0 {
		return false
	}
	return float64(ux*vx+uy*vy)/(lu*lv) < math.Cos(15*math.Pi/180)
}

func disc(c point32, r float32) []point32 {
	n := max(8, min(32, int(r*4)))
	points := make([]point32, n)
	for i := range points {
		angle := 2 * math.Pi * float64(i) / float64(n)
		points[i] = point32{c.x + r*float32(math.Cos(angle)), c.y + r*float32(math.Sin(angle))}
	}
	return points
}

// addPolygon adds the closed polygon through points to r, wound clockwise
// on screen.
func addPolygon(r *vector.Rasterizer, points []point32) {
	var area float32
	for i, p := range points {
		q := points[(i+1)%len(points)]
		area += p.x*q.y - q.x*p.y
	}
	if area < 0 {
		slices.Reverse(points)
	}
	r.MoveTo(points[0].x, points[0].y)
	for _, p := range points[1:] {
		r.LineTo(p.x, p.y)
	}
	r.ClosePath()
}
