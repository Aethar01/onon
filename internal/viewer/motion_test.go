package viewer

import (
	"math"
	"os"
	"path/filepath"

	"github.com/jupiterrider/purego-sdl3/sdl"
	lua "github.com/yuin/gopher-lua"
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
	linear := config.Animation{Duration: 100, Easing: config.Easing{Name: "linear", Curve: [4]float64{0, 0, 1, 1}}}
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

func TestOSReducedMotionStopsGlidesButKeepsFades(t *testing.T) {
	app := &App{tab: &tab{}}
	theme, _ := config.Preset("moss")
	app.config.Theme = theme
	app.motion.osReduced.Store(true)
	sel := app.anim(config.MotionSelection, config.MomentChange)
	stepMotion(app, time.Now(), 0)
	app.glide(tweenKey{kind: "x"}, 10, sel)
	stepMotion(app, time.Now(), 0)
	if got := app.glide(tweenKey{kind: "x"}, 20, sel); got != 20 || app.motion.animating {
		t.Fatalf("with the OS asking for less motion = %v, animating %v", got, app.motion.animating)
	}
	start := time.Now()
	stepMotion(app, start, 0)
	panel := app.anim(config.MotionPanel, config.MomentEnter)
	if l, done := app.layerAt(panel, panel.Duration/2, true, layerCtx{}); done || l.opacity <= 0 || l.opacity >= 1 {
		t.Fatalf("reduced panel half way = %+v, done %v; want it fading", l, done)
	}
	// Forced reduced motion is the same without the OS.
	app.motion.osReduced.Store(false)
	app.config.Theme.Motion.ForceReduced = true
	if app.anim(config.MotionSelection, config.MomentChange).Glide {
		t.Fatal("force_reduced should stop the glide")
	}
}

func TestLayerTracks(t *testing.T) {
	app := &App{tab: &tab{}, winW: 800, winH: 600}
	app.config.Theme.Motion.Scale = 1
	stepMotion(app, time.Now(), 0)
	p, err := config.AnimationFromLua(lua.LString("fade slide x:50% scale:0.5 100ms linear"))
	if err != nil {
		t.Fatal(err)
	}
	anim := p.Apply(config.Animation{})
	ctx := layerCtx{rect: sdl.FRect{X: 100, Y: 100, W: 200, H: 100}, side: "bottom", auto: 500}
	in, done := app.layerAt(anim, 50, true, ctx)
	if done || math.Abs(in.opacity-0.5) > 1e-6 || math.Abs(in.scale-0.75) > 1e-6 || math.Abs(in.dx-50) > 1e-4 || math.Abs(in.dy-250) > 1e-4 {
		t.Fatalf("half way in = %+v", in)
	}
	out, _ := app.layerAt(anim, 25, false, ctx)
	if math.Abs(out.opacity-0.75) > 1e-6 || math.Abs(out.dy-125) > 1e-4 {
		t.Fatalf("a quarter out = %+v", out)
	}
	if l, done := app.layerAt(anim, 100, true, ctx); !done || l != restLayer {
		t.Fatalf("in = %+v, done %v", l, done)
	}
	// Stagger and track timing: a track waits for its delay.
	p, _ = config.AnimationFromLua(lua.LString("fade 100ms linear delay:40ms"))
	if l, _ := app.layerAt(p.Apply(config.Animation{}), 20, true, ctx); l.opacity != 0 {
		t.Fatalf("before the delay = %+v", l)
	}
}

func TestChangeLayersCrossfade(t *testing.T) {
	app := &App{tab: &tab{}}
	app.config.Theme.Motion.Scale = 1
	p, _ := config.AnimationFromLua(lua.LString("crossfade 100ms linear"))
	anim := p.Apply(config.Animation{})
	key := tweenKey{kind: "sel"}
	start := time.Now()
	stepMotion(app, start, 0)
	if in, _, _, leaving := app.changeLayers(key, 1, anim, layerCtx{}); in != restLayer || leaving {
		t.Fatal("the first selection should not move")
	}
	stepMotion(app, start, 10*time.Millisecond)
	app.changeLayers(key, 2, anim, layerCtx{})
	stepMotion(app, start, 60*time.Millisecond)
	in, prev, out, leaving := app.changeLayers(key, 2, anim, layerCtx{})
	if prev != 1 || !leaving || math.Abs(in.opacity-0.5) > 1e-6 || math.Abs(out.opacity-0.5) > 1e-6 {
		t.Fatalf("mid crossfade: in %+v, out %+v, prev %d, leaving %v", in, out, prev, leaving)
	}
	stepMotion(app, start, 200*time.Millisecond)
	if _, _, _, leaving := app.changeLayers(key, 2, anim, layerCtx{}); leaving {
		t.Fatal("the old selection should be gone")
	}
	// A glide moves the one box, and nothing leaves.
	p, _ = config.AnimationFromLua(lua.LString("glide fade 100ms"))
	stepMotion(app, start, 300*time.Millisecond)
	app.changeLayers(key, 3, p.Apply(config.Animation{}), layerCtx{})
	stepMotion(app, start, 310*time.Millisecond)
	if _, _, _, leaving := app.changeLayers(key, 3, p.Apply(config.Animation{}), layerCtx{}); leaving {
		t.Fatal("a glide leaves nothing behind")
	}
}

func TestLuaEasingAndViewMotion(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.lua")
	source := `
onon.theme.motion.panel = { enter = { opacity = 0, duration = 100, easing = function(t) return t * t end } }
bind("u", function()
  onon.ui.create({ id = "moving", rows = { "a", "b" }, motion = { exit = "none", rows = "fade 50ms stagger:10ms" } }):show()
end)
`
	if err := os.WriteFile(configPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := config.Open(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app := testLayoutApp(0)
	app.runtime = rt
	app.config = rt.Config()
	rt.AttachHost(app)
	anim := app.anim(config.MotionPanel, config.MomentEnter)
	if got := app.ease(anim.Easing, 0.5); math.Abs(got-0.25) > 1e-9 {
		t.Fatalf("Lua easing at 0.5 = %v, want 0.25", got)
	}
	if handled, _, err := rt.RunAction(rt.Config().KeyBindings["u"]); !handled || err != nil {
		t.Fatalf("show view: handled=%v err=%v", handled, err)
	}
	view := app.views.views["lua:moving:1"]
	if view == nil || view.motion == nil {
		t.Fatalf("the view's motion was lost: %v %v", view, app.views.views)
	}
	if app.viewAnim(view, config.MotionPanel, config.MomentExit).Moves() || !app.viewAnim(view, config.MotionPanel, config.MomentEnter).Moves() {
		t.Fatal("the view's exit should be none, its enter the theme's")
	}
	if rows := app.viewAnim(view, config.MotionRows, config.MomentEnter); rows.Stagger != 10 || rows.Duration != 50 {
		t.Fatalf("the view's rows = %+v", rows)
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
