package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openThemeChoiceRuntime opens a runtime with config.lua holding source in
// dir, after writing each theme file into dir/themes.
func openThemeChoiceRuntime(t *testing.T, dir, source string, themes map[string]string) *Runtime {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.lua"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(themes) > 0 {
		if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, theme := range themes {
		if err := os.WriteFile(filepath.Join(dir, "themes", name+".lua"), []byte(theme), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := Open(filepath.Join(dir, "config.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	return rt
}

func TestChosenThemeIsKeptUnderTheConfigsChanges(t *testing.T) {
	setTestDataDir(t)
	dir := t.TempDir()
	config := `onon.theme.accent = "#112233"`
	rt := openThemeChoiceRuntime(t, dir, config, nil)
	birch := mustPreset(t, "birch")

	// A preview has config.lua's changes, as the theme will on restart.
	if err := rt.PreviewTheme("birch"); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().Theme; got.Background != birch.Background || got.Accent != hex("#112233") {
		t.Fatalf("preview: background %v accent %v", got.Background, got.Accent)
	}
	if rt.ChosenTheme() != "" {
		t.Fatalf("a preview was kept as %q", rt.ChosenTheme())
	}
	if err := rt.ChooseTheme("birch"); err != nil {
		t.Fatal(err)
	}
	for _, rt := range []*Runtime{rt, openThemeChoiceRuntime(t, dir, config, nil)} {
		if err := rt.Reload(); err != nil {
			t.Fatal(err)
		}
		got := rt.Config().Theme
		if got.Background != birch.Background || got.Accent != hex("#112233") || rt.ChosenTheme() != "birch" {
			t.Fatalf("after reload: background %v accent %v chosen %q", got.Background, got.Accent, rt.ChosenTheme())
		}
		if rt.ConfigSetsTheme() {
			t.Fatal("changing a field counted as setting the theme")
		}
	}
	// --no-config uses the defaults, with no theme chosen.
	rt, err := OpenWithOptions(filepath.Join(dir, "config.lua"), "", OpenOptions{NoConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if got := rt.Config().Theme.Background; got != mustPreset(t, DefaultTheme).Background {
		t.Fatalf("--no-config background %v", got)
	}
}

func TestConfigAssigningTheThemeOutranksTheChoice(t *testing.T) {
	setTestDataDir(t)
	dir := t.TempDir()
	if err := setSetting(themeChoiceSetting, "birch"); err != nil {
		t.Fatal(err)
	}
	rt := openThemeChoiceRuntime(t, dir, `onon.theme.accent = "#445566"
onon.theme = "classic"
onon.theme.muted = "#778899"`, nil)
	got := rt.Config().Theme
	if got.Background != mustPreset(t, "classic").Background || !rt.ConfigSetsTheme() {
		t.Fatalf("background %v, config sets theme %v", got.Background, rt.ConfigSetsTheme())
	}
	// Previewing keeps only the changes made after the theme was assigned.
	if err := rt.PreviewTheme("birch"); err != nil {
		t.Fatal(err)
	}
	got = rt.Config().Theme
	if got.Muted != hex("#778899") || got.Accent != mustPreset(t, "birch").Accent {
		t.Fatalf("preview muted %v accent %v", got.Muted, got.Accent)
	}
}

func TestThemeFilesCanBeChosen(t *testing.T) {
	setTestDataDir(t)
	dir := t.TempDir()
	themes := map[string]string{
		"forest": `return { base = "birch", accent = "#2f5d3a" }`,
		"named":  `return "classic"`,
		"birch":  `return { accent = "#000000" }`,
		"broken": `return 42`,
	}
	rt := openThemeChoiceRuntime(t, dir, "", themes)
	var names []string
	for _, choice := range rt.ThemeChoices() {
		names = append(names, choice.Name)
	}
	if got, want := strings.Join(names, " "), "birch classic moss broken forest named"; got != want {
		t.Fatalf("choices %q, want %q", got, want)
	}
	if err := rt.ChooseTheme("forest"); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().Theme; got.Accent != hex("#2f5d3a") || got.Background != mustPreset(t, "birch").Background {
		t.Fatalf("forest: accent %v background %v", got.Accent, got.Background)
	}
	if err := rt.PreviewTheme("named"); err != nil || rt.Config().Theme.Background != mustPreset(t, "classic").Background {
		t.Fatalf("a file naming a built-in: %v", err)
	}
	if err := rt.PreviewTheme("broken"); err == nil {
		t.Fatal("a file returning a number loaded")
	}
	if err := rt.PreviewTheme("missing"); err == nil {
		t.Fatal("an unknown theme loaded")
	}

	// The choice is kept, and a file since removed leaves the default.
	if got := openThemeChoiceRuntime(t, dir, "", nil).Config().Theme.Accent; got != hex("#2f5d3a") {
		t.Fatalf("after restart accent %v", got)
	}
	if err := os.Remove(filepath.Join(dir, "themes", "forest.lua")); err != nil {
		t.Fatal(err)
	}
	rt = openThemeChoiceRuntime(t, dir, "", nil)
	if got := rt.Config().Theme.Accent; got != mustPreset(t, DefaultTheme).Accent {
		t.Fatalf("with the file gone accent %v", got)
	}
	if warnings := rt.TakeWarnings(); len(warnings) == 0 || !strings.Contains(warnings[0], "forest") {
		t.Fatalf("warnings %q", warnings)
	}
}

func TestThemeFileShaderIsBesideIt(t *testing.T) {
	setTestDataDir(t)
	dir := t.TempDir()
	rt := openThemeChoiceRuntime(t, dir, "", map[string]string{
		"frosty": `return { elements = { panel = { shader = "frost.glsl" }, hint = { shader = "/abs/hint.glsl" }, row = { shader = false } } }`,
	})
	if err := rt.PreviewTheme("frosty"); err != nil {
		t.Fatal(err)
	}
	theme := rt.Config().Theme
	if got, want := theme.Style(ElementPanel).Shader.V, filepath.Join(dir, "themes", "frost.glsl"); got != want {
		t.Errorf("panel shader = %q, want %q", got, want)
	}
	if got := theme.Style(ElementHint).Shader.V; got != "/abs/hint.glsl" {
		t.Errorf("hint shader = %q, want its absolute path", got)
	}
	if got := theme.Style(ElementRow).Shader.V; got != "" {
		t.Errorf("row shader = %q, want none", got)
	}
}
