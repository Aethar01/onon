package viewer

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"onon/internal/commands"
	"onon/internal/config"
	"onon/internal/remote"
)

type completionState struct {
	view  *uiView
	items []completionItem
	start int
	end   int
}

type completionItem struct {
	value   string
	display string
	recent  bool
}

func (a *App) showCompletion() {
	if a.mode != modeCommand {
		return
	}
	if a.completion.view != nil && a.completion.view.visible {
		a.moveCompletion(1)
		return
	}
	items, start, end := a.commandCompletions()
	if len(items) == 0 {
		return
	}
	a.completion = completionState{items: items, start: start, end: end}
	if len(items) == 1 {
		a.acceptCompletion()
		return
	}
	// The completions are a panel over the status bar, their text lined up
	// with the word being completed; a click accepts one.
	view := &uiView{
		id: "completion", owner: "core", headless: true, element: config.ElementCompletion,
		rows: completionRowsFor(items), selected: -1,
		onSelect: func(a *App, _ uiRow) { a.acceptCompletion() },
	}
	uiViewSelectedRow(view, view.rows)
	a.completion.view = view
	a.placeCompletion()
	a.showUIView(view)
}

// placeCompletion sizes the completions to their widest, showing at most
// completion_max_items rows, and pins them above the status bar, their
// text under the word being completed.
func (a *App) placeCompletion() {
	view := a.completion.view
	if a.fontFace == nil {
		return // nothing is drawn without a font
	}
	panel, row := a.style(config.ElementCompletion), a.style(config.ElementRow)
	padTop, padRight, padBottom, padLeft := a.insets(panel.Padding.V)
	_, rowPadRight, _, rowPadLeft := a.insets(row.Padding.V)
	widest := 0
	for _, r := range view.rows {
		widest = max(widest, a.depthIndent(r.depth)+measureText(a.fontFace, r.text))
	}
	prefix := a.rowPrefix(view, view.rows, math.MaxInt32)
	widest += prefix.width() + prefix.marker
	margin := a.ipx(8)
	width := widest + int(math.Round(float64(padLeft+rowPadLeft+rowPadRight+padRight)))
	width = clampInt(width, a.ipx(120), max(a.ipx(120), a.winW-2*margin))
	shown := min(len(view.rows), max(1, a.config.CompletionMaxItems))
	height := shown*a.modalListRowHeight() + int(math.Round(float64(padTop+padBottom)))
	x := a.promptOrigin() + measureText(a.fontFace, a.inputPrefix()+a.input.Left()) - int(math.Round(float64(padLeft+rowPadLeft)))
	x = clampInt(x, margin, max(margin, a.winW-width-margin))
	y := a.statusTop() - a.ipx(6)
	scale := float64(a.px(1))
	// Half a pixel over, so the rows fit however the scale rounds.
	view.place = viewPlacement{
		width:  config.UISize{Value: float64(width) / scale},
		height: config.UISize{Value: (float64(height) + 0.5) / scale},
		at:     &config.UIPoint{X: float64(x) / scale, Y: float64(y) / scale},
		anchor: "bottom_left", // its bottom left at the point
	}
}

// moveCompletion moves the selection delta completions, round from one
// end to the other, past the section headings.
func (a *App) moveCompletion(delta int) {
	view := a.completion.view
	if view == nil || !view.visible {
		a.showCompletion()
		return
	}
	n := len(view.rows)
	if n == 0 {
		a.closeCompletion()
		return
	}
	i := max(0, view.selected)
	for range n {
		i = ((i+delta)%n + n) % n
		if !view.rows[i].disabled {
			break
		}
	}
	view.selected = i
	a.ensureUIViewSelectionVisible(view)
	a.pendingRedraw = true
}

func (a *App) acceptCompletion() {
	if len(a.completion.items) == 0 {
		a.closeCompletion()
		return
	}
	value := a.completion.items[0].value
	if view := a.completion.view; view != nil && view.selected >= 0 && view.selected < len(view.rows) {
		value = view.rows[view.selected].value
	}
	a.input.ReplaceRange(a.completion.start, a.completion.end, value)
	a.closeCompletion()
}

