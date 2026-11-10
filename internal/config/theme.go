package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Theme is everything about how the viewer looks: its colours, the UI font
// and the shape of its panels. It is set as one Lua table, onon.theme.
type Theme struct {
	Base             string  // the preset the theme starts from
	Palette                  // colours in normal mode
	Alt              Palette // colours in alternate-color mode
	Font             ThemeFont
	Radius           int // corner radius in logical pixels
	Padding          int // space around UI text in logical pixels
	Shadow           bool
	StatusBarStyle   string // bar or pill
	StatusBarPadding int    // horizontal status bar padding in logical pixels
	TabBarStyle      string // bar or pill
	Motion           Motion
	// Elements are the styles the elements table sets, over the ones the
	// fields above give.
	Elements [ElementCount]Style
}

// Palette is the colours of one color mode.
type Palette struct {
	Background     [3]uint8 // canvas behind the pages
	Page           [3]uint8 // pages without content, and the paper of recoloured pages
	Foreground     [3]uint8 // UI text; also the ink of recoloured pages
	Muted          [3]uint8
	Accent         [3]uint8
	Panel          [3]uint8
	Border         [3]uint8
	StatusBar      [3]uint8
	Selection      [3]uint8
	Search         [3]uint8
	SearchCurrent  [3]uint8
	HintForeground [3]uint8
	Presentation   [3]uint8
}

// ThemeFont picks the UI font: a file, an installed family, or with
// neither the system's interface font.
type ThemeFont struct {
	Family string
	Path   string
	Size   int // logical pixels
	Weight int // CSS weight, 100 to 900
	Style  string
}

// DefaultTheme is the preset the defaults come from.
const DefaultTheme = "moss"

func hex(s string) [3]uint8 {
	c, err := parseColorOption(s)
	if err != nil {
		panic(err)
	}
	return c
}

var defaultThemeFont = ThemeFont{Size: 13, Weight: 400, Style: "normal"}

