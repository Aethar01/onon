package viewer

import (
	"math"
	"slices"
	"strconv"

	"onon/internal/config"
	"onon/internal/instance"
)

func (a *App) currentScaleFromRows(viewportW, viewportH int, baseRows []rowLayout) float64 {
	if a.fitMode == fitManual {
		return a.zoom
	}
	if a.overview != nil {
		return a.overviewScale(viewportW)
	}
	if a.renderMode == renderSingle && len(baseRows) > 0 && a.page >= 0 {
		row := baseRows[clampInt(a.baseRowIndexForPage(a.page, baseRows), 0, len(baseRows)-1)]
		return a.fitScale(viewportW, viewportH, []rowLayout{row})
	}
	return a.fitScale(viewportW, viewportH, baseRows)
}

// fitScale is the scale at which base rows fit the viewport under the
// current fit mode. Gaps between pages keep their size, so each row fits
// when its pages' scaled width plus its gaps does.
func (a *App) fitScale(viewportW, viewportH int, rows []rowLayout) float64 {
	available := float64(viewportW) - float64(a.horizontalGap()*2)
	widthScale, maxHeight := math.Inf(1), 1.0
	for _, row := range rows {
		widthScale = math.Min(widthScale, (available-row.gaps)/math.Max(1, row.width))
		maxHeight = math.Max(maxHeight, row.height)
	}
	if math.IsInf(widthScale, 1) {
		widthScale = available
	}
	heightScale := (float64(viewportH) - float64(a.verticalGap()*2)) / maxHeight
	switch a.fitMode {
	case fitWidth:
		return math.Max(0.05, widthScale)
	case fitHeight:
		return math.Max(0.05, heightScale)
	default:
		return math.Max(0.05, math.Min(widthScale, heightScale))
	}
}

func (a *App) nextPage() {
	if a.pageCount == 0 {
		return
	}
	if a.dualPage {
		a.nextSpread()
		return
	}
	page := clampInt(a.page, 0, a.pageCount-1)
	if page < a.pageCount-1 {
		a.alignPageToAnchor(page + 1)
	} else {
		a.alignPageToViewportEdge(page, true)
	}
}

func (a *App) prevPage() {
	if a.pageCount == 0 {
		return
	}
	if a.dualPage {
		a.prevSpread()
		return
	}
	page := clampInt(a.page, 0, a.pageCount-1)
	if page > 0 {
		a.alignPageToAnchor(page - 1)
	} else {
		a.alignPageToViewportEdge(page, false)
	}
}

func (a *App) nextSpread() {
	if a.pageCount == 0 {
		return
	}
	row := a.rowIndexForPage(a.anchorPage(a.page))
	if row < len(a.rows)-1 {
		a.alignPageToAnchor(a.rows[row+1].pages[0])
	} else {
		a.alignPageToViewportEdge(a.pageCount-1, true)
	}
}

func (a *App) prevSpread() {
	if a.pageCount == 0 {
		return
	}
	row := a.rowIndexForPage(a.anchorPage(a.page))
	if row > 0 {
		a.alignPageToAnchor(a.rows[row-1].pages[0])
	} else {
		a.alignPageToViewportEdge(0, false)
	}
}

func (a *App) scrollBy(dx, dy float64) {
	oldX, oldY := a.scrollX, a.scrollY
	a.scrollX += dx
	a.scrollY += dy
	a.clampScroll()
	if a.renderMode == renderSingle || (a.scrollX == oldX && a.scrollY == oldY) {
		return
	}
	a.followScroll()
}

// followScroll updates the current page after the view scrolled freely. In
// the overview the selection follows the pointer instead, as on hover.
func (a *App) followScroll() {
	if a.overview != nil {
		a.hoverOverview(float64(a.pointer.X), float64(a.pointer.Y))
		return
	}
	a.updateCurrentPageFromScroll()
}

func (a *App) scrollByInput(dx, dy float64) {
	if !a.smoothScrollInputEnabled(a.currentSmoothInputSource()) {
		a.cancelSmoothScroll()
		a.scrollBy(dx, dy)
		return
	}
	a.queueSmoothScroll(dx, dy)
}

func (a *App) alignPageToAnchor(page int) {
	if page < 0 || page >= len(a.pageToRow) {
		return
	}
	page = a.anchorPage(page)
	if a.positionMatchesPageAnchor(page) {
		return
	}
	a.recordJump()
	if a.renderMode == renderSingle {
		a.page = page
		a.scrollX = 0
		a.scrollY = 0
		a.recomputeLayout(a.viewportSize())
		a.clampScroll()
		return
	}
	row := a.rows[a.pageToRow[page]]
	a.scrollY = a.scrollYForAnchoredRow(row)
	a.page = page
	a.clampScroll()
}

