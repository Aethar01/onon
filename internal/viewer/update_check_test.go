package viewer

import (
	"context"
	"errors"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/update"
)

func setUpdateGlobals(t *testing.T, version, method string, result update.Result, err error) *[]string {
	t.Helper()
	previousCheck, previousMethod, previousOpen := checkForUpdates, installMethod, openReleasesPage
	previousVersion := config.Version
	t.Cleanup(func() {
		checkForUpdates, installMethod, openReleasesPage = previousCheck, previousMethod, previousOpen
		config.Version = previousVersion
	})
	config.Version = version
	installMethod = func() string { return method }
	checkForUpdates = func(context.Context, string) (update.Result, error) { return result, err }
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
	setUpdateGlobals(t, "1.0.0", "aur", update.Result{Latest: "1.1.0", Available: true}, nil)
	app := runUpdateCheck(t)
	if want := "onon 1.1.0 is available: update it with aur"; app.message != want || app.mode != modeNormal {
		t.Fatalf("got %q in mode %v, want %q", app.message, app.mode, want)
	}
}

func TestCheckUpdatesOffersReleasesPageForDownloads(t *testing.T) {
	for answer, wantOpened := range map[string]bool{"": true, "Y": true, "yes": true, "n": false} {
		opened := setUpdateGlobals(t, "1.0.0", "", update.Result{Latest: "1.1.0", Available: true}, nil)
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

func TestCheckUpdatesReportsUpToDate(t *testing.T) {
	setUpdateGlobals(t, "1.1.0", "", update.Result{Latest: "1.1.0"}, nil)
	if app := runUpdateCheck(t); app.message != "onon 1.1.0 is up to date" {
		t.Fatalf("got %q", app.message)
	}
}

func TestCheckUpdatesReportsErrors(t *testing.T) {
	setUpdateGlobals(t, "1.0.0", "", update.Result{}, errors.New("offline"))
	if app := runUpdateCheck(t); app.message != "update check failed: offline" {
		t.Fatalf("got %q", app.message)
	}
}

func TestCheckUpdatesSkipsDevBuilds(t *testing.T) {
	setUpdateGlobals(t, "dev", "", update.Result{}, update.ErrUnreleased)
	if app := runUpdateCheck(t); app.message != "dev builds are not checked for updates" {
		t.Fatalf("got %q", app.message)
	}
}
