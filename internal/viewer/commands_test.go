package viewer

import (
	"path/filepath"
	"slices"
	"testing"

	"onon/internal/config"
)

func TestRunCommandValidationPaths(t *testing.T) {
	app := testLayoutApp(3)
	app.runCommand(":page")
	if app.message != "usage: :page <n>" {
		t.Fatalf("expected page usage message, got %q", app.message)
	}

	app.message = ""
	app.runCommand(":page nope")
	if app.message != "invalid page or label: nope" {
		t.Fatalf("expected invalid page message, got %q", app.message)
	}

	app.message = ""
	app.runCommand(":mode")
	if app.message != "usage: :mode continuous|single" {
		t.Fatalf("expected mode usage message, got %q", app.message)
	}

	app.message = ""
	app.runCommand(":colors")
	if app.message != "usage: :colors normal|alt" {
		t.Fatalf("expected colors usage message, got %q", app.message)
	}

	app.message = ""
	app.runCommand(":wat")
	if app.message != "unknown command: wat" {
		t.Fatalf("expected unknown command message, got %q", app.message)
	}

	app.runCommand(":quit")
	if !app.quit {
		t.Fatal("expected quit command to set quit flag")
	}
}

func TestVersionCommandShowsBuildVersion(t *testing.T) {
	previous := config.Version
	config.Version = "v1.2.3"
	defer func() { config.Version = previous }()
	app := testLayoutApp(1)
	app.runCommand(":version")
	if app.message != "onon v1.2.3" {
		t.Fatalf("expected version message, got %q", app.message)
	}
}

func TestGotoPageInputAcceptsPageLabel(t *testing.T) {
	app := testLayoutApp(4)
	app.pageMetrics[0].label = "i"
	app.pageMetrics[1].label = "ii"
	app.pageMetrics[2].label = "1"
	app.pageMetrics[3].label = "A-2"

	if page, ok := app.resolvePageInput("a-2"); !ok || page != 3 {
		t.Fatalf("expected label A-2 to resolve to page 4, got %d, %t", page+1, ok)
	}
	if page, ok := app.resolvePageInput("1"); !ok || page != 2 {
		t.Fatalf("expected numeric label 1 to resolve to page 3, got %d, %t", page+1, ok)
	}
}

func TestRunCommandAppliesViewerSettings(t *testing.T) {
	app := testLayoutApp(4)
	app.winW = 800
	app.winH = 600
	app.recomputeLayout(app.viewportSize())
	app.page = 2

	app.runCommand(":mode single")
	if app.renderMode != renderSingle || app.page != 2 {
		t.Fatalf("expected :mode single to preserve page 2, mode=%q page=%d", app.renderMode, app.page)
	}

	app.runCommand(":mode sideways")
	if app.renderMode != renderContinuous {
		t.Fatalf("expected invalid render mode to fall back to continuous, got %q", app.renderMode)
	}

	app.runCommand(":fit width")
	if app.fitMode != fitWidth {
		t.Fatalf("expected :fit width to set fit mode, got %q", app.fitMode)
	}

	app.runCommand(":fit unknown")
	if app.fitMode != fitPage {
		t.Fatalf("expected invalid fit mode to fall back to page, got %q", app.fitMode)
	}

	app.runCommand(":colors alt")
	if !app.altColors {
		t.Fatal("expected :colors alt to enable alternate colors")
	}

	app.runCommand(":colors normal")
	if app.altColors {
		t.Fatal("expected :colors normal to disable alternate colors")
	}
}

func TestSetCommandInspectsAndAssignsRegisteredOptions(t *testing.T) {
	rt, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app := testLayoutApp(3)
	app.runtime = rt
	app.applyConfigState(rt.Config())

	app.runCommand(":set scroll_step=80")
	if app.config.ScrollStep != 80 || app.pageStep != 80 || app.message != "scroll_step=80" {
		t.Fatalf("expected applied scroll_step, config=%d step=%v message=%q", app.config.ScrollStep, app.pageStep, app.message)
	}
	app.runCommand(":set invert_scroll={ trackpad = true }")
	if app.config.InvertScrollSources != config.SmoothInputTrackpad || app.message != "invert_scroll={ mouse = false, trackpad = true }" {
		t.Fatalf("expected trackpad-only invert_scroll, value=%q message=%q", config.FormatSmoothInputSources(app.config.InvertScrollSources), app.message)
	}
	app.runCommand(":set fit_mode?")
	if app.message != `fit_mode="page"` {
		t.Fatalf("expected inspected fit_mode, got %q", app.message)
	}
	app.runCommand(":set")
	view := app.activeUIView()
	if view == nil || view.title != "Options" || len(view.rows) != len(config.OptionNames()) {
		t.Fatalf("expected options inspector, view=%+v", view)
	}
	if dirty, _ := rt.ConsumeDirty(); dirty {
		t.Fatal("expected :set changes to consume runtime dirty state")
	}
}

func TestToggleAltColorsFromSystemFlipsWhatIsShown(t *testing.T) {
	rt, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app := testLayoutApp(3)
	app.runtime = rt
	app.runCommand(`:set alt_colors="system"`)
	if !app.config.AltColorsSystem {
		t.Fatal("alt_colors did not follow the system")
	}
	app.altColors = false // as with the OS in light mode

	app.runCommand(":set alt_colors!")
	if app.config.AltColorsSystem || !app.config.AltColors || !app.altColors || app.message != "alt_colors=true" {
		t.Fatalf("toggled from a light system: system %v, config %v, shown %v, message %q",
			app.config.AltColorsSystem, app.config.AltColors, app.altColors, app.message)
	}
	app.runCommand(":set alt_colors!")
	if app.config.AltColors || app.altColors {
		t.Fatalf("toggled back: config %v, shown %v", app.config.AltColors, app.altColors)
	}
}

