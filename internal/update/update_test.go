package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveLatest(t *testing.T, status int, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	previous := latestURL
	latestURL = server.URL
	t.Cleanup(func() { latestURL = previous })
}

func TestCheckComparesVersions(t *testing.T) {
	for current, available := range map[string]bool{
		"1.9.2":   true,
		"v1.9.9":  true,
		"1.10.0":  false,
		"v1.10.0": false,
		"2.0.0":   false,
	} {
		serveLatest(t, http.StatusOK, `{"tag_name":"1.10.0"}`)
		got, err := Check(context.Background(), current)
		if err != nil {
			t.Fatal(err)
		}
		if got != (Result{Latest: "1.10.0", Available: available}) {
			t.Errorf("%s: got %+v", current, got)
		}
	}
}

func TestCheckSkipsUnreleasedBuilds(t *testing.T) {
	previous := latestURL
	latestURL = "http://127.0.0.1:0" // must not be reached
	t.Cleanup(func() { latestURL = previous })
	for _, current := range []string{"dev", "", "1.2", "1.2.3-rc1"} {
		if _, err := Check(context.Background(), current); !errors.Is(err, ErrUnreleased) {
			t.Fatalf("%q: got %v, want ErrUnreleased", current, err)
		}
	}
}

func TestCheckReportsGitHubErrors(t *testing.T) {
	serveLatest(t, http.StatusForbidden, `{}`)
	if _, err := Check(context.Background(), "1.0.0"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
}

func TestInstruction(t *testing.T) {
	for method, want := range map[string]string{
		"":         "download it from " + ReleasesURL,
		"homebrew": "update it with homebrew",
		"aur":      "update it with aur",
	} {
		if got := Instruction(method); got != want {
			t.Errorf("%q: got %q, want %q", method, got, want)
		}
	}
}

func TestMethodPrefersBuiltValue(t *testing.T) {
	if got := Method("aur"); got != "aur" {
		t.Fatalf("got %q", got)
	}
}
