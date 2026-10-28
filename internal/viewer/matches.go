package viewer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"onon/internal/mupdf"
)

// showSearchMatches lists every match of the current search with the line
// it is on; choosing one makes it the current match.
func (a *App) showSearchMatches() {
	if len(a.search.order) == 0 {
		a.message = a.searchStatusMessage()
		if a.search.query == "" {
			a.message = "no active search"
		}
		return
	}
	pattern := parseSearchQuery(a.search.query, a.page)
	re, _ := compileSearchPattern(pattern) // it compiled for the search
	rows := make([]uiRow, len(a.search.order))
	seen := map[searchLine]int{}
	for i, ref := range a.search.order {
		text, highlight := a.searchMatchContext(ref, pattern, re, seen)
		rows[i] = uiRow{index: i, key: "p. " + a.pageLabel(ref.page), text: text, highlight: highlight, value: strconv.Itoa(i)}
	}
	current := a.search.current
	view := a.showRowList("matches", fmt.Sprintf("Matches for /%s", a.search.query), rows, 80, 40, func(view *uiView) {
		preview := a.previewDocument(view, func(a *App, row uiRow) { a.previewSearchMatch(view, row.index) })
		view.onClose = func(a *App) {
			preview.restore(a)
			a.search.current = current
			a.message = a.searchStatusMessage()
		}
		view.onSelect = func(a *App, row uiRow) {
			preview.restore(a)
			a.closeUIView(view, false)
			a.search.current, _ = strconv.Atoi(row.value)
			a.focusSearchCurrent()
			a.message = a.searchStatusMessage()
		}
		view.selected = max(0, a.search.current)
	})
	view.notifiedSelected = view.selected
}

// previewSearchMatch makes match i the current one and shows it in the
// middle of the larger part of the document view leaves, above it or
// below it.
func (a *App) previewSearchMatch(view *uiView, i int) {
	a.search.current = i
	a.focusSearchCurrent() // which centres it in the viewport
	rect, _ := view.frameGeometry(a)
	_, viewportH := a.viewportSize()
	h := float64(viewportH)
	top := float64(rect.Y - a.documentRect().Y)
	bottom := top + float64(rect.H)
	middle := top / 2
	if h-bottom > top {
		middle = (bottom + h) / 2
	}
	a.scrollBy(0, h/2-middle)
	a.message = a.searchStatusMessage()
}

// matchLead is about how much of a line is kept before a match, in bytes,
// so that a match far along a long line still shows.
const matchLead = 32

// searchLine is a line of a page that matches are on.
type searchLine struct {
	page int
	quad mupdf.Quad // its first
}

// searchMatchContext returns the text of the line a match of pattern,
// or of re for one that needs a regular expression, is on, and where in
// it the match is. A match far along the line has the line start a little
// before it, from a word where there is one. seen counts the matches of
// each line passed, a page's matches being in reading order, so that the
// second on a line is the second in its text.
func (a *App) searchMatchContext(ref searchHitRef, pattern searchPattern, re *regexp.Regexp, seen map[searchLine]int) (string, [2]int) {
	quads := a.search.matches[ref.page][ref.hit].Quads
	if len(quads) == 0 {
		return "", [2]int{}
	}
	first, last := quads[0], quads[len(quads)-1]
	sel, err := a.doc.ExtractSelection(ref.page, first.UL, last.LR, mupdf.SelectLines)
	if err != nil {
		return "", [2]int{}
	}
	line := strings.Join(strings.Fields(sel.Text), " ")
	found := lineMatches(line, pattern, re)
	if len(found) == 0 {
		return line, [2]int{}
	}
	nth := 0
	if len(sel.Quads) > 0 {
		key := searchLine{ref.page, sel.Quads[0]}
		nth = seen[key]
		seen[key]++
	}
	at := found[min(nth, len(found)-1)]
	if at[0] > matchLead {
		cut := at[0] - matchLead/2
		from := strings.LastIndexByte(line[:cut], ' ') + 1
		if from == 0 { // no word to start from, as in a long URL
			from = cut
			for !utf8.RuneStart(line[from]) {
				from++
			}
		}
		line = "…" + line[from:]
		shift := len("…") - from
		at = [2]int{at[0] + shift, at[1] + shift}
	}
	return line, at
}

// lineMatches finds where pattern, or re, matches in line, as the search
// found them on the page: a regular expression as it matches the text
// layer, and plain text ignoring case and runs of spaces, as MuPDF does.
func lineMatches(line string, pattern searchPattern, re *regexp.Regexp) [][2]int {
	var found [][2]int
	if re != nil {
		for _, loc := range re.FindAllStringSubmatchIndex(line, -1) {
			if loc[0] != loc[1] && wordBoundsHold(re, line, loc) {
				found = append(found, [2]int{loc[0], loc[1]})
			}
		}
		return found
	}
	needle := strings.ToLower(strings.Join(strings.Fields(pattern.text), " "))
	lower := strings.ToLower(line)
	if needle == "" || len(lower) != len(line) { // offsets in lower are line's
		return nil
	}
	for from := 0; ; {
		i := strings.Index(lower[from:], needle)
		if i < 0 {
			return found
		}
		found = append(found, [2]int{from + i, from + i + len(needle)})
		from += i + len(needle)
	}
}
