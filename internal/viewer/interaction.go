package viewer

import (
	"fmt"
	"image/color"
	"math"
	"net/url"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) captureViewportAnchor() viewportAnchor {
	screenX, screenY := a.viewportAnchorScreenPoint()
	page, x, y, ok := a.pageAtAnchorScreenPoint(screenX, screenY)
	if !ok {
		return viewportAnchor{}
	}
	return viewportAnchor{page: page, point: a.pageTransform(page).toPage(x, y), valid: true}
}

func (a *App) restoreViewportAnchor(anchor viewportAnchor) {
	if !anchor.valid || anchor.page < 0 || anchor.page >= len(a.pageToRow) || len(a.rows) == 0 {
		a.clampScroll()
		return
	}
	pageX, pageY, ok := a.pageScreenOrigin(anchor.page)
	if !ok {
		a.clampScroll()
		return
	}
	dx, dy := a.pageTransform(anchor.page).toScreen(anchor.point.X, anchor.point.Y)
	targetX, targetY := a.viewportAnchorScreenPoint()
	a.scrollX += pageX + dx - targetX
	a.scrollY += pageY + dy - targetY
	a.clampScroll()
	if a.renderMode == renderContinuous {
		pageX, pageY, ok = a.pageScreenOrigin(anchor.page)
		if ok && (math.Abs(pageX+dx-targetX) > 0.5 || math.Abs(pageY+dy-targetY) > 0.5) {
			a.page = anchor.page
		} else {
			a.updateCurrentPageFromScroll()
		}
	}
}

func (a *App) relayoutWithViewportAnchor(update func()) {
	fallbackPage := a.page
	anchor := a.captureViewportAnchor()
	if update != nil {
		update()
	}
	a.recomputeLayout(a.viewportSize())
	if !anchor.valid {
		a.page = clampInt(fallbackPage, 0, a.pageCount-1)
		a.clampScroll()
		return
	}
	a.restoreViewportAnchor(anchor)
}

func (a *App) viewportAnchorScreenPoint() (float64, float64) {
	viewportW, _ := a.viewportSize()
	return float64(viewportW) / 2, a.viewportAnchorScreenY()
}

func (a *App) viewportAnchorScreenY() float64 {
	_, viewportH := a.viewportSize()
	switch a.config.AnchorPosition {
	case "top":
		return 0
	case "bottom":
		return float64(viewportH)
	default:
		return float64(viewportH) / 2
	}
}

func (a *App) pageAtAnchorScreenPoint(screenX, screenY float64) (int, float64, float64, bool) {
	rowIndex := a.viewportAnchorRowIndex()
	if rowIndex < 0 || rowIndex >= len(a.rows) {
		return 0, 0, 0, false
	}
	row := a.rows[rowIndex]
	for i, page := range row.pages {
		x, y := a.rowPageScreenOrigin(row, i)
		if screenX >= x && screenX <= x+row.pageW[i] && screenY >= y && screenY <= y+row.pageH[i] {
			return page, screenX - x, screenY - y, true
		}
	}
	if len(row.pages) == 0 {
		return 0, 0, 0, false
	}
	pageIndex := 0
	for i := range row.pages {
		x, _ := a.rowPageScreenOrigin(row, i)
		bestX, _ := a.rowPageScreenOrigin(row, pageIndex)
		if math.Abs(screenX-(x+row.pageW[i]/2)) < math.Abs(screenX-(bestX+row.pageW[pageIndex]/2)) {
			pageIndex = i
		}
	}
	x, y := a.rowPageScreenOrigin(row, pageIndex)
	return row.pages[pageIndex], screenX - x, screenY - y, true
}

func (a *App) pageScreenOrigin(page int) (float64, float64, bool) {
	if page < 0 || page >= len(a.pageToRow) || len(a.rows) == 0 {
		return 0, 0, false
	}
	row := a.rows[a.pageToRow[page]]
	for i, candidate := range row.pages {
		if candidate == page {
			x, y := a.rowPageScreenOrigin(row, i)
			return x, y, true
		}
	}
	return 0, 0, false
}

func (a *App) rowPageScreenOrigin(row rowLayout, pageIndex int) (float64, float64) {
	if a.renderMode == renderSingle {
		viewportW, viewportH := a.viewportSize()
		baseX := math.Max(float64(a.horizontalGap()), (float64(viewportW)-row.width)/2)
		baseY := math.Max(float64(a.verticalGap()), (float64(viewportH)-row.height)/2)
		return baseX + (row.pageX[pageIndex] - row.x) - a.scrollX, baseY + (row.pageY[pageIndex] - row.y) - a.scrollY
	}
	offsetX, offsetY := a.contentViewportOffset()
	return row.pageX[pageIndex] - a.scrollX + offsetX, row.pageY[pageIndex] - a.scrollY + offsetY
}

