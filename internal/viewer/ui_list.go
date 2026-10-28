package viewer

import (
	"fmt"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	"onon/internal/config"
	"onon/internal/keys"

	"github.com/jupiterrider/purego-sdl3/sdl"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/image/font"
)

type uiRow struct {
	index     int
	id        string
	text      string
	value     string
	secondary string
	depth     int
	marker    string
	disabled  bool
	key       string        // a key binding, drawn in a column before the text
	heading   bool          // a section title, drawn in the heading face; also disabled
	button    bool          // drawn as a button, until selected
	swatch    *config.Color // a colour sample drawn before the text
	checkbox  bool          // a check box drawn before the text, checked with checked
	checked   bool
	cells     []string   // the text in each of the view's columns
	lua       lua.LValue // the row a plugin gave, handed back to its callbacks
}

// splitHeader splits a panel header such as "Outline /query (3/10)" into
// its title and the detail after it, drawn muted.
func splitHeader(header string) (title, detail string) {
	cut := len(header)
	for _, sep := range []string{" /", " ("} {
		if i := strings.Index(header, sep); i > 0 && i < cut {
			cut = i
		}
	}
	title, detail = header[:cut], strings.TrimSpace(header[cut:])
	// Only the parentheses around the trailing count go; a query keeps its own.
	if strings.HasSuffix(detail, ")") {
		if i := strings.LastIndex(detail, " ("); i >= 0 {
			detail = detail[:i] + "  " + detail[i+2:len(detail)-1]
		} else if strings.HasPrefix(detail, "(") {
			detail = detail[1 : len(detail)-1]
		}
	}
	return title, detail
}

// uiView is a menu or panel: onon's own and plugins' alike. Its body is
// its rows, or what its draw function draws, under a header with its
// title or prompt; hooks give it behaviour of its own.
type uiView struct {
	id         string
	owner      string
	generation int
	title      string
	// header, if set, is the header's text for the rows shown, in place of
	// the title and the query; empty is what an empty list says.
	header   func(*App, *uiView, int) string
	empty    func(*App, *uiView) string
	headless bool           // no header: the rows or body start below the panel's padding
	element  config.Element // the panel's
	modal    bool
	stack    bool // a modal view opened over the menu open, not in its place
	place    viewPlacement
	// resizable lets the pointer drag a docked view's inner edge.
	resizable bool
	visible   bool

	rows       []uiRow
	selected   int
	scroll     int
	searchable bool
	noFilter   bool // rows stay unfiltered, for a view that sets its own for each query
	searching  bool
	query      string
	// offset is the row shown at the list's top, which may be fractional;
	// it follows scroll, the offset last followed being offsetScroll,
	// while settling. See listOffset.
	offset       float64
	offsetScroll int
	settling     bool
	offsetAt     time.Time   // when offset was last brought up to date
	filtered     uiRowFilter // visibleRows' last result
	keyWidth     uiKeyWidth  // keyColumnWidth's last result

	// drawBody draws the body in place of the rows, which the canvas's
	// default draws, returning whether to draw it again on the next frame.
	drawBody func(a *App, canvas *elementCanvas, state config.DrawState) (bool, error)
	// regions are those drawBody last named, and extent how far below the
	// body's top it drew, which scrollPx scrolls into it; all in output
	// pixels.
	regions  []uiRegion
	extent   float64
	scrollPx float64

	draggingScrollbar    bool
	scrollbarDragOffsetY int
	// pointerDown is set from a press in the view to its release, and
	// pointerOver while the pointer is over it, over hoveredRegion.
	pointerDown, pointerOver bool
	hoveredRegion            lua.LValue

	// prompt labels the text field in the header whose text is the query.
	prompt       string
	promptInput  textInput
	promptScroll [2]int
	promptRect   sdl.FRect  // where its text was last drawn
	promptLayout textLayout // its text as last drawn
	// fields are the text fields drawBody has drawn, and controls where
	// it last drew its buttons and check boxes. focusOrder holds what it
	// drew last that the keys can reach, in order, and focus the one they
	// go to; pressedControl is a button or check box pressed and not yet
	// let go.
	fields         map[string]*uiField
	controls       map[string]sdl.FRect
	focusOrder     []string
	focus          string
	pressedControl string
	// columns lay out rows' cells as a table.
	columns []uiColumn

	// keyHook has each key pressed while the view has the keys, but for
	// those typed into it, before the list; it reports whether it took
	// the key.
	keyHook func(a *App, key keys.Key, repeat bool) bool
	// onAction runs an action in the view's own way, reporting false for
	// actions it leaves to the list.
	onAction func(*App, *uiView, string) bool
	// mouseHook has the pointer events over the view before the list,
	// reporting whether it took the event.
	mouseHook func(a *App, ev config.UIMouseEvent) bool
	onSelect  func(*App, uiRow)
	onClose   func(*App)
	// onQueryChanged runs as the user changes the query.
	onQueryChanged func(*App, *uiView)
	// onSelectionChanged runs after an event moves the selection, which
	// was notifiedSelected when it last ran.
	onSelectionChanged func(*App, *uiView)
	notifiedSelected   int
	// onInput runs as a field's text changes, and onSubmit on Enter in a
	// field, or the prompt, whose id is empty.
	onInput, onSubmit func(a *App, id, text string)
	// onClick runs when a button or check box is clicked.
	onClick func(a *App, id string)
}

