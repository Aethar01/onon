package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openSchemeConfig(t *testing.T, lua string) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(path, []byte(lua), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, err := Open(path, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	return rt
}

func TestRegisteredSchemesResolve(t *testing.T) {
	rt := openSchemeConfig(t, `
onon.register_scheme("arxiv", function(id, whole)
  return "https://arxiv.org/pdf/" .. id .. "#page=" .. (whole == "arXiv:1234.5678" and 3 or 1)
end)
onon.register_scheme("paper", function(name) return "arxiv:" .. name end)
onon.register_scheme("ping", function() return "pong:x" end)
onon.register_scheme("pong", function() return "ping:x" end)
onon.register_scheme("none", function() return nil end)
onon.register_scheme("boom", function() error("no such paper") end)
`)
	for arg, want := range map[string]string{
		"arXiv:1234.5678":   "https://arxiv.org/pdf/1234.5678#page=3",
		"arxiv://2401.0001": "https://arxiv.org/pdf/2401.0001#page=1",
		"paper:2401.0002":   "https://arxiv.org/pdf/2401.0002#page=1",
	} {
		got, ok, err := rt.ResolveScheme(arg)
		if err != nil || !ok || got != want {
			t.Errorf("ResolveScheme(%q) = %q, %t, %v; want %q", arg, got, ok, err, want)
		}
	}
	if got, ok, err := rt.ResolveScheme("host:doc.pdf"); ok || err != nil || got != "host:doc.pdf" {
		t.Errorf("unregistered = %q, %t, %v", got, ok, err)
	}
	for arg, want := range map[string]string{"ping:x": "loop", "none:x": "gave nothing", "boom:x": "no such paper"} {
		if _, _, err := rt.ResolveScheme(arg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ResolveScheme(%q) err = %v, want %q", arg, err, want)
		}
	}
}

func TestBuiltInSchemesCannotBeRegistered(t *testing.T) {
	for _, lua := range []string{
		`onon.register_scheme("https", function() return "x" end)`,
		`onon.register_scheme("c", function() return "x" end)`,
		`onon.register_scheme("no spaces", function() return "x" end)`,
	} {
		path := filepath.Join(t.TempDir(), "config.lua")
		if err := os.WriteFile(path, []byte(lua), 0o600); err != nil {
			t.Fatal(err)
		}
		rt, err := Open(path, "")
		if err == nil {
			rt.Close()
			t.Errorf("%s was accepted", lua)
		}
	}
}