// themes are the built-in presets.
var themes = map[string]Theme{
	// Stone-grey canvas with a moss-green accent; forest charcoal when dark.
	"moss": {
		Palette: Palette{
			Background: hex("#e6e4dd"), Page: hex("#ffffff"), Foreground: hex("#2a2e29"), Muted: hex("#737a70"),
			Accent: hex("#4d7044"), Panel: hex("#fbfaf7"), Border: hex("#d5d2c9"), StatusBar: hex("#f1f0eb"),
			Selection: hex("#bfd9a9"), Search: hex("#f2dc8c"), SearchCurrent: hex("#eaa660"),
			HintForeground: hex("#fbfaf7"), Presentation: hex("#0f110f"),
		},
		Alt: Palette{
			Background: hex("#121412"), Page: hex("#1f2420"), Foreground: hex("#d9dfd4"), Muted: hex("#8b9487"),
			Accent: hex("#9dc48b"), Panel: hex("#232823"), Border: hex("#363c35"), StatusBar: hex("#1c1f1c"),
			Selection: hex("#4f6b45"), Search: hex("#7a6a32"), SearchCurrent: hex("#9a6430"),
			HintForeground: hex("#1c1f1c"), Presentation: hex("#0f110f"),
		},
		Font: defaultThemeFont, Radius: 3, Padding: 8, Shadow: true, StatusBarStyle: "bar", StatusBarPadding: 10, TabBarStyle: "bar",
		Motion: defaultMotion,
	},
	// Cream paper with a bark-brown accent; dark wood when dark.
	"birch": {
		Palette: Palette{
			Background: hex("#ebe5da"), Page: hex("#fffdf9"), Foreground: hex("#34291f"), Muted: hex("#7f7061"),
			Accent: hex("#8b5e34"), Panel: hex("#fdfaf5"), Border: hex("#ddd3c3"), StatusBar: hex("#f5f0e7"),
			Selection: hex("#ecd3a4"), Search: hex("#f3dc8f"), SearchCurrent: hex("#e59a5b"),
			HintForeground: hex("#fdfaf5"), Presentation: hex("#12100d"),
		},
		Alt: Palette{
			Background: hex("#16130f"), Page: hex("#25211c"), Foreground: hex("#e6ddcf"), Muted: hex("#9a8d7e"),
			Accent: hex("#d6a670"), Panel: hex("#29241e"), Border: hex("#3d362d"), StatusBar: hex("#211d18"),
			Selection: hex("#6b5233"), Search: hex("#7a6430"), SearchCurrent: hex("#9a5f2c"),
			HintForeground: hex("#211d18"), Presentation: hex("#12100d"),
		},
		Font: defaultThemeFont, Radius: 3, Padding: 8, Shadow: true, StatusBarStyle: "bar", StatusBarPadding: 10, TabBarStyle: "bar",
		Motion: defaultMotion,
	},
	// The flat grey look of earlier releases: greys, with yellow for
	// highlights.
	"classic": {
		Palette: Palette{
			Background: hex("220,220,220"), Page: hex("255,255,255"), Foreground: hex("20,20,20"), Muted: hex("20,20,20"),
			Accent: hex("60,60,60"), Panel: hex("220,220,220"), Border: hex("200,200,200"), StatusBar: hex("220,220,220"),
			Selection: hex("255,224,102"), Search: hex("255,224,102"), SearchCurrent: hex("255,150,50"),
			HintForeground: hex("0,0,0"), Presentation: hex("0,0,0"),
		},
		Alt: Palette{
			Background: hex("20,20,20"), Page: hex("17,17,17"), Foreground: hex("255,255,255"), Muted: hex("255,255,255"),
			Accent: hex("200,200,200"), Panel: hex("20,20,20"), Border: hex("50,50,50"), StatusBar: hex("20,20,20"),
			Selection: hex("255,224,102"), Search: hex("255,224,102"), SearchCurrent: hex("255,150,50"),
			HintForeground: hex("0,0,0"), Presentation: hex("0,0,0"),
		},
		Font: defaultThemeFont, Radius: 0, Padding: 4, Shadow: false, StatusBarStyle: "bar", StatusBarPadding: 8, TabBarStyle: "bar",
		Motion:   stillMotion,
		Elements: classicElements(),
	},
}

// classicElements keep the classic theme's link hints the yellow tags
// with black text they always were; its grey accent is too dark for them.
func classicElements() [ElementCount]Style {
	var elements [ElementCount]Style
	elements[ElementHint].Fill = some(paletteColor("selection", 1))
	return elements
}

// ThemeNames lists the built-in themes in order.
func ThemeNames() []string {
	return slices.Sorted(maps.Keys(themes))
}

// Preset returns the built-in theme of that name.
func Preset(name string) (Theme, bool) {
	theme, ok := themes[strings.ToLower(strings.TrimSpace(name))]
	if ok {
		theme.Base = strings.ToLower(strings.TrimSpace(name))
	}
	return theme, ok
}

func presetOrError(name string) (Theme, error) {
	theme, ok := Preset(name)
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q; expected one of %s", strings.TrimSpace(name), strings.Join(ThemeNames(), ", "))
	}
	return theme, nil
}

// themeField is one entry of the theme table, named as in onon.theme with
// dots for the alt and font tables, such as "alt.accent".
type themeField struct {
	name string
	desc optionDesc
}

// themeGroups are the tables nested in the theme table.
var themeGroups = []string{"alt", "font", "motion", "motion.reduced"}

var themeFields = buildThemeFields()

