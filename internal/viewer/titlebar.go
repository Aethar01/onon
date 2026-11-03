package viewer

import (
	"image/color"
	"math"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A window without a system title bar keeps its controls hidden until the
// pointer reaches the top strip, then fades them in over the page. The strip
// itself drags the window. Sizes are in logical pixels.
const (
	titleBarHeight       = 32
	titleBarButtonWidth  = 46
	titleBarGlyphSize    = 10
	titleBarResizeBorder = 6
	titleBarFade         = 160 * time.Millisecond
	// titleBarHoverPoll is how often the loop checks whether the pointer
	// left the strip, which reports no motion while it drags the window.
	titleBarHoverPoll = 50 * time.Millisecond
)

type titleButton int

const (
	titleButtonNone titleButton = iota
	titleButtonMinimize
	titleButtonMaximize
	titleButtonClose
)

type titleBar struct {
	shown    bool // the controls are wanted, though they may still be fading
	fadeFrom float64
	fadeAt   time.Time
	hovered  titleButton
	pressed  titleButton
}

// alpha is how opaque the controls are at now.
func (t *titleBar) alpha(now time.Time) float64 {
	target := 0.0
	if t.shown {
		target = 1
	}
	progress := min(1, float64(now.Sub(t.fadeAt))/float64(titleBarFade))
	return t.fadeFrom + (target-t.fadeFrom)*progress
}

func (t *titleBar) fading(now time.Time) bool {
	return now.Sub(t.fadeAt) < titleBarFade
}

func (t *titleBar) setShown(shown bool, now time.Time) bool {
	if t.shown == shown {
		return false
	}
	t.fadeFrom = t.alpha(now)
	t.fadeAt = now
	t.shown = shown
	if !shown {
		t.hovered, t.pressed = titleButtonNone, titleButtonNone
	}
	return true
}

// titleBarLayout is the title bar's geometry in render pixels.
type titleBarLayout struct {
	width, height, buttonWidth, border float64
	resizable                          bool
}

func newTitleBarLayout(width int, scale float64, resizable bool) titleBarLayout {
	return titleBarLayout{
		width:       float64(width),
		height:      math.Round(titleBarHeight * scale),
		buttonWidth: math.Round(titleBarButtonWidth * scale),
		border:      math.Round(titleBarResizeBorder * scale),
		resizable:   resizable,
	}
}

// buttonRect is the rectangle of b, the buttons running minimize, maximize,
// close up to the right edge.
func (l titleBarLayout) buttonRect(b titleButton) sdl.FRect {
	x := l.width - float64(titleButtonClose-b+1)*l.buttonWidth
	return sdl.FRect{X: float32(x), Y: 0, W: float32(l.buttonWidth), H: float32(l.height)}
}

func (l titleBarLayout) buttonAt(x, y float64) titleButton {
	if y < 0 || y >= l.height {
		return titleButtonNone
	}
	for b := titleButtonMinimize; b <= titleButtonClose; b++ {
		r := l.buttonRect(b)
		if x >= float64(r.X) && x < float64(r.X+r.W) {
			return b
		}
	}
	return titleButtonNone
}

// hitTest tells the system what the point at x, y does: resizes at the
// window edges, drags in the strip, and is the window's own elsewhere. The
// buttons are the window's own, so they get the clicks.
func (l titleBarLayout) hitTest(x, y, windowHeight float64) sdl.HitTestResult {
	if l.resizable {
		left, right := x < l.border, x >= l.width-l.border
		top, bottom := y < l.border, y >= windowHeight-l.border
		switch {
		case top && left:
			return sdl.HitTestResizeTopLeft
		case top && right:
			return sdl.HitTestResizeTopRight
		case bottom && left:
			return sdl.HitTestResizeBottomLeft
		case bottom && right:
			return sdl.HitTestResizeBottomRight
		case left:
			return sdl.HitTestResizeLeft
		case right:
			return sdl.HitTestResizeRight
		case bottom:
			return sdl.HitTestResizeBottom
		case top && l.buttonAt(x, y) == titleButtonNone:
			return sdl.HitTestResizeTop
		}
	}
	if y < l.height && l.buttonAt(x, y) == titleButtonNone {
		return sdl.HitTestDraggable
	}
	return sdl.HitTestNormal
}

func (a *App) windowMaximized() bool {
	return a.window != nil && sdl.GetWindowFlags(a.window)&sdl.WindowMaximized != 0
}

func (a *App) titleBarLayout() titleBarLayout {
	return newTitleBarLayout(a.winW, a.displayScale(), !a.windowMaximized())
}

// titleBarActive reports whether the window has a title bar of onon's own.
// A fullscreen window has none at all.
func (a *App) titleBarActive() bool {
	return a.titleBar != nil && !a.fullscreen
}

// titleBarHitTest answers the system's hit test at x, y in render pixels.
// The system asks on every pointer move, including over the strip, where
// SDL reports no motion, so this is also where the controls are revealed.
func (a *App) titleBarHitTest(x, y float64) sdl.HitTestResult {
	if !a.titleBarActive() {
		return sdl.HitTestNormal
	}
	l := a.titleBarLayout()
	a.noteTitleBarPointer(x, y, true)
	result := l.hitTest(x, y, float64(a.winH))
	if result == sdl.HitTestDraggable && a.tabAt(x, y) >= 0 {
		return sdl.HitTestNormal // a tab is clicked, the strip between them drags
	}
	return result
}

// noteTitleBarPointer shows the controls while x, y is in the strip and
// tracks the button under it. inWindow is false once the pointer left.
func (a *App) noteTitleBarPointer(x, y float64, inWindow bool) {
	if !a.titleBarActive() {
		return
	}
	now := time.Now()
	t := a.titleBar
	l := a.titleBarLayout()
	in := inWindow && y >= 0 && y < l.height && x >= 0 && x < l.width
	// A pressed button stays shown until released, wherever the pointer is,
	// and the buttons stay in the tab bar while it shares their row.
	changed := t.setShown(in || t.pressed != titleButtonNone || a.tabBarShown(), now)
	hovered := titleButtonNone
	if in {
		hovered = l.buttonAt(x, y)
	}
	if hovered != t.hovered {
		t.hovered = hovered
		changed = true
	}
	if changed {
		a.pendingRedraw = true
	}
}

// advanceTitleBar hides the controls once the pointer left the strip, which
// SDL does not report while the pointer is over it, and keeps a fade drawing.
func (a *App) advanceTitleBar() {
	if !a.titleBarActive() {
		if a.titleBar != nil && a.titleBar.setShown(false, time.Now()) {
			a.pendingRedraw = true
		}
		return
	}
	t := a.titleBar
	if a.tabBarShown() && t.setShown(true, time.Now()) {
		a.pendingRedraw = true // in the tab bar's row, the buttons stay
	}
	// A press the system took for its own, as Windows does the maximize
	// button's, may be released where the window hears nothing of it.
	if t.pressed != titleButtonNone && sdl.GetGlobalMouseState(nil, nil)&sdl.ButtonLMask == 0 {
		t.pressed = titleButtonNone
		a.pendingRedraw = true
	}
	if t.shown && t.pressed == titleButtonNone {
		x, y, ok := a.pointerInWindow()
		a.noteTitleBarPointer(x, y, ok)
	}
	if t.fading(time.Now()) {
		a.pendingRedraw = true
	}
}

// pointerInWindow is the pointer in render pixels, from the system rather
// than from events, and whether it is over the window.
func (a *App) pointerInWindow() (x, y float64, ok bool) {
	if a.window == nil {
		return 0, 0, false
	}
	var gx, gy float32
	sdl.GetGlobalMouseState(&gx, &gy)
	var wx, wy int32
	if !sdl.GetWindowPosition(a.window, &wx, &wy) {
		return 0, 0, false
	}
	density := float64(sdl.GetWindowPixelDensity(a.window))
	if density <= 0 {
		density = 1
	}
	x = (float64(gx) - float64(wx)) * density
	y = (float64(gy) - float64(wy)) * density
	return x, y, x >= 0 && y >= 0 && x < float64(a.winW) && y < float64(a.winH)
}

// titleBarWaitTimeout is how long the loop may sleep before the title bar
// needs it again, or 0 when it does not.
func (a *App) titleBarWaitTimeout() time.Duration {
	if !a.titleBarActive() {
		return 0
	}
	if a.titleBar.fading(time.Now()) {
		return a.animationFrameDuration()
	}
	if a.titleBar.shown {
		return titleBarHoverPoll
	}
	return 0
}

// titleBarButtonAt is the shown button at x, y in render pixels.
func (a *App) titleBarButtonAt(x, y float64) titleButton {
	if !a.titleBarActive() || !a.titleBar.shown {
		return titleButtonNone
	}
	return a.titleBarLayout().buttonAt(x, y)
}

// handleTitleBarButton presses and releases the window's buttons, reporting
// whether it took the event.
func (a *App) handleTitleBarButton(e *sdl.MouseButtonEvent) bool {
	if !a.titleBarActive() || e.Button != uint8(sdl.ButtonLeft) {
		return false
	}
	b := a.titleBarButtonAt(float64(e.X), float64(e.Y))
	return a.pressTitleBarButton(b, e.Type == sdl.EventMouseButtonDown)
}

// pressTitleBarButton presses b, or with down unset releases the button
// pressed over b, running it when b is that button. It reports whether it
// took the press or release.
func (a *App) pressTitleBarButton(b titleButton, down bool) bool {
	t := a.titleBar
	if down {
		if b == titleButtonNone {
			return false
		}
		t.pressed = b
		a.pendingRedraw = true
		return true
	}
	pressed := t.pressed
	if pressed == titleButtonNone {
		return false
	}
	t.pressed = titleButtonNone
	a.pendingRedraw = true
	if b != pressed {
		return true
	}
	switch pressed {
	case titleButtonMinimize:
		sdl.MinimizeWindow(a.window)
	case titleButtonMaximize:
		if a.windowMaximized() {
			sdl.RestoreWindow(a.window)
		} else {
			sdl.MaximizeWindow(a.window)
		}
	case titleButtonClose:
		a.quit = a.confirmDiscard("closing")
	}
	return true
}

// drawTitleBar draws the window's buttons over everything else, styled
// as the title_bar element so they read over any page.
func (a *App) drawTitleBar(renderer *sdl.Renderer) error {
	if !a.titleBarActive() {
		return nil
	}
	t := a.titleBar
	alpha := t.alpha(time.Now())
	if alpha <= 0 {
		return nil
	}
	return a.faded(alpha, func() error {
		l := a.titleBarLayout()
		first, last := l.buttonRect(titleButtonMinimize), l.buttonRect(titleButtonClose)
		bar := a.style(config.ElementTitleBar)
		a.drawBox(renderer, &bar, sdl.FRect{X: first.X, Y: 0, W: last.X + last.W - first.X, H: first.H})
		scale := a.displayScale()
		for b := titleButtonMinimize; b <= titleButtonClose; b++ {
			r := l.buttonRect(b)
			glyph := a.textColor(&bar, false)
			// The button pressed, or with none pressed the one under the
			// pointer, is lit.
			if t.pressed == b || t.hovered == b && t.pressed == titleButtonNone {
				element := config.ElementTitleButton
				if b == titleButtonClose {
					element = config.ElementTitleClose
				}
				st := a.style(element)
				if t.pressed == b {
					st.Opacity.V *= 0.8 // pressed, a little fainter
				}
				a.drawBox(renderer, &st, r)
				glyph = a.textColor(&st, false)
			}
			if err := a.drawTitleBarGlyph(renderer, b, r, glyph, scale); err != nil {
				return err
			}
		}
		return nil
	})
}

// drawTitleBarGlyph draws the symbol of b centered in r, with strokes as
// wide as a logical pixel.
func (a *App) drawTitleBarGlyph(renderer *sdl.Renderer, b titleButton, r sdl.FRect, clr color.RGBA, scale float64) error {
	size := float32(math.Round(titleBarGlyphSize * scale))
	stroke := float32(max(1, math.Round(scale)))
	x := float32(math.Round(float64(r.X + (r.W-size)/2)))
	y := float32(math.Round(float64(r.Y + (r.H-size)/2)))
	switch b {
	case titleButtonMinimize:
		return fillRect(renderer, sdl.FRect{X: x, Y: y + (size-stroke)/2, W: size, H: stroke}, clr)
	case titleButtonMaximize:
		if !a.windowMaximized() {
			return strokeRect(renderer, sdl.FRect{X: x, Y: y, W: size, H: size}, clr, int(stroke))
		}
		// Restore: a window in front with the edges of one behind it.
		offset := float32(math.Round(2 * scale))
		front := size - offset
		if err := strokeRect(renderer, sdl.FRect{X: x, Y: y + offset, W: front, H: front}, clr, int(stroke)); err != nil {
			return err
		}
		if err := fillRect(renderer, sdl.FRect{X: x + offset, Y: y, W: front, H: stroke}, clr); err != nil {
			return err
		}
		return fillRect(renderer, sdl.FRect{X: x + size - stroke, Y: y, W: stroke, H: front}, clr)
	case titleButtonClose:
		if !sdl.SetRenderDrawColor(renderer, clr.R, clr.G, clr.B, clr.A) {
			return sdlError("set draw color")
		}
		for i := float32(0); i < stroke; i++ {
			if !sdl.RenderLine(renderer, x+i, y, x+size-stroke+i, y+size) ||
				!sdl.RenderLine(renderer, x+size-stroke+i, y, x+i, y+size) {
				return sdlError("draw line")
			}
		}
	}
	return nil
}
