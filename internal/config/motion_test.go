package config

import (
	"math"
	"reflect"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestParseAnimationText(t *testing.T) {
	for raw, want := range map[string]string{
		"140ms ease-out":                           "140ms ease-out",
		"0.2s":                                     "200ms ease",
		"linear 50":                                "50ms linear",
		`"cubic-bezier(0.2, 0, 0, 1) 300ms"`:       "300ms cubic-bezier(0.2, 0, 0, 1)",
		"none":                                     "none",
		"0ms ease-in":                              "none",
		"fade rise 140ms ease-out":                 "fade rise 140ms ease-out",
		"rise fade 140ms ease-out":                 "fade rise 140ms ease-out",
		"crossfade 80ms":                           "fade 80ms ease",
		"glide 80ms ease-out":                      "glide 80ms ease-out",
		"slide from:left 200ms":                    "slide from:left 200ms ease",
		"opacity:0.5>1 y:50% 1s steps(4)":          "opacity:0.5>1 y:50% 1000ms steps(4)",
		"zoom 200ms spring(170, 26) 40ms":          "zoom 200ms spring(170, 26) delay:40ms",
		"fade 100ms stagger:15ms":                  "fade 100ms ease stagger:15ms",
		"glide stretch:40ms 300ms spring(200, 14)": "glide 300ms spring(200, 14) stretch:40ms",
		"wipe from:top 90ms linear":                "wipe from:top 90ms linear",
		"x:-8 scale:0.9 120ms":                     "x:-8px scale:0.9 120ms ease",
	} {
		raw, err := parseStringOption(raw)
		if err != nil {
			t.Fatal(err)
		}
		got, err := parseAnimationText(raw)
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		if s, _ := animationText(got); s != want {
			t.Errorf("%s = %q, want %q", raw, s, want)
		}
	}
	for _, raw := range []string{"ease-out", "140ms bounce", "140ms ease-in ease-out", "-5ms", "cubic-bezier(2, 0, 0, 1) 1s", "fade 1ms 2ms 3ms", "y:far 1s", "slide from:middle 1s", "steps(0) 1s", "spring(1) 1s"} {
		if _, err := parseAnimationText(raw); err == nil {
			t.Errorf("parseAnimationText(%q) succeeded", raw)
		}
	}
}

func TestEasings(t *testing.T) {
	ease := func(name string) Easing {
		e, err := parseEasingName(name)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	if got := ease("linear").Ease(0.25); math.Abs(got-0.25) > 1e-6 {
		t.Errorf("linear(0.25) = %v", got)
	}
	easeOut := ease("ease-out")
	if easeOut.Ease(0) != 0 || easeOut.Ease(1) != 1 || easeOut.Ease(0.5) <= 0.6 {
		t.Errorf("ease-out: %v %v %v", easeOut.Ease(0), easeOut.Ease(0.5), easeOut.Ease(1))
	}
	// An overshooting curve goes past its target on the way.
	if back := ease("cubic-bezier(0.3, 1.6, 0.6, 1)"); back.Ease(0.6) <= 1 {
		t.Errorf("overshooting curve at 0.6 = %v", back.Ease(0.6))
	}
	steps := ease("steps(4)")
	if steps.Ease(0.2) != 0 || steps.Ease(0.3) != 0.25 || steps.Ease(0.99) != 0.75 || steps.Ease(1) != 1 {
		t.Errorf("steps(4): %v %v %v", steps.Ease(0.2), steps.Ease(0.3), steps.Ease(0.99))
	}
	// A loose spring overshoots and settles; a stiff, damped one does not.
	bouncy, still := ease("spring(200, 8)"), ease("spring(100, 40)")
	peak := 0.0
	for p := 0.0; p < 1; p += 0.01 {
		peak = max(peak, bouncy.Ease(p))
		if still.Ease(p) > 1+1e-9 {
			t.Fatalf("overdamped spring overshoots at %v", p)
		}
	}
	if peak <= 1.1 || math.Abs(bouncy.Ease(0.999)-1) > 0.01 {
		t.Errorf("spring: peak %v, end %v", peak, bouncy.Ease(0.999))
	}
}

func TestTrackValues(t *testing.T) {
	fade := presets["fade"]
	if fade.At(0, true) != 0 || fade.At(1, true) != 1 || fade.At(0, false) != 1 || fade.At(1, false) != 0 {
		t.Error("fade should come in from 0 and go out to 0")
	}
	pair, err := parseTrackText("scale", "0.5>2")
	if err != nil {
		t.Fatal(err)
	}
	if pair.At(0.5, true) != 1.25 || pair.At(0.5, false) != 1.25 {
		t.Errorf("pair at 0.5 = %v", pair.At(0.5, true))
	}
	a := Animation{Duration: 100, Delay: 10, Tracks: []Track{{Prop: "y", Timing: &TrackTiming{Duration: some(300.0), Delay: some(50.0)}}}}
	if a.Length() != 360 {
		t.Errorf("length = %v", a.Length())
	}
}

func TestMotionInTheTheme(t *testing.T) {
	if theme := mustPreset(t, "classic"); theme.Motion.Scale != 0 {
		t.Errorf("classic should not move: %+v", theme.Motion)
	}
	rt := mustLoadThemeTestConfig(t, `
onon.theme.motion = { scale = 2, panel = "1s linear", cursor = { duration = 30 } }
onon.theme.motion.message = { easing = "ease-in" }
assert(onon.theme.motion.panel == "fade 1000ms linear", onon.theme.motion.panel)
`)
	m := rt.Config().Theme.Motion
	panel := m.Moves[MotionPanel][MomentEnter]
	if m.Scale != 2 || panel.Duration != 1000 || panel.Easing.Name != "linear" || len(panel.Tracks) != 1 {
		t.Fatalf("panel = %+v", panel)
	}
	if cursor := m.Moves[MotionCursor][MomentChange]; cursor.Duration != 30 || cursor.Easing.Name != "ease-out" || !cursor.Glide {
		t.Fatalf("cursor = %+v", cursor)
	}
	message := m.Moves[MotionMessage][MomentExit]
	if message.Easing.Name != "ease-in" || message.Duration != defaultMotion.Moves[MotionMessage][MomentExit].Duration {
		t.Fatalf("message = %+v", message)
	}
	if err := rt.SetOption("theme.motion.selection", "250ms ease-in-out"); err != nil {
		t.Fatal(err)
	}
	if got, _ := rt.OptionValue("theme.motion.selection"); got != `"glide 250ms ease-in-out"` {
		t.Fatalf("selection = %s", got)
	}
	if _, err := loadThemeTestConfig(t, `onon.theme.motion.panel = "fast"`); err == nil || !strings.Contains(err.Error(), "unknown word") {
		t.Fatalf("bad animation: %v", err)
	}
	if _, err := loadThemeTestConfig(t, `onon.theme.motion.selection = { enter = "fade" }`); err == nil || !strings.Contains(err.Error(), "selection moves at change") {
		t.Fatalf("bad moment: %v", err)
	}
}

func TestMotionMoments(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme.motion.panel = {
  enter = { opacity = 0, y = "8px", scale = { 0.9, 1, duration = 300, easing = "spring(170, 26)" }, duration = 140 },
  exit = "fade 90ms",
}
onon.theme.motion.rows = { enter = "fade x:-6 120ms stagger:15ms" }
local p = onon.theme.motion.panel
assert(p.exit == "fade 90ms ease", p.exit)
assert(type(p.enter) == "table" and p.enter.scale.duration == 300, "enter")
onon.theme.motion.selection = { change = { easing = function(t) return t * t end, duration = 60, glide = true } }
`)
	m := rt.Config().Theme.Motion.Moves
	enter := m[MotionPanel][MomentEnter]
	if enter.Duration != 140 || len(enter.Tracks) != 3 || enter.Length() != 300 || enter.Glide {
		t.Fatalf("enter = %+v", enter)
	}
	if rows := m[MotionRows][MomentEnter]; rows.Stagger != 15 || len(rows.Tracks) != 2 || m[MotionRows][MomentExit].Duration != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	if sel := m[MotionSelection][MomentChange]; sel.Easing.Func == nil || !sel.Glide || len(sel.Tracks) != 0 {
		t.Fatalf("selection = %+v", sel)
	}
	// The theme still survives a trip through Lua.
	theme := rt.Config().Theme
	cfg := Default()
	got, err := themeFromLua(&cfg, luaThemeTable(rt.state, theme))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Motion, theme.Motion) {
		t.Fatalf("panel after a trip = %+v", got.Motion.Moves[MotionPanel])
	}
}

func TestReducedMotion(t *testing.T) {
	m := defaultMotion
	moves, scale := m.Active(false)
	if !moves[MotionSelection][MomentChange].Glide || scale != 1 {
		t.Fatal("full motion should glide")
	}
	// By default reduced motion keeps only the fades.
	moves, _ = m.Active(true)
	if sel := moves[MotionSelection][MomentChange]; sel.Glide || len(sel.Tracks) != 0 {
		t.Errorf("reduced selection = %+v", sel)
	}
	if panel := moves[MotionPanel][MomentEnter]; len(panel.Tracks) != 1 || panel.Tracks[0].Prop != "opacity" {
		t.Errorf("reduced panel = %+v", panel)
	}
	if moves[MotionEdges][MomentEnter].Duration != 0 {
		t.Error("reduced edges should not move the page")
	}

	rt := mustLoadThemeTestConfig(t, `
onon.theme.motion = {
  panel = "fade rise zoom 140ms ease-out",
  reduced = {
    panel = { enter = "fade 60ms" },
    selection = "crossfade 60ms",
    scale = 0.5,
  },
}
assert(onon.theme.motion.reduced.base == "fade")
assert(onon.theme.motion.reduced.selection == "fade 60ms ease", onon.theme.motion.reduced.selection)
assert(onon.theme.motion.reduced.cursor == nil)
`)
	motion := rt.Config().Theme.Motion
	moves, scale = motion.Active(true)
	if enter := moves[MotionPanel][MomentEnter]; enter.Duration != 60 || len(enter.Tracks) != 1 {
		t.Errorf("reduced panel enter = %+v", enter)
	}
	// What reduced does not set comes from the faded motion.
	if exit := moves[MotionPanel][MomentExit]; exit.Duration != 140 || len(exit.Tracks) != 1 || exit.Tracks[0].Prop != "opacity" {
		t.Errorf("reduced panel exit = %+v", exit)
	}
	if sel := moves[MotionSelection][MomentChange]; sel.Glide || len(sel.Tracks) != 1 || scale != 0.5 {
		t.Errorf("reduced selection = %+v at %v", sel, scale)
	}
	motion.ForceReduced = true
	if moves, _ := motion.Active(false); moves[MotionPanel][MomentEnter].Duration != 60 {
		t.Error("force_reduced should reduce motion")
	}

	rt = mustLoadThemeTestConfig(t, `
onon.theme.motion.reduced = { base = "none", message = "fade 100ms" }
`)
	motion = rt.Config().Theme.Motion
	moves, _ = motion.Active(true)
	if moves[MotionPanel][MomentEnter].Moves() || !moves[MotionMessage][MomentEnter].Moves() {
		t.Errorf("base none: panel %+v message %+v", moves[MotionPanel][MomentEnter], moves[MotionMessage][MomentEnter])
	}
	if err := rt.SetOption("theme.motion.reduced", "full"); err != nil {
		t.Fatal(err)
	}
	motion = rt.Config().Theme.Motion
	if moves, _ := motion.Active(true); !reflect.DeepEqual(moves, motion.Moves) {
		t.Error("reduced = full should leave motion as it is, and drop what was set over it")
	}
}

func TestViewMotion(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	if err := L.DoString(`spec = { enter = "slide 200ms", rows = { enter = "fade stagger:10ms 100ms" } }`); err != nil {
		t.Fatal(err)
	}
	vm, err := ViewMotionFromLua(L, L.GetGlobal("spec"))
	if err != nil {
		t.Fatal(err)
	}
	if vm[MotionPanel][MomentEnter] == nil || vm[MotionPanel][MomentExit] != nil || vm[MotionRows][MomentEnter].Stagger.V != 10 {
		t.Fatalf("view motion = %+v", vm)
	}
	vm, err = ViewMotionFromLua(L, lua.LString("none"))
	if err != nil || !vm[MotionPanel][MomentExit].None {
		t.Fatalf("view motion none = %+v, %v", vm, err)
	}
}
