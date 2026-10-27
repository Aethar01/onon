package viewer

import (
	"os"
	"path/filepath"
	"testing"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/testpdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestQuitWithUnsavedEditsNeedsConfirmation(t *testing.T) {
	app := testLayoutApp(1)
	app.markEdited()
	app.runCommand(":q")
	if app.quit {
		t.Fatal("quit with unsaved edits did not warn first")
	}
	app.runCommand(":q")
	if !app.quit {
		t.Fatal("second :q did not quit")
	}

	app = testLayoutApp(1)
	app.markEdited()
	app.runCommand(":q!")
	if !app.quit {
		t.Fatal(":q! did not quit")
	}
}

func TestWriteSavesCopyAndInPlace(t *testing.T) {
	path := testpdf.Write(t, "hello")
	doc, err := mupdf.Open(path, mupdf.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	app := testLayoutApp(1)
	app.doc, app.docPath, app.docFile = doc, path, path

	copyPath := filepath.Join(filepath.Dir(path), "copy.pdf")
	app.markEdited()
	app.runCommand(":w " + copyPath)
	if _, err := os.Stat(copyPath); err != nil || !app.unsaved {
		t.Fatalf("copy: stat err=%v unsaved=%v (a copy leaves the edits unsaved here)", err, app.unsaved)
	}
	app.runCommand(":w")
	if app.unsaved {
		t.Fatalf("in-place write left unsaved set: %s", app.message)
	}
	if reopened, err := mupdf.Open(path, mupdf.OpenOptions{}); err != nil {
		t.Fatalf("saved file does not open: %v", err)
	} else {
		reopened.Close()
	}
}

func TestUndoTracksSavedState(t *testing.T) {
	app, screenX, lineY := testSelectionApp(t, "select this text")
	app.docPath = t.TempDir() + "/doc.pdf"
	highlight := func() {
		app.handleSDLMouseButton(&sdl.MouseButtonEvent{Type: sdl.EventMouseButtonDown, Button: uint8(sdl.ButtonLeft), Clicks: 2, X: screenX(120), Y: lineY})
		app.config.AnnotationColors = config.Default().AnnotationColors
		app.highlightSelection(0)
	}

	highlight()
	app.savedPos = app.editPos // as if written
	app.unsaved = false
	highlight()
	if !app.unsaved {
		t.Fatal("second highlight not unsaved")
	}
	app.runAction("undo")
	if app.unsaved {
		t.Fatalf("undo back to the saved state still unsaved (%s)", app.message)
	}
	app.runAction("undo")
	if !app.unsaved {
		t.Fatal("undo past the saved state not unsaved")
	}
	highlight() // a new edit here drops the redo history containing the saved state
	app.runAction("undo")
	app.runAction("redo")
	if !app.unsaved {
		t.Fatal("saved state reachable after the redo history was replaced")
	}
	app.runAction("redo")
	if app.message != "nothing to redo" {
		t.Fatalf("redo past the end: %q", app.message)
	}
}
