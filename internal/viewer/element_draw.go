package viewer

import (
	"encoding/binary"
	"math"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// runDrawFunction draws a box with its style's draw function, reporting
// whether the function drew the default box. If the function fails, the
// error is reported, the function is not called again until the config
// changes, and the box is drawn as it would be without it.
func (a *App) runDrawFunction(renderer *sdl.Renderer, st *config.Style, rect sdl.FRect, parts boxParts) bool {
	return a.drawElement(renderer, st, rect, func() { a.drawDefaultBox(renderer, st, rect, parts) })
}

// drawElement draws an element over rect: with its style's draw function
// if it has one, canvas:default() running drawDefault, and otherwise with
// drawDefault. It reports whether the default was drawn.
func (a *App) drawElement(renderer *sdl.Renderer, st *config.Style, rect sdl.FRect, drawDefault func()) bool {
	if st.Draw.V == nil || a.themeErrors[st.Draw.V] {
		drawDefault()
		return true
	}
	box := pixelRect(rect)
	canvas := &elementCanvas{app: a, renderer: renderer, style: st, box: box, drawDefault: drawDefault}
	scale := float64(a.px(1))
	state := config.DrawState{
		Element: st.Element,
		X:       float64(box.X) / scale, Y: float64(box.Y) / scale,
		W: float64(box.W) / scale, H: float64(box.H) / scale,
		Alt:  a.altColors,
		Time: time.Since(startTime).Seconds(),
	}
	err := a.runtime.RunDraw(st.Draw.V, canvas, state)
	canvas.Unclip()
	if err != nil {
		a.reportThemeError(st.Draw.V, "theme draw "+st.Element.String(), err)
		if !canvas.drewDefault {
			canvas.Default()
		}
	}
	return canvas.drewDefault
}

// elementCanvas is the canvas a draw function draws an element's box, or
// a plugin's view, on.
type elementCanvas struct {
	app         *App
	renderer    *sdl.Renderer
	style       *config.Style
	box         sdl.Rect
	drawDefault func()
	drewDefault bool
	// clip is the canvas's own clip, while clipped.
	clip    sdl.Rect
	clipped bool
	// view is the view the canvas draws, if it draws one, and pointerOver
	// whether the pointer is over it, for its regions.
	view        *uiView
	pointerOver bool
	// extent is how far below the box's top the canvas has drawn, in
	// output pixels.
	extent float64
}

func (c *elementCanvas) Default() {
	if c.drewDefault {
		return
	}
	c.drewDefault = true
	c.drawDefault()
	// What the default drew clipped itself, leaving the clip it found.
	if c.clipped && c.renderer != nil {
		sdl.SetRenderClipRect(c.renderer, &c.clip)
	}
}

func (c *elementCanvas) Fill(path []config.PathOp, clr config.Color) {
	c.drawPath(path, clr, 0)
}

func (c *elementCanvas) Stroke(path []config.PathOp, clr config.Color, width float64) {
	c.drawPath(path, clr, c.app.lineWidth(width))
}

// drawPath fills path, or with stroke strokes it, as a shape on the box,
// so its mask is kept as a shape's is. The mask covers only the path's
// bounds, not the whole box: a line across a panel or a drop on a page
// takes a mask its own size, and the same path moved by whole pixels, as
// one animated across the box is, finds the mask already made.
func (c *elementCanvas) drawPath(path []config.PathOp, clr config.Color, stroke float32) {
	if len(path) == 0 || c.box.W <= 0 || c.box.H <= 0 {
		return
	}
	ops, bounds := fitPath(path, float32(c.box.W), float32(c.box.H), c.app.px(1))
	c.extend(float64(bounds.Y + bounds.H))
	rgba := c.app.styleColor(clr, c.style.Opacity.V)
	if rgba.A == 0 || c.renderer == nil {
		return
	}
	shape := boxShape{Shape: config.Shape{Kind: "path"}, ops: c.app.pathKeys.pack(ops), scale: 1}
	spec := maskSpec{shape: shape, w: bounds.W, h: bounds.H, stroke: stroke}
	c.app.drawMask(c.renderer, spec, c.box.X+bounds.X, c.box.Y+bounds.Y, rgba)
}

// fitPath lays ops over a w by h box, scale output pixels to the logical
// pixel, and returns them in output pixels from the top left of the whole
// pixels they reach, with those pixels' place in the box. The bounds start
// on a whole pixel, so the path is rasterised as it would be over the box.
func fitPath(ops []config.PathOp, w, h, scale float32) ([]config.PathOp, sdl.Rect) {
	at := pathPlacer(0, 0, w, h, scale)
	minX, minY := float32(math.Inf(1)), float32(math.Inf(1))
	maxX, maxY := float32(math.Inf(-1)), float32(math.Inf(-1))
	for _, op := range ops {
		for _, p := range op.Pts[:opPoints(op.Op)] {
			x, y := at(p)
			minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
		}
	}
	if minX > maxX {
		return nil, sdl.Rect{}
	}
	left, top := math.Floor(float64(minX)), math.Floor(float64(minY))
	bounds := sdl.Rect{
		X: int32(left), Y: int32(top),
		// at least a pixel each way, so a straight line still has a box to stroke in
		W: max(1, int32(math.Ceil(float64(maxX)))-int32(left)),
		H: max(1, int32(math.Ceil(float64(maxY)))-int32(top)),
	}
	fitted := make([]config.PathOp, len(ops))
	for i, op := range ops {
		fitted[i].Op = op.Op
		for j, p := range op.Pts[:opPoints(op.Op)] {
			x, y := at(p)
			fitted[i].Pts[j] = config.PathPoint{
				X: config.Length{Px: float64(x) - left},
				Y: config.Length{Px: float64(y) - top},
			}
		}
	}
	return fitted, bounds
}

// opPoints is how many of its points a path op uses.
func opPoints(op byte) int {
	switch op {
	case 'M', 'L':
		return 1
	case 'Q':
		return 2
	case 'C':
		return 3
	}
	return 0
}

// pathKeys packs paths for the mask cache's keys, keeping the strings
// it made so a path drawn on every frame is packed without allocating.
type pathKeys struct {
	buf  []byte
	keys map[string]string
}

// maxPathKeys bounds the strings kept, which are let go when full, in
// case a function draws ever new paths.
const maxPathKeys = 256

// pathOpBytes is the size of a packed path op: its op and three points
// of two lengths of two float64s.
const pathOpBytes = 1 + 3*4*8

// pack is ops packed: each op's byte and the exact bits of its points,
// cheaper to make than its path data and as unique.
func (k *pathKeys) pack(ops []config.PathOp) string {
	buf := k.buf[:0]
	for _, op := range ops {
		buf = append(buf, op.Op)
		for _, p := range op.Pts {
			for _, v := range [4]float64{p.X.Frac, p.X.Px, p.Y.Frac, p.Y.Px} {
				buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(v))
			}
		}
	}
	k.buf = buf
	if key, ok := k.keys[string(buf)]; ok {
		return key
	}
	if k.keys == nil || len(k.keys) >= maxPathKeys {
		k.keys = map[string]string{}
	}
	key := string(buf)
	k.keys[key] = key
	return key
}

// unpackPath is the path pack packed.
func unpackPath(packed string) []config.PathOp {
	ops := make([]config.PathOp, 0, len(packed)/pathOpBytes)
	for b := []byte(packed); len(b) >= pathOpBytes; b = b[pathOpBytes:] {
		op := config.PathOp{Op: b[0]}
		next := func(i int) float64 { return math.Float64frombits(binary.LittleEndian.Uint64(b[1+8*i:])) }
		for i := range op.Pts {
			op.Pts[i] = config.PathPoint{
				X: config.Length{Frac: next(4 * i), Px: next(4*i + 1)},
				Y: config.Length{Frac: next(4*i + 2), Px: next(4*i + 3)},
			}
		}
		ops = append(ops, op)
	}
	return ops
}
