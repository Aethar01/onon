package viewer

import (
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestLinkPreviewAppearsAfterDelayAndRequestsTiles(t *testing.T) {
	app := testPrefetchApp(5, 1)
	app.config.LinkPreview, app.config.LinkPreviewDelayMS = true, 400
	link := mupdf.Link{Page: 3, Y: 50, HasY: true}

	app.hoverLinkPreview(link, true, 100, 100)
	if app.previewShown() || app.previewDeadline().IsZero() {
		t.Fatal("preview shown before the delay")
	}
	app.preview.since = time.Now().Add(-app.linkPreviewDelay())
	app.revealLinkPreview()
	view := app.preview.view
	if !app.previewShown() || view == nil || !view.visible || !app.pendingRedraw {
		t.Fatal("preview not revealed after the delay")
	}
	// Drawn, the panel notes the page's tiles, which are asked for once
	// the render worker knows they are wanted, and kept wanted.
	if err := app.drawViewBody(nil, view, app.uiViewBody(view), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !app.canvasRendersUnasked() {
		t.Fatal("the preview's tiles were asked for before the worker knew they were wanted")
	}
	app.prefetchVisiblePages()
	if app.canvasRendersUnasked() {
		t.Fatal("the prefetch did not ask for the preview's tiles")
	}
	requested := false
	for key, req := range app.renderPending {
		requested = requested || key.page == 3 && req.priority == 0
	}
	if !requested {
		t.Fatalf("no on-screen tiles requested for the previewed page: %v", app.renderPending)
	}
	for key := range app.canvas.tiles {
		if _, pending := app.renderPending[key]; !pending {
			t.Fatalf("the preview's tile %+v was let go by the prefetch", key)
		}
	}

	popup, _, y := app.previewPlacement()
	if y+50*app.scale != float64(popup.Y)+16 {
		t.Fatalf("destination drawn at y=%.1f, want popup top + 16 (%.1f)", y+50*app.scale, popup.Y+16)
	}
	if rect, _ := view.frameGeometry(app); rect.W != popup.W || rect.H != popup.H {
		t.Fatalf("the panel is %+v, the popup %+v", rect, popup)
	}

	app.hoverLinkPreview(mupdf.Link{External: true, URI: "https://x"}, true, 0, 0)
	if app.preview != nil || view.visible {
		t.Fatal("external links are not previewed, and leaving the link closes its preview")
	}
}

func TestLinkPreviewPaddingInsetsPageAndCorners(t *testing.T) {
	app := testPrefetchApp(5, 1)
	app.config.LinkPreview, app.config.LinkPreviewDelayMS = true, 0
	lp := &app.config.Theme.Elements[config.ElementLinkPreview]
	lp.Padding = config.Opt[config.Insets]{V: config.Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}, Set: true}
	lp.Radius = config.Opt[config.Corners]{V: config.Corners{14, 14, 14, 14}, Set: true}
	app.styles = nil

	link := mupdf.Link{Page: 2, Y: 40, HasY: true}
	app.hoverLinkPreview(link, true, 100, 100)
	app.revealLinkPreview()
	view := app.preview.view
	if view == nil || !view.visible {
		t.Fatal("preview not shown")
	}

	popup, _, y := app.previewPlacement()
	// Destination sits 16px inside the padded inner top edge (popup.Y + 8 + 16).
	if got, want := y+40*app.scale, float64(popup.Y)+8+16; got != want {
		t.Fatalf("destination y=%.1f, want %.1f", got, want)
	}

	body := app.uiViewBody(view)
	if body.Y != popup.Y+8 {
		t.Fatalf("body.Y=%.1f, want %.1f (popup.Y + top padding)", body.Y, popup.Y+8)
	}
	if err := app.drawViewBody(nil, view, body, func() error { return nil }); err != nil {
		t.Fatal(err)
	}

	// Concentric inner outline exists when radius (14) > padding (8),
	// and vanishes when padding >= radius.
	inner := sdl.FRect{X: popup.X + 8, Y: popup.Y + 8, W: popup.W - 16, H: popup.H - 16}
	c := &elementCanvas{app: app, view: view}
	outline := c.boxOutline(inner)
	if len(outline) == 0 {
		t.Fatal("expected concentric rounded outline for radius 14 with padding 8")
	}
	for _, pt := range outline {
		if pt.x < inner.X-0.5 || pt.x > inner.X+inner.W+0.5 || pt.y < inner.Y-0.5 || pt.y > inner.Y+inner.H+0.5 {
			t.Fatalf("outline point (%.1f, %.1f) outside inner rect %+v", pt.x, pt.y, inner)
		}
	}

	lp.Radius = config.Opt[config.Corners]{V: config.Corners{8, 8, 8, 8}, Set: true}
	app.styles = nil
	if got := c.boxOutline(inner); got != nil {
		t.Fatalf("expected square inner corners (nil outline) when radius == padding, got %d points", len(got))
	}
}
