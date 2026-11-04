package viewer

import (
	"testing"

	"onon/internal/config"
)

func TestZoomUsesConfiguredBounds(t *testing.T) {
	app := &App{
		tab:    &tab{viewStateFields: viewStateFields{zoom: 1, scale: 1, fitMode: fitManual}},
		config: config.Config{MinZoom: 1, MaxZoom: 8},
	}

	if got := app.clampZoom(0.5); got != 1 {
		t.Fatalf("expected configured minimum zoom, got %.2f", got)
	}
	if got := app.clampZoom(12); got != 8 {
		t.Fatalf("expected configured maximum zoom, got %.2f", got)
	}
}

func TestJumpHistoryMovesBackAndForwardBetweenRecordedPositions(t *testing.T) {
	app := testLayoutApp(5)
	app.winW = 100
	app.winH = 100
	app.recomputeLayout(app.viewportSize())

	app.page = 0
	app.scrollX = 0
	app.scrollY = app.rows[0].y
	app.recordJump()
	app.recordJump()
	if len(app.jumpBack) != 1 {
		t.Fatalf("expected duplicate jump positions to be coalesced, got %d", len(app.jumpBack))
	}

	app.page = 2
	app.scrollX = 0
	app.scrollY = app.rows[2].y
	app.recordJump()
	app.page = 4
	app.scrollX = 0
	app.scrollY = app.rows[4].y

	app.jumpBackward()
	if app.page != 2 || app.scrollX != 0 || app.scrollY != app.rows[2].y || len(app.jumpAhead) != 1 {
		t.Fatalf("expected jump back to restore previous position, page=%d scroll=(%.1f,%.1f) ahead=%d", app.page, app.scrollX, app.scrollY, len(app.jumpAhead))
	}

	app.jumpForward()
	if app.page != 4 || app.scrollX != 0 || app.scrollY != app.rows[4].y {
		t.Fatalf("expected jump forward to restore original position, page=%d scroll=(%.1f,%.1f)", app.page, app.scrollX, app.scrollY)
	}
}

