package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"onon/internal/actions"
	"onon/internal/commands"

	lua "github.com/yuin/gopher-lua"
)

type pluginManifest struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Module       string   `json:"module"`
	Dependencies []string `json:"dependencies"`
	Root         string   `json:"-"`
}

type pluginCatalog struct {
	manifests map[string]pluginManifest
	warnings  []string
	disabled  map[string]bool
}

type pluginState struct {
	runtime          *Runtime
	active           map[string]*pluginInstance
	activationOrder  []string
	nextSubscription int
}

type pluginInstance struct {
	runtime       *Runtime
	manifest      pluginManifest
	module        *lua.LTable
	actions       map[string]pluginAction
	commands      map[string]pluginCommand
	options       map[string]*pluginOption
	subscriptions map[int]pluginSubscription
	actionsTable  *lua.LTable
}

type pluginAction struct {
	plugin      string
	name        string
	fullName    string
	description string
	countable   bool
	function    *lua.LFunction
}

type pluginCommand struct {
	plugin         string
	name           string
	fullName       string
	description    string
	argCompletions []string
	function       *lua.LFunction
}

type pluginOption struct {
	name        string
	fullName    string
	kind        string
	description string
	value       lua.LValue
}

type pluginSubscription struct {
	plugin string
	event  string
	fn     *lua.LFunction
}

type limitedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.Buffer.Write(p[:remaining])
			b.truncated = true
		} else {
			_, _ = b.Buffer.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func discoverPluginCatalog(paths, disabled []string) *pluginCatalog {
	catalog := &pluginCatalog{
		manifests: make(map[string]pluginManifest),
		disabled:  make(map[string]bool, len(disabled)),
	}
	for _, id := range disabled {
		id = normalizePluginID(id)
		if id != "" {
			catalog.disabled[id] = true
		}
	}

	for _, base := range unique(paths) {
		entries, err := os.ReadDir(base)
		if err != nil {
			if !os.IsNotExist(err) {
				catalog.warnings = append(catalog.warnings, fmt.Sprintf("plugin directory %q: %v", base, err))
			}
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue // .git and the like, in a plugin directory that is a clone
			}
			dir := filepath.Join(base, entry.Name())
			manifest, ok, err := readPluginManifest(dir)
			if err != nil {
				catalog.warnings = append(catalog.warnings, fmt.Sprintf("plugin %q: %v", dir, err))
				continue
			}
			if !ok {
				manifest = pluginManifest{ID: entry.Name(), Module: entry.Name()}
			}
			if manifest.ID == "" {
				manifest.ID = entry.Name()
			}
			manifest.ID = normalizePluginID(manifest.ID)
			if !validPluginID(manifest.ID) {
				catalog.warnings = append(catalog.warnings, fmt.Sprintf("plugin %q has invalid id", dir))
				continue
			}
			if manifest.Module == "" {
				manifest.Module = manifest.ID
			}
			manifest.Root = dir
			if _, exists := catalog.manifests[manifest.ID]; exists {
				catalog.warnings = append(catalog.warnings, fmt.Sprintf("duplicate plugin %q; keeping the first one", manifest.ID))
				continue
			}
			catalog.manifests[manifest.ID] = manifest
			if catalog.disabled[manifest.ID] {
				continue
			}

		}
	}
	return catalog
}

func readPluginManifest(dir string) (pluginManifest, bool, error) {
	path := filepath.Join(dir, "onon-plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pluginManifest{}, false, nil
		}
		return pluginManifest{}, false, err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pluginManifest{}, false, fmt.Errorf("invalid onon-plugin.json: %w", err)
	}
	return manifest, true, nil
}

