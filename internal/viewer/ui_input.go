package viewer

import (
	"image/color"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"onon/internal/config"
	"onon/internal/keys"

	"github.com/jupiterrider/purego-sdl3/sdl"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/image/font"
)

// uiField is a text field a view's canvas draws.
type uiField struct {
	input  textInput
	rect   sdl.FRect // where it was last drawn, in the window
	textX  float32   // where its text starts, scrolled
	scroll int       // how far its text is scrolled left, keeping the cursor in it
	face   font.Face
	secret bool
}

// display is the text the field shows: its own, or dots for a secret.
func (f *uiField) display() string {
	if f.secret {
		return strings.Repeat("•", utf8.RuneCountInString(f.input.Value))
	}
	return f.input.Value
}

func (view *uiView) field(id string) *uiField {
	if view.fields == nil {
		view.fields = map[string]*uiField{}
	}
	f := view.fields[id]
	if f == nil {
		f = &uiField{}
		view.fields[id] = f
	}
	return f
}

// keyView is the view the keys go to: the menu open, or the panel
// focused.
func (a *App) keyView() *uiView {
	if view := a.activeModalUIView(); view != nil {
		return view
	}
	if view := a.views.focused; view != nil && view.visible {
		return view
	}
	return nil
}

// focusUIView gives a panel the keys, or with focus false takes them back
// for the document. A menu has the keys while it is open.
func (a *App) focusUIView(view *uiView, focus bool) {
	switch {
	case focus && !view.modal && view.visible:
		a.views.focused = view
	case !focus && a.views.focused == view:
		a.views.focused = nil
	}
	a.syncTextInput()
	a.pendingRedraw = true
}

// uiEdit is the text typing edits in a view: a field's, by id, or the
// prompt's, whose id is empty.
type uiEdit struct {
	view  *uiView
	id    string
	input *textInput
}

// uiEditTarget is the text typing edits, if a view with the keys has a
// field focused or a prompt.
func (a *App) uiEditTarget() (uiEdit, bool) {
	if a.mode != modeNormal {
		return uiEdit{}, false
	}
	view := a.keyView()
	if view == nil {
		return uiEdit{}, false
	}
	if f := view.fields[view.focusField]; f != nil {
		return uiEdit{view: view, id: view.focusField, input: &f.input}, true
	}
	if view.prompt != "" {
		return uiEdit{view: view, input: &view.promptInput}, true
	}
	return uiEdit{}, false
}

// editingInput is the text being typed into: a view's, or a prompt's in
// the status bar.
func (a *App) editingInput() *textInput {
	if target, ok := a.uiEditTarget(); ok {
		return target.input
	}
	if a.mode != modeNormal {
		return &a.input
	}
	return nil
}

// editUIText makes an edit to a view's text, telling the view if it
// changed: a prompt's text is the view's query.
func (a *App) editUIText(target uiEdit, edit func(*textInput)) {
	before := target.input.Value
	edit(target.input)
	a.pendingRedraw = true
	if target.input.Value == before {
		return
	}
	if target.id == "" {
		a.setUIViewQuery(target.view, target.input.Value)
	} else if target.view.onInput != nil {
		target.view.onInput(a, target.id, target.input.Value)
	}
}

// typeUIText types text into a view's field or prompt, reporting whether
// one took it.
func (a *App) typeUIText(text string) bool {
	target, ok := a.uiEditTarget()
	if !ok {
		return false
	}
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, text)
	a.editUIText(target, func(t *textInput) { t.InsertText(text) })
	return true
}