// uiColumn is a column of a table of rows; see config.UIColumn.
type uiColumn struct {
	title string
	width config.UISize
	fill  bool
	align string
}

// takesKeys reports whether a click gives a panel the keys: it has keys
// of its own, or text to type.
func (v *uiView) takesKeys() bool {
	return v.keyHook != nil || v.prompt != "" || len(v.focusOrder) > 0
}

type uiRowFilter struct {
	query  string
	source []uiRow
	rows   []uiRow
}

// uiKeyWidth is the widest key of rows, measured in face.
type uiKeyWidth struct {
	rows  []uiRow
	face  font.Face
	width int
}

type uiManager struct {
	views  map[string]*uiView
	active *uiView
	// panels are the open views that are not modal, in the order they
	// opened. They stay open as menus come and go, drawn under them.
	panels []*uiView
	// hovered is the panel the pointer was last over, and pressed the one
	// a button went down on, which has the pointer until it is let go.
	hovered, pressed *uiView
	// stack holds the modal views under the active one, which closing it
	// returns to, bottom first.
	stack []*uiView
	// focused is the panel the keys go to, while no menu is open.
	focused *uiView
	// resizing is the docked view whose edge is being dragged.
	resizing *uiView
}

func (m *uiManager) add(view *uiView) {
	if m.views == nil {
		m.views = make(map[string]*uiView)
	}
	if old := m.views[view.id]; old != nil && old != view {
		m.close(old)
	}
	m.views[view.id] = view
}

func (m *uiManager) show(view *uiView) {
	m.add(view)
	view.visible = true
	if !view.modal {
		m.panels = append(slices.DeleteFunc(m.panels, func(p *uiView) bool { return p == view }), view)
		return
	}
	if m.active != nil && m.active != view && m.active.visible && view.stack {
		m.stack = append(m.stack, m.active)
	} else {
		for _, under := range m.stack {
			under.visible = false
		}
		m.stack = nil
		if m.active != nil && m.active != view {
			m.active.visible = false
		}
	}
	m.active = view
}