func (a *App) closeCompletion() {
	if a.completion.view != nil || len(a.completion.items) > 0 {
		a.closeUIView(a.completion.view, false)
		a.completion = completionState{}
		a.pendingRedraw = true
	}
}

func (a *App) commandCompletions() ([]completionItem, int, int) {
	left := a.input.Left()
	cmdStart := firstNonSpaceRune(left)
	cmdEnd := commandNameEndRune(a.input.Value, cmdStart)
	if a.input.Cursor <= cmdEnd {
		prefix := strings.TrimSpace(sliceRunes(a.input.Value, cmdStart, a.input.Cursor))
		return a.prefixedCommandCompletions(prefix), cmdStart, cmdEnd
	}
	cmd := strings.TrimSpace(sliceRunes(a.input.Value, cmdStart, cmdEnd))
	argStart := nextNonSpaceRune(a.input.Value, cmdEnd)
	if a.input.Cursor < argStart {
		argStart = a.input.Cursor
	}
	argEnd := nextSpaceRune(a.input.Value, argStart)
	arg := sliceRunes(a.input.Value, argStart, a.input.Cursor)
	if cmd == "open" {
		return a.openPathCompletions(arg), argStart, argEnd
	}
	if cmd == "theme" && a.runtime != nil {
		items := []completionItem{}
		for _, choice := range a.runtime.ThemeChoices() {
			if strings.HasPrefix(choice.Name, arg) {
				items = append(items, completionItem{value: choice.Name, display: choice.Name})
			}
		}
		if len(items) > 0 {
			return items, argStart, argEnd
		}
	}
	if cmd == "set" {
		names := config.OptionNames()
		if a.runtime != nil {
			names = a.runtime.OptionNames()
		}
		items := []completionItem{}
		for _, name := range names {
			if strings.HasPrefix(name, arg) {
				items = append(items, completionItem{value: name, display: name})
			}
		}
		if len(items) > 0 {
			return items, argStart, argEnd
		}
	}
	if a.runtime != nil {
		if values := a.runtime.CommandCompletions(cmd, arg); len(values) > 0 {
			items := make([]completionItem, 0, len(values))
			for _, value := range values {
				items = append(items, completionItem{value: value, display: value})
			}
			return items, argStart, argEnd
		}
	}
	if validArgs := commands.ArgCompletionValues(cmd); len(validArgs) > 0 {
		items := []completionItem{}
		for _, v := range validArgs {
			if strings.HasPrefix(v, arg) {
				items = append(items, completionItem{value: v, display: v})
			}
		}
		if len(items) > 0 {
			return items, argStart, argEnd
		}
	}
	return nil, 0, 0
}

func (a *App) prefixedCommandCompletions(prefix string) []completionItem {
	items := []completionItem{}
	for _, spec := range commands.All() {
		if strings.HasPrefix(spec.Name, prefix) {
			items = append(items, completionItem{value: spec.Name, display: spec.Name})
		}
	}
	if a.runtime != nil {
		for _, name := range a.runtime.CommandNames() {
			if strings.HasPrefix(name, prefix) {
				items = append(items, completionItem{value: name, display: name})
			}
		}
	}
	return items
}

func (a *App) openPathCompletions(arg string) []completionItem {
	arg = unescapeCommandArg(arg)
	recent := a.recentFileCompletions(arg)
	base, prefix, typedBase := splitCompletionPath(arg)
	if arg == "." {
		return append(recent, completionItem{value: "." + pathSeparator(), display: "." + pathSeparator()})
	}
	if arg == ".." {
		return append(recent, completionItem{value: ".." + pathSeparator(), display: ".." + pathSeparator()})
	}
	readDir := base
	if strings.HasPrefix(base, "~") {
		readDir = expandHomePath(base)
	} else if !filepath.IsAbs(readDir) && remote.IsURI(a.docPath) {
		return recent // beside a remote document is on its host
	} else if !filepath.IsAbs(readDir) && a.docPath != "" {
		readDir = filepath.Join(filepath.Dir(a.docPath), readDir)
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return recent
	}
	items := []completionItem{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		value := escapeCompletionPath(typedBase + name)
		display := name
		if entry.IsDir() {
			value += pathSeparator()
			display += pathSeparator()
		}
		items = append(items, completionItem{value: value, display: display})
	}
	sort.Slice(items, func(i, j int) bool {
		iDir := strings.HasSuffix(items[i].value, pathSeparator())
		jDir := strings.HasSuffix(items[j].value, pathSeparator())
		if iDir != jDir {
			return iDir
		}
		return strings.ToLower(items[i].display) < strings.ToLower(items[j].display)
	})
	return append(recent, items...)
}

