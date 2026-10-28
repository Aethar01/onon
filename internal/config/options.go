package config

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

type optionDesc struct {
	kind        string
	description string
	get         func(L *lua.LState, cfg *Config) lua.LValue
	apply       func(cfg *Config, value lua.LValue) error
	format      func(*Config) string
	applyText   func(*Config, string) error
}

func boolOption(description string, get func(*Config) bool, set func(*Config, bool)) optionDesc {
	return optionDesc{
		kind:        "boolean",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LBool(get(cfg)) },
		format:      func(cfg *Config) string { return strconv.FormatBool(get(cfg)) },
		applyText: func(cfg *Config, raw string) error {
			value, err := parseBoolOption(raw)
			if err != nil {
				return err
			}
			set(cfg, value)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			if value.Type() != lua.LTBool {
				return fmt.Errorf("expected boolean")
			}
			set(cfg, lua.LVAsBool(value))
			return nil
		},
	}
}

func inputSourcesOption(option, description string, allowed SmoothInputSources, get func(*Config) SmoothInputSources, set func(*Config, SmoothInputSources)) optionDesc {
	applySources := func(cfg *Config, sources, present SmoothInputSources) {
		current := get(cfg)
		set(cfg, (current&^present)|sources)
	}
	return optionDesc{
		kind:        "table",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return inputSourcesTable(L, get(cfg), allowed) },
		format:      func(cfg *Config) string { return formatInputSources(get(cfg), allowed) },
		applyText: func(cfg *Config, raw string) error {
			sources, present, err := parseInputSourcesText(raw, option, allowed)
			if err != nil {
				return err
			}
			applySources(cfg, sources, present)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			sources, present, err := parseInputSourcesValue(value, option, allowed)
			if err != nil {
				return err
			}
			applySources(cfg, sources, present)
			return nil
		},
	}
}

func intOption(description string, get func(*Config) int, set func(*Config, int)) optionDesc {
	return optionDesc{
		kind:        "integer",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LNumber(get(cfg)) },
		format:      func(cfg *Config) string { return strconv.Itoa(get(cfg)) },
		applyText: func(cfg *Config, raw string) error {
			value, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("expected integer")
			}
			set(cfg, value)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			if value.Type() != lua.LTNumber {
				return fmt.Errorf("expected number")
			}
			set(cfg, int(lua.LVAsNumber(value)))
			return nil
		},
	}
}

func floatOption(description string, get func(*Config) float64, set func(*Config, float64)) optionDesc {
	return optionDesc{
		kind:        "number",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LNumber(get(cfg)) },
		format:      func(cfg *Config) string { return strconv.FormatFloat(get(cfg), 'g', -1, 64) },
		applyText: func(cfg *Config, raw string) error {
			value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil {
				return fmt.Errorf("expected number")
			}
			set(cfg, value)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			if value.Type() != lua.LTNumber {
				return fmt.Errorf("expected number")
			}
			set(cfg, float64(lua.LVAsNumber(value)))
			return nil
		},
	}
}

func stringOption(description string, get func(*Config) string, set func(*Config, string)) optionDesc {
	return optionDesc{
		kind:        "string",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LString(get(cfg)) },
		format:      func(cfg *Config) string { return strconv.Quote(get(cfg)) },
		applyText: func(cfg *Config, raw string) error {
			value, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			set(cfg, value)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			if value.Type() != lua.LTString {
				return fmt.Errorf("expected string")
			}
			set(cfg, value.String())
			return nil
		},
	}
}

// stringListOption holds a Lua array of strings. The :set form accepts
// comma- or space-separated words.
func stringListOption(description string, get func(*Config) []string, set func(*Config, []string)) optionDesc {
	return optionDesc{
		kind:        "table",
		description: description,
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			tbl := L.NewTable()
			for _, value := range get(cfg) {
				tbl.Append(lua.LString(value))
			}
			return tbl
		},
		format: func(cfg *Config) string {
			quoted := make([]string, 0, len(get(cfg)))
			for _, value := range get(cfg) {
				quoted = append(quoted, strconv.Quote(value))
			}
			return "{" + strings.Join(quoted, ", ") + "}"
		},
		applyText: func(cfg *Config, raw string) error {
			raw = strings.Trim(strings.TrimSpace(raw), "{}")
			fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
			values := make([]string, 0, len(fields))
			for _, field := range fields {
				values = append(values, strings.Trim(field, `"'`))
			}
			set(cfg, values)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			tbl, ok := value.(*lua.LTable)
			if !ok {
				return fmt.Errorf("expected table")
			}
			values := make([]string, 0, tbl.Len())
			for i := 1; i <= tbl.Len(); i++ {
				item := tbl.RawGetInt(i)
				if item.Type() != lua.LTString {
					return fmt.Errorf("expected table of strings")
				}
				values = append(values, item.String())
			}
			set(cfg, values)
			return nil
		},
	}
}

