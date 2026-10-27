package viewer

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"onon/internal/mupdf"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

type searchMode int

const (
	searchModeForward searchMode = iota
	searchModeBackward
)

type searchHitRef struct {
	page int
	hit  int
}

type searchState struct {
	query      string
	matches    map[int][]mupdf.SearchHit
	order      []searchHitRef
	current    int
	running    bool
	generation int
	mode       searchMode
}

// searchPattern is a search query with its vim-style modifiers resolved.
type searchPattern struct {
	text      string // what MuPDF searches for, ignoring case, when regex is false
	regex     bool   // match source with Go's regexp instead
	source    string
	matchCase bool
	firstPage int // 0-based bounds of the pages searched, inclusive
	lastPage  int
}

// Names of the empty groups that stand for \< and \> in a compiled pattern.
const (
	wordStartGroup = "ononWordStart"
	wordEndGroup   = "ononWordEnd"
)

type searchRequest struct {
	generation int
	pattern    searchPattern
	startPage  int
	pageCount  int
}

type searchUpdate struct {
	generation int
	page       int
	hits       []mupdf.SearchHit
	done       bool
	err        error
}

type searchWorker struct {
	workerLifecycle
	requests chan searchRequest
	updates  chan searchUpdate
}

func newSearchWorker(doc *mupdf.Document, wake func()) *searchWorker {
	w := &searchWorker{
		workerLifecycle: newWorkerLifecycle(wake),
		requests:        make(chan searchRequest, 1),
		updates:         make(chan searchUpdate, 64),
	}
	go w.run(doc)
	return w
}

func (w *searchWorker) Start(req searchRequest) bool {
	select {
	case <-w.closing:
		return false
	case w.requests <- req:
		return true
	default:
	}
	select {
	case <-w.closing:
		return false
	case <-w.requests:
	default:
	}
	select {
	case <-w.closing:
		return false
	case w.requests <- req:
		return true
	}
}

func (w *searchWorker) run(doc *mupdf.Document) {
	defer close(w.done)
	if doc == nil {
		sendWorkerUpdate(&w.workerLifecycle, w.updates, searchUpdate{done: true, err: fmt.Errorf("search worker: no document open")})
		w.closeOnce.Do(func() { close(w.closing) })
		return
	}
	for {
		var req searchRequest
		select {
		case <-w.closing:
			return
		case req = <-w.requests:
		}
		for {
			if req.pattern.empty() {
				sendWorkerUpdate(&w.workerLifecycle, w.updates, searchUpdate{generation: req.generation, done: true})
				break
			}
			re, err := compileSearchPattern(req.pattern)
			if err != nil {
				sendWorkerUpdate(&w.workerLifecycle, w.updates, searchUpdate{generation: req.generation, done: true, err: err})
				break
			}
			restarted := false
			for _, page := range searchPageOrder(req.startPage, req.pageCount) {
				if page < req.pattern.firstPage || page > req.pattern.lastPage {
					continue
				}
				select {
				case <-w.closing:
					return
				case next := <-w.requests:
					req = next
					restarted = true
				default:
				}
				if restarted {
					break
				}
				hits, err := searchDocumentPage(doc, page, req.pattern.text, re)
				if err != nil {
					sendWorkerUpdate(&w.workerLifecycle, w.updates, searchUpdate{generation: req.generation, done: true, err: err})
					restarted = true
					break
				}
				if len(hits) > 0 { // pages without hits change nothing on screen
					sendWorkerUpdate(&w.workerLifecycle, w.updates, searchUpdate{generation: req.generation, page: page, hits: hits})
				}
			}
			if restarted {
				continue
			}
			sendWorkerUpdate(&w.workerLifecycle, w.updates, searchUpdate{generation: req.generation, done: true})
			break
		}
	}
}

