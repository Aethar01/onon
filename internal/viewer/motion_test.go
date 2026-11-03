package viewer

import (
	"math"
	"runtime"
	"testing"
	"time"

	"onon/internal/config"
)

// stepMotion starts a frame at start plus elapsed.
func stepMotion(app *App, start time.Time, elapsed time.Duration) {
	app.beginMotionFrame()
	app.motion.now = start.Add(elapsed)
}

func TestAnimateMovesToItsTarget(t *testing.T) {
	app := &App{tab: &tab{}}
	app.config.Theme.Motion.Scale = 1
	tr, _ := config.Preset("moss")
	linear := tr.Motion.Panel
	linear.Duration, linear.Curve = 100, [4]float64{0, 0, 1, 1}
	start := time.Now()

	stepMotion(app, start, 0)
	if got := app.animate(tweenKey{kind: "x"}, 10, linear); got != 10 || app.motion.animating {
		t.Fatalf("first frame = %v, animating %v; want the target at once", got, app.motion.animating)
	}
	stepMotion(app, start, 0)
	if got := app.animate(tweenKey{kind: "x"}, 20, linear); got != 10 || !app.motion.animating {
		t.Fatalf("new target starts at %v, animating %v", got, app.motion.animating)
	}
	stepMotion(app, start, 50*time.Millisecond)
	if got := app.animate(tweenKey{kind: "x"}, 20, linear); math.Abs(got-15) > 1e-6 {
		t.Fatalf("halfway = %v, want 15", got)
	}
	// A new target mid-way starts from the last target, so a held key
	// cannot leave the value further and further behind.
	stepMotion(app, start, 50*time.Millisecond)
	if got := app.animate(tweenKey{kind: "x"}, 0, linear); got != 20 {
		t.Fatalf("retarget = %v, want the last target, 20", got)
	}
	stepMotion(app, start, 150*time.Millisecond)
	if got := app.animate(tweenKey{kind: "x"}, 0, linear); got != 0 || app.motion.animating {
		t.Fatalf("done = %v, animating %v", got, app.motion.animating)
	}

	// A value left out of a frame is back at its target when next asked.
	stepMotion(app, start, 200*time.Millisecond)
	stepMotion(app, start, 200*time.Millisecond)
	if got := app.animate(tweenKey{kind: "x"}, 30, linear); got != 30 {
		t.Fatalf("after a frame away = %v, want the target", got)
	}
	// animateFrom starts a new value where it says.
	if got := app.animateFrom(tweenKey{kind: "panel"}, 0, 1, linear); got != 0 {
		t.Fatalf("opening panel = %v, want 0", got)
	}

	// No motion moves straight to the target.
	app.config.Theme.Motion.Scale = 0
	stepMotion(app, start, 300*time.Millisecond)
	if got := app.animate(tweenKey{kind: "x"}, 40, linear); got != 40 || app.motion.animating {
		t.Fatalf("scale 0 = %v, animating %v", got, app.motion.animating)
	}
}

func TestFadeFadesStyleColours(t *testing.T) {
	app := &App{tab: &tab{}}
	clr := config.Color{RGB: [3]uint8{10, 20, 30}, Alpha: 1}
	_ = app.faded(0.5, func() error {
		if got := app.styleColor(clr, 1).A; got != 128 {
			t.Errorf("half faded alpha = %d", got)
		}
		return app.faded(0.5, func() error {
			if got := app.styleColor(clr, 1).A; got != 64 {
				t.Errorf("faded twice alpha = %d", got)
			}
			return nil
		})
	})
	if app.motion.fade != 0 {
		t.Fatalf("fade left at %v", app.motion.fade)
	}
}

func TestOSReducedMotionStopsMotion(t *testing.T) {
	app := &App{tab: &tab{}}
	app.config.Theme.Motion.Scale = 1
	tr, _ := config.Preset("moss")
	app.motion.osReduced.Store(true)
	stepMotion(app, time.Now(), 0)
	app.animate(tweenKey{kind: "x"}, 10, tr.Motion.Selection)
	stepMotion(app, time.Now(), 0)
	if got := app.animate(tweenKey{kind: "x"}, 20, tr.Motion.Selection); got != 20 || app.motion.animating {
		t.Fatalf("with the OS asking for less motion = %v, animating %v", got, app.motion.animating)
	}
}

func TestReducedMotionIsReadAtMostOnceAWhile(t *testing.T) {
	reads := make(chan struct{}, 4)
	defer func(read func() bool) { readOSMotion = read }(readOSMotion)
	readOSMotion = func() bool { reads <- struct{}{}; return true }
	app := &App{tab: &tab{}}
	app.refreshReducedMotion()
	<-reads
	for app.motion.osReading.Load() {
		runtime.Gosched()
	}
	app.refreshReducedMotion() // focused again straight away
	if len(reads) != 0 || !app.motion.osReduced.Load() {
		t.Fatalf("read again within the interval, or lost the first read")
	}
	app.motion.osReadAt = time.Now().Add(-osMotionReadInterval)
	app.refreshReducedMotion()
	<-reads
}
