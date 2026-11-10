package config

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPluginsAreLazyAndExposeModuleConfiguration(t *testing.T) {
	pluginRoot := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample", {
  options = {
    enabled = { type = "boolean", default = false, description = "Enable the sample plugin." },
    command = { type = "string", default = "sample", description = "Sample command." }
  }
})
M:register_action("hello", { description = "Say hello" }, function()
  onon.message("hello")
end)
M:register_command("say", { description = ":sample-say - Say something" }, function(ctx)
  onon.message(ctx.args[1] or "nothing")
end)
M:on("document_opened", function(event)
  onon.message("opened " .. event.document.name)
end)
return M
`)

	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), filepath.Join(t.TempDir(), "paper.pdf"), OpenOptions{PluginPaths: []string{pluginRoot}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if containsString(rt.ActionNames(), "sample.hello") {
		t.Fatal("discovered plugin should not be active before require")
	}
	if containsString(rt.OptionNames(), "sample.enabled") {
		t.Fatal("discovered plugin options should not be active before require")
	}

	dirty, err := rt.Eval(`
sample = require("sample")
assert(sample.enabled == false)
sample.enabled = true
sample.command = "sample-tool"
onon.bind("X", sample.actions.hello)
`)
	if err != nil || !dirty {
		t.Fatalf("require and configure plugin, dirty=%v err=%v", dirty, err)
	}
	if !containsString(rt.ActionNames(), "sample.hello") {
		t.Fatal("required plugin action was not registered")
	}
	if !containsString(rt.OptionNames(), "sample.enabled") {
		t.Fatal("required plugin option was not registered")
	}
	if got, err := rt.OptionValue("sample.command"); err != nil || got != `"sample-tool"` {
		t.Fatalf("plugin option value = %q, %v", got, err)
	}
	if got := rt.Config().KeyBindings["X"]; got != "sample.hello" {
		t.Fatalf("expected plugin action binding, got %q", got)
	}

	host := &stubHost{}
	rt.AttachHost(host)
	if handled, _, err := rt.RunAction("sample.hello"); !handled || err != nil {
		t.Fatalf("run plugin action, handled=%v err=%v", handled, err)
	}
	if host.message != "hello" {
		t.Fatalf("expected plugin action message, got %q", host.message)
	}

	if handled, err := rt.RunPluginCommand("sample-say", "hello world"); !handled || err != nil {
		t.Fatalf("run plugin command, handled=%v err=%v", handled, err)
	}
	if host.message != "hello" {
		t.Fatalf("expected plugin command message, got %q", host.message)
	}

	rt.EmitPluginEvent("document_opened", map[string]any{
		"document": map[string]any{"name": "paper.pdf"},
	})
	if host.message != "opened paper.pdf" {
		t.Fatalf("expected document event message, got %q", host.message)
	}
}

func TestPluginActionBindingCanBeLoadedBeforePluginRequire(t *testing.T) {
	pluginRoot := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample")
M:register_action("hello", function() end)
return M
`)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(configPath, []byte(`onon.bind("X", "sample.hello")`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := OpenWithOptions(configPath, "", OpenOptions{PluginPaths: []string{pluginRoot}})
	if err == nil {
		rt.Close()
		t.Fatal("expected a user config binding to require an active plugin")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("expected unknown action error, got %v", err)
	}
}

func TestPluginReloadRequiresPluginAgain(t *testing.T) {
	pluginRoot := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample", {
  options = { value = { type = "integer", default = 1 } }
})
return M
`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{pluginRoot}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("sample")`); err != nil {
		t.Fatal(err)
	}
	if !containsString(rt.OptionNames(), "sample.value") {
		t.Fatal("expected active plugin option")
	}
	if err := rt.Reload(); err != nil {
		t.Fatal(err)
	}
	if containsString(rt.OptionNames(), "sample.value") {
		t.Fatal("plugin should not remain active after reload without require")
	}
}