// headersOption is a table of tables: header names and values by host. The
// values are often secrets, such as API tokens, so :set shows only the names.
func headersOption(description string, get func(*Config) map[string]map[string]string, set func(*Config, map[string]map[string]string)) optionDesc {
	return optionDesc{
		kind:        "table",
		description: description,
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			tbl := L.NewTable()
			for host, headers := range get(cfg) {
				inner := L.NewTable()
				for name, value := range headers {
					inner.RawSetString(name, lua.LString(value))
				}
				tbl.RawSetString(host, inner)
			}
			return tbl
		},
		format: func(cfg *Config) string {
			hosts := make([]string, 0, len(get(cfg)))
			for host, headers := range get(cfg) {
				names := make([]string, 0, len(headers))
				for name := range headers {
					names = append(names, name+" = …")
				}
				sort.Strings(names)
				hosts = append(hosts, fmt.Sprintf("[%q] = {%s}", host, strings.Join(names, ", ")))
			}
			sort.Strings(hosts)
			return "{" + strings.Join(hosts, ", ") + "}"
		},
		applyText: func(cfg *Config, raw string) error {
			return fmt.Errorf("set it in config.lua, where its values can come from os.getenv")
		},
		apply: func(cfg *Config, value lua.LValue) error {
			tbl, ok := value.(*lua.LTable)
			if !ok {
				return fmt.Errorf("expected table of hosts")
			}
			result := map[string]map[string]string{}
			var err error
			tbl.ForEach(func(key, inner lua.LValue) {
				headers, ok := inner.(*lua.LTable)
				if err != nil || key.Type() != lua.LTString || !ok {
					if err == nil {
						err = fmt.Errorf("expected a table of headers for each host")
					}
					return
				}
				values := map[string]string{}
				headers.ForEach(func(name, value lua.LValue) {
					if err == nil && (name.Type() != lua.LTString || value.Type() != lua.LTString) {
						err = fmt.Errorf("headers for %s must be strings", key.String())
					}
					values[name.String()] = value.String()
				})
				result[strings.ToLower(key.String())] = values
			})
			if err != nil {
				return err
			}
			set(cfg, result)
			return nil
		},
	}
}

func menusOption(description string, get func(*Config) map[string]MenuPlacement, set func(*Config, map[string]MenuPlacement)) optionDesc {
	desc := optionDesc{
		kind:        "table",
		description: description,
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			tbl := L.NewTable()
			for name, p := range get(cfg) {
				inner := L.NewTable()
				for field, size := range map[string]UISize{"width": p.Width, "height": p.Height} {
					if size.Value > 0 {
						inner.RawSetString(field, uiSizeValue(size))
					}
				}
				if p.Anchor != "" {
					inner.RawSetString("anchor", lua.LString(p.Anchor))
				}
				tbl.RawSetString(name, inner)
			}
			return tbl
		},
		format: func(cfg *Config) string {
			menus := make([]string, 0, len(get(cfg)))
			for name, p := range get(cfg) {
				var fields []string
				for _, f := range []struct {
					name string
					size UISize
				}{{"width", p.Width}, {"height", p.Height}} {
					switch {
					case f.size.Percent:
						fields = append(fields, fmt.Sprintf("%s = %q", f.name, uiSizeValue(f.size).String()))
					case f.size.Value > 0:
						fields = append(fields, f.name+" = "+uiSizeValue(f.size).String())
					}
				}
				if p.Anchor != "" {
					fields = append(fields, fmt.Sprintf("anchor = %q", p.Anchor))
				}
				menus = append(menus, name+" = {"+strings.Join(fields, ", ")+"}")
			}
			sort.Strings(menus)
			return "{" + strings.Join(menus, ", ") + "}"
		},
	}
	desc.apply = func(cfg *Config, value lua.LValue) error {
		tbl, ok := value.(*lua.LTable)
		if !ok {
			return fmt.Errorf("expected a table of menus, by name")
		}
		result := map[string]MenuPlacement{}
		var err error
		tbl.ForEach(func(key, inner lua.LValue) {
			if err != nil {
				return
			}
			name := key.String()
			fields, ok := inner.(*lua.LTable)
			if key.Type() != lua.LTString || !slices.Contains(MenuNames, name) {
				err = fmt.Errorf("unknown menu %q; use one of %s", name, strings.Join(MenuNames, ", "))
				return
			}
			if !ok {
				err = fmt.Errorf("%s: expected a table of width, height and anchor", name)
				return
			}
			var p MenuPlacement
			fields.ForEach(func(field, value lua.LValue) {
				if err != nil {
					return
				}
				switch field.String() {
				case "width", "height":
					size, e := luaUISize(value)
					if e != nil {
						err = fmt.Errorf("%s: %s %v", name, field.String(), e)
					} else if field.String() == "width" {
						p.Width = size
					} else {
						p.Height = size
					}
				case "anchor":
					p.Anchor = value.String()
					if !slices.Contains(UIAnchors, p.Anchor) {
						err = fmt.Errorf("%s: unknown anchor %q; use one of %s", name, p.Anchor, strings.Join(UIAnchors, ", "))
					}
				default:
					err = fmt.Errorf("%s: unknown field %q; menus take width, height and anchor", name, field.String())
				}
			})
			result[name] = p
		})
		if err != nil {
			return err
		}
		set(cfg, result)
		return nil
	}
	desc.applyText = func(cfg *Config, raw string) error {
		// The text is a Lua table, read by a Lua with nothing it could run.
		L := lua.NewState(lua.Options{SkipOpenLibs: true})
		defer L.Close()
		if err := L.DoString("return " + raw); err != nil {
			return fmt.Errorf("expected a Lua table such as { help = { anchor = \"top\" } }")
		}
		return desc.apply(cfg, L.Get(-1))
	}
	return desc
}