func (m *uiManager) close(view *uiView) {
	if view == nil {
		return
	}
	view.visible = false
	m.stack = slices.DeleteFunc(m.stack, func(v *uiView) bool { return v == view })
	if m.active == view {
		m.active = nil
		if n := len(m.stack); n > 0 {
			m.active, m.stack = m.stack[n-1], m.stack[:n-1]
		}
	}
	m.panels = slices.DeleteFunc(m.panels, func(p *uiView) bool { return p == view })
	if m.hovered == view {
		m.hovered = nil
	}
	if m.pressed == view {
		m.pressed = nil
	}
	if m.focused == view {
		m.focused = nil
	}
	if m.resizing == view {
		m.resizing = nil
	}
}

// closeAll closes the modal views, leaving the panels open.
func (m *uiManager) closeAll() {
	for _, view := range m.views {
		if view.modal {
			view.visible = false
		}
	}
	m.active = nil
	m.stack = nil
}

// visibleRows returns the rows matching the view's query, which callers must
// not modify. It runs per frame and per pointer motion, so the filtered rows
// are kept until the query changes or rows is assigned a new slice.
func (v *uiView) visibleRows() []uiRow {
	query := strings.ToLower(strings.TrimSpace(v.query))
	if query == "" || v.noFilter {
		return v.rows
	}
	if f := v.filtered; f.query == query && len(f.source) == len(v.rows) && (len(v.rows) == 0 || &f.source[0] == &v.rows[0]) {
		return f.rows
	}
	rows := make([]uiRow, 0, len(v.rows))
	for _, row := range v.rows {
		if strings.Contains(strings.ToLower(row.text), query) || strings.Contains(strings.ToLower(row.value), query) {
			rows = append(rows, row)
		}
	}
	v.filtered = uiRowFilter{query: query, source: v.rows, rows: rows}
	return rows
}

func (v *uiView) frameGeometry(a *App) (sdl.FRect, int) {
	return a.viewGeometry(v)
}

// contentGeometry is the view's frame and how many rows it shows.
func (v *uiView) contentGeometry(a *App) (sdl.FRect, int) {
	return a.viewGeometry(v)
}

// viewInsets is the padding of view's panel.
func (a *App) viewInsets(view *uiView) (top, right, bottom, left float32) {
	return a.insets(a.style(view.element).Padding.V)
}

// uiViewHeadHeight is how far view's rows or body start below its top:
// below its header, if it has one, and its columns' titles.
func (a *App) uiViewHeadHeight(view *uiView) int {
	head := a.modalListHeadHeight()
	if view.headless {
		top, _, _, _ := a.viewInsets(view)
		head = int(math.Round(float64(top)))
	}
	if view.hasColumnTitles() {
		head += a.modalListRowHeight()
	}
	return head
}

func (v *uiView) hasColumnTitles() bool {
	for _, c := range v.columns {
		if c.title != "" {
			return true
		}
	}
	return false
}

// columnSpans lays view's columns out from left to right: each fixed
// column its width, a percentage being of the whole, and the rest shared
// by the others, a gap between them.
func (a *App) columnSpans(view *uiView, left, right int) [][2]int {
	gap := a.ipx(12)
	whole := right - left - gap*(len(view.columns)-1)
	fixed, fills := 0, 0
	widths := make([]int, len(view.columns))
	for i, c := range view.columns {
		if c.fill {
			fills++
			continue
		}
		widths[i] = int(math.Round(a.uiLength(c.width, float64(whole))))
		fixed += widths[i]
	}
	for i, c := range view.columns {
		if c.fill {
			widths[i] = max(0, whole-fixed) / fills
		}
	}
	spans := make([][2]int, len(widths))
	x := left
	for i, w := range widths {
		spans[i] = [2]int{x, w}
		x += w + gap
	}
	return spans
}

// drawCells draws a row's cells, or the columns' titles, in view's
// columns from left to right.
func (a *App) drawCells(renderer *sdl.Renderer, view *uiView, cells []string, left, right, baseline int, face font.Face, clr color.RGBA, bold bool) {
	for i, span := range a.columnSpans(view, left, right) {
		if i >= len(cells) {
			break
		}
		text := truncateText(face, cells[i], span[1])
		x := span[0]
		switch view.columns[i].align {
		case "center":
			x += (span[1] - measureText(face, text)) / 2
		case "right":
			x += span[1] - measureText(face, text)
		}
		a.drawTextFace(renderer, text, x, baseline, clr, bold)
	}
}

