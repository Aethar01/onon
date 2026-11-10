package viewer

import (
	"math"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// An aside is a part of a list view beside its rows, on its left or right,
// under its header: the view grows to that side by its width, and a
// function draws in it, as a file browser draws the file selected. The
// rows, their column titles, the scrollbar and a drawn body keep to the
// rest of the panel.

type viewAside struct {
	side  string // left or right
	width config.UISize
	draw  func(a *App, canvas *elementCanvas, state config.DrawState) (bool, error)
}

// asideKey is what of an aside a view's geometry depends on.
type asideKey struct {
	side  string
	width config.UISize
}

func (s *viewAside) key() asideKey {
	if s == nil {
		return asideKey{}
	}
	return asideKey{s.side, s.width}
}

// maxAsideShare is the most of its view's width an aside takes, so its
// rows keep some.
const maxAsideShare = 0.6

// asideWidth is how wide view's aside is, as asked: a share of the window,
// or logical pixels; 0 for none.
func (a *App) asideWidth(view *uiView) int {
	if view.aside == nil {
		return 0
	}
	return max(0, int(math.Round(a.uiLength(view.aside.width, float64(a.winW)))))
}

// asideSplit is how much of panel, view's frame, its aside takes.
func (a *App) asideSplit(view *uiView, panel sdl.FRect) float32 {
	w := float32(a.asideWidth(view))
	return min(w, panel.W*maxAsideShare)
}

// listPart is the part of panel, view's frame, its rows take: all of it
// but its aside.
func (a *App) listPart(view *uiView, panel sdl.FRect) sdl.FRect {
	if view == nil || view.aside == nil {
		return panel
	}
	w := a.asideSplit(view, panel)
	panel.W -= w
	if view.aside.side == "left" {
		panel.X += w
	}
	return panel
}

// asideBox is where view's aside draws: its side of the frame, below the
// header, within the panel's padding but on the side of its rows, whose
// own padding parts them.
func (a *App) asideBox(view *uiView) sdl.FRect {
	frame, _ := view.frameGeometry(a)
	w := a.asideSplit(view, frame)
	top, right, bottom, left := a.viewInsets(view)
	head := float32(a.modalListHeadHeight())
	if view.headless {
		head = top
	}
	box := sdl.FRect{X: frame.X + frame.W - w, Y: frame.Y + head, W: w - right, H: frame.H - head - bottom}
	if view.aside.side == "left" {
		box = sdl.FRect{X: frame.X + left, Y: box.Y, W: w - left, H: box.H}
	}
	box.W, box.H = max(0, box.W), max(0, box.H)
	return box
}

// drawViewAside draws view's aside with its function, clipped to its box.
// If the function fails, the error is reported once and the aside is left
// empty until the configuration changes.
func (a *App) drawViewAside(renderer *sdl.Renderer, view *uiView) {
	aside := view.aside
	if aside == nil || aside.draw == nil || a.themeErrors[aside] {
		return
	}
	body := a.asideBox(view)
	if body.W <= 0 || body.H <= 0 {
		return
	}
	st := a.style(view.element)
	st.Opacity.V = 1
	canvas := &elementCanvas{app: a, renderer: renderer, style: &st, box: pixelRect(body), view: view, drawDefault: func() {}}
	scale := float64(a.px(1))
	state := config.DrawState{
		Element: view.element,
		X:       float64(body.X) / scale, Y: float64(body.Y) / scale,
		W: float64(body.W) / scale, H: float64(body.H) / scale,
		Alt:  a.altColors,
		Time: time.Since(startTime).Seconds(),
	}
	err := a.withClip(renderer, body, func() error {
		again, err := aside.draw(a, canvas, state)
		canvas.Unclip()
		if again {
			a.pendingRedraw = true
		}
		return err
	})
	if err != nil {
		a.reportThemeError(aside, "ui aside", err)
	}
}

// SetUIAside gives the view id an aside, or takes it away for nil.
func (a *App) SetUIAside(id string, aside *config.UIAside) {
	view := a.views.views[id]
	if view == nil {
		return
	}
	view.aside = a.luaAside(view, aside)
	a.pendingRedraw = true
}

// luaAside is the aside spec asks for, of a plugin's view: nil for none.
func (a *App) luaAside(view *uiView, spec *config.UIAside) *viewAside {
	if spec == nil {
		return nil
	}
	rt := a.runtime
	callback := spec.Draw
	return &viewAside{
		side:  spec.Side,
		width: spec.Width,
		draw: func(a *App, canvas *elementCanvas, state config.DrawState) (bool, error) {
			if a.staleLuaView(view) || callback == "" {
				return false, nil // its function is the old generation's
			}
			return rt.RunUIDraw(callback, canvas, state)
		},
	}
}
