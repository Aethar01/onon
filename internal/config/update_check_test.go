package config

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"onon/internal/update"
)

func runLuaUpdateCheck(t *testing.T, info update.Info, err error) *Runtime {
	t.Helper()
	previous := checkUpdates
	checkUpdates = func(context.Context) (update.Info, error) { return info, err }
	t.Cleanup(func() { checkUpdates = previous })
	dir := t.TempDir()
	rt, openErr := Open(filepath.Join(dir, "missing.lua"), filepath.Join(dir, "doc.pdf"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(rt.Close)
	if _, err := rt.Eval(`update_info = nil; onon.check_updates(function(info) update_info = info end)`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		rt.PollPluginOperations()
		if _, err := rt.Eval(`assert(update_info)`); err == nil {
			return rt
		}
		if time.Now().After(deadline) {
			t.Fatal("onon.check_updates never called back")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLuaCheckUpdatesReportsInfo(t *testing.T) {
	rt := runLuaUpdateCheck(t, update.Info{Current: "1.0.0", Latest: "1.1.0", Available: true, Method: "homebrew", URL: update.ReleasesURL}, nil)
	if _, err := rt.Eval(`
local i = update_info
assert(i.success == true and i.error == "", i.error)
assert(i.current == "1.0.0" and i.latest == "1.1.0" and i.available == true)
assert(i.method == "homebrew" and i.url == "` + update.ReleasesURL + `")
`); err != nil {
		t.Fatal(err)
	}
}

func TestLuaCheckUpdatesReportsUnreleasedBuilds(t *testing.T) {
	err := update.UnreleasedError{Version: "dev"}
	rt := runLuaUpdateCheck(t, update.Info{Current: "dev", URL: update.ReleasesURL}, err)
	if _, err := rt.Eval(`
local i = update_info
assert(i.success == false and i.available == false and i.latest == "")
assert(i.current == "dev" and i.error == "dev builds are not checked for updates", i.error)
`); err != nil {
		t.Fatal(err)
	}
}
