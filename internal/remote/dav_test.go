package remote

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// davServer is a WebDAV server holding one file, which it versions by ETag.
type davServer struct {
	mu      sync.Mutex
	content string
	version int
	puts    []http.Header
}

func (d *davServer) etag() string { return fmt.Sprintf(`"v%d"`, d.version) }

func (d *davServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		w.Header().Set("ETag", d.etag())
		if r.Header.Get("If-None-Match") == d.etag() {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(d.content)))
		if r.Method == http.MethodGet {
			io.WriteString(w, d.content)
		}
	case http.MethodPut:
		d.puts = append(d.puts, r.Header.Clone())
		if match := r.Header.Get("If-Match"); match != "" && match != d.etag() {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		d.content = string(body)
		d.version++
		w.Header().Set("ETag", d.etag())
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (d *davServer) change(content string) {
	d.mu.Lock()
	d.content = content
	d.version++
	d.mu.Unlock()
}

func TestWebDAVFetchSaveAndConflict(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	dav := &davServer{content: "%PDF one"}
	server := httptest.NewServer(dav)
	defer server.Close()
	uri, _, err := FromArg("webdav://" + strings.TrimPrefix(server.URL, "http://") + "/files/a.pdf")
	if err != nil || !strings.HasPrefix(uri, "dav://") || !Writable(uri) || !Watchable(uri) {
		t.Fatalf("FromArg = %q, %v", uri, err)
	}

	opened, err := Fetch(context.Background(), uri, nil)
	if err != nil || opened.URI != uri || opened.ETag != `"v0"` {
		t.Fatalf("fetch = %+v, %v", opened, err)
	}
	// As the viewer does before saving edits into the copy.
	Forget(uri, opened.Local)
	if err := os.WriteFile(opened.Local, []byte("%PDF edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved, err := Upload(context.Background(), uri, opened.Local, opened, false)
	if err != nil || saved.ETag != `"v1"` || dav.content != "%PDF edited" || dav.puts[0].Get("If-Match") != `"v0"` {
		t.Fatalf("save = %+v, %v; server has %q", saved, err, dav.content)
	}
	if again, err := Fetch(context.Background(), uri, nil); err != nil || again.Local != opened.Local {
		t.Fatalf("the saved copy is not reused: %+v, %v", again, err)
	}

	// Changed on the server: seen by a check, refused by a save, kept by :w!.
	dav.change("%PDF theirs")
	if now, ok, err := Changed(uri); err != nil || !ok || now.SameVersion(saved) {
		t.Fatalf("Changed = %+v, %t, %v", now, ok, err)
	}
	if _, err := Upload(context.Background(), uri, opened.Local, saved, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting save err = %v", err)
	}
	if _, err := Upload(context.Background(), uri, opened.Local, saved, true); err != nil || dav.content != "%PDF edited" {
		t.Fatalf("forced save: %v, server has %q", err, dav.content)
	}
	if last := dav.puts[len(dav.puts)-1]; last.Get("If-Match") != "" {
		t.Fatalf("forced save sent If-Match %q", last.Get("If-Match"))
	}
	// The conflict dropped the record; the forced save made it again.
	if again, err := Fetch(context.Background(), uri, nil); err != nil || again.Local != opened.Local {
		t.Fatalf("the force-saved copy is not reused: %+v, %v", again, err)
	}
}

func TestWebDAVSaveRedirected(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a.pdf" && r.Method == http.MethodPut {
			http.Redirect(w, r, "/moved/a.pdf", http.StatusMovedPermanently)
			return
		}
		if r.Method == http.MethodPut {
			t.Errorf("redirected PUT reached %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"v0"`)
		io.WriteString(w, "%PDF")
	}))
	defer server.Close()
	uri := "dav" + strings.TrimPrefix(server.URL, "http") + "/a.pdf"
	opened, err := Fetch(context.Background(), uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	// net/http would send it again as a GET, whose 200 passed for a save.
	if _, err := Upload(context.Background(), uri, opened.Local, opened, false); err == nil || !strings.Contains(err.Error(), "/moved/a.pdf") {
		t.Fatalf("err = %v", err)
	}
}

// A server that compresses, or sends in chunks, what it is asked for with
// gzip: the version fetched must match what a HEAD says.
func TestWebDAVFetchMatchesHead(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("ETag", `"v0-gzip"`)
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			io.WriteString(gz, "%PDF")
			gz.Close()
			return
		}
		w.Header().Set("ETag", `"v0"`)
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "4")
			return
		}
		io.WriteString(w, "%PDF")
		w.(http.Flusher).Flush() // chunked, so no Content-Length
	}))
	defer server.Close()
	uri := "dav" + strings.TrimPrefix(server.URL, "http") + "/a.pdf"
	opened, err := Fetch(context.Background(), uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	if now, ok, err := Changed(uri); err != nil || !ok || !now.SameVersion(opened) || opened.Size != 4 {
		t.Fatalf("Changed = %+v, %t, %v; opened %+v", now, ok, err, opened)
	}
}

func TestWebDAVSaveRefused(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		io.WriteString(w, "%PDF")
	}))
	defer server.Close()
	uri := "dav" + strings.TrimPrefix(server.URL, "http") + "/a.pdf"
	opened, err := Fetch(context.Background(), uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Upload(context.Background(), uri, opened.Local, opened, false); err == nil || !strings.Contains(err.Error(), "does not accept saving") {
		t.Fatalf("err = %v", err)
	}
}

func TestPageFragment(t *testing.T) {
	for arg, want := range map[string]struct {
		rest string
		page int
	}{
		"https://example.com/a.pdf#page=12":         {"https://example.com/a.pdf", 12},
		"https://example.com/a.pdf#zoom=50&page=3":  {"https://example.com/a.pdf", 3},
		"file:///tmp/a.pdf#page=2":                  {"file:///tmp/a.pdf", 2},
		"https://example.com/a.pdf#nameddest=intro": {"https://example.com/a.pdf", 0},
		"https://example.com/a.pdf#page=0":          {"https://example.com/a.pdf", 0},
		"https://example.com/a.pdf":                 {"https://example.com/a.pdf", 0},
		"notes#page=2.pdf":                          {"notes#page=2.pdf", 0},
	} {
		rest, page := PageFragment(arg)
		if rest != want.rest || page != want.page {
			t.Errorf("PageFragment(%q) = %q, %d; want %q, %d", arg, rest, page, want.rest, want.page)
		}
	}
}

func TestFetchDecodesGzipSentAnyway(t *testing.T) {
	useTempCache(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		io.WriteString(gz, "%PDF")
		gz.Close()
	}))
	defer server.Close()
	opened, err := Fetch(context.Background(), server.URL+"/a.pdf", nil)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(opened.Local); string(data) != "%PDF" {
		t.Fatalf("content = %q", data)
	}
}
