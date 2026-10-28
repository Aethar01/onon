package viewer

import (
	"image"
	"image/color"
	"slices"

	"onon/internal/config"
)

func (a *App) statusVisible() bool {
	return a.statusBarShown || a.promptInBar()
}

// palette is the theme's colours for the current color mode.
func (a *App) palette() *config.Palette {
	if a.altColors {
		return &a.config.Theme.Alt
	}
	return &a.config.Theme.Palette
}

func (a *App) backgroundColor() color.RGBA {
	if a.presentation != nil {
		return rgb(a.palette().Presentation)
	}
	return rgb(a.palette().Background)
}

func (a *App) pageBackgroundColor() color.RGBA { return rgb(a.palette().Page) }
func (a *App) foregroundColor() color.RGBA     { return rgb(a.palette().Foreground) }
func (a *App) mutedColor() color.RGBA          { return rgb(a.palette().Muted) }
func (a *App) statusBarColor() color.RGBA      { return rgb(a.palette().StatusBar) }

// showWindowColors gives the system's window frame the colours behind
// the pages, on systems that draw one, when they have changed.
func (a *App) showWindowColors(bg color.RGBA) {
	muted := a.mutedColor()
	border := color.RGBA{
		R: mixChannel(muted.R, bg.R, 190),
		G: mixChannel(muted.G, bg.G, 190),
		B: mixChannel(muted.B, bg.B, 190),
		A: 0xff,
	}
	colors := [2]color.RGBA{bg, border}
	if a.window == nil || a.windowColors == colors {
		return
	}
	a.windowColors = colors
	setNativeWindowColors(a.window, bg, border)
}

func rgb(c [3]uint8) color.RGBA {
	return color.RGBA{R: c[0], G: c[1], B: c[2], A: 0xff}
}

// isLight reports whether c is a light colour, one dark text reads on.
func isLight(c color.RGBA) bool {
	return int(c.R)*299+int(c.G)*587+int(c.B)*114 > 128*1000
}

// remapPageColors maps each pixel's luminance onto the fg-bg range, leaving
// pixels inside keep, in img's coordinates, as they are.
func remapPageColors(img *image.RGBA, bg, fg [3]uint8, keep []image.Rectangle) {
	var palette [256][3]uint8 // the colour each luminance maps to
	for t := range palette {
		for c := range 3 {
			palette[t][c] = mixChannel(fg[c], bg[c], uint8(t))
		}
	}
	keep = slices.SortedFunc(slices.Values(keep), func(a, b image.Rectangle) int { return a.Min.X - b.Min.X })
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := img.Pix[(y-bounds.Min.Y)*img.Stride:]
		// Remap the runs of the row between the kept rects crossing it.
		x := bounds.Min.X
		for _, r := range keep {
			if y < r.Min.Y || y >= r.Max.Y {
				continue
			}
			remapRun(row, &palette, x-bounds.Min.X, min(r.Min.X, bounds.Max.X)-bounds.Min.X)
			x = max(x, r.Max.X)
		}
		remapRun(row, &palette, x-bounds.Min.X, bounds.Dx())
	}
}

// remapRun remaps the pixels from index from to index to of row.
func remapRun(row []uint8, palette *[256][3]uint8, from, to int) {
	for i := from * 4; i < to*4; i += 4 {
		px := row[i : i+4 : i+4]
		if px[3] == 0 {
			continue
		}
		lum := uint16(px[0])*77 + uint16(px[1])*150 + uint16(px[2])*29
		c := palette[lum>>8]
		px[0], px[1], px[2] = c[0], c[1], c[2]
	}
}

func mixChannel(fg, bg, t uint8) uint8 {
	return uint8((uint16(fg)*(255-uint16(t)) + uint16(bg)*uint16(t)) / 255)
}
