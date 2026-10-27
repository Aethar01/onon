package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Schemes registered from Lua turn names such as arxiv:2401.12345 or
// doi:10.1000/182 into a URL or path to open, so sources of documents can be
// added without onon knowing them.

var schemeName = regexp.MustCompile(`^[a-z][a-z0-9+.-]+$`)

// builtinSchemes are the schemes onon opens itself, which Lua cannot take.
var builtinSchemes = map[string]bool{
	"file": true, "http": true, "https": true, "sftp": true, "ssh": true, "scp": true,
	"dav": true, "davs": true, "webdav": true, "webdavs": true,
}

// maxSchemeDepth limits schemes resolving to other schemes, so two that
// name each other end with an error rather than hanging.
const maxSchemeDepth = 8

// luaRegisterScheme makes name: a scheme that :open, onon.open and the
// command line accept, resolved by a function to the URL or path to open.
//
// # Parameters
//
//   - name: The scheme, two or more letters, digits, +, - or ., starting with a letter, as in "arxiv". Built-in schemes such as https and sftp cannot be taken.
//   - resolve: A function given the text after name: (and any //) and the whole argument, returning the URL or path to open. It may return another registered scheme.
//
// # Returns
//
// Nothing. A later registration of the same name replaces it.
//
// # Errors
//
// Raises an error when name is not a valid scheme or is built in. When resolve raises an error or returns nothing, opening fails with that error.
//
// # Example
//
//	onon.register_scheme("arxiv", function(id)
//	  return "https://arxiv.org/pdf/" .. id
//	end)
func luaRegisterScheme(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		name := strings.ToLower(L.CheckString(1))
		resolve := L.CheckFunction(2)
		if !schemeName.MatchString(name) {
			L.ArgError(1, fmt.Sprintf("%q is not a scheme name", name))
		}
		if builtinSchemes[name] {
			L.ArgError(1, fmt.Sprintf("%s is built in", name))
		}
		if rt.schemes == nil {
			rt.schemes = map[string]*lua.LFunction{}
		}
		rt.schemes[name] = resolve
		return 0
	}
}

// HasScheme reports whether arg starts with a scheme registered from Lua.
func (r *Runtime) HasScheme(arg string) bool {
	if r == nil {
		return false
	}
	name, _, ok := strings.Cut(arg, ":")
	return ok && r.schemes[strings.ToLower(name)] != nil
}

// ResolveScheme turns arg, if it starts with a scheme registered from Lua,
// into what that scheme names, and reports whether it did. A local file of
// the same name wins, as it does over scp-style host:path.
func (r *Runtime) ResolveScheme(arg string) (string, bool, error) {
	if !r.HasScheme(arg) {
		return arg, false, nil
	}
	if _, err := os.Lstat(arg); err == nil {
		return arg, false, nil
	}
	original := arg
	for depth := 0; ; depth++ {
		name, rest, ok := strings.Cut(arg, ":")
		resolve := r.schemes[strings.ToLower(name)]
		if !ok || resolve == nil {
			return arg, true, nil
		}
		if depth == maxSchemeDepth {
			return "", true, fmt.Errorf("%s: schemes resolve to each other in a loop", original)
		}
		var result lua.LValue
		err := r.doLua(func() error {
			if err := r.state.CallByParam(lua.P{Fn: resolve, NRet: 1, Protect: true}, lua.LString(strings.TrimPrefix(rest, "//")), lua.LString(arg)); err != nil {
				return err
			}
			result = r.state.Get(-1)
			r.state.Pop(1)
			return nil
		})
		if err != nil {
			return "", true, fmt.Errorf("%s: %w", original, err)
		}
		if result.Type() != lua.LTString || result.String() == "" {
			return "", true, fmt.Errorf("%s: the %s scheme gave nothing to open", original, name)
		}
		arg = result.String()
	}
}
