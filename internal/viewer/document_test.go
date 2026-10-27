package viewer

import (
	"os"
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

func TestResolveOpenPathPrefersAFileBesideTheDocument(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "notes:v2.pdf")
	if err := os.WriteFile(want, nil, 0o600); err != nil {
		t.Skip("no colons in file names here:", err)
	}
	app := &App{documentState: documentState{docPath: filepath.Join(dir, "a.pdf")}}
	if got, err := app.resolveOpenPath("notes:v2.pdf"); err != nil || got != want {
		t.Fatalf("resolveOpenPath = %q, %v; want the local %q", got, err, want)
	}
	if got, _ := app.resolveOpenPath("host:c.pdf"); got != "sftp://host/~/c.pdf" {
		t.Fatalf("resolveOpenPath(host:c.pdf) = %q", got)
	}
}