func (a *App) alignPageToViewportEdge(page int, bottom bool) {
	if page < 0 || page >= len(a.pageToRow) || len(a.rows) == 0 {
		return
	}
	row := a.rows[a.pageToRow[page]]
	for i, rowPage := range row.pages {
		if rowPage != page {
			continue
		}
		_, y := a.rowPageScreenOrigin(row, i)
		targetY := 0.0
		if bottom {
			_, viewportH := a.viewportSize()
			targetY = float64(viewportH) - row.pageH[i]
		}
		a.scrollY += y - targetY
		a.clampScroll()
		a.page = a.anchorPage(page)
		return
	}
}

// jumpToDestination navigates to a link or outline target. Missing
// coordinates default to the horizontal centre and top edge of the page.
func (a *App) jumpToDestination(page int, x, y float64, hasX, hasY bool) {
	if page < 0 || page >= len(a.pageMetrics) {
		return
	}
	if !hasX && !hasY {
		a.alignPageToAnchor(page)
		return
	}
	bounds := a.pageMetrics[page].bounds
	if !hasX {
		x = float64(bounds.X0+bounds.X1) / 2
	}
	if !hasY {
		y = float64(bounds.Y0)
	}
	a.alignPageToDocumentPoint(page, x, y)
}

func (a *App) alignPageToDocumentPoint(page int, x, y float64) {
	if page < 0 || page >= len(a.pageToRow) {
		return
	}
	page = a.anchorPage(page)
	a.recordJump()
	if a.renderMode == renderSingle {
		a.page = page
		a.recomputeLayout(a.viewportSize())
	}
	row := a.rows[a.pageToRow[page]]
	pageIndex := 0
	for i, rowPage := range row.pages {
		if rowPage == page {
			pageIndex = i
			break
		}
	}
	dx, dy := a.pageTransform(page).toScreen(x, y)
	viewportW, viewportH := a.viewportSize()
	a.scrollX = row.pageX[pageIndex] + dx - float64(viewportW)/2
	a.scrollY = row.pageY[pageIndex] + dy - float64(viewportH)/4
	a.page = page
	a.clampScroll()
}

// The jump list is the tab's, as Vim's is the window's, and its places
// name their documents: going to one in another document shows the tab
// that has it open, or opens it in this one. A new tab starts with the
// list of the tab it was opened from and where that was, and onon with the
// list it closed with.

func (a *App) recordJump() {
	if a.hasJumpPosition() {
		a.jumpBack = appendJump(a.jumpBack, a.currentJumpPosition())
	}
	a.jumpAhead = nil
	a.trimJumps()
}

// appendJump adds pos to the end of jumps unless it ends them already.
func appendJump(jumps []jumpPosition, pos jumpPosition) []jumpPosition {
	if len(jumps) > 0 && jumps[len(jumps)-1] == pos {
		return jumps
	}
	return append(jumps, pos)
}

// trimJumps keeps the tab's jump list to jump_list_max places, dropping
// the oldest.
func (a *App) trimJumps() {
	limit := a.config.JumpListMax
	if limit <= 0 {
		return
	}
	if over := len(a.jumpBack) + len(a.jumpAhead) - limit; over > 0 {
		drop := min(over, len(a.jumpBack))
		a.jumpBack = a.jumpBack[drop:]
		a.jumpAhead = a.jumpAhead[over-drop:]
	}
}

// hasJumpPosition reports whether the tab shows somewhere to come back to.
func (a *App) hasJumpPosition() bool {
	return a.docPath != "" || len(a.pageToRow) > 0
}

func (a *App) jumpForward() { a.followJump(&a.jumpAhead, &a.jumpBack, 1) }

func (a *App) jumpBackward() { a.followJump(&a.jumpBack, &a.jumpAhead, 1) }

// followJump goes to the nth place from the end of to, which loses it and
// those after it, leaving where the tab was and then those at the end of
// from, for the way back.
func (a *App) followJump(to, from *[]jumpPosition, n int) {
	n = min(n, len(*to))
	if n <= 0 {
		return
	}
	jump := (*to)[len(*to)-n]
	here, ok := a.leaveForJump(jump)
	if !ok {
		return
	}
	if a.hasJumpPosition() {
		*from = appendJump(*from, a.currentJumpPosition())
	}
	passed := (*to)[len(*to)-n+1:]
	for i := len(passed) - 1; i >= 0; i-- {
		*from = append(*from, passed[i])
	}
	*to = (*to)[:len(*to)-n]
	a.goToJump(jump, here)
}

