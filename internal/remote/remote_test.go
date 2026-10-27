package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"onon/internal/remote/remotetest"
)

func TestMain(m *testing.M) {
	remotetest.ServeIfFakeSSH()
	if code, ok := RunAskpass(); ok {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func useTempCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := CacheDir
	CacheDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { CacheDir = old })
	return dir
}

func useFakeSSH(t *testing.T) (home, argsFile string) {
	t.Helper()
	command, home, argsFile := remotetest.FakeSSH(t)
	old := SSHCommand
	SSHCommand = command
	t.Cleanup(func() {
		Close()
		SSHCommand = old
	})
	return home, argsFile
}

func TestFromArg(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "a:b.pdf")
	if err := os.WriteFile(local, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		arg, want string
		remote    bool
	}{
		{"https://Example.com/a/doc.pdf#page=2", "https://example.com/a/doc.pdf", true},
		{"ssh://me@Host/doc.pdf", "sftp://me@host/doc.pdf", true},
		{"scp://me;fingerprint=ssh-rsa-xx@host:2222//abs/doc.pdf", "sftp://me@host:2222/abs/doc.pdf", true},
		{"sftp://host:22/~/docs/a%20b.pdf", "sftp://host/~/docs/a%20b.pdf", true},
		{"sftp://[::1]:22/x.pdf", "sftp://[::1]/x.pdf", true},
		{"me@host:docs/doc.pdf", "sftp://me@host/~/docs/doc.pdf", true},
		{"host:/srv/doc.pdf", "sftp://host/srv/doc.pdf", true},
		{"host:~/doc.pdf", "sftp://host/~/doc.pdf", true},
		{"me@[::1]:doc.pdf", "sftp://me@[::1]/~/doc.pdf", true},
		{"doc.pdf", "", false},
		{"./host:doc.pdf", "", false},
		{"/tmp/host:doc.pdf", "", false},
		{"C:doc.pdf", "", false},
		{"file:///tmp/doc.pdf", "", false},
		{"-oProxyCommand=x:doc.pdf", "", false},
		{local, "", false},
	}
	for _, tt := range tests {
		got, remote, err := FromArg(tt.arg)
		if err != nil || remote != tt.remote || got != tt.want {
			t.Errorf("FromArg(%q) = %q, %t, %v; want %q, %t", tt.arg, got, remote, err, tt.want, tt.remote)
		}
	}
	for _, bad := range []string{"sftp://host/", "sftp://-oProxyCommand=x/doc.pdf", "sftp://host/~bob/doc.pdf", "host:~bob/doc.pdf"} {
		if _, _, err := FromArg(bad); err == nil {
			t.Errorf("FromArg(%q) succeeded", bad)
		}
	}
}

func TestResolveAndName(t *testing.T) {
	got, err := Resolve("sftp://host/~/papers/a.pdf", "../b.pdf")
	if err != nil || got != "sftp://host/~/b.pdf" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	got, err = Resolve("https://example.com/x/a.pdf?v=1", "b.pdf")
	if err != nil || got != "https://example.com/x/b.pdf" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if name := Name("https://example.com/x/a%20b.pdf?v=1"); name != "a b.pdf" {
		t.Fatalf("Name = %q", name)
	}
	if name := Name("https://example.com/"); name != "example.com" {
		t.Fatalf("Name = %q", name)
	}
	if shown := Display("https://me:secret@example.com/a.pdf?X-Amz-Signature=token"); strings.Contains(shown, "secret") || strings.Contains(shown, "token") {
		t.Fatalf("Display = %q", shown)
	}
}

func TestFetchHTTPRevalidates(t *testing.T) {
	cache := useTempCache(t)
	etag := `"v1"`
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		downloads.Add(1)
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "%PDF "+etag)
	}))
	defer server.Close()

	uri := server.URL + "/files/report"
	var progressed int64
	first, err := Fetch(context.Background(), uri, func(done, total int64) { progressed = done })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first.Local) != "report.pdf" || !strings.HasPrefix(first.Local, cache) {
		t.Fatalf("local = %q", first.Local)
	}
	if progressed == 0 {
		t.Fatal("no progress reported")
	}
	second, err := Fetch(context.Background(), uri, nil)
	if err != nil || second.Local != first.Local || downloads.Load() != 1 {
		t.Fatalf("revalidated fetch = %+v, %v after %d downloads", second, err, downloads.Load())
	}

	etag = `"v2"`
	third, err := Fetch(context.Background(), uri, nil)
	if err != nil || third.Local == first.Local {
		t.Fatalf("changed fetch = %+v, %v", third, err)
	}
	if data, _ := os.ReadFile(third.Local); string(data) != `%PDF "v2"` {
		t.Fatalf("content = %q", data)
	}
	if _, err := os.Stat(first.Local); !os.IsNotExist(err) {
		t.Fatalf("old version kept: %v", err)
	}
}

