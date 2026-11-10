package viewer

import (
	"math"
	"strings"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

// viewPlacement is where a view sits: its size, and one of an anchor in
// the window, a point it is pinned to, a box it is attached to, or a side
// of the window it is docked to. The methods below set which, each
// clearing the others, so a view is placed one way only.
type viewPlacement struct {
	width, height config.UISize
	// anchor is where in the window the view sits, or, with at, which
	// part of it sits at that point. An attached view falls back to it
	// when what it is attached to is not shown.
	anchor string
	at     *config.UIPoint
	attach *attachment
	dock   string
	order  int // among what lines the edges, for a docked view
	// fit narrows a list to what its rows take, its width being the most
	// and least, if set, the least.
	fit   bool
	least config.UISize
}

// attachment is what an attached view sits against, the selected row of
// a menu or a box, and the side of it it sits on first. Without room
// there it takes the other side, and without room on either the side
// with more, kept in the document's part of the window.
type attachment struct {
	row  *uiView
	box  config.UIBox
	side string // right, left, below or above
}

// percentPlace is a view of the given percentages of the window, centred,
// as menus are.
func percentPlace(width, height int) viewPlacement {
	return viewPlacement{
		width:  config.UISize{Value: float64(width), Percent: true},
		height: config.UISize{Value: float64(height), Percent: true},
		anchor: "center",
	}
}

// menuPlace is a menu's place: centred, at most the given percentages of
// the window, and as narrow as its rows let it be.
func menuPlace(width, height int) viewPlacement {
	p := percentPlace(width, height)
	p.fit = true
	return p
}

// inWindow sits the view at anchor in the document's part of the window:
// center, or an edge or corner such as top or bottom_right.
func (p viewPlacement) inWindow(anchor string) viewPlacement {
	p.anchor, p.at, p.attach, p.dock = anchor, nil, nil, ""
	return p
}

// atPoint pins the part of the view corner names, such as top_left or
// center, to pt.
func (p viewPlacement) atPoint(pt config.UIPoint, corner string) viewPlacement {
	p.anchor, p.at, p.attach, p.dock = corner, &pt, nil, ""
	return p
}

// besideRow sits the view on the right of menu, its first row level with
// menu's selected row, as a submenu does; centred while menu is not shown.
func (p viewPlacement) besideRow(menu *uiView) viewPlacement {
	return p.attachTo(attachment{row: menu, side: "right"})
}

// belowBox sits the view below box, its left edge with box's, as a
// dropdown does; centred while box is on a page not shown.
func (p viewPlacement) belowBox(box config.UIBox) viewPlacement {
	return p.attachTo(attachment{box: box, side: "below"})
}

// atPointer sits the view's top left at pt, a point in the window, as a
// context menu does, or as near as it fits.
func (p viewPlacement) atPointer(pt config.UIPoint) viewPlacement {
	return p.belowBox(config.UIBox{X: pt.X, Y: pt.Y})
}

func (p viewPlacement) attachTo(att attachment) viewPlacement {
	if p.anchor == "" {
		p.anchor = "center"
	}
	p.at, p.attach, p.dock = nil, &att, ""
	return p
}

// onSide is p attached as it is, on side first.
func (p viewPlacement) onSide(side string) viewPlacement {
	if p.attach != nil {
		att := *p.attach
		att.side = side
		p.attach = &att
	}
	return p
}

// windowPoint is the window point at output pixels x, y.
func (a *App) windowPoint(x, y float32) config.UIPoint {
	scale := float64(a.px(1))
	return config.UIPoint{X: float64(x) / scale, Y: float64(y) / scale}
}

// placeMenu places one of onon's own menus where the menus option puts it,
// over its own place. Beside puts one opened over another beside the row
// it was opened from.
func (a *App) placeMenu(view *uiView) {
	if view.owner != "core" || len(a.config.Menus) == 0 {
		return
	}
	var p config.MenuPlacement
	found := false
	for name, id := range config.MenuViews {
		if id == view.id {
			p, found = a.config.Menus[name]
			break
		}
	}
	if !found {
		return
	}
	if p.Width.Value > 0 {
		view.place.width = p.Width
	}
	if p.Height.Value > 0 {
		view.place.height = p.Height
	}
	switch under := a.views.active; {
	case p.Anchor == config.MenuBeside:
		if view.stack && under != nil && under != view && under.visible {
			view.place = view.place.besideRow(under)
		}
	case p.Anchor != "":
		view.place = view.place.inWindow(p.Anchor)
	}
}

// maxDockShare is the most of the document's part of the window a docked
// view takes.
const maxDockShare = 0.8

// uiLength is size in output pixels, a percentage being of whole.
func (a *App) uiLength(size config.UISize, whole float64) float64 {
	if size.Percent {
		return whole * size.Value / 100
	}
	return size.Value * float64(a.px(1))
}

// documentRect is the document's part of the window: inside the bars and
// docked views along its edges.
func (a *App) documentRect() sdl.FRect {
	return a.layoutEdges().doc
}

// dockLayout returns the document's part of the window and target's
// place, docked at its side.
func (a *App) dockLayout(target *uiView) (doc, rect sdl.FRect) {
	l := a.layoutEdges()
	return l.doc, l.panel(target)
}

// hasDock reports whether a view is docked, moving the document from the
// window's corner.
func (a *App) hasDock() bool {
	for _, view := range a.views.panels {
		if view.place.dock != "" && view.visible {
			return true
		}
	}
	return false
}

// documentMoved reports whether the document's part of the window starts
// anywhere but its top left corner: below the tab bar, or beside a docked
// view.
func (a *App) documentMoved() bool {
	if a.edgesPlain() {
		return false
	}
	r := a.documentRect()
	return r.X != 0 || r.Y != 0
}

// withDocumentViewport draws with the document's part of the window as
// the renderer's viewport while views are docked, so the document is
// drawn as it is laid out, from its own corner, and not under them.
func (a *App) withDocumentViewport(renderer *sdl.Renderer, draw func()) {
	if !a.documentMoved() {
		draw()
		return
	}
	r := a.documentRect()
	rect := sdl.Rect{X: int32(r.X), Y: int32(r.Y), W: int32(r.W), H: int32(r.H)}
	sdl.SetRenderViewport(renderer, &rect)
	defer sdl.SetRenderViewport(renderer, nil)
	draw()
}

// viewportOrigin is where the document's part of the window starts, below
// and right of the views docked at the top and left.
func (a *App) viewportOrigin() (float64, float64) {
	if !a.documentMoved() {
		return 0, 0 // nothing moves it from the window's corner
	}
	r := a.documentRect()
	return float64(r.X), float64(r.Y)
}

// uiPointOnScreen is where pt is in the window, and false when it is on a
// page not shown.
func (a *App) uiPointOnScreen(pt config.UIPoint) (float64, float64, bool) {
	if pt.Page == 0 {
		scale := float64(a.px(1))
		return pt.X * scale, pt.Y * scale, true
	}
	page := pt.Page - 1
	if page >= len(a.pageMetrics) || page >= len(a.pageToRow) {
		return 0, 0, false
	}
	if a.renderMode == renderSingle && (a.page < 0 || a.page >= len(a.pageToRow) || a.pageToRow[page] != a.pageToRow[a.page]) {
		return 0, 0, false
	}
	x, y, ok := a.pageScreenOrigin(page)
	if !ok {
		return 0, 0, false
	}
	dx, dy := a.pageTransform(page).toScreen(pt.X, pt.Y)
	ox, oy := a.viewportOrigin()
	return ox + x + dx, oy + y + dy, true
}

// viewGeometry places a view, returning its frame and how many rows of a
// list it shows. A docked view fills its place. Another takes its size,
// at most the document's part of the window, and sits at its anchor there,
// a gap from the edges it is anchored to, or with a corner at the point it
// is pinned to. A list fits its panel to its rows, keeping the edge it is
// anchored to, or a centred one its top, so filtering leaves the header
// in place.
func (a *App) viewGeometry(view *uiView) (sdl.FRect, int) {
	// A view's geometry is asked for many times a frame and an event, and
	// changes only with what the key holds; one pinned to a page moves
	// with it, so it is worked out each time.
	if at := view.place.at; (at != nil && at.Page > 0) || view.place.attach != nil {
		return a.placeView(view) // an attached view moves with what it is attached to
	}
	edges := a.layoutEdges()
	key := viewGeometryKey{
		winW: a.winW, winH: a.winH, status: a.statusVisible(), doc: edges.doc, panel: edges.panel(view), styles: a.styles, font: a.fontFace, scale: a.uiScale,
		panels: a.views.layoutGen, rows: len(view.visibleRows()), place: view.place,
		headless: view.headless, titles: view.hasColumnTitles(), body: view.drawBody != nil,
		content: a.listContentWidth(view), aside: view.aside.key(),
	}
	if g := &view.geometry; g.ok && g.key == key {
		return g.rect, g.rows
	}
	rect, rows := a.placeView(view)
	view.geometry = viewGeometryCache{key: key, rect: rect, rows: rows, ok: a.styles != nil}
	return rect, rows
}

// viewGeometryKey is what a view's geometry depends on: the window, the
// status bar, the theme and font, the panels docked, the rows it fits, and
// its own placement and parts.
type viewGeometryKey struct {
	winW, winH             int
	status                 bool
	doc, panel             sdl.FRect // what the bars and docked views leave, and its own place
	styles                 *[config.ElementCount]config.Style
	font                   font.Face
	scale                  float64
	panels                 uint64
	rows                   int
	place                  viewPlacement
	aside                  asideKey
	headless, titles, body bool
	content                int
}

type viewGeometryCache struct {
	key  viewGeometryKey
	rect sdl.FRect
	rows int
	ok   bool
}

// placeView works out view's geometry for viewGeometry.
func (a *App) placeView(view *uiView) (sdl.FRect, int) {
	p := &view.place
	rowHeight, head, bottom := a.modalListRowHeight(), a.uiViewHeadHeight(view), a.viewBottomPadding(view)
	if p.dock != "" {
		_, rect := a.dockLayout(view)
		return rect, max(1, (int(rect.H)-head-bottom)/rowHeight)
	}
	fit := view.drawBody == nil
	area := a.documentRect()
	gap := 0
	if p.at == nil && p.anchor != "center" {
		gap = a.ipx(12)
	}
	areaW, areaH := int(area.W)-2*gap, int(area.H)-2*gap
	w := clampInt(int(math.Round(a.uiLength(p.width, float64(a.winW)))), 1, max(1, areaW))
	h := clampInt(int(math.Round(a.uiLength(p.height, float64(a.winH)))), 1, max(1, areaH))
	least := 260
	if content := a.listContentWidth(view); content > 0 {
		most := w
		w = min(w, content)
		least = a.ipx(320) // a fitted menu of short rows still reads as a menu
		if p.least.Value > 0 {
			least = max(least, int(math.Round(a.uiLength(p.least, float64(a.winW)))))
		}
		least = min(least, most)
	}
	if fit && p.at == nil {
		// a menu keeps a least size
		w = max(w, min(least, areaW))
		h = max(h, min(160, areaH))
	}
	// An aside widens the view to its side, and keeps it its full height,
	// however few its rows, for the aside to draw in.
	if aside := a.asideWidth(view); aside > 0 {
		w = min(w+aside, max(1, areaW))
	}
	rows := max(1, (h-head-bottom)/rowHeight)
	full := h
	if fit {
		full = min(h, head+rows*rowHeight+bottom)
		rows = min(rows, max(1, len(view.visibleRows())))
		if view.aside == nil {
			h = min(full, head+rows*rowHeight+bottom)
		} else {
			h = full
		}
	}
	if p.at != nil {
		return a.pinnedRect(*p.at, p.anchor, w, h), rows
	}
	if p.attach != nil {
		if rect, ok := a.attachedRect(*p.attach, area, head, w, h); ok {
			return rect, rows
		}
	}
	x, y := anchorIn(area, p.anchor, w, full, gap)
	if strings.HasPrefix(p.anchor, "bottom") {
		y += full - h // its bottom kept at the edge
	}
	return sdl.FRect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, rows
}

// attachedRect is the frame of a view w by h, head tall above its rows,
// attached as att says, kept in area: on att's side of what it is
// attached to, a gap from it, or the other side, or whichever side has
// more room. Beside a row its first row is level with the row; beside or
// below a box, its edges are with the box's. It is false while what it is
// attached to is not shown.
func (a *App) attachedRect(att attachment, area sdl.FRect, head, w, h int) (sdl.FRect, bool) {
	box, ok := a.attachmentBox(att)
	if !ok {
		return sdl.FRect{}, false
	}
	gap := float64(a.px(6))
	if box.W == 0 && box.H == 0 {
		gap = 0 // at a point, such as the pointer
	}
	fw, fh := float64(w), float64(h)
	left, top := float64(area.X), float64(area.Y)
	right, bottom := left+float64(area.W), top+float64(area.H)
	x, y := box.X, box.Y
	switch att.side {
	case "right", "left":
		if att.row != nil {
			y -= float64(head)
		}
		after, before := box.X+box.W+gap, box.X-gap-fw
		x = pickSide(att.side == "right", after, before, right-after, box.X-gap-left, fw)
	default: // below, above
		after, before := box.Y+box.H+gap, box.Y-gap-fh
		y = pickSide(att.side != "above", after, before, bottom-after, box.Y-gap-top, fh)
	}
	x = clampFloat(x, left, math.Max(left, right-fw))
	y = clampFloat(y, top, math.Max(top, bottom-fh))
	return sdl.FRect{X: float32(math.Round(x)), Y: float32(math.Round(y)), W: float32(w), H: float32(h)}, true
}

// pickSide is where a view size long sits along one axis: at after, past
// what it is attached to, or at before, short of it, room being how much
// each side has; afterFirst tries after first.
func pickSide(afterFirst bool, after, before, roomAfter, roomBefore, size float64) float64 {
	fitsAfter, fitsBefore := roomAfter >= size, roomBefore >= size
	switch {
	case afterFirst && fitsAfter, !afterFirst && !fitsBefore && fitsAfter:
		return after
	case fitsBefore:
		return before
	case roomAfter >= roomBefore:
		return after
	default:
		return before
	}
}

// screenBox is a box in output pixels.
type screenBox struct{ X, Y, W, H float64 }

// attachmentBox is the box, in output pixels, a view attached as att
// says sits against: the selected row of its menu across the menu's
// width, or its box in the window or on its page.
func (a *App) attachmentBox(att attachment) (box screenBox, ok bool) {
	if menu := att.row; menu != nil {
		if !menu.visible {
			return box, false
		}
		frame, _ := menu.frameGeometry(a)
		rowHeight := float64(a.modalListRowHeight())
		box.X, box.W, box.H = float64(frame.X), float64(frame.W), rowHeight
		box.Y = float64(frame.Y) + float64(a.uiViewHeadHeight(menu))
		for i, row := range menu.visibleRows() {
			if row.index == menu.selected {
				box.Y += (float64(i) - menu.offset) * rowHeight
				break
			}
		}
		return box, true
	}
	b := att.box
	x0, y0, ok0 := a.uiPointOnScreen(config.UIPoint{Page: b.Page, X: b.X, Y: b.Y})
	x1, y1, ok1 := a.uiPointOnScreen(config.UIPoint{Page: b.Page, X: b.X + b.W, Y: b.Y + b.H})
	if !ok0 || !ok1 {
		return box, false
	}
	box.X, box.Y = math.Min(x0, x1), math.Min(y0, y1)
	box.W, box.H = math.Abs(x1-x0), math.Abs(y1-y0)
	return box, true
}

// anchorIn is where a box w by h anchored at anchor sits in area, gap
// from the edges it is anchored to.
func anchorIn(area sdl.FRect, anchor string, w, h, gap int) (int, int) {
	x, y := int(area.X)+(int(area.W)-w)/2, int(area.Y)+(int(area.H)-h)/2
	if strings.HasSuffix(anchor, "left") {
		x = int(area.X) + gap
	} else if strings.HasSuffix(anchor, "right") {
		x = int(area.X+area.W) - w - gap
	}
	if strings.HasPrefix(anchor, "top") {
		y = int(area.Y) + gap
	} else if strings.HasPrefix(anchor, "bottom") {
		y = int(area.Y+area.H) - h - gap
	}
	return x, y
}

// pinnedRect is the frame of a view w by h whose part anchor names, such
// as its top left, its bottom or its centre, sits at pt. One pinned to a
// window point is kept in the window; one pinned to a page goes out of
// sight with it.
func (a *App) pinnedRect(pt config.UIPoint, anchor string, w, h int) sdl.FRect {
	x, y, ok := a.uiPointOnScreen(pt)
	if !ok {
		return sdl.FRect{X: -1e6, Y: -1e6, W: float32(w), H: float32(h)}
	}
	switch {
	case strings.HasSuffix(anchor, "right"):
		x -= float64(w)
	case !strings.HasSuffix(anchor, "left"):
		x -= float64(w) / 2
	}
	switch {
	case strings.HasPrefix(anchor, "bottom"):
		y -= float64(h)
	case !strings.HasPrefix(anchor, "top"):
		y -= float64(h) / 2
	}
	if pt.Page == 0 {
		x = clampFloat(x, 0, math.Max(0, float64(a.winW-w)))
		y = clampFloat(y, 0, math.Max(0, float64(a.winH-h)))
	}
	return sdl.FRect{X: float32(math.Round(x)), Y: float32(math.Round(y)), W: float32(w), H: float32(h)}
}

// dockEdgeAt is the resizable docked view whose inner edge is at x, y,
// within a few pixels either side.
func (a *App) dockEdgeAt(x, y float32) *uiView {
	band := a.px(4)
	for _, view := range a.views.panels {
		if !view.visible || !view.resizable || view.place.dock == "" {
			continue
		}
		_, r := a.dockLayout(view)
		var near bool
		switch view.place.dock {
		case "left":
			near = abs32(x-(r.X+r.W)) <= band && y >= r.Y && y <= r.Y+r.H
		case "right":
			near = abs32(x-r.X) <= band && y >= r.Y && y <= r.Y+r.H
		case "top":
			near = abs32(y-(r.Y+r.H)) <= band && x >= r.X && x <= r.X+r.W
		case "bottom":
			near = abs32(y-r.Y) <= band && x >= r.X && x <= r.X+r.W
		}
		if near {
			return view
		}
	}
	return nil
}

// handleDockResizeButton starts dragging a docked view's edge on a press
// at it, and ends the drag on the release, reporting whether it did.
func (a *App) handleDockResizeButton(e *sdl.MouseButtonEvent) bool {
	if e.Button != uint8(sdl.ButtonLeft) {
		return false
	}
	if e.Type == sdl.EventMouseButtonUp {
		resizing := a.views.resizing != nil
		a.views.resizing = nil
		return resizing
	}
	a.views.resizing = a.dockEdgeAt(e.X, e.Y)
	return a.views.resizing != nil
}

// dragDockEdge resizes the docked view whose edge is being dragged to the
// pointer at x, y, the document laying itself out beside it, and reports
// whether one was.
func (a *App) dragDockEdge(x, y float32) bool {
	view := a.views.resizing
	if view == nil || !view.visible {
		a.views.resizing = nil
		return false
	}
	_, r := a.dockLayout(view)
	var size float32
	switch view.place.dock {
	case "left":
		size = x - r.X
	case "right":
		size = r.X + r.W - x
	case "top":
		size = y - r.Y
	case "bottom":
		size = r.Y + r.H - y
	}
	length := config.UISize{Value: float64(max(size, a.px(40))) / float64(a.px(1))}
	a.relayoutWithViewportAnchor(func() {
		if view.place.dock == "left" || view.place.dock == "right" {
			view.place.width = length
		} else {
			view.place.height = length
		}
		a.views.layoutGen++ // every view sits beside it
	})
	a.pendingRedraw = true
	return true
}

func (a *App) UIViewSize(id string) (float64, float64) {
	view := a.views.views[id]
	if view == nil {
		return 0, 0
	}
	return a.viewSize(view)
}

// viewSize is view's size in logical pixels.
func (a *App) viewSize(view *uiView) (float64, float64) {
	rect, _ := view.frameGeometry(a)
	scale := float64(a.px(1))
	return float64(rect.W) / scale, float64(rect.H) / scale
}
