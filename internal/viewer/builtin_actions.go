package viewer

import (
	"fmt"

	"onon/internal/filepicker"
	"onon/internal/mupdf"
)

// builtinActions runs each action in the actions registry; a test keeps
// the two in step. It is filled in init because its handlers can run
// actions themselves.
var builtinActions map[string]func(*App) error

func init() {
	builtinActions = map[string]func(*App) error{
		"next_page":    do((*App).nextPage),
		"prev_page":    do((*App).prevPage),
		"scroll_down":  do(func(a *App) { a.scrollByInput(0, a.pageStep) }),
		"scroll_up":    do(func(a *App) { a.scrollByInput(0, -a.pageStep) }),
		"scroll_left":  do(func(a *App) { a.scrollByInput(-a.pageStep, 0) }),
		"scroll_right": do(func(a *App) { a.scrollByInput(a.pageStep, 0) }),
		"pan":          do((*App).startPan),
		"autoscroll":   do((*App).startAutoscroll),
		"next_spread":  do((*App).nextSpread),
		"prev_spread":  do((*App).prevSpread),
		"first_page":   do(func(a *App) { a.alignPageToAnchor(0) }),
		"last_page":    do(func(a *App) { a.alignPageToAnchor(a.pageCount - 1) }),

		"command_mode":           do(func(a *App) { a.openInputMode(modeCommand) }),
		"goto_page_prompt":       do(func(a *App) { a.openInputMode(modeGotoPage) }),
		"search_prompt":          do(func(a *App) { a.openSearchPrompt(searchModeForward) }),
		"search_prompt_backward": do(func(a *App) { a.openSearchPrompt(searchModeBackward) }),
		"search_next":            do(func(a *App) { a.repeatSearch(true) }),
		"search_prev":            do(func(a *App) { a.repeatSearch(false) }),
		"search_matches":         do((*App).showSearchMatches),
		"clear_search":           do((*App).clearSearch),

		"toggle_dual_page": do(func(a *App) {
			a.relayoutWithViewportAnchor(func() { a.dualPage = !a.dualPage })
			a.message = boolWord(a.dualPage, "dual-page on", "dual-page off")
		}),
		"toggle_render_mode": do((*App).toggleRenderMode),
		"toggle_alt_colors": do(func(a *App) {
			a.setAltColors(!a.altColors)
			a.message = boolWord(a.altColors, "alt colors on", "alt colors off")
		}),
		"toggle_first_page_offset": do(func(a *App) {
			a.relayoutWithViewportAnchor(func() { a.firstPageOffset = !a.firstPageOffset })
			a.message = boolWord(a.firstPageOffset, "first-page offset on", "first-page offset off")
		}),
		"toggle_status_bar": do(func(a *App) {
			a.relayoutWithViewportAnchor(func() { a.statusBarShown = !a.statusBarShown })
		}),
		"toggle_fullscreen": do(func(a *App) {
			a.fullscreen = !a.fullscreen
			a.SetFullscreen(a.fullscreen)
		}),
		"toggle_trim_margins": do((*App).toggleTrimMargins),

		"outline":      do((*App).toggleOutlineMenu),
		"keybinds":     do((*App).toggleKeybindMenu),
		"help":         do((*App).toggleHelp),
		"follow_link":  do((*App).startLinkHints),
		"overview":     do((*App).toggleOverview),
		"presentation": do((*App).togglePresentation),

		"highlight_selection": do((*App).pickHighlightColor),
		"undo":                do(func(a *App) { a.undoEdit(false) }),
		"redo":                do(func(a *App) { a.undoEdit(true) }),
		"delete_annotation":   do((*App).deleteAnnotationUnderPointer),

		"zoom_in":    do(func(a *App) { a.setManualZoom(a.zoomStep()) }),
		"zoom_out":   do(func(a *App) { a.setManualZoom(1 / a.zoomStep()) }),
		"reset_zoom": do(func(a *App) { a.setManualZoomTarget(1) }),
		"fit_width":  do(func(a *App) { a.setFitMode(fitWidth) }),
		"fit_page":   do(func(a *App) { a.setFitMode(fitPage) }),
		"fit_height": do(func(a *App) { a.setFitMode(fitHeight) }),
		"rotate_cw":  do(func(a *App) { a.rotateBy(90) }),
		"rotate_ccw": do(func(a *App) { a.rotateBy(270) }),

		"confirm": do((*App).confirm),
		"close":   do((*App).closeActiveUI),
		"copy": do(func(a *App) {
			if !a.copyActiveTextInputToClipboard() {
				a.copyPersistentSelectionToClipboard()
			}
		}),
		"cut":             do(func(a *App) { a.cutActiveTextInputToClipboard() }),
		"paste":           do(func(a *App) { a.pasteIntoActiveTextInput() }),
		"show_completion": do((*App).showCompletion),
		"next_completion": do(func(a *App) { a.moveCompletion(1) }),
		"prev_completion": do(func(a *App) { a.moveCompletion(-1) }),

		"jump_forward":      do((*App).jumpForward),
		"jump_backward":     do((*App).jumpBackward),
		"open_file_picker":  (*App).openFilePicker,
		"show_recent_files": do((*App).showRecentFiles),
		"save":              do(func(a *App) { a.writeDocument("", false) }),
		"print":             do(func(a *App) { a.printDocument("") }),
		"reload_config":     do((*App).reloadConfig),
		"open_config":       do((*App).openConfig),
		"quit":              do(func(a *App) { a.quit = a.confirmDiscard("quit") }),
	}
}

// do adapts an action that cannot fail.
func do(run func(*App)) func(*App) error {
	return func(a *App) error {
		run(a)
		return nil
	}
}

func (a *App) runBuiltinAction(action string) error {
	defer a.syncTextInput()
	run, ok := builtinActions[action]
	if !ok {
		return fmt.Errorf("unknown action: %s", action)
	}
	return run(a)
}

// startPan pans with the key or mouse button that ran the action until it
// is released.
func (a *App) startPan() {
	switch {
	case a.actionKeycode != 0:
		a.panning = true
		a.panKeycode = a.actionKeycode
		a.panButton = 0
	case a.mouseButton != 0:
		a.panning = true
		a.panButton = a.mouseButton
		a.panKeycode = 0
	}
	if a.panning {
		a.stopAutoscroll()
	}
	a.updateCursor()
}

func (a *App) openInputMode(m mode) {
	a.closeAllUI()
	a.mode = m
	a.input.Reset()
}

func (a *App) openSearchPrompt(direction searchMode) {
	a.openInputMode(modeSearch)
	a.searchInput = direction
}

func (a *App) toggleRenderMode() {
	a.relayoutWithViewportAnchor(func() {
		if a.renderMode == renderSingle {
			a.renderMode = renderContinuous
		} else {
			a.renderMode = renderSingle
		}
	})
	a.message = "render mode " + a.renderMode.String()
}

func (a *App) rotateBy(degrees float64) {
	a.relayoutWithViewportAnchor(func() {
		a.rotation = normalizeRotation(a.rotation + degrees)
		a.updatePageMetricSizes()
	})
}

// confirm accepts a completion, activates the open menu or commits the
// prompt.
func (a *App) confirm() {
	if a.completion.view != nil && a.completion.view.visible {
		a.acceptCompletion()
	} else if view := a.activeModalUIView(); view != nil {
		a.activateUIView(view)
	} else if a.mode != modeNormal {
		a.commitInputMode()
	}
}

func (a *App) openFilePicker() error {
	path, err := filepicker.PickDocument(mupdf.SupportedExtensions())
	if err != nil || path == "" {
		return err
	}
	return a.Open(path)
}
