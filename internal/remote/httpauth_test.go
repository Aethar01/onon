package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetAuth(t *testing.T) {
	t.Helper()
	clear := func() {
		authMu.Lock()
		logins, users, extra = map[string]login{}, map[string]string{}, map[string]http.Header{}
		authMu.Unlock()
	}
	clear()
	t.Setenv("NETRC", filepath.Join(t.TempDir(), "none"))
	t.Cleanup(clear)
}

// basicServer serves a PDF to user:pass and a Basic challenge to anyone
// else, counting the challenges.
func basicServer(t *testing.T, user, pass string) (*httptest.Server, *int) {
	t.Helper()
	challenges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); ok && u == user && p == pass {
			fmt.Fprint(w, "%PDF")
			return
		}
		challenges++
		w.Header().Set("WWW-Authenticate", `Basic realm="Papers", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	return server, &challenges
}

func TestHTTPLoginIsAskedAndRemembered(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server, _ := basicServer(t, "me", "secret")
	host := strings.TrimPrefix(server.URL, "http://")
	answers := []string{"me", "wrong", "me", "secret"}
	asked := usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) {
		answer := answers[0]
		answers = answers[1:]
		return answer, true
	})

	if _, err := Fetch(context.Background(), server.URL+"/a.pdf", nil); err != nil {
		t.Fatal(err)
	}
	want := []Prompt{
		{Host: host, Text: "Username for " + host + " (Papers)", Kind: PromptText},
		{Host: host, Text: "Password for me@" + host, Kind: PromptSecret},
		{Host: host, Text: "Login refused; username for " + host + " (Papers)", Kind: PromptText, Initial: "me"},
		{Host: host, Text: "Password for me@" + host, Kind: PromptSecret},
	}
	if fmt.Sprint(*asked) != fmt.Sprint(want) {
		t.Fatalf("asked\n%+v\nwant\n%+v", *asked, want)
	}

	// The login is kept for the host, so the next fetch asks nothing.
	if _, err := Fetch(context.Background(), server.URL+"/b.pdf", nil); err != nil || len(*asked) != 4 {
		t.Fatalf("second fetch: err=%v asked=%d", err, len(*asked))
	}
}

func TestHTTPLoginRefused(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server, _ := basicServer(t, "me", "secret")
	usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) { return "", false })
	_, err := Fetch(context.Background(), server.URL+"/a.pdf", nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}

	// Three wrong logins and it stops asking.
	tries := 0
	usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) {
		if p.Kind == PromptText {
			tries++
		}
		return "wrong", true
	})
	if _, err := Fetch(context.Background(), server.URL+"/a.pdf", nil); err == nil || tries != maxLoginPrompts {
		t.Fatalf("err=%v tries=%d", err, tries)
	}
}

func TestHTTPLoginFromURLAndNetrc(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server, challenges := basicServer(t, "me", "secret")
	u, _ := url.Parse(server.URL)
	usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) {
		t.Errorf("asked %q", p.Text)
		return "", false
	})

	// A login in the URL is used but not kept in it.
	uri, _, err := FromArg("http://me:secret@" + u.Host + "/a.pdf")
	if err != nil || strings.Contains(uri, "secret") || strings.Contains(uri, "me@") {
		t.Fatalf("FromArg = %q, %v", uri, err)
	}
	if _, err := Fetch(context.Background(), uri, nil); err != nil || *challenges != 0 {
		t.Fatalf("URL login: err=%v challenges=%d", err, *challenges)
	}

	resetAuth(t)
	netrc := filepath.Join(t.TempDir(), "netrc")
	content := "machine other.example login x password y\n" +
		"macdef init\nmachine " + u.Hostname() + " login wrong password wrong\n\n" +
		"machine " + u.Hostname() + "\n  login me\n  password secret\n" +
		"default login anon password anon\n"
	if err := os.WriteFile(netrc, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETRC", netrc)
	if _, err := Fetch(context.Background(), server.URL+"/b.pdf", nil); err != nil || *challenges != 1 {
		t.Fatalf("netrc login: err=%v challenges=%d", err, *challenges)
	}
	if user, pass, ok := netrcLogin("unlisted.example"); !ok || user != "anon" || pass != "anon" {
		t.Fatalf("default entry = %q %q %t", user, pass, ok)
	}
}

func TestHTTPUserFromURLIsOffered(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	server, _ := basicServer(t, "me", "secret")
	u, _ := url.Parse(server.URL)
	asked := usePrompter(t, func(ctx context.Context, p Prompt) (string, bool) {
		if p.Kind == PromptText {
			return p.Initial, true
		}
		return "secret", true
	})
	uri, _, _ := FromArg("http://me@" + u.Host + "/a.pdf")
	if _, err := Fetch(context.Background(), uri, nil); err != nil || (*asked)[0].Initial != "me" {
		t.Fatalf("err=%v asked=%+v", err, *asked)
	}
}

func TestHTTPHeadersGoOnlyToTheirHost(t *testing.T) {
	useTempCache(t)
	resetAuth(t)
	var leaked http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Clone()
		fmt.Fprint(w, "%PDF")
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("X-Api-Key") != "key" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/moved.pdf" {
			// To another host: localhost is not 127.0.0.1.
			http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1)+"/x.pdf", http.StatusFound)
			return
		}
		fmt.Fprint(w, "%PDF")
	}))
	defer api.Close()
	apiHost := strings.TrimPrefix(api.URL, "http://")

	if _, err := Fetch(context.Background(), api.URL+"/a.pdf", nil); err == nil || !strings.Contains(err.Error(), "http_headers") {
		t.Fatalf("without headers err = %v", err)
	}
	SetHTTPHeaders(map[string]map[string]string{apiHost: {"Authorization": "Bearer token", "x-api-key": "key"}})
	if _, err := Fetch(context.Background(), api.URL+"/a.pdf", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(context.Background(), api.URL+"/moved.pdf", nil); err != nil {
		t.Fatal(err)
	}
	if leaked == nil || leaked.Get("Authorization") != "" || leaked.Get("X-Api-Key") != "" {
		t.Fatalf("redirected request headers = %v", leaked)
	}

	SetHTTPHeaders(map[string]map[string]string{apiHost: {"Authorization": "Bearer stale"}})
	if _, err := Fetch(context.Background(), api.URL+"/b.pdf", nil); err == nil || !strings.Contains(err.Error(), "Authorization header set for") {
		t.Fatalf("wrong token err = %v", err)
	}
}

func TestHeadersForMatchesMostSpecific(t *testing.T) {
	resetAuth(t)
	SetHTTPHeaders(map[string]map[string]string{
		"*.example.com":      {"X": "wild"},
		"api.example.com":    {"X": "host"},
		"api.example.com:81": {"X": "port"},
	})
	for host, want := range map[string]string{
		"api.example.com:81": "port", "api.example.com": "host", "API.example.com:443": "host",
		"a.b.example.com": "wild", "example.com": "", "badexample.com": "",
	} {
		if got := headersFor(host).Get("X"); got != want {
			t.Errorf("headersFor(%q) = %q, want %q", host, got, want)
		}
	}
}

type recordTransport struct{ req *http.Request }

func (r *recordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestLoginsStayOnTheirSchemeAndOffDowngrades(t *testing.T) {
	resetAuth(t)
	setLogin("https://host", login{user: "me", pass: "secret"})
	SetHTTPHeaders(map[string]map[string]string{"host": {"X-Api-Key": "key"}})
	base := &recordTransport{}
	transport := authTransport{base: base}
	send := func(raw string, from *http.Request) *http.Request {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		req.Response = &http.Response{Request: from}
		if from == nil {
			req.Response = nil
		}
		transport.RoundTrip(req)
		return base.req
	}

	if got := send("https://host/a.pdf", nil); got.Header.Get("Authorization") == "" || got.Header.Get("X-Api-Key") != "key" {
		t.Fatalf("https request headers = %v", got.Header)
	}
	if got := send("http://host/a.pdf", nil); got.Header.Get("Authorization") != "" {
		t.Fatal("an https login was sent over http")
	}
	from, _ := http.NewRequest(http.MethodGet, "https://host/a.pdf", nil)
	if got := send("http://host/b.pdf", from); got.Header.Get("Authorization") != "" || got.Header.Get("X-Api-Key") != "" {
		t.Fatalf("a redirect down to http carried %v", got.Header)
	}
}