func (a *App) recentFileCompletions(arg string) []completionItem {
	if !a.config.SessionDatabase {
		return nil
	}
	argLower := strings.ToLower(arg)
	items := []completionItem{}
	seen := map[string]bool{}
	for _, path := range config.RecentFiles(a.config.RecentFilesMax) {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		base := filepath.Base(path)
		if argLower != "" && !strings.Contains(strings.ToLower(path), argLower) && !strings.Contains(strings.ToLower(base), argLower) {
			continue
		}
		items = append(items, completionItem{value: escapeCompletionPath(path), display: base, recent: true})
	}
	return items
}

func escapeCompletionPath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == ' ' || (path[i] == '\\' && filepath.Separator != '\\') {
			b.WriteByte('\\')
		}
		b.WriteByte(path[i])
	}
	return b.String()
}

func splitCompletionPath(arg string) (base, prefix, typedBase string) {
	arg = filepath.FromSlash(arg)
	sep := pathSeparator()
	if arg == "" {
		return ".", "", ""
	}
	if arg == "~" {
		return "~", "", "~" + sep
	}
	if hasHomePathPrefix(arg) {
		base, prefix = filepath.Split(arg)
		return strings.TrimSuffix(base, sep), prefix, base
	}
	if strings.HasSuffix(arg, sep) {
		return filepath.Clean(arg), "", arg
	}
	base, prefix = filepath.Split(arg)
	if base != "" {
		return strings.TrimSuffix(base, sep), prefix, base
	}
	return ".", arg, ""
}

func pathSeparator() string {
	return string(filepath.Separator)
}

func expandHomePath(path string) string {
	path = filepath.FromSlash(path)
	if path != "~" && !hasHomePathPrefix(path) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	rest := strings.TrimPrefix(path, "~"+pathSeparator())
	return filepath.Join(home, rest)
}

func hasHomePathPrefix(path string) bool {
	path = filepath.FromSlash(path)
	return strings.HasPrefix(path, "~"+pathSeparator())
}

// completionRowsFor lists items as rows, under section headings when
// some are recent.
func completionRowsFor(items []completionItem) []uiRow {
	rows := []uiRow{}
	categorized := hasRecentItems(items)
	recentHeaderShown := false
	suggestionHeaderShown := false
	heading := func(text string) {
		rows = append(rows, uiRow{index: len(rows), text: text, heading: true, disabled: true})
	}
	for _, item := range items {
		if item.recent && !recentHeaderShown {
			heading("Recents:")
			recentHeaderShown = true
		}
		if !item.recent && categorized && !suggestionHeaderShown {
			heading("Suggestions:")
			suggestionHeaderShown = true
		}
		row := uiRow{index: len(rows), text: item.display, value: item.value}
		if categorized {
			row.depth = 1
		}
		rows = append(rows, row)
	}
	return rows
}

func hasRecentItems(items []completionItem) bool {
	for _, item := range items {
		if item.recent {
			return true
		}
	}
	return false
}

func firstNonSpaceRune(s string) int {
	for i, r := range []rune(s) {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return len([]rune(s))
}

func commandNameEndRune(s string, start int) int {
	runes := []rune(s)
	for i := start; i < len(runes); i++ {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i
		}
	}
	return len(runes)
}

func nextNonSpaceRune(s string, start int) int {
	runes := []rune(s)
	for i := start; i < len(runes); i++ {
		if runes[i] != ' ' && runes[i] != '\t' {
			return i
		}
	}
	return len(runes)
}

func nextSpaceRune(s string, start int) int {
	runes := []rune(s)
	for i := start; i < len(runes); i++ {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i
		}
	}
	return len(runes)
}

func sliceRunes(s string, start, end int) string {
	runes := []rune(s)
	start = clampInt(start, 0, len(runes))
	end = clampInt(end, start, len(runes))
	return string(runes[start:end])
}