// jumpPrevious goes to the place before the last jump, as two quotes do in Vim:
// where the tab was takes its place, so doing it again comes back.
func (a *App) jumpPrevious() {
	n := len(a.jumpBack)
	if n == 0 {
		a.message = "no place to jump back to"
		return
	}
	jump := a.jumpBack[n-1]
	here, ok := a.leaveForJump(jump)
	if !ok {
		return
	}
	a.jumpBack = a.jumpBack[:n-1]
	if a.hasJumpPosition() {
		a.jumpBack = appendJump(a.jumpBack, a.currentJumpPosition())
	}
	a.message = ""
	a.goToJump(jump, here)
}

// leaveForJump reports whether jump is in the tab's document, and whether
// the tab may go to it: not when that would lose unsaved edits, before a
// second asking.
func (a *App) leaveForJump(jump jumpPosition) (here, ok bool) {
	here = jump.path == "" || sameDocument(jump.path, a.docPath)
	return here, here || a.tabShowing(jump.path) >= 0 || a.confirmDiscard("jumping")
}

func (a *App) goToJump(jump jumpPosition, here bool) {
	if here {
		a.placeJump(jump, jump.stored)
	} else {
		a.openJump(jump)
	}
	a.pendingRedraw = true
}

// showJumps lists the jump list, oldest first as Vim's :jumps does, with
// where the tab is among its places; choosing one goes there.
func (a *App) showJumps() {
	back, ahead := a.jumpBack, a.jumpAhead
	if len(back)+len(ahead) == 0 {
		a.message = "no jumps"
		return
	}
	var rows []uiRow
	add := func(jump jumpPosition, steps int) {
		name, folder := "this document", ""
		if jump.path != "" {
			name, folder = recentFileParts(jump.path)
		}
		rows = append(rows, uiRow{index: len(rows), text: name, secondary: folder, key: "p. " + strconv.Itoa(jump.page+1), value: strconv.Itoa(steps)})
	}
	for i, jump := range back {
		add(jump, i-len(back))
	}
	selected := len(rows) - 1
	if a.hasJumpPosition() {
		selected = len(rows)
		add(a.currentJumpPosition(), 0)
		rows[selected].hint = "here"
	}
	for i := len(ahead) - 1; i >= 0; i-- {
		add(ahead[i], len(ahead)-i)
	}
	a.showRowList("jumps", "Jumps", rows, 70, 70, func(view *uiView) {
		view.selected = selected
		view.onSelect = func(a *App, row uiRow) {
			a.CloseUI("jumps")
			switch steps, _ := strconv.Atoi(row.value); {
			case steps < 0:
				a.followJump(&a.jumpBack, &a.jumpAhead, -steps)
			case steps > 0:
				a.followJump(&a.jumpAhead, &a.jumpBack, steps)
			}
		}
	})
}

// openJump goes to a place in another document: the tab that has it open,
// as it is, or the document opened in this tab, there.
func (a *App) openJump(jump jumpPosition) {
	if a.showOpenDocument(jump.path, 0) {
		return
	}
	opts := openDocumentOptions{jump: &jump}
	if a.doc != nil {
		state := a.captureViewState().atDocumentStart()
		opts.reloadConfig, opts.preserveView = a.windowGroup().documentsOpen() <= 1, &state
	}
	if err := a.openDocument(jump.path, opts); err != nil {
		a.message = err.Error()
	}
}

// sameDocument reports whether two paths name one document.
func sameDocument(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return instance.Key(config.AbsoluteDocumentPath(a)) == instance.Key(config.AbsoluteDocumentPath(b))
}

func (a *App) currentJumpPosition() jumpPosition {
	return jumpPosition{
		path:    a.docPath,
		page:    a.page,
		scrollX: a.scrollX,
		scrollY: a.scrollY,
		anchor:  a.captureViewportAnchor(),
	}
}

func (a *App) restoreJump(jump jumpPosition) {
	a.page = jump.page
	a.scrollX = jump.scrollX
	a.scrollY = jump.scrollY
	a.recomputeLayout(a.viewportSize())
}

// placeJump shows jump's place in the tab's document: by its scroll, or
// byAnchor when the document has been laid out again since, by what was
// at the viewport's anchor.
func (a *App) placeJump(jump jumpPosition, byAnchor bool) {
	if !byAnchor {
		a.restoreJump(jump)
		return
	}
	back, ahead := a.jumpBack, a.jumpAhead
	a.page = clampInt(jump.page, 0, max(0, a.pageCount-1))
	a.recomputeLayout(a.viewportSize())
	if jump.anchor.valid {
		a.restoreViewportAnchor(jump.anchor)
	} else {
		a.alignPageToAnchor(a.page)
	}
	a.jumpBack, a.jumpAhead = back, ahead
	a.pendingRedraw = true
}