func TestFetchHTTPErrors(t *testing.T) {
	useTempCache(t)
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	_, err := Fetch(context.Background(), server.URL+"/missing.pdf", nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "partial")
		w.(http.Flusher).Flush()
		cancel()
		<-block
	}))
	defer slow.Close()
	defer close(block)
	if _, err := Fetch(ctx, slow.URL+"/slow.pdf", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fetch err = %v", err)
	}
}

func TestHTTPName(t *testing.T) {
	resp := &http.Response{Header: http.Header{"Content-Disposition": {`attachment; filename="../Paper.epub"`}}}
	if name := httpName("https://example.com/download", resp); name != "Paper.epub" {
		t.Fatalf("name = %q", name)
	}
	if name := safeName(`a/b\c:d.pdf`); name != "a_b_c_d.pdf" {
		t.Fatalf("safeName = %q", name)
	}
}

func TestFetchSFTP(t *testing.T) {
	useTempCache(t)
	home, argsFile := useFakeSSH(t)
	if err := os.MkdirAll(filepath.Join(home, "papers"), 0o700); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(home, "papers", "a.pdf")
	if err := os.WriteFile(doc, []byte("%PDF one"), 0o640); err != nil {
		t.Fatal(err)
	}

	abs := "sftp://me@host:2200" + filepath.ToSlash(doc)
	info, err := Fetch(context.Background(), abs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.URI != abs || filepath.Base(info.Local) != "a.pdf" {
		t.Fatalf("info = %+v", info)
	}
	args, _ := os.ReadFile(argsFile)
	if want := "-l\nme\n-p\n2200\n-s\n--\nhost\nsftp"; !strings.HasSuffix(string(args), want) {
		t.Fatalf("ssh args = %q", args)
	}

	// Relative to the home, and found there when written as absolute.
	for _, uri := range []string{"sftp://me@host:2200/~/papers/a.pdf", "ssh://me@host:2200/papers/a.pdf"} {
		got, err := Fetch(context.Background(), uri, nil)
		if err != nil || got.URI != "sftp://me@host:2200/~/papers/a.pdf" {
			t.Fatalf("Fetch(%q) = %+v, %v", uri, got, err)
		}
	}

	// A copy still current is not fetched again; a changed file is.
	again, err := Fetch(context.Background(), abs, nil)
	if err != nil || again.Local != info.Local {
		t.Fatalf("refetch = %+v, %v", again, err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.WriteFile(doc, []byte("%PDF two!"), 0o640); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(doc, later, later)
	if now, connected, err := Changed(abs); err != nil || !connected || now.SameVersion(info) {
		t.Fatalf("Changed = %+v, %t, %v", now, connected, err)
	}
	changed, err := Fetch(context.Background(), abs, nil)
	if err != nil || changed.Local == info.Local {
		t.Fatalf("changed fetch = %+v, %v", changed, err)
	}

	if _, err := Fetch(context.Background(), "sftp://me@host:2200/~/missing.pdf", nil); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestUploadSFTP(t *testing.T) {
	useTempCache(t)
	home, _ := useFakeSSH(t)
	doc := filepath.Join(home, "a.pdf")
	if err := os.WriteFile(doc, []byte("%PDF one"), 0o640); err != nil {
		t.Fatal(err)
	}
	uri := "sftp://host/~/a.pdf"
	opened, err := Fetch(context.Background(), uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opened.Local, []byte("%PDF edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := Upload(context.Background(), uri, opened.Local, opened, false)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(doc); string(data) != "%PDF edited" {
		t.Fatalf("remote = %q", data)
	}
	if st, _ := os.Stat(doc); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", st.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(home, ".a.pdf.onon-*")); len(leftovers) > 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
	// The cache now holds the saved version, so opening again reuses it.
	if again, err := Fetch(context.Background(), uri, nil); err != nil || again.Local != opened.Local {
		t.Fatalf("after save fetch = %+v, %v", again, err)
	}

	// Someone else writes it: a save over that is refused unless forced.
	later := time.Now().Add(time.Hour)
	_ = os.WriteFile(doc, []byte("%PDF theirs, longer"), 0o640)
	_ = os.Chtimes(doc, later, later)
	if _, err := Upload(context.Background(), uri, opened.Local, saved, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting upload err = %v", err)
	}
	if _, err := Upload(context.Background(), uri, opened.Local, saved, true); err != nil {
		t.Fatalf("forced upload err = %v", err)
	}
	if data, _ := os.ReadFile(doc); string(data) != "%PDF edited" {
		t.Fatalf("remote after force = %q", data)
	}

	if _, err := Upload(context.Background(), "https://example.com/a.pdf", opened.Local, saved, false); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("http upload err = %v", err)
	}
}

func TestSSHConnectError(t *testing.T) {
	useTempCache(t)
	old := SSHCommand
	SSHCommand = "false"
	defer func() { SSHCommand = old }()
	_, err := Fetch(context.Background(), "sftp://nowhere/a.pdf", nil)
	if err == nil || !strings.Contains(err.Error(), "ssh nowhere") {
		t.Fatalf("err = %v", err)
	}
}
