package viewer

import (
	"testing"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font/basicfont"
)

func TestShiftArrowSelectionUsesKeyboardEventModifiers(t *testing.T) {
	app := &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}
	app.input.Set("hello")
	app.input.SetCursor(3, false)

	left := sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeLeft, Mod: sdl.KeymodShift}
	if !app.handleInputEditKey(&left) {
		t.Fatal("expected Shift+Left to be handled as text selection")
	}
	if got, ok := app.input.SelectedText(); !ok || got != "l" {
		t.Fatalf("expected Shift+Left to select l, got %q, %v", got, ok)
	}

	right := sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeRight, Mod: sdl.KeymodShift}
	if !app.handleInputEditKey(&right) {
		t.Fatal("expected Shift+Right to be handled as text selection")
	}
	if app.input.HasSelection() {
		t.Fatalf("expected Shift+Right back to the anchor to clear selection, got cursor=%d anchor=%d", app.input.Cursor, app.input.Anchor)
	}
}

func TestCtrlShiftArrowExtendsSelectionByWord(t *testing.T) {
	app := &App{tab: &tab{}, inputState: inputState{mode: modeCommand}}
	app.input.Set("one two three")

	e := sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.KeycodeLeft, Mod: sdl.KeymodCtrl | sdl.KeymodShift}
	if !app.handleInputEditKey(&e) {
		t.Fatal("expected Ctrl+Shift+Left to be handled")
	}
	if got, ok := app.input.SelectedText(); !ok || got != "three" {
		t.Fatalf("expected word selection three, got %q, %v", got, ok)
	}
}

func TestMouseDragSelectsStatusBarTextInput(t *testing.T) {
	app := &App{
		tab:         &tab{viewStateFields: viewStateFields{statusBarShown: true}},
		config:      config.Config{Theme: config.Theme{StatusBarPadding: 8}},
		inputState:  inputState{mode: modeCommand},
		sdlState:    sdlState{fontFace: basicfont.Face7x13},
		windowState: windowState{winW: 500, winH: 200},
	}
	app.input.Set("hello")

	startX := float32(8 + measureText(app.fontFace, ":"))
	y := float32(185)
	down := sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), X: startX, Y: y}
	if !app.handleInputMouseButton(&down) {
		t.Fatal("expected mouse down in input to start selection")
	}

	motion := sdl.MouseMotionEvent{Type: sdl.EventMouseMotion, State: sdl.MouseButtonFlags(sdl.ButtonLMask), X: startX + float32(measureText(app.fontFace, "hel")), Y: y}
	if !app.handleInputMouseMotion(&motion) {
		t.Fatal("expected mouse drag to extend input selection")
	}

	up := sdl.MouseButtonEvent{Type: sdl.EventMouseButtonUp, Button: uint8(sdl.ButtonLeft), X: motion.X, Y: y}
	if !app.handleInputMouseButton(&up) {
		t.Fatal("expected mouse up to finish input selection")
	}
	if got, ok := app.input.SelectedText(); !ok || got != "hel" {
		t.Fatalf("expected mouse selection hel, got %q, %v", got, ok)
	}
}
