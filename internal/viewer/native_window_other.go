//go:build !darwin && !windows

package viewer

import (
	"image/color"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) configureNativeWindow(window *sdl.Window) {}

func setNativeWindowDocument(window *sdl.Window, path string, edited bool) {}

func setNativeWindowColors(window *sdl.Window, background, border color.RGBA) {}

// windowTitleBand is the height, in logical pixels, of a title bar band
// inside the window, which the system draws outside it here.
const windowTitleBand = 0

// windowButtonsLeading is the room the window's buttons take at the left
// of its title row: none, here.
const windowButtonsLeading = 0
