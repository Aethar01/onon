package viewer

import (
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestRepeatedArrowKeyRunsCountableBinding(t *testing.T) {
	app := testLayoutApp(5)
	app.config.SmoothScrollSources = 0
	app.pageStep = 64
	app.setKeyBindings(map[string]string{
		"<Up>": "scroll_up",
	})
	app.recomputeLayout(1000, 100)
	app.scrollY = 192

	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeUp})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeUp, Repeat: true})

	assertClose(t, app.scrollY, 64)
}

func TestRepeatedKeyDoesNotExpandMultiKeySequence(t *testing.T) {
	app := testLayoutApp(5)
	app.setKeyBindings(map[string]string{
		"gg": "first_page",
	})

	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeG})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeG, Repeat: true})

	if got := len(app.sequence); got != 1 {
		t.Fatalf("expected one pending sequence token, got %d (%v)", got, app.sequence)
	}
}

func TestCountableKeyContinuesAPendingSequence(t *testing.T) {
	app := testLayoutApp(5)
	app.setKeyBindings(map[string]string{"gr": "toggle_dual_page", "r": "rotate_cw", "gg": "first_page"})
	dual := app.dualPage

	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeG})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeR})
	if app.rotation != 0 || app.dualPage == dual || len(app.sequence) != 0 {
		t.Fatalf("g r rotated to %v, dual page %v, sequence %v: want gr run", app.rotation, app.dualPage, app.sequence)
	}
	// Alone, r still runs at once.
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeR})
	if app.rotation != 90 {
		t.Fatalf("r alone rotated to %v, want 90", app.rotation)
	}
}

func TestSmoothScrollQueuesTargetWithoutImmediateJump(t *testing.T) {
	app := testLayoutApp(5)
	app.config.SmoothScrollDampening = 0.35
	app.recomputeLayout(1000, 100)
	app.scrollY = 100
	defer app.cancelSmoothScroll()

	app.queueSmoothScroll(0, 64)

	assertClose(t, app.scrollY, 100)
	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected smooth scroll to become active")
	}
	assertClose(t, state.targetY, 164)

	if !app.advanceSmoothScrollBy(smoothAnimationFrame) {
		t.Fatal("expected first animation frame to move the viewport")
	}
	assertClose(t, app.scrollY, 122.4)
}

func TestKeyboardScrollQueuesSmoothTarget(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollDampening = 0.35
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	if err := app.runBuiltinAction("scroll_down"); err != nil {
		t.Fatal(err)
	}
	assertClose(t, app.scrollY, 100)
	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected keyboard scroll target")
	}
	assertClose(t, state.targetY, 164)

	if !app.advanceSmoothScrollBy(smoothAnimationFrame) {
		t.Fatal("expected keyboard scroll animation frame to move the viewport")
	}
	assertClose(t, app.scrollY, 122.4)
}

func TestKeyboardScrollCanBeDisabledIndependently(t *testing.T) {
	app := testLayoutApp(5)
	app.pageStep = 64
	app.config.SmoothScrollSources = config.SmoothInputMouse
	app.recomputeLayout(1000, 100)
	app.scrollY = 100

	if err := app.runBuiltinAction("scroll_down"); err != nil {
		t.Fatal(err)
	}
	assertClose(t, app.scrollY, 164)
	if app.smoothScrollActive() {
		t.Fatal("expected disabled keyboard smoothing to remain immediate")
	}
}

func TestSmoothScrollAccumulatesWheelBurstIntoOneTarget(t *testing.T) {
	app := testLayoutApp(5)
	app.config.SmoothScrollDampening = 0.35
	app.recomputeLayout(1000, 100)
	app.scrollY = 100
	defer app.cancelSmoothScroll()

	app.queueSmoothScroll(0, 16)
	app.queueSmoothScroll(0, 24)
	app.queueSmoothScroll(0, 8)

	assertClose(t, app.scrollY, 100)
	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected smooth scroll to become active")
	}
	assertClose(t, state.targetY, 148)

	app.advanceSmoothScrollBy(smoothAnimationFrame)
	assertClose(t, app.scrollY, 116.8)
}

func TestDirectNavigationCancelsPendingSmoothScroll(t *testing.T) {
	app := testLayoutApp(5)
	app.config.SmoothScrollDampening = 0.35
	app.recomputeLayout(1000, 100)
	app.scrollY = 100
	defer app.cancelSmoothScroll()

	app.queueSmoothScroll(0, 64)
	app.advanceSmoothScrollBy(smoothAnimationFrame)
	app.scrollBy(0, 10)
	position := app.scrollY

	if app.advanceSmoothScrollBy(smoothAnimationFrame) {
		t.Fatal("expected direct navigation to cancel the pending wheel target")
	}
	if app.smoothScrollActive() {
		t.Fatal("expected smooth scroll state to be cleared")
	}
	assertClose(t, app.scrollY, position)
}

