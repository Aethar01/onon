package viewer

import (
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"onon/internal/actions"
	"onon/internal/keys"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) handleSDLKeyDown(e *sdl.KeyboardEvent) {
	if runtime.GOOS == "darwin" && e.Repeat && e.Key == a.lastKeyUpCode && time.Since(a.lastKeyUpAt) < 100*time.Millisecond {
		if key, ok := keyToken(e); ok && a.ignoreText == "" {
			a.ignoreKeyText(key)
		}
		return
	}
	// A prompt is always opened after any menu still showing, so it takes
	// the keys first.
	if view := a.activeModalUIView(); view != nil && view.onKey != nil && a.mode == modeNormal {
		view.onKey(a, e)
		return
	}
	if a.mode != modeNormal {
		if a.handleInputEditKey(e) {
			return
		}
		switch e.Key {
		case sdl.KeycodeUp, sdl.KeycodeDown:
			older := 1
			if e.Key == sdl.KeycodeDown {
				older = -1
			}
			completing := a.completion.view != nil && a.completion.view.visible
			if !completing && a.stepPromptHistory(older) {
				return
			}
		}
		if key, ok := keyToken(e); ok && a.handleInputModeBinding(key) {
			return
		}
	}
	if a.mode == modeNormal {
		if key, ok := keyToken(e); ok {
			token := key.String()
			prevMode := a.mode
			a.actionKeycode = e.Key
			defer func() { a.actionKeycode = 0 }()
			if a.handleHintToken(token) {
				return
			}
			if a.handleMarkToken(token) {
				return
			}
			if a.handleCountToken(token) {
				return
			}
			if !e.Repeat {
				a.pushToken(token)
			}
			if prevMode == modeNormal && a.mode != modeNormal {
				a.ignoreKeyText(key)
			}
		}
		return
	}
}

// ignoreKeyText drops the text a key types from the next text input event,
// for a key that opened a prompt without being typed into it.
func (a *App) ignoreKeyText(key keys.Key) {
	if text, ok := key.Text(); ok {
		a.ignoreText = text
	}
}

// handleInputModeBinding runs the binding of a key that types no text while
// a prompt is open. A character pressed with Alt alone is typed too: Option
// on macOS and AltGr elsewhere type characters of their own.
func (a *App) handleInputModeBinding(key keys.Key) bool {
	if _, ok := key.Text(); ok {
		return false
	}
	if key.Mod&(keys.Ctrl|keys.Cmd) == 0 && key.Mod&keys.Alt != 0 && utf8.RuneCountInString(key.Name) == 1 {
		return false
	}
	action, ok := a.sequenceLookup[key.String()]
	if !ok {
		return false
	}
	if a.completion.view != nil && a.completion.view.visible {
		switch action {
		case "confirm", "show_completion", "next_completion", "prev_completion", "close":
		default:
			a.closeCompletion()
		}
	}
	a.runAction(action)
	return true
}

func (a *App) handleSDLKeyUp(e *sdl.KeyboardEvent) {
	a.lastKeyUpCode = e.Key
	a.lastKeyUpAt = time.Now()
	// A held action ends when its physical key is released, even if the
	// modifiers changed while it was held.
	if a.panning && a.panKeycode != 0 && e.Key == a.panKeycode {
		a.stopPan()
	}
	if a.autoscroll != nil && a.autoscroll.keycode != 0 && e.Key == a.autoscroll.keycode {
		a.releaseAutoscroll()
	}
}

func (a *App) handleSDLTextInput(e *sdl.TextInputEvent) {
	text := e.Text()
	if text == "" {
		return
	}
	if a.ignoreText != "" {
		text, _ = strings.CutPrefix(text, a.ignoreText)
		a.ignoreText = ""
		if text == "" {
			return
		}
	}
	if view := a.activeModalUIView(); view != nil {
		if view.searching && view.searchable {
			a.setUIViewQuery(view, view.query+text)
		}
		return
	}
	if a.mode == modeNormal {
		return
	}
	for _, r := range text {
		if r >= 0x20 && r != 0x7f {
			a.editInput(func(input *textInput) { input.InsertRune(r) })
		}
	}
}

func (a *App) handleSDLMouseWheel(e *sdl.MouseWheelEvent) {
	if view := a.activeModalUIView(); view != nil {
		// Without smooth scrolling a list moves a whole row an event, as
		// the page moves a step, however far the wheel says.
		if view.onWheel == nil || !view.onWheel(a, e) {
			a.scrollUIViewByWheel(view, e)
		}
		return
	}
	if a.handlePanelMouseWheel(e) {
		return
	}
	wx, wy := normalizedWheelDeltas(e)
	if sdl.GetModState()&sdl.KeymodCtrl != 0 {
		if wy > 0 {
			a.runMouseBinding("<C-wheel_up>")
		}
		if wy < 0 {
			a.runMouseBinding("<C-wheel_down>")
		}
		return
	}
	a.runDiscreteMouseWheel(wx, wy)
}