func installPluginPackagePath(L *lua.LState, runtime *Runtime) {
	catalog := runtime.pluginCatalog
	if L == nil || catalog == nil {
		return
	}
	packageTable, ok := L.GetGlobal("package").(*lua.LTable)
	if !ok {
		return
	}
	preload, ok := L.GetField(packageTable, "preload").(*lua.LTable)
	if !ok {
		return
	}
	ids := make([]string, 0, len(catalog.manifests))
	for id := range catalog.manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		manifest := catalog.manifests[id]
		L.SetField(preload, id, L.NewFunction(func(L *lua.LState) int {
			if catalog.disabled[manifest.ID] {
				L.RaiseError("plugin %q is disabled", manifest.ID)
			}
			for _, dependency := range manifest.Dependencies {
				dependency = normalizePluginID(dependency)
				if _, ok := catalog.manifests[dependency]; !ok {
					L.RaiseError("plugin %q requires missing dependency %q", manifest.ID, dependency)
				}
				if err := L.CallByParam(lua.P{Fn: L.GetGlobal("require"), NRet: 1, Protect: true}, lua.LString(dependency)); err != nil {
					L.RaiseError("plugin %q dependency %q: %v", manifest.ID, dependency, err)
				}
				L.Pop(1)
			}
			path, err := pluginEntrypoint(manifest)
			if err != nil {
				L.RaiseError("plugin %q: %v", manifest.ID, err)
			}
			chunk, err := L.LoadFile(path)
			if err != nil {
				L.RaiseError("plugin %q: %v", manifest.ID, err)
			}
			previous := runtime.loadingPlugin
			runtime.loadingPlugin = manifest.ID
			L.SetFEnv(chunk, newPluginEnvironment(L, runtime, manifest, packageTable))
			defer func() { runtime.loadingPlugin = previous }()
			if err := L.CallByParam(lua.P{Fn: chunk, NRet: 1, Protect: true}); err != nil {
				runtime.rollbackPluginLoad(manifest.ID)
				L.RaiseError("plugin %q: %v", manifest.ID, err)
			}
			instance := runtime.plugins.active[manifest.ID]
			if instance == nil {
				runtime.rollbackPluginLoad(manifest.ID)
				L.RaiseError("plugin %q did not call onon.plugin.register", manifest.ID)
			}
			L.Pop(1)
			L.Push(instance.module)
			return 1
		}))
	}
}

func newPluginEnvironment(L *lua.LState, runtime *Runtime, manifest pluginManifest, packageTable *lua.LTable) *lua.LTable {
	env := L.NewTable()
	metatable := L.NewTable()
	L.SetField(metatable, "__index", L.GetGlobal("_G"))
	L.SetMetatable(env, metatable)
	L.SetField(env, "_G", env)
	globalRequire := L.GetGlobal("require")
	loaded, _ := L.GetField(packageTable, "loaded").(*lua.LTable)
	L.SetField(env, "require", L.NewFunction(func(L *lua.LState) int {
		name := strings.TrimSpace(L.CheckString(1))
		if dependency, ok := runtime.pluginCatalog.manifests[normalizePluginID(name)]; ok {
			if dependency.ID != manifest.ID && !containsPluginDependency(manifest.Dependencies, dependency.ID) {
				L.RaiseError("plugin %q cannot require undeclared plugin %q", manifest.ID, dependency.ID)
			}
			if err := L.CallByParam(lua.P{Fn: globalRequire, NRet: 1, Protect: true}, lua.LString(dependency.ID)); err != nil {
				L.RaiseError("plugin %q require %q: %v", manifest.ID, name, err)
			}
			return 1
		}
		path, localName, ok := pluginLocalModulePath(manifest, name)
		if !ok {
			if err := L.CallByParam(lua.P{Fn: globalRequire, NRet: 1, Protect: true}, lua.LString(name)); err != nil {
				L.RaiseError("plugin %q require %q: %v", manifest.ID, name, err)
			}
			return 1
		}
		cacheKey := "onon-plugin:" + manifest.ID + ":" + localName
		if loaded != nil {
			if cached := loaded.RawGetString(cacheKey); cached != lua.LNil {
				L.Push(cached)
				return 1
			}
			loaded.RawSetString(cacheKey, lua.LTrue)
		}
		chunk, err := L.LoadFile(path)
		if err != nil {
			if loaded != nil {
				loaded.RawSetString(cacheKey, lua.LNil)
			}
			L.RaiseError("plugin %q module %q: %v", manifest.ID, name, err)
		}
		L.SetFEnv(chunk, env)
		if err := L.CallByParam(lua.P{Fn: chunk, NRet: 1, Protect: true}); err != nil {
			if loaded != nil {
				loaded.RawSetString(cacheKey, lua.LNil)
			}
			L.RaiseError("plugin %q module %q: %v", manifest.ID, name, err)
		}
		result := L.Get(-1)
		if result == lua.LNil {
			L.Pop(1)
			result = lua.LTrue
			L.Push(result)
		}
		if loaded != nil {
			loaded.RawSetString(cacheKey, result)
		}
		return 1
	}))
	return env
}

func containsPluginDependency(dependencies []string, id string) bool {
	for _, dependency := range dependencies {
		if normalizePluginID(dependency) == id {
			return true
		}
	}
	return false
}

