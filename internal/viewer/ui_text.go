package viewer

import (
	"math"
	"strings"
	"unicode/utf8"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

// Text being edited, in the status bar's prompt, a view's prompt or a
// text field, is laid out, drawn with its selection and cursor, composed
// with an input method, and found under the pointer the same way.

// maskText is text hidden as a password is.
func maskText(s string) string {
	return strings.Repeat("•", utf8.RuneCountInString(s))
}

// textLine is a line of text as laid out, holding runes start to end of
// the whole.
type textLine struct {
	text       string
	start, end int
}

// textLayout is text being edited as drawn: its face and lines, where the
// first line's text starts, scrolled, and how far apart lines are.
type textLayout struct {
	face       font.Face
	lines      []textLine
	x, y       float32
	lineHeight int
}

// layoutLines lays s out: on one line, or broken at newlines and between
// words to at most width pixels in face.
func layoutLines(face font.Face, s string, width int, multiline bool) []textLine {
	if !multiline {
		return []textLine{{text: s, end: utf8.RuneCountInString(s)}}
	}
	var lines []textLine
	offset := 0
	for para := range strings.SplitSeq(s, "\n") {
		runes := []rune(para)
		for _, seg := range wrapSegments(face, runes, width) {
			lines = append(lines, textLine{text: string(runes[seg[0]:seg[1]]), start: offset + seg[0], end: offset + seg[1]})
		}
		offset += len(runes) + 1 // past the newline
	}
	return lines
}

// wrapSegments breaks runes into lines at most width pixels wide, after
// the spaces following a word, a word too long for a line broken where it
// reaches the edge. Every rune is in a segment, so they run on unbroken.
func wrapSegments(face font.Face, runes []rune, width int) [][2]int {
	var segs [][2]int
	start, fits := 0, 0 // the line's start, and the end of what fits on it
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && runes[j] != ' ' {
			j++
		}
		k := j
		for k < len(runes) && runes[k] == ' ' {
			k++
		}
		switch {
		case measureText(face, string(runes[start:j])) <= width:
			fits, i = k, k
		case fits > start:
			segs = append(segs, [2]int{start, fits})
			start = fits
		default: // a word too long for a line of its own
			cut := start + max(1, len(fittingRunes(face, runes[start:j], width)))
			segs = append(segs, [2]int{start, cut})
			start, fits, i = cut, cut, cut
		}
	}
	return append(segs, [2]int{start, len(runes)})
}

// fittingRunes is the longest prefix of runes that fits in width pixels.
func fittingRunes(face font.Face, runes []rune, width int) []rune {
	n := 0
	for n < len(runes) && measureText(face, string(runes[:n+1])) <= width {
		n++
	}
	return runes[:n]
}

// line is the index of the line the rune index falls in: at a break, the
// line it starts.
func (l *textLayout) line(index int) int {
	line := 0
	for i, ln := range l.lines {
		if ln.start <= index {
			line = i
		}
	}
	return line
}

// at is where the rune index is drawn: the left of its line's box at it.
func (l *textLayout) at(index int) (x, y float32) {
	i := l.line(index)
	ln := l.lines[i]
	before, _ := splitAtRune(ln.text, index-ln.start)
	return l.x + float32(measureText(l.face, before)), l.y + float32(i*l.lineHeight)
}

// runeAt is the rune boundary nearest x, y.
func (l *textLayout) runeAt(x, y float32) int {
	i := clampInt(int(math.Floor(float64(y-l.y)/float64(l.lineHeight))), 0, len(l.lines)-1)
	ln := l.lines[i]
	return ln.start + runeIndexAt(l.face, ln.text, float64(x-l.x))
}

// moveLines is the rune index lines lines up or down from index, as near
// across as it is.
func (l *textLayout) moveLines(index, lines int) int {
	x, _ := l.at(index)
	i := l.line(index) + lines
	if i < 0 {
		return 0
	}
	if i >= len(l.lines) {
		return l.lines[len(l.lines)-1].end
	}
	ln := l.lines[i]
	return min(ln.start+runeIndexAt(l.face, ln.text, float64(x-l.x)), ln.end)
}