// handleUIEditKey handles a key pressed in view while it has a field or
// prompt to type in: Tab and Shift-Tab move between its fields, Enter
// submits, and the editing keys edit. Keys that type text are taken, the
// text coming as it is typed. It reports whether it took the key.
func (a *App) handleUIEditKey(view *uiView, key keys.Key) bool {
	target, ok := a.uiEditTarget()
	if !ok || target.view != view {
		return false
	}
	name := key.String()
	switch name {
	case "<Tab>", "<S-Tab>":
		if n := len(view.fieldOrder); n > 0 {
			step := 1
			if name == "<S-Tab>" {
				step = n - 1
			}
			i := max(0, slices.Index(view.fieldOrder, view.focusField))
			view.focusField = view.fieldOrder[(i+step)%n]
			view.fields[view.focusField].input.SelectAll()
			a.pendingRedraw = true
			return true
		}
	case "<CR>":
		if view.onSubmit != nil {
			view.onSubmit(a, target.id, target.input.Value)
			return true
		}
		return false
	}
	if edit, ok := textEditFor(runtime.GOOS, name); ok {
		edit(a)
		return true
	}
	if _, ok := key.Text(); ok {
		return true
	}
	// Option on macOS and AltGr elsewhere type characters of their own.
	return key.Mod&(keys.Ctrl|keys.Cmd) == 0 && key.Mod&keys.Alt != 0 && utf8.RuneCountInString(key.Name) == 1
}

// pressUIField focuses the field of view at x, y, putting the cursor
// where it was pressed, and reports whether there was one.
func (a *App) pressUIField(view *uiView, x, y float32) bool {
	for _, id := range view.fieldOrder {
		f := view.fields[id]
		r := f.rect
		if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			continue
		}
		view.focusField = id
		if f.face != nil {
			f.input.Set(f.input.Value)
			f.input.Cursor = runeIndexAt(f.face, f.display(), float64(x-f.textX))
			f.input.Anchor = f.input.Cursor
		}
		if !view.modal {
			a.focusUIView(view, true)
		}
		a.pendingRedraw = true
		return true
	}
	return false
}

// textBoxScroll keeps the cursor at cursorX within width, scrolled by
// scroll, returning the new scroll.
func textBoxScroll(scroll, cursorX, textW, width int) int {
	switch {
	case cursorX-scroll > width-1:
		scroll = cursorX - width + 1
	case cursorX < scroll:
		scroll = cursorX
	}
	return clampInt(scroll, 0, max(0, textW-width+1))
}

// drawEditableText draws text being edited in face along a line from x,
// at most width wide, with input's selection and, if focused, its
// cursor, scrolled to keep the cursor in sight, returning where the text
// starts. With no text, placeholder is drawn instead, in placeholderColor.
func (a *App) drawEditableText(renderer *sdl.Renderer, face font.Face, input *textInput, display string, x, top, width int, scroll *int, focused bool, textColor, placeholderColor color.RGBA, placeholder string) float32 {
	m := face.Metrics()
	ascent, height := m.Ascent.Ceil(), faceLineHeight(face)
	left, _ := splitAtRune(display, input.Cursor)
	cursorX := measureText(face, left)
	*scroll = textBoxScroll(*scroll, cursorX, measureText(face, display), width)
	textX := x - *scroll
	clip := sdl.FRect{X: float32(x), Y: float32(top), W: float32(width), H: float32(height)}
	a.withClip(renderer, clip, func() error {
		if start, end, ok := input.SelectionRange(); ok && focused {
			before, rest := splitAtRune(display, start)
			selected, _ := splitAtRune(rest, end-start)
			st := a.style(config.ElementInputSelection)
			a.drawBox(renderer, &st, sdl.FRect{X: float32(textX + measureText(face, before)), Y: float32(top), W: float32(max(1, measureText(face, selected))), H: float32(height)})
		}
		if display == "" && placeholder != "" {
			a.drawTextWithFace(renderer, placeholder, textX, top+ascent, placeholderColor, face, false)
		} else {
			a.drawTextWithFace(renderer, display, textX, top+ascent, textColor, face, false)
		}
		if focused {
			st := a.style(config.ElementCursor)
			a.drawBox(renderer, &st, sdl.FRect{X: float32(textX + cursorX), Y: float32(top), W: a.lineWidth(st.Width.V), H: float32(height)})
		}
		return nil
	})
	return float32(textX)
}