// paletteColors are the colours of a palette, as the theme table names
// them.
var paletteColors = []struct {
	name, description string
	field             func(*Palette) *[3]uint8
}{
	{"background", "Canvas behind the pages", func(p *Palette) *[3]uint8 { return &p.Background }},
	{"page", "Paper of pages still rendering", func(p *Palette) *[3]uint8 { return &p.Page }},
	{"foreground", "UI text", func(p *Palette) *[3]uint8 { return &p.Foreground }},
	{"muted", "Secondary UI text, such as menu details and the right of the status bar", func(p *Palette) *[3]uint8 { return &p.Muted }},
	{"accent", "Prompts, link hints, headings, the selected menu row's ribbon and the overview's selected page", func(p *Palette) *[3]uint8 { return &p.Accent }},
	{"panel", "Background of menus, completion and other panels", func(p *Palette) *[3]uint8 { return &p.Panel }},
	{"border", "Hairlines around panels and above the status bar", func(p *Palette) *[3]uint8 { return &p.Border }},
	{"status_bar", "Status bar background", func(p *Palette) *[3]uint8 { return &p.StatusBar }},
	{"selection", "Highlight of selected text", func(p *Palette) *[3]uint8 { return &p.Selection }},
	{"search", "Highlight of search matches", func(p *Palette) *[3]uint8 { return &p.Search }},
	{"search_current", "Highlight of the current search match", func(p *Palette) *[3]uint8 { return &p.SearchCurrent }},
	{"hint_foreground", "Text of link hints, set on the accent", func(p *Palette) *[3]uint8 { return &p.HintForeground }},
	{"presentation", "Background around the page in presentation mode", func(p *Palette) *[3]uint8 { return &p.Presentation }},
}

func buildThemeFields() []themeField {
	var fields []themeField
	for _, group := range []struct {
		prefix, suffix string
		palette        func(*Config) *Palette
	}{
		{"", ".", func(c *Config) *Palette { return &c.Theme.Palette }},
		{"alt.", " in alternate-color mode.", func(c *Config) *Palette { return &c.Theme.Alt }},
	} {
		for _, color := range paletteColors {
			field, palette := color.field, group.palette
			description := color.description + group.suffix
			if group.prefix == "alt." && altRecolors[color.name] != "" {
				description = altRecolors[color.name]
			}
			fields = append(fields, themeField{group.prefix + color.name, themeColorOption(description, func(c *Config) *[3]uint8 { return field(palette(c)) })})
		}
	}
	fields = append(fields,
		themeField{"font.family", stringOption("Installed UI font family; empty uses the system interface font, such as SF on macOS or Segoe UI on Windows.", func(c *Config) string { return c.Theme.Font.Family }, func(c *Config, v string) { c.Theme.Font.Family = strings.TrimSpace(v) })},
		themeField{"font.path", stringOption("UI font file; overrides font.family, font.style and font.weight.", func(c *Config) string { return c.Theme.Font.Path }, func(c *Config, v string) { c.Theme.Font.Path = strings.TrimSpace(v) })},
		themeField{"font.size", intOption("UI font size in logical pixels, scaled up on high-density displays.", func(c *Config) int { return c.Theme.Font.Size }, func(c *Config, v int) { c.Theme.Font.Size = max(1, v) })},
		themeField{"font.weight", uiFontWeightOption()},
		themeField{"font.style", stringOption("UI font style: normal, italic, or oblique.", func(c *Config) string { return c.Theme.Font.Style }, func(c *Config, v string) { c.Theme.Font.Style = normalizeUIFontStyle(v) })},
		themeField{"radius", intOption("Corner radius of panels, menu rows and the status pill in logical pixels; 0 gives square corners.", func(c *Config) int { return c.Theme.Radius }, func(c *Config, v int) { c.Theme.Radius = max(0, v) })},
		themeField{"padding", intOption("Space around UI text in menu rows, panels and the status bar, in logical pixels.", func(c *Config) int { return c.Theme.Padding }, func(c *Config, v int) { c.Theme.Padding = max(0, v) })},
		themeField{"shadow", boolOption("Draw a soft shadow under floating panels.", func(c *Config) bool { return c.Theme.Shadow }, func(c *Config, v bool) { c.Theme.Shadow = v })},
		themeField{"status_bar_style", stringOption("Status bar layout: bar along the bottom of the window, or pill, floating over the page.", func(c *Config) string { return c.Theme.StatusBarStyle }, func(c *Config, v string) { c.Theme.StatusBarStyle = NormalizeBarStyle(v) })},
		themeField{"status_bar_padding", intOption("Horizontal status bar padding in logical pixels.", func(c *Config) int { return c.Theme.StatusBarPadding }, func(c *Config, v int) { c.Theme.StatusBarPadding = max(0, v) })},
		themeField{"tab_bar_style", stringOption("Tab bar layout: bar, along the window's edge, or pill, tabs floating over the page as pills.", func(c *Config) string { return c.Theme.TabBarStyle }, func(c *Config, v string) { c.Theme.TabBarStyle = NormalizeBarStyle(v) })},
	)
	return append(fields, motionThemeFields()...)
}

