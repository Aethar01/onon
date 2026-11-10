package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func loadThemeTestConfig(t *testing.T, source string) (*Runtime, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := OpenWithOptions(path, "", OpenOptions{NoPlugins: true})
	if rt != nil {
		t.Cleanup(rt.Close)
	}
	return rt, err
}

func mustLoadThemeTestConfig(t *testing.T, source string) *Runtime {
	t.Helper()
	rt, err := loadThemeTestConfig(t, source)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func mustPreset(t *testing.T, name string) Theme {
	t.Helper()
	theme, ok := Preset(name)
	if !ok {
		t.Fatalf("no preset %s", name)
	}
	return theme
}

func TestDefaultThemeIsMoss(t *testing.T) {
	if got := Default().Theme; !reflect.DeepEqual(got, mustPreset(t, DefaultTheme)) {
		t.Fatalf("default theme = %+v, want the %s preset", got, DefaultTheme)
	}
	if Default().Theme.Base != DefaultTheme {
		t.Fatalf("default theme base = %q", Default().Theme.Base)
	}
}

func TestEveryPresetSetsEveryField(t *testing.T) {
	for _, name := range ThemeNames() {
		theme := mustPreset(t, name)
		if theme.Font.Size == 0 || theme.Font.Weight == 0 || theme.Font.Style == "" || theme.StatusBarStyle == "" {
			t.Errorf("%s leaves a font or status bar field unset: %+v", name, theme)
		}
		// Every colour is set, so none is accidentally black.
		cfg := Config{Theme: theme}
		for _, field := range themeFields {
			if field.desc.kind == "color" && field.desc.format(&cfg) == "#000000" && name != "classic" {
				t.Errorf("%s: %s is unset", name, field.name)
			}
		}
	}
}

func TestThemeTableStartsFromItsBase(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme = {
  base = "birch",
  accent = "#010203",
  alt = { accent = { 4, 5, 6 } },
  font = { size = 15 },
  radius = 3,
}
`)
	got := rt.Config().Theme
	want := mustPreset(t, "birch")
	want.Accent = [3]uint8{1, 2, 3}
	want.Alt.Accent = [3]uint8{4, 5, 6}
	want.Font.Size = 15
	want.Radius = 3
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %+v\nwant %+v", got, want)
	}
}

func TestThemeTableWithoutBaseStartsFromDefault(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme.radius = 1
onon.theme = { panel = "#ffffff" }
`)
	got := rt.Config().Theme
	want := mustPreset(t, DefaultTheme)
	want.Panel = [3]uint8{255, 255, 255}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assigning a table should replace the earlier theme: got %+v", got)
	}
}

func TestThemeFieldsCanBeSetOneByOne(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme = "classic"
onon.theme.accent = "#336633"
onon.theme.alt.panel = "#101010"
onon.theme.font.family = "Iosevka"
onon.theme.alt = { border = "#202020" }
onon.options.theme.shadow = true
assert(onon.theme.accent == "#336633", onon.theme.accent)
assert(onon.theme.alt.panel == "#101010")
assert(onon.theme.base == "classic")
assert(onon.options.theme.font.family == "Iosevka")
`)
	got := rt.Config().Theme
	want := mustPreset(t, "classic")
	want.Accent = [3]uint8{0x33, 0x66, 0x33}
	want.Alt.Panel = [3]uint8{0x10, 0x10, 0x10}
	want.Alt.Border = [3]uint8{0x20, 0x20, 0x20}
	want.Font.Family = "Iosevka"
	want.Shadow = true
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %+v\nwant %+v", got, want)
	}
}

// onon.theme holds no fields of its own, so assigning it back must keep
// the theme rather than copy nothing.
func TestThemeAssignedToItselfIsKept(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
local theme = onon.theme
theme.accent = "#123456"
onon.theme = theme
onon.theme = onon.options.theme
`)
	want := mustPreset(t, DefaultTheme)
	want.Accent = [3]uint8{0x12, 0x34, 0x56}
	if got := rt.Config().Theme; !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %+v\nwant %+v", got, want)
	}
}

