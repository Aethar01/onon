package viewer

import (
	"testing"
	"time"

	"onon/internal/mupdf"
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
	// Drawn, the panel asks for the page's tiles at once, and keeps them
	// wanted.
	if err := app.drawViewBody(nil, view, app.uiViewBody(view), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	requested := false
	for key, req := range app.renderPending {
		requested = requested || key.page == 3 && req.priority == 0
	}
	if !requested {
		t.Fatalf("no on-screen tiles requested for the previewed page: %v", app.renderPending)
	}
	app.prefetchVisiblePages()
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
