package viewer

import (
	"math"
	"unicode/utf8"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const modalScrollbarWidth = 8

// modalListRowHeight is the height of a menu row: a line of text and the
// row's padding above and below it.
func (a *App) modalListRowHeight() int {
	top, _, bottom, _ := a.insets(a.style(config.ElementRow).Padding.V)
	return a.uiLineHeight() + int(math.Round(float64(top+bottom)))
}

// modalListHeaderHeight is the height of a menu's header: a line of text
// and the header's padding above and below it.
func (a *App) modalListHeaderHeight() int {
	top, _, bottom, _ := a.insets(a.style(config.ElementHeader).Padding.V)
	return a.uiLineHeight() + int(math.Round(float64(top+bottom)))
}

// modalListHeadHeight is how far a menu's rows start below its top: the
// panel's padding above the header, and the header.
func (a *App) modalListHeadHeight() int {
	top, _, _, _ := a.insets(a.style(config.ElementPanel).Padding.V)
	return int(math.Round(float64(top))) + a.modalListHeaderHeight()
}

func (a *App) modalListBaselineOffset(rowHeight int) int {
	return (rowHeight + a.fontFace.Metrics().Ascent.Ceil() - a.fontFace.Metrics().Descent.Ceil()) / 2
}

// drawPanel draws a panel styled as element over rect, with drawContent
// clipped to it, and its border over that.
func (a *App) drawPanel(renderer *sdl.Renderer, element config.Element, rect sdl.FRect, drawContent func() error) error {
	st := a.style(element)
	border := a.drawBoxParts(renderer, &st, rect, boxBody)
	if err := a.withClip(renderer, rect, drawContent); err != nil {
		return err
	}
	if border {
		a.drawBoxParts(renderer, &st, rect, boxBorder)
	}
	return nil
}

// drawModalListHeader draws a panel's title at its top, below the panel's
// padding, with detail such as a count after it in the secondary colour.
func (a *App) drawModalListHeader(renderer *sdl.Renderer, rect sdl.FRect, title, detail string) error {
	st := a.style(config.ElementHeader)
	height := a.modalListHeaderHeight()
	top := rect.Y + float32(a.modalListHeadHeight()-height)
	a.drawBox(renderer, &st, sdl.FRect{X: rect.X, Y: top, W: rect.W, H: float32(height)})
	_, padRight, _, padLeft := a.insets(st.Padding.V)
	baseline := int(top) + a.modalListBaselineOffset(height)
	x := int(rect.X + padLeft)
	end := int(rect.X + rect.W - padRight)
	face := a.styleFace(&st)
	title = truncateText(face, title, end-x)
	if err := a.drawTextFace(renderer, title, x, baseline, a.textColor(&st, false), st.Bold.V); err != nil {
		return err
	}
	if detail != "" {
		x += measureText(face, title) + a.ipx(st.Gap.V)
		if err := a.drawText(renderer, a.truncateModalListText(detail, end-x), x, baseline, a.textColor(&st, true)); err != nil {
			return err
		}
	}
	return nil
}

// styleFace is the face st's text is drawn in.
func (a *App) styleFace(st *config.Style) font.Face {
	if st.Bold.V {
		return a.headingFont()
	}
	return a.fontFace
}

// modalListRowAt is the row of a list at x, y, its rows starting head
// pixels below the top of rect.
func (a *App) modalListRowAt(rect sdl.FRect, rows, head, rowHeight, x, y int) (int, bool) {
	if float32(x) < rect.X || float32(x) > rect.X+rect.W || float32(y) < rect.Y || float32(y) > rect.Y+rect.H {
		return 0, false
	}
	if float32(y) < rect.Y+float32(head) {
		return 0, false
	}
	row := (y - int(rect.Y) - head) / rowHeight
	if row < 0 || row >= rows {
		return 0, false
	}
	return row, true
}

// listScrollbarRects is the scrollbar's track and thumb for a list in
// area showing rows of total rows from offset, if it needs one.
func listScrollbarRects(area sdl.FRect, rows, total int, offset float64) (sdl.FRect, sdl.FRect, bool) {
	if rows <= 0 || total <= rows || area.H < 20 {
		return sdl.FRect{}, sdl.FRect{}, false
	}
	track := sdl.FRect{X: area.X + area.W - modalScrollbarWidth - 6, Y: area.Y, W: modalScrollbarWidth, H: area.H}
	thumbH := max(20, track.H*float32(rows)/float32(total))
	maxScroll := max(0, total-rows)
	thumbTravel := track.H - thumbH
	thumbY := track.Y
	if maxScroll > 0 && thumbTravel > 0 {
		thumbY += thumbTravel * float32(clampFloat(offset, 0, float64(maxScroll))) / float32(maxScroll)
	}
	return track, sdl.FRect{X: track.X, Y: thumbY, W: track.W, H: thumbH}, true
}

// modalListScrollbarScrollForY is the offset that puts the thumb at y,
// grabbed dragOffset pixels from its top; it follows the pointer exactly,
// so it may fall part-way into a row.
func modalListScrollbarScrollForY(track, thumb sdl.FRect, rows, total, y, dragOffset int) float64 {
	maxScroll := max(0, total-rows)
	if maxScroll == 0 {
		return 0
	}
	travel := track.H - thumb.H
	if travel <= 0 {
		return 0
	}
	rel := clampFloat(float64(float32(y)-track.Y-float32(dragOffset)), 0, float64(travel))
	return rel / float64(travel) * float64(maxScroll)
}

// modalListScrollForSelection returns the first row to show so the selected
// row has scrollOff rows of context above and below; like vim, a scrollOff of
// half the window or more keeps the selection centred.
func modalListScrollForSelection(scroll, selected, rows, total, scrollOff int) int {
	if rows < 1 {
		rows = 1
	}
	maxScroll := max(0, total-rows)
	scrollOff = max(0, scrollOff)
	if scrollOff*2 >= rows {
		middleRow := (rows - 1) / 2
		return clampInt(selected-middleRow, 0, maxScroll)
	}
	if selected < scroll+scrollOff {
		scroll = selected - scrollOff
	}
	if selected >= scroll+rows-scrollOff {
		scroll = selected - rows + scrollOff + 1
	}
	return clampInt(scroll, 0, maxScroll)
}

func pointInRect(x, y int, rect sdl.FRect) bool {
	return float32(x) >= rect.X && float32(x) <= rect.X+rect.W && float32(y) >= rect.Y && float32(y) <= rect.Y+rect.H
}

// drawScrollbarThumb draws thumb as a slim bar set off the right edge of
// panel; the wider track around it is what takes clicks.
func (a *App) drawScrollbarThumb(renderer *sdl.Renderer, panel, thumb sdl.FRect) {
	st := a.style(config.ElementScrollbar)
	_, margin, _, _ := a.insets(st.Margin.V)
	w := max(1, a.px(st.Width.V))
	thumb.X = panel.X + panel.W - margin - w
	thumb.W = w
	a.drawBox(renderer, &st, thumb)
}

// truncateModalListText shortens s with an ellipsis to fit maxWidth.
func (a *App) truncateModalListText(s string, maxWidth int) string {
	return truncateText(a.fontFace, s, maxWidth)
}

// truncateText shortens s with an ellipsis to fit maxWidth. Rows are
// truncated on every frame, so the cut is found in one pass over s,
// measuring each prefix with the ellipsis as the drawer would.
func truncateText(face font.Face, s string, maxWidth int) string {
	if maxWidth <= 0 || measureText(face, s) <= maxWidth {
		return s
	}
	const ellipsis = "..."
	// The longest prefix, of at least one rune, that fits with the ellipsis.
	dots, limit := font.MeasureString(face, ellipsis), fixed.I(maxWidth)
	var width fixed.Int26_6
	prev, keep := rune(-1), 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if prev >= 0 {
			width += face.Kern(prev, r)
		}
		advance, _ := face.GlyphAdvance(r)
		width += advance
		if keep > 0 && width+face.Kern(r, '.')+dots > limit {
			break
		}
		prev, i = r, i+size
		keep = i
	}
	return s[:keep] + ellipsis
}