func TestPluginCannotRegisterAsAnotherPlugin(t *testing.T) {
	firstRoot := writeTestPlugin(t, "first", `return onon.plugin.register("second")`)
	secondRoot := writeTestPlugin(t, "second", `return onon.plugin.register("second")`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{firstRoot, secondRoot}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("first")`); err == nil || !strings.Contains(err.Error(), `can only register while require("second") is loading`) {
		t.Fatalf("expected plugin identity error, got %v", err)
	}
}

func TestPluginManifestModuleSelectsEntrypoint(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sample")
	if err := os.MkdirAll(filepath.Join(dir, "lua", "entry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "onon-plugin.json"), []byte(`{"id":"sample","module":"entry.main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lua", "entry", "main.lua"), []byte(`return onon.plugin.register("sample")`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`assert(require("sample").id == "sample")`); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedPluginManifestIsRejected(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "onon-plugin.json"), []byte(`{`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := discoverPluginCatalog([]string{root}, nil)
	if _, ok := catalog.manifests["broken"]; ok || len(catalog.warnings) == 0 {
		t.Fatalf("expected malformed manifest rejection, catalog=%+v", catalog)
	}
}

func TestHiddenPluginDirectoriesAreSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	catalog := discoverPluginCatalog([]string{root}, nil)
	if len(catalog.manifests) != 0 || len(catalog.warnings) != 0 {
		t.Fatalf("expected .git to be skipped, catalog=%+v", catalog)
	}
}

func TestNoPluginsIgnoresExplicitPluginPaths(t *testing.T) {
	root := writeTestPlugin(t, "sample", `return onon.plugin.register("sample")`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}, NoPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("sample")`); err == nil {
		t.Fatal("expected --no-plugins semantics to suppress explicit plugin paths")
	}
}

func TestFailedReloadPreservesRunningPluginJobs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(configPath, []byte(`-- valid config`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := Open(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	ctx, cancel := context.WithCancel(context.Background())
	rt.operations[1] = &pluginOperation{id: 1, generation: rt.pluginGeneration, kind: "job", cancel: cancel}
	if err := os.WriteFile(configPath, []byte(`this is not lua`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rt.Reload(); err == nil {
		t.Fatal("expected reload failure")
	}
	if _, ok := rt.operations[1]; !ok {
		t.Fatal("expected failed reload to preserve the old job")
	}
	select {
	case <-ctx.Done():
		t.Fatal("expected preserved job context to remain active")
	default:
	}
}

func TestFailedPluginEntrypointRollsBackRegistration(t *testing.T) {
	root := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample")
M:register_action("partial", function() end)
error("entrypoint failed")
`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("sample")`); err == nil {
		t.Fatal("expected entrypoint failure")
	}
	if containsString(rt.ActionNames(), "sample.partial") {
		t.Fatal("failed plugin left a partial action registered")
	}
}

func TestPluginEntrypointMustRegister(t *testing.T) {
	root := writeTestPlugin(t, "sample", `return {}`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("sample")`); err == nil || !strings.Contains(err.Error(), "did not call onon.plugin.register") {
		t.Fatalf("expected registration postcondition error, got %v", err)
	}
}