// uiSizeValue is size as Lua writes it: logical pixels, or a percentage.
// promptsOption is a table of prompts, by name, each "bar" or a table of
// width and anchor.
func promptsOption(description string, get func(*Config) map[string]PromptPlacement, set func(*Config, map[string]PromptPlacement)) optionDesc {
	desc := optionDesc{
		kind:        "table",
		description: description,
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			tbl := L.NewTable()
			for name, p := range get(cfg) {
				if p.Anchor == "" {
					tbl.RawSetString(name, lua.LString("bar"))
					continue
				}
				inner := L.NewTable()
				if p.Width.Value > 0 {
					inner.RawSetString("width", uiSizeValue(p.Width))
				}
				inner.RawSetString("anchor", lua.LString(p.Anchor))
				tbl.RawSetString(name, inner)
			}
			return tbl
		},
		format: func(cfg *Config) string {
			prompts := make([]string, 0, len(get(cfg)))
			for name, p := range get(cfg) {
				if p.Anchor == "" {
					prompts = append(prompts, name+` = "bar"`)
					continue
				}
				var fields []string
				switch {
				case p.Width.Percent:
					fields = append(fields, fmt.Sprintf("width = %q", uiSizeValue(p.Width).String()))
				case p.Width.Value > 0:
					fields = append(fields, "width = "+uiSizeValue(p.Width).String())
				}
				fields = append(fields, fmt.Sprintf("anchor = %q", p.Anchor))
				prompts = append(prompts, name+" = {"+strings.Join(fields, ", ")+"}")
			}
			sort.Strings(prompts)
			return "{" + strings.Join(prompts, ", ") + "}"
		},
	}
	desc.apply = func(cfg *Config, value lua.LValue) error {
		tbl, ok := value.(*lua.LTable)
		if !ok {
			return fmt.Errorf("expected a table of prompts, by name")
		}
		result := map[string]PromptPlacement{}
		var err error
		tbl.ForEach(func(key, inner lua.LValue) {
			if err != nil {
				return
			}
			name := key.String()
			if key.Type() != lua.LTString || !slices.Contains(PromptNames, name) {
				err = fmt.Errorf("unknown prompt %q; use one of %s", name, strings.Join(PromptNames, ", "))
				return
			}
			if inner.Type() == lua.LTString && inner.String() == "bar" {
				result[name] = PromptPlacement{}
				return
			}
			fields, ok := inner.(*lua.LTable)
			if !ok {
				err = fmt.Errorf(`%s: expected "bar" or a table of width and anchor`, name)
				return
			}
			p := PromptPlacement{Anchor: "center"}
			fields.ForEach(func(field, value lua.LValue) {
				if err != nil {
					return
				}
				switch field.String() {
				case "width":
					p.Width, err = luaUISize(value)
					if err != nil {
						err = fmt.Errorf("%s: width %v", name, err)
					}
				case "anchor":
					p.Anchor = value.String()
					if !slices.Contains(UIAnchors, p.Anchor) {
						err = fmt.Errorf("%s: unknown anchor %q; use one of %s", name, p.Anchor, strings.Join(UIAnchors, ", "))
					}
				default:
					err = fmt.Errorf("%s: unknown field %q; prompts take width and anchor", name, field.String())
				}
			})
			result[name] = p
		})
		if err != nil {
			return err
		}
		set(cfg, result)
		return nil
	}
	desc.applyText = func(cfg *Config, raw string) error {
		// The text is a Lua table, read by a Lua with nothing it could run.
		L := lua.NewState(lua.Options{SkipOpenLibs: true})
		defer L.Close()
		if err := L.DoString("return " + raw); err != nil {
			return fmt.Errorf(`expected a Lua table such as { command = { anchor = "center" } }`)
		}
		return desc.apply(cfg, L.Get(-1))
	}
	return desc
}

