package viewer

import (
	"fmt"
	"log"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"onon/internal/config"
	"onon/internal/filepicker"
	"onon/internal/keys"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func (a *App) pushToken(token string) {
	a.sequence = append(a.sequence, token)
	a.sequenceAt = time.Now()
	a.wakeAfter(time.Duration(a.config.SequenceTimeoutMS) * time.Millisecond)
	for len(a.sequence) > 0 {
		joined := strings.Join(a.sequence, "")
		cmd, exact := a.sequenceLookup[joined]
		prefix := a.hasPrefix(joined)
		if exact && !prefix {
			a.sequence = nil
			a.runAction(cmd)
			return
		}
		if exact && prefix {
			return
		}
		if prefix {
			return
		}
		if len(a.sequence) == 1 {
			a.sequence = nil
			return
		}
		a.sequence = a.sequence[1:]
	}
}

func (a *App) expireSequence() {
	if len(a.sequence) == 0 {
		return
	}
	if time.Since(a.sequenceAt) < time.Duration(a.config.SequenceTimeoutMS)*time.Millisecond {
		return
	}
	joined := strings.Join(a.sequence, "")
	if cmd, ok := a.sequenceLookup[joined]; ok {
		a.sequence = nil
		a.runAction(cmd)
		return
	}
	a.sequence = nil
}

// setKeyBindings indexes key bindings, keyed by canonical sequence, for
// dispatch.
func (a *App) setKeyBindings(bindings map[string]string) {
	a.sequenceLookup = maps.Clone(bindings)
	a.sequencePrefixes = map[string]struct{}{}
	for binding := range bindings {
		// The configuration stores only sequences that parse.
		seq, _ := keys.ParseSequence(binding)
		for n := 1; n < len(seq); n++ {
			a.sequencePrefixes[keys.Format(seq[:n])] = struct{}{}
		}
	}
	a.updateMenuBar()
}

// hasPrefix reports whether a longer bound sequence starts with joined.
func (a *App) hasPrefix(joined string) bool {
	_, ok := a.sequencePrefixes[joined]
	return ok
}

func (a *App) runAction(action string) {
	// The overview and presentation modes reinterpret actions only while
	// they have the keyboard, not while a prompt or menu opened over them.
	inFront := a.mode == modeNormal && a.activeModalUIView() == nil
	if inFront && a.overview != nil && a.runOverviewAction(action) {
		return
	}
	if inFront && a.presentation != nil && a.runPresentationAction(action) {
		return
	}
	if handled, dirty, err := a.runtime.RunAction(action); handled {
		if err != nil {
			a.message = err.Error()
			return
		}
		if dirty {
			a.applyRuntimeChanges("action")
		}
		return
	}
	if err := a.runBuiltinAction(action); err != nil {
		a.message = err.Error()
	}
}

func (a *App) runActionFrom(source smoothInputSource, action string) {
	previous := a.inputSource
	a.inputSource = source
	defer func() { a.inputSource = previous }()
	a.runAction(action)
}

func (a *App) closeAllUI() {
	a.closeAllUIWithCallbacks(true)
}

func (a *App) closeAllUIWithCallbacks(callCallbacks bool) {
	a.closeAllUIViews(callCallbacks)
	if a.mode != modeNormal {
		a.closeCompletion()
		if a.mode == modePassword {
			a.passwordPrompt = pendingPasswordPrompt{}
		}
		if a.mode == modePrompt {
			a.cancelPrompt()
		}
		a.mode = modeNormal
		a.input.Reset()
		a.ignoreText = ""
	}
}

func (a *App) closeActiveUI() {
	if view := a.activeModalUIView(); view != nil {
		a.closeUIView(view, true)
		return
	}
	if a.mode != modeNormal {
		if a.completion.view != nil && a.completion.view.visible {
			a.closeCompletion()
			return
		}
		if a.mode == modePassword {
			a.passwordPrompt = pendingPasswordPrompt{}
		}
		if a.mode == modePrompt {
			a.cancelPrompt()
		}
		a.mode = modeNormal
		a.input.Reset()
		a.ignoreText = ""
		return
	}
	if a.cancelRemoteTask() {
		a.message = "cancelled"
		return
	}
	if a.search.query != "" || len(a.search.order) > 0 || a.search.running {
		a.clearSearch()
		return
	}
	if !a.selection.empty() {
		a.clearSelection()
		return
	}
	a.sequence = nil
	a.pendingCount = ""
}

func (a *App) ExecuteAction(action string) error { return a.runBuiltinAction(action) }

func (a *App) PickDirectory() (string, error) {
	return filepicker.PickDirectory()
}

func (a *App) Page() int { return a.page + 1 }

func (a *App) PageCount() int { return a.pageCount }

func (a *App) GotoPage(page int) error {
	if a.pageCount == 0 {
		return nil
	}
	a.alignPageToAnchor(clampInt(page-1, 0, a.pageCount-1))
	return nil
}

func (a *App) GotoDocumentPoint(page int, x, y float64) error {
	if a.pageCount == 0 {
		return nil
	}
	if page < 1 || page > a.pageCount {
		return fmt.Errorf("page %d out of range", page)
	}
	a.alignPageToDocumentPoint(page-1, x, y)
	a.pendingRedraw = true
	return nil
}

func (a *App) Message() string { return a.message }

func (a *App) SetMessage(message string) { a.message = message }

func (a *App) RunCommand(command string) error {
	a.runCommand(command)
	return nil
}

// applyConfigState applies cfg in full, view settings included, as at
// startup and when a document opens.
func (a *App) applyConfigState(cfg config.Config) {
	a.config = cfg
	remote.SetHTTPHeaders(cfg.HTTPHeaders)
	a.document.setRemotePolling(cfg.AutoReload)
	a.cancelSmoothZoom()
	a.cancelSmoothScroll()
	a.fitMode = parseFitMode(cfg.FitMode)
	a.renderMode = parseRenderMode(cfg.RenderMode)
	a.altColors = a.wantAltColors(cfg)
	a.dualPage = cfg.DualPage
	a.trimMargins = cfg.TrimMargins
	a.firstPageOffset = cfg.FirstPageOffset
	a.statusBarShown = cfg.StatusBarVisible
	a.applyConfigSettings()
	a.loadUIFont()
}

// applyConfig applies a changed config to the running viewer, only as far as
// it changed. The view settings it holds, such as fit_mode and dual_page, are
// initial values, so one changed since by an action stays unless the config
// changes it or the option is assigned (named in assigned); the UI font and
// rendered tiles are kept unless their own settings change.
func (a *App) applyConfig(cfg config.Config, assigned map[string]bool) {
	prev := a.config
	a.config = cfg
	a.applyConfigSettings()
	// Shapes drawn by Lua may draw differently after any change.
	a.masks.clear()
	a.themeErrors = nil
	if prev.Theme.Font != cfg.Theme.Font {
		a.loadUIFont()
	}
	if prev.AltColors != cfg.AltColors || prev.AltColorsSystem != cfg.AltColorsSystem || assigned["alt_colors"] {
		a.setAltColors(a.wantAltColors(cfg))
	}
	if prev.TrimMargins != cfg.TrimMargins || assigned["trim_margins"] {
		a.setTrimMargins(cfg.TrimMargins)
	} else if prev.TrimPadding != cfg.TrimPadding && a.trimMargins {
		a.retrimPages()
	}
	if a.tilesRenderedDifferently(prev, cfg) {
		a.restyleTiles()
	}
	a.showConfigWarnings()
	a.relayoutWithViewportAnchor(func() {
		changed := false
		apply := func(differs bool, option string, set func()) {
			if differs || assigned[option] {
				set()
				changed = true
			}
		}
		apply(prev.FitMode != cfg.FitMode, "fit_mode", func() { a.fitMode = parseFitMode(cfg.FitMode) })
		apply(prev.RenderMode != cfg.RenderMode, "render_mode", func() { a.renderMode = parseRenderMode(cfg.RenderMode) })
		apply(prev.DualPage != cfg.DualPage, "dual_page", func() { a.dualPage = cfg.DualPage })
		apply(prev.FirstPageOffset != cfg.FirstPageOffset, "first_page_offset", func() { a.firstPageOffset = cfg.FirstPageOffset })
		apply(prev.StatusBarVisible != cfg.StatusBarVisible, "status_bar_visible", func() { a.statusBarShown = cfg.StatusBarVisible })
		if changed {
			a.cancelSmoothZoom()
			a.cancelSmoothScroll()
		}
	})
}

// wantAltColors is whether cfg asks for alternate colors: as it says, or
// with alt_colors = "system" while the OS is in dark mode. The OS is only
// asked once SDL is running.
// toggleOption flips the boolean option name. alt_colors following the OS
// flips from what is shown, and so stops following it.
func (a *App) toggleOption(name string) error {
	if strings.EqualFold(strings.TrimSpace(name), "alt_colors") && a.config.AltColorsSystem {
		return a.runtime.SetOption("alt_colors", strconv.FormatBool(!a.altColors))
	}
	return a.runtime.ToggleOption(name)
}

func (a *App) wantAltColors(cfg config.Config) bool {
	if !cfg.AltColorsSystem {
		return cfg.AltColors
	}
	return a.renderer != nil && sdl.GetSystemTheme() == sdl.SystemThemeDark
}

// followSystemColors switches alternate colors with the OS's dark mode,
// when the config asks for that.
func (a *App) followSystemColors() {
	if a.config.AltColorsSystem {
		a.setAltColors(a.wantAltColors(a.config))
	}
}

// showConfigWarnings shows in the status bar what the configuration was
// applied in spite of, such as theme fields this version does not know;
// they are also logged.
func (a *App) showConfigWarnings() {
	if warnings := a.runtime.TakeWarnings(); len(warnings) > 0 {
		a.message = strings.Join(warnings, "; ")
		a.pendingRedraw = true
	}
}

// applyConfigSettings applies the settings that are cheap to apply whether
// or not they changed.
func (a *App) applyConfigSettings() {
	cfg := a.config
	a.styles = nil
	a.zoom = a.clampZoom(a.zoom)
	a.cache.byteLimit = pageCacheByteLimit(cfg)
	a.cache.evict()
	a.setKeyBindings(cfg.KeyBindings)
	a.mouseBindings = maps.Clone(cfg.MouseBindings)
	a.pageStep = float64(cfg.ScrollStep)
	a.document.setDelay(time.Duration(cfg.AutoReloadDelayMS) * time.Millisecond)
}

// loadUIFont loads the theme's UI font at the window's display scale.
func (a *App) loadUIFont() {
	a.clearTextTextureCache()
	a.masks.clear()
	a.closeUIFonts()
	a.uiScale = a.displayScale()
	font := a.config.Theme.Font
	var warning error
	a.fontFace, a.headingFace, warning = loadUIFonts(font, int(math.Round(float64(font.Size)*a.uiScale)))
	a.logf("load UI font family=%q path=%q size=%d scale=%.2f", font.Family, font.Path, font.Size, a.uiScale)
	if warning != nil && warning.Error() != a.fontWarning {
		log.Printf("UI font: %v", warning)
		a.fontWarning = warning.Error()
	}
}

// tilesRenderedDifferently reports whether tiles rendered under one config
// would come out differently under the other; the alternate colours only
// matter while they are shown.
func (a *App) tilesRenderedDifferently(prev, cfg config.Config) bool {
	return prev.AntiAliasing != cfg.AntiAliasing || a.altColors && (prev.Theme.Alt.Page != cfg.Theme.Alt.Page ||
		prev.Theme.Alt.Foreground != cfg.Theme.Alt.Foreground || prev.AltColorsKeepImages != cfg.AltColorsKeepImages)
}

func (a *App) Mode() string {
	switch a.mode {
	case modeCommand:
		return "command"
	case modeGotoPage:
		return "goto"
	case modeSearch:
		return "search"
	default:
		return "normal"
	}
}

func (a *App) Search(query string, backward bool) error {
	mode := searchModeForward
	if backward {
		mode = searchModeBackward
	}
	a.startSearch(query, mode)
	return nil
}

func (a *App) SearchQuery() string { return a.search.query }

func (a *App) SearchMatchCount() int { return len(a.search.order) }

func (a *App) SearchMatchIndex() int {
	if a.search.current < 0 || a.search.current >= len(a.search.order) {
		return 0
	}
	return a.search.current + 1
}

func (a *App) CurrentCount() string { return a.pendingCount }

func (a *App) PendingKeys() []string { return append([]string(nil), a.sequence...) }

func (a *App) ClearPendingKeys() {
	a.sequence = nil
	a.pendingMark = ""
	a.pendingCount = ""
	if a.mode == modeNormal {
		a.message = ""
	}
}

func (a *App) FitMode() string { return a.fitMode.String() }

func (a *App) SetFitMode(name string) error {
	a.setFitMode(parseFitMode(name))
	return nil
}

func (a *App) RenderMode() string { return a.renderMode.String() }

func (a *App) SetRenderMode(name string) error {
	mode := parseRenderMode(name)
	if a.renderMode == mode {
		return nil
	}
	a.relayoutWithViewportAnchor(func() { a.renderMode = mode })
	return nil
}

func (a *App) Zoom() float64 { return a.scale }

func (a *App) SetZoom(zoom float64) error {
	if zoom <= 0 {
		return fmt.Errorf("zoom must be positive")
	}
	a.cancelSmoothZoom()
	a.relayoutWithViewportAnchor(func() {
		a.fitMode = fitManual
		a.zoom = a.clampZoom(zoom)
		a.scheduleRenderScaleTarget(a.zoom)
	})
	return nil
}

func (a *App) Rotation() float64 { return normalizeRotation(a.rotation) }

func (a *App) SetRotation(rotation float64) error {
	a.relayoutWithViewportAnchor(func() {
		a.rotation = normalizeRotation(rotation)
		a.updatePageMetricSizes()
	})
	return nil
}

func (a *App) Fullscreen() bool { return a.fullscreen }

func (a *App) SetFullscreen(fullscreen bool) error {
	a.fullscreen = fullscreen
	if a.window == nil {
		return nil
	}
	if fullscreen {
		return renderBool(sdl.SetWindowFullscreen(a.window, true), "set fullscreen")
	}
	return renderBool(sdl.SetWindowFullscreen(a.window, false), "set fullscreen")
}

func (a *App) CacheEntries() int { return len(a.cache.entries) }

func (a *App) CachePending() int { return len(a.renderPending) }

// CacheLimit is the render cache memory limit in MiB; 0 means unlimited.
func (a *App) CacheLimit() int { return int(a.cache.byteLimit >> 20) }

func (a *App) SetCacheLimit(mib int) error {
	if mib < 0 {
		return fmt.Errorf("cache limit must not be negative")
	}
	a.cache.byteLimit = int64(mib) << 20
	a.cache.evict()
	return nil
}

func (a *App) ClearCache() { a.clearCache() }

func (a *App) gotoPageInput(input string) {
	page, ok := a.resolvePageInput(input)
	if !ok {
		a.message = fmt.Sprintf("invalid page or label: %s", strings.TrimSpace(input))
		return
	}
	a.alignPageToAnchor(page)
}

func (a *App) resolvePageInput(input string) (int, bool) {
	input = strings.TrimSpace(input)
	for page, metric := range a.pageMetrics {
		if metric.label != "" && strings.EqualFold(metric.label, input) {
			return page, true
		}
	}
	n, err := strconv.Atoi(input)
	if err != nil {
		return 0, false
	}
	return clampInt(n-1, 0, a.pageCount-1), true
}

// commandHandlers run the built-in : commands, given their arguments; a
// test keeps them in step with the command specs. It is filled in init
// because handlers run commands and actions themselves.
var commandHandlers map[string]func(a *App, args string)

// commandAliases are short names of commands.
var commandAliases = map[string]string{"q": "quit", "q!": "quit!", "p": "page", "w": "write", "w!": "write!"}

func init() {
	commandHandlers = map[string]func(*App, string){
		"quit":   func(a *App, _ string) { a.quit = a.confirmDiscard(":q") },
		"quit!":  func(a *App, _ string) { a.quit = true },
		"write":  func(a *App, args string) { a.writeDocument(args, false) },
		"write!": func(a *App, args string) { a.writeDocument(args, true) },
		"wq": func(a *App, args string) {
			a.writeDocument(args, false)
			if task := a.remoteTask; task != nil && task.kind == remoteSave {
				task.quitAfter = true // once it is on the remote host
				return
			}
			a.quit = !a.unsaved
		},
		"print":     (*App).printDocument,
		"undo":      func(a *App, _ string) { a.undoEdit(false) },
		"redo":      func(a *App, _ string) { a.undoEdit(true) },
		"highlight": func(a *App, _ string) { a.highlightSelection(a.highlightColor) },
		"page":      withArgument("usage: :page <n>", (*App).gotoPageInput),
		"set":       (*App).runSet,
		"mode":      withArgument("usage: :mode continuous|single", func(a *App, mode string) { a.SetRenderMode(mode) }),
		"colors": withArgument("usage: :colors normal|alt", func(a *App, colors string) {
			a.setAltColors(strings.EqualFold(colors, "alt"))
		}),
		"fit":           withArgument("usage: :fit width|height|page|manual", func(a *App, mode string) { a.SetFitMode(mode) }),
		"rotate":        (*App).runRotateCommand,
		"zoom":          (*App).runZoomCommand,
		"reload-config": func(a *App, _ string) { a.reloadConfig() },
		"keybinds":      func(a *App, _ string) { a.toggleKeybindMenu() },
		"theme": func(a *App, name string) {
			if name == "" {
				a.showThemePicker()
				return
			}
			a.chooseTheme(name)
		},
		"matches": func(a *App, _ string) { a.showSearchMatches() },
		"search":  func(a *App, query string) { a.startSearch(query, searchModeForward) },
		"open": func(a *App, path string) {
			if path == "" {
				a.message = "usage: :open <filename>"
			} else if err := a.Open(unescapeCommandArg(path)); err != nil {
				a.message = err.Error()
			}
		},
		"open_file_picker": func(a *App, _ string) { a.runAction("open_file_picker") },
		"recent":           func(a *App, _ string) { a.showRecentFiles() },
		"lua":              (*App).runLuaCommand,
		"help":             func(a *App, _ string) { a.toggleHelp() },
		"version":          func(a *App, _ string) { a.message = "onon " + config.Version },
	}
	for name, action := range actionCommands {
		commandHandlers[name] = func(a *App, _ string) { a.runAction(action) }
	}
	for name, toggle := range toggleCommands {
		commandHandlers[name] = func(a *App, args string) { a.runToggleCommand(name, toggle, args) }
	}
}

// withArgument adapts a command taking one argument, its first word,
// showing usage when there is none.
func withArgument(usage string, run func(a *App, arg string)) func(*App, string) {
	return func(a *App, args string) {
		arg, _, _ := strings.Cut(args, " ")
		if arg == "" {
			a.message = usage
			return
		}
		run(a, arg)
	}
}

func (a *App) runCommand(input string) {
	command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), ":"))
	if _, err := strconv.Atoi(command); err == nil {
		a.gotoPageInput(command)
		return
	}
	name, args, _ := strings.Cut(command, " ")
	if name == "" {
		return
	}
	args = strings.TrimSpace(args)
	if alias, ok := commandAliases[name]; ok {
		name = alias
	}
	if run, ok := commandHandlers[name]; ok {
		run(a, args)
		return
	}
	if a.runtime != nil {
		if handled, err := a.runtime.RunPluginCommand(name, args); handled {
			if err != nil {
				a.message = err.Error()
			}
			a.applyRuntimeChanges("command")
			return
		}
	}
	a.message = "unknown command: " + name
}