func TestThemesTableGivesPresetsToChange(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
local mine = onon.themes.birch
mine.accent = "#123456"
mine.alt.accent = "#654321"
assert(onon.themes.birch.accent ~= "#123456", "onon.themes must give a fresh copy")
assert(onon.themes.jungle == nil)
onon.theme = mine
`)
	got := rt.Config().Theme
	want := mustPreset(t, "birch")
	want.Accent = [3]uint8{0x12, 0x34, 0x56}
	want.Alt.Accent = [3]uint8{0x65, 0x43, 0x21}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %+v\nwant %+v", got, want)
	}
}

func TestThemeTableRoundTrips(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	for _, name := range ThemeNames() {
		theme := mustPreset(t, name)
		cfg := Default()
		got, err := themeFromLua(&cfg, luaThemeTable(L, theme))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, theme) {
			t.Errorf("%s did not survive a trip through Lua: %+v", name, got)
		}
	}
}

func TestSetThemeAtRuntime(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, ``)
	if err := rt.SetOption("theme", "birch"); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().Theme; !reflect.DeepEqual(got, mustPreset(t, "birch")) {
		t.Fatalf(":set theme=birch gave %+v", got)
	}
	if err := rt.SetOption("theme.alt.accent", "#abcdef"); err != nil {
		t.Fatal(err)
	}
	if got, _ := rt.OptionValue("theme.alt.accent"); got != "#abcdef" {
		t.Fatalf("theme.alt.accent = %s", got)
	}
	if err := rt.ToggleOption("theme.shadow"); err != nil {
		t.Fatal(err)
	}
	if rt.Config().Theme.Shadow {
		t.Fatal("toggling theme.shadow left it on")
	}
	if got, _ := rt.OptionValue("theme"); got != "birch" {
		t.Fatalf("theme = %q, want its base", got)
	}
}

func TestThemeErrors(t *testing.T) {
	tests := []struct {
		name, lua, want string
	}{
		{"unknown preset", `onon.theme = "jungle"`, `unknown theme "jungle"; expected one of birch, classic, moss`},
		{"unknown base", `onon.theme = { base = "jungle" }`, `unknown theme "jungle"`},
		{"bad colour", `onon.theme.accent = "green"`, "onon.theme.accent: expected #RRGGBB or r,g,b"},
		{"read unknown", `local _ = onon.theme.nope`, "onon.theme.nope: unknown theme field"},
		{"moved option", `onon.options.ui_font_size = 12`, "ui_font_size moved to onon.theme.font.size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadThemeTestConfig(t, tt.lua)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}

	rt := mustLoadThemeTestConfig(t, ``)
	if err := rt.SetOption("selection_color", "#ffffff"); err == nil || !strings.Contains(err.Error(), "theme.selection") {
		t.Fatalf(":set of a moved option = %v, want it to name theme.selection", err)
	}
	// A rejected table leaves the theme as it was.
	if err := rt.SetOption("theme.accent", "#010101"); err != nil {
		t.Fatal(err)
	}
	before := rt.Config().Theme
	if err := rt.setOption("theme", rt.state.NewTable()); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(rt.Config().Theme, before) {
		t.Fatal("an empty table should reset the theme to the default")
	}
	tbl := rt.state.NewTable()
	tbl.RawSetString("accent", lua.LString("#020202"))
	tbl.RawSetString("panel", lua.LTrue)
	before = rt.Config().Theme
	if err := rt.setOption("theme", tbl); err == nil {
		t.Fatal("expected a bad value to be rejected")
	}
	if !reflect.DeepEqual(rt.Config().Theme, before) {
		t.Fatal("a rejected theme table changed the theme")
	}
}

func TestStatusBarStyleNormalizes(t *testing.T) {
	for raw, want := range map[string]string{"pill": "pill", " PILL ": "pill", "bar": "bar", "floating": "bar", "": "bar"} {
		if got := NormalizeBarStyle(raw); !reflect.DeepEqual(got, want) {
			t.Errorf("NormalizeBarStyle(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestThemeReferencesCoverEveryField(t *testing.T) {
	refs := ThemeReferences()
	if len(refs) != len(themeFields) {
		t.Fatalf("%d references for %d fields", len(refs), len(themeFields))
	}
	for _, ref := range refs {
		if ref.Description == "" {
			t.Errorf("theme.%s is undocumented", ref.Name)
		}
	}
	for _, ref := range OptionReferences() {
		if isThemeOption(ref.Name) {
			t.Errorf("option reference lists theme field %s", ref.Name)
		}
	}
}

// The generated example config writes the default theme out in full,
// commented out so that it does not replace the theme chosen with :theme;
// uncommented, it must give the default theme back.
func TestExampleConfigThemeIsTheDefault(t *testing.T) {
	example := filepath.Join("..", "..", "config.example.lua")
	rt, err := OpenWithOptions(example, "", OpenOptions{NoPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if rt.ConfigSetsTheme() {
		t.Fatal("the example config assigns the theme")
	}

	data, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	var theme []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r") // as Windows checks it out
		if strings.HasPrefix(line, "-- onon.theme = {") || len(theme) > 0 {
			theme = append(theme, strings.TrimPrefix(line, "-- "))
			if line == "-- }" {
				break
			}
		}
	}
	if len(theme) == 0 {
		t.Fatal("no theme in the example config")
	}
	uncommented := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(uncommented, []byte(strings.Join(theme, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err = OpenWithOptions(uncommented, "", OpenOptions{NoPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if got := rt.Config().Theme; !reflect.DeepEqual(got, Default().Theme) {
		t.Fatalf("example theme = %+v\nwant %+v", got, Default().Theme)
	}
}

// Lua files beside the config can be required, so a theme can live in its
// own file, whether it returns its table or sets the theme itself.
func TestConfigRequiresThemeFilesBesideIt(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("themes/forest.lua", `return { base = "birch", accent = "#2f5d3a", alt = { accent = "#8fbf8f" } }`)
	write("fern/init.lua", `onon.theme.radius = 3`)
	write("config.lua", `
onon.theme = require("themes.forest")
require("fern")
`)
	// The search must not depend on the working directory.
	t.Chdir(t.TempDir())
	rt, err := OpenWithOptions(filepath.Join(dir, "config.lua"), "", OpenOptions{NoPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	want := mustPreset(t, "birch")
	want.Accent = [3]uint8{0x2f, 0x5d, 0x3a}
	want.Alt.Accent = [3]uint8{0x8f, 0xbf, 0x8f}
	want.Radius = 3
	if got := rt.Config().Theme; !reflect.DeepEqual(got, want) {
		t.Fatalf("theme = %+v\nwant %+v", got, want)
	}
}

// A theme written for a newer onon, with fields this one does not know,
// loads without them, and they are reported.
func TestUnknownThemeFieldsAreSkipped(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme = {
  accent = "#123456",
  glow = 3,
  alt = { sparkle = true },
  elements = { panel = { radius = 2, wobble = 1 }, sidebar = { fill = "accent" }, prompt = { radius = 4 } },
}
onon.theme.newthing = 1
onon.theme.elements.pannel = { radius = 2 }
`)
	theme := rt.Config().Theme
	if theme.Accent != [3]uint8{0x12, 0x34, 0x56} || theme.Style(ElementPanel).Radius.V != corners(2) {
		t.Fatalf("known fields were not applied: accent %v, panel radius %v", theme.Accent, theme.Style(ElementPanel).Radius.V)
	}
	warnings := strings.Join(rt.TakeWarnings(), "\n")
	for _, name := range []string{"glow", "alt.sparkle", "elements.panel.wobble", "elements.sidebar", "elements.prompt.radius", "newthing", "elements.pannel"} {
		if !strings.Contains(warnings, name) {
			t.Errorf("warnings do not name %s:\n%s", name, warnings)
		}
	}
	if len(rt.TakeWarnings()) != 0 {
		t.Fatal("warnings were not cleared once taken")
	}
	// :set still refuses a field it does not know.
	if err := rt.SetOption("theme.glow", "3"); err == nil {
		t.Fatal(":set theme.glow succeeded")
	}
}

