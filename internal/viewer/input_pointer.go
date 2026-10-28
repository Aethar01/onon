package viewer

import (
	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func (a *App) handleInputMouseButton(e *sdl.MouseButtonEvent) bool {
	if a.mode == modeNormal || !a.statusVisible() || e.Button != uint8(sdl.ButtonLeft) {
		return false
	}
	if e.Type == sdl.EventMouseButtonDown {
		pos, ok := a.inputPositionAt(float64(e.X), float64(e.Y), false)
		if !ok {
			return false
		}
		a.editInput(func(input *textInput) {
			input.SetCursor(pos, false)
			input.mouseSelecting = true
		})
		return true
	}
	if e.Type == sdl.EventMouseButtonUp && a.input.mouseSelecting {
		pos, _ := a.inputPositionAt(float64(e.X), float64(e.Y), true)
		a.editInput(func(input *textInput) {
			input.SetCursor(pos, true)
			input.mouseSelecting = false
		})
		return true
	}
	return false
}

func (a *App) handleInputMouseMotion(e *sdl.MouseMotionEvent) bool {
	if a.mode == modeNormal || !a.input.mouseSelecting {
		return false
	}
	if uint32(e.State)&uint32(sdl.ButtonLMask) == 0 {
		a.input.mouseSelecting = false
		return false
	}
	pos, _ := a.inputPositionAt(float64(e.X), float64(e.Y), true)
	a.editInput(func(input *textInput) { input.SetCursor(pos, true) })
	return true
}

func (a *App) inputPositionAt(x, y float64, dragging bool) (int, bool) {
	area := a.statusLayout().leftArea
	if !dragging && (y < float64(area.Y) || y > float64(area.Y+area.H)) {
		return 0, false
	}
	layout := a.statusTextLayout(a.statusLayout())
	startX := float64(layout.x)
	endX := startX + float64(measureText(a.fontFace, layout.lines[0].text))
	if !dragging && (x < startX-3 || x > inputMaxFloat64(endX+5, startX+8)) {
		return 0, false
	}
	return layout.runeAt(float32(x), layout.y), true
}

// runeIndexAt returns the index of the rune boundary in s nearest x pixels
// from its start, measuring each prefix as measureText would in one pass.
func runeIndexAt(face font.Face, s string, x float64) int {
	var width fixed.Int26_6
	prev, i := rune(-1), 0
	for _, r := range s {
		left := width.Ceil()
		if prev >= 0 {
			width += face.Kern(prev, r)
		}
		advance, _ := face.GlyphAdvance(r)
		width += advance
		if x < float64(left+width.Ceil())/2 {
			return i
		}
		prev = r
		i++
	}
	return i
}

func inputMaxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
