package viewer

import (
	"path/filepath"
	"testing"
)

func TestResolveOpenPathReturnsAbsolutePath(t *testing.T) {
	want, err := filepath.Abs("test.txt")
	if err != nil {
		t.Fatal(err)
	}

	app := &App{}
	if got, _ := app.resolveOpenPath("test.txt"); got != want {
		t.Fatalf("expected absolute path %q, got %q", want, got)
	}
}

func TestResolveOpenPathUsesCurrentDocumentDirectory(t *testing.T) {
	dir := t.TempDir()
	app := &App{documentState: documentState{docPath: filepath.Join(dir, "paper.pdf")}}
	want := filepath.Join(dir, "other.pdf")

	if got, _ := app.resolveOpenPath("other.pdf"); got != want {
		t.Fatalf("expected path relative to document directory %q, got %q", want, got)
	}
}