// altRecolors describes the alt colours that pages are also recoloured to.
var altRecolors = map[string]string{
	"page":       "Paper of pages still rendering, and of pages recoloured in alternate-color mode.",
	"foreground": "UI text, and the ink of pages recoloured in alternate-color mode.",
}

// themeOptionPrefix names a theme field as an option, for :set.
const themeOptionPrefix = "theme."

func init() {
	for _, field := range themeFields {
		registerOption(themeOptionPrefix+field.name, field.desc)
	}
	registerOption("theme", optionDesc{
		kind:        "theme",
		description: "The theme: a table of colours, font and shape, or the name of a preset (" + strings.Join(ThemeNames(), ", ") + ").",
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return luaThemeTable(L, cfg.Theme) },
		format:      func(cfg *Config) string { return cfg.Theme.Base },
		applyText: func(cfg *Config, raw string) error {
			name, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			theme, err := presetOrError(name)
			if err != nil {
				return err
			}
			cfg.Theme = theme
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			theme, err := themeFromLua(cfg, value)
			if err != nil && !isSkipped(err) {
				return err
			}
			cfg.Theme = theme
			return err
		},
	})
}

// isThemeOption reports whether name is the theme or one of its fields,
// which the options reference leaves to the theme reference.
func isThemeOption(name string) bool {
	return name == "theme" || strings.HasPrefix(name, themeOptionPrefix)
}

// themeFromLua reads a preset name, or a theme table: the preset named by
// its base, the default if none, with the fields the table sets.
func themeFromLua(cfg *Config, value lua.LValue) (Theme, error) {
	switch value := value.(type) {
	case lua.LString:
		return presetOrError(string(value))
	case *lua.LTable:
		if isLuaThemeProxy(value) {
			return cfg.Theme, nil // onon.theme assigned to itself
		}
		base := DefaultTheme
		if b := value.RawGetString("base"); b != lua.LNil {
			if b.Type() != lua.LTString {
				return Theme{}, fmt.Errorf("base: expected string")
			}
			base = b.String()
		}
		theme, err := presetOrError(base)
		if err != nil {
			return Theme{}, err
		}
		work := *cfg
		work.Theme = theme
		if err := applyThemeTable(&work, value, ""); err != nil && !isSkipped(err) {
			return Theme{}, err
		} else {
			return work.Theme, err // with any fields skipped
		}
	default:
		return Theme{}, fmt.Errorf("expected theme table or preset name")
	}
}

// applyThemeTable sets the fields in tbl, whose names start with prefix.
// Fields it does not know are skipped, and returned as skippedFields.
func applyThemeTable(cfg *Config, tbl *lua.LTable, prefix string) error {
	var skipped skips
	var err error
	tbl.ForEach(func(key, value lua.LValue) {
		if err != nil {
			return
		}
		if key.Type() != lua.LTString {
			err = fmt.Errorf("theme keys must be strings, got %s", key.Type())
			return
		}
		name := prefix + strings.ToLower(key.String())
		if name == "base" {
			return
		}
		if name == "elements" {
			sub, ok := value.(*lua.LTable)
			if !ok {
				err = fmt.Errorf("elements: expected a table of elements")
				return
			}
			err = skipped.add(applyElementsTable(cfg, sub))
			return
		}
		if sub, ok := value.(*lua.LTable); ok && slices.Contains(themeGroups, name) {
			err = skipped.add(applyThemeTable(cfg, sub, name+"."))
			return
		}
		desc, ok := configOptions[themeOptionPrefix+name]
		if !ok {
			skipped.add(skipField(name))
			return
		}
		if e := desc.apply(cfg, value); e != nil {
			err = fmt.Errorf("%s: %w", name, e)
		}
	})
	if err != nil {
		return err
	}
	return skipped.err()
}