func pluginLocalModulePath(manifest pluginManifest, name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, manifest.ID+".")
	if name == "" || strings.ContainsAny(name, `/\\`) || strings.Contains(name, "..") {
		return "", "", false
	}
	base := filepath.Join(manifest.Root, "lua")
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		base = manifest.Root
	}
	relative := filepath.FromSlash(strings.ReplaceAll(name, ".", "/"))
	for _, path := range []string{filepath.Join(base, relative+".lua"), filepath.Join(base, relative, "init.lua")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, name, true
		}
	}
	return "", "", false
}

func (r *Runtime) rollbackPluginLoad(id string) {
	if r.plugins != nil {
		delete(r.plugins.active, id)
		for i, activeID := range r.plugins.activationOrder {
			if activeID == id {
				r.plugins.activationOrder = append(r.plugins.activationOrder[:i], r.plugins.activationOrder[i+1:]...)
				break
			}
		}
	}
	r.cancelPluginOperationsFor(id)
	if r.state == nil {
		return
	}
	packageTable, _ := r.state.GetGlobal("package").(*lua.LTable)
	if packageTable == nil {
		return
	}
	loaded, _ := r.state.GetField(packageTable, "loaded").(*lua.LTable)
	if loaded == nil {
		return
	}
	prefix := "onon-plugin:" + id + ":"
	var keys []lua.LValue
	loaded.ForEach(func(key, _ lua.LValue) {
		if strings.HasPrefix(lua.LVAsString(key), prefix) {
			keys = append(keys, key)
		}
	})
	for _, key := range keys {
		loaded.RawSet(key, lua.LNil)
	}
}

func pluginEntrypoint(manifest pluginManifest) (string, error) {
	module := strings.TrimSpace(manifest.Module)
	if module == "" || strings.ContainsAny(module, `/\\`) || strings.Contains(module, "..") {
		return "", fmt.Errorf("invalid module %q", module)
	}
	base := filepath.Join(manifest.Root, "lua")
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		base = manifest.Root
	}
	relative := filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))
	for _, path := range []string{filepath.Join(base, relative+".lua"), filepath.Join(base, relative, "init.lua")} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("module %q has no entrypoint", module)
}

func newPluginState(runtime *Runtime) *pluginState {
	return &pluginState{
		runtime: runtime,
		active:  make(map[string]*pluginInstance),
	}
}

func newLuaPluginAPI(L *lua.LState, runtime *Runtime) *lua.LTable {
	api := L.NewTable()
	registerLuaFunctions(L, api, "onon.plugin.", []luaFunctionSpec{
		{
			Signature: "onon.plugin.register(id[, spec])",
			Function:  luaPluginRegister(runtime),
		},
	})
	return api
}

func (r *Runtime) registerPlugin(L *lua.LState, id string, spec *lua.LTable) (*lua.LTable, error) {
	id = normalizePluginID(id)
	if !validPluginID(id) {
		return nil, fmt.Errorf("invalid plugin id %q", id)
	}
	if r.loadingPlugin == "" || r.loadingPlugin != id {
		return nil, fmt.Errorf("plugin %q can only register while require(%q) is loading", id, id)
	}
	if r.plugins == nil {
		r.plugins = newPluginState(r)
	}
	if existing, ok := r.plugins.active[id]; ok {
		return existing.module, nil
	}
	if r.pluginCatalog != nil && r.pluginCatalog.disabled[id] {
		return nil, fmt.Errorf("plugin %q is disabled", id)
	}
	if r.pluginCatalog == nil {
		return nil, fmt.Errorf("plugin %q was not discovered", id)
	}
	manifest, ok := r.pluginCatalog.manifests[id]
	if !ok {
		return nil, fmt.Errorf("plugin %q was not discovered", id)
	}
	instance := &pluginInstance{
		runtime:       r,
		manifest:      manifest,
		actions:       make(map[string]pluginAction),
		commands:      make(map[string]pluginCommand),
		options:       make(map[string]*pluginOption),
		subscriptions: make(map[int]pluginSubscription),
	}
	module := L.NewTable()
	instance.module = module
	instance.actionsTable = L.NewTable()
	L.SetField(module, "id", lua.LString(id))
	L.SetField(module, "version", lua.LString(manifest.Version))
	L.SetField(module, "actions", instance.actionsTable)
	L.SetField(module, "fs", newLuaPluginFS(L, instance))
	L.SetField(module, "timer", newLuaPluginTimer(L, instance))
	L.SetField(module, "storage", newLuaPluginStorage(L, id))
	L.SetField(module, "http", newLuaPluginHTTP(L, instance))
	L.SetField(module, "document", newLuaPluginDocument(L, instance))
	if err := r.registerPluginOptions(L, instance, spec); err != nil {
		return nil, err
	}

	L.SetField(module, "register_action", L.NewFunction(func(L *lua.LState) int {
		return instance.registerAction(L)
	}))
	L.SetField(module, "register_command", L.NewFunction(func(L *lua.LState) int {
		return instance.registerCommand(L)
	}))
	L.SetField(module, "on", L.NewFunction(func(L *lua.LState) int {
		return instance.subscribe(L)
	}))
	L.SetField(module, "off", L.NewFunction(func(L *lua.LState) int {
		return instance.unsubscribe(L)
	}))
	L.SetField(module, "job", L.NewFunction(func(L *lua.LState) int {
		return instance.startJob(L)
	}))

	metatable := L.NewTable()
	L.SetField(metatable, "__index", L.NewFunction(func(L *lua.LState) int {
		name := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		if option, ok := instance.options[name]; ok {
			L.Push(option.value)
			return 1
		}
		L.Push(lua.LNil)
		return 1
	}))
	L.SetField(metatable, "__newindex", L.NewFunction(func(L *lua.LState) int {
		name := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		value := L.CheckAny(3)
		if option, ok := instance.options[name]; ok {
			if err := validatePluginOption(option.kind, value); err != nil {
				L.RaiseError("plugin %s.%s: %v", id, name, err)
			}
			option.value = value
			r.dirty = true
			return 0
		}
		module.RawSetString(name, value)
		return 0
	}))
	L.SetMetatable(module, metatable)
	r.plugins.active[id] = instance
	r.plugins.activationOrder = append(r.plugins.activationOrder, id)
	return module, nil
}

