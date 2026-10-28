package viewer

import (
	"math"
	"slices"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	lua "github.com/yuin/gopher-lua"
)

// A view's canvas draws buttons and check boxes as it draws text fields:
// each named, focused in turn with Tab, and clicked with the pointer or,
// focused, with Space or Enter.

// noteControl records a button or check box of view drawn at rect, for the
// pointer and the keys, and reports whether it is focused and whether the
// pointer is over it.
func (c *elementCanvas) noteControl(id string, rect sdl.FRect) (focused, hovered bool) {
	view, a := c.view, c.app
	if view.controls == nil {
		view.controls = map[string]sdl.FRect{}
	}
	view.controls[id] = rect
	view.focusOrder = append(view.focusOrder, id)
	view.regions = append(view.regions, uiRegion{id: lua.LString(id), rect: rect})
	p := a.pointer
	hovered = c.pointerOver && pointInFRect(p.X, p.Y, rect)
	return view.focus == id && a.keyView() == view, hovered
}

func (c *elementCanvas) Button(id string, x, y, w float64, label string) float64 {
	if c.view == nil || c.app.fontFace == nil {
		return 0
	}
	a := c.app
	st := a.style(config.ElementButton)
	padTop, padRight, padBottom, padLeft := a.insets(st.Padding.V)
	scale := float64(a.px(1))
	h := (float64(a.uiLineHeight()) + float64(padTop+padBottom)) / scale
	rect := c.rect(x, y, w, h)
	focused, hovered := c.noteControl(id, rect)
	if focused || hovered || c.view.pressedControl == id {
		st = a.style(config.ElementButtonFocused)
	}
	if c.renderer == nil || !c.visible(rect) {
		return h
	}
	a.drawBox(c.renderer, &st, rect)
	face := a.styleFace(&st)
	text := truncateText(face, label, int(rect.W-padLeft-padRight))
	tx := rect.X + (rect.W-float32(measureText(face, text)))/2
	baseline := int(rect.Y+padTop) + face.Metrics().Ascent.Ceil()
	a.drawTextFace(c.renderer, text, int(math.Round(float64(tx))), baseline, a.textColor(&st, false), st.Bold.V)
	c.restoreClip()
	return h
}

func (c *elementCanvas) Checkbox(id string, x, y float64, label string, checked bool) (float64, float64) {
	if c.view == nil || c.app.fontFace == nil {
		return 0, 0
	}
	a := c.app
	scale := float64(a.px(1))
	size := float64(a.uiLineHeight()) / scale
	gap := 0.0
	if label != "" {
		gap = 8 + float64(measureText(a.fontFace, label))/scale
	}
	rect := c.rect(x, y, size+gap, size)
	focused, hovered := c.noteControl(id, rect)
	if c.renderer == nil || !c.visible(rect) {
		return size + gap, size
	}
	a.drawCheckbox(c.renderer, sdl.FRect{X: rect.X, Y: rect.Y, W: rect.H, H: rect.H}, checked, focused || hovered)
	if label != "" {
		st := a.style(config.ElementRow)
		baseline := int(rect.Y) + (int(rect.H)+a.fontFace.Metrics().Ascent.Ceil()-a.fontFace.Metrics().Descent.Ceil())/2
		a.drawText(c.renderer, label, int(rect.X+rect.H)+a.ipx(8), baseline, a.textColor(&st, false))
	}
	return size + gap, size
}

// drawCheckbox draws a check box over box: an input's box, focused or not,
// with the check element's mark in it when checked.
func (a *App) drawCheckbox(renderer *sdl.Renderer, box sdl.FRect, checked, focused bool) {
	element := config.ElementInput
	if focused {
		element = config.ElementInputFocused
	}
	st := a.style(element)
	a.drawBox(renderer, &st, box)
	if checked {
		mark := a.style(config.ElementCheck)
		inset := box.W / 4
		a.drawBox(renderer, &mark, sdl.FRect{X: box.X + inset, Y: box.Y + inset, W: box.W - 2*inset, H: box.H - 2*inset})
	}
}

// clickUIControl clicks view's button or check box id.
func (a *App) clickUIControl(view *uiView, id string) {
	a.pendingRedraw = true
	if view.onClick != nil {
		view.onClick(a, id)
	}
}

// uiTarget is what of a view the pointer can work.
type uiTarget int

const (
	targetNone    uiTarget = iota
	targetField            // a text field
	targetPrompt           // the header's prompt
	targetControl          // a button or check box
)

// controlAt is what of view is at x, y that the pointer works, with its
// id, as last drawn: one hit-test for pressing and for the cursor.
func (view *uiView) controlAt(x, y float32) (uiTarget, string) {
	for _, id := range view.focusOrder {
		if f := view.fields[id]; f != nil && pointInFRect(x, y, f.rect) {
			return targetField, id
		}
		if r, ok := view.controls[id]; ok && pointInFRect(x, y, r) {
			return targetControl, id
		}
	}
	if view.prompt != "" && pointInFRect(x, y, view.promptRect) {
		return targetPrompt, promptFocus
	}
	return targetNone, ""
}

// pressUIControl handles a press at x, y in view on a text field or the
// prompt, which it focuses, putting the cursor there and starting a
// selection a drag stretches, or on a button or check box, which it
// focuses and presses, its release over it clicking it. It reports
// whether there was one.
func (a *App) pressUIControl(view *uiView, x, y float32) bool {
	target, id := view.controlAt(x, y)
	press := func(l *textLayout, input *textInput) {
		if l.face != nil && len(l.lines) > 0 {
			pressText(l, input, x, y, false)
			input.mouseSelecting = true
		}
	}
	switch target {
	case targetNone:
		return false
	case targetField:
		f := view.fields[id]
		press(&f.layout, &f.input)
	case targetPrompt:
		press(&view.promptLayout, &view.promptInput)
	case targetControl:
		view.pressedControl = id
	}
	view.focus = id
	if !view.modal {
		a.focusUIView(view, true)
	}
	a.pendingRedraw = true
	return true
}

// releaseUIControl handles the release of a button or check box pressed,
// clicking it if the pointer is still over it, and reports whether one was
// pressed.
func (a *App) releaseUIControl(view *uiView, x, y float32) bool {
	id := view.pressedControl
	if id == "" {
		return false
	}
	view.pressedControl = ""
	if r := view.controls[id]; pointInFRect(x, y, r) {
		a.clickUIControl(view, id)
	}
	a.pendingRedraw = true
	return true
}

// handleUIControlKey handles the keys that move between a view's controls,
// Tab and Shift-Tab, and click a focused button or check box, Space and
// Enter, reporting whether it took the key.
func (a *App) handleUIControlKey(view *uiView, name string) bool {
	switch name {
	case "<Tab>", "<S-Tab>":
		order := view.tabOrder()
		n := len(order)
		if n < 2 && (n == 0 || order[0] == view.focus) {
			return false
		}
		step := 1
		if name == "<S-Tab>" {
			step = n - 1
		}
		i := slices.Index(order, view.focus)
		if i < 0 {
			i = n - 1 // the first Tab focuses the first control
			if step != 1 {
				i = 0
			}
		}
		view.focus = order[(i+step)%n]
		if f := view.fields[view.focus]; f != nil {
			f.input.SelectAll()
		}
		a.syncTextInput()
		a.pendingRedraw = true
		return true
	case "<Space>", "<CR>":
		if _, ok := view.controls[view.focus]; ok && slices.Contains(view.focusOrder, view.focus) {
			a.clickUIControl(view, view.focus)
			return true
		}
	}
	return false
}
