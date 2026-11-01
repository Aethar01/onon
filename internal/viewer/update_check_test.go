package viewer

import (
	"context"
	"errors"
	"testing"
	"time"

	"onon/internal/update"
)

func setUpdateGlobals(t *testing.T, info update.Info, err error) *[]string {
	t.Helper()
	previousCheck, previousOpen := checkForUpdates, openReleasesPage
	t.Cleanup(func() { checkForUpdates, openReleasesPage = previousCheck, previousOpen })
	if info.URL == "" {
		info.URL = update.ReleasesURL
	}
	checkForUpdates = func(context.Context) (update.Info, error) { return info, err }
	var opened []string
	openReleasesPage = func(_ *App, uri string) error {
		opened = append(opened, uri)
		return nil
	}
	return &opened
}

func runUpdateCheck(t *testing.T) *App {
	t.Helper()
	app := testLayoutApp(1)
	app.runCommand(":check-updates")
	if app.message != "checking for updates" {
		t.Fatalf("got %q while checking", app.message)
	}
	deadline := time.Now().Add(2 * time.Second)
	for app.updateResult != nil {
		if time.Now().After(deadline) {
			t.Fatal("update check never finished")
		}
		app.pollUpdateCheck()
		time.Sleep(time.Millisecond)
	}
	return app
}

func TestCheckUpdatesNamesInstallMethod(t *testing.T) {
	setUpdateGlobals(t, update.Info{Current: "1.0.0", Latest: "1.1.0", Available: true, Method: "aur"}, nil)
	app := runUpdateCheck(t)
	if want := "onon 1.1.0 is available: update it with aur"; app.message != want || app.mode != modeNormal {
		t.Fatalf("got %q in mode %v, want %q", app.message, app.mode, want)
	}
}

func TestCheckUpdatesOffersReleasesPageForDownloads(t *testing.T) {
	for answer, wantOpened := range map[string]bool{"": true, "Y": true, "yes": true, "n": false} {
		opened := setUpdateGlobals(t, update.Info{Current: "1.0.0", Latest: "1.1.0", Available: true}, nil)
		app := runUpdateCheck(t)
		if app.mode != modePrompt {
			t.Fatalf("got mode %v, want the prompt", app.mode)
		}
		app.answerPrompt(answer)
		if got := len(*opened) == 1 && (*opened)[0] == update.ReleasesURL; got != wantOpened {
			t.Errorf("answer %q: opened %v", answer, *opened)
		}
	}
}

func TestCheckUpdatesSaysHowFarBehindGitBuildsAre(t *testing.T) {
	git := update.Info{Current: "0.9.4.r0.g49e499b", Latest: "5825ad4", Available: true, Behind: 73, URL: update.CommitsURL}
	withMethod := git
	withMethod.Method = "aur"
	setUpdateGlobals(t, withMethod, nil)
	app := runUpdateCheck(t)
	if want := "onon is 73 commits behind main: update it with aur"; app.message != want {
		t.Fatalf("got %q, want %q", app.message, want)
	}

	opened := setUpdateGlobals(t, git, nil)
	app = runUpdateCheck(t)
	if want := "onon is 73 commits behind main, open its commits? [Y/n]"; app.mode != modePrompt || app.promptState.label != want {
		t.Fatalf("got %q in mode %v, want %q", app.promptState.label, app.mode, want)
	}
	app.answerPrompt("")
	if len(*opened) != 1 || (*opened)[0] != update.CommitsURL {
		t.Fatalf("opened %v", *opened)
	}
}

func TestCheckUpdatesReportsUpToDate(t *testing.T) {
	setUpdateGlobals(t, update.Info{Current: "1.1.0", Latest: "1.1.0"}, nil)
	if app := runUpdateCheck(t); app.message != "onon 1.1.0 is up to date" {
		t.Fatalf("got %q", app.message)
	}
}

func TestCheckUpdatesReportsErrors(t *testing.T) {
	setUpdateGlobals(t, update.Info{Current: "1.0.0"}, errors.New("offline"))
	if app := runUpdateCheck(t); app.message != "update check failed: offline" {
		t.Fatalf("got %q", app.message)
	}
}

func TestCheckUpdatesSkipsDevBuilds(t *testing.T) {
	setUpdateGlobals(t, update.Info{Current: "dev"}, update.UnreleasedError{Version: "dev"})
	if app := runUpdateCheck(t); app.message != "dev builds are not checked for updates" {
		t.Fatalf("got %q", app.message)
	}
}
