package config

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestScrollOptionDefaults(t *testing.T) {
	cfg := Default()
	if cfg.InvertScrollSources != 0 {
		t.Fatalf("expected invert_scroll to default to no sources, got %q", FormatSmoothInputSources(cfg.InvertScrollSources))
	}
	if cfg.SmoothScrollSources != SmoothInputAll {
		t.Fatalf("expected smooth_scroll to default to all sources, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}
	if cfg.SmoothZoomSources != SmoothInputAll {
		t.Fatalf("expected smooth_zoom to default to all sources, got %q", FormatSmoothInputSources(cfg.SmoothZoomSources))
	}
	if cfg.SmoothScrollDampening != 0.8 {
		t.Fatalf("expected smooth_scroll_dampening=0.8, got %v", cfg.SmoothScrollDampening)
	}
	if cfg.SmoothZoomDampening != 0.8 {
		t.Fatalf("expected smooth_zoom_dampening=0.8, got %v", cfg.SmoothZoomDampening)
	}
	if cfg.AnimationFrameMS != 0 {
		t.Fatalf("expected animation_frame_ms=0, following the display, got %d", cfg.AnimationFrameMS)
	}
}

func TestAnimationFrameOptionIsClamped(t *testing.T) {
	cfg := Default()
	desc := configOptions["animation_frame_ms"]

	if err := desc.applyText(&cfg, "24"); err != nil {
		t.Fatal(err)
	}
	if cfg.AnimationFrameMS != 24 {
		t.Fatalf("expected animation frame=24, got %d", cfg.AnimationFrameMS)
	}

	if err := desc.applyText(&cfg, "-3"); err != nil {
		t.Fatal(err)
	}
	if cfg.AnimationFrameMS != 0 {
		t.Fatalf("expected a negative animation frame to follow the display, got %d", cfg.AnimationFrameMS)
	}
}

func TestSmoothScrollOptionCanBeDisabled(t *testing.T) {
	cfg := Default()
	desc := configOptions["smooth_scroll"]

	if err := desc.applyText(&cfg, `{ mouse = false, trackpad = false, keyboard = false }`); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothScrollSources != 0 {
		t.Fatalf("expected smooth_scroll to be disabled, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}

	if err := desc.applyText(&cfg, `{ mouse = true, trackpad = true, keyboard = true }`); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothScrollSources != SmoothInputAll {
		t.Fatalf("expected smooth_scroll to be enabled for all sources, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}
}

func TestSmoothScrollOptionSupportsCombinations(t *testing.T) {
	cfg := Default()
	desc := configOptions["smooth_scroll"]

	if err := desc.applyText(&cfg, `{ mouse = true, trackpad = false, keyboard = true }`); err != nil {
		t.Fatal(err)
	}
	want := SmoothInputMouse | SmoothInputKeyboard
	if cfg.SmoothScrollSources != want {
		t.Fatalf("expected mouse and keyboard sources, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}
	if got := desc.format(&cfg); got != `{ mouse = true, trackpad = false, keyboard = true }` {
		t.Fatalf("expected canonical source format, got %s", got)
	}
}

func TestSmoothScrollOptionAcceptsLuaTable(t *testing.T) {
	cfg := Default()
	desc := configOptions["smooth_scroll"]
	L := lua.NewState()
	defer L.Close()
	table := L.NewTable()
	table.RawSetString("mouse", lua.LFalse)

	if err := desc.apply(&cfg, table); err != nil {
		t.Fatal(err)
	}
	want := SmoothInputTrackpad | SmoothInputKeyboard
	if cfg.SmoothScrollSources != want {
		t.Fatalf("expected only mouse to be disabled, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}
}

func TestSmoothScrollDampeningOptionIsClamped(t *testing.T) {
	cfg := Default()
	desc := configOptions["smooth_scroll_dampening"]

	if err := desc.applyText(&cfg, "0.6"); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothScrollDampening != 0.6 {
		t.Fatalf("expected dampening=0.6, got %v", cfg.SmoothScrollDampening)
	}

	if err := desc.applyText(&cfg, "2"); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothScrollDampening != 1 {
		t.Fatalf("expected upper clamp=1, got %v", cfg.SmoothScrollDampening)
	}

	if err := desc.applyText(&cfg, "0"); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothScrollDampening != 0.01 {
		t.Fatalf("expected lower clamp=0.01, got %v", cfg.SmoothScrollDampening)
	}
}

func TestSmoothZoomOptionUsesIndependentSourcesAndDampening(t *testing.T) {
	cfg := Default()
	sources := configOptions["smooth_zoom"]
	if err := sources.applyText(&cfg, `{ trackpad = false }`); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothZoomSources != SmoothInputMouse|SmoothInputKeyboard {
		t.Fatalf("expected only trackpad zoom smoothing to be disabled, got %q", FormatSmoothInputSources(cfg.SmoothZoomSources))
	}

	dampening := configOptions["smooth_zoom_dampening"]
	if err := dampening.applyText(&cfg, "0.6"); err != nil {
		t.Fatal(err)
	}
	if cfg.SmoothZoomDampening != 0.6 {
		t.Fatalf("expected smooth_zoom_dampening=0.6, got %v", cfg.SmoothZoomDampening)
	}
}
