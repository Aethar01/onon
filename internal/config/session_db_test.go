package config

import (
	"runtime"
	"testing"
)

func TestDocumentSessionRoundTrip(t *testing.T) {
	setTestDataDir(t)
	path := "/tmp/example.pdf"
	want := DocumentSession{
		Page:            4,
		ScrollX:         12.5,
		ScrollY:         99.25,
		AnchorPage:      5,
		AnchorX:         10.25,
		AnchorY:         20.5,
		AnchorValid:     true,
		Zoom:            1.75,
		FitMode:         "manual",
		RenderMode:      "single",
		Rotation:        90,
		DualPage:        true,
		FirstPageOffset: false,
		StatusBarShown:  true,
		AltColors:       true,
	}
	if err := SetDocumentSession(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok := GetDocumentSession(path)
	if !ok {
		t.Fatal("saved document session was not found")
	}
	if got != want {
		t.Fatalf("document session = %#v, want %#v", got, want)
	}
}

func TestSessionDatabaseDefaultOn(t *testing.T) {
	if !Default().SessionDatabase {
		t.Fatal("session database should default to on")
	}
}

func TestRecentFiles(t *testing.T) {
	setTestDataDir(t)
	one := AbsoluteDocumentPath("/tmp/one.pdf")
	two := AbsoluteDocumentPath("/tmp/two.pdf")
	if err := RecordRecentFile("/tmp/one.pdf", 10); err != nil {
		t.Fatal(err)
	}
	if err := RecordRecentFile("/tmp/two.pdf", 10); err != nil {
		t.Fatal(err)
	}
	if err := RecordRecentFile("/tmp/one.pdf", 10); err != nil {
		t.Fatal(err)
	}
	got := RecentFiles(10)
	if len(got) != 2 {
		t.Fatalf("RecentFiles returned %d entries: %v", len(got), got)
	}
	if got[0] != one || got[1] != two {
		t.Fatalf("RecentFiles = %v, want [%s %s]", got, one, two)
	}
}

func TestRecentFilesMaxEntries(t *testing.T) {
	setTestDataDir(t)
	two := AbsoluteDocumentPath("/tmp/two.pdf")
	three := AbsoluteDocumentPath("/tmp/three.pdf")
	if err := RecordRecentFile("/tmp/one.pdf", 2); err != nil {
		t.Fatal(err)
	}
	if err := RecordRecentFile("/tmp/two.pdf", 2); err != nil {
		t.Fatal(err)
	}
	if err := RecordRecentFile("/tmp/three.pdf", 2); err != nil {
		t.Fatal(err)
	}
	got := RecentFiles(10)
	if len(got) != 2 {
		t.Fatalf("RecentFiles returned %d entries: %v", len(got), got)
	}
	if got[0] != three || got[1] != two {
		t.Fatalf("RecentFiles = %v, want [%s %s]", got, three, two)
	}
}

func TestDocumentMarkRoundTrip(t *testing.T) {
	setTestDataDir(t)
	want := DocumentMark{Page: 2, ScrollX: 3, ScrollY: 4, AnchorPage: 5, AnchorX: 6, AnchorY: 7, AnchorValid: true}
	if err := SetDocumentMark("/tmp/doc.pdf", "a", want); err != nil {
		t.Fatal(err)
	}
	got, ok := GetDocumentMark("/tmp/doc.pdf", "a")
	if !ok {
		t.Fatal("saved mark was not found")
	}
	if got != want {
		t.Fatalf("mark = %#v, want %#v", got, want)
	}
}

func setTestDataDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("APPDATA", dir)
	case "darwin":
		t.Setenv("HOME", dir)
	default:
		t.Setenv("XDG_DATA_HOME", dir)
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("XDG_CONFIG_DIRS", "")
		t.Setenv("HOME", dir)
	}
	isolateSystemDir(t)
	t.Cleanup(CloseSessionDatabase)
}

func TestPromptHistoryKeepsNewestDistinctEntries(t *testing.T) {
	setTestDataDir(t)
	for _, entry := range []string{"one", "two", "one", "three"} {
		if err := AddPromptHistory("search", entry, 2); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddPromptHistory("command", "set x=1", 2); err != nil {
		t.Fatal(err)
	}
	if got := PromptHistory("search", 10); len(got) != 2 || got[0] != "three" || got[1] != "one" {
		t.Fatalf("search history = %q, want [three one]", got)
	}
	if got := PromptHistory("command", 10); len(got) != 1 {
		t.Fatalf("command history = %q", got)
	}
}

func TestLastWindowTabsRoundTrip(t *testing.T) {
	setTestDataDir(t)
	want := WindowTabs{Paths: []string{"/tmp/one.pdf", "sftp://host/two.pdf", "/tmp/three.pdf"}, Active: 1}
	if err := SetLastWindowTabs(want); err != nil {
		t.Fatal(err)
	}
	got, ok := LastWindowTabs()
	if !ok || got.Active != 1 || len(got.Paths) != 3 || got.Paths[1] != want.Paths[1] {
		t.Fatalf("LastWindowTabs = %+v, %t, want %+v", got, ok, want)
	}
	// A window closing with one tab leaves nothing to restore.
	if err := SetLastWindowTabs(WindowTabs{Paths: []string{"/tmp/one.pdf"}}); err != nil {
		t.Fatal(err)
	}
	if got, ok := LastWindowTabs(); ok {
		t.Fatalf("LastWindowTabs = %+v after a window with one tab", got)
	}
}
