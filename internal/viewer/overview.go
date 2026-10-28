package viewer

import (
	"cmp"
	"math"
	"slices"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// The page overview lays pages out as a grid of thumbnails. It reuses the
// normal continuous layout with several pages per row, so rendering and
// scrolling work as usual; only navigation and the selection outline differ.

type overviewState struct {
	selected int       // the first page of the selected spread
	columns  int       // 0 picks a count from the window width
	saved    viewState // the view to return to
}

func (a *App) toggleOverview() {
	if a.overview != nil {
		a.closeOverview(a.overview.saved.page)
		return
	}
	a.closeViewMode()
	a.closeAllUI()
	a.overview = &overviewState{selected: a.spreadStart(a.page), saved: a.captureViewState()}
	a.renderMode = renderContinuous
	a.fitMode = fitWidth
	a.relayoutOverview()
}

// overviewReturnState is the view to return to: the one from before the
// overview, keeping settings changed in it that the overview does not
// override itself, such as dual-page mode.
func (a *App) overviewReturnState() viewState {
	s := a.overview.saved
	s.dualPage, s.firstPageOffset, s.rotation = a.dualPage, a.firstPageOffset, a.rotation
	s.statusBarShown, s.altColors = a.statusBarShown, a.altColors
	return s
}

// closeOverview returns to the view from before the overview, at page.
func (a *App) closeOverview(page int) {
	saved := a.overviewReturnState()
	a.overview = nil
	a.restoreViewState(saved)
	if page != saved.page {
		a.alignPageToAnchor(page)
	}
}

// overviewThumbLongSide caps the size of thumbnails rendered for the overview.
const overviewThumbLongSide = 1024

// prefetchOverviewThumbnails requests thumbnails for the pages shown and
// just beyond, rendered at the grid's scale where a page lacks one that is
// sharp enough or current. The reading view's tiles are left alone.
func (a *App) prefetchOverviewThumbnails() {
	_, viewportH := a.viewportSize()
	margin := float64(viewportH)
	type candidate struct {
		req      renderRequest
		distance float64
	}
	var visible, nearby []candidate
	a.forEachDisplayedPage(margin, func(page int, x, y float64) {
		m := a.pageMetrics[page]
		scale := math.Min(a.scale, overviewThumbLongSide/math.Max(1, math.Max(float64(m.bounds.X1-m.bounds.X0), float64(m.bounds.Y1-m.bounds.Y0))))
		version := a.tileVersion(page)
		if thumb, ok := a.cache.get(thumbnailKey(page)); ok && thumb.scale >= scale*renderUpgradeTolerance && thumb.version == version {
			return
		}
		c := candidate{req: renderRequest{key: thumbnailKey(page), scale: scale, rect: mupdf.DeviceRect(m.bounds, scale), version: version}}
		switch {
		case y+m.height*a.scale < 0:
			c.distance = -y
			nearby = append(nearby, c)
		case y > float64(viewportH):
			c.distance = y - float64(viewportH)
			nearby = append(nearby, c)
		default:
			visible = append(visible, c)
		}
	})

	wanted := map[tileKey]bool{}
	onScreen := map[tileKey]bool{}
	protected := map[tileKey]bool{}
	a.forEachDisplayedPage(0, func(page int, _, _ float64) { protected[thumbnailKey(page)] = true })
	for _, c := range visible {
		wanted[c.req.key], onScreen[c.req.key] = true, true
	}
	for _, c := range nearby {
		wanted[c.req.key] = true
	}
	a.addCanvasThumbnails(wanted, onScreen, protected)
	a.cache.protected = protected
	if a.renderWorker != nil {
		a.renderWorker.SetWanted(wanted)
		a.renderWorker.SetVisible(onScreen)
		a.renderWorker.DrainUnwanted(a.renderGeneration)
	}
	for key := range a.renderPending {
		if !wanted[key] {
			delete(a.renderPending, key)
		}
	}
	for _, c := range visible {
		a.queueRender(c.req, 0)
	}
	slices.SortFunc(nearby, func(x, y candidate) int { return cmp.Compare(x.distance, y.distance) })
	remaining := maxPendingPrefetchRenders - a.pendingBackgroundRenderCount()
	for i, c := range nearby {
		if remaining <= 0 {
			break
		}
		if a.queueRender(c.req, renderPrefetchPriority+i) {
			remaining--
		}
	}
}

// overviewColumns is how many cells a row of the grid holds. It does not
// depend on how many pages there are, so a short document keeps the
// thumbnail size a long one gets rather than stretching across the window.
func (a *App) overviewColumns() int {
	if a.overview.columns > 0 {
		return a.overview.columns
	}
	cellWidth := max(1, a.config.OverviewThumbWidth)
	if a.dualPage {
		cellWidth *= 2 // a spread takes two thumbnails' width
	}
	viewportW, _ := a.viewportSize()
	return clampInt(viewportW/(cellWidth+a.config.OverviewGap), 2, max(2, a.config.OverviewMaxColumns))
}

// overviewSlot is the unscaled width of a grid cell: the widest spread's.
func (a *App) overviewSlot() float64 {
	slot := 0.0
	for _, spread := range a.spreads() {
		slot = math.Max(slot, a.spreadWidth(spread))
	}
	return slot
}

// overviewScale fits a full row of cells to the viewport width, whether or
// not there are pages enough to fill one.
func (a *App) overviewScale(viewportW int) float64 {
	columns := a.overviewColumns()
	gap := float64(a.config.OverviewGap)
	available := float64(viewportW) - 2*gap - float64(columns-1)*gap
	return math.Max(0.05, available/math.Max(1, float64(columns)*a.overviewSlot()))
}

func (a *App) relayoutOverview() {
	a.recomputeLayout(a.viewportSize())
	a.page = a.overview.selected
	a.scrollOverviewSelectionIntoView()
}

// runOverviewAction handles actions while the overview is shown, reporting
// false for those that should run as usual. The overview scrolls by whole
// rows with its selection; pan and autoscroll scroll it freely.
func (a *App) runOverviewAction(action string) bool {
	columns := a.overviewColumns()
	switch action {
	case "scroll_down":
		a.moveOverviewSelection(columns)
	case "scroll_up":
		a.moveOverviewSelection(-columns)
	case "scroll_right", "next_spread":
		a.moveOverviewSelection(1)
	case "scroll_left", "prev_spread":
		a.moveOverviewSelection(-1)
	case "next_page":
		a.moveOverviewSelection(a.overviewVisibleRows() * columns)
	case "prev_page":
		a.moveOverviewSelection(-a.overviewVisibleRows() * columns)
	case "first_page":
		a.moveOverviewSelection(-a.pageCount)
	case "last_page":
		a.moveOverviewSelection(a.pageCount)
	case "zoom_in":
		a.overview.columns = max(1, columns-1)
		a.relayoutOverview()
	case "zoom_out":
		a.overview.columns = min(max(1, a.config.OverviewMaxColumns), columns+1)
		a.relayoutOverview()
	case "reset_zoom":
		a.overview.columns = 0
		a.relayoutOverview()
	case "confirm":
		a.closeOverview(a.overview.selected)
	case "close", "overview":
		a.closeOverview(a.overview.saved.page)
	default:
		return false
	}
	return true
}

// moveOverviewSelection moves the selection by delta spreads.
func (a *App) moveOverviewSelection(delta int) {
	spreads := a.spreads()
	if len(spreads) == 0 {
		return
	}
	i, _ := a.spreadOf(a.overview.selected)
	a.overview.selected = spreads[clampInt(i+delta, 0, len(spreads)-1)][0]
	a.page = a.overview.selected
	a.scrollOverviewSelectionIntoView()
}

// spreadOf returns the index and pages of the spread holding page.
func (a *App) spreadOf(page int) (int, []int) {
	for i, spread := range a.spreads() {
		if slices.Contains(spread, page) {
			return i, spread
		}
	}
	return 0, []int{page}
}

// spreadStart is the first page of the spread holding page.
func (a *App) spreadStart(page int) int {
	_, spread := a.spreadOf(page)
	return spread[0]
}

// overviewVisibleRows is how many whole rows of thumbnails fit on screen.
func (a *App) overviewVisibleRows() int {
	if len(a.rows) == 0 {
		return 1
	}
	_, viewportH := a.viewportSize()
	return max(1, int(float64(viewportH)/(a.rows[0].height+float64(a.config.OverviewGap))))
}

// scrollOverviewSelectionIntoView scrolls by whole rows so the selected row
// keeps scroll_off rows of context, the same way menus do.
func (a *App) scrollOverviewSelectionIntoView() {
	if a.overview.selected >= len(a.pageToRow) {
		return
	}
	first := a.rowIndexAtContentY(a.scrollY + float64(a.config.OverviewGap))
	first = modalListScrollForSelection(first, a.pageToRow[a.overview.selected], a.overviewVisibleRows(), len(a.rows), a.config.ScrollOff)
	a.scrollY = a.rows[first].y - float64(a.config.OverviewGap)
	a.clampScroll()
}

// clickOverview jumps to the page under a click.
func (a *App) clickOverview(e *sdl.MouseButtonEvent) {
	if e.Type != sdl.EventMouseButtonDown || e.Button != uint8(sdl.ButtonLeft) {
		return
	}
	if page, _, ok := a.pagePointAtScreen(float64(e.X), float64(e.Y)); ok {
		a.closeOverview(page)
	}
}

// hoverOverview selects the spread under the pointer, as menus do, without
// scrolling so the grid stays put under the mouse. It reports whether the
// selection changed.
func (a *App) hoverOverview(x, y float64) bool {
	page, _, ok := a.pagePointAtScreen(x, y)
	if !ok {
		return false
	}
	selected := a.spreadStart(page)
	if selected == a.overview.selected {
		return false
	}
	a.overview.selected = selected
	a.page = selected
	return true
}

func (a *App) drawOverviewSelection(renderer *sdl.Renderer) {
	if a.overview == nil {
		return
	}
	if a.overview.selected >= len(a.pageToRow) {
		return
	}
	// Outline the whole selected spread.
	row := a.rows[a.pageToRow[a.overview.selected]]
	_, spread := a.spreadOf(a.overview.selected)
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i, page := range row.pages {
		if slices.Contains(spread, page) {
			x, y := a.rowPageScreenOrigin(row, i)
			minX, minY = math.Min(minX, x), math.Min(minY, y)
			maxX, maxY = math.Max(maxX, x+row.pageW[i]), math.Max(maxY, y+row.pageH[i])
		}
	}
	// The outline glides to the selection as a menu's selected row does.
	// It moves among the pages, so it stays with them as the grid scrolls.
	tr := a.config.Theme.Motion.Selection
	glide := func(kind string, v, scroll float64) float64 {
		return a.animate(tweenKey{kind: kind}, v+scroll, tr) - scroll
	}
	minX, maxX = glide("overview selection x0", minX, a.scrollX), glide("overview selection x1", maxX, a.scrollX)
	minY, maxY = glide("overview selection y0", minY, a.scrollY), glide("overview selection y1", maxY, a.scrollY)
	// The padding is between the page and the inside of the border.
	st := a.style(config.ElementOverviewSelection)
	top, right, bottom, left := a.insets(st.Padding.V)
	border := a.lineWidth(st.BorderWidth.V)
	rect := sdl.FRect{
		X: float32(minX) - left - border, Y: float32(minY) - top - border,
		W: float32(maxX-minX) + left + right + 2*border, H: float32(maxY-minY) + top + bottom + 2*border,
	}
	a.drawBox(renderer, &st, rect)
}
