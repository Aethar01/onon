package viewer

import (
	"math"
	"strings"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

// viewPlacement is where a view sits: its size, and an anchor, a point it
// is pinned to, or a side of the window it is docked to.
type viewPlacement struct {
	width, height config.UISize
	anchor        string
	at            *config.UIPoint
	dock          string
	// fit narrows a list to what its rows take, its width being the most
	// and least, if set, the least.
	fit   bool
	least config.UISize
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

// placeMenu places one of onon's own menus where the menus option puts it,
// over its own place.
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
	if p.Anchor != "" {
		view.place.anchor = p.Anchor
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

// documentRect is the document's part of the window: above the status
// bar, and beside the docked views.
func (a *App) documentRect() sdl.FRect {
	doc, _ := a.dockLayout(nil)
	return doc
}

// dockLayout lays out the docked views in the order they opened, each
// taking its width or height from its side of what those before it left.
// It returns what is left, the document's part of the window, and
// target's place.
func (a *App) dockLayout(target *uiView) (doc, rect sdl.FRect) {
	h := a.winH
	if a.statusVisible() {
		h -= a.statusReservedHeight()
	}
	doc = sdl.FRect{W: float32(max(a.winW, 1)), H: float32(max(h, 1))}
	for _, view := range a.views.panels {
		if view.place.dock == "" || !view.visible {
			continue
		}
		var r sdl.FRect
		switch view.place.dock {
		case "left", "right":
			r = sdl.FRect{X: doc.X, Y: doc.Y, H: doc.H}
			r.W = float32(math.Round(min(a.uiLength(view.place.width, float64(a.winW)), float64(doc.W)*maxDockShare)))
			if view.place.dock == "right" {
				r.X = doc.X + doc.W - r.W
			} else {
				doc.X += r.W
			}
			doc.W -= r.W
		default:
			r = sdl.FRect{X: doc.X, Y: doc.Y, W: doc.W}
			r.H = float32(math.Round(min(a.uiLength(view.place.height, float64(a.winH)), float64(doc.H)*maxDockShare)))
			if view.place.dock == "bottom" {
				r.Y = doc.Y + doc.H - r.H
			} else {
				doc.Y += r.H
			}
			doc.H -= r.H
		}
		if view == target {
			rect = r
		}
	}
	return doc, rect
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

// withDocumentViewport draws with the document's part of the window as
// the renderer's viewport while views are docked, so the document is
// drawn as it is laid out, from its own corner, and not under them.
func (a *App) withDocumentViewport(renderer *sdl.Renderer, draw func()) {
	if !a.hasDock() {
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
	if len(a.views.panels) == 0 || !a.hasDock() {
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
	if at := view.place.at; at != nil && at.Page > 0 {
		return a.placeView(view)
	}
	key := viewGeometryKey{
		winW: a.winW, winH: a.winH, status: a.statusVisible(), styles: a.styles, font: a.fontFace, scale: a.uiScale,
		panels: a.views.layoutGen, rows: len(view.visibleRows()), place: view.place,
		headless: view.headless, titles: view.hasColumnTitles(), body: view.drawBody != nil,
		content: a.listContentWidth(view),
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
	styles                 *[config.ElementCount]config.Style
	font                   font.Face
	scale                  float64
	panels                 uint64
	rows                   int
	place                  viewPlacement
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
	rows := max(1, (h-head-bottom)/rowHeight)
	full := h
	if fit {
		full = min(h, head+rows*rowHeight+bottom)
		rows = min(rows, max(1, len(view.visibleRows())))
		h = min(full, head+rows*rowHeight+bottom)
	}
	if p.at != nil {
		return a.pinnedRect(*p.at, p.anchor, w, h), rows
	}
	x, y := anchorIn(area, p.anchor, w, full, gap)
	if strings.HasPrefix(p.anchor, "bottom") {
		y += full - h // its bottom kept at the edge
	}
	return sdl.FRect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, rows
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