func TestSmoothWheelOnlyHandlesDefaultScrollBindings(t *testing.T) {
	app := testLayoutApp(5)
	app.mouseBindings = map[string]string{
		"wheel_up":    "scroll_up",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}

	if !app.canSmoothWheel(0, 0.5) {
		t.Fatal("expected default wheel binding to use smooth scrolling")
	}

	app.mouseBindings["wheel_up"] = "next_page"
	if app.canSmoothWheel(0, 0.5) {
		t.Fatal("expected custom wheel binding to remain discrete")
	}
}

func TestDiscreteWheelDoesNotReenterSmoothScrolling(t *testing.T) {
	app := testLayoutApp(5)
	app.mouseBindings = map[string]string{
		"wheel_up":    "next_page",
		"wheel_down":  "scroll_down",
		"wheel_left":  "scroll_left",
		"wheel_right": "scroll_right",
	}
	app.recomputeLayout(1000, 100)

	app.handleMouseWheel(&sdl.MouseWheelEvent{Y: 1})

	if app.smoothScrollActive() {
		t.Fatal("expected custom wheel binding to stay on the discrete path")
	}
	if app.page != 1 {
		t.Fatalf("expected custom wheel binding to advance page, got %d", app.page)
	}
}

func TestSmoothTowardUsesConfiguredDampening(t *testing.T) {
	assertClose(t, smoothToward(0, 1, 0.35, smoothAnimationFrame, smoothAnimationFrame), 0.35)
	assertClose(t, smoothToward(10, 20, 0.5, smoothAnimationFrame, smoothAnimationFrame), 15)
}

func TestSmoothAnimationUsesConfiguredFrame(t *testing.T) {
	app := &App{tab: &tab{}, config: config.Config{AnimationFrameMS: 32}}
	if got := app.animationFrameDuration(); got != 32*time.Millisecond {
		t.Fatalf("expected configured animation frame, got %v", got)
	}
	assertClose(t, smoothToward(0, 1, 0.35, 32*time.Millisecond, app.animationFrameDuration()), 0.35)
}

func TestSmoothTowardNormalizesForElapsedTime(t *testing.T) {
	oneFrame := smoothToward(0, 1, 0.35, smoothAnimationFrame, smoothAnimationFrame)
	twoFrames := smoothToward(0, 1, 0.35, 2*smoothAnimationFrame, smoothAnimationFrame)
	steppedTwice := smoothToward(oneFrame, 1, 0.35, smoothAnimationFrame, smoothAnimationFrame)

	assertClose(t, twoFrames, steppedTwice)
}

func TestSmoothTowardClampsDampening(t *testing.T) {
	assertClose(t, smoothToward(0, 1, 2, smoothAnimationFrame, smoothAnimationFrame), 1)
	assertClose(t, smoothToward(0, 1, 0, smoothAnimationFrame, smoothAnimationFrame), 0.01)
}

func TestMetricRelayoutKeepsPendingSmoothScrollDistance(t *testing.T) {
	app := testLayoutApp(5)
	app.config.SmoothScrollDampening = 0.35
	app.recomputeLayout(1000, 100)
	app.scrollY = 500
	defer app.cancelSmoothScroll()

	app.queueSmoothScroll(0, 100)
	app.advanceSmoothScrollBy(smoothAnimationFrame)
	remaining := app.smoothScrollState().targetY - app.scrollY

	// A page above the viewport grows once its real size arrives, so the
	// anchor restore shifts scrollY to keep the same content in view.
	app.metricLoader = &metricLoader{updates: make(chan pageMetricUpdate, 1)}
	app.metricLoader.updates <- pageMetricUpdate{page: 0, metrics: newPageMetrics(mupdf.PageInfo{Bounds: mupdf.Rect{X1: 100, Y1: 300}})}
	oldY := app.scrollY
	app.pollMetricUpdates()
	if app.scrollY == oldY {
		t.Fatal("expected metric relayout to move the viewport")
	}

	shiftedY := app.scrollY
	state := app.smoothScrollState()
	if state == nil {
		t.Fatal("expected metric relayout to keep the smooth scroll active")
	}
	assertClose(t, state.targetY-shiftedY, remaining)
	for i := 0; i < 100 && app.smoothScrollActive(); i++ {
		app.advanceSmoothScrollBy(smoothAnimationFrame)
	}
	assertClose(t, app.scrollY, shiftedY+remaining)
}

func TestHeldAndCountedRotate(t *testing.T) {
	app := testLayoutApp(5)
	app.setKeyBindings(map[string]string{"r": "rotate_cw"})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeR})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeR, Repeat: true})
	if app.rotation != 180 {
		t.Fatalf("holding rotate_cw gave %v degrees, want 180", app.rotation)
	}
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.Keycode2})
	app.handleSDLKeyDown(&sdl.KeyboardEvent{Key: sdl.KeycodeR})
	if app.rotation != 0 {
		t.Fatalf("2r from 180 gave %v degrees, want 0", app.rotation)
	}
}