func TestAltColorsCanFollowTheSystem(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.options.alt_colors = "system"
assert(onon.options.alt_colors == "system")
`)
	if cfg := rt.Config(); !cfg.AltColorsSystem {
		t.Fatalf("alt_colors = %+v", cfg.AltColors)
	}
	if err := rt.SetOption("alt_colors", "true"); err != nil {
		t.Fatal(err)
	}
	if cfg := rt.Config(); !cfg.AltColors || cfg.AltColorsSystem {
		t.Fatalf("alt_colors=true left system %v, on %v", cfg.AltColorsSystem, cfg.AltColors)
	}
	if err := rt.SetOption("alt_colors", "system"); err != nil {
		t.Fatal(err)
	}
	if got, _ := rt.OptionValue("alt_colors"); got != `"system"` {
		t.Fatalf("alt_colors = %s", got)
	}
	if err := rt.SetOption("alt_colors", "sometimes"); err == nil || !strings.Contains(err.Error(), `"system"`) {
		t.Fatalf("bad alt_colors: %v", err)
	}
	// Toggling "system" cannot know which way the OS is, so it refuses
	// rather than guess.
	if err := rt.ToggleOption("alt_colors"); err == nil || !rt.Config().AltColorsSystem {
		t.Fatalf("toggling system: %v, still system %v", err, rt.Config().AltColorsSystem)
	}
}
