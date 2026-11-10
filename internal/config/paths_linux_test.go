//go:build linux

package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// isolateSystemDir keeps a test from finding plugins and themes installed
// on the machine running it.
func isolateSystemDir(t *testing.T) {
	t.Helper()
	old := systemDir
	systemDir = t.TempDir()
	t.Cleanup(func() { systemDir = old })
}

func TestLinuxSearchPaths(t *testing.T) {
	t.Setenv("HOME", "/home/reader")
	t.Setenv("XDG_DATA_HOME", "/data")
	t.Setenv("XDG_CONFIG_HOME", "/config")
	t.Setenv("XDG_CONFIG_DIRS", "/etc/xdg:/opt/xdg")
	for name, got := range map[string][]string{"plugins": PluginPaths(), "themes": ThemePaths()} {
		want := []string{
			"/usr/lib/onon/" + name,
			"/data/onon/" + name,
			"/config/onon/" + name,
			"/home/reader/.config/onon/" + name,
			"/etc/xdg/onon/" + name,
			"/opt/xdg/onon/" + name,
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s paths = %v\nwant %v", name, got, want)
		}
	}

	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CONFIG_DIRS", "")
	want := []string{
		"/usr/lib/onon/themes",
		"/home/reader/.local/share/onon/themes",
		"/home/reader/.config/onon/themes",
	}
	if got := ThemePaths(); !slices.Equal(got, want) {
		t.Errorf("theme paths without XDG variables = %v\nwant %v", got, want)
	}
}

// Plugins in different directories all load: one under ~/.local/share and
// one under ~/.config, say.
func TestPluginsLoadFromEverySearchPath(t *testing.T) {
	setTestDataDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	for id, base := range map[string]string{
		"local":  filepath.Join(home, ".local", "share", "onon", "plugins"),
		"user":   filepath.Join(home, ".config", "onon", "plugins"),
		"system": filepath.Join(systemDir, "plugins"),
	} {
		root := writeTestPlugin(t, id, `return onon.plugin.register("`+id+`")`)
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(root, id), filepath.Join(base, id)); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("local"); require("user"); require("system")`); err != nil {
		t.Fatal(err)
	}
}

// Themes are found in the same directories as plugins, after the one beside
// config.lua, which wins when two have a theme of the same name.
func TestThemesLoadFromEverySearchPath(t *testing.T) {
	setTestDataDir(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	write := func(dir, name, source string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".lua"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	configDir := t.TempDir()
	write(filepath.Join(configDir, "themes"), "forest", `return { accent = "#111111" }`)
	write(filepath.Join(home, ".local", "share", "onon", "themes"), "dusk", `return { accent = "#222222" }`)
	write(filepath.Join(home, ".config", "onon", "themes"), "forest", `return { accent = "#333333" }`)
	write(filepath.Join(systemDir, "themes"), "slate", `return { accent = "#444444" }`)
	rt := openThemeChoiceRuntime(t, configDir, "", nil)

	var names []string
	for _, choice := range rt.ThemeChoices() {
		if choice.Path != "" {
			names = append(names, choice.Name)
		}
	}
	if got := strings.Join(names, " "); got != "dusk forest slate" {
		t.Fatalf("theme files %q", got)
	}
	for name, accent := range map[string]string{"forest": "#111111", "dusk": "#222222", "slate": "#444444"} {
		if err := rt.PreviewTheme(name); err != nil {
			t.Fatal(err)
		}
		if got := rt.Config().Theme.Accent; got != hex(accent) {
			t.Errorf("%s: accent %v, want %s", name, got, accent)
		}
	}
}