func (r *Runtime) registerPluginOptions(L *lua.LState, instance *pluginInstance, spec *lua.LTable) error {
	if spec == nil {
		return nil
	}
	value := spec.RawGetString("options")
	options, ok := value.(*lua.LTable)
	if !ok {
		if value != lua.LNil {
			return fmt.Errorf("options must be a table")
		}
		return nil
	}
	var optionErr error
	options.ForEach(func(key, value lua.LValue) {
		if optionErr != nil {
			return
		}
		nameValue, ok := key.(lua.LString)
		if !ok {
			optionErr = fmt.Errorf("option names must be strings")
			return
		}
		name := strings.ToLower(strings.TrimSpace(string(nameValue)))
		if !validPluginMemberName(name) {
			optionErr = fmt.Errorf("invalid option name %q", name)
			return
		}
		metadata, ok := value.(*lua.LTable)
		if !ok {
			optionErr = fmt.Errorf("option %s must be a table", name)
			return
		}
		kind := strings.ToLower(strings.TrimSpace(lua.LVAsString(metadata.RawGetString("type"))))
		defaultValue := metadata.RawGetString("default")
		if kind == "" {
			kind = inferPluginOptionKind(defaultValue)
		}
		if defaultValue == lua.LNil {
			defaultValue = pluginOptionDefault(L, kind)
		}
		if err := validatePluginOption(kind, defaultValue); err != nil {
			optionErr = fmt.Errorf("option %s: %v", name, err)
			return
		}
		instance.options[name] = &pluginOption{
			name:        name,
			fullName:    instance.manifest.ID + "." + name,
			kind:        kind,
			description: strings.TrimSpace(lua.LVAsString(metadata.RawGetString("description"))),
			value:       cloneLuaValue(L, defaultValue),
		}
	})
	return optionErr
}

func inferPluginOptionKind(value lua.LValue) string {
	switch value.Type() {
	case lua.LTBool:
		return "boolean"
	case lua.LTNumber:
		return "number"
	case lua.LTTable:
		return "list"
	default:
		return "string"
	}
}

func pluginOptionDefault(L *lua.LState, kind string) lua.LValue {
	switch kind {
	case "boolean", "bool":
		return lua.LFalse
	case "integer", "number", "float":
		return lua.LNumber(0)
	case "list", "table":
		return L.NewTable()
	default:
		return lua.LString("")
	}
}

