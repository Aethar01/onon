package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"onon/internal/remote"

	lua "github.com/yuin/gopher-lua"
)

func Load(explicitPath string) (Config, error) {
	rt, err := Open(explicitPath, "")
	if err != nil {
		return Config{}, err
	}
	defer rt.Close()
	return rt.Config(), nil
}

func Open(explicitPath, docPath string, verbose ...bool) (*Runtime, error) {
	options := OpenOptions{}
	if len(verbose) > 0 {
		options.Verbose = verbose[0]
	}
	return OpenWithOptions(explicitPath, docPath, options)
}

type OpenOptions struct {
	Verbose bool
	// PluginPaths are searched ahead of the platform directories.
	PluginPaths []string
	// DisabledPlugins are discovered but refuse to load.
	DisabledPlugins []string
	// NoPlugins skips plugin discovery entirely.
	NoPlugins bool
	// NoConfig starts from built-in defaults, loading neither the user's
	// configuration nor the generated settings file.
	NoConfig bool
}

func OpenWithOptions(explicitPath, docPath string, options OpenOptions) (*Runtime, error) {
	docPath = AbsoluteDocumentPath(docPath)
	rt := &Runtime{
		explicitPath:     explicitPath,
		docPath:          docPath,
		docName:          DocumentName(docPath),
		docMeta:          loadDocumentMeta(docPath),
		verbose:          options.Verbose,
		operationResults: make(chan pluginOperationResult, 64),
	}
	var pluginPaths []string
	if !options.NoPlugins {
		pluginPaths = append(pluginPaths, options.PluginPaths...)
		pluginPaths = append(pluginPaths, PluginPaths()...)
	}
	rt.pluginPaths = unique(pluginPaths)
	rt.disabledPlugins = append([]string(nil), options.DisabledPlugins...)
	rt.noConfig = options.NoConfig
	if err := rt.Reload(); err != nil {
		return nil, err
	}
	for _, note := range migrationNotes {
		rt.warnings = append(rt.warnings, note) // logged when made
	}
	migrationNotes = nil
	return rt, nil
}

func candidatePaths(explicitPath string) []string {
	if explicitPath != "" {
		return []string{explicitPath}
	}
	return unique(platformConfigPaths())
}

