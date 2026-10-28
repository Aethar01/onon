package viewer

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"onon/internal/config"

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
	swatch    *config.Color // a colour sample drawn before the text
	lua       lua.LValue    // the row a plugin gave, handed back to its callbacks
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

type uiView struct {
	id                string
	owner             string
	generation        int
	title             string
	rows              []uiRow
	selected          int
	scroll            int
	searchable        bool
	noFilter          bool // rows stay unfiltered, for a view that sets its own for each query
	searching         bool
	query             string
	visible           bool
	draggingScrollbar bool
	// offset is the row shown at the list's top, which may be fractional;
	// it follows scroll, the offset last followed being offsetScroll,
	// while settling. See listOffset.
	offset               float64
	offsetScroll         int
	settling             bool
	offsetAt             time.Time // when offset was last brought up to date
	scrollbarDragOffsetY int
	modal                bool
	headless             bool // no header: the rows or body start below the panel's padding
	widthPercent         int
	heightPercent        int
	geometry             func(*App) (sdl.FRect, int)
	listGeometry         func(*App) (sdl.FRect, int)
	header               func(*App, *uiView, int) string
	empty                func(*App, *uiView) string
	onSelect             func(*App, uiRow)
	// onAction runs an action in the view's own way, reporting false for
	// actions it leaves to the generic list.
	onAction       func(*App, *uiView, string) bool
	onClose        func(*App)
	onQueryChanged func(*App, *uiView)
	// onSelectionChanged runs after an event moves the selection, which
	// was notifiedSelected when it last ran.
	onSelectionChanged func(*App, *uiView)
	notifiedSelected   int
	onKey              func(*App, *sdl.KeyboardEvent) bool
	onMouseButton      func(*App, *sdl.MouseButtonEvent) bool
	onMouseMotion      func(*App, *sdl.MouseMotionEvent) bool
	draw               func(*App, *sdl.Renderer) error
	filtered           uiRowFilter // visibleRows' last result
	keyWidth           uiKeyWidth  // keyColumnWidth's last result

	// onWheel takes a wheel event before the list scrolls, reporting
	// whether it took it.
	onWheel func(*App, *sdl.MouseWheelEvent) bool
	// drawBody draws the view below its header in place of its rows, which
	// drawRows draws.
	drawBody func(a *App, renderer *sdl.Renderer, body sdl.FRect, drawRows func() error) error
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
	if m.active != nil && m.active != view {
		m.active.visible = false
	}
	m.active = view
}

