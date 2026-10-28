package viewer

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// testListApp shows a list of n rows.
func testListApp(n int) (*App, *uiView) {
	app := testStyleApp("bar")
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("row %d", i)
	}
	app.showCoreList("test", "Test", rows, nil)
	return app, app.activeModalUIView()
}

func TestListOffsetFollowsScroll(t *testing.T) {
	app, view := testListApp(100)
	_, rows := view.frameGeometry(app)
	start := time.Now()
	app.motion.now = start
	if got := app.listOffset(view, rows, 100); got != 0 {
		t.Fatalf("first offset = %v", got)
	}

	// The selection moving out of view scrolls the list smoothly.
	app.moveUIViewSelection(view, rows+2)
	target := float64(view.scroll)
	app.motion.now = start.Add(8 * time.Millisecond)
	mid := app.listOffset(view, rows, 100)
	if mid <= 0 || mid >= target || !app.motion.animating {
		t.Fatalf("offset part-way = %v toward %v, animating %v", mid, target, app.motion.animating)
	}
	app.motion.now = start.Add(time.Second)
	if got := app.listOffset(view, rows, 100); got != target {
		t.Fatalf("offset settled at %v, want %v", got, target)
	}

	// Without keyboard smooth scrolling it jumps.
	app.config.SmoothScrollSources = 0
	app.moveUIViewSelection(view, -rows-2)
	if got := app.listOffset(view, rows, 100); got != float64(view.scroll) {
		t.Fatalf("offset = %v, want a jump to %d", got, view.scroll)
	}
}

func TestListRestsWhereScrolledTo(t *testing.T) {
	app, view := testListApp(100)
	_, rows := view.frameGeometry(app)
	app.listOffset(view, rows, 100)
	app.scrollListBy(view, 2.4)
	if view.offset != 2.4 || view.scroll != 2 {
		t.Fatalf("offset %v scroll %d after scrolling 2.4 rows", view.offset, view.scroll)
	}
	app.motion.now = time.Now().Add(time.Second)
	if got := app.listOffset(view, rows, 100); got != 2.4 {
		t.Fatalf("a swipe's resting place moved to %v", got)
	}
	// The row under the pointer is the one drawn there.
	rect, _ := view.frameGeometry(app)
	rowHeight := app.modalListRowHeight()
	top := int(rect.Y) + rowHeight
	if row, ok := app.uiViewIndexAt(view, int(rect.X)+20, top+1); !ok || row.index != 2 {
		t.Fatalf("top of the list = row %d, %v; want 2, part-hidden", row.index, ok)
	}
	if row, ok := app.uiViewIndexAt(view, int(rect.X)+20, top+int(math.Ceil(0.6*float64(rowHeight)))+1); !ok || row.index != 3 {
		t.Fatalf("just past the part-hidden row = row %d, %v; want 3", row.index, ok)
	}
	// The keyboard settles it on a whole row.
	app.moveUIViewSelection(view, 3)
	app.config.SmoothScrollSources = 0
	if got := app.listOffset(view, rows, 100); got != math.Round(got) {
		t.Fatalf("offset after a key = %v, want a whole row", got)
	}
	// Scrolling clamps to the list.
	app.scrollListBy(view, 1000)
	if want := float64(100 - rows); view.offset != want {
		t.Fatalf("offset past the end = %v, want %v", view.offset, want)
	}
}

func TestWheelGlidesAList(t *testing.T) {
	app, view := testListApp(100)
	_, rows := view.frameGeometry(app)
	app.listOffset(view, rows, 100)
	app.queueModalSmoothScroll(view, 3)
	if !app.advanceSmoothScrollBy(16 * time.Millisecond) {
		t.Fatal("the wheel did not move the list")
	}
	if view.offset <= 0 || view.offset >= 3 {
		t.Fatalf("offset after one frame = %v", view.offset)
	}
	for range 100 {
		app.advanceSmoothScrollBy(16 * time.Millisecond)
	}
	if view.offset != 3 || app.smoothScrollActive() {
		t.Fatalf("offset = %v, still active %v", view.offset, app.smoothScrollActive())
	}
	// A key mid-glide takes the list over.
	app.queueModalSmoothScroll(view, 2)
	app.advanceSmoothScrollBy(16 * time.Millisecond)
	app.moveUIViewSelection(view, 1)
	app.advanceSmoothScrollBy(16 * time.Millisecond)
	if app.smoothScrollActive() {
		t.Fatal("the wheel kept scrolling after a key")
	}
}

