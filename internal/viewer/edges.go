package viewer

import (
	"cmp"
	"math"
	"slices"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// The status bar, the tab bar and the docked views line the window's
// edges. Each has a side and an order, and takes its side of what those
// of lower order leave, the document having the rest: so a status bar of
// lower order spans the window beneath a tab bar at the left, and one of
// higher order sits beside it. A bar that floats takes no room, and floats
// over the document's edge.

// edgeKind is what lines an edge.
type edgeKind int

const (
	edgeStatus edgeKind = iota
	edgeTabBar
	edgePanel
)

type edgeItem struct {
	kind  edgeKind
	side  string // top, bottom, left or right
	order int
	view  *uiView // a docked view
}

// edgeLayout is where what lines the edges is, in render pixels.
type edgeLayout struct {
	doc    sdl.FRect // the document's part of the window
	status sdl.FRect // the status bar's strip, margins and all; empty when hidden
	tabBar sdl.FRect // empty when hidden
	panels []sdl.FRect
	views  []*uiView // the docked view each of panels is for
}

// panel is where view is, or an empty rect when it is not docked.
func (l edgeLayout) panel(view *uiView) sdl.FRect {
	if i := slices.Index(l.views, view); i >= 0 {
		return l.panels[i]
	}
	return sdl.FRect{}
}

// edgeItems is what lines the edges now, in their order, ties in the order
// of the status bar, the tab bar, then the views in the order they opened.
func (a *App) edgeItems() []edgeItem {
	var items []edgeItem
	if a.statusVisible() {
		items = append(items, edgeItem{kind: edgeStatus, side: "bottom", order: a.config.StatusBarOrder})
	}
	if a.tabBarShown() {
		items = append(items, edgeItem{kind: edgeTabBar, side: a.tabBarSide(), order: a.config.TabBarOrder})
	}
	for _, view := range a.views.panels {
		if view.place.dock != "" && view.visible {
			items = append(items, edgeItem{kind: edgePanel, side: view.place.dock, order: view.place.order, view: view})
		}
	}
	slices.SortStableFunc(items, func(x, y edgeItem) int { return cmp.Compare(x.order, y.order) })
	return items
}

// layoutEdges lays out what lines the window's edges.
func (a *App) layoutEdges() edgeLayout {
	rest := sdl.FRect{W: float32(max(a.winW, 1)), H: float32(max(a.winH, 1))}
	if a.presentation == nil && a.nativeInset > 0 {
		// The system's tab bar, over the top of the window.
		inset := float32(a.nativeInset)
		rest.Y, rest.H = inset, max(rest.H-inset, 1)
	}
	var l edgeLayout
	var floating []edgeItem // laid over the document once it is known
	for _, item := range a.edgeItemsMoving() {
		if a.edgeFloats(item) {
			floating = append(floating, item)
			continue
		}
		var size float32
		in := float32(a.edgeIn(item)) // how far in it is, while the edges move
		switch item.kind {
		case edgeStatus:
			size = float32(a.statusStripHeight())
		case edgeTabBar:
			size = a.tabBarThickness(rest)
		case edgePanel:
			view := item.view
			if item.side == "left" || item.side == "right" {
				size = float32(math.Round(min(a.uiLength(view.place.width, float64(a.winW)), float64(rest.W)*maxDockShare)))
			} else {
				size = float32(math.Round(min(a.uiLength(view.place.height, float64(a.winH)), float64(rest.H)*maxDockShare)))
			}
		}
		// It is laid out where it rests, and drawn slid out as far as it is
		// out; the document has what it leaves.
		resting := rest
		r := takeEdge(&resting, item.side, size)
		takeEdge(&rest, item.side, size*in)
		switch item.kind {
		case edgeStatus:
			l.status = r
		case edgeTabBar:
			l.tabBar = r
		case edgePanel:
			l.panels = append(l.panels, r)
			l.views = append(l.views, item.view)
		}
	}
	l.doc = sdl.FRect{X: rest.X, Y: rest.Y, W: max(rest.W, 1), H: max(rest.H, 1)}
	// What floats lies over the document's edges, in its order too.
	over := l.doc
	for _, item := range floating {
		switch item.kind {
		case edgeStatus:
			l.status = takeEdge(&over, item.side, float32(a.statusStripHeight()))
		case edgeTabBar:
			l.tabBar = takeEdge(&over, item.side, a.tabBarThickness(over))
		}
	}
	return l
}

// edgeFloats reports whether item floats over the document rather than
// taking its side from it.
func (a *App) edgeFloats(item edgeItem) bool {
	switch item.kind {
	case edgeStatus:
		return a.style(config.ElementStatus).Floating.V
	case edgeTabBar:
		return a.style(config.ElementTabBar).Floating.V
	}
	return false
}

// takeEdge takes a strip size deep from side of rest, and returns it.
func takeEdge(rest *sdl.FRect, side string, size float32) sdl.FRect {
	switch side {
	case "left", "right":
		size = min(max(size, 0), rest.W)
		r := sdl.FRect{X: rest.X, Y: rest.Y, W: size, H: rest.H}
		if side == "right" {
			r.X = rest.X + rest.W - size
		} else {
			rest.X += size
		}
		rest.W -= size
		return r
	default:
		size = min(max(size, 0), rest.H)
		r := sdl.FRect{X: rest.X, Y: rest.Y, W: rest.W, H: size}
		if side == "bottom" {
			r.Y = rest.Y + rest.H - size
		} else {
			rest.Y += size
		}
		rest.H -= size
		return r
	}
}

// statusStripHeight is the depth of the status bar's strip: the bar and
// its margin below.
func (a *App) statusStripHeight() int {
	_, _, marginBottom, _ := a.insets(a.style(config.ElementStatus).Margin.V)
	return a.statusBarHeight() + int(math.Round(float64(marginBottom)))
}

// edgesPlain reports whether nothing moves the document from the window's
// top left or narrows it but a status bar along the bottom, as is usual,
// which the layout of every row of pages need not ask more about.
func (a *App) edgesPlain() bool {
	if a.tabBarShown() || a.edgeMotion.active || (a.presentation == nil && a.nativeInset > 0) {
		return false
	}
	for _, view := range a.views.panels {
		if view.place.dock != "" && view.visible {
			return false
		}
	}
	return true
}
