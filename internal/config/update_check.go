package config

import (
	"context"

	"onon/internal/update"

	lua "github.com/yuin/gopher-lua"
)

// CheckUpdates asks GitHub whether a newer onon release exists, for this
// build's Version and InstallMethod. It is what :check-updates and
// onon.check_updates both check with.
func CheckUpdates(ctx context.Context) (update.Info, error) {
	return checkUpdates(ctx)
}

// checkUpdates is replaced by tests, so that none reaches GitHub.
var checkUpdates = func(ctx context.Context) (update.Info, error) {
	return update.Check(ctx, Version, InstallMethod)
}

// luaCheckUpdates asks GitHub, in the background, whether a newer onon
// release exists, as :check-updates does, and calls a callback with what it
// found.
//
// # Parameters
//
//   - callback: Receives an [Update info](#update-info) table once the check
//     finishes. Builds that are not a release, such as dev builds, are not
//     checked: success is false and error says why.
//
// # Returns
//
// An operation handle with numeric id, cancel(), and active() members.
//
// # Example
//
//	onon.check_updates(function(info)
//	  if info.available then
//	    onon.message("onon " .. info.latest .. " is available")
//	  end
//	end)
func luaCheckUpdates(rt *Runtime) lua.LGFunction {
	return func(L *lua.LState) int {
		fn, ok := L.Get(1).(*lua.LFunction)
		if !ok {
			L.RaiseError("check_updates: expected callback")
		}
		id := rt.startPluginOperation(rt.owningPluginID(), "check_updates", fn, func(ctx context.Context) map[string]any {
			info, err := CheckUpdates(ctx)
			return map[string]any{
				"success":   err == nil,
				"error":     errorString(err),
				"current":   info.Current,
				"latest":    info.Latest,
				"available": info.Available,
				"method":    info.Method,
				"url":       info.URL,
			}
		})
		L.Push(newPluginOperationHandle(L, rt, id))
		return 1
	}
}
