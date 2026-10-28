package viewer

import (
	"fmt"
	"strings"

	"onon/internal/config"
)

type outlineMenuState struct {
	view                 *uiView
	preview              *documentPreview
	expanded             map[int]bool
	visibleIndices       []int
	visibleQuery         string
	visibleExpandedCount int
}

func (a *App) toggleOutlineMenu() {
	if a.outlineMenu.view != nil && a.outlineMenu.view.visible {
		a.closeUIView(a.outlineMenu.view, true)
		return
	}
	a.closeAllUI()
	if a.outline == nil && a.doc != nil {
		outline, err := a.doc.Outline()
		if err == nil {
			a.outline = outline
		}
	}
	a.outlineMenu = outlineMenuState{expanded: map[int]bool{}}
	a.outlineMenu.view = a.newOutlineView()
	a.outlineMenu.preview = a.previewDocument(a.outlineMenu.view, func(a *App, row uiRow) { a.previewOutlineEntry(row.index) })
	if len(a.outline) > 0 {
		a.outlineMenu.view.selected = a.outlineIndexForPage(a.page)
		for i, item := range a.outline {
			if item.HasChildren && item.Depth < a.config.OutlineInitialDepth {
				a.outlineMenu.expanded[i] = true
			}
		}
		selected := a.outlineMenu.view.selected
		for selected >= 0 && selected < len(a.outline) && a.outline[selected].Parent >= 0 {
			selected = a.outline[selected].Parent
			a.outlineMenu.expanded[selected] = true
		}
	}
	a.refreshOutlineView()
	a.showUIView(a.outlineMenu.view)
	a.outlineMenu.view.notifiedSelected = a.outlineMenu.view.selected // the page shown is already where it opened
}

func (a *App) newOutlineView() *uiView {
	view := &uiView{
		id:         "core:outline",
		owner:      "core",
		modal:      true,
		searchable: true,
		element:    config.ElementPanel,
		place:      menuPlace(a.config.OutlineWidthPercent, a.config.OutlineHeightPercent),
	}
	// Wide enough for its levels to stand apart, however short its titles.
	view.place.least = config.UISize{Value: 40, Percent: true}
	view.header = func(a *App, view *uiView, visible int) string {
		if view.searching || view.query != "" {
			return fmt.Sprintf("Outline /%s (%d/%d)", view.query, visible, len(a.outline))
		}
		return fmt.Sprintf("Outline (%d)", len(a.outline))
	}
	view.empty = func(_ *App, view *uiView) string {
		if view.query != "" {
			return "No matching outline entries"
		}
		return "No PDF outline found"
	}
	view.onSelect = func(a *App, _ uiRow) { a.activateSelectedOutline() }
	view.onAction = func(a *App, view *uiView, action string) bool {
		switch action {
		case "scroll_left":
			a.collapseSelectedOutline()
		case "scroll_right":
			a.expandSelectedOutline()
		case "outline": // its own toggle closes it, as close does
			a.runUIViewAction(view, "close")
		default:
			return false
		}
		return true
	}
	view.onQueryChanged = func(a *App, _ *uiView) {
		a.invalidateVisibleOutlineIndices()
		a.refreshOutlineView()
	}
	return view
}

func (a *App) outlineIndexForPage(page int) int {
	best := 0
	bestPage := -1
	for i, item := range a.outline {
		if item.Page >= 0 && item.Page <= page && item.Page >= bestPage {
			best = i
			bestPage = item.Page
		}
	}
	return best
}

func (a *App) visibleOutlineIndices() []int {
	query := ""
	if a.outlineMenu.view != nil {
		query = strings.ToLower(strings.TrimSpace(a.outlineMenu.view.query))
	}
	expandedCount := len(a.outlineMenu.expanded)
	if a.outlineMenu.visibleIndices != nil && a.outlineMenu.visibleQuery == query && a.outlineMenu.visibleExpandedCount == expandedCount {
		return a.outlineMenu.visibleIndices
	}
	visible := make([]int, 0, len(a.outline))
	for i, item := range a.outline {
		if query != "" {
			if strings.Contains(strings.ToLower(item.Title), query) {
				visible = append(visible, i)
			}
			continue
		}
		show := true
		parent := item.Parent
		for parent >= 0 {
			if !a.outlineMenu.expanded[parent] {
				show = false
				break
			}
			parent = a.outline[parent].Parent
		}
		if show {
			visible = append(visible, i)
		}
	}
	a.outlineMenu.visibleIndices = visible
	a.outlineMenu.visibleQuery = query
	a.outlineMenu.visibleExpandedCount = expandedCount
	return visible
}