func (a *App) runLuaCommand(code string) {
	if a.runtime == nil {
		a.message = "no Lua runtime"
		return
	}
	dirty, err := a.runtime.Eval(code)
	if err != nil {
		a.message = err.Error()
		return
	}
	if dirty {
		a.applyRuntimeChanges("command")
	}
}

func unescapeCommandArg(arg string) string {
	if !strings.Contains(arg, `\`) {
		return arg
	}
	var b strings.Builder
	b.Grow(len(arg))
	for i := 0; i < len(arg); i++ {
		if arg[i] == '\\' && i+1 < len(arg) && (arg[i+1] == ' ' || arg[i+1] == '\\') {
			b.WriteByte(arg[i+1])
			i++
			continue
		}
		b.WriteByte(arg[i])
	}
	return b.String()
}

func (a *App) reloadConfig() {
	oldActive := a.activeUIView()
	err := a.runtime.Reload()
	a.removeStaleLuaViews(a.runtime.Generation())
	if err != nil {
		if oldActive != nil && a.views.views[oldActive.id] == oldActive {
			a.showUIView(oldActive)
		}
		a.message = err.Error()
		return
	}
	cfg := a.runtime.Config()
	a.applyConfig(cfg, nil)
	a.emitPluginEvent("config_reloaded", a.documentEventPayload())
	a.message = boolWord(cfg.ConfigPath != "", "config reloaded", "defaults reloaded")
}

// openConfig opens the config file in the system's text editor, creating
// it if there is none.
func (a *App) openConfig() {
	path, err := a.runtime.EditableConfigPath()
	if err == nil {
		err = openTextFile(path)
	}
	if err != nil {
		a.message = err.Error()
		return
	}
	a.message = "editing " + path + "; :reload-config applies it"
}

func (a *App) showRecentFiles() {
	if !a.config.SessionDatabase {
		a.message = "session database disabled"
		return
	}
	paths := config.RecentFiles(a.config.RecentFilesMax)
	if len(paths) == 0 {
		a.message = "no recent files"
		return
	}
	rows := make([]uiRow, len(paths))
	for i, path := range paths {
		name, folder := recentFileParts(path)
		rows[i] = uiRow{index: i, text: name, secondary: folder, value: path}
		if s, ok := config.GetDocumentSession(path); ok {
			rows[i].key = "p. " + strconv.Itoa(s.Page+1)
		}
	}
	a.showRowList("recent-files", "Recent Files", rows, 70, 70, func(view *uiView) {
		view.onSelect = func(a *App, row uiRow) {
			a.CloseUI("recent-files")
			if err := a.Open(row.value); err != nil {
				a.message = err.Error()
			}
		}
	})
}

// recentFileParts splits a recent file's path, or a remote document's URI,
// into its name and the folder it is in, the home directory written ~ and
// a deep folder cut to its last two, which tell folders apart.
func recentFileParts(path string) (name, folder string) {
	if strings.Contains(path, "://") {
		i := strings.LastIndexByte(path, '/')
		return path[i+1:], path[:i]
	}
	name, folder = filepath.Base(path), filepath.Dir(path)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, ok := strings.CutPrefix(folder, home); ok && (rel == "" || rel[0] == filepath.Separator) {
			folder = "~" + rel
		}
	}
	sep := string(filepath.Separator)
	if parts := strings.Split(folder, sep); len(parts) > 4 {
		folder = "…" + sep + strings.Join(parts[len(parts)-2:], sep)
	}
	return name, folder
}

func (a *App) runSet(input string) {
	if a.runtime == nil {
		a.message = "no config runtime"
		return
	}
	input = strings.TrimSpace(input)
	if input == "" {
		a.showOptions()
		return
	}
	if name, value, ok := strings.Cut(input, "="); ok {
		if err := a.runtime.SetOption(name, value); err != nil {
			a.message = err.Error()
			return
		}
		a.applyRuntimeChanges("set")
		name = strings.TrimSpace(name)
		current, _ := a.runtime.OptionValue(name)
		a.message = name + "=" + current
		return
	}
	if name, ok := strings.CutSuffix(input, "!"); ok {
		if err := a.toggleOption(name); err != nil {
			a.message = err.Error()
			return
		}
		a.applyRuntimeChanges("set")
		name = strings.TrimSpace(name)
		current, _ := a.runtime.OptionValue(name)
		a.message = name + "=" + current
		return
	}
	name := strings.TrimSuffix(input, "?")
	value, err := a.runtime.OptionValue(name)
	if err != nil {
		a.message = err.Error()
		return
	}
	a.message = strings.TrimSpace(name) + "=" + value
}

// showOptions lists the options with their values. Enter on one that is
// true or false switches it, and on another opens the command prompt to
// set it, its value given.
func (a *App) showOptions() {
	a.showRowList("options", "Options", a.optionRows(), 70, 70, func(view *uiView) {
		view.onSelect = func(a *App, row uiRow) {
			if row.secondary != "true" && row.secondary != "false" {
				a.closeAllUI()
				a.mode = modeCommand
				a.input.Set("set " + row.value + "=" + row.secondary)
				return
			}
			if err := a.toggleOption(row.value); err != nil {
				a.message = err.Error()
				return
			}
			a.applyRuntimeChanges("set")
			current, _ := a.runtime.OptionValue(row.value)
			a.message = row.value + "=" + current
			a.setUIViewRows(view, a.optionRows())
		}
	})
}

func (a *App) optionRows() []uiRow {
	names := a.runtime.OptionNames()
	rows := make([]uiRow, 0, len(names))
	for i, name := range names {
		value, _ := a.runtime.OptionValue(name)
		rows = append(rows, uiRow{index: i, text: name, value: name, secondary: value})
	}
	return rows
}

func (a *App) runMouseBinding(event string) bool {
	if action, ok := a.mouseBindings[event]; ok {
		source := a.inputSource
		if source == smoothInputSourceUnset {
			source = smoothInputSourceMouse
		}
		a.runActionFrom(source, action)
		return true
	}
	return false
}
