package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// NewLuaPlatformModule returns the synchronous platform information exposed to Lua.
func NewLuaPlatformModule(L *lua.LState) *lua.LTable {
	mod := L.NewTable()
	L.SetField(mod, "os", lua.LString(LuaOSName(runtime.GOOS)))
	L.SetField(mod, "arch", lua.LString(runtime.GOARCH))
	L.SetField(mod, "hostname", lua.LString(hostname()))
	L.SetField(mod, "home_dir", lua.LString(homeDir()))
	L.SetField(mod, "config_dir", lua.LString(platformLuaConfigDir()))
	L.SetField(mod, "data_dir", lua.LString(DataDir()))
	L.SetField(mod, "cache_dir", lua.LString(platformLuaCacheDir()))
	L.SetField(mod, "temp_dir", lua.LString(os.TempDir()))
	return mod
}

// LuaOSName is how onon.platform.os names goos.
func LuaOSName(goos string) string {
	if goos == "darwin" {
		return "macos"
	}
	return goos
}

// hostname is the machine's name up to its first dot, as `hostname -s` gives it.
func hostname() string {
	name, _ := os.Hostname()
	short, _, _ := strings.Cut(name, ".")
	return short
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

func platformLuaConfigDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "onon")
	}
	return ""
}

func platformLuaCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "onon")
	}
	return ""
}
