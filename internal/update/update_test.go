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

func TestCheckReportsNewerRelease(t *testing.T) {
	serveLatest(t, http.StatusOK, `{"tag_name":"1.10.0"}`)
	msg, err := Check(context.Background(), "1.9.2", "homebrew")
	if err != nil {
		t.Fatal(err)
	}
	if want := "onon 1.10.0 is available: update it with homebrew"; msg != want {
		t.Fatalf("got %q, want %q", msg, want)
	}
}

func TestCheckReportsUpToDate(t *testing.T) {
	for _, current := range []string{"1.2.3", "v1.2.3", "1.3.0"} {
		serveLatest(t, http.StatusOK, `{"tag_name":"1.2.3"}`)
		msg, err := Check(context.Background(), current, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(msg, "is up to date") {
			t.Fatalf("%s: got %q", current, msg)
		}
	}
}

func TestCheckSkipsUnreleasedBuilds(t *testing.T) {
	previous := latestURL
	latestURL = "http://127.0.0.1:0" // must not be reached
	t.Cleanup(func() { latestURL = previous })
	for _, current := range []string{"dev", "", "1.2", "1.2.3-rc1"} {
		if _, err := Check(context.Background(), current, ""); !errors.Is(err, ErrUnreleased) {
			t.Fatalf("%q: got %v, want ErrUnreleased", current, err)
		}
	}
}

func TestCheckReportsGitHubErrors(t *testing.T) {
	serveLatest(t, http.StatusForbidden, `{}`)
	if _, err := Check(context.Background(), "1.0.0", ""); err == nil || !strings.Contains(err.Error(), "403") {
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
