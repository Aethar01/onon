package viewer

import (
	"testing"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestHighlightPickerAnnotatesSelection(t *testing.T) {
	app, screenX, lineY := testSelectionApp(t, "select this text")
	app.config.AnnotationColors = config.Default().AnnotationColors
	app.highlightSelection(0)
	if app.message != "select text to highlight" || app.unsaved {
		t.Fatalf("highlight without selection: message=%q unsaved=%v", app.message, app.unsaved)
	}

	app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), Clicks: 2, X: screenX(120), Y: lineY})
	app.pickHighlightColor()
	view := app.activeUIView()
	if view == nil || len(view.rows) != 4 || view.rows[1].swatch == nil {
		t.Fatalf("picker = %+v", view)
	}
	app.viewKey(view, &sdl.KeyboardEvent{Type: sdl.EventKeyDown, Key: sdl.Keycode2})

	if !app.unsaved || app.highlightColor != 1 || app.pageRevisions[0] != 1 {
		t.Fatalf("after picking: unsaved=%v colour=%d revision=%d message=%q", app.unsaved, app.highlightColor, app.pageRevisions[0], app.message)
	}
	if !app.selection.empty() || app.activeUIView() != nil {
		t.Fatal("selection or picker left open after highlighting")
	}
}

func TestDeleteAndRecolourAnnotations(t *testing.T) {
	app, screenX, lineY := testSelectionApp(t, "select this text")
	app.config.AnnotationColors = config.Default().AnnotationColors
	app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), Clicks: 2, X: screenX(120), Y: lineY})
	app.highlightSelection(0)

	app.pointer = sdl.FPoint{X: screenX(10), Y: lineY} // off the highlight
	app.runAction("delete_annotation")
	if app.message != "no annotation under the pointer" {
		t.Fatalf("x off the highlight: %q", app.message)
	}
	app.pointer = sdl.FPoint{X: screenX(120), Y: lineY}
	app.runAction("delete_annotation")
	if app.message != "deleted highlight" {
		t.Fatalf("x on the highlight: %q", app.message)
	}
	app.runAction("undo")

	right := &sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonRight), X: screenX(120), Y: lineY}
	app.handleSDLMouseButton(right)
	menu := app.activeUIView()
	if menu == nil || menu.title != "Highlight" {
		t.Fatalf("right-click menu = %+v", menu)
	}
	menu.onSelect(app, menu.rows[1]) // Change colour…
	picker := app.activeUIView()
	if picker == nil || len(picker.rows) != 4 {
		t.Fatalf("colour picker = %+v", picker)
	}
	revision := app.pageRevisions[0]
	picker.onSelect(app, picker.rows[2])
	if app.pageRevisions[0] != revision+1 || app.highlightColor != 2 {
		t.Fatalf("recolour: revision %d→%d colour=%d msg=%q", revision, app.pageRevisions[0], app.highlightColor, app.message)
	}

	app.handleSDLMouseButton(right)
	app.activeUIView().onSelect(app, app.activeUIView().rows[0]) // Delete
	if _, ok := app.annotationAtScreen(float64(screenX(120)), float64(lineY)); ok {
		t.Fatal("annotation left after deleting from the menu")
	}
}