func TestLeftButtonStaysTheListsWhenBoundToPan(t *testing.T) {
	app, view := testListApp(100)
	app.mouseBindings = map[string]string{"left_down": "pan", "middle_down": "pan"}

	// The middle button pans the list.
	app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonMiddle)})
	if !app.panning {
		t.Fatal("the middle button did not pan the list")
	}
	app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonMiddle)})
	if app.panning {
		t.Fatal("releasing the middle button did not stop the pan")
	}

	// The left button, clicked outside the list, closes it.
	rect, _ := view.frameGeometry(app)
	app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: rect.X + rect.W + 5, Y: rect.Y + rect.H + 5})
	if app.panning || app.activeModalUIView() != nil {
		t.Fatalf("a left click outside the list panned %v, left it open %v", app.panning, app.activeModalUIView() != nil)
	}
}

func TestWheelWithoutSmoothScrollingMovesAWholeRow(t *testing.T) {
	app, view := testListApp(100)
	_, rows := view.frameGeometry(app)
	app.listOffset(view, rows, 100)
	app.scrollListTo(view, 5)
	for _, step := range []struct {
		y    float32
		want float64
	}{{-0.1, 6}, {-6, 7}, {2.5, 6}} {
		app.handleSDLMouseWheel(&sdl.MouseWheelEvent{Y: step.y})
		if view.offset != step.want || view.scroll != int(step.want) {
			t.Fatalf("after a wheel of %v: offset %v scroll %d, want %v", step.y, view.offset, view.scroll, step.want)
		}
	}
}

func TestMenusFitTheirRows(t *testing.T) {
	app := testStyleApp("bar")
	app.winW, app.winH = 2000, 800
	app.showRowList("test", "Test", []uiRow{{text: "short", secondary: "12"}}, 70, 70, func(*uiView) {})
	view := app.activeModalUIView()
	narrow, _ := view.frameGeometry(app)
	if narrow.W >= 1400 || narrow.W < 260 {
		t.Fatalf("a menu of one short row is %v wide", narrow.W)
	}

	// A longer row widens it, up to the share of the window it may take.
	app.setUIViewRows(view, []uiRow{{text: strings.Repeat("long ", 20), secondary: "12"}})
	wider, _ := view.frameGeometry(app)
	if wider.W <= narrow.W {
		t.Fatalf("a longer row left it %v wide, from %v", wider.W, narrow.W)
	}
	app.setUIViewRows(view, []uiRow{{text: strings.Repeat("long ", 400)}})
	if full, _ := view.frameGeometry(app); full.W != 1400 {
		t.Fatalf("a menu wider than its share is %v wide, want 1400", full.W)
	}

	// Filtering keeps its width.
	app.setUIViewRows(view, []uiRow{{text: "short"}, {text: strings.Repeat("long ", 20)}})
	before, _ := view.frameGeometry(app)
	app.setUIViewQuery(view, "short")
	if after, _ := view.frameGeometry(app); after.W != before.W {
		t.Fatalf("filtering changed its width from %v to %v", before.W, after.W)
	}
}

func TestMenusOptionPlacesOnonsMenus(t *testing.T) {
	app := testStyleApp("bar")
	app.winW, app.winH = 1000, 800
	app.config.Menus = map[string]config.MenuPlacement{"help": {Width: config.UISize{Value: 30, Percent: true}, Anchor: "top"}}
	app.toggleHelp()
	view := app.activeModalUIView()
	rect, _ := view.frameGeometry(app)
	if rect.W > 300 || view.place.anchor != "top" || rect.Y > 20 {
		t.Fatalf("help placed at %+v, anchor %q", rect, view.place.anchor)
	}
	// Menus not named keep their own place.
	app.showRowList("themes", "Themes", []uiRow{{text: "moss"}}, 50, 60, func(*uiView) {})
	if view := app.activeModalUIView(); view.place.anchor != "center" {
		t.Fatalf("themes anchored %q", view.place.anchor)
	}
	for id := range menuNames {
		if !slices.Contains(config.MenuNames, menuNames[id]) {
			t.Errorf("menu %s is named %q, which the menus option does not know", id, menuNames[id])
		}
	}
	if len(menuNames) != len(config.MenuNames) {
		t.Errorf("%d menus named, the option knows %d", len(menuNames), len(config.MenuNames))
	}
}