func (a *App) invalidateVisibleOutlineIndices() {
	a.outlineMenu.visibleIndices = nil
	a.outlineMenu.visibleQuery = ""
	a.outlineMenu.visibleExpandedCount = 0
}

func (a *App) refreshOutlineView() {
	view := a.outlineMenu.view
	if view == nil {
		return
	}
	visible := a.visibleOutlineIndices()
	rows := make([]uiRow, 0, len(visible))
	for _, index := range visible {
		rows = append(rows, a.outlineRow(index))
	}
	view.rows = rows
	if view.fitRows == nil {
		// Its width is every entry's, so filtering and expanding leave
		// it where it is.
		view.fitRows = make([]uiRow, len(a.outline))
		for i := range a.outline {
			view.fitRows[i] = a.outlineRow(i)
		}
	}
	view.selected = clampOutlineSelection(view.selected, visible)
	a.ensureUIViewSelectionVisible(view)
}

func clampOutlineSelection(selected int, visible []int) int {
	for _, index := range visible {
		if index == selected {
			return selected
		}
	}
	if len(visible) == 0 {
		return -1
	}
	return visible[0]
}

// outlineRow is the row of the outline's entry index.
func (a *App) outlineRow(index int) uiRow {
	item := a.outline[index]
	marker := ""
	if item.HasChildren {
		marker = "▸"
		if a.outlineMenu.expanded[index] {
			marker = "▾"
		}
	}
	text := strings.TrimSpace(item.Title)
	if text == "" {
		text = "untitled"
	}
	secondary := ""
	if item.Page >= 0 {
		secondary = fmt.Sprintf("%d", item.Page+1)
	}
	return uiRow{index: index, text: text, value: item.Title, secondary: secondary, depth: item.Depth, marker: marker}
}

func (a *App) activateSelectedOutline() {
	view := a.outlineMenu.view
	if view == nil || view.selected < 0 || view.selected >= len(a.outline) {
		return
	}
	item := a.outline[view.selected]
	if item.External {
		if a.openDocumentURI(item.URI) {
			a.outlineMenu.preview.restore(a)
			a.closeUIView(view, false)
		}
		return
	}
	if item.Page < 0 {
		return
	}
	a.outlineMenu.preview.restore(a)
	a.closeUIView(view, false)
	a.jumpToDestination(item.Page, item.X, item.Y, item.HasX, item.HasY)
}

// previewOutlineEntry shows the place the outline's entry index names
// behind the outline.
func (a *App) previewOutlineEntry(index int) {
	if index < 0 || index >= len(a.outline) {
		return
	}
	if item := a.outline[index]; !item.External && item.Page >= 0 {
		a.jumpToDestination(item.Page, item.X, item.Y, item.HasX, item.HasY)
	}
}

func (a *App) collapseSelectedOutline() {
	view := a.outlineMenu.view
	if view == nil || view.selected < 0 || view.selected >= len(a.outline) {
		return
	}
	selected := view.selected
	if a.outline[selected].HasChildren && a.outlineMenu.expanded[selected] {
		delete(a.outlineMenu.expanded, selected)
		a.invalidateVisibleOutlineIndices()
		a.refreshOutlineView()
		return
	}
	if parent := a.outline[selected].Parent; parent >= 0 {
		view.selected = parent
		a.refreshOutlineView()
	}
}

func (a *App) expandSelectedOutline() {
	view := a.outlineMenu.view
	if view == nil || view.selected < 0 || view.selected >= len(a.outline) || !a.outline[view.selected].HasChildren {
		return
	}
	a.outlineMenu.expanded[view.selected] = true
	a.invalidateVisibleOutlineIndices()
	a.refreshOutlineView()
}
