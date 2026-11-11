package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func newPortableLuaState(t *testing.T) *lua.LState {
	t.Helper()
	L := lua.NewState()
	PreloadPortableLuaModules(L)
	t.Cleanup(L.Close)
	return L
}

func TestLuaPlatformModule(t *testing.T) {
	L := newPortableLuaState(t)
	wantOS := runtime.GOOS
	if wantOS == "darwin" {
		wantOS = "macos"
	}
	script := `
local platform = require("onon.platform")
assert(platform.os == ` + luaQuote(wantOS) + `)
assert(platform.arch == ` + luaQuote(runtime.GOARCH) + `)
assert(type(platform.hostname) == "string" and not platform.hostname:find(".", 1, true))
assert(type(platform.home_dir) == "string")
assert(type(platform.config_dir) == "string")
assert(type(platform.data_dir) == "string")
assert(type(platform.cache_dir) == "string")
assert(platform.temp_dir == ` + luaQuote(os.TempDir()) + `)
`
	if err := L.DoString(script); err != nil {
		t.Fatal(err)
	}
}

func TestLuaPathModule(t *testing.T) {
	L := newPortableLuaState(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wantJoined := filepath.Join("one", "two", "file.pdf")
	wantExpanded := filepath.Join(home, "docs", "file.pdf")
	script := `
local path = require("onon.path")
assert(path.join("one", "two", "file.pdf") == ` + luaQuote(wantJoined) + `)
assert(path.base(path.join("one", "file.pdf")) == "file.pdf")
assert(path.dir(path.join("one", "file.pdf")) == "one")
assert(path.ext("file.pdf") == ".pdf")
assert(path.expand_home("~/docs/file.pdf") == ` + luaQuote(wantExpanded) + `)
assert(path.expand_home("~someone/file.pdf") == "~someone/file.pdf")
assert(path.is_abs(path.abs("file.pdf")))
assert(path.rel("one", path.join("one", "two")) == "two")
`
	if err := L.DoString(script); err != nil {
		t.Fatal(err)
	}
}

// The path module delegates to path/filepath, so drive letters and backslashes
// are only meaningful on Windows. Asserting both branches keeps the portable
// contract explicit rather than assuming the host's rules.
func TestLuaPathModuleUsesNativeRules(t *testing.T) {
	L := newPortableLuaState(t)
	script := `
local path = require("onon.path")
assert(path.separator == ` + luaQuote(string(filepath.Separator)) + `)
`
	if runtime.GOOS == "windows" {
		script += `
assert(path.is_absolute("C:\\Users\\reader"))
assert(not path.is_absolute("Users\\reader"))
assert(path.join("C:\\Users", "reader", "a.pdf") == "C:\\Users\\reader\\a.pdf")
assert(path.clean("C:/Users/./reader/../reader") == "C:\\Users\\reader")
assert(path.dirname("C:\\Users\\a.pdf") == "C:\\Users")
assert(path.relative("C:\\Users", "C:\\Users\\reader") == "reader")
assert(path.expand_home("~\\docs") ~= "~\\docs")
`
	} else {
		script += `
-- A Windows-style path is an ordinary relative name on POSIX hosts.
assert(not path.is_absolute("C:\\Users\\reader"))
assert(path.is_absolute("/users/reader"))
assert(path.join("/users", "reader", "a.pdf") == "/users/reader/a.pdf")
assert(path.clean("/users/./reader/../reader") == "/users/reader")
assert(path.dirname("/users/a.pdf") == "/users")
assert(path.relative("/users", "/users/reader") == "reader")
`
	}
	if err := L.DoString(script); err != nil {
		t.Fatal(err)
	}
}

func TestLuaJSONRoundTripPreservesShapesAndNull(t *testing.T) {
	L := newPortableLuaState(t)
	if err := L.DoString(`
local json = require("onon.json")
assert(json.null == require("onon.json").null)
local value = json.decode('{"array":[],"object":{},"values":[null,true,2.5,"x"]}')
assert(value.values[1] == json.null)
assert(json.encode(value) == '{"array":[],"object":{},"values":[null,true,2.5,"x"]}')
assert(json.encode({1, 2, 3}) == '[1,2,3]')
assert(json.encode({answer = 42}) == '{"answer":42}')
`); err != nil {
		t.Fatal(err)
	}
}

func TestLuaJSONRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{"cycle", `local t = {}; t.self = t; require("onon.json").encode(t)`, "cycle detected"},
		{"function", `require("onon.json").encode(function() end)`, "unsupported function"},
		{"sparse array", `require("onon.json").encode({[2] = "x"})`, "contiguous positive integer keys"},
		{"mixed table", `require("onon.json").encode({"x", key = "y"})`, "contiguous positive integer keys"},
		{"nil", `require("onon.json").encode(nil)`, "use json.null"},
		{"trailing input", `require("onon.json").decode('{} {}')`, "unexpected data after JSON value"},
		{"truncated object", `require("onon.json").decode('{"a":')`, "unexpected"},
		{"unquoted key", `require("onon.json").decode('{a:1}')`, "invalid character"},
		{"empty input", `require("onon.json").decode('')`, "EOF"},
		{"nan", `require("onon.json").encode(0/0)`, "finite"},
		{"infinity", `require("onon.json").encode(1/0)`, "finite"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			L := newPortableLuaState(t)
			err := L.DoString(test.code)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func luaQuote(value string) string {
	return strconv.Quote(value)
}