// parseSearchQuery resolves the vim-style modifiers in a query. The text is
// literal until \v, after which it is a regular expression until \V. \c and
// \C ignore or match case, \< and \> match the start and end of a word, and
// \%.p, \%3p, \%<3p and \%>3p keep to the current page, page 3, or the pages
// before or after it. \\ is a backslash in literal text.
func parseSearchQuery(query string, currentPage int) searchPattern {
	p := searchPattern{lastPage: math.MaxInt}
	var text, source strings.Builder
	literal := true
	write := func(s string) {
		if literal {
			text.WriteString(s)
			s = regexp.QuoteMeta(s)
		}
		source.WriteString(s)
	}
	words := 0
	for i := 0; i < len(query); i++ {
		if query[i] != '\\' || i+1 == len(query) {
			write(query[i : i+1])
			continue
		}
		switch query[i+1] {
		case 'v':
			literal, p.regex = false, true
		case 'V':
			literal = true
		case 'c':
			p.matchCase = false
		case 'C':
			p.matchCase, p.regex = true, true
		case '<', '>':
			group := wordStartGroup
			if query[i+1] == '>' {
				group = wordEndGroup
			}
			fmt.Fprintf(&source, "(?P<%s%d>)", group, words)
			words++
			p.regex = true
		case '%':
			n, ok := p.restrictPages(query[i+2:], currentPage)
			if !ok {
				write(query[i : i+2])
			}
			i += n
		case '\\':
			if literal {
				write(`\`)
				break
			}
			fallthrough
		default:
			write(query[i : i+2])
		}
		i++
	}
	p.text, p.source = text.String(), source.String()
	return p
}

// restrictPages applies a page modifier such as .p, 3p or <3p from the start
// of s, the rest of a \% item, and reports how many bytes it took.
func (p *searchPattern) restrictPages(s string, currentPage int) (int, bool) {
	rest := strings.TrimLeft(s, "<>")
	if len(s)-len(rest) > 1 {
		return 0, false
	}
	var page, n int
	if strings.HasPrefix(rest, ".") {
		page, n = currentPage, 1
	} else {
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		number, err := strconv.Atoi(rest[:n])
		if err != nil || number < 1 {
			return 0, false
		}
		page = number - 1
	}
	if n == len(rest) || rest[n] != 'p' {
		return 0, false
	}
	switch s[0] {
	case '<':
		p.lastPage = min(p.lastPage, page-1)
	case '>':
		p.firstPage = max(p.firstPage, page+1)
	default:
		p.firstPage, p.lastPage = max(p.firstPage, page), min(p.lastPage, page)
	}
	return len(s) - len(rest) + n + 1, true
}

func (p searchPattern) empty() bool {
	if p.regex {
		return p.source == ""
	}
	return strings.TrimSpace(p.text) == ""
}

// compileSearchPattern returns nil when MuPDF's plain search suffices.
func compileSearchPattern(p searchPattern) (*regexp.Regexp, error) {
	if !p.regex {
		return nil, nil
	}
	if !p.matchCase {
		return regexp.Compile("(?i)" + p.source)
	}
	return regexp.Compile(p.source)
}

func searchDocumentPage(doc *mupdf.Document, page int, text string, re *regexp.Regexp) ([]mupdf.SearchHit, error) {
	if re == nil {
		return doc.SearchPage(page, text)
	}
	layer, err := doc.TextLayer(page)
	if err != nil {
		return nil, err
	}
	return matchTextLayer(layer, re), nil
}

func matchTextLayer(layer *mupdf.TextLayer, re *regexp.Regexp) []mupdf.SearchHit {
	var hits []mupdf.SearchHit
	for _, loc := range re.FindAllStringSubmatchIndex(layer.Text, -1) {
		if loc[0] == loc[1] || !wordBoundsHold(re, layer.Text, loc) {
			continue
		}
		if quads := layer.Quads(loc[0], loc[1]); len(quads) > 0 {
			hits = append(hits, mupdf.SearchHit{Quads: quads})
		}
	}
	return hits
}

// wordBoundsHold checks where a match passed through \< and \>, which Go's
// regexp cannot test itself: its \b knows only ASCII words.
func wordBoundsHold(re *regexp.Regexp, text string, loc []int) bool {
	for i, name := range re.SubexpNames() {
		at := loc[2*i]
		if at < 0 {
			continue
		}
		before, after := wordRuneBefore(text, at), wordRuneAfter(text, at)
		if strings.HasPrefix(name, wordStartGroup) && (before || !after) ||
			strings.HasPrefix(name, wordEndGroup) && (!before || after) {
			return false
		}
	}
	return true
}

func wordRuneBefore(text string, at int) bool {
	r, size := utf8.DecodeLastRuneInString(text[:at])
	return size > 0 && isWordRune(r)
}

func wordRuneAfter(text string, at int) bool {
	r, size := utf8.DecodeRuneInString(text[at:])
	return size > 0 && isWordRune(r)
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

func searchPageOrder(start, count int) []int {
	pages := make([]int, 0, count)
	if count <= 0 {
		return pages
	}
	start = clampInt(start, 0, count-1)
	for page := start; page < count; page++ {
		pages = append(pages, page)
	}
	for page := 0; page < start; page++ {
		pages = append(pages, page)
	}
	return pages
}

func (a *App) initSearch() {
	a.logf("init search state")
	a.search = searchState{matches: map[int][]mupdf.SearchHit{}, current: -1, mode: searchModeForward}
}

func (a *App) pollSearchUpdates() {
	if a.searchWorker == nil {
		return
	}
	for {
		select {
		case update := <-a.searchWorker.updates:
			if update.generation == 0 && update.err != nil {
				a.logf("search worker failed err=%v", update.err)
				a.search.running = false
				a.message = update.err.Error()
				continue
			}
			if update.generation != a.search.generation {
				continue
			}
			a.pendingRedraw = true
			if update.err != nil {
				a.logf("search failed query=%q err=%v", a.search.query, update.err)
				a.search.running = false
				a.message = update.err.Error()
				continue
			}
			if update.done {
				a.search.running = false
				if len(a.search.order) == 0 && a.search.query != "" {
					a.message = fmt.Sprintf("no matches for %s", a.search.query)
				} else if len(a.search.order) > 0 {
					a.message = a.searchStatusMessage()
				}
				continue
			}
			if len(update.hits) > 0 {
				a.search.matches[update.page] = update.hits
				for i := range update.hits {
					a.search.order = append(a.search.order, searchHitRef{page: update.page, hit: i})
				}
				if a.search.current < 0 {
					a.search.current = 0
					a.focusSearchCurrent()
				}
				a.message = a.searchStatusMessage()
			}
		default:
			return
		}
	}
}

func (a *App) startSearch(query string, mode searchMode) {
	query = strings.TrimSpace(query)
	pattern := parseSearchQuery(query, a.page)
	a.search.generation++
	a.search.query = query
	a.search.matches = map[int][]mupdf.SearchHit{}
	a.search.order = nil
	a.search.current = -1
	a.search.running = false
	a.search.mode = mode
	if pattern.empty() {
		a.search.query = ""
		a.message = ""
		return
	}
	if a.searchWorker == nil && a.doc != nil {
		a.logf("start search worker path=%q", remote.Display(a.docPath))
		a.searchWorker = newSearchWorker(a.doc, a.wakeLoop)
	}
	if a.searchWorker == nil {
		a.message = "no document open"
		return
	}
	a.search.running = true
	a.logf("start search query=%q pattern=%+v page=%d", query, pattern, a.page+1)
	a.message = fmt.Sprintf("searching for %s", query)
	if !a.searchWorker.Start(searchRequest{
		generation: a.search.generation,
		pattern:    pattern,
		startPage:  a.page,
		pageCount:  a.pageCount,
	}) {
		a.search.running = false
		a.message = "search unavailable"
	}
}

func (a *App) clearSearch() {
	a.search.generation++
	a.search.query = ""
	a.search.matches = map[int][]mupdf.SearchHit{}
	a.search.order = nil
	a.search.current = -1
	a.search.running = false
	a.search.mode = searchModeForward
	a.message = ""
}

func (a *App) repeatSearch(forward bool) {
	delta := 1
	if a.search.mode == searchModeBackward {
		delta = -1
	}
	if !forward {
		delta = -delta
	}
	a.moveSearch(delta)
}

func (a *App) moveSearch(delta int) {
	if a.search.query == "" {
		a.message = "no active search"
		return
	}
	if len(a.search.order) == 0 {
		if a.search.running {
			a.message = fmt.Sprintf("searching for %s", a.search.query)
			return
		}
		a.message = fmt.Sprintf("no matches for %s", a.search.query)
		return
	}
	if a.search.current < 0 {
		if delta >= 0 {
			a.search.current = 0
		} else {
			a.search.current = len(a.search.order) - 1
		}
	} else {
		a.search.current = (a.search.current + delta + len(a.search.order)) % len(a.search.order)
	}
	a.focusSearchCurrent()
	a.message = a.searchStatusMessage()
}

func (a *App) focusSearchCurrent() {
	if a.search.current < 0 || a.search.current >= len(a.search.order) {
		return
	}
	ref := a.search.order[a.search.current]
	a.alignPageToAnchor(ref.page)
	x, y, ok := a.pageScreenOrigin(ref.page)
	if !ok {
		return
	}
	hits := a.search.matches[ref.page]
	if ref.hit < 0 || ref.hit >= len(hits) {
		return
	}
	minX, minY, maxX, maxY := a.searchHitBounds(hits[ref.hit], ref.page, x, y)
	viewportW, viewportH := a.viewportSize()
	centerX := (minX + maxX) / 2
	centerY := (minY + maxY) / 2
	a.scrollBy(centerX-float64(viewportW)/2, centerY-float64(viewportH)/2)
	if a.renderMode == renderSingle {
		a.page = ref.page
	}
}

func (a *App) searchHitBounds(hit mupdf.SearchHit, page int, x, y float64) (float64, float64, float64, float64) {
	minX, minY := math.MaxFloat64, math.MaxFloat64
	maxX, maxY := -math.MaxFloat64, -math.MaxFloat64
	for _, quad := range hit.Quads {
		quadMinX, quadMinY, quadMaxX, quadMaxY := a.quadScreenBounds(quad, page, x, y)
		minX = math.Min(minX, quadMinX)
		minY = math.Min(minY, quadMinY)
		maxX = math.Max(maxX, quadMaxX)
		maxY = math.Max(maxY, quadMaxY)
	}
	return minX, minY, maxX, maxY
}

func (a *App) drawSearchHighlightsForPage(renderer *sdl.Renderer, page int, x, y float64) {
	hits := a.search.matches[page]
	if len(hits) == 0 {
		return
	}
	for i, hit := range hits {
		bg := rgb(a.palette().Search)
		if a.search.current >= 0 && a.search.current < len(a.search.order) {
			if ref := a.search.order[a.search.current]; ref.page == page && ref.hit == i {
				bg = rgb(a.palette().SearchCurrent)
			}
		}
		a.drawHighlightQuads(renderer, hit.Quads, page, x, y, bg)
	}
}

func (a *App) drawHighlightQuads(renderer *sdl.Renderer, quads []mupdf.Quad, page int, x, y float64, bg color.RGBA) {
	for _, quad := range quads {
		minX, minY, maxX, maxY := a.quadScreenBounds(quad, page, x, y)
		a.drawTextHighlight(renderer, sdl.FRect{X: float32(minX), Y: float32(minY), W: float32(maxX - minX), H: float32(maxY - minY)}, bg)
	}
}

func (a *App) searchStatusMessage() string {
	if a.search.query == "" {
		return ""
	}
	if len(a.search.order) == 0 || a.search.current < 0 {
		return fmt.Sprintf("search /%s", a.search.query)
	}
	return fmt.Sprintf("match %d/%d /%s", a.search.current+1, len(a.search.order), a.search.query)
}

func (a *App) searchStatusCounter() string {
	if a.search.query == "" || len(a.search.order) == 0 || a.search.current < 0 {
		return ""
	}
	return fmt.Sprintf("[%d/%d]", a.search.current+1, len(a.search.order))
}