func TestLuaCommandAppliesChangedOptions(t *testing.T) {
	app := testLayoutApp(4)
	app.winW = 800
	app.winH = 600
	app.dualPage = true
	app.recomputeLayout(app.viewportSize())

	rt, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app.runtime = rt
	rt.AttachHost(app)

	app.runCommand(":lua onon.options.dual_page = false")
	if app.dualPage {
		t.Fatal("expected inline lua option assignment to disable dual-page mode")
	}
}

func TestRunCommandSearchAndOpenMessages(t *testing.T) {
	app := &App{}
	app.runCommand(":search needle")
	if app.message != "no document open" {
		t.Fatalf("expected search without document message, got %q", app.message)
	}

	app.runCommand(":open")
	if app.message != "usage: :open <filename>" {
		t.Fatalf("expected open usage message, got %q", app.message)
	}

	app.runCommand(":help")
	view := app.activeUIView()
	if view == nil {
		t.Fatal("expected help command to open the help window")
	}
	if view.title != "Help" {
		t.Fatalf("expected help window title, got %q", view.title)
	}
	if len(view.rows) == 0 {
		t.Fatal("expected help window rows")
	}
	if !slices.ContainsFunc(view.rows, func(row uiRow) bool {
		return row.text == ":open_file_picker" && row.secondary == "Open the PDF file picker"
	}) {
		t.Fatal("expected help rows to include open_file_picker command")
	}
}

func TestRecentFilesCommandReportsDisabledDatabase(t *testing.T) {
	app := &App{config: config.Config{SessionDatabase: false}}
	app.runCommand(":recent")
	if app.message != "session database disabled" {
		t.Fatalf("expected disabled database message, got %q", app.message)
	}
}

func TestConfigChangesKeepActionSettingsUnlessAssigned(t *testing.T) {
	rt, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app := testLayoutApp(4)
	app.winW, app.winH = 800, 600
	app.runtime = rt
	rt.AttachHost(app)
	app.applyConfigState(rt.Config())
	app.cache.add(testTile(0, 1, 0, 0, 16))

	app.runAction("toggle_dual_page")
	app.runCommand(":set scroll_step=80")
	app.runCommand(":lua onon.o.status_bar_right = '{page}'")
	if !app.dualPage {
		t.Fatal("unrelated config changes undid dual-page mode set by an action")
	}
	if len(app.cache.entries) != 1 {
		t.Fatal("config changes that do not affect rendering dropped the tiles")
	}
	app.runCommand(":set dual_page=false")
	if app.dualPage {
		t.Fatal("assigning dual_page its current config value did not apply it")
	}
}

func TestSetStatusBarVisibleAppliesAfterToggle(t *testing.T) {
	rt, err := config.Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	app := testLayoutApp(4)
	app.winW, app.winH = 800, 600
	app.runtime = rt
	rt.AttachHost(app)
	app.applyConfigState(rt.Config())

	app.runAction("toggle_status_bar") // hidden, while the config still says shown
	app.runCommand(":set status_bar_visible=true")
	if !app.statusBarShown {
		t.Fatal(":set status_bar_visible=true did not show the status bar")
	}
	app.runCommand(":set status_bar_visible=false")
	if app.statusBarShown {
		t.Fatal(":set status_bar_visible=false did not hide the status bar")
	}
}

func TestOptionsListSwitchesAndSetsOptions(t *testing.T) {
	app := luaUITestApp(t, "")
	app.config = app.runtime.Config()
	app.runSet("")
	view := app.activeModalUIView()
	find := func(name string) uiRow {
		for _, row := range view.rows {
			if row.value == name {
				return row
			}
		}
		t.Fatalf("no row for %s", name)
		return uiRow{}
	}
	row := find("copy_on_select")
	before := row.secondary
	view.selected = row.index
	app.activateUIView(view)
	if after := find("copy_on_select").secondary; after == before || app.activeModalUIView() != view || view.selected != row.index {
		t.Fatalf("Enter on copy_on_select: %s to %s, view %v, selected %d", before, after, app.activeModalUIView(), view.selected)
	}

	row = find("fit_mode")
	view.selected = row.index
	app.activateUIView(view)
	if app.activeModalUIView() != nil || app.mode != modeCommand || app.input.Value != "set fit_mode="+row.secondary {
		t.Fatalf("Enter on fit_mode: mode %v, input %q", app.mode, app.input.Value)
	}
}

func TestRecentFilePartsNameTheFileFirst(t *testing.T) {
	home := string(filepath.Separator) + "h"
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tc := range []struct{ path, name, folder string }{
		{filepath.Join(home, "papers", "a.pdf"), "a.pdf", filepath.Join("~", "papers")},
		{filepath.Join(home+"x", "b.pdf"), "b.pdf", home + "x"},
		{"https://example.com/docs/c.pdf", "c.pdf", "https://example.com/docs"},
		{filepath.Join(home, "a", "b", "c", "d", "e.pdf"), "e.pdf", filepath.Join("…", "c", "d")},
	} {
		if name, folder := recentFileParts(tc.path); name != tc.name || folder != tc.folder {
			t.Errorf("recentFileParts(%q) = %q, %q; want %q, %q", tc.path, name, folder, tc.name, tc.folder)
		}
	}
}
