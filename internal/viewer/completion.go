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

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

type completionState struct {
	view  *uiView
	items []completionItem
	start int
	end   int
	// rows are items under their section headings, and rowOf each item's
	// row, built when first drawn; width is the widest row's text in
	// widthFace. They stay as long as the items do.
	rows      []completionRow
	rowOf     []int
	width     int
	widthFace font.Face
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
	rows := make([]uiRow, len(items))
	for i, item := range items {
		rows[i] = uiRow{index: i, text: item.display, value: item.value}
	}
	view := &uiView{id: "completion", owner: "core", rows: rows, selected: 0, modal: false}
	view.draw = func(a *App, renderer *sdl.Renderer) error { return a.drawCompletion(renderer) }
	a.completion.view = view
	a.showUIView(view)
}

func (a *App) moveCompletion(delta int) {
	if a.completion.view == nil || !a.completion.view.visible {
		a.showCompletion()
		return
	}
	n := len(a.completion.items)
	if n == 0 {
		a.closeCompletion()
		return
	}
	a.completion.view.selected = (a.completion.view.selected + delta + n) % n
	a.pendingRedraw = true
}

func (a *App) acceptCompletion() {
	if len(a.completion.items) == 0 {
		a.closeCompletion()
		return
	}
	selected := 0
	if a.completion.view != nil {
		selected = a.completion.view.selected
	}
	item := a.completion.items[clampInt(selected, 0, len(a.completion.items)-1)]
	a.input.ReplaceRange(a.completion.start, a.completion.end, item.value)
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

func (a *App) drawCompletion(renderer *sdl.Renderer) error {
	rows, selectedRow := a.completionRows()
	if len(rows) == 0 {
		return nil
	}
	visible := a.scrollCompletion(len(rows), selectedRow)
	panel, row := a.style(config.ElementCompletion), a.style(config.ElementRow)
	padTop, padRight, padBottom, padLeft := a.insets(panel.Padding.V)
	_, rowPadRight, _, rowPadLeft := a.insets(row.Padding.V)
	rowHeight := a.modalListRowHeight()
	inset := int(math.Round(float64(padLeft + rowPadLeft)))
	margin := a.ipx(8)
	// The popup fits every completion, so it keeps its width as it scrolls.
	if a.completion.widthFace != a.fontFace {
		a.completion.width = 0
		for _, row := range rows {
			a.completion.width = max(a.completion.width, measureText(a.fontFace, row.text))
		}
		a.completion.widthFace = a.fontFace
	}
	width := a.completion.width + int(math.Round(float64(padLeft+rowPadLeft+rowPadRight+padRight)))
	width = clampInt(width, a.ipx(120), max(a.ipx(120), a.winW-2*margin))
	// Line the completions' text up with the word being completed.
	x := a.promptOrigin() + measureText(a.fontFace, a.inputPrefix()+a.input.Left()) - inset
	x = clampInt(x, margin, max(margin, a.winW-width-margin))
	height := visible*rowHeight + int(math.Round(float64(padTop+padBottom)))
	y := max(margin, a.statusTop()-height-a.ipx(6))
	rect := sdl.FRect{X: float32(x), Y: float32(y), W: float32(width), H: float32(height)}
	return a.drawPanel(renderer, config.ElementCompletion, rect, func() error {
		return a.drawCompletionRows(renderer, rows, selectedRow, visible, rect, &panel)
	})
}

// scrollCompletion keeps the selected row in view, scroll_off rows from
// the popup's edges, and returns how many of the total rows show.
func (a *App) scrollCompletion(total, selectedRow int) int {
	view := a.completion.view
	visible := min(total, max(1, a.config.CompletionMaxItems))
	if selectedRow >= 0 {
		view.scroll = modalListScrollForSelection(view.scroll, selectedRow, visible, total, a.config.ScrollOff)
	}
	return visible
}

// drawCompletionRows draws visible of rows at a time, scrolled as the
// completion view's offset says, the selected row's box gliding among
// them.
func (a *App) drawCompletionRows(renderer *sdl.Renderer, rows []completionRow, selectedRow, visible int, rect sdl.FRect, panel *config.Style) error {
	view := a.completion.view
	padTop, padRight, _, padLeft := a.insets(panel.Padding.V)
	rowHeight := a.modalListRowHeight()
	baseline := a.modalListBaselineOffset(rowHeight)
	offset := a.listOffset(view, visible, len(rows))
	top := float64(rect.Y + padTop)
	rowY := func(index float64) int { return int(math.Round(top + (index-offset)*float64(rowHeight))) }
	rowBox := func(y int) sdl.FRect {
		return sdl.FRect{X: rect.X + padLeft, Y: float32(y), W: rect.W - padLeft - padRight, H: float32(rowHeight)}
	}
	list := sdl.FRect{X: rect.X, Y: float32(top), W: rect.W, H: float32(visible * rowHeight)}
	first, last := int(math.Floor(offset)), min(len(rows), int(math.Ceil(offset))+visible)
	err := a.withClip(renderer, list, func() error {
		if selectedRow >= first && selectedRow < last {
			st := a.style(config.ElementRowSelected)
			at := a.animate(tweenKey{kind: "selection", view: view}, float64(selectedRow), a.config.Theme.Motion.Selection)
			a.drawBox(renderer, &st, rowBox(rowY(at)))
		}
		for index := first; index < last; index++ {
			row := rows[index]
			y := rowY(float64(index))
			element := config.ElementRow
			switch {
			case row.item < 0:
				element = config.ElementHeading // as a menu's section titles are
			case index == selectedRow:
				element = config.ElementRowSelected
			}
			st := a.style(element)
			box := rowBox(y)
			if index != selectedRow {
				a.drawBox(renderer, &st, box)
			}
			_, rowPadRight, _, rowPadLeft := a.insets(st.Padding.V)
			textX := int(math.Round(float64(box.X + rowPadLeft)))
			width := int(math.Round(float64(box.X+box.W-rowPadRight))) - textX
			if err := a.drawTextFace(renderer, truncateText(a.styleFace(&st), row.text, width), textX, y+baseline, a.textColor(&st, false), st.Bold.V); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, thumb, ok := listScrollbarRects(list, visible, len(rows), offset); ok {
		a.drawScrollbarThumb(renderer, rect, thumb)
	}
	return nil
}

type completionRow struct {
	text string
	item int // the index of the row's completion, or -1 for a heading
}

// completionRows lists the completions under their section headings,
// with the index of the selected row, or -1.
func (a *App) completionRows() ([]completionRow, int) {
	c := &a.completion
	if len(c.items) == 0 || c.view == nil {
		return nil, -1
	}
	if c.rows == nil {
		c.rows, c.rowOf = completionRowsFor(c.items)
	}
	return c.rows, c.rowOf[clampInt(c.view.selected, 0, len(c.items)-1)]
}

// completionRowsFor lists items under their section headings, with the
// row each item is on.
func completionRowsFor(items []completionItem) ([]completionRow, []int) {
	rows, rowOf := []completionRow{}, make([]int, len(items))
	categorized := hasRecentItems(items)
	recentHeaderShown := false
	suggestionHeaderShown := false
	for i, item := range items {
		if item.recent && !recentHeaderShown {
			rows = append(rows, completionRow{text: "Recents:", item: -1})
			recentHeaderShown = true
		}
		if !item.recent && categorized && !suggestionHeaderShown {
			rows = append(rows, completionRow{text: "Suggestions:", item: -1})
			suggestionHeaderShown = true
		}
		text := item.display
		if categorized {
			text = "  " + text
		}
		rowOf[i] = len(rows)
		rows = append(rows, completionRow{text: text, item: i})
	}
	return rows, rowOf
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
