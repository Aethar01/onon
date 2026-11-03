package viewer

import (
	"testing"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestTitleBarHitTest(t *testing.T) {
	l := newTitleBarLayout(1000, 1.5, true) // 48 high, buttons 69 wide, border 9
	const h = 800
	tests := []struct {
		name string
		x, y float64
		want sdl.HitTestResult
	}{
		{"strip", 400, 20, sdl.HitTestDraggable},
		{"below strip", 400, 48, sdl.HitTestNormal},
		{"close", 990 - 9, 20, sdl.HitTestNormal},
		{"minimize", 1000 - 3*69, 20, sdl.HitTestNormal},
		{"left of minimize", 1000 - 3*69 - 1, 20, sdl.HitTestDraggable},
		{"top edge", 400, 2, sdl.HitTestResizeTop},
		{"top edge over close", 950, 2, sdl.HitTestNormal},
		{"top left", 2, 2, sdl.HitTestResizeTopLeft},
		{"top right", 998, 2, sdl.HitTestResizeTopRight},
		{"left", 2, 400, sdl.HitTestResizeLeft},
		{"right", 998, 400, sdl.HitTestResizeRight},
		{"bottom", 400, 798, sdl.HitTestResizeBottom},
		{"bottom right", 998, 798, sdl.HitTestResizeBottomRight},
	}
	for _, tt := range tests {
		if got := l.hitTest(tt.x, tt.y, h); got != tt.want {
			t.Errorf("%s (%v, %v): got %d, want %d", tt.name, tt.x, tt.y, got, tt.want)
		}
	}
}

func TestTitleBarHitTestMaximizedHasNoResizeEdges(t *testing.T) {
	l := newTitleBarLayout(1000, 1, false)
	if got := l.hitTest(400, 1, 800); got != sdl.HitTestDraggable {
		t.Errorf("top edge: got %d, want draggable", got)
	}
	if got := l.hitTest(1, 400, 800); got != sdl.HitTestNormal {
		t.Errorf("left edge: got %d, want normal", got)
	}
}

func TestTitleBarButtonAt(t *testing.T) {
	l := newTitleBarLayout(1000, 1, true)
	tests := []struct {
		x, y float64
		want titleButton
	}{
		{999, 0, titleButtonClose},
		{954, 31, titleButtonClose},
		{953, 10, titleButtonMaximize},
		{908, 10, titleButtonMaximize},
		{862, 10, titleButtonMinimize},
		{861, 10, titleButtonNone},
		{999, 32, titleButtonNone},
	}
	for _, tt := range tests {
		if got := l.buttonAt(tt.x, tt.y); got != tt.want {
			t.Errorf("(%v, %v): got %d, want %d", tt.x, tt.y, got, tt.want)
		}
	}
}

func TestTitleBarFadesBothWays(t *testing.T) {
	var bar titleBar
	start := time.Now()
	if got := bar.alpha(start); got != 0 {
		t.Fatalf("hidden alpha = %v, want 0", got)
	}
	bar.setShown(true, start)
	if got := bar.alpha(start.Add(titleBarFade / 2)); got < 0.49 || got > 0.51 {
		t.Errorf("half faded in = %v, want 0.5", got)
	}
	// Hiding partway through fades out from where it got to.
	mid := start.Add(titleBarFade / 2)
	bar.setShown(false, mid)
	if got := bar.alpha(mid); got < 0.49 || got > 0.51 {
		t.Errorf("reversed at %v, want 0.5", got)
	}
	if got := bar.alpha(mid.Add(titleBarFade)); got != 0 {
		t.Errorf("faded out = %v, want 0", got)
	}
	if bar.fading(mid.Add(titleBarFade)) {
		t.Error("still fading after the fade")
	}
}

func TestTitleBarPointerRevealsAndHides(t *testing.T) {
	app := &App{tab: &tab{}}
	app.winW, app.winH = 1000, 800
	app.titleBar = &titleBar{}
	app.noteTitleBarPointer(980, 10, true)
	if !app.titleBar.shown || app.titleBar.hovered != titleButtonClose {
		t.Fatalf("in strip: shown=%v hovered=%d", app.titleBar.shown, app.titleBar.hovered)
	}
	app.noteTitleBarPointer(980, 100, true)
	if app.titleBar.shown || app.titleBar.hovered != titleButtonNone {
		t.Fatalf("below strip: shown=%v hovered=%d", app.titleBar.shown, app.titleBar.hovered)
	}
	app.noteTitleBarPointer(500, 10, true)
	app.noteTitleBarPointer(500, 10, false)
	if app.titleBar.shown {
		t.Fatal("still shown after the pointer left the window")
	}
}

func TestTitleBarPressedButtonStaysShown(t *testing.T) {
	app := &App{tab: &tab{}}
	app.winW, app.winH = 1000, 800
	app.titleBar = &titleBar{}
	app.noteTitleBarPointer(880, 10, true)
	down := &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: 880, Y: 10}
	if !app.handleTitleBarButton(down) || app.titleBar.pressed != titleButtonMinimize {
		t.Fatalf("press: pressed=%d", app.titleBar.pressed)
	}
	app.noteTitleBarPointer(880, 300, true)
	if !app.titleBar.shown {
		t.Fatal("hid while pressed")
	}
	// Released away from the button: taken, but does nothing.
	up := &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: 880, Y: 300}
	if !app.handleTitleBarButton(up) || app.titleBar.pressed != titleButtonNone {
		t.Fatalf("release: pressed=%d", app.titleBar.pressed)
	}
}

func TestTitleBarInactiveWithoutOneOrInFullscreen(t *testing.T) {
	app := &App{tab: &tab{}}
	app.winW, app.winH = 1000, 800
	if got := app.titleBarHitTest(400, 10); got != sdl.HitTestNormal {
		t.Errorf("no title bar: got %d", got)
	}
	app.titleBar = &titleBar{}
	app.fullscreen = true
	if got := app.titleBarHitTest(400, 10); got != sdl.HitTestNormal {
		t.Errorf("fullscreen: got %d", got)
	}
	if app.titleBar.shown {
		t.Error("shown in fullscreen")
	}
}
