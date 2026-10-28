package viewer

import (
	"math"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Resting the pointer on an internal link previews its destination in a
// panel, a view drawing the target page's tiles at the current zoom.

type linkPreview struct {
	link   mupdf.Link
	since  time.Time
	anchor sdl.FPoint // pointer position when the hover began, in the document's part of the window
	view   *uiView    // the panel, once shown
}

// hoverLinkPreview starts or ends a preview as the pointer moves.
func (a *App) hoverLinkPreview(link mupdf.Link, over bool, x, y float32) {
	internal := over && !link.External && link.Page >= 0 && link.Page < a.pageCount
	switch {
	case !internal || !a.config.LinkPreview:
		a.closeLinkPreview()
	case a.preview == nil || a.preview.link != link:
		a.closeLinkPreview()
		a.preview = &linkPreview{link: link, since: time.Now(), anchor: sdl.FPoint{X: x, Y: y}}
		a.wakeAfter(a.linkPreviewDelay())
	}
}

// closeLinkPreview ends a preview, closing its panel if it showed.
func (a *App) closeLinkPreview() {
	if a.preview != nil && a.preview.view != nil {
		a.closeUIView(a.preview.view, false)
	}
	a.preview = nil
}

func (a *App) linkPreviewDelay() time.Duration {
	return time.Duration(a.config.LinkPreviewDelayMS) * time.Millisecond
}

// revealLinkPreview shows a pending preview once it is due.
func (a *App) revealLinkPreview() {
	if a.previewShown() && a.preview.view == nil {
		a.showLinkPreview()
	}
}

func (a *App) previewShown() bool {
	return a.preview != nil && time.Since(a.preview.since) >= a.linkPreviewDelay()
}

// previewDeadline is when a pending preview appears, or zero.
func (a *App) previewDeadline() time.Time {
	if a.preview == nil || a.previewShown() {
		return time.Time{}
	}
	return a.preview.since.Add(a.linkPreviewDelay())
}

// showLinkPreview opens the preview's panel: headless, in the
// link_preview element, pinned where previewPlacement puts it, drawing
// the part of the target page from the destination.
func (a *App) showLinkPreview() {
	link := a.preview.link
	popup, x, y := a.previewPlacement()
	ox, oy := a.viewportOrigin()
	scale := float64(a.px(1))
	// The page drawn from x, y puts the destination near the popup's top
	// left; the point at its top left is where the drawing starts.
	from := a.pageTransform(link.Page).toPage(float64(popup.X)-x, float64(popup.Y)-y)
	view := &uiView{
		id: "core:link-preview", owner: "core", headless: true, element: config.ElementLinkPreview,
		place: viewPlacement{
			width:  config.UISize{Value: float64(popup.W) / scale},
			height: config.UISize{Value: float64(popup.H) / scale},
			at:     &config.UIPoint{X: (ox + float64(popup.X)) / scale, Y: (oy + float64(popup.Y)) / scale},
		},
		drawBody: func(a *App, c *elementCanvas, state config.DrawState) (bool, error) {
			c.Page(link.Page, 0, 0, state.W, state.H, &config.PageArea{X: from.X, Y: from.Y, Zoom: 1})
			return false, nil
		},
	}
	a.preview.view = view
	a.showUIView(view)
}

// previewPlacement returns the popup rect and the screen origin at which to
// draw the target page so its destination sits near the popup's top left.
func (a *App) previewPlacement() (popup sdl.FRect, x, y float64) {
	viewportW, viewportH := a.viewportSize()
	link := a.preview.link
	m := a.pageMetrics[link.Page]
	popup.W = float32(math.Min(float64(viewportW)*0.5, m.width*a.scale))
	popup.H = float32(float64(viewportH) * 0.35)
	popup.X = min(max(0, a.preview.anchor.X-popup.W/2), float32(viewportW)-popup.W)
	popup.Y = a.preview.anchor.Y + 20
	if popup.Y+popup.H > float32(viewportH) {
		popup.Y = max(0, a.preview.anchor.Y-20-popup.H)
	}

	destX, destY := float64(m.bounds.X0+m.bounds.X1)/2, float64(m.bounds.Y0)
	if link.HasX {
		destX = link.X
	}
	if link.HasY {
		destY = link.Y
	}
	dx, dy := newPageTransform(m.bounds, a.scale, a.rotation).toScreen(destX, destY)
	const inset = 16
	screenX := float64(popup.X + popup.W/2) // centre the page when x is not given
	if link.HasX {
		screenX = float64(popup.X) + inset
	}
	return popup, screenX - dx, float64(popup.Y) + inset - dy
}