// dropStoredJumpHere forgets the last place of an earlier run when the
// document has just opened there, as the one onon closed with does.
func (a *App) dropStoredJumpHere() {
	n := len(a.jumpBack)
	if n == 0 {
		return
	}
	last, here := a.jumpBack[n-1], a.captureViewportAnchor()
	if !last.stored || !sameDocument(last.path, a.docPath) {
		return
	}
	same := last.page == a.page
	if last.anchor.valid && here.valid {
		same = last.anchor.page == here.page &&
			math.Abs(last.anchor.point.X-here.point.X) < 2 && math.Abs(last.anchor.point.Y-here.point.Y) < 2
	}
	if same {
		a.jumpBack = a.jumpBack[:n-1]
	}
}

// jumpsForNewTab is the jump list a tab opened from this one starts with:
// this one's, and where it is.
func (a *App) jumpsForNewTab() []jumpPosition {
	jumps := slices.DeleteFunc(slices.Clone(a.jumpBack), func(j jumpPosition) bool { return j.path == "" })
	if a.docPath != "" && a.pageCount > 0 {
		jumps = appendJump(jumps, a.currentJumpPosition())
	}
	if limit := a.config.JumpListMax; limit > 0 && len(jumps) > limit {
		jumps = jumps[len(jumps)-limit:]
	}
	return jumps
}

// rememberJumps records the jump list of the tab shown, for the windows
// opened next.
func (a *App) rememberJumps() {
	if !a.config.SessionDatabase {
		return
	}
	jumps := a.jumpsForNewTab()
	entries := make([]config.JumpEntry, len(jumps))
	for i, jump := range jumps {
		entries[i] = config.JumpEntry{Path: jump.path, DocumentMark: config.DocumentMark{
			Page: jump.page, AnchorPage: jump.anchor.page, AnchorX: jump.anchor.point.X, AnchorY: jump.anchor.point.Y, AnchorValid: jump.anchor.valid,
		}}
	}
	if err := config.SetJumpList(entries); err != nil {
		a.logf("cannot record the jump list: %v", err)
	}
}

// storedJumps is the jump list rememberJumps recorded, its last limit
// places when limit is above 0.
func storedJumps(limit int) []jumpPosition {
	entries := config.JumpList()
	jumps := make([]jumpPosition, 0, len(entries))
	for _, entry := range entries {
		if entry.Path != "" {
			jumps = append(jumps, jumpPosition{path: entry.Path, page: entry.Page, anchor: viewportAnchorFromDocumentMark(entry.DocumentMark), stored: true})
		}
	}
	if limit > 0 && len(jumps) > limit {
		jumps = jumps[len(jumps)-limit:]
	}
	return jumps
}

func (a *App) positionMatchesPageAnchor(page int) bool {
	if a.renderMode == renderSingle {
		return a.page == page && a.scrollX == 0 && a.scrollY == 0
	}
	row := a.rows[a.pageToRow[page]]
	expectedY := a.clampedScrollY(a.scrollYForAnchoredRow(row))
	return a.page == page && a.scrollY == expectedY
}

func (a *App) scrollYForAnchoredRow(row rowLayout) float64 {
	_, viewportH := a.viewportSize()
	switch a.config.AnchorPosition {
	case "top":
		return row.y
	case "bottom":
		return row.y + row.height - float64(viewportH)
	default:
		return row.y + row.height/2 - float64(viewportH)/2
	}
}

func (a *App) clampedScrollY(scrollY float64) float64 {
	_, viewportH := a.viewportSize()
	maxY := math.Max(0, a.contentH-float64(viewportH))
	return clampFloat(scrollY, 0, maxY)
}

func (a *App) anchorPage(page int) int {
	if page < 0 || page >= len(a.pageToRow) {
		return page
	}
	if !a.dualPage || len(a.rows) == 0 {
		return page
	}
	row := a.rows[a.pageToRow[page]]
	if len(row.pages) == 0 {
		return page
	}
	return row.pages[0]
}

func (a *App) clampZoom(zoom float64) float64 {
	minZoom := a.config.MinZoom
	if minZoom <= 0 {
		minZoom = config.Default().MinZoom
	}
	maxZoom := a.config.MaxZoom
	if maxZoom < minZoom {
		maxZoom = minZoom
	}
	return clampFloat(zoom, minZoom, maxZoom)
}

func (a *App) setFitMode(mode fitMode) {
	a.cancelSmoothZoom()
	a.relayoutWithViewportAnchor(func() {
		a.fitMode = mode
		a.scheduleRenderScaleTarget(a.zoom)
	})
}

func (a *App) setAltColors(enabled bool) {
	if a.altColors == enabled {
		return
	}
	a.altColors = enabled
	a.restyleTiles()
}
