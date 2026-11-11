package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// The theme picked with :theme is kept in the session database and applied
// over the defaults before config.lua runs. Assigning onon.theme whole in
// config.lua replaces it, as config.lua has the last word; changing single
// fields, as onon.theme.accent = "#335533" does, changes the picked theme.
// Those changes are recorded as config.lua makes them, so a theme previewed
// later gets them too, and looks as it will once onon restarts.

// themeChoiceSetting names the picked theme in the settings table.
const themeChoiceSetting = "theme"

// themeOp is one change config.lua made to the theme: a field set by
// setThemeField, or a theme option such as "theme.accent".
type themeOp struct {
	field bool
	name  string
	value lua.LValue
}

// ThemeChoice is a theme :theme offers: a built-in, or a file in one of the
// theme directories.
type ThemeChoice struct {
	Name string
	Path string // the file it is loaded from; empty for a built-in
}

// recordThemeOp notes a change config.lua makes to the theme. Assigning the
// theme whole undoes the changes before it, so they are dropped.
func (r *Runtime) recordThemeOp(op themeOp) {
	if !r.loadingConfig {
		return
	}
	if !op.field && op.name == "theme" {
		r.configSetsTheme = true
		r.themeOps = nil
		return
	}
	r.themeOps = append(r.themeOps, op)
}

// ConfigSetsTheme reports whether config.lua assigns onon.theme whole, so
// that a theme picked with :theme lasts only until onon restarts.
func (r *Runtime) ConfigSetsTheme() bool { return r.configSetsTheme }

// ChosenTheme is the theme picked with :theme, or "" when none is.
func (r *Runtime) ChosenTheme() string { return r.themeChoice }

// themeDirs are the directories searched for theme files: the one beside
// config.lua, or beside where config.lua would be when there is none, and
// then ThemePaths. A name found in more than one means the first.
func (r *Runtime) themeDirs() []string {
	var dirs []string
	if r.cfg.ConfigPath != "" {
		dirs = append(dirs, filepath.Join(filepath.Dir(r.cfg.ConfigPath), "themes"))
	} else if paths := candidatePaths(r.explicitPath); len(paths) > 0 {
		dirs = append(dirs, filepath.Join(filepath.Dir(paths[0]), "themes"))
	}
	return unique(append(dirs, ThemePaths()...))
}

// ThemeChoices lists the built-in themes and then those in the theme
// directories, each named for its file. A file named for a built-in is left
// out, as the name means the built-in everywhere else.
func (r *Runtime) ThemeChoices() []ThemeChoice {
	var choices []ThemeChoice
	for _, name := range ThemeNames() {
		choices = append(choices, ThemeChoice{Name: name})
	}
	var files []ThemeChoice
	seen := map[string]bool{}
	for _, dir := range r.themeDirs() {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.lua"))
		for _, path := range paths {
			name := strings.TrimSuffix(filepath.Base(path), ".lua")
			if _, builtin := Preset(name); builtin || seen[name] {
				continue
			}
			seen[name] = true
			files = append(files, ThemeChoice{Name: name, Path: path})
		}
	}
	slices.SortFunc(files, func(a, b ThemeChoice) int { return strings.Compare(a.Name, b.Name) })
	return append(choices, files...)
}

// loadTheme returns the theme name stands for: a built-in, or what the
// first file of that name in the theme directories returns, a theme table
// or a built-in's name.
func (r *Runtime) loadTheme(name string) (Theme, error) {
	name = strings.TrimSpace(name)
	if theme, ok := Preset(name); ok {
		return theme, nil
	}
	if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return Theme{}, fmt.Errorf("unknown theme %q", name)
	}
	dirs := r.themeDirs()
	path := ""
	for _, dir := range dirs {
		candidate := filepath.Join(dir, name+".lua")
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
			break
		} else if !os.IsNotExist(err) {
			return Theme{}, err
		}
	}
	if path == "" {
		return Theme{}, fmt.Errorf("unknown theme %q: neither built in nor in %s", name, strings.Join(dirs, ", "))
	}
	L := r.state
	if L == nil {
		L = r.initLuaState()
	}
	top := L.GetTop()
	defer L.SetTop(top)
	if err := L.DoFile(path); err != nil {
		return Theme{}, err
	}
	if L.GetTop() == top {
		return Theme{}, fmt.Errorf("%s: returns no theme", path)
	}
	theme, err := themeFromLua(&r.cfg, L.Get(top+1))
	if err != nil && !isSkipped(err) {
		return Theme{}, fmt.Errorf("%s: %w", path, err)
	} else if err != nil {
		r.warn(err)
	}
	// A shader is beside the theme's file unless it says where it is.
	for i := range theme.Elements {
		if s := &theme.Elements[i].Shader; s.V != "" && !filepath.IsAbs(s.V) {
			s.V = filepath.Join(filepath.Dir(path), s.V)
		}
	}
	return theme, nil
}

// useTheme makes theme the theme, with config.lua's changes to it.
func (r *Runtime) useTheme(theme Theme) {
	r.cfg.Theme = theme
	ops := r.themeOps
	loading := r.loadingConfig
	r.loadingConfig = false // replaying is not config.lua changing it again
	defer func() { r.loadingConfig = loading }()
	for _, op := range ops {
		var err error
		if op.field {
			err = r.setThemeField(op.name, op.value)
		} else {
			err = r.setOption(op.name, op.value)
		}
		if err != nil {
			r.warn(fmt.Errorf("%s: %w", op.name, err))
		}
	}
	r.markAssigned("theme")
	r.dirty = true
}

// PreviewTheme shows the theme name stands for, without keeping it.
func (r *Runtime) PreviewTheme(name string) error {
	theme, err := r.loadTheme(name)
	if err != nil {
		return err
	}
	r.useTheme(theme)
	return nil
}

// ChooseTheme switches to the theme name stands for and keeps it for the
// next time onon starts.
func (r *Runtime) ChooseTheme(name string) error {
	name = strings.TrimSpace(name)
	theme, err := r.loadTheme(name)
	if err != nil {
		return err
	}
	if err := setSetting(themeChoiceSetting, name); err != nil {
		return err
	}
	r.useTheme(theme)
	r.themeChoice = name
	return nil
}

// RestoreTheme puts back theme, as it was before a preview.
func (r *Runtime) RestoreTheme(theme Theme) {
	r.cfg.Theme = theme
	r.markAssigned("theme")
	r.dirty = true
}

// applyChosenTheme applies the theme picked with :theme, before config.lua
// runs. One that cannot be loaded, such as a file since removed, leaves the
// default with a warning.
func (r *Runtime) applyChosenTheme() {
	name, err := getSetting(themeChoiceSetting)
	if err != nil {
		log.Printf("chosen theme: %v", err)
		return
	}
	if name == "" {
		return
	}
	theme, err := r.loadTheme(name)
	if err != nil {
		r.warn(fmt.Errorf("theme %q chosen with :theme: %w", name, err))
		return
	}
	r.cfg.Theme = theme
	r.themeChoice = name
}