func validatePluginOption(kind string, value lua.LValue) error {
	switch kind {
	case "boolean", "bool":
		if value.Type() != lua.LTBool {
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		if value.Type() != lua.LTNumber || float64(lua.LVAsNumber(value)) != float64(int(lua.LVAsNumber(value))) {
			return fmt.Errorf("expected integer")
		}
	case "number", "float":
		if value.Type() != lua.LTNumber {
			return fmt.Errorf("expected number")
		}
	case "string", "path", "command":
		if value.Type() != lua.LTString {
			return fmt.Errorf("expected string")
		}
	case "list", "table":
		if value.Type() != lua.LTTable {
			return fmt.Errorf("expected table")
		}
	default:
		return fmt.Errorf("unknown option type %q", kind)
	}
	return nil
}

func cloneLuaValue(L *lua.LState, value lua.LValue) lua.LValue {
	table, ok := value.(*lua.LTable)
	if !ok {
		return value
	}
	clone := L.NewTable()
	table.ForEach(func(key, value lua.LValue) {
		clone.RawSet(key, cloneLuaValue(L, value))
	})
	return clone
}

func (instance *pluginInstance) registerAction(L *lua.LState) int {
	instance.requireLoadingPlugin(L, "register_action")
	name := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
	metadata, fn, err := pluginFunctionArguments(L, 3)
	if err != nil {
		L.RaiseError("plugin %s action: %v", instance.manifest.ID, err)
	}
	if !validPluginMemberName(name) {
		L.RaiseError("plugin %s action: invalid name %q", instance.manifest.ID, name)
	}
	fullName := instance.manifest.ID + "." + name
	if instance.runtime.actionExists(fullName) {
		L.RaiseError("plugin %s action already exists", fullName)
	}
	description := fullName
	countable := false
	if metadata != nil {
		if value := strings.TrimSpace(lua.LVAsString(metadata.RawGetString("description"))); value != "" {
			description = value
		}
		countable = lua.LVAsBool(metadata.RawGetString("countable"))
	}
	token := newLuaActionValue(L, instance.runtime, fullName)
	instance.actions[name] = pluginAction{plugin: instance.manifest.ID, name: name, fullName: fullName, description: description, countable: countable, function: fn}
	instance.actionsTable.RawSetString(name, token)
	L.Push(token)
	return 1
}

func (instance *pluginInstance) registerCommand(L *lua.LState) int {
	instance.requireLoadingPlugin(L, "register_command")
	name := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
	metadata, fn, err := pluginFunctionArguments(L, 3)
	if err != nil {
		L.RaiseError("plugin %s command: %v", instance.manifest.ID, err)
	}
	// Plugin IDs may contain "-", so command names may not: that keeps the
	// last "-" of a full name the separator, and full names unique.
	if !validPluginMemberName(name) || strings.Contains(name, "-") {
		L.RaiseError("plugin %s command: invalid name %q", instance.manifest.ID, name)
	}
	fullName := instance.manifest.ID + "-" + name
	if instance.runtime.commandExists(fullName) {
		L.RaiseError("plugin %s command already exists", fullName)
	}
	description := ":" + fullName
	var completions []string
	if metadata != nil {
		if value := strings.TrimSpace(lua.LVAsString(metadata.RawGetString("description"))); value != "" {
			description = value
		}
		completions = luaTableStrings(metadata.RawGetString("arg_completions"))
	}
	instance.commands[name] = pluginCommand{plugin: instance.manifest.ID, name: name, fullName: fullName, description: description, argCompletions: completions, function: fn}
	L.Push(lua.LString(fullName))
	return 1
}

func pluginFunctionArguments(L *lua.LState, metadataIndex int) (*lua.LTable, *lua.LFunction, error) {
	metadata, _ := L.Get(metadataIndex).(*lua.LTable)
	functionIndex := metadataIndex + 1
	if _, ok := L.Get(metadataIndex).(*lua.LFunction); ok {
		metadata = nil
		functionIndex = metadataIndex
	}
	fn, ok := L.Get(functionIndex).(*lua.LFunction)
	if !ok {
		return nil, nil, fmt.Errorf("expected callback function")
	}
	return metadata, fn, nil
}

func (instance *pluginInstance) subscribe(L *lua.LState) int {
	instance.requireLoadingPlugin(L, "on")
	event := strings.ToLower(strings.TrimSpace(L.CheckString(2)))
	if !validPluginEvent(event) {
		L.RaiseError("plugin %s: unknown event %q", instance.manifest.ID, event)
	}
	fn, ok := L.Get(3).(*lua.LFunction)
	if !ok {
		L.RaiseError("plugin %s.on: expected callback function", instance.manifest.ID)
	}
	instance.runtime.plugins.nextSubscription++
	id := instance.runtime.plugins.nextSubscription
	instance.subscriptions[id] = pluginSubscription{plugin: instance.manifest.ID, event: event, fn: fn}
	L.Push(lua.LNumber(id))
	return 1
}

func (instance *pluginInstance) requireLoadingPlugin(L *lua.LState, method string) {
	if instance.runtime.loadingPlugin != instance.manifest.ID {
		L.RaiseError("plugin %s.%s can only be called while the plugin is loading", instance.manifest.ID, method)
	}
}

func (instance *pluginInstance) unsubscribe(L *lua.LState) int {
	id := int(lua.LVAsNumber(L.CheckAny(2)))
	delete(instance.subscriptions, id)
	return 0
}

func (instance *pluginInstance) startJob(L *lua.LState) int {
	spec, ok := L.Get(2).(*lua.LTable)
	if !ok {
		L.RaiseError("plugin %s.job: expected specification table", instance.manifest.ID)
	}
	callback, ok := L.Get(3).(*lua.LFunction)
	if !ok {
		L.RaiseError("plugin %s.job: expected callback function", instance.manifest.ID)
	}
	id, err := instance.runtime.startPluginJob(instance.manifest.ID, spec, callback)
	if err != nil {
		L.RaiseError("plugin %s.job: %v", instance.manifest.ID, err)
	}
	L.Push(newPluginOperationHandle(L, instance.runtime, id))
	return 1
}

func (r *Runtime) actionExists(name string) bool {
	return actions.IsBuiltin(name) || r.pluginAction(name) != nil
}

func (r *Runtime) isCountableAction(name string) bool {
	if actions.IsCountable(name) {
		return true
	}
	action := r.pluginAction(name)
	return action != nil && action.countable
}

// pluginMember finds the active plugin a member's full name belongs to, and
// the member's own name. Full names join the two with sep, which neither
// plugin IDs nor member names contain for actions and options ("."), and
// command names do not contain for commands ("-").
func (r *Runtime) pluginMember(name, sep string) (*pluginInstance, string) {
	if r == nil || r.plugins == nil {
		return nil, ""
	}
	name = strings.ToLower(strings.TrimSpace(name))
	i := strings.LastIndex(name, sep)
	if i < 0 {
		return nil, ""
	}
	return r.plugins.active[name[:i]], name[i+len(sep):]
}

func (r *Runtime) pluginAction(name string) *pluginAction {
	instance, member := r.pluginMember(name, ".")
	if instance == nil {
		return nil
	}
	action, ok := instance.actions[member]
	if !ok {
		return nil
	}
	return &action
}

func (r *Runtime) pluginCommand(name string) *pluginCommand {
	instance, member := r.pluginMember(name, "-")
	if instance == nil {
		return nil
	}
	command, ok := instance.commands[member]
	if !ok {
		return nil
	}
	return &command
}

func (r *Runtime) actionNames() []string {
	names := actions.Names()
	if r == nil || r.plugins == nil {
		return names
	}
	for _, instance := range r.plugins.active {
		for _, action := range instance.actions {
			names = append(names, action.fullName)
		}
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) executeAction(action string) error {
	if r == nil || r.host == nil {
		return fmt.Errorf("%s: cannot execute during config load; pass it to bind(...) or call it from a callback", action)
	}
	if pluginAction := r.pluginAction(action); pluginAction != nil {
		return r.callPluginLua(pluginAction.plugin, lua.P{Fn: pluginAction.function, NRet: 0, Protect: true})
	}
	return r.host.ExecuteAction(action)
}

func (r *Runtime) runPluginCommand(name, args string) (bool, error) {
	command := r.pluginCommand(name)
	if command == nil {
		return false, nil
	}
	context := luaCommandContext(r.state, command.fullName, args)
	return true, r.callPluginLua(command.plugin, lua.P{Fn: command.function, NRet: 0, Protect: true}, context)
}

func luaCommandContext(L *lua.LState, name, raw string) *lua.LTable {
	context := L.NewTable()
	L.SetField(context, "name", lua.LString(name))
	L.SetField(context, "raw", lua.LString(raw))
	L.SetField(context, "args", luaStringsTable(L, strings.Fields(raw)))
	return context
}

func (r *Runtime) commandNames() []string {
	names := []string{}
	if r == nil || r.plugins == nil {
		return names
	}
	for _, instance := range r.plugins.active {
		for _, command := range instance.commands {
			names = append(names, command.fullName)
		}
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) commandCompletions(name, prefix string) []string {
	command := r.pluginCommand(name)
	if command == nil {
		return nil
	}
	values := []string{}
	for _, value := range command.argCompletions {
		if strings.HasPrefix(value, prefix) {
			values = append(values, value)
		}
	}
	return values
}

func (r *Runtime) commandHelpRows() []string {
	rows := []string{}
	for _, name := range r.commandNames() {
		rows = append(rows, r.pluginCommand(name).description)
	}
	return rows
}

func (r *Runtime) optionNames() []string {
	names := OptionNames()
	if r == nil || r.plugins == nil {
		return names
	}
	for _, instance := range r.plugins.active {
		for _, option := range instance.options {
			names = append(names, option.fullName)
		}
	}
	sort.Strings(names)
	return names
}

func (r *Runtime) pluginOption(name string) (*pluginOption, bool) {
	instance, member := r.pluginMember(name, ".")
	if instance == nil {
		return nil, false
	}
	option, ok := instance.options[member]
	return option, ok
}

func (r *Runtime) pluginOptionValue(name string) (string, error) {
	option, ok := r.pluginOption(name)
	if !ok {
		return "", fmt.Errorf("unknown option: %s", name)
	}
	return formatPluginOption(option), nil
}

func formatPluginOption(option *pluginOption) string {
	switch option.kind {
	case "boolean", "bool":
		return strconv.FormatBool(lua.LVAsBool(option.value))
	case "integer":
		return strconv.Itoa(int(lua.LVAsNumber(option.value)))
	case "number", "float":
		return strconv.FormatFloat(float64(lua.LVAsNumber(option.value)), 'g', -1, 64)
	case "list", "table":
		return option.value.String()
	default:
		return strconv.Quote(lua.LVAsString(option.value))
	}
}

func (r *Runtime) setPluginOption(name string, raw string) error {
	option, ok := r.pluginOption(name)
	if !ok {
		return fmt.Errorf("unknown option: %s", name)
	}
	var value lua.LValue
	switch option.kind {
	case "boolean", "bool":
		parsed, err := parseBoolOption(raw)
		if err != nil {
			return err
		}
		value = lua.LBool(parsed)
	case "integer":
		parsed, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return fmt.Errorf("expected integer")
		}
		value = lua.LNumber(parsed)
	case "number", "float":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return fmt.Errorf("expected number")
		}
		value = lua.LNumber(parsed)
	case "list", "table":
		return fmt.Errorf("expected Lua table")
	default:
		parsed, err := parseStringOption(raw)
		if err != nil {
			return err
		}
		value = lua.LString(parsed)
	}
	if err := validatePluginOption(option.kind, value); err != nil {
		return err
	}
	option.value = value
	r.dirty = true
	return nil
}

func (r *Runtime) setPluginOptionValue(name string, value lua.LValue) error {
	option, ok := r.pluginOption(name)
	if !ok {
		return fmt.Errorf("unknown option: %s", name)
	}
	if err := validatePluginOption(option.kind, value); err != nil {
		return err
	}
	option.value = value
	r.dirty = true
	return nil
}

// PluginEvents are the events plugins can subscribe to, which the viewer
// emits; a viewer test keeps the two in step.
var PluginEvents = []string{
	"app_ready",
	"document_open_pre", "document_opened",
	"document_close_pre", "document_closed",
	"document_reloaded", "config_reloaded",
	"mouse_button_pre", "mouse_button",
	"selection_changed", "page_changed", "zoom_changed", "option_changed",
	"tab_changed",
	"shutdown",
}

func validPluginEvent(event string) bool { return slices.Contains(PluginEvents, event) }

func (r *Runtime) emitPluginEvent(event string, payload map[string]any) bool {
	if r == nil || r.state == nil || r.plugins == nil {
		return false
	}
	consumed := false
	err := r.doLua(func() error {
		consumed = r.emitPluginEventCallbacks(event, payload)
		return nil
	})
	if err != nil {
		r.logf("plugin event %s: %v", event, err)
	}
	return consumed
}

func (r *Runtime) emitPluginEventCallbacks(event string, payload map[string]any) bool {
	ids := append([]string(nil), r.plugins.activationOrder...)
	consumed := false
	for _, id := range ids {
		instance := r.plugins.active[id]
		subscriptionIDs := make([]int, 0, len(instance.subscriptions))
		for subscriptionID := range instance.subscriptions {
			subscriptionIDs = append(subscriptionIDs, subscriptionID)
		}
		sort.Ints(subscriptionIDs)
		for _, subscriptionID := range subscriptionIDs {
			subscription, ok := instance.subscriptions[subscriptionID]
			if !ok || subscription.event != event {
				continue
			}
			top := r.state.GetTop()
			err := r.callPluginLua(id, lua.P{Fn: subscription.fn, NRet: 1, Protect: true}, luaTableFromMap(r.state, payload))
			if err != nil {
				r.deferred = nil
				r.logf("plugin %s event %s: %v", id, event, err)
			}
			if r.state.GetTop() > top {
				result := r.state.Get(-1)
				r.state.SetTop(top)
				if result.Type() == lua.LTBool && lua.LVAsBool(result) {
					consumed = true
				}
			}
		}
	}
	return consumed
}

func luaTableFromMap(L *lua.LState, values map[string]any) *lua.LTable {
	table := L.NewTable()
	for key, value := range values {
		L.SetField(table, key, luaValueFromAny(L, value))
	}
	return table
}

func luaValueFromAny(L *lua.LState, value any) lua.LValue {
	switch value := value.(type) {
	case nil:
		return lua.LNil
	case string:
		return lua.LString(value)
	case bool:
		return lua.LBool(value)
	case int:
		return lua.LNumber(value)
	case int32:
		return lua.LNumber(value)
	case int64:
		return lua.LNumber(value)
	case float32:
		return lua.LNumber(value)
	case float64:
		return lua.LNumber(value)
	case []string:
		return luaStringsTable(L, value)
	case []int:
		table := L.NewTable()
		for i, item := range value {
			table.RawSetInt(i+1, lua.LNumber(item))
		}
		return table
	case map[string]any:
		return luaTableFromMap(L, value)
	default:
		return luaReflectValue(L, reflect.ValueOf(value))
	}
}

func luaReflectValue(L *lua.LState, value reflect.Value) lua.LValue {
	if !value.IsValid() {
		return lua.LNil
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		table := L.NewTable()
		for i := 0; i < value.Len(); i++ {
			table.RawSetInt(i+1, luaValueFromAny(L, value.Index(i).Interface()))
		}
		return table
	}
	return lua.LString(fmt.Sprint(value.Interface()))
}

func (r *Runtime) startPluginJob(pluginID string, spec *lua.LTable, callback *lua.LFunction) (int, error) {
	if r == nil || r.state == nil {
		return 0, fmt.Errorf("Lua runtime unavailable")
	}
	command := strings.TrimSpace(lua.LVAsString(spec.RawGetString("command")))
	if command == "" {
		return 0, fmt.Errorf("command is required")
	}
	args := luaTableStrings(spec.RawGetString("args"))
	cwd := lua.LVAsString(spec.RawGetString("cwd"))
	timeoutMS := int(lua.LVAsNumber(spec.RawGetString("timeout_ms")))
	stdin := lua.LVAsString(spec.RawGetString("stdin"))
	env := map[string]string{}
	if envTable, ok := spec.RawGetString("env").(*lua.LTable); ok {
		var envErr error
		envTable.ForEach(func(key, value lua.LValue) {
			if key.Type() != lua.LTString || value.Type() != lua.LTString {
				envErr = fmt.Errorf("env must contain string keys and values")
				return
			}
			env[lua.LVAsString(key)] = lua.LVAsString(value)
		})
		if envErr != nil {
			return 0, envErr
		}
	}

	timeout := time.Duration(timeoutMS) * time.Millisecond
	return r.startPluginOperation(pluginID, "job", callback, func(ctx context.Context) map[string]any {
		return runPluginJob(ctx, timeout, command, args, cwd, env, stdin)
	}), nil
}

func runPluginJob(ctx context.Context, timeout time.Duration, command string, args []string, cwd string, env map[string]string, stdin string) map[string]any {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout := &limitedBuffer{limit: 4 << 20}
	stderr := &limitedBuffer{limit: 4 << 20}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	errText := ""
	if err != nil && !timedOut {
		errText = err.Error()
	}
	return map[string]any{
		"code":      code,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"error":     errText,
		"timed_out": timedOut,
		"success":   errText == "" && !timedOut && code == 0,
	}
}

func normalizePluginID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func validPluginID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\\`) {
		return false
	}
	return validPluginMemberName(id)
}

func validPluginMemberName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\.`) {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func (r *Runtime) commandExists(name string) bool {
	return slices.Contains(commands.Names(), strings.ToLower(name)) || r.pluginCommand(name) != nil
}

func (r *Runtime) ActionNames() []string { return r.actionNames() }

func (r *Runtime) IsCountableAction(name string) bool { return r.isCountableAction(name) }

func (r *Runtime) CommandNames() []string { return r.commandNames() }

func (r *Runtime) CommandCompletions(name, prefix string) []string {
	return r.commandCompletions(name, prefix)
}

func (r *Runtime) CommandHelpRows() []string { return r.commandHelpRows() }

func (r *Runtime) OptionNames() []string { return r.optionNames() }

func (r *Runtime) RunPluginCommand(name, args string) (bool, error) {
	return r.runPluginCommand(name, args)
}

func (r *Runtime) EmitPluginEvent(event string, payload map[string]any) bool {
	return r.emitPluginEvent(event, payload)
}