// viewBottomPadding is the space below view's last row.
func (a *App) viewBottomPadding(view *uiView) int {
	_, _, bottom, _ := a.viewInsets(view)
	return int(math.Round(float64(bottom)))
}

// uiViewBody is the part of view below its header, where its rows are
// drawn, or a draw function draws.
func (a *App) uiViewBody(view *uiView) sdl.FRect {
	rect, _ := view.contentGeometry(a)
	head := float32(a.uiViewHeadHeight(view))
	return sdl.FRect{X: rect.X, Y: rect.Y + head, W: rect.W, H: max(0, rect.H-head)}
}

// viewRowBox is the box of a row of view at y.
func (a *App) viewRowBox(view *uiView, panel sdl.FRect, y, rowHeight int) sdl.FRect {
	_, right, _, left := a.viewInsets(view)
	return sdl.FRect{X: panel.X + left, Y: float32(y), W: panel.W - left - right, H: float32(rowHeight)}
}

func (a *App) activeUIView() *uiView {
	if a.views.active == nil || !a.views.active.visible {
		return nil
	}
	return a.views.active
}

func (a *App) activeModalUIView() *uiView {
	view := a.activeUIView()
	if view == nil || !view.modal {
		return nil
	}
	return view
}

func (a *App) showUIView(view *uiView) {
	if view.place.dock != "" {
		a.relayoutWithViewportAnchor(func() { a.views.show(view) })
	} else {
		a.views.show(view)
	}
	items := view.visibleRows()
	if len(items) == 0 {
		view.selected = -1
		view.scroll = 0
	} else {
		uiViewSelectedRow(view, items)
	}
	a.pendingRedraw = true
}

func (a *App) closeUIView(view *uiView, callCallback bool) {
	if view == nil || !view.visible {
		return
	}
	if view.place.dock != "" {
		a.relayoutWithViewportAnchor(func() { a.views.close(view) })
	} else {
		a.views.close(view)
	}
	a.pendingRedraw = true
	a.syncTextInput()
	if callCallback && view.onClose != nil {
		view.onClose(a)
	}
}

func (a *App) closeAllUIViews(callCallbacks bool) {
	visible := make([]*uiView, 0, 1)
	for _, view := range a.views.views {
		if view.visible && view.modal {
			visible = append(visible, view)
		}
	}
	a.views.closeAll()
	a.pendingRedraw = true
	a.syncTextInput()
	if callCallbacks {
		for _, view := range visible {
			if view.onClose != nil {
				view.onClose(a)
			}
		}
	}
}

func (a *App) removeStaleLuaViews(generation int) {
	for id, view := range a.views.views {
		if view.owner != "lua" || view.generation == generation {
			continue
		}
		delete(a.views.views, id)
		a.canvas.clearImages() // a reloaded plugin may have changed its images
		if view.visible && view.place.dock != "" {
			a.relayoutWithViewportAnchor(func() { a.views.close(view) })
		} else {
			a.views.close(view)
		}
		if state := a.smoothScrollState(); state != nil && state.modalView == view {
			a.cancelSmoothScroll()
		}
	}
}

func (a *App) setUIViewRows(view *uiView, rows []uiRow) {
	if view == nil {
		return
	}
	view.rows = rows
	if len(rows) == 0 {
		view.selected = -1
		view.scroll = 0
	} else {
		view.selected = clampInt(view.selected, 0, len(rows)-1)
		a.ensureUIViewSelectionVisible(view)
	}
	a.pendingRedraw = true
}