func uiSizeValue(size UISize) lua.LValue {
	if size.Percent {
		return lua.LString(strconv.FormatFloat(size.Value, 'f', -1, 64) + "%")
	}
	return lua.LNumber(size.Value)
}

func normalizeLinkSchemes(schemes []string) []string {
	normalized := make([]string, 0, len(schemes))
	for _, scheme := range schemes {
		if scheme = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(scheme), ":")); scheme != "" {
			normalized = append(normalized, scheme)
		}
	}
	return normalized
}

func OptionNames() []string {
	names := make([]string, 0, len(configOptions))
	for name := range configOptions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) OptionValue(name string) (string, error) {
	name = normalizeOptionName(name)
	desc, ok := lookupOption(name)
	if !ok {
		return r.pluginOptionValue(name)
	}
	return desc.format(&r.cfg), nil
}

func (r *Runtime) SetOption(name, value string) error {
	name = normalizeOptionName(name)
	desc, ok := lookupOption(name)
	if !ok {
		if err := movedOptionError(name); err != nil {
			return err
		}
		if err := r.setPluginOption(name, value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		r.dirty = true
		return nil
	}
	if err := desc.applyText(&r.cfg, value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	r.markAssigned(name)
	r.dirty = true
	return nil
}

func (r *Runtime) ToggleOption(name string) error {
	name = normalizeOptionName(name)
	desc, ok := lookupOption(name)
	if !ok {
		option, ok := r.pluginOption(name)
		if !ok {
			return fmt.Errorf("unknown option: %s", name)
		}
		if option.kind != "boolean" && option.kind != "bool" {
			return fmt.Errorf("%s: expected boolean option", name)
		}
		return r.SetOption(name, strconv.FormatBool(!lua.LVAsBool(option.value)))
	}
	if desc.kind != "boolean" {
		return fmt.Errorf("%s: expected boolean option", name)
	}
	current := desc.get(r.state, &r.cfg)
	value, ok := current.(lua.LBool)
	if !ok {
		// alt_colors may be "system", which is neither true nor false.
		return fmt.Errorf("%s is %q, not true or false", name, current.String())
	}
	return r.SetOption(name, strconv.FormatBool(!bool(value)))
}

func normalizeOptionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func parseBoolOption(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "on", "yes", "1":
		return true, nil
	case "false", "off", "no", "0":
		return false, nil
	default:
		return false, fmt.Errorf("expected boolean")
	}
}

func parseStringOption(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if raw[0] == '\'' || raw[0] == '"' {
		if raw[0] == '\'' {
			if len(raw) < 2 || raw[len(raw)-1] != '\'' {
				return "", fmt.Errorf("unterminated string")
			}
			return raw[1 : len(raw)-1], nil
		}
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid quoted string")
		}
		return value, nil
	}
	return raw, nil
}

// ParseColor reads a colour written as #RRGGBB or r,g,b.
func ParseColor(raw string) ([3]uint8, error) { return parseColorOption(raw) }

func parseColorOption(raw string) ([3]uint8, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "#") {
		if len(raw) != 7 {
			return [3]uint8{}, fmt.Errorf("expected #RRGGBB or r,g,b")
		}
		value, err := strconv.ParseUint(raw[1:], 16, 24)
		if err != nil {
			return [3]uint8{}, fmt.Errorf("expected #RRGGBB or r,g,b")
		}
		return [3]uint8{uint8(value >> 16), uint8(value >> 8), uint8(value)}, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 3 {
		return [3]uint8{}, fmt.Errorf("expected #RRGGBB or r,g,b")
	}
	var color [3]uint8
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 0 || value > 255 {
			return [3]uint8{}, fmt.Errorf("color channels must be between 0 and 255")
		}
		color[i] = uint8(value)
	}
	return color, nil
}