func (a *App) handleSDLMouseButton(e *sdl.MouseButtonEvent) {
	if view := a.activeModalUIView(); view != nil {
		if a.handleListScrollButton(e) {
			return
		}
		if view.onMouseButton != nil {
			view.onMouseButton(a, e)
		}
		return
	}
	if a.handlePanelMouseButton(e) {
		return
	}
	if a.presentation != nil {
		a.clickPresentation(e)
		return
	}
	if e.Type == sdl.EventMouseButtonUp && a.panning && e.Button == a.panButton {
		a.stopPan()
		return
	}
	if event, ok := mouseButtonEvent(e.Button, e.Type); ok {
		a.mouseButton = e.Button
		handled := a.runMouseBinding(event)
		a.mouseButton = 0
		if handled {
			return
		}
	}
	if a.panning {
		return
	}
	if a.overview != nil {
		a.clickOverview(e)
		return
	}
	if e.Button == uint8(sdl.ButtonLeft) && e.Type == sdl.EventMouseButtonDown && a.clickFormField(float64(e.X), float64(e.Y)) {
		return
	}
	if e.Button == uint8(sdl.ButtonRight) && e.Type == sdl.EventMouseButtonDown && a.openAnnotationMenu(e) {
		return
	}
	if e.Button != uint8(sdl.ButtonLeft) || a.handleLinkButton(e) || !a.config.MouseTextSelect {
		return
	}
	// A press starts a new selection, replacing any previous one; the
	// finished selection stays highlighted until the next press or close.
	if e.Type == sdl.EventMouseButtonDown {
		a.selection = textSelection{}
		if page, point, ok := a.pagePointAtScreen(float64(e.X), float64(e.Y)); ok {
			a.selection = textSelection{active: true, mode: clickSelectMode(e.Clicks), anchorPage: page, anchor: point, focusPage: page, focus: point}
			if a.selection.mode != mupdf.SelectChars {
				a.refreshSelection() // a double or triple click selects without a drag
			}
		}
		a.emitSelectionChanged()
		return
	}
	if e.Type == sdl.EventMouseButtonUp && a.selection.active {
		a.selection.active = false
		a.copySelectionToClipboard()
		a.emitSelectionChanged()
	}
}

func clickSelectMode(clicks uint8) mupdf.SelectMode {
	switch {
	case clicks >= 3:
		return mupdf.SelectLines
	case clicks == 2:
		return mupdf.SelectWords
	default:
		return mupdf.SelectChars
	}
}

// handleLinkButton follows a link once the left button is pressed and
// released over it, so a drag that starts on a link does not fire it.
func (a *App) handleLinkButton(e *sdl.MouseButtonEvent) bool {
	link, over := a.linkAt(float64(e.X), float64(e.Y))
	if e.Type == sdl.EventMouseButtonDown {
		a.links.pressed = nil
		if !over {
			return false
		}
		a.links.pressed = &link
		if a.selection.active || !a.selection.empty() {
			a.clearSelection()
		}
		return true
	}
	pressed := a.links.pressed
	a.links.pressed = nil
	if pressed == nil {
		return false
	}
	if over && link == *pressed {
		a.activateLink(link)
	}
	return true
}