func TestSpreadNavigationAlignsTargetToViewportAnchor(t *testing.T) {
	app := testLayoutApp(6)
	app.winW = 300
	app.winH = 500
	app.dualPage = true
	app.config.AnchorPosition = "center"
	app.recomputeLayout(app.viewportSize())

	if got := app.viewportAnchorRowIndex(); got != 0 {
		t.Fatalf("expected document start to anchor row 0 before navigation, got row %d", got)
	}
	if app.page != app.rows[0].pages[0] {
		t.Fatalf("expected current page to follow start row 0, got page %d", app.page)
	}

	app.nextSpread()
	assertClose(t, app.scrollY, app.scrollYForAnchoredRow(app.rows[1]))
	if app.page != app.rows[1].pages[0] || app.viewportAnchorRowIndex() != 1 {
		t.Fatalf("expected next spread to center row 1, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
	}

	app.prevSpread()
	assertClose(t, app.scrollY, 0)
	if app.page != app.rows[0].pages[0] || app.viewportAnchorRowIndex() != 0 {
		t.Fatalf("expected previous spread to return to row 0, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
	}
}

func TestPageNavigationAlignsTargetToViewportAnchor(t *testing.T) {
	app := testLayoutApp(5)
	app.winW = 300
	app.winH = 500
	app.config.AnchorPosition = "center"
	app.recomputeLayout(app.viewportSize())

	if app.page != 0 {
		t.Fatalf("expected top document edge to keep current page 0 before navigation, got %d", app.page)
	}

	app.nextPage()
	assertClose(t, app.scrollY, app.scrollYForAnchoredRow(app.rows[1]))
	if app.page != 1 || app.viewportAnchorRowIndex() != 1 {
		t.Fatalf("expected next page to center page 1, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
	}

	app.prevPage()
	assertClose(t, app.scrollY, 0)
	if app.page != 0 || app.viewportAnchorRowIndex() != 0 {
		t.Fatalf("expected previous page to return to document start, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
	}
}

func TestPageNavigationAlignsDocumentBoundaryPagesToViewportEdges(t *testing.T) {
	for _, renderMode := range []renderMode{renderContinuous, renderSingle} {
		for _, dualPage := range []bool{false, true} {
			name := renderMode.String()
			if dualPage {
				name += " dual"
			}
			t.Run(name, func(t *testing.T) {
				app := testLayoutApp(5)
				app.winW = 300
				app.winH = 100
				app.renderMode = renderMode
				app.dualPage = dualPage
				app.config.PageGapVertical = 20
				app.recomputeLayout(app.viewportSize())

				app.page = 0
				app.scrollY = 70
				app.prevPage()
				_, top, ok := app.pageScreenOrigin(0)
				if !ok {
					t.Fatal("expected first page screen position")
				}
				assertClose(t, top, 0)

				lastPage := app.pageCount - 1
				app.page = app.anchorPage(lastPage)
				app.recomputeLayout(app.viewportSize())
				lastRow := app.rows[app.pageToRow[lastPage]]
				app.scrollY = lastRow.y
				app.page = app.anchorPage(lastPage)
				app.nextPage()
				_, top, ok = app.pageScreenOrigin(lastPage)
				if !ok {
					t.Fatal("expected last page screen position")
				}
				pageIndex := len(lastRow.pages) - 1
				assertClose(t, top+lastRow.pageH[pageIndex], float64(app.winH))
			})
		}
	}
}

func TestJumpToDocumentEdgesKeepsEdgePageCurrent(t *testing.T) {
	for _, anchor := range []string{"top", "center", "bottom"} {
		t.Run(anchor, func(t *testing.T) {
			app := testLayoutApp(5)
			app.winW = 300
			app.winH = 500
			app.config.AnchorPosition = anchor
			app.recomputeLayout(app.viewportSize())

			app.alignPageToAnchor(app.pageCount - 1)
			if app.page != app.pageCount-1 || app.viewportAnchorRowIndex() != len(app.rows)-1 {
				t.Fatalf("expected last page current at document end, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
			}

			app.alignPageToAnchor(0)
			if app.page != 0 || app.viewportAnchorRowIndex() != 0 {
				t.Fatalf("expected first page current at document start, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
			}
		})
	}
}

func TestBottomAnchorCanPageUpFromClampedTopJump(t *testing.T) {
	app := testLayoutApp(5)
	app.winW = 300
	app.winH = 500
	app.config.AnchorPosition = "bottom"
	app.recomputeLayout(app.viewportSize())

	app.nextPage()
	assertClose(t, app.scrollY, 0)
	if app.page != 1 {
		t.Fatalf("expected clamped jump to keep page 1 as active page, got %d", app.page)
	}

	app.prevPage()
	assertClose(t, app.scrollY, 0)
	if app.page != 0 {
		t.Fatalf("expected PageUp to return to page 0, got %d", app.page)
	}
}

func TestBottomAnchorCanPageUpFromClampedTopSpreadJump(t *testing.T) {
	app := testLayoutApp(6)
	app.winW = 300
	app.winH = 500
	app.dualPage = true
	app.config.AnchorPosition = "bottom"
	app.recomputeLayout(app.viewportSize())

	app.nextSpread()
	assertClose(t, app.scrollY, 0)
	if app.page != app.rows[1].pages[0] || app.rowIndexForPage(app.anchorPage(app.page)) != 1 {
		t.Fatalf("expected clamped jump to keep row 1 as active row, page=%d row=%d", app.page, app.rowIndexForPage(app.anchorPage(app.page)))
	}

	app.prevSpread()
	assertClose(t, app.scrollY, 0)
	if app.page != app.rows[0].pages[0] || app.rowIndexForPage(app.anchorPage(app.page)) != 0 {
		t.Fatalf("expected PageUp to return to row 0, page=%d row=%d", app.page, app.rowIndexForPage(app.anchorPage(app.page)))
	}
}

func TestScrollOnlyUpdatesCurrentPageWhenPositionChanges(t *testing.T) {
	app := testLayoutApp(5)
	app.winW = 300
	app.winH = 500
	app.config.AnchorPosition = "bottom"
	app.recomputeLayout(app.viewportSize())

	app.nextPage()
	assertClose(t, app.scrollY, 0)
	if app.page != 1 {
		t.Fatalf("expected clamped jump to keep page 1 active, got %d", app.page)
	}

	app.scrollBy(0, -64)
	if app.page != 1 {
		t.Fatalf("expected blocked scroll to preserve active page 1, got %d", app.page)
	}

	app.scrollBy(0, 64)
	if app.page == 1 {
		t.Fatalf("expected moving scroll to refresh active page from viewport anchor")
	}
}

func TestJumpAlignmentUsesConfiguredViewportAnchor(t *testing.T) {
	for _, tt := range []struct {
		anchor string
		wantY  float64
	}{
		{anchor: "top", wantY: 460},
		{anchor: "center", wantY: 410},
		{anchor: "bottom", wantY: 360},
	} {
		t.Run(tt.anchor, func(t *testing.T) {
			app := testLayoutApp(5)
			app.winW = 300
			app.winH = 300
			app.config.PageGapVertical = 20
			app.config.AnchorPosition = tt.anchor
			app.recomputeLayout(app.viewportSize())

			app.alignPageToAnchor(2)
			assertClose(t, app.scrollY, tt.wantY)
			if app.page != 2 || app.viewportAnchorRowIndex() != 2 {
				t.Fatalf("expected jump to anchor page 2, page=%d row=%d", app.page, app.viewportAnchorRowIndex())
			}
		})
	}
}

func TestZoomActionsStepByZoomStep(t *testing.T) {
	app := testLayoutApp(1)
	app.winW, app.winH = 1000, 800
	app.fitMode, app.zoom = fitManual, 1
	app.config.MinZoom, app.config.MaxZoom, app.config.ZoomStep = 0.1, 10, 2
	app.recomputeLayout(app.viewportSize())
	app.runAction("zoom_in")
	assertClose(t, app.zoom, 2)
	app.runAction("zoom_out")
	app.runAction("zoom_out")
	assertClose(t, app.zoom, 0.5)
}
