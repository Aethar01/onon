// Package update checks GitHub for a newer onon release and says how to
// upgrade to it, given how onon was installed.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ReleasesURL is where a downloaded onon is upgraded from.
const ReleasesURL = "https://github.com/Aethar01/onon/releases/latest"

// latestURL is the GitHub API's latest release; tests replace it.
var latestURL = "https://api.github.com/repos/Aethar01/onon/releases/latest"

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
	Latest    string // the latest release's tag; empty if the check failed
	Available bool   // Latest is newer than Current
	Method    string // how onon was installed, as Method gives it
	URL       string // the releases page
}

// Check asks GitHub whether a release newer than current exists, for onon
// built with the install method built. Info is filled in as far as it got,
// even when an error is returned.
func Check(ctx context.Context, current, built string) (Info, error) {
	info := Info{Current: current, Method: Method(built), URL: ReleasesURL}
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

// Instruction says how to upgrade onon installed by method.
func Instruction(method string) string {
	if method == "" {
		return "download it from " + ReleasesURL
	}
	return "update it with " + method
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
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.TagName, nil
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