func (a *App) handleSDLMouseMotion(e *sdl.MouseMotionEvent) bool {
	a.pointer = sdl.FPoint{X: e.X, Y: e.Y}
	if view := a.activeModalUIView(); view != nil {
		if a.autoscroll != nil {
			a.noteAutoscrollPointer() // the frame loop does the scrolling
			return false
		}
		if a.panning && (a.panButton == 0 || uint32(e.State)&buttonMask(a.panButton) != 0) {
			old := view.offset
			a.scrollListBy(view, -float64(e.Yrel)/float64(a.modalListRowHeight()))
			return view.offset != old
		}
		if view.onMouseMotion != nil {
			return view.onMouseMotion(a, e)
		}
		return a.uiViewHover(view, int(e.X), int(e.Y))
	}
	if a.autoscroll != nil {
		a.noteAutoscrollPointer() // the frame loop does the scrolling
		return false
	}
	if a.panning && (a.panButton == 0 || uint32(e.State)&buttonMask(a.panButton) != 0) {
		oldX, oldY := a.scrollX, a.scrollY
		a.scrollBy(-float64(e.Xrel), -float64(e.Yrel))
		return a.scrollX != oldX || a.scrollY != oldY
	}
	if taken, redraw := a.handlePanelMouseMotion(e); taken {
		// a link under the panel is no longer hovered
		a.hoverLinkPreview(mupdf.Link{}, false, e.X, e.Y)
		return a.setHoveredLink(mupdf.Link{}, false) || redraw
	} else if redraw {
		a.pendingRedraw = true
	}
	a.stopPan()
	if a.overview != nil {
		return a.hoverOverview(float64(e.X), float64(e.Y))
	}

	link, overLink := a.linkAt(float64(e.X), float64(e.Y))
	if a.titleBarButtonAt(float64(e.X), float64(e.Y)) != titleButtonNone {
		link, overLink = mupdf.Link{}, false // the button is over the link
	}
	hoverChanged := a.setHoveredLink(link, overLink)
	a.hoverLinkPreview(link, overLink, e.X, e.Y)

	if !a.selection.active || uint32(e.State)&uint32(sdl.ButtonLMask) == 0 {
		return hoverChanged
	}
	page, point, ok := a.pagePointAtScreen(float64(e.X), float64(e.Y))
	if !ok || page == a.selection.focusPage && point == a.selection.focus {
		return false
	}
	a.selection.focusPage, a.selection.focus = page, point
	a.selection.stale = true
	return true
}

// setHoveredLink shows the hovered link's target in the status bar and
// restores the previous message when the pointer leaves it. It reports
// whether the message changed.
func (a *App) setHoveredLink(link mupdf.Link, over bool) bool {
	a.links.overLink = over
	a.updateCursor()
	target := ""
	if over {
		target = a.linkTarget(link)
	}
	if target == a.links.hoverMessage {
		return false
	}
	if a.links.hoverMessage == "" || a.message != a.links.hoverMessage {
		a.links.messageBeforeHover = a.message
	}
	a.links.hoverMessage = target
	if target == "" {
		a.message = a.links.messageBeforeHover
	} else {
		a.message = target
	}
	return true
}

func (a *App) linkTarget(link mupdf.Link) string {
	if link.External || link.Page < 0 {
		return link.URI
	}
	return "page " + a.pageLabel(link.Page)
}

func (a *App) stopPan() {
	a.panning = false
	a.panButton = 0
	a.panKeycode = 0
	a.updateCursor()
}

func (a *App) handleCountToken(token string) bool {
	if len(token) == 1 && token[0] >= '1' && token[0] <= '9' {
		a.pendingCount += token
		a.message = a.pendingCount
		return true
	}
	if token == "0" && a.pendingCount != "" {
		a.pendingCount += token
		a.message = a.pendingCount
		return true
	}
	if token == "g" && a.pendingCount != "" {
		a.gotoPageInput(a.pendingCount)
		a.pendingCount = ""
		return true
	}
	if a.pendingCount != "" {
		if a.runCountAction(token) {
			a.pendingCount = ""
			return true
		}
		a.pendingCount = ""
	}
	return false
}

func (a *App) runCountAction(token string) bool {
	count, err := strconv.Atoi(a.pendingCount)
	if err != nil || count <= 0 {
		return false
	}
	action, ok := a.sequenceLookup[token]
	if !ok || !a.isCountableAction(action) {
		return false
	}
	for range count {
		a.runAction(action)
	}
	return true
}

func (a *App) isCountableAction(action string) bool {
	if a.runtime != nil {
		return a.runtime.IsCountableAction(action)
	}
	return actions.IsCountable(action)
}

func (a *App) commitInputMode() {
	if a.completion.view != nil && a.completion.view.visible {
		a.acceptCompletion()
		return
	}
	currentMode := a.mode
	input := strings.TrimSpace(a.input.Value)
	// Passwords and prompt answers are taken as typed, blank included.
	verbatim := currentMode == modePassword || currentMode == modePrompt
	if verbatim {
		input = a.input.Value
	}
	a.mode = modeNormal
	a.input.Reset()
	a.closeCompletion()
	if input == "" && !verbatim {
		return
	}
	a.recordPromptHistory(currentMode, input)
	switch currentMode {
	case modeCommand:
		a.runCommand(input)
	case modeGotoPage:
		a.gotoPageInput(input)
	case modeSearch:
		a.startSearch(input, a.searchInput)
	case modePassword:
		a.submitDocumentPassword(input)
	case modePrompt:
		a.answerPrompt(input)
	}
}

func (a *App) editInput(edit func(*textInput)) {
	a.closeCompletion()
	edit(&a.input)
}