// registerOption adds an option defined outside the configOptions literal,
// refusing names that are already taken.
func registerOption(name string, desc optionDesc) {
	if _, ok := configOptions[name]; ok {
		panic("config: option " + name + " registered twice")
	}
	configOptions[name] = desc
}

var configOptions = map[string]optionDesc{
	"mouse_text_select":      boolOption("Enable text selection with the left mouse button.", func(c *Config) bool { return c.MouseTextSelect }, func(c *Config, v bool) { c.MouseTextSelect = v }),
	"smooth_scroll":          inputSourcesOption("smooth_scroll", "Animate scrolling for selected inputs; assignments update only specified mouse, trackpad, and keyboard fields.", SmoothInputAll, func(c *Config) SmoothInputSources { return c.SmoothScrollSources }, func(c *Config, v SmoothInputSources) { c.SmoothScrollSources = v }),
	"smooth_zoom":            inputSourcesOption("smooth_zoom", "Animate zooming for selected inputs; assignments update only specified mouse, trackpad, and keyboard fields.", SmoothInputAll, func(c *Config) SmoothInputSources { return c.SmoothZoomSources }, func(c *Config, v SmoothInputSources) { c.SmoothZoomSources = v }),
	"invert_scroll":          inputSourcesOption("invert_scroll", "Invert horizontal and vertical wheel scrolling for selected inputs; true or false sets both, and a table updates only the mouse and trackpad fields it names.", SmoothInputWheel, func(c *Config) SmoothInputSources { return c.InvertScrollSources }, func(c *Config, v SmoothInputSources) { c.InvertScrollSources = v }),
	"open_in_new_window":     boolOption("On macOS, open a document Finder or `open` sends to a running onon in a new window rather than the current one.", func(c *Config) bool { return c.OpenInNewWindow }, func(c *Config, v bool) { c.OpenInNewWindow = v }),
	"session_database":       boolOption("Persist per-document view state, marks, and recent files.", func(c *Config) bool { return c.SessionDatabase }, func(c *Config, v bool) { c.SessionDatabase = v }),
	"alt_colors_keep_images": boolOption("Keep raster images in their own colors in alternate-color mode.", func(c *Config) bool { return c.AltColorsKeepImages }, func(c *Config, v bool) { c.AltColorsKeepImages = v }),
	"trim_margins":           boolOption("Lay pages out by their content, trimming blank margins.", func(c *Config) bool { return c.TrimMargins }, func(c *Config, v bool) { c.TrimMargins = v }),
	"annotation_colors":      stringListOption("Highlight colours offered by the highlight picker, as #RRGGBB.", func(c *Config) []string { return c.AnnotationColors }, func(c *Config, v []string) { c.AnnotationColors = v }),
	"link_preview":           boolOption("Preview the destination of an internal link after hovering it briefly.", func(c *Config) bool { return c.LinkPreview }, func(c *Config, v bool) { c.LinkPreview = v }),
	"dual_page":              boolOption("Start in dual-page mode.", func(c *Config) bool { return c.DualPage }, func(c *Config, v bool) { c.DualPage = v }),
	"first_page_offset":      boolOption("Treat the first page as a standalone cover in dual-page mode.", func(c *Config) bool { return c.FirstPageOffset }, func(c *Config, v bool) { c.FirstPageOffset = v }),
	"anti_aliasing":          intOption("MuPDF antialiasing level from 0 through 8.", func(c *Config) int { return c.AntiAliasing }, func(c *Config, v int) { c.AntiAliasing = v }),
	"http_headers":           headersOption("Headers sent when fetching remote documents, by host, host:port or *.domain, such as an API token; :set shows only their names.", func(c *Config) map[string]map[string]string { return c.HTTPHeaders }, func(c *Config, v map[string]map[string]string) { c.HTTPHeaders = v }),
	"link_schemes":           stringListOption("URI schemes that document links may open externally; other links only show their target.", func(c *Config) []string { return c.LinkSchemes }, func(c *Config, v []string) { c.LinkSchemes = normalizeLinkSchemes(v) }),
	"mupdf_store_mb":         intOption("MuPDF cache for fonts and decoded images in MiB; applies to newly opened documents.", func(c *Config) int { return c.MuPDFStoreMB }, func(c *Config, v int) { c.MuPDFStoreMB = max(1, v) }),
	"page_cache_memory_mb":   intOption("Maximum memory in MiB for cached page renders; 0 disables the limit.", func(c *Config) int { return c.PageCacheMemoryMB }, func(c *Config, v int) { c.PageCacheMemoryMB = max(0, v) }),
	"page_cache_size":        intOption("Deprecated and ignored; the cache is bounded by page_cache_memory_mb.", func(*Config) int { return 0 }, func(*Config, int) {}),
	"outline_initial_depth":  intOption("Outline levels expanded when the outline opens.", func(c *Config) int { return c.OutlineInitialDepth }, func(c *Config, v int) { c.OutlineInitialDepth = v }),
	"outline_width_percent":  intOption("Most of the window the outline overlay takes, as a percentage of its width; the outline is narrower when its entries are.", func(c *Config) int { return c.OutlineWidthPercent }, func(c *Config, v int) { c.OutlineWidthPercent = v }),
	"menus":                  menusOption("Where onon's own menus sit, by name, such as { outline = { anchor = \"left\", width = \"40%\" } }: each takes width and height, the most of the window it takes, in logical pixels or a percentage, and anchor, as views do. Menus: "+strings.Join(MenuNames, ", ")+".", func(c *Config) map[string]MenuPlacement { return c.Menus }, func(c *Config, v map[string]MenuPlacement) { c.Menus = v }),
	"prompts":                promptsOption("Where prompts sit, by name, such as { command = { anchor = \"center\", width = \"50%\" } }: each is \"bar\", in the status bar as by default, or a box of its own with an anchor, as views take, centred if not given, and a width in logical pixels or a percentage, half the window if not given. Prompts: "+strings.Join(PromptNames, ", ")+".", func(c *Config) map[string]PromptPlacement { return c.Prompts }, func(c *Config, v map[string]PromptPlacement) { c.Prompts = v }),
	"outline_height_percent": intOption("Outline overlay height as a percentage of the window.", func(c *Config) int { return c.OutlineHeightPercent }, func(c *Config, v int) { c.OutlineHeightPercent = v }),
	"completion_max_items":   intOption("Maximum command-completion rows.", func(c *Config) int { return c.CompletionMaxItems }, func(c *Config, v int) { c.CompletionMaxItems = max(1, v) }),
	"auto_reload":            boolOption("Reload the document when its file changes on disk.", func(c *Config) bool { return c.AutoReload }, func(c *Config, v bool) { c.AutoReload = v }),
	"auto_reload_delay_ms":   intOption("Milliseconds a changed file must stay unwritten before it reloads, so a file being written reloads once.", func(c *Config) int { return c.AutoReloadDelayMS }, func(c *Config, v int) { c.AutoReloadDelayMS = max(0, v) }),
	"link_preview_delay_ms":  intOption("Milliseconds to hover an internal link before its preview appears.", func(c *Config) int { return c.LinkPreviewDelayMS }, func(c *Config, v int) { c.LinkPreviewDelayMS = max(0, v) }),
	"prompt_history_max":     intOption("Maximum entries kept in each of the command and search prompt histories; 0 keeps none.", func(c *Config) int { return c.PromptHistoryMax }, func(c *Config, v int) { c.PromptHistoryMax = max(0, v) }),
	"trim_padding":           intOption("Margin in points kept around a page's content when trimming margins.", func(c *Config) int { return c.TrimPadding }, func(c *Config, v int) { c.TrimPadding = max(0, v) }),
	"loading_indicator":      boolOption("Animate a loading indicator on pages still rendering for the first time.", func(c *Config) bool { return c.LoadingIndicator }, func(c *Config, v bool) { c.LoadingIndicator = v }),
	"overview_thumb_width":   intOption("Width in pixels the page overview aims to show each page at, which sets how many columns fit.", func(c *Config) int { return c.OverviewThumbWidth }, func(c *Config, v int) { c.OverviewThumbWidth = max(1, v) }),
	"overview_max_columns":   intOption("Most columns the page overview shows, whether fitted to the window or set with zoom_out.", func(c *Config) int { return c.OverviewMaxColumns }, func(c *Config, v int) { c.OverviewMaxColumns = max(1, v) }),
	"overview_gap":           intOption("Gap in pixels between the page overview's cells.", func(c *Config) int { return c.OverviewGap }, func(c *Config, v int) { c.OverviewGap = max(0, v) }),
	"recent_files_max":       intOption("Maximum recent files retained and displayed.", func(c *Config) int { return c.RecentFilesMax }, func(c *Config, v int) { c.RecentFilesMax = max(0, v) }),
	"scroll_step":            intOption("Keyboard and mouse scroll distance in pixels.", func(c *Config) int { return c.ScrollStep }, func(c *Config, v int) { c.ScrollStep = v }),
	"page_gap": intOption("Vertical gap between pages; aliases page_gap_vertical.", func(c *Config) int { return c.PageGap }, func(c *Config, v int) {
		c.PageGap = v
		c.PageGapVertical = v
	}),
	"spread_gap": intOption("Horizontal spread gap; aliases page_gap_horizontal.", func(c *Config) int { return c.SpreadGap }, func(c *Config, v int) {
		c.SpreadGap = v
		c.PageGapHorizontal = v
	}),
	"page_gap_vertical": intOption("Vertical gap between page rows.", func(c *Config) int { return c.PageGapVertical }, func(c *Config, v int) {
		c.PageGapVertical = v
		c.PageGap = v
	}),
	"page_gap_horizontal": intOption("Horizontal gap between pages in a spread.", func(c *Config) int { return c.PageGapHorizontal }, func(c *Config, v int) {
		c.PageGapHorizontal = v
		c.SpreadGap = v
	}),
	"sequence_timeout_ms": intOption("Maximum delay between keys in a binding sequence.", func(c *Config) int { return c.SequenceTimeoutMS }, func(c *Config, v int) { c.SequenceTimeoutMS = v }),
	"animation_frame_ms":  intOption("Animation timestep in milliseconds; 0 follows the display's refresh rate.", func(c *Config) int { return c.AnimationFrameMS }, func(c *Config, v int) { c.AnimationFrameMS = max(0, v) }),
	"render_threads":      intOption("Page rendering threads; 0 picks one per core up to 4, leaving a core free. Applies to newly opened documents.", func(c *Config) int { return c.RenderThreads }, func(c *Config, v int) { c.RenderThreads = max(0, v) }),
	"render_oversample":   floatOption("Render scale multiplier; values above 1 supersample.", func(c *Config) float64 { return c.RenderOversample }, func(c *Config, v float64) { c.RenderOversample = v }),
	"smooth_scroll_dampening": floatOption("Catch-up factor for smooth scrolling per animation frame; higher values are more responsive and less damped; clamped to 0.01 through 1.", func(c *Config) float64 { return c.SmoothScrollDampening }, func(c *Config, v float64) {
		c.SmoothScrollDampening = max(0.01, min(1, v))
	}),
	"smooth_zoom_dampening": floatOption("Catch-up factor for smooth zooming per animation frame; higher values are more responsive and less damped; clamped to 0.01 through 1.", func(c *Config) float64 { return c.SmoothZoomDampening }, func(c *Config, v float64) {
		c.SmoothZoomDampening = max(0.01, min(1, v))
	}),
	"autoscroll_speed_factor": floatOption("Multiplier for autoscroll speed at every pointer distance from the anchor, up to autoscroll_max_speed; clamped to at least 0.01.", func(c *Config) float64 { return c.AutoscrollSpeedFactor }, func(c *Config, v float64) {
		c.AutoscrollSpeedFactor = max(0.01, v)
	}),
	"autoscroll_max_speed": floatOption("Fastest autoscroll speed in pixels per second, however far the pointer is from the anchor; 0 or less removes the limit.", func(c *Config) float64 { return c.AutoscrollMaxSpeed }, func(c *Config, v float64) {
		c.AutoscrollMaxSpeed = max(0, v)
	}),
	"min_zoom": floatOption("Minimum manual zoom scale.", func(c *Config) float64 { return c.MinZoom }, func(c *Config, v float64) {
		if v > 0 {
			c.MinZoom = v
		}
	}),
	"max_zoom": floatOption("Maximum manual zoom scale.", func(c *Config) float64 { return c.MaxZoom }, func(c *Config, v float64) {
		if v > 0 {
			c.MaxZoom = v
		}
	}),
	"zoom_step": floatOption("Factor zoom_in multiplies the zoom by, and zoom_out divides it by; above 1.", func(c *Config) float64 { return c.ZoomStep }, func(c *Config, v float64) {
		if v > 1 {
			c.ZoomStep = v
		}
	}),
	"pinch_sensitivity": floatOption("Trackpad pinch zoom sensitivity; 1 preserves the gesture scale.", func(c *Config) float64 { return c.PinchSensitivity }, func(c *Config, v float64) {
		if v > 0 {
			c.PinchSensitivity = v
		}
	}),
	"renderer":           stringOption(`Renderer SDL draws with, read when onon starts: "auto" for SDL's own choice, "gpu" for its GPU renderer (Vulkan, Metal or Direct3D 12), or one of SDL's drivers, such as "vulkan", "opengl", "metal", "direct3d11" or "software". One that cannot start falls back to "auto"; SDL_RENDER_DRIVER in the environment overrides it.`, func(c *Config) string { return c.Renderer }, func(c *Config, v string) { c.Renderer = NormalizeRenderer(v) }),
	"render_mode":        stringOption("Initial render mode: continuous or single.", func(c *Config) string { return c.RenderMode }, func(c *Config, v string) { c.RenderMode = NormalizeRenderMode(v) }),
	"hint_chars":         stringOption("Characters used for link hint labels, in order of preference.", func(c *Config) string { return c.HintChars }, func(c *Config, v string) { c.HintChars = v }),
	"fit_mode":           stringOption("Initial fit mode: page, width, height, or manual.", func(c *Config) string { return c.FitMode }, func(c *Config, v string) { c.FitMode = NormalizeFitMode(v) }),
	"anchor_position":    stringOption("Viewport anchor: center, top, or bottom.", func(c *Config) string { return c.AnchorPosition }, func(c *Config, v string) { c.AnchorPosition = NormalizeAnchorPosition(v) }),
	"status_bar_visible": boolOption("Show the status bar.", func(c *Config) bool { return c.StatusBarVisible }, func(c *Config, v bool) { c.StatusBarVisible = v }),
	"status_bar_left":    stringOption("Left status bar template.", func(c *Config) string { return c.StatusBarLeft }, func(c *Config, v string) { c.StatusBarLeft = v }),
	"status_bar_right":   stringOption("Right status bar template.", func(c *Config) string { return c.StatusBarRight }, func(c *Config, v string) { c.StatusBarRight = v }),
}