func (m *uiManager) close(view *uiView) {
	if view == nil {
		return
	}
	view.visible = false
	if m.active == view {
		m.active = nil
	}
	m.panels = slices.DeleteFunc(m.panels, func(p *uiView) bool { return p == view })
	if m.hovered == view {
		m.hovered = nil
	}
	if m.pressed == view {
		m.pressed = nil
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
	if v.geometry != nil {
		return v.geometry(a)
	}
	rect, rows := a.modalListGeometry(v.widthPercent, v.heightPercent)
	if v.listGeometry != nil {
		return rect, rows
	}
	// A short list gets a panel to fit, from the same top, so filtering it
	// leaves the header in place.
	if fit := max(1, len(v.visibleRows())); fit < rows {
		rect.H -= float32((rows - fit) * a.modalListRowHeight())
		rows = fit
	}
	return rect, rows
}

func (v *uiView) contentGeometry(a *App) (sdl.FRect, int) {
	if v.listGeometry != nil {
		return v.listGeometry(a)
	}
	return v.frameGeometry(a)
}

// headHeight is how far view's rows or body start below its top.
func (a *App) uiViewHeadHeight(view *uiView) int {
	if view != nil && view.headless {
		top, _, _, _ := a.insets(a.style(config.ElementPanel).Padding.V)
		return int(math.Round(float64(top)))
	}
	return a.modalListHeadHeight()
}

// uiViewBody is the part of view below its header, where its rows are
// drawn, or a draw function draws.
func (a *App) uiViewBody(view *uiView) sdl.FRect {
	rect, _ := view.contentGeometry(a)
	head := float32(a.uiViewHeadHeight(view))
	return sdl.FRect{X: rect.X, Y: rect.Y + head, W: rect.W, H: max(0, rect.H-head)}
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
	a.views.show(view)
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
	a.views.close(view)
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
		a.views.close(view)
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
	if view.draw != nil {
		return view.draw(a, renderer)
	}
	rect, _ := view.frameGeometry(a)
	return a.drawPanel(renderer, config.ElementPanel, rect, func() error { return a.drawUIViewContent(renderer, view, rect) })
}

func (a *App) drawUIViewContent(renderer *sdl.Renderer, view *uiView, rect sdl.FRect) error {
	listRect, rows := view.contentGeometry(a)
	items := view.visibleRows()
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
	if !view.headless {
		title, detail := splitHeader(header)
		if err := a.drawModalListHeader(renderer, rect, title, detail); err != nil {
			return err
		}
	}
	drawRows := func() error {
		if len(items) == 0 {
			empty := "No items"
			if view.empty != nil {
				empty = view.empty(a, view)
			}
			st := a.style(config.ElementRowDisabled)
			baselineOffset := a.modalListBaselineOffset(a.modalListRowHeight())
			return a.drawText(renderer, empty, int(listRect.X)+a.modalListTextInset(), int(listRect.Y)+a.uiViewHeadHeight(view)+baselineOffset, a.textColor(&st, false))
		}
		return a.drawUIListItems(renderer, listRect, rows, view, items)
	}
	if view.drawBody != nil {
		return view.drawBody(a, renderer, a.uiViewBody(view), drawRows)
	}
	return drawRows()
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
	keyColumn := a.keyColumnWidth(view, items, int(rect.W*0.35))
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
				a.drawBox(renderer, &st, a.rowBox(rect, rowY(at), rowHeight))
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
			}
			st := a.style(element)
			box := a.rowBox(rect, y, rowHeight)
			if element != config.ElementRowSelected {
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
			textWidth := textEnd - textX
			if secondary != "" {
				textWidth -= secondaryWidth + a.ipx(12)
			}
			if item.swatch != nil {
				swatchStyle := a.style(config.ElementSwatch)
				swatchStyle.Fill.V = *item.swatch
				size := float32(a.uiLineHeight())
				a.drawBox(renderer, &swatchStyle, sdl.FRect{X: float32(textX), Y: float32(y) + (float32(rowHeight)-size)/2, W: size, H: size})
				advance := int(size) + a.ipx(swatchStyle.Gap.V)
				textX += advance
				textWidth -= advance
			}
			if item.key != "" {
				if err := a.drawText(renderer, a.truncateModalListText(item.key, keyColumn), textX, baseline, a.textColor(&st, true)); err != nil {
					return err
				}
			}
			if keyColumn > 0 {
				textX += keyColumn + a.ipx(16)
				textWidth -= keyColumn + a.ipx(16)
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
	if err != nil {
		return err
	}
	return a.drawModalListScrollbar(renderer, rect, head, rows, len(items), offset)
}

// keyColumnWidth is the width of the key column: the widest key of items,
// at most limit, or 0 with no keys. It is measured once for each list of
// items view shows.
func (a *App) keyColumnWidth(view *uiView, items []uiRow, limit int) int {
	k := &view.keyWidth
	if k.face != a.fontFace || len(k.rows) != len(items) || len(items) > 0 && &k.rows[0] != &items[0] {
		*k = uiKeyWidth{rows: items, face: a.fontFace}
		for _, item := range items {
			if item.key != "" {
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

func (a *App) uiViewStartScrollbarDrag(view *uiView, x, y int) bool {
	if view == nil {
		return false
	}
	rect, rows := view.contentGeometry(a)
	offset := view.offset
	if !modalListStartScrollbarDrag(rect, a.uiViewHeadHeight(view), rows, len(view.visibleRows()), x, y, &offset, &view.scrollbarDragOffsetY, &view.draggingScrollbar) {
		return false
	}
	a.scrollListTo(view, offset)
	return true
}

func (a *App) uiViewDragScrollbar(view *uiView, y int) {
	if view == nil {
		return
	}
	rect, rows := view.contentGeometry(a)
	offset := view.offset
	modalListDragScrollbar(rect, a.uiViewHeadHeight(view), rows, len(view.visibleRows()), y, &offset, view.scrollbarDragOffsetY)
	a.scrollListTo(view, offset)
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

func (a *App) createCoreListView(id, title string, rows []uiRow, widthPercent, heightPercent int) *uiView {
	selected := -1
	if len(rows) > 0 {
		selected = 0
	}
	view := &uiView{id: id, owner: "core", title: title, rows: rows, selected: selected, searchable: true, modal: true, widthPercent: widthPercent, heightPercent: heightPercent}
	a.views.add(view)
	return view
}

func (a *App) showCoreList(id, title string, rows []string, onSelect func(string)) {
	a.showRowList(id, title, uiRowsFromStrings(rows), 70, 70, func(view *uiView) {
		if onSelect != nil {
			view.onSelect = func(_ *App, row uiRow) { onSelect(row.value) }
		}
	})
}

// showRowList replaces any open UI with a modal list of rows that uses the
// standard key and mouse handling. setup, which sets onSelect and may set
// the selection, runs before the view is shown.
func (a *App) showRowList(id, title string, rows []uiRow, widthPercent, heightPercent int, setup func(*uiView)) *uiView {
	a.closeAllUI()
	view := a.createCoreListView(id, title, rows, widthPercent, heightPercent)
	view.onKey = func(a *App, e *sdl.KeyboardEvent) bool { return a.handleGenericUIViewKey(view, e) }
	view.onMouseButton = func(a *App, e *sdl.MouseButtonEvent) bool { return a.handleGenericUIViewMouseButton(view, e) }
	view.onMouseMotion = func(a *App, e *sdl.MouseMotionEvent) bool { return a.handleGenericUIViewMouseMotion(view, e) }
	setup(view)
	a.showUIView(view)
	return view
}
