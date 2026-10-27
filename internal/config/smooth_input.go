package config

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

type SmoothInputSources uint8

const (
	SmoothInputMouse SmoothInputSources = 1 << iota
	SmoothInputTrackpad
	SmoothInputKeyboard
	SmoothInputAll = SmoothInputMouse | SmoothInputTrackpad | SmoothInputKeyboard
	// SmoothInputWheel is the sources that scroll with a wheel or swipe,
	// the only ones scrolling can be inverted for.
	SmoothInputWheel = SmoothInputMouse | SmoothInputTrackpad
)

var inputSourceNames = []struct {
	name   string
	source SmoothInputSources
}{
	{"mouse", SmoothInputMouse},
	{"trackpad", SmoothInputTrackpad},
	{"keyboard", SmoothInputKeyboard},
}

func (s SmoothInputSources) Has(source SmoothInputSources) bool {
	return s&source != 0
}

func FormatSmoothInputSources(sources SmoothInputSources) string {
	return formatInputSources(sources, SmoothInputAll)
}

// formatInputSources writes sources as the table an option takes, with a
// field for each source in allowed.
func formatInputSources(sources, allowed SmoothInputSources) string {
	var fields []string
	for _, n := range inputSourceNames {
		if allowed.Has(n.source) {
			fields = append(fields, fmt.Sprintf("%s = %t", n.name, sources.Has(n.source)))
		}
	}
	return "{ " + strings.Join(fields, ", ") + " }"
}

func inputSourcesTable(L *lua.LState, sources, allowed SmoothInputSources) *lua.LTable {
	table := L.NewTable()
	for _, n := range inputSourceNames {
		if allowed.Has(n.source) {
			table.RawSetString(n.name, lua.LBool(sources.Has(n.source)))
		}
	}
	return table
}

// parseInputSourcesValue reads a source table, or a boolean that sets
// every allowed source, from Lua. It returns the sources set true and the
// sources the value named, so that a table naming some sources leaves the
// rest as they were.
func parseInputSourcesValue(value lua.LValue, option string, allowed SmoothInputSources) (SmoothInputSources, SmoothInputSources, error) {
	if value.Type() == lua.LTBool {
		if lua.LVAsBool(value) {
			return allowed, allowed, nil
		}
		return 0, allowed, nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return 0, 0, fmt.Errorf("expected table or boolean")
	}
	var names []string
	known := map[string]SmoothInputSources{}
	for _, n := range inputSourceNames {
		if allowed.Has(n.source) {
			names = append(names, n.name)
			known[n.name] = n.source
		}
	}
	var sources, present SmoothInputSources
	var parseErr error
	table.ForEach(func(key, value lua.LValue) {
		if parseErr != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok {
			parseErr = fmt.Errorf("%s table keys must be %s", option, strings.Join(names, ", "))
			return
		}
		source, ok := known[string(name)]
		if !ok {
			parseErr = fmt.Errorf("unknown %s source %q", option, string(name))
			return
		}
		if value.Type() != lua.LTBool {
			parseErr = fmt.Errorf("%s.%s must be boolean", option, string(name))
			return
		}
		present |= source
		if lua.LVAsBool(value) {
			sources |= source
		}
	})
	return sources, present, parseErr
}

func parseInputSourcesText(raw, option string, allowed SmoothInputSources) (SmoothInputSources, SmoothInputSources, error) {
	state := lua.NewState()
	defer state.Close()

	fn, err := state.Load(strings.NewReader("return "+strings.TrimSpace(raw)), option)
	if err != nil {
		return 0, 0, fmt.Errorf("expected table or boolean")
	}
	if err := state.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		return 0, 0, fmt.Errorf("expected table or boolean")
	}
	return parseInputSourcesValue(state.Get(-1), option, allowed)
}