func readColor(tbl *lua.LTable, fallback [3]uint8) [3]uint8 {
	out := fallback
	for i := 1; i <= 3; i++ {
		if v := tbl.RawGetInt(i); v.Type() == lua.LTNumber {
			n := int(lua.LVAsNumber(v))
			if n < 0 {
				n = 0
			}
			if n > 255 {
				n = 255
			}
			out[i-1] = uint8(n)
		}
	}
	return out
}

func NormalizeFitMode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "width" || s == "height" || s == "manual" {
		return s
	}
	return "page"
}

func NormalizeRenderMode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "single" {
		return s
	}
	return "continuous"
}

// NormalizeRenderer lower-cases a renderer name, which is "auto" when
// empty. Other names are SDL's to accept or refuse.
func NormalizeRenderer(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "auto"
	}
	return s
}

func NormalizeAnchorPosition(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "top" || s == "bottom" {
		return s
	}
	return "center"
}

func init() {
	registerOption("alt_colors", altColorsOption())
}

// altColorsOption starts in alternate colors, true or false, or with
// "system" follows the OS's dark mode.
func altColorsOption() optionDesc {
	set := func(c *Config, value string) error {
		if strings.EqualFold(strings.TrimSpace(value), "system") {
			c.AltColorsSystem = true
			return nil
		}
		on, err := parseBoolOption(value)
		if err != nil {
			return fmt.Errorf(`expected true, false or "system"`)
		}
		c.AltColors, c.AltColorsSystem = on, false
		return nil
	}
	return optionDesc{
		kind:        "boolean",
		description: `Start with alternate colors: true, false, or "system" to follow the OS's dark mode as it changes.`,
		get: func(L *lua.LState, c *Config) lua.LValue {
			if c.AltColorsSystem {
				return lua.LString("system")
			}
			return lua.LBool(c.AltColors)
		},
		format: func(c *Config) string {
			if c.AltColorsSystem {
				return `"system"`
			}
			return strconv.FormatBool(c.AltColors)
		},
		applyText: func(c *Config, raw string) error {
			value, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			return set(c, value)
		},
		apply: func(c *Config, value lua.LValue) error {
			switch value := value.(type) {
			case lua.LBool:
				c.AltColors, c.AltColorsSystem = bool(value), false
				return nil
			case lua.LString:
				return set(c, string(value))
			}
			return fmt.Errorf(`expected true, false or "system"`)
		},
	}
}