func (a *App) setUIViewQuery(view *uiView, query string) {
	if view == nil {
		return
	}
	view.query = query
	if view.onQueryChanged != nil {
		view.onQueryChanged(a, view)
	}
	a.ensureUIViewSelectionVisible(view)
	a.pendingRedraw = true
}

func (a *App) moveUIViewSelection(view *uiView, delta int) {
	if view == nil {
		return
	}
	items := view.visibleRows()
	if len(items) == 0 {
		return
	}
	row := uiViewSelectedRow(view, items)
	for next := clampInt(row+delta, 0, len(items)-1); next != row; next = clampInt(next+delta, 0, len(items)-1) {
		row = next
		if !items[row].disabled {
			break
		}
	}
	if items[row].disabled {
		return
	}
	view.selected = items[row].index
	_, rows := view.contentGeometry(a)
	view.scroll = modalListScrollForSelection(view.scroll, row, rows, len(items), a.config.ScrollOff)
	settleList(view)
	a.pendingRedraw = true
}

// listPageDelta turns the page movement actions into list movement: Page
// Up and Down move a screen of rows, and first and last page jump to the
// ends. ok is false for other actions.
func (a *App) listPageDelta(view *uiView, action string) (delta int, ok bool) {
	_, rows := view.contentGeometry(a)
	switch action {
	case "next_page":
		return max(1, rows), true
	case "prev_page":
		return -max(1, rows), true
	case "last_page":
		return len(view.rows), true
	case "first_page":
		return -len(view.rows), true
	}
	return 0, false
}

func (a *App) ensureUIViewSelectionVisible(view *uiView) {
	if view == nil {
		return
	}
	items := view.visibleRows()
	if len(items) == 0 {
		view.selected = -1
		view.scroll = 0
		return
	}
	row := uiViewSelectedRow(view, items)
	_, rows := view.contentGeometry(a)
	view.scroll = modalListScrollForSelection(view.scroll, row, rows, len(items), a.config.ScrollOff)
	settleList(view)
}

func uiViewSelectedRow(view *uiView, items []uiRow) int {
	for i, item := range items {
		if item.index == view.selected && !item.disabled {
			return i
		}
	}
	start := clampInt(view.scroll, 0, len(items)-1)
	for offset := range len(items) {
		row := (start + offset) % len(items)
		if !items[row].disabled {
			view.selected = items[row].index
			return row
		}
	}
	view.selected = -1
	return start
}

func scrollUIView(view *uiView, delta, rows int) {
	if view == nil {
		return
	}
	view.scroll = clampInt(view.scroll+delta, 0, max(0, len(view.visibleRows())-rows))
}

func (a *App) drawUIView(renderer *sdl.Renderer, view *uiView) error {
	if view == nil || !view.visible {
		return nil
	}
	return a.drawUIViewFrame(renderer, view)
}

// drawUIViewFrame draws view whether or not it is open, as it is while
// it fades out.
func (a *App) drawUIViewFrame(renderer *sdl.Renderer, view *uiView) error {
	rect, _ := view.frameGeometry(a)
	draw := func() error {
		return a.drawPanel(renderer, view.element, rect, func() error { return a.drawUIViewContent(renderer, view, rect) })
	}
	if at := view.place.at; at != nil && at.Page > 0 {
		// A view pinned to a page goes under the docked views and the
		// status bar as its page does.
		return a.withClip(renderer, a.documentRect(), draw)
	}
	return draw()
}

