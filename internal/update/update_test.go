package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		got, err := Check(context.Background(), current, "aur")
		if err != nil {
			t.Fatal(err)
		}
		want := Info{Current: current, Latest: "1.10.0", Available: available, Method: "aur", URL: ReleasesURL}
		if got != want {
			t.Errorf("%s: got %+v", current, got)
		}
	}
}

func serveCompare(t *testing.T, status int, body string) *string {
	t.Helper()
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	previous := compareURL
	compareURL = server.URL + "/compare/main..."
	t.Cleanup(func() { compareURL = previous })
	return &path
}

func TestCheckComparesGitBuildsWithMain(t *testing.T) {
	for _, current := range []string{"0.9.4.r0.g49e499b", "v0.9.4.r2.g49e499b", "0.9.4-2-g49e499b", "v0.9.4-0-g49e499b"} {
		for _, behind := range []int{73, 0} {
			path := serveCompare(t, http.StatusOK, `{"behind_by":`+strconv.Itoa(behind)+`,"base_commit":{"sha":"5825ad4bb6e4cbdd494235b83181c9b1aa245135"}}`)
			got, err := Check(context.Background(), current, "aur")
			if err != nil {
				t.Fatal(err)
			}
			want := Info{Current: current, Latest: "5825ad4", Available: behind > 0, Behind: behind, Method: "aur", URL: CommitsURL}
			if got != want {
				t.Errorf("%s behind %d: got %+v", current, behind, got)
			}
			if *path != "/compare/main...49e499b" {
				t.Errorf("%s: asked for %q", current, *path)
			}
		}
	}
}

func TestCheckReportsUnknownGitCommits(t *testing.T) {
	serveCompare(t, http.StatusNotFound, `{}`)
	_, err := Check(context.Background(), "v0.9.4.r3.gdeadbee", "")
	if err == nil || err.Error() != "commit deadbee is not on GitHub" {
		t.Fatalf("got %v", err)
	}
}

func TestCheckSkipsUnreleasedBuilds(t *testing.T) {
	previous := latestURL
	latestURL = "http://127.0.0.1:0" // must not be reached
	t.Cleanup(func() { latestURL = previous })
	for _, current := range []string{"dev", "", "1.2", "1.2.3-rc1", "1.2.3.r4", "1.2.3.r4.gxyz1234", "1.2.3-4-g12ab", "1.2.3-g1a2b3c4"} {
		info, err := Check(context.Background(), current, "aur")
		if !errors.Is(err, ErrUnreleased) {
			t.Fatalf("%q: got %v, want ErrUnreleased", current, err)
		}
		if want := current + " builds are not checked for updates"; err.Error() != want {
			t.Fatalf("got %q, want %q", err, want)
		}
		if info.Current != current || info.Method != "aur" || info.Latest != "" {
			t.Fatalf("%q: got %+v", current, info)
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
	for _, c := range []struct {
		info Info
		want string
	}{
		{Info{URL: ReleasesURL}, "download it from " + ReleasesURL},
		{Info{URL: CommitsURL}, "rebuild it from main"},
		{Info{Method: "homebrew", URL: ReleasesURL}, "update it with homebrew"},
		{Info{Method: "aur", URL: CommitsURL}, "update it with aur"},
	} {
		if got := Instruction(c.info); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.info, got, c.want)
		}
	}
}

func TestSummary(t *testing.T) {
	for _, c := range []struct {
		info Info
		want string
	}{
		{Info{Latest: "1.10.0", URL: ReleasesURL}, "onon 1.10.0 is available"},
		{Info{Latest: "5825ad4", Behind: 73, URL: CommitsURL}, "onon is 73 commits behind main"},
		{Info{Latest: "5825ad4", Behind: 1, URL: CommitsURL}, "onon is 1 commit behind main"},
	} {
		if got := Summary(c.info); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.info, got, c.want)
		}
	}
}

func TestMethodPrefersBuiltValue(t *testing.T) {
	if got := Method("aur"); got != "aur" {
		t.Fatalf("got %q", got)
	}
}