func (c *elementCanvas) Input(id string, x, y, w float64, o config.InputOptions) float64 {
	view := c.view
	if view == nil {
		return 0
	}
	a := c.app
	f := view.field(id)
	f.secret = o.Secret
	view.fieldOrder = append(view.fieldOrder, id)
	if view.fields[view.focusField] == nil {
		view.focusField = id
	}
	focused := view.focusField == id && a.keyView() == view
	element := config.ElementInput
	if focused {
		element = config.ElementInputFocused
	}
	st := a.style(element)
	f.face = a.styleFace(&st)
	if f.face == nil {
		return 0
	}
	padTop, padRight, padBottom, padLeft := a.insets(st.Padding.V)
	scale := float64(a.px(1))
	h := (float64(faceLineHeight(f.face)) + float64(padTop+padBottom)) / scale
	rect := c.rect(x, y, w, h)
	f.rect = rect
	view.regions = append(view.regions, uiRegion{id: lua.LString(id), rect: rect})
	if c.renderer == nil {
		return h
	}
	a.drawBox(c.renderer, &st, rect)
	left := int(math.Round(float64(rect.X + padLeft)))
	width := int(rect.X+rect.W-padRight) - left
	f.textX = a.drawEditableText(c.renderer, f.face, &f.input, f.display(), left, int(math.Round(float64(rect.Y+padTop))), width, &f.scroll, focused,
		a.textColor(&st, false), a.textColor(&st, true), o.Placeholder)
	c.restoreClip()
	return h
}

// restoreClip puts back the canvas's own clip, which drawing that clips
// itself leaves.
func (c *elementCanvas) restoreClip() {
	if c.clipped && c.renderer != nil {
		sdl.SetRenderClipRect(c.renderer, &c.clip)
	}
}

// drawPromptHeader draws a view's header as its prompt: the label, then
// the text typed, with the count of rows matching after it.
func (a *App) drawPromptHeader(renderer *sdl.Renderer, view *uiView, rect sdl.FRect, shown, total int) {
	st := a.style(config.ElementHeader)
	height := a.modalListHeaderHeight()
	top := rect.Y + float32(a.modalListHeadHeight()-height)
	a.drawBox(renderer, &st, sdl.FRect{X: rect.X, Y: top, W: rect.W, H: float32(height)})
	_, padRight, _, padLeft := a.insets(st.Padding.V)
	baseline := int(top) + a.modalListBaselineOffset(height)
	x, end := int(rect.X+padLeft), int(rect.X+rect.W-padRight)
	face := a.styleFace(&st)
	label := a.style(config.ElementPrompt)
	a.drawTextFace(renderer, view.prompt, x, baseline, a.textColor(&label, false), st.Bold.V)
	x += measureText(face, view.prompt) + a.ipx(st.Gap.V)
	if view.query != "" && !view.noFilter {
		count := formatCount(shown, total)
		w := measureText(a.fontFace, count)
		a.drawText(renderer, count, end-w, baseline, a.textColor(&st, true))
		end -= w + a.ipx(st.Gap.V)
	}
	textTop := baseline - a.fontFace.Metrics().Ascent.Ceil()
	a.drawEditableText(renderer, a.fontFace, &view.promptInput, view.promptInput.Value, x, textTop, max(1, end-x), &view.promptScroll, a.keyView() == view,
		a.textColor(&st, false), a.textColor(&st, true), "")
}

func formatCount(shown, total int) string {
	return strconv.Itoa(shown) + "/" + strconv.Itoa(total)
}

func (a *App) UIField(id, field string) string {
	if view := a.views.views[id]; view != nil {
		if f := view.fields[field]; f != nil {
			return f.input.Value
		}
	}
	return ""
}

func (a *App) SetUIField(id, field, text string) {
	view := a.views.views[id]
	if view == nil {
		return
	}
	view.field(field).input.Set(text)
	a.pendingRedraw = true
}

func (a *App) FocusUI(id string, focus bool) {
	if view := a.views.views[id]; view != nil {
		a.focusUIView(view, focus)
	}
}

func (a *App) UIFocused(id string) bool {
	view := a.views.views[id]
	return view != nil && a.keyView() == view
}
