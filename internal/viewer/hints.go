package viewer

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Link hints label every link on screen; typing a label follows it.

type linkHint struct {
	label string
	page  int
	link  mupdf.Link
}

type hintState struct {
	hints []linkHint
	typed string
}

func (a *App) startLinkHints() {
	var hints []linkHint
	viewportW, viewportH := a.viewportSize()
	a.forEachDisplayedPage(0, func(page int, x, y float64) {
		links, err := a.linksForPage(page)
		if err != nil {
			a.logf("load links page=%d err=%v", page+1, err)
			return
		}
		for _, link := range links {
			minX, minY, maxX, maxY := a.quadScreenBounds(rectQuad(link.Bounds), page, x, y)
			if maxX >= 0 && maxY >= 0 && minX <= float64(viewportW) && minY <= float64(viewportH) {
				hints = append(hints, linkHint{page: page, link: link})
			}
		}
	})
	if len(hints) == 0 {
		a.message = "no links on screen"
		return
	}
	for i, label := range hintLabels(len(hints), a.config.HintChars) {
		hints[i].label = label
	}
	a.hints = &hintState{hints: hints}
	a.message = "follow link (Esc cancels)"
}

// hintLabels returns n distinct labels of equal length drawn from chars,
// as short as chars allows.
func hintLabels(n int, chars string) []string {
	var alphabet []rune
	for _, r := range chars {
		if !slices.Contains(alphabet, r) {
			alphabet = append(alphabet, r)
		}
	}
	if len(alphabet) < 2 {
		alphabet = []rune(config.Default().HintChars)
	}
	length := 1
	for capacity := len(alphabet); capacity < n; capacity *= len(alphabet) {
		length++
	}
	labels := make([]string, n)
	for i := range labels {
		label := make([]rune, length)
		for j, v := length-1, i; j >= 0; j, v = j-1, v/len(alphabet) {
			label[j] = alphabet[v%len(alphabet)]
		}
		labels[i] = string(label)
	}
	return labels
}

// handleHintToken consumes every key while hints are shown: hint characters
// narrow the hints, Backspace undoes one, and Esc cancels.
func (a *App) handleHintToken(token string) bool {
	if a.hints == nil {
		return false
	}
	switch token {
	case "<Esc>":
		a.cancelLinkHints()
		return true
	case "<BS>":
		if _, size := utf8.DecodeLastRuneInString(a.hints.typed); size > 0 {
			a.hints.typed = a.hints.typed[:len(a.hints.typed)-size]
		}
		return true
	}
	// Labels match in either case, so Shift and Caps Lock don't get in the
	// way; typed keeps the label's own spelling for drawing.
	typed := a.hints.typed + token
	prefix := ""
	for _, hint := range a.hints.hints {
		if len(hint.label) < len(typed) || !strings.EqualFold(hint.label[:len(typed)], typed) {
			continue
		}
		if len(hint.label) == len(typed) {
			a.cancelLinkHints()
			a.activateLink(hint.link)
			return true
		}
		prefix = hint.label[:len(typed)]
	}
	if prefix != "" {
		a.hints.typed = prefix
	}
	return true
}

func (a *App) cancelLinkHints() {
	a.hints = nil
	a.message = ""
}

func (a *App) drawLinkHints(renderer *sdl.Renderer) {
	if a.hints == nil {
		return
	}
	viewportW, viewportH := a.viewportSize()
	st := a.style(config.ElementHint)
	face := a.styleFace(&st)
	metrics := face.Metrics()
	ascent, descent := metrics.Ascent.Ceil(), metrics.Descent.Ceil()
	padTop, padRight, padBottom, padLeft := a.insets(st.Padding.V)
	fg, typedFG := a.textColor(&st, false), a.textColor(&st, true)
	for _, hint := range a.hints.hints {
		if !strings.HasPrefix(hint.label, a.hints.typed) {
			continue
		}
		x, y, ok := a.pageScreenOrigin(hint.page)
		if !ok {
			continue
		}
		minX, minY, _, maxY := a.quadScreenBounds(rectQuad(hint.link.Bounds), hint.page, x, y)
		w := float64(measureText(face, hint.label)) + float64(padLeft+padRight)
		box := hintBox(minX, minY, maxY, math.Round(w), math.Round(float64(ascent+descent)+float64(padTop+padBottom)), float64(viewportW), float64(viewportH), float64(a.px(1)))
		box.X, box.Y = float32(math.Round(float64(box.X))), float32(math.Round(float64(box.Y)))
		a.drawBox(renderer, &st, box)
		left, baseline := int(box.X+padLeft), int(math.Round(float64(box.Y+padTop)))+ascent
		typedW := measureText(face, a.hints.typed)
		a.drawTextFace(renderer, a.hints.typed, left, baseline, typedFG, st.Bold.V)
		a.drawTextFace(renderer, hint.label[len(a.hints.typed):], left+typedW, baseline, fg, st.Bold.V)
	}
}

// hintBox places a w by h label above the top-left corner x, minY of its link
// (or below maxY when there is no room above), nudged slightly left so it
// leaves the link text uncovered and stays 2 logical pixels inside the
// viewport. scale is output pixels per logical one.
func hintBox(x, minY, maxY, w, h, viewportW, viewportH, scale float64) sdl.FRect {
	margin := 2 * scale
	x = math.Max(margin, math.Min(x-4*scale, viewportW-w-margin))
	y := minY - h
	if y < margin && maxY+h <= viewportH-margin {
		y = maxY
	}
	y = math.Max(margin, math.Min(y, viewportH-h-margin))
	return sdl.FRect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}
}

func rectQuad(r mupdf.Rect) mupdf.Quad {
	x0, y0, x1, y1 := float64(r.X0), float64(r.Y0), float64(r.X1), float64(r.Y1)
	return mupdf.Quad{UL: mupdf.Point{X: x0, Y: y0}, UR: mupdf.Point{X: x1, Y: y0}, LL: mupdf.Point{X: x0, Y: y1}, LR: mupdf.Point{X: x1, Y: y1}}
}
