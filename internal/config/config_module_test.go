package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// openConfigRuntime loads source as config.lua, with a plugin named sample
// whose module records, in order, who heard app_ready.
func openConfigRuntime(t *testing.T, source string) *Runtime {
	t.Helper()
	pluginRoot := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample")
M:on("app_ready", function() M.heard = (M.heard or "") .. "sample " end)
return M
`)
	configPath := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(configPath, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := OpenWithOptions(configPath, "", OpenOptions{PluginPaths: []string{pluginRoot}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	return rt
}

func TestConfigModuleRegistersActionsCommandsAndEvents(t *testing.T) {
	rt := openConfigRuntime(t, `
local cfg = onon.config
assert(cfg.id == "config")
local hello = cfg:register_action("hello", { description = "Say hello" }, function() onon.message("hello") end)
onon.bind("X", hello)
cfg:register_command("say", function(ctx) onon.message(ctx.args[1]) end)
cfg:on("app_ready", function() sample.heard = (sample.heard or "") .. "config " end)
sample = require("sample")
`)
	if got := rt.Config().KeyBindings["X"]; got != "config.hello" {
		t.Fatalf("binding = %q, want config.hello", got)
	}
	host := &stubHost{}
	rt.AttachHost(host)
	if handled, _, err := rt.RunAction("config.hello"); !handled || err != nil || host.message != "hello" {
		t.Fatalf("run config action: handled=%v err=%v message=%q", handled, err, host.message)
	}
	if handled, err := rt.RunPluginCommand("config-say", "hi"); !handled || err != nil || host.message != "hi" {
		t.Fatalf("run config command: handled=%v err=%v message=%q", handled, err, host.message)
	}
	// The config subscribed before requiring the plugin, yet hears the event after it.
	rt.EmitPluginEvent("app_ready", map[string]any{})
	if got := luaStringField(rt, "sample", "heard"); got != "sample config " {
		t.Fatalf("event order = %q, want plugins first, then config", got)
	}
}

func TestConfigModuleTimer(t *testing.T) {
	rt := openConfigRuntime(t, `
onon.config.timer:after(1, function() fired = true end)
`)
	pollRuntimeUntil(t, rt, func() bool { return evalBool(rt, `fired == true`) })
}

func TestConfigModuleRegistersOnlyWhileConfigLoads(t *testing.T) {
	rt := openConfigRuntime(t, `
onon.bind("Y", function() onon.config:on("app_ready", function() end) end)
`)
	if _, err := rt.Eval(`onon.config:register_action("late", function() end)`); err == nil || !strings.Contains(err.Error(), "only be called while the plugin is loading") {
		t.Fatalf("register after load: err=%v", err)
	}
	// A plugin config.lua requires cannot register on the config's behalf.
	pluginRoot := writeTestPlugin(t, "sneaky", `
onon.config:register_action("sneaky", function() end)
return onon.plugin.register("sneaky")
`)
	configPath := filepath.Join(t.TempDir(), "config.lua")
	if err := os.WriteFile(configPath, []byte(`require("sneaky")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if rt, err := OpenWithOptions(configPath, "", OpenOptions{PluginPaths: []string{pluginRoot}}); err == nil {
		rt.Close()
		t.Fatal("a plugin registered an action on onon.config")
	}
}

func TestPluginCannotTakeTheConfigID(t *testing.T) {
	root := writeTestPlugin(t, "config", `return onon.plugin.register("config")`)
	catalog := discoverPluginCatalog([]string{root}, nil)
	if _, ok := catalog.manifests["config"]; ok {
		t.Fatal("a plugin was discovered with the configuration's ID")
	}
	if len(catalog.warnings) == 0 || !strings.Contains(catalog.warnings[0], "configuration's own") {
		t.Fatalf("warnings = %q", catalog.warnings)
	}
}
