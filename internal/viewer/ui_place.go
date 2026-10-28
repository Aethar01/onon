package viewer

import (
	"math"
	"strings"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// viewPlacement is where a view sits: its size, and an anchor, a point it
// is pinned to, or a side of the window it is docked to.
type viewPlacement struct {
	width, height config.UISize
	anchor        string
	at            *config.UIPoint
	dock          string
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
	if fit && p.at == nil {
		// a menu keeps a least size
		w = max(w, min(260, areaW))
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
		return a.pinnedRect(*p.at, w, h), rows
	}
	x, y := int(area.X)+(int(area.W)-w)/2, int(area.Y)+(int(area.H)-full)/2
	if strings.HasSuffix(p.anchor, "left") {
		x = int(area.X) + gap
	} else if strings.HasSuffix(p.anchor, "right") {
		x = int(area.X+area.W) - w - gap
	}
	if strings.HasPrefix(p.anchor, "top") {
		y = int(area.Y) + gap
	} else if strings.HasPrefix(p.anchor, "bottom") {
		y = int(area.Y+area.H) - h - gap
	}
	return sdl.FRect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)}, rows
}

// pinnedRect is the frame of a view w by h with the corner pt names at
// pt. One pinned to a window point is kept in the window; one pinned to a
// page goes out of sight with it.
func (a *App) pinnedRect(pt config.UIPoint, w, h int) sdl.FRect {
	x, y, ok := a.uiPointOnScreen(pt)
	if !ok {
		return sdl.FRect{X: -1e6, Y: -1e6, W: float32(w), H: float32(h)}
	}
	if strings.HasSuffix(pt.Corner, "right") {
		x -= float64(w)
	}
	if strings.HasPrefix(pt.Corner, "bottom") {
		y -= float64(h)
	}
	if pt.Page == 0 {
		x = clampFloat(x, 0, math.Max(0, float64(a.winW-w)))
		y = clampFloat(y, 0, math.Max(0, float64(a.winH-h)))
	}
	return sdl.FRect{X: float32(math.Round(x)), Y: float32(math.Round(y)), W: float32(w), H: float32(h)}
}