// viewportSize is the size of the document's part of the window: above
// the status bar, and beside any docked views.
func (a *App) viewportSize() (int, int) {
	if len(a.views.panels) > 0 && a.hasDock() {
		r := a.documentRect()
		return max(1, int(r.W)), max(1, int(r.H))
	}
	// Asked for many times a frame, as each row of pages is placed, the
	// window less the status bar is worked out directly.
	h := a.winH
	if a.statusVisible() {
		h -= a.statusReservedHeight()
	}
	return max(1, a.winW), max(1, h)
}

func (a *App) contentViewportOffset() (float64, float64) {
	viewportW, viewportH := a.viewportSize()
	offsetX := math.Max(0, (float64(viewportW)-a.contentW)/2)
	offsetY := math.Max(0, (float64(viewportH)-a.contentH)/2)
	if a.renderMode == renderContinuous {
		offsetY = 0
	}
	return offsetX, offsetY
}

func (a *App) renderMargin() float64 {
	_, viewportH := a.viewportSize()
	return math.Max(float64(viewportH)/2, a.pageStep*2)
}

func (a *App) pagePointAtScreen(sx, sy float64) (int, mupdf.Point, bool) {
	page, transformedX, transformedY, ok := a.pageGeometryAtScreen(sx, sy)
	if !ok || page < 0 || page >= len(a.pageMetrics) {
		return 0, mupdf.Point{}, false
	}
	return page, a.pageTransform(page).toPage(transformedX, transformedY), true
}

func (a *App) pageGeometryAtScreen(sx, sy float64) (int, float64, float64, bool) {
	if len(a.rows) == 0 {
		return 0, 0, 0, false
	}
	// Pages are laid out from the document's corner, which docked views
	// move from the window's.
	ox, oy := a.viewportOrigin()
	sx, sy = sx-ox, sy-oy
	if a.renderMode == renderSingle {
		if a.page < 0 || a.page >= len(a.pageToRow) {
			return 0, 0, 0, false
		}
		row := a.rows[a.pageToRow[a.page]]
		for i, page := range row.pages {
			x, y := a.rowPageScreenOrigin(row, i)
			if sx >= x && sy >= y && sx <= x+row.pageW[i] && sy <= y+row.pageH[i] {
				return page, sx - x, sy - y, true
			}
		}
		return 0, 0, 0, false
	}
	// Only the rows at the point's height can hold it; rows run down the
	// document in order.
	_, offsetY := a.contentViewportOffset()
	contentY := sy + a.scrollY - offsetY
	start, end := a.rowRangeForContentY(contentY, contentY)
	for _, row := range a.rows[start:end] {
		for i, page := range row.pages {
			x, y := a.rowPageScreenOrigin(row, i)
			if sx >= x && sy >= y && sx <= x+row.pageW[i] && sy <= y+row.pageH[i] {
				return page, sx - x, sy - y, true
			}
		}
	}
	return 0, 0, 0, false
}

// refreshStaleSelection extracts the selection once its focus has moved. A
// drag moves the focus on every pointer motion, often many times a frame, so
// the event loop calls this once per frame and before any other event.
func (a *App) refreshStaleSelection() {
	if a.selection.stale {
		a.refreshSelection()
	}
}

// refreshSelection extracts the selected text and quads. The first page is
// selected from its end point to where its text ends, pages in between
// entirely, and the last page from where its text starts to its end point,
// so a sentence running across pages selects as it reads.
func (a *App) refreshSelection() {
	sel := &a.selection
	first, firstPoint := sel.anchorPage, sel.anchor
	last, lastPoint := sel.focusPage, sel.focus
	if last < first {
		first, firstPoint, last, lastPoint = last, lastPoint, first, firstPoint
	}
	sel.stale = false
	sel.parts = sel.parts[:0]
	var text []string
	for page := first; page <= last; page++ {
		extracted, err := a.selectionOnPage(page, first, last, firstPoint, lastPoint)
		if err != nil {
			a.message = err.Error()
			return
		}
		if extracted == nil {
			continue
		}
		if len(extracted.Quads) > 0 {
			sel.parts = append(sel.parts, selectionPart{page: page, quads: extracted.Quads})
		}
		if extracted.Text != "" {
			text = append(text, extracted.Text)
		}
	}
	sel.text = strings.Join(text, "\n")
	a.emitSelectionChanged()
}

// selectionOnPage extracts the selection on one page, or nil when the page
// has no text. Pages between the ends are selected whole, so they are
// extracted once per selection rather than on every drag motion.
func (a *App) selectionOnPage(page, first, last int, firstPoint, lastPoint mupdf.Point) (*mupdf.Selection, error) {
	sel := &a.selection
	if page == first || page == last {
		return a.extractSelection(page, first, last, firstPoint, lastPoint)
	}
	if extracted, ok := sel.wholePages[page]; ok {
		return extracted, nil
	}
	extracted, err := a.extractSelection(page, first, last, firstPoint, lastPoint)
	if err == nil {
		if sel.wholePages == nil {
			sel.wholePages = map[int]*mupdf.Selection{}
		}
		sel.wholePages[page] = extracted
	}
	return extracted, err
}