// setThemeField sets one field, or every field a nested table holds, as for
// onon.theme.alt = { accent = "#..." }; element names start "elements.".
// Nothing changes if any is invalid.
func (r *Runtime) setThemeField(name string, value lua.LValue) error {
	work := r.cfg
	sub, isTable := value.(*lua.LTable)
	var err error
	switch element, isElement := strings.CutPrefix(name, "elements."); {
	case name == "elements" && isTable:
		err = applyElementsTable(&work, sub)
	case name == "elements":
		err = fmt.Errorf("elements: expected a table of elements")
	case isElement:
		err = applyElementValue(&work, element, value)
	case isTable && slices.Contains(themeGroups, name):
		err = applyThemeTable(&work, sub, name+".")
	default:
		if _, ok := lookupOption(themeOptionPrefix + name); !ok {
			r.warn(skipField(name))
			return nil
		}
		return r.setOption(themeOptionPrefix+name, value)
	}
	if err != nil && !isSkipped(err) {
		return err
	}
	if err != nil {
		r.warn(err)
	}
	r.cfg.Theme = work.Theme
	r.recordThemeOp(themeOp{field: true, name: name, value: value})
	r.markAssigned("theme")
	r.dirty = true
	return nil
}

// luaThemeTable writes theme as a plain table, as onon.theme is set.
func luaThemeTable(L *lua.LState, theme Theme) *lua.LTable {
	cfg := Config{Theme: theme}
	tbl := L.NewTable()
	tbl.RawSetString("base", lua.LString(theme.Base))
	for _, field := range themeFields {
		if slices.Contains(themeGroups, field.name) {
			continue // a table's own value, such as motion.reduced's base, is one of its fields too
		}
		target := tbl
		parts := strings.Split(field.name, ".")
		for _, group := range parts[:len(parts)-1] {
			sub, ok := target.RawGetString(group).(*lua.LTable)
			if !ok {
				sub = L.NewTable()
				target.RawSetString(group, sub)
			}
			target = sub
		}
		target.RawSetString(parts[len(parts)-1], field.desc.get(L, &cfg))
	}
	tbl.RawSetString("elements", luaElementsTable(L, &theme.Elements))
	return tbl
}

// themeProxyKey marks the metatable of onon.theme, which holds no fields
// of its own to copy, with the prefix of the table it stands for.
const themeProxyKey = "__onon_theme"

// isLuaThemeProxy reports whether tbl is onon.theme itself.
func isLuaThemeProxy(tbl *lua.LTable) bool {
	mt, ok := tbl.Metatable.(*lua.LTable)
	return ok && mt.RawGetString(themeProxyKey) == lua.LString("")
}

// newLuaThemeTable is onon.theme: reading a field gives its value and
// assigning one sets it. prefix names the nested table it stands for.
func newLuaThemeTable(L *lua.LState, rt *Runtime, cfg *Config, prefix string) *lua.LTable {
	tbl := L.NewTable()
	mt := L.NewTable()
	mt.RawSetString(themeProxyKey, lua.LString(prefix))
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		name := prefix + strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		switch {
		case name == "base":
			L.Push(lua.LString(cfg.Theme.Base))
		case slices.Contains(themeGroups, name):
			L.Push(newLuaThemeTable(L, rt, cfg, name+"."))
		case name == "elements":
			L.Push(newLuaElementsTable(L, rt, cfg, ""))
		default:
			desc, ok := configOptions[themeOptionPrefix+name]
			if !ok {
				L.RaiseError("onon.theme.%s: unknown theme field", name)
			}
			L.Push(desc.get(L, cfg))
		}
		return 1
	}))
	L.SetField(mt, "__newindex", L.NewFunction(func(L *lua.LState) int {
		name := prefix + strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		if err := rt.setThemeField(name, L.CheckAny(3)); err != nil {
			if strings.HasPrefix(name, "elements") {
				L.RaiseError("onon.theme.%v", err) // the error names the element
			}
			L.RaiseError("onon.theme.%s: %v", name, err)
		}
		return 0
	}))
	L.SetMetatable(tbl, mt)
	return tbl
}

