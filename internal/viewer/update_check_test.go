package viewer

import (
	"context"
	"errors"
	"testing"
	"time"

	"onon/internal/config"
	"onon/internal/update"
)

func runUpdateCheck(t *testing.T, check func(context.Context, string, string) (string, error)) string {
	t.Helper()
	previous := checkForUpdates
	checkForUpdates = check
	t.Cleanup(func() { checkForUpdates = previous })
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
	return app.message
}

func TestCheckUpdatesReportsResult(t *testing.T) {
	previous := config.InstallMethod
	config.InstallMethod = "aur"
	t.Cleanup(func() { config.InstallMethod = previous })
	var gotMethod string
	msg := runUpdateCheck(t, func(_ context.Context, _, method string) (string, error) {
		gotMethod = method
		return "onon 9.9.9 is available", nil
	})
	if msg != "onon 9.9.9 is available" || gotMethod != "aur" {
		t.Fatalf("got %q with method %q", msg, gotMethod)
	}
}

func TestCheckUpdatesReportsErrors(t *testing.T) {
	msg := runUpdateCheck(t, func(context.Context, string, string) (string, error) {
		return "", errors.New("offline")
	})
	if msg != "update check failed: offline" {
		t.Fatalf("got %q", msg)
	}
}

func TestCheckUpdatesSkipsDevBuilds(t *testing.T) {
	msg := runUpdateCheck(t, func(context.Context, string, string) (string, error) {
		return "", update.ErrUnreleased
	})
	if msg != config.Version+" builds are not checked for updates" {
		t.Fatalf("got %q", msg)
	}
}