// drawTextSelection draws input's selection under the text.
func (a *App) drawTextSelection(renderer *sdl.Renderer, l *textLayout, input *textInput) {
	start, end, ok := input.SelectionRange()
	if !ok {
		return
	}
	st := a.style(config.ElementInputSelection)
	for i, ln := range l.lines {
		s, e := max(start, ln.start), min(end, ln.end)
		if s > e || s == e && !(s == ln.end && end > ln.end) {
			continue
		}
		x0, _ := l.at(s)
		x1, _ := l.at(e)
		if e == ln.end && end > ln.end {
			x1 += float32(measureText(l.face, " ")) // the newline or space it runs on past
		}
		y := l.y + float32(i*l.lineHeight)
		a.drawBox(renderer, &st, sdl.FRect{X: x0, Y: y, W: max(1, x1-x0), H: float32(l.lineHeight)})
	}
}

// drawTextCursor draws input's cursor over the text. It glides when moved
// through the text, and snaps to where typing or deleting puts it, which
// a glide would only lag behind; key, which holds the text, says which.
func (a *App) drawTextCursor(renderer *sdl.Renderer, l *textLayout, input *textInput, key tweenKey) {
	x, y := l.at(input.Cursor)
	x = float32(a.animate(key, float64(x), a.config.Theme.Motion.Cursor))
	st := a.style(config.ElementCursor)
	a.drawBox(renderer, &st, sdl.FRect{X: float32(math.Round(float64(x))), Y: y, W: a.lineWidth(st.Width.V), H: float32(l.lineHeight)})
	a.placeTextInputArea(x, y, l.lineHeight)
}

// compose is the text input shows, display, with the input method's
// composition at its cursor while it is the text typed into, and the
// runes the composition takes.
func (a *App) compose(input *textInput, display string) (string, int, int) {
	if a.composition == "" || a.editingInput() != input {
		return display, 0, 0
	}
	left, right := splitAtRune(display, input.Cursor)
	return left + a.composition + right, input.Cursor, utf8.RuneCountInString(a.composition)
}

// drawComposition underlines the n runes from start the input method is
// composing.
func (a *App) drawComposition(renderer *sdl.Renderer, l *textLayout, start, n int, clr config.Color) {
	if n == 0 {
		return
	}
	x0, y := l.at(start)
	x1, _ := l.at(start + n)
	st := a.style(config.ElementCursor)
	st.Fill.V = clr
	a.drawBox(renderer, &st, sdl.FRect{X: x0, Y: y + float32(l.lineHeight) - a.hairline(), W: x1 - x0, H: a.hairline()})
}

// placeTextInputArea tells the input method where the cursor is, so its
// candidates show beside it. onon draws in the renderer's pixels, which
// the window's coordinates the input method takes are fewer of on a
// screen of high density.
func (a *App) placeTextInputArea(x, y float32, height int) {
	if a.window == nil {
		return
	}
	area := textInputArea(x, y, height, sdl.GetWindowPixelDensity(a.window))
	if area != a.textInputArea {
		a.textInputArea = area
		sdl.SetTextInputArea(a.window, &area, 0)
	}
}

// handleSDLTextEditing notes the text the input method is composing.
func (a *App) handleSDLTextEditing(e *sdl.TextEditingEvent) {
	a.composition = e.Text()
	a.pendingRedraw = true
}

// pressText puts input's cursor at x, y in its layout, the selection
// stretching to it with extend, as a press or drag of the pointer does.
func pressText(l *textLayout, input *textInput, x, y float32, extend bool) {
	input.SetCursor(l.runeAt(x, y), extend)
}

// textInputArea is the cursor at x, y, height high, in render pixels, as
// the window's coordinates have it at density render pixels to one.
func textInputArea(x, y float32, height int, density float32) sdl.Rect {
	if density <= 0 {
		density = 1
	}
	return sdl.Rect{X: int32(x / density), Y: int32(y / density), W: 1, H: int32(math.Ceil(float64(float32(height) / density)))}
}
