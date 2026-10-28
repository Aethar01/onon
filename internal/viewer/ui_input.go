package viewer

import (
	"image/color"
	"math"
	"runtime"
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
	rect   sdl.FRect  // where it was last drawn, in the window
	layout textLayout // its text as last drawn
	scroll [2]int     // how far it is scrolled: left in pixels, down in lines
	lines  int        // how many lines it shows; more than one wraps and takes newlines
	secret bool
}

// display is the text the field shows: its own, or dots for a secret.
func (f *uiField) display() string {
	if f.secret {
		return maskText(f.input.Value)
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
	if f := view.fields[view.focus]; f != nil {
		return uiEdit{view: view, id: view.focus, input: &f.input}, true
	}
	if view.promptFocused() {
		return uiEdit{view: view, input: &view.promptInput}, true
	}
	return uiEdit{}, false
}

// promptFocused reports whether view's prompt has its focus.
func (view *uiView) promptFocused() bool {
	return view.prompt != "" && (view.focus == promptFocus || view.focus == "")
}

// promptFocus is the focus of a view's prompt, which no field, button or
// check box a plugin names can be.
const promptFocus = "\x00prompt"

// tabOrder is what Tab moves through in view: its prompt, then what its
// body drew that the keys can reach.
func (view *uiView) tabOrder() []string {
	if view.prompt == "" {
		return view.focusOrder
	}
	return append([]string{promptFocus}, view.focusOrder...)
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

// editingSecret reports whether the text being typed into is hidden, as
// a password is.
func (a *App) editingSecret() bool {
	if target, ok := a.uiEditTarget(); ok {
		f := target.view.fields[target.id]
		return f != nil && f.secret
	}
	return a.mode != modeNormal && a.inputMasked()
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
// prompt to type in: Enter submits, and the editing keys edit. In a field of more than one line,
// Enter starts a new line, Ctrl- or Cmd-Enter submits, and the arrows,
// Home and End move by line. Keys that type text are taken, the text
// coming as it is typed. It reports whether it took the key.
func (a *App) handleUIEditKey(view *uiView, key keys.Key, name string) bool {
	if view.prompt == "" && len(view.fields) == 0 {
		return false // nothing to type in
	}
	target, ok := a.uiEditTarget()
	if !ok || target.view != view {
		return false
	}
	field := view.fields[target.id]
	multiline := field != nil && field.lines > 1
	switch name {
	case "<CR>", "<C-CR>", "<D-CR>":
		if multiline && name == "<CR>" {
			a.editUIText(target, func(t *textInput) { t.InsertText("\n") })
			return true
		}
		if view.onSubmit != nil {
			view.onSubmit(a, target.id, target.input.Value)
			return true
		}
		return false
	}
	if multiline {
		l := &field.layout
		move := map[string]func(int) int{
			"Up":   func(i int) int { return l.moveLines(i, -1) },
			"Down": func(i int) int { return l.moveLines(i, 1) },
			"Home": func(i int) int { return l.lines[l.line(i)].start },
			"End":  func(i int) int { return l.lines[l.line(i)].end },
		}
		if to, ok := move[key.Name]; ok && key.Mod&^keys.Shift == 0 && len(l.lines) > 0 {
			extend := key.Mod&keys.Shift != 0
			a.editUIText(target, func(t *textInput) { t.SetCursor(to(t.Cursor), extend) })
			return true
		}
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

// dragUIField stretches the selection of the field or prompt of view being
// selected with the pointer to x, y, done once the button is let go, and
// reports whether one was.
func (a *App) dragUIField(view *uiView, x, y float32, done bool) bool {
	drag := func(l *textLayout, input *textInput) bool {
		if !input.mouseSelecting || len(l.lines) == 0 {
			return false
		}
		pressText(l, input, x, y, true)
		input.mouseSelecting = !done
		a.pendingRedraw = true
		return true
	}
	for _, f := range view.fields {
		if drag(&f.layout, &f.input) {
			return true
		}
	}
	return drag(&view.promptLayout, &view.promptInput)
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

// editBox is a box of text being typed into: the text, as shown, in a face,
// lines lines high and width wide from x, y, scrolled to keep the cursor
// in sight; a box of one line scrolls across, and one of more wraps and
// scrolls down. With no text, the placeholder shows.
type editBox struct {
	input                *textInput
	display, placeholder string
	face                 font.Face
	x, y, width, lines   int
	scroll               *[2]int
	focused              bool
	textColor, phColor   color.RGBA
	compositionColor     config.Color
	cursor               tweenKey
}

// drawEditBox draws b, with its selection, the input method's composition
// and, if focused, its cursor, returning its text as laid out.
func (a *App) drawEditBox(renderer *sdl.Renderer, b editBox) textLayout {
	multiline := b.lines > 1
	display, composeStart, composeLen := a.compose(b.input, b.display)
	l := textLayout{face: b.face, lines: layoutLines(b.face, display, b.width, multiline), lineHeight: faceLineHeight(b.face)}
	cursorX, _ := l.at(b.input.Cursor)
	if multiline {
		line := l.line(b.input.Cursor)
		b.scroll[1] = clampInt(b.scroll[1], line-b.lines+1, line)
		b.scroll[0] = 0
	} else {
		b.scroll[0] = textBoxScroll(b.scroll[0], int(cursorX), measureText(b.face, display), b.width)
	}
	l.x, l.y = float32(b.x-b.scroll[0]), float32(b.y-b.scroll[1]*l.lineHeight)
	if renderer == nil {
		return l
	}
	clip := sdl.FRect{X: float32(b.x), Y: float32(b.y), W: float32(b.width), H: float32(b.lines * l.lineHeight)}
	ascent := b.face.Metrics().Ascent.Ceil()
	a.withClip(renderer, clip, func() error {
		if b.focused {
			a.drawTextSelection(renderer, &l, b.input)
		}
		if display == "" && b.placeholder != "" {
			a.drawTextWithFace(renderer, b.placeholder, int(l.x), int(l.y)+ascent, b.phColor, b.face, false)
		}
		for i, ln := range l.lines {
			a.drawTextWithFace(renderer, ln.text, int(l.x), int(l.y)+i*l.lineHeight+ascent, b.textColor, b.face, false)
		}
		a.drawComposition(renderer, &l, composeStart, composeLen, b.compositionColor)
		if b.focused {
			a.drawTextCursor(renderer, &l, b.input, b.cursor)
		}
		return nil
	})
	return l
}

func (c *elementCanvas) Input(id string, x, y, w float64, o config.InputOptions) float64 {
	view := c.view
	if view == nil {
		return 0
	}
	a := c.app
	f := view.field(id)
	f.secret, f.lines = o.Secret, max(1, o.Lines)
	view.focusOrder = append(view.focusOrder, id)
	if view.focus == "" {
		view.focus = id // the first field has the typing
	}
	focused := view.focus == id && a.keyView() == view
	element := config.ElementInput
	if focused {
		element = config.ElementInputFocused
	}
	st := a.style(element)
	face := a.styleFace(&st)
	if face == nil {
		return 0
	}
	padTop, padRight, padBottom, padLeft := a.insets(st.Padding.V)
	scale := float64(a.px(1))
	h := (float64(f.lines*faceLineHeight(face)) + float64(padTop+padBottom)) / scale
	rect := c.rect(x, y, w, h)
	f.rect = rect
	view.regions = append(view.regions, uiRegion{id: lua.LString(id), rect: rect})
	if c.renderer != nil {
		a.drawBox(c.renderer, &st, rect)
	}
	left := int(math.Round(float64(rect.X + padLeft)))
	f.layout = a.drawEditBox(c.renderer, editBox{
		input: &f.input, display: f.display(), placeholder: o.Placeholder, face: face,
		x: left, y: int(math.Round(float64(rect.Y + padTop))), width: int(rect.X+rect.W-padRight) - left, lines: f.lines,
		scroll: &f.scroll, focused: focused, textColor: a.textColor(&st, false), phColor: a.textColor(&st, true),
		compositionColor: st.Text.V, cursor: tweenKey{kind: "cursor", view: view, text: id + "\x00" + f.input.Value},
	})
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
	view.promptRect = sdl.FRect{X: float32(x), Y: top, W: float32(max(1, end-x)), H: float32(height)}
	view.promptLayout = a.drawEditBox(renderer, editBox{
		input: &view.promptInput, display: view.promptInput.Value, face: a.fontFace,
		x: x, y: textTop, width: max(1, end-x), lines: 1, scroll: &view.promptScroll,
		focused:   a.keyView() == view && view.promptFocused(),
		textColor: a.textColor(&st, false), phColor: a.textColor(&st, true), compositionColor: st.Text.V,
		cursor: tweenKey{kind: "cursor", view: view, text: view.promptInput.Value},
	})
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