func (a *App) drawUIViewContent(renderer *sdl.Renderer, view *uiView, rect sdl.FRect) error {
	listRect, rows := view.contentGeometry(a)
	items := view.visibleRows()
	switch {
	case view.prompt != "":
		a.drawPromptHeader(renderer, view, rect, len(items), len(view.rows))
	case !view.headless:
		header := view.title
		if view.header != nil {
			header = view.header(a, view, len(items))
		}
		if header == "" {
			header = "Menu"
		}
		if view.header == nil && (view.searching || view.query != "") {
			header = fmt.Sprintf("%s /%s (%d/%d)", header, view.query, len(items), len(view.rows))
		}
		title, detail := splitHeader(header)
		if err := a.drawModalListHeader(renderer, rect, title, detail); err != nil {
			return err
		}
	}
	if view.hasColumnTitles() {
		titles := make([]string, len(view.columns))
		for i, c := range view.columns {
			titles[i] = c.title
		}
		st := a.style(config.ElementRowDisabled)
		box := a.viewRowBox(view, rect, int(rect.Y)+a.uiViewHeadHeight(view)-a.modalListRowHeight(), a.modalListRowHeight())
		_, padRight, _, padLeft := a.insets(st.Padding.V)
		baseline := int(box.Y) + a.modalListBaselineOffset(a.modalListRowHeight())
		// The titles line up with the cells, past the rows' prefix.
		left := int(math.Round(float64(box.X+padLeft))) + a.rowPrefix(view, items, int(rect.W*0.35)).width()
		a.drawCells(renderer, view, titles, left, int(math.Round(float64(box.X+box.W-padRight))), baseline, a.styleFace(&st), a.textColor(&st, false), st.Bold.V)
	}
	drawRows := func() error {
		if len(items) == 0 {
			if view.drawBody != nil {
				return nil // a view that draws its body has no rows to miss
			}
			empty := "No items"
			if view.empty != nil {
				empty = view.empty(a, view)
			}
			st := a.style(config.ElementRowDisabled)
			_, _, _, left := a.viewInsets(view)
			_, _, _, rowLeft := a.insets(a.style(config.ElementRow).Padding.V)
			baselineOffset := a.modalListBaselineOffset(a.modalListRowHeight())
			return a.drawText(renderer, empty, int(math.Round(float64(listRect.X+left+rowLeft))), int(listRect.Y)+a.uiViewHeadHeight(view)+baselineOffset, a.textColor(&st, false))
		}
		return a.drawUIListItems(renderer, listRect, rows, view, items)
	}
	var err error
	if view.drawBody != nil {
		err = a.drawViewBody(renderer, view, a.uiViewBody(view), drawRows)
	} else {
		err = drawRows()
	}
	a.drawViewScrollbar(renderer, view)
	return err
}