func (a *App) extractSelection(page, first, last int, firstPoint, lastPoint mupdf.Point) (*mupdf.Selection, error) {
	start, end := firstPoint, lastPoint
	if first != last {
		textStart, textEnd, ok, err := a.doc.TextEnds(page)
		if err != nil || !ok {
			return nil, nil // no text to select on this page
		}
		if page != first {
			start = textStart
		}
		if page != last {
			end = textEnd
		}
	}
	return a.doc.ExtractSelection(page, start, end, a.selection.mode)
}

func (a *App) emitSelectionChanged() {
	pages := make([]int, len(a.selection.parts))
	for i, part := range a.selection.parts {
		pages[i] = part.page + 1
	}
	a.emitPluginEvent("selection_changed", map[string]any{"pages": pages, "text": a.selection.text, "active": a.selection.active})
}

func (a *App) copySelectionToClipboard() {
	if !a.config.CopyOnSelect || strings.TrimSpace(a.selection.text) == "" {
		return
	}
	if err := a.SetClipboard(a.selection.text); err != nil {
		a.message = "clipboard unavailable"
		return
	}
	a.message = fmt.Sprintf("copied %d chars", len(a.selection.text))
}

func (a *App) linkAt(sx, sy float64) (mupdf.Link, bool) {
	page, point, ok := a.pagePointAtScreen(sx, sy)
	if !ok {
		return mupdf.Link{}, false
	}
	links, _ := a.loadedLinks(page)
	for _, link := range links {
		if link.Bounds.Contains(point) {
			return link, true
		}
	}
	return mupdf.Link{}, false
}

// loadedLinks returns a page's links if they are loaded. The pointer looks
// for links on every motion, so it never waits on the document, which a
// render can hold for long: render workers load a page's links with its
// first tile.
func (a *App) loadedLinks(page int) ([]mupdf.Link, bool) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	links, ok := a.pageLinks[page]
	return links, ok
}

func (a *App) storeLinks(page int, links []mupdf.Link) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	if a.pageLinks == nil {
		a.pageLinks = map[int][]mupdf.Link{}
	}
	a.pageLinks[page] = links
}

func (a *App) linksForPage(page int) ([]mupdf.Link, error) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	return a.linksForPageLocked(page)
}

func (a *App) linksForPageLocked(page int) ([]mupdf.Link, error) {
	if links, ok := a.pageLinks[page]; ok {
		return links, nil
	}
	links, err := a.doc.Links(page)
	if err != nil {
		return nil, err
	}
	a.pageLinks[page] = links
	return links, nil
}

func (a *App) activateLink(link mupdf.Link) {
	if link.External {
		a.openDocumentURI(link.URI)
		return
	}
	if link.Page >= 0 {
		a.jumpToDestination(link.Page, link.X, link.Y, link.HasX, link.HasY)
		return
	}
	if link.URI != "" {
		a.message = link.URI
	}
}

// openDocumentURI opens a URI found in the document. Only schemes listed in
// link_schemes reach the OS; other URIs are shown instead.
func (a *App) openDocumentURI(uri string) bool {
	if uri == "" {
		return false
	}
	if !linkSchemeAllowed(uri, a.config.LinkSchemes) {
		a.message = "blocked link: " + uri
		return false
	}
	if err := a.OpenExternal(uri); err != nil {
		a.message = err.Error()
		return false
	}
	a.message = uri
	return true
}

func linkSchemeAllowed(uri string, schemes []string) bool {
	u, err := url.Parse(uri)
	return err == nil && u.Scheme != "" && slices.Contains(schemes, strings.ToLower(u.Scheme))
}

func (a *App) OpenExternal(uri string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", uri)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", uri)
	default:
		cmd = exec.Command("xdg-open", uri)
	}
	return cmd.Start()
}

// openTextFile opens path in the system's text editor.
func openTextFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-t", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func (a *App) drawSelection(renderer *sdl.Renderer) {
	for _, part := range a.selection.parts {
		if x, y, ok := a.pageScreenOrigin(part.page); ok {
			a.drawHighlightQuads(renderer, part.quads, part.page, x, y, a.selectionColor())
		}
	}
}

// selectionColor highlights selected text.
func (a *App) selectionColor() color.RGBA { return rgb(a.palette().Selection) }

// quadScreenBounds maps a quad on page, whose screen origin is (x, y), to
// its screen bounding box.
func (a *App) quadScreenBounds(quad mupdf.Quad, page int, x, y float64) (float64, float64, float64, float64) {
	pts := []mupdf.Point{quad.UL, quad.UR, quad.LL, quad.LR}
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	t := a.pageTransform(page)
	for _, pt := range pts {
		dx, dy := t.toScreen(pt.X, pt.Y)
		sx, sy := x+dx, y+dy
		minX = math.Min(minX, sx)
		minY = math.Min(minY, sy)
		maxX = math.Max(maxX, sx)
		maxY = math.Max(maxY, sy)
	}
	return minX, minY, maxX, maxY
}
