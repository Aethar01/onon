//go:build !darwin && !windows

package platform

import (
	"image/color"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func ConfigureWindow(window *sdl.Window, hitTest func(x, y float64) sdl.HitTestResult) bool {
	return false
}

func SetWindowDocument(window *sdl.Window, path string, edited bool) {}

func SetWindowColors(window *sdl.Window, background, border color.RGBA, dark bool) {}

const WindowTitleBand = 0
const WindowButtonsLeading = 0
