package viewer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"
	"onon/internal/remote/remotetest"
	"onon/internal/testpdf"
)

func useRemoteTestEnvironment(t *testing.T) (sshHome string) {
	t.Helper()
	dir := t.TempDir()
	oldCache, oldSSH := remote.CacheDir, remote.SSHCommand
	remote.CacheDir = func() (string, error) { return dir, nil }
	command, home, _ := remotetest.FakeSSH(t)
	remote.SSHCommand = command
	t.Cleanup(func() {
		remote.Close()
		remote.CacheDir, remote.SSHCommand = oldCache, oldSSH
	})
	return home
}

func newRemoteTestApp(t *testing.T, uri string) *App {
	t.Helper()
	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	app, err := New(uri, runtime, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	if err := app.openInitialDocument(); err != nil {
		t.Fatal(err)
	}
	waitForRemoteTask(t, app)
	return app
}

func waitForRemoteTask(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for app.remoteTask != nil {
		if time.Now().After(deadline) {
			t.Fatalf("remote task still running: %s", app.message)
		}
		time.Sleep(5 * time.Millisecond)
		app.pollRemoteTask()
	}
}

func TestOpenRemoteDocumentOverHTTP(t *testing.T) {
	useRemoteTestEnvironment(t)
	pdf, err := os.ReadFile(testpdf.Write(t, "remote"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/papers/remote.pdf", func(w http.ResponseWriter, r *http.Request) { w.Write(pdf) })
	server := httptest.NewServer(mux)
	defer server.Close()

	uri := server.URL + "/papers/remote.pdf"
	app := newRemoteTestApp(t, uri)
	if app.doc == nil {
		t.Fatalf("document not opened: %s", app.message)
	}
	if app.docPath != uri || app.docName != "remote.pdf" || app.docFile == uri || filepath.Base(app.docFile) != "remote.pdf" {
		t.Fatalf("docPath=%q docName=%q docFile=%q", app.docPath, app.docName, app.docFile)
	}
	if !slices.Contains(config.RecentFiles(10), uri) {
		t.Fatalf("recent files = %v", config.RecentFiles(10))
	}

	app.runCommand(":w")
	if !strings.Contains(app.message, "cannot write") {
		t.Fatalf(":w message = %q", app.message)
	}

	// Beside a remote document is on its server.
	app.runCommand(":open missing.pdf")
	if app.remoteTask == nil || app.remoteTask.uri != server.URL+"/papers/missing.pdf" {
		t.Fatalf("open beside = %+v", app.remoteTask)
	}
	waitForRemoteTask(t, app)
	if !strings.Contains(app.message, "404") || app.docPath != uri {
		t.Fatalf("failed open: message=%q docPath=%q", app.message, app.docPath)
	}
}

func TestEscCancelsRemoteFetch(t *testing.T) {
	useRemoteTestEnvironment(t)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	app := testLayoutApp(1)
	app.fetchRemoteDocument(server.URL+"/slow.pdf", openDocumentOptions{}, "")
	if !strings.Contains(app.message, "fetching slow.pdf") {
		t.Fatalf("message = %q", app.message)
	}
	task := app.remoteTask
	app.runAction("close")
	if app.remoteTask != nil || app.message != "cancelled" {
		t.Fatalf("task=%v message=%q", app.remoteTask, app.message)
	}
	<-task.done // the fetch stops
}

func TestRemoteDocumentSavesBackOverSSH(t *testing.T) {
	home := useRemoteTestEnvironment(t)
	original := filepath.Join(home, "doc.pdf")
	if err := os.WriteFile(original, mustRead(t, testpdf.Write(t, "over ssh")), 0o644); err != nil {
		t.Fatal(err)
	}

	// Written absolute, it is found under the home and known by that name.
	app := newRemoteTestApp(t, "ssh://me@host/doc.pdf")
	if app.doc == nil {
		t.Fatalf("document not opened: %s", app.message)
	}
	if app.docPath != "sftp://me@host/~/doc.pdf" {
		t.Fatalf("docPath = %q", app.docPath)
	}
	if app.document.remote == nil {
		t.Fatal("the SSH document is not watched")
	}

	// A change on the host is fetched and reloaded.
	if err := os.WriteFile(original, mustRead(t, testpdf.WritePages(t, []string{"one"}, []string{"two"})), 0o644); err != nil {
		t.Fatal(err)
	}
	opened := app.docFile
	if err := app.reloadUpdatedDocument(documentChange{}); err != nil {
		t.Fatal(err)
	}
	waitForRemoteTask(t, app)
	if app.pageCount != 2 || app.docFile == opened || app.docPath != "sftp://me@host/~/doc.pdf" {
		t.Fatalf("after reload pages=%d file=%q path=%q: %s", app.pageCount, app.docFile, app.docPath, app.message)
	}

	app.markEdited()
	app.runCommand(":w")
	if app.remoteTask == nil || app.remoteTask.kind != remoteSave {
		t.Fatalf("no save started: %s", app.message)
	}
	waitForRemoteTask(t, app)
	if app.unsaved || !strings.HasPrefix(app.message, "wrote doc.pdf to host") {
		t.Fatalf("unsaved=%t message=%q", app.unsaved, app.message)
	}
	if !app.remoteInfo.SameVersion(statInfo(t, original)) {
		t.Fatalf("remoteInfo %+v is not the saved file", app.remoteInfo)
	}

	// A change made on the host since is not saved over without :w!.
	later := time.Now().Add(time.Hour)
	if err := os.WriteFile(original, mustRead(t, testpdf.Write(t, "theirs, and longer")), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(original, later, later)
	app.markEdited()
	app.runCommand(":wq")
	waitForRemoteTask(t, app)
	if app.quit || !strings.Contains(app.message, ":w! overwrites it") {
		t.Fatalf("quit=%t message=%q", app.quit, app.message)
	}
	app.runCommand(":w!")
	waitForRemoteTask(t, app)
	if app.unsaved {
		t.Fatalf(":w! left unsaved edits: %s", app.message)
	}
	if doc, err := mupdf.Open(original, mupdf.OpenOptions{}); err != nil {
		t.Fatalf("saved file does not open: %v", err)
	} else {
		doc.Close()
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func statInfo(t *testing.T, path string) remote.Info {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return remote.Info{ModTime: st.ModTime(), Size: st.Size()}
}

// waitForRemotePrompt runs the loop's polling until ssh's question is asked.
func waitForRemotePrompt(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for app.remotePrompt == nil {
		if time.Now().After(deadline) || app.remoteTask == nil {
			t.Fatalf("no ssh prompt: %s", app.message)
		}
		time.Sleep(5 * time.Millisecond)
		app.pollRemoteTask()
		app.pollRemotePrompts()
	}
}

func TestSSHPasswordIsAskedInTheWindow(t *testing.T) {
	home := useRemoteTestEnvironment(t)
	if err := os.WriteFile(filepath.Join(home, "doc.pdf"), mustRead(t, testpdf.Write(t, "locked")), 0o644); err != nil {
		t.Fatal(err)
	}
	remotetest.RequireAnswer(t, "me@host's password: ", "hunter2")

	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	app, err := New("", runtime, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Esc refuses, and ssh gives up.
	app.runCommand(":open me@host:doc.pdf")
	waitForRemotePrompt(t, app)
	if app.mode != modePrompt || !app.inputMasked() || app.inputPrefix() != " me@host's password: " {
		t.Fatalf("mode=%v masked=%t prefix=%q", app.mode, app.inputMasked(), app.inputPrefix())
	}
	app.runAction("close")
	waitForRemoteTask(t, app)
	if app.doc != nil || !strings.Contains(app.message, "Permission denied") {
		t.Fatalf("refused: doc=%v message=%q", app.doc, app.message)
	}

	app.runCommand(":open me@host:doc.pdf")
	waitForRemotePrompt(t, app)
	app.input.Set("hunter2")
	app.commitInputMode()
	waitForRemoteTask(t, app)
	if app.doc == nil || app.docPath != "sftp://me@host/~/doc.pdf" {
		t.Fatalf("after the password: docPath=%q message=%q", app.docPath, app.message)
	}
	if len(app.histories) != 0 {
		t.Fatalf("the password went into prompt history: %v", app.histories)
	}
}

func TestRemotePromptGoesAwayWithTheFetch(t *testing.T) {
	home := useRemoteTestEnvironment(t)
	if err := os.WriteFile(filepath.Join(home, "doc.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	remotetest.RequireAnswer(t, "me@host's password: ", "hunter2")
	app := testLayoutApp(1)
	app.remotePrompts = make(chan *remotePrompt, 4)
	remote.SetPrompter(app.askRemote)
	defer remote.SetPrompter(nil)

	app.fetchRemoteDocument("sftp://me@host/~/doc.pdf", openDocumentOptions{}, "")
	task := app.remoteTask
	waitForRemotePrompt(t, app)
	app.cancelRemoteTask() // as Esc does once the prompt is closed, or a new :open
	<-task.done
	deadline := time.Now().Add(5 * time.Second)
	for app.remotePrompt != nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		app.pollRemotePrompts()
	}
	if app.remotePrompt != nil || app.mode != modeNormal {
		t.Fatalf("prompt left up: mode=%v", app.mode)
	}
}

func TestHTTPLoginIsAskedInTheWindow(t *testing.T) {
	useRemoteTestEnvironment(t)
	t.Setenv("NETRC", filepath.Join(t.TempDir(), "none"))
	pdf := mustRead(t, testpdf.Write(t, "members only"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); ok && user == "me" && pass == "secret" {
			w.Write(pdf)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="Members"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	runtime, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	app, err := New("", runtime, 0, nil, NewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// The user in the URL is offered; the password is asked for hidden.
	u, _ := url.Parse(server.URL)
	app.runCommand(":open http://me@" + u.Host + "/members.pdf")
	waitForRemotePrompt(t, app)
	if !strings.HasPrefix(app.inputPrefix(), " Username for "+u.Host+" (Members)") || app.input.Value != "me" || app.inputMasked() {
		t.Fatalf("prefix=%q input=%q masked=%t", app.inputPrefix(), app.input.Value, app.inputMasked())
	}
	app.commitInputMode()
	waitForRemotePrompt(t, app)
	if !app.inputMasked() {
		t.Fatal("password not masked")
	}
	app.input.Set("secret")
	app.commitInputMode()
	waitForRemoteTask(t, app)
	if app.doc == nil || app.docPath != server.URL+"/members.pdf" {
		t.Fatalf("docPath=%q message=%q", app.docPath, app.message)
	}
}

func TestEditsMadeWhileFetchingAreKept(t *testing.T) {
	useRemoteTestEnvironment(t)
	pdf := mustRead(t, testpdf.Write(t, "page"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(pdf) }))
	defer server.Close()
	app := newRemoteTestApp(t, server.URL+"/first.pdf")

	// Opening another: an edit made during the download keeps the first.
	app.runCommand(":open second.pdf")
	app.markEdited()
	waitForRemoteTask(t, app)
	if app.docName != "first.pdf" || !strings.Contains(app.message, "kept the edits") {
		t.Fatalf("docName=%q message=%q", app.docName, app.message)
	}
	app.runCommand(":open second.pdf") // now agreed to
	waitForRemoteTask(t, app)
	if app.docName != "second.pdf" || app.unsaved {
		t.Fatalf("second open: docName=%q unsaved=%t", app.docName, app.unsaved)
	}

	// A change found on the server does not reload over edits.
	app.markEdited()
	file := app.docFile
	if err := app.reloadUpdatedDocument(documentChange{}); err != nil {
		t.Fatal(err)
	}
	waitForRemoteTask(t, app)
	if app.docFile != file || !app.unsaved || !strings.Contains(app.message, "keeping unsaved edits") {
		t.Fatalf("reload over edits: file changed=%t unsaved=%t message=%q", app.docFile != file, app.unsaved, app.message)
	}
}

func TestRemotePromptWaitsForTyping(t *testing.T) {
	app := testLayoutApp(1)
	app.remotePrompts = make(chan *remotePrompt, 1)
	app.remotePrompts <- &remotePrompt{ctx: context.Background(), prompt: remote.Prompt{Text: "password", Kind: remote.PromptSecret}, reply: make(chan remoteAnswer, 1)}
	app.mode = modeCommand
	app.input.Set("page 3")
	app.pollRemotePrompts()
	if app.mode != modeCommand || app.input.Value != "page 3" {
		t.Fatalf("typing interrupted: mode=%v input=%q", app.mode, app.input.Value)
	}
	app.runAction("close")
	app.pollRemotePrompts()
	if app.mode != modePrompt || !app.inputMasked() {
		t.Fatalf("not asked after: mode=%v", app.mode)
	}
}

func TestOpenReportsABadRemoteArgument(t *testing.T) {
	app := testLayoutApp(1)
	err := app.Open("me@host:~bob/doc.pdf")
	if err == nil || !strings.Contains(err.Error(), "~user") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteWatcherForgetsOurOwnSave(t *testing.T) {
	w := newRemoteWatcher(remote.Info{URI: "sftp://host/~/a.pdf"}, false)
	defer w.Close()
	w.changed <- watcherChange{size: 1} // seen before our save was recorded
	w.setKnown(remote.Info{URI: "sftp://host/~/a.pdf", Size: 1})
	if _, ok := w.waitForChange(0); ok {
		t.Fatal("our own save was reported as a change")
	}
}
