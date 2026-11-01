// Package update checks GitHub for a newer onon release, or for a git build
// newer commits, and says how to upgrade, given how onon was installed.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ReleasesURL is where a downloaded onon is upgraded from.
const ReleasesURL = "https://github.com/Aethar01/onon/releases/latest"

// CommitsURL is where a git build's newer commits are listed.
const CommitsURL = "https://github.com/Aethar01/onon/commits/main"

// latestURL is the GitHub API's latest release, and compareURL, followed by
// a commit, compares main with it; tests replace them.
var (
	latestURL  = "https://api.github.com/repos/Aethar01/onon/releases/latest"
	compareURL = "https://api.github.com/repos/Aethar01/onon/compare/main..."
)

// gitVersion matches the version git describe gives a build from git, 4
// commits after the 1.2.3 tag at commit 1a2b3c4: as is, 1.2.3-4-g1a2b3c4,
// or in the form AUR -git packages use, 1.2.3.r4.g1a2b3c4.
var gitVersion = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-\d+-g|\.r\d+\.g)([0-9a-f]{7,40})$`)

const timeout = 10 * time.Second

// ErrUnreleased matches the error returned for builds that are not a
// release, such as dev builds, which are not checked.
var ErrUnreleased = errors.New("not a release build")

// UnreleasedError is the error for a build that is not a release, which
// names its version.
type UnreleasedError struct{ Version string }

func (e UnreleasedError) Error() string {
	return e.Version + " builds are not checked for updates"
}

func (e UnreleasedError) Is(target error) bool { return target == ErrUnreleased }

// Info is what a check found. :check-updates and onon.check_updates both
// report it.
type Info struct {
	Current   string // the running version
	Latest    string // the latest release's tag, or for a git build main's latest commit; empty if the check failed
	Available bool   // Latest is newer than Current
	Behind    int    // for a git build, how many commits main has that it does not
	Method    string // how onon was installed, as Method gives it
	URL       string // the releases page, or for a git build main's commits
}

// Check asks GitHub whether a release newer than current exists, for onon
// built with the install method built. A git build, whose version names its
// commit, is instead checked for newer commits on main. Info is filled in as
// far as it got, even when an error is returned.
func Check(ctx context.Context, current, built string) (Info, error) {
	info := Info{Current: current, Method: Method(built), URL: ReleasesURL}
	if m := gitVersion.FindStringSubmatch(current); m != nil {
		info.URL = CommitsURL
		head, behind, err := compare(ctx, m[1])
		if err != nil {
			return info, err
		}
		info.Latest, info.Behind, info.Available = head, behind, behind > 0
		return info, nil
	}
	have, ok := parseVersion(current)
	if !ok {
		return info, UnreleasedError{current}
	}
	tag, err := latest(ctx)
	if err != nil {
		return info, err
	}
	want, ok := parseVersion(tag)
	if !ok {
		return info, fmt.Errorf("latest release has an unexpected tag %q", tag)
	}
	info.Latest, info.Available = tag, newer(want, have)
	return info, nil
}

// Summary says what update info found is available, such as "onon 1.2.3
// is available".
func Summary(info Info) string {
	if info.URL != CommitsURL {
		return "onon " + info.Latest + " is available"
	}
	commits := "commits"
	if info.Behind == 1 {
		commits = "commit"
	}
	return fmt.Sprintf("onon is %d %s behind main", info.Behind, commits)
}

// Instruction says how to upgrade onon that info was checked for.
func Instruction(info Info) string {
	switch {
	case info.Method != "":
		return "update it with " + info.Method
	case info.URL == CommitsURL:
		return "rebuild it from main"
	default:
		return "download it from " + ReleasesURL
	}
}

// Method is how this onon was installed: the build's InstallMethod, else
// homebrew when its cask is installed, else empty for a download.
func Method(built string) string {
	if built != "" {
		return built
	}
	if runtime.GOOS == "darwin" && homebrewCask() {
		return "homebrew"
	}
	return ""
}

// homebrewCask reports whether Homebrew has the onon cask installed. The
// cask installs the release app unchanged, so it cannot be told apart from
// a download any other way.
func homebrewCask() bool {
	prefixes := []string{"/opt/homebrew", "/usr/local"}
	if prefix := os.Getenv("HOMEBREW_PREFIX"); prefix != "" {
		prefixes = append([]string{prefix}, prefixes...)
	}
	for _, prefix := range prefixes {
		if info, err := os.Stat(filepath.Join(prefix, "Caskroom", "onon")); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// latest returns the tag of the latest release.
func latest(ctx context.Context) (string, error) {
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := getJSON(ctx, latestURL, &release); err != nil {
		return "", err
	}
	return release.TagName, nil
}

// compare returns main's latest commit, shortened as commit is, and how
// many commits main has that commit does not.
func compare(ctx context.Context, commit string) (string, int, error) {
	var comparison struct {
		BehindBy   int `json:"behind_by"`
		BaseCommit struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
	}
	err := getJSON(ctx, compareURL+commit, &comparison)
	var status statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return "", 0, fmt.Errorf("commit %s is not on GitHub", commit)
	}
	if err != nil {
		return "", 0, err
	}
	head := comparison.BaseCommit.SHA
	if len(head) < len(commit) {
		return "", 0, fmt.Errorf("GitHub gave an unexpected commit %q", head)
	}
	return head[:len(commit)], comparison.BehindBy, nil
}

// statusError is a GitHub API answer other than 200 OK.
type statusError struct {
	code   int
	status string
}

func (e statusError) Error() string { return "GitHub answered " + e.status }

// getJSON decodes the GitHub API's answer at url into v.
func getJSON(ctx context.Context, url string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError{resp.StatusCode, resp.Status}
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// parseVersion reads a MAJOR.MINOR.PATCH tag, with or without a leading v.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func newer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
