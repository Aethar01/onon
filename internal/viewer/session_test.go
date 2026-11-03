package viewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/testpdf"
)

func TestDocumentSessionPollIsNonBlocking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s documentSession
	s.record(path)
	defer s.Close()

	start := time.Now()
	if _, ok := s.poll(start); ok {
		t.Fatal("expected no change without modification")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("poll blocked for %s", elapsed)
	}
}

// waitForDocumentChange polls s, as the event loop does, until the watcher
// reports a change once its debounce has passed.
func waitForDocumentChange(t *testing.T, s *documentSession) documentChange {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if change, ok := s.poll(time.Now()); ok {
			return change
		}
	}
	t.Fatal("no change reported after the file was modified")
	return documentChange{}
}

func testDocumentSession(t *testing.T) (*documentSession, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &documentSession{}
	s.record(path) // watches the directory before returning
	t.Cleanup(s.Close)
	return s, path
}

func TestDocumentSessionDetectsChanges(t *testing.T) {
	s, path := testDocumentSession(t)
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.commit(waitForDocumentChange(t, s))
	if _, ok := s.poll(time.Now()); ok {
		t.Fatal("expected no change after commit")
	}
}

func TestDocumentSessionRateLimitsRetries(t *testing.T) {
	s, path := testDocumentSession(t)
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitForDocumentChange(t, s)
	if _, ok := s.poll(time.Now()); ok {
		t.Fatal("expected rate limiting on consecutive polls")
	}
}

func TestDocumentSessionNoChangeWithoutModification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	var s documentSession
	s.record(path)
	defer s.Close()

	time.Sleep(50 * time.Millisecond)

	if _, ok := s.poll(time.Now()); ok {
		t.Fatal("expected no change without modification")
	}
}

func TestViewStateRoundTrip(t *testing.T) {
	app := &App{tab: &tab{documentState: documentState{page: 3}, viewStateFields: viewStateFields{scrollX: 12, scrollY: 34, zoom: 1.5, fitMode: fitManual, renderMode: renderSingle, dualPage: true, firstPageOffset: false, statusBarShown: true, altColors: true}}}
	state := app.captureViewState()

	if state.page != 3 || state.scrollX != 12 || state.scrollY != 34 || state.zoom != 1.5 || state.fitMode != fitManual || state.renderMode != renderSingle || !state.dualPage || state.firstPageOffset || !state.statusBarShown || !state.altColors {
		t.Fatalf("unexpected captured view state: %#v", state)
	}
}

func TestViewStateAtDocumentStartPreservesPreferencesOnly(t *testing.T) {
	state := viewState{page: 7, scrollX: 12, scrollY: 34, anchor: viewportAnchor{valid: true, page: 7}, zoom: 1.5, fitMode: fitManual, renderMode: renderSingle, dualPage: true, firstPageOffset: true, statusBarShown: true, altColors: true}
	state = state.atDocumentStart()

	if state.page != 0 || state.scrollX != 0 || state.scrollY != 0 || state.anchor.valid {
		t.Fatalf("expected document start location, got %#v", state)
	}
	if state.zoom != 1.5 || state.fitMode != fitManual || state.renderMode != renderSingle || !state.dualPage || !state.firstPageOffset || !state.statusBarShown || !state.altColors {
		t.Fatalf("expected viewer preferences to be preserved, got %#v", state)
	}
}

func TestEscCancelsPendingMark(t *testing.T) {
	app := &App{tab: &tab{}}
	app.pendingMark = "set"
	if !app.handleMarkToken("<Esc>") {
		t.Fatal("Esc was not handled")
	}
	if app.pendingMark != "" || app.message != "" {
		t.Fatalf("pendingMark=%q message=%q, want cancelled silently", app.pendingMark, app.message)
	}
}

func TestDocumentChangesWaitWhileAutoReloadIsOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{tab: &tab{}}
	app.document.setDelay(time.Millisecond)
	app.document.record(path)
	defer app.document.Close()
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	app.pollDocumentUpdate() // auto_reload is off
	if app.message != "" {
		t.Fatalf("change handled with auto_reload off: %q", app.message)
	}
	waitForDocumentChange(t, &app.document) // still pending
}

func TestOpenResumesSavedPage(t *testing.T) {
	pages := func(n int) [][]string {
		out := make([][]string, n)
		for i := range out {
			out[i] = []string{fmt.Sprintf("page %d", i+1)}
		}
		return out
	}
	first, second := testpdf.WritePages(t, pages(6)...), testpdf.WritePages(t, pages(6)...)
	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	app, err := New(first, runtime, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	app.winW, app.winH = 1000, 800
	if err := app.openInitialDocument(); err != nil {
		t.Fatal(err)
	}
	if err := config.SetDocumentSession(config.AbsoluteDocumentPath(second), viewState{page: 3, zoom: 1, anchor: viewportAnchor{page: 3, valid: true}}.documentSession()); err != nil {
		t.Fatal(err)
	}

	if err := app.Open(second); err != nil {
		t.Fatal(err)
	}
	if app.page != 3 || !app.pageMetrics[3].loaded {
		t.Fatalf("opened at page %d (metrics loaded %t), want saved page 4", app.page+1, app.pageMetrics[3].loaded)
	}

	// An explicit page goes there, in the document's saved view.
	app.zoom = 2
	if err := app.Open("file:///" + strings.TrimPrefix(filepath.ToSlash(first), "/") + "#page=3"); err != nil {
		t.Fatal(err)
	}
	if app.page != 2 || app.zoom != 1 {
		t.Fatalf("opened #page=3 at page %d zoom %v, want page 3 in the saved view", app.page+1, app.zoom)
	}

	// Reopening the open document resumes where it is, not where it was.
	app.gotoPageInput("5")
	if err := app.Open(first); err != nil {
		t.Fatal(err)
	}
	if app.page != 4 {
		t.Fatalf("reopened at page %d, want 5", app.page+1)
	}
}
