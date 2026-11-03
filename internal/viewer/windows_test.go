package viewer

import (
	"strings"
	"testing"
)

// twoWindows opens first and second in two windows of one process, as
// macOS opens them, sharing a runtime.
func twoWindows(t *testing.T) (*App, *App) {
	t.Helper()
	first, second := testPages(t, 2), testPages(t, 2)
	a := tabsApp(t, first)
	b := newApp(a.runtime, nil, NewOptions{})
	b.winW, b.winH = 1000, 800
	g := a.windowGroup()
	b.group = g
	g.windows = append(g.windows, b)
	b.initialDocPath = second
	if err := b.openInitialDocument(); err != nil {
		t.Fatal(err)
	}
	g.focus(b)
	return a, b
}

func TestWindowsShareConfigurationChanges(t *testing.T) {
	a, b := twoWindows(t)
	if err := a.runtime.SetOption("scroll_step", "99"); err != nil {
		t.Fatal(err)
	}
	b.applyRuntimeChanges("set")
	if a.pageStep != 99 || b.pageStep != 99 {
		t.Fatalf("scroll_step reached the windows as %v and %v, want 99", a.pageStep, b.pageStep)
	}
}

func TestQuitWarnsAboutUnsavedEditsInAnyWindow(t *testing.T) {
	a, b := twoWindows(t)
	a.markEdited()
	b.runAction("quit")
	if a.quit || b.quit || !strings.Contains(b.message, "1 tab has unsaved changes") {
		t.Fatalf("quit with an edit in the other window: quit %t %t, message %q", a.quit, b.quit, b.message)
	}
	b.runAction("quit")
	if !a.quit || !b.quit {
		t.Fatal("the second quit did not quit both windows")
	}
}

func TestClosingAWindowLeavesTheOthers(t *testing.T) {
	a, b := twoWindows(t)
	g := a.windowGroup()
	if g.documentsOpen() != 2 {
		t.Fatalf("%d documents open, want 2", g.documentsOpen())
	}
	b.runCommand(":q")
	if !b.quit || a.quit {
		t.Fatalf(":q in the second window set quit %t %t, want only it closing", a.quit, b.quit)
	}
	g.closeQuitWindows()
	if len(g.windows) != 1 || g.windows[0] != a || g.focused != a {
		t.Fatal("closing the second window did not leave the first, focused")
	}
}

func TestOpeningADocumentOpenInAnotherWindowLeavesItThere(t *testing.T) {
	a, b := twoWindows(t)
	if err := a.Open(b.docPath); err != nil {
		t.Fatal(err)
	}
	if a.docPath == b.docPath {
		t.Fatal("the document was opened again in the first window")
	}
}