func TestPluginLocalModulesAreIsolated(t *testing.T) {
	first := writeTestPlugin(t, "first", `
local util = require("util")
local M = onon.plugin.register("first")
M:register_action("value", function() onon.message(util.value) end)
return M
`)
	second := writeTestPlugin(t, "second", `
local util = require("util")
local M = onon.plugin.register("second")
M:register_action("value", function() onon.message(util.value) end)
return M
`)
	if err := os.WriteFile(filepath.Join(first, "first", "lua", "util.lua"), []byte(`return { value = "first" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "second", "lua", "util.lua"), []byte(`return { value = "second" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("first"); require("second")`); err != nil {
		t.Fatal(err)
	}
	host := &stubHost{}
	rt.AttachHost(host)
	if _, _, err := rt.RunAction("first.value"); err != nil || host.message != "first" {
		t.Fatalf("first plugin local module = %q, err=%v", host.message, err)
	}
	if _, _, err := rt.RunAction("second.value"); err != nil || host.message != "second" {
		t.Fatalf("second plugin local module = %q, err=%v", host.message, err)
	}
}

func TestPluginLocalModulesResolveAsLuaRequireDoes(t *testing.T) {
	root := writeTestPlugin(t, "sample", `
local helper = require("sample.helper")
local util = require("util")
assert(not pcall(require, "sample.util"), "sample.util found lua/util.lua")
local M = onon.plugin.register("sample")
M.value = helper.value .. " " .. util.value
return M
`)
	dir := filepath.Join(root, "sample", "lua")
	for path, source := range map[string]string{
		"sample/helper.lua": `return { value = "beside the entrypoint" }`,
		"util.lua":          `return { value = "at the root" }`,
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`assert(require("sample").value == "beside the entrypoint at the root")`); err != nil {
		t.Fatal(err)
	}
}

func TestDeferredOpenRunsAfterAllEventSubscribers(t *testing.T) {
	second := filepath.Join(t.TempDir(), "second.pdf")
	root := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample")
M:on("app_ready", function() onon.open(`+strconv.Quote(second)+`) end)
M:on("app_ready", function() onon.message("all callbacks ran") end)
return M
`)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(configPath, []byte(`require("sample")`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := OpenWithOptions(configPath, filepath.Join(dir, "first.pdf"), OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	host := &reloadingOpenHost{rt: rt}
	rt.AttachHost(host)
	rt.EmitPluginEvent("app_ready", map[string]any{})
	if host.opened != second || host.message != "all callbacks ran" {
		t.Fatalf("expected all callbacks before deferred open, host=%+v", host)
	}
}

func TestPluginCatalogRefreshesOnReload(t *testing.T) {
	root := t.TempDir()
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	dir := filepath.Join(root, "late")
	if err := os.MkdirAll(filepath.Join(dir, "lua", "late"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "onon-plugin.json"), []byte(`{"id":"late"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lua", "late", "init.lua"), []byte(`return onon.plugin.register("late")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rt.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`require("late")`); err != nil {
		t.Fatalf("new plugin was not discovered after reload: %v", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := rt.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`require("late")`); err == nil {
		t.Fatal("removed plugin remained discoverable after reload")
	}
}

func TestPluginEventsFollowDependencyActivationOrder(t *testing.T) {
	dependencyRoot := writeTestPlugin(t, "zzz", `
local M = onon.plugin.register("zzz")
M:on("app_ready", function() onon.message(onon.message() .. "dependency,") end)
return M
`)
	pluginRoot := writeTestPlugin(t, "aaa", `
local M = onon.plugin.register("aaa")
M:on("app_ready", function() onon.message(onon.message() .. "dependent") end)
return M
`)
	manifestPath := filepath.Join(pluginRoot, "aaa", "onon-plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{"id":"aaa","version":"0.1.0","dependencies":["zzz"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{pluginRoot, dependencyRoot}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("aaa")`); err != nil {
		t.Fatal(err)
	}
	host := &stubHost{}
	rt.AttachHost(host)
	rt.EmitPluginEvent("app_ready", map[string]any{})
	if host.message != "dependency,dependent" {
		t.Fatalf("unexpected event order %q", host.message)
	}
}

func writeTestPlugin(t *testing.T, id, source string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(filepath.Join(dir, "lua", id), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"` + id + `","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, "onon-plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lua", id, "init.lua"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestPluginCommandNamesCannotContainHyphens(t *testing.T) {
	root := writeTestPlugin(t, "sample", `
local M = onon.plugin.register("sample")
M:register_command("say-hi", function() end)
return M
`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	// "sample-say-hi" would clash with command "hi" of a plugin "sample-say".
	if _, err := rt.Eval(`require("sample")`); err == nil || !strings.Contains(err.Error(), `invalid name "say-hi"`) {
		t.Fatalf("expected hyphenated command name to be rejected, err=%v", err)
	}
}

func TestPluginMembersOfHyphenatedIDsResolve(t *testing.T) {
	root := writeTestPlugin(t, "file-browser", `
local M = onon.plugin.register("file-browser", {
  options = { root = { type = "string", default = "/", description = "Root." } }
})
M:register_action("open", function() onon.message("action") end)
M:register_command("open", { arg_completions = { "here", "there" } }, function(ctx) onon.message(ctx.name) end)
return M
`)
	rt, err := OpenWithOptions(filepath.Join(t.TempDir(), "missing.lua"), "", OpenOptions{PluginPaths: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if _, err := rt.Eval(`require("file-browser")`); err != nil {
		t.Fatal(err)
	}
	host := &stubHost{}
	rt.AttachHost(host)
	if handled, err := rt.RunPluginCommand("File-Browser-Open", ""); !handled || err != nil || host.message != "file-browser-open" {
		t.Fatalf("command: handled=%v err=%v message=%q", handled, err, host.message)
	}
	if got := rt.commandCompletions("file-browser-open", "th"); len(got) != 1 || got[0] != "there" {
		t.Fatalf("completions = %v", got)
	}
	if !rt.actionExists("file-browser.open") || rt.actionExists("file-browser.close") {
		t.Fatal("action lookup")
	}
	if _, ok := rt.pluginOption("file-browser.root"); !ok {
		t.Fatal("option lookup")
	}
}