func unique(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func (r *Runtime) Close() {
	r.cancelPluginOperations()
	r.closeLuaState()
}

func (r *Runtime) Config() Config {
	return r.cfg
}

// ConsumeDirty reports whether the config changed since the last call, and
// which built-in options were assigned meanwhile, including ones assigned
// the value they already had.
func (r *Runtime) ConsumeDirty() (bool, map[string]bool) {
	if r == nil {
		return false, nil
	}
	dirty, assigned := r.dirty, r.assigned
	r.dirty, r.assigned = false, nil
	return dirty, assigned
}

func (r *Runtime) markAssigned(name string) {
	if r.assigned == nil {
		r.assigned = map[string]bool{}
	}
	r.assigned[name] = true
}

func (r *Runtime) AttachHost(host Host) {
	r.host = host
}

// SetWake sets the function that wakes the viewer when a plugin operation
// finishes, so it can call PollPluginOperations then rather than on a timer.
// It must be safe to call from any goroutine.
func (r *Runtime) SetWake(wake func()) {
	r.wake = wake
}

func (r *Runtime) SetDocument(path string, pageCount ...int) error {
	path = AbsoluteDocumentPath(path)
	r.docPath = path
	r.docName = DocumentName(path)
	r.docMeta = loadDocumentMeta(path)
	if len(pageCount) > 0 {
		r.docMeta.pageCount = pageCount[0]
		r.docMeta.hasPages = true
	}
	if err := r.Reload(); err != nil {
		r.updateLuaDocument()
		return err
	}
	return nil
}

func (r *Runtime) SetPageCount(pages int) {
	r.docMeta.pageCount = pages
	r.docMeta.hasPages = true
	r.updateLuaDocument()
}

func (r *Runtime) Reload() error {
	r.logf("reload config explicit=%q doc=%q", r.explicitPath, r.docPath)
	old := r.luaGeneration
	committed := false
	r.luaGeneration = luaGeneration{
		cfg:              Default(),
		callbacks:        map[string]*lua.LFunction{},
		schemes:          map[string]*lua.LFunction{},
		pluginCatalog:    discoverPluginCatalog(r.pluginPaths, r.disabledPlugins),
		operations:       map[int]*pluginOperation{},
		pluginGeneration: old.pluginGeneration,
	}
	for _, warning := range r.pluginCatalog.warnings {
		r.logf("%s", warning)
	}
	defer func() {
		if committed {
			r.assigned = nil // loading the config is not a change to it
			cancelPluginOperationMap(old.operations)
			if old.state != nil {
				old.state.Close()
			}
			return
		}
		r.Close()
		r.luaGeneration = old
	}()
	// --no-config means built-in defaults only: neither the user's file nor the
	// generated one is read, and nothing is written back.
	if r.noConfig {
		r.initLuaState()
		r.dirty = false
		r.logf("configuration disabled")
		committed = true
		return nil
	}
	r.applyStoredKeyBindings()
	path, err := r.findConfig()
	if err != nil {
		return err
	}
	r.cfg.ConfigPath = path // the themes directory is beside it
	r.applyChosenTheme()
	if path != "" {
		r.logf("apply config %q", path)
		r.loadingConfig = true
		err := r.applyLuaConfig(path)
		r.loadingConfig = false
		if err != nil {
			return err
		}
		r.dirty = false
		committed = true
		return nil
	}
	if r.state == nil { // a theme file chosen with :theme may have started it
		r.initLuaState()
	}
	r.dirty = false
	r.logf("no user config loaded")
	committed = true
	return nil
}

// findConfig returns the first config file there is, or "" for none.
func (r *Runtime) findConfig() (string, error) {
	for _, path := range candidatePaths(r.explicitPath) {
		r.logf("check config %q", path)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if !info.IsDir() {
			return path, nil
		}
	}
	return "", nil
}

// newConfigFile is what a config file onon creates for editing holds.
const newConfigFile = `-- onon configuration. Set only what you want to change; every option,
-- theme field and default binding is in the reference:
-- https://ononreader.com/
-- :reload-config applies changes without restarting.

`

// EditableConfigPath returns the config file to edit: the one loaded, else
// the one given with --config, else the first place onon looks for one,
// where it creates one.
func (r *Runtime) EditableConfigPath() (string, error) {
	if r.cfg.ConfigPath != "" {
		return r.cfg.ConfigPath, nil
	}
	path := r.explicitPath
	if path == "" {
		candidates := candidatePaths("")
		if len(candidates) == 0 {
			return "", fmt.Errorf("no place for a config file")
		}
		path = candidates[0]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	defer file.Close()
	_, err = file.WriteString(newConfigFile)
	return path, err
}

func (r *Runtime) Generation() int {
	if r == nil {
		return 0
	}
	return r.pluginGeneration
}

func (r *Runtime) closeLuaState() {
	if r.state != nil {
		r.state.Close()
		r.state = nil
	}
	r.plugins = nil
}

func (r *Runtime) logf(format string, args ...any) {
	if r != nil && r.verbose {
		log.Printf(format, args...)
	}
}

func (r *Runtime) RunAction(action string) (bool, bool, error) {
	if r == nil {
		return false, false, nil
	}
	fn, ok := r.callbacks[action]
	if !ok {
		if r.pluginAction(action) == nil {
			return false, false, nil
		}
		r.dirty = false
		err := r.runPluginAction(action)
		return true, r.dirty, err
	}
	r.dirty = false
	if err := r.callLua(lua.P{Fn: fn, NRet: 0, Protect: true}); err != nil {
		return true, r.dirty, err
	}
	return true, r.dirty, nil
}

func (r *Runtime) runPluginAction(action string) error {
	pluginAction := r.pluginAction(action)
	if pluginAction == nil {
		return fmt.Errorf("unknown action: %s", action)
	}
	return r.callPluginLua(pluginAction.plugin, lua.P{Fn: pluginAction.function, NRet: 0, Protect: true})
}

func (r *Runtime) Eval(code string) (bool, error) {
	if r == nil || r.state == nil {
		return false, fmt.Errorf("no Lua state")
	}
	r.dirty = false
	err := r.doLua(func() error { return r.state.DoString(code) })
	if err != nil {
		return r.dirty, err
	}
	return r.dirty, nil
}

// RunUIRow runs a view's row callback, on_select or on_selection_changed,
// as fn(index, value, text, id, row), row being the row the plugin gave.
func (r *Runtime) RunUIRow(callback string, row UIRowRef) error {
	lv := row.Lua
	if lv == nil {
		lv = lua.LNil
	}
	return r.runCallback(callback, lua.LNumber(row.Index), lua.LString(row.Value), lua.LString(row.Text), lua.LString(row.ID), lv)
}

func (r *Runtime) RunUIQuery(callback, query string) error {
	return r.runCallback(callback, lua.LString(query))
}

// RunUIKey runs a view's on_key with a key's name, as bind takes it,
// reporting whether the callback took the key.
func (r *Runtime) RunUIKey(callback, key string) (bool, error) {
	return r.runCallbackHandled(callback, lua.LString(key))
}

// RunUIAction runs a view's on_action with the action bound to a key,
// reporting whether the callback took the action.
func (r *Runtime) RunUIAction(callback, action string) (bool, error) {
	return r.runCallbackHandled(callback, lua.LString(action))
}

// RunUIMouse runs a view's on_mouse, reporting whether the callback took
// the event.
func (r *Runtime) RunUIMouse(callback string, e UIMouseEvent) (bool, error) {
	if r == nil || r.state == nil {
		return false, nil
	}
	tbl := r.state.NewTable()
	tbl.RawSetString("type", lua.LString(e.Type))
	if e.Button != "" {
		tbl.RawSetString("button", lua.LString(e.Button))
	}
	tbl.RawSetString("x", lua.LNumber(e.X))
	tbl.RawSetString("y", lua.LNumber(e.Y))
	if e.Clicks > 0 {
		tbl.RawSetString("clicks", lua.LNumber(e.Clicks))
	}
	if e.Type == "wheel" {
		tbl.RawSetString("dx", lua.LNumber(e.DX))
		tbl.RawSetString("dy", lua.LNumber(e.DY))
	}
	return r.runCallbackHandled(callback, tbl)
}

// RunUIDraw runs a view's draw function as draw(canvas, box, state).
func (r *Runtime) RunUIDraw(callback string, canvas Canvas, state DrawState) error {
	fn, ok := r.callbacks[callback]
	if !ok {
		return fmt.Errorf("unknown lua callback: %s", callback)
	}
	return r.RunDraw(fn, canvas, state)
}

func (r *Runtime) RunUIClose(callback string) error {
	return r.runCallback(callback)
}

func (r *Runtime) runCallback(callback string, args ...lua.LValue) error {
	if r == nil || callback == "" {
		return nil
	}
	fn, ok := r.callbacks[callback]
	if !ok {
		return fmt.Errorf("unknown lua callback: %s", callback)
	}
	params := lua.P{Fn: fn, NRet: 0, Protect: true}
	return r.callLua(params, args...)
}

// runCallbackHandled runs a callback that reports, by returning true,
// whether it handled what it was given.
func (r *Runtime) runCallbackHandled(callback string, args ...lua.LValue) (bool, error) {
	if r == nil || callback == "" {
		return false, nil
	}
	fn, ok := r.callbacks[callback]
	if !ok {
		return false, fmt.Errorf("unknown lua callback: %s", callback)
	}
	handled := false
	err := r.doLua(func() error {
		if err := r.state.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, args...); err != nil {
			return err
		}
		handled = lua.LVAsBool(r.state.Get(-1))
		r.state.Pop(1)
		return nil
	})
	return handled, err
}

func (r *Runtime) callLua(params lua.P, args ...lua.LValue) (err error) {
	return r.doLua(func() error {
		return r.state.CallByParam(params, args...)
	})
}

func (r *Runtime) doLua(fn func() error) (err error) {
	r.luaCallDepth++
	panicked := true
	defer func() {
		r.luaCallDepth--
		if r.luaCallDepth != 0 {
			return
		}
		if panicked || err != nil {
			r.deferredOpen = ""
			return
		}
		err = r.flushDeferredOpen()
	}()
	err = fn()
	panicked = false
	return err
}

func (r *Runtime) open(path string) error {
	if r.host == nil {
		return fmt.Errorf("viewer host unavailable")
	}
	if r.luaCallDepth > 0 {
		r.deferredOpen = path
		return nil
	}
	return r.host.Open(path)
}

func (r *Runtime) flushDeferredOpen() error {
	if r.deferredOpen == "" {
		return nil
	}
	path := r.deferredOpen
	r.deferredOpen = ""
	if r.host == nil {
		return fmt.Errorf("open: viewer host unavailable")
	}
	if err := r.host.Open(path); err != nil {
		return fmt.Errorf("open: %w", err)
	}
	return nil
}

func loadDocumentMeta(docPath string) documentMeta {
	meta := documentMeta{ext: strings.ToLower(filepath.Ext(DocumentName(docPath)))}
	if docPath == "" || remote.IsURI(docPath) {
		return meta
	}
	info, err := os.Stat(docPath)
	if err == nil && !info.IsDir() {
		meta.exists = true
		meta.sizeBytes = info.Size()
	}
	return meta
}