// newLuaThemesTable is onon.themes: each preset as a fresh table to
// change and assign to onon.theme.
func newLuaThemesTable(L *lua.LState) *lua.LTable {
	tbl := L.NewTable()
	mt := L.NewTable()
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		theme, ok := Preset(L.CheckString(2))
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(luaThemeTable(L, theme))
		return 1
	}))
	L.SetMetatable(tbl, mt)
	return tbl
}

// themeColorOption holds a colour, written "#RRGGBB" in the theme table
// and accepting r,g,b, or a Lua {r, g, b} table as before.
func themeColorOption(description string, field func(*Config) *[3]uint8) optionDesc {
	return optionDesc{
		kind:        "color",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LString(FormatColor(*field(cfg))) },
		format:      func(cfg *Config) string { return FormatColor(*field(cfg)) },
		applyText: func(cfg *Config, raw string) error {
			raw, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			color, err := parseColorOption(raw)
			if err != nil {
				return err
			}
			*field(cfg) = color
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			switch value := value.(type) {
			case lua.LString:
				color, err := parseColorOption(string(value))
				if err != nil {
					return err
				}
				*field(cfg) = color
			case *lua.LTable:
				*field(cfg) = readColor(value, *field(cfg))
			default:
				return fmt.Errorf("expected \"#RRGGBB\" or {r, g, b}")
			}
			return nil
		},
	}
}

// FormatColor writes a colour as #rrggbb.
func FormatColor(c [3]uint8) string {
	return fmt.Sprintf("#%02x%02x%02x", c[0], c[1], c[2])
}

// NormalizeBarStyle reads a status_bar_style or tab_bar_style: pill, or
// bar for anything else.
func NormalizeBarStyle(s string) string {
	if strings.ToLower(strings.TrimSpace(s)) == "pill" {
		return "pill"
	}
	return "bar"
}

// movedToTheme names the theme field each former style option became.
var movedToTheme = map[string]string{
	"background":              "background",
	"page_background":         "page",
	"foreground":              "foreground",
	"status_bar_color":        "status_bar",
	"alt_background":          "alt.background",
	"alt_page_background":     "alt.page",
	"alt_foreground":          "alt.foreground",
	"alt_status_bar_color":    "alt.status_bar",
	"highlight_foreground":    "hint_foreground",
	"selection_color":         "selection",
	"search_highlight_color":  "search",
	"search_current_color":    "search_current",
	"presentation_background": "presentation",
	"ui_font":                 "font.family",
	"ui_font_path":            "font.path",
	"ui_font_size":            "font.size",
	"ui_font_style":           "font.style",
	"ui_font_weight":          "font.weight",
	"status_bar_padding":      "status_bar_padding",
}

// movedOptionError explains where a former style option went, or is nil.
func movedOptionError(name string) error {
	field, ok := movedToTheme[name]
	if !ok {
		return nil
	}
	return fmt.Errorf("%s moved to onon.theme.%s (:set theme.%s=...)", name, field, field)
}

// ThemeFieldReference documents one field of the theme table.
type ThemeFieldReference struct {
	Name        string // dotted, such as "alt.accent"
	Type        string
	Default     string // as written in Lua
	Description string
}

// ThemeReferences documents the theme table's fields with the default
// theme's values.
func ThemeReferences() []ThemeFieldReference {
	cfg := Default()
	refs := make([]ThemeFieldReference, 0, len(themeFields))
	for _, field := range themeFields {
		value := field.desc.format(&cfg)
		if field.desc.kind == "color" {
			value = strconv.Quote(value)
		}
		refs = append(refs, ThemeFieldReference{Name: field.name, Type: field.desc.kind, Default: value, Description: field.desc.description})
	}
	return refs
}