func (a *App) drawUIListItems(renderer *sdl.Renderer, rect sdl.FRect, rows int, view *uiView, items []uiRow) error {
	if view == nil {
		return nil
	}
	rowHeight := a.modalListRowHeight()
	baselineOffset := a.modalListBaselineOffset(rowHeight)
	rows = max(1, rows)
	view.scroll = clampInt(view.scroll, 0, max(0, len(items)-rows))
	offset := a.listOffset(view, rows, len(items))
	prefix := a.rowPrefix(view, items, int(rect.W*0.35))
	// Rows sit where the offset puts them, those part-way out of the list
	// cut off at its edges.
	head := a.uiViewHeadHeight(view)
	top := float64(rect.Y) + float64(head)
	rowY := func(index float64) int { return int(math.Round(top + (index-offset)*float64(rowHeight))) }
	first, last := int(math.Floor(offset)), min(len(items), int(math.Ceil(offset))+rows)
	list := sdl.FRect{X: rect.X, Y: float32(top), W: rect.W, H: float32(rows * rowHeight)}
	err := a.withClip(renderer, list, func() error {
		// The selected row's box is drawn first, gliding to it from the
		// row selected before. It moves among the rows, so it stays with
		// its row as the list scrolls.
		for index := first; index < last; index++ {
			if item := items[index]; item.index == view.selected && !item.heading {
				at := a.animate(tweenKey{kind: "selection", view: view}, float64(index), a.config.Theme.Motion.Selection)
				st := a.style(config.ElementRowSelected)
				a.drawBox(renderer, &st, a.viewRowBox(view, rect, rowY(at), rowHeight))
			}
		}
		for index := first; index < last; index++ {
			item := items[index]
			y := rowY(float64(index))
			element := config.ElementRow
			switch {
			case item.heading:
				element = config.ElementHeading
			case item.index == view.selected:
				element = config.ElementRowSelected
			case item.disabled:
				element = config.ElementRowDisabled
			case item.button:
				element = config.ElementButton
			}
			st := a.style(element)
			box := a.viewRowBox(view, rect, y, rowHeight)
			if element == config.ElementButton {
				// a framed box within the row
				inset := a.px(3)
				a.drawBox(renderer, &st, sdl.FRect{X: box.X + inset, Y: box.Y + inset, W: box.W - 2*inset, H: box.H - 2*inset})
			} else if element != config.ElementRowSelected {
				a.drawBox(renderer, &st, box)
			}
			_, padRight, _, padLeft := a.insets(st.Padding.V)
			face := a.styleFace(&st)
			textX, textEnd := int(math.Round(float64(box.X+padLeft))), int(math.Round(float64(box.X+box.W-padRight)))
			baseline := y + baselineOffset
			if item.heading {
				if err := a.drawTextFace(renderer, truncateText(face, item.text, textEnd-textX), textX, baseline, a.textColor(&st, false), st.Bold.V); err != nil {
					return err
				}
				continue
			}
			text := strings.Repeat("  ", max(0, item.depth)) + item.marker + item.text
			// The right-hand column gets at most 45% of a narrow row.
			secondary := a.truncateModalListText(item.secondary, int(rect.W*0.45))
			secondaryWidth := measureText(a.fontFace, secondary)
			if !item.button {
				// Every row leaves the room any row's swatch, check box or
				// key takes, so their text lines up.
				size := float32(a.uiLineHeight())
				box := sdl.FRect{X: float32(textX), Y: float32(y) + (float32(rowHeight)-size)/2, W: size, H: size}
				if item.swatch != nil {
					swatchStyle := a.style(config.ElementSwatch)
					swatchStyle.Fill.V = *item.swatch
					a.drawBox(renderer, &swatchStyle, box)
				}
				box.X += float32(prefix.swatch)
				if item.checkbox {
					a.drawCheckbox(renderer, box, item.checked, false)
				}
				if item.key != "" {
					if err := a.drawText(renderer, a.truncateModalListText(item.key, prefix.key), textX+prefix.swatch+prefix.checkbox, baseline, a.textColor(&st, true)); err != nil {
						return err
					}
				}
				textX += prefix.width()
			}
			textWidth := textEnd - textX
			if secondary != "" {
				textWidth -= secondaryWidth + a.ipx(12)
			}
			if len(view.columns) > 0 && item.cells != nil {
				a.drawCells(renderer, view, item.cells, textX, textEnd, baseline, face, a.textColor(&st, false), st.Bold.V)
				continue
			}
			if err := a.drawTextFace(renderer, truncateText(face, text, textWidth), textX, baseline, a.textColor(&st, false), st.Bold.V); err != nil {
				return err
			}
			if secondary != "" {
				if err := a.drawText(renderer, secondary, textEnd-secondaryWidth, baseline, a.textColor(&st, true)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return err
}

// listPrefix is the room the rows of a list leave before their text: for
// a swatch, a check box and the key column, each where any row has one.
type listPrefix struct{ swatch, checkbox, key, keyGap int }

func (p listPrefix) width() int { return p.swatch + p.checkbox + p.key + p.keyGap }

// rowPrefix measures the room the rows of items leave before their text,
// the key column at most keyLimit wide.
func (a *App) rowPrefix(view *uiView, items []uiRow, keyLimit int) listPrefix {
	var p listPrefix
	size := a.uiLineHeight()
	for _, item := range items {
		if item.swatch != nil {
			p.swatch = size + a.ipx(a.style(config.ElementSwatch).Gap.V)
		}
		if item.checkbox {
			p.checkbox = size + a.ipx(8)
		}
	}
	if p.key = a.keyColumnWidth(view, items, keyLimit); p.key > 0 {
		p.keyGap = a.ipx(16)
	}
	return p
}

// keyColumnWidth is the width of the key column: the widest key of items,
// at most limit, or 0 with no keys. It is measured once for each list of
// items view shows.
func (a *App) keyColumnWidth(view *uiView, items []uiRow, limit int) int {
	k := &view.keyWidth
	if k.face != a.fontFace || len(k.rows) != len(items) || len(items) > 0 && &k.rows[0] != &items[0] {
		*k = uiKeyWidth{rows: items, face: a.fontFace}
		for _, item := range items {
			if item.key != "" && !item.button {
				k.width = max(k.width, measureText(a.fontFace, item.key))
			}
		}
	}
	return min(k.width, limit)
}

// uiViewIndexAt is the row of view at x, y, which depends on how far into
// a row the list has scrolled.
func (a *App) uiViewIndexAt(view *uiView, x, y int) (uiRow, bool) {
	if view == nil {
		return uiRow{}, false
	}
	rect, rows := view.contentGeometry(a)
	rowHeight, head := a.modalListRowHeight(), a.uiViewHeadHeight(view)
	if _, ok := a.modalListRowAt(rect, rows, head, rowHeight, x, y); !ok {
		return uiRow{}, false
	}
	items := view.visibleRows()
	top := float64(rect.Y) + float64(head)
	itemIndex := int(math.Floor(view.offset + (float64(y)-top)/float64(rowHeight)))
	if itemIndex < 0 || itemIndex >= len(items) {
		return uiRow{}, false
	}
	return items[itemIndex], true
}

func (a *App) uiViewHover(view *uiView, x, y int) bool {
	if view == nil {
		return false
	}
	old := view.selected
	if item, ok := a.uiViewIndexAt(view, x, y); ok {
		if !item.disabled {
			view.selected = item.index
		}
	}
	return old != view.selected
}

func uiRowsFromConfig(rows []config.UIListRow) []uiRow {
	result := make([]uiRow, len(rows))
	for i, row := range rows {
		result[i] = uiRow{
			index: i, id: row.ID, text: row.Text, value: row.Value, secondary: row.Secondary, depth: row.Depth,
			disabled: row.Disabled || row.Heading, key: row.Key, marker: row.Marker, heading: row.Heading, swatch: row.Swatch, lua: row.Lua,
			button: row.Button, checkbox: row.Checkbox, checked: row.Checked, cells: row.Cells,
		}
	}
	return result
}

func uiRowsFromStrings(rows []string) []uiRow {
	result := make([]uiRow, len(rows))
	for i, text := range rows {
		result[i] = uiRow{index: i, text: text, value: text}
	}
	return result
}

// showCoreList shows a menu of strings, onSelect given the one chosen.
func (a *App) showCoreList(id, title string, rows []string, onSelect func(string)) {
	a.showRowList(id, title, uiRowsFromStrings(rows), 70, 70, func(view *uiView) {
		if onSelect != nil {
			view.onSelect = func(_ *App, row uiRow) { onSelect(row.value) }
		}
	})
}

// showRowList replaces any open menu with a searchable menu of rows,
// widthPercent and heightPercent of the window. setup, which sets onSelect
// and may set the selection and hooks, runs before the view is shown.
func (a *App) showRowList(id, title string, rows []uiRow, widthPercent, heightPercent int, setup func(*uiView)) *uiView {
	a.closeAllUI()
	view := &uiView{
		id: id, owner: "core", title: title, rows: rows, selected: min(0, len(rows)-1),
		searchable: true, modal: true, element: config.ElementPanel, place: percentPlace(widthPercent, heightPercent),
	}
	setup(view)
	a.showUIView(view)
	return view
}
