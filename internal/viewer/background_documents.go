package viewer

import (
	"errors"
	"fmt"
	"sync"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"
)

// A background document is a document open without a tab: no view,
// session, history or edits, nothing in the tabs or the recent files. A
// plugin opens one to ask about a file other than the one shown, as a file
// browser reads a file's title or draws its first page.
//
// Each has a goroutine of its own, which opens it, renders the pages asked
// for (see background_document_pages.go) and closes it. The questions a
// plugin asks of it, its metadata, outline and pages' text, are answered on
// whichever goroutine asks, MuPDF's document lock keeping them apart from
// the rendering.
//
// The window group keeps them, as the Lua runtime acts for whichever window
// is focused.

// maxBackgroundDocuments is how many background documents a process keeps
// open; opening another closes the one used least recently.
const maxBackgroundDocuments = 8

// noPage asks document for no page.
const noPage = -1

var (
	errBackgroundRemote = errors.New("background documents are local files")
	errNotOpen          = errors.New("document not open")
)

type backgroundDocument struct {
	id   int
	path string

	mu        sync.Mutex // guards state, pageCount, err and doc
	state     string     // opening, open, failed or closed
	pageCount int
	err       string
	doc       *mupdf.Document
	opened    chan struct{} // closed once state leaves opening

	done chan struct{} // closed when it closes
	used uint64        // when it was last used, by the group's clock

	pages backgroundPages // what windows drew of its pages; the main goroutine's
}

type backgroundDocuments struct {
	open    map[int]*backgroundDocument
	next    int
	clock   uint64 // counts uses, for the one used least recently
	results chan backgroundPageResult
}

func (g *windowGroup) backgroundDocuments() *backgroundDocuments {
	if g.background.open == nil {
		g.background.open = map[int]*backgroundDocument{}
		g.background.results = make(chan backgroundPageResult, 64)
	}
	return &g.background
}

// touch marks d used now, for the limit to close the one used least
// recently.
func (s *backgroundDocuments) touch(d *backgroundDocument) uint64 {
	s.clock++
	d.used = s.clock
	return s.clock
}

// OpenBackgroundDocument starts opening path without a tab, returning its
// id and a channel closed once it opened or failed.
func (a *App) OpenBackgroundDocument(path string) (int, <-chan struct{}, error) {
	if remote.IsURI(path) {
		return 0, nil, errBackgroundRemote
	}
	g := a.windowGroup()
	s := g.backgroundDocuments()
	if len(s.open) >= maxBackgroundDocuments {
		var oldest *backgroundDocument
		for _, d := range s.open {
			if oldest == nil || d.used < oldest.used {
				oldest = d
			}
		}
		a.CloseBackgroundDocument(oldest.id)
	}
	s.next++
	d := &backgroundDocument{
		id: s.next, path: config.AbsoluteDocumentPath(path), state: "opening",
		opened: make(chan struct{}), done: make(chan struct{}),
		pages: newBackgroundPages(),
	}
	s.touch(d)
	s.open[d.id] = d
	go d.run(a.openOptions(""), s.results, g.waker)
	return d.id, d.opened, nil
}

// BackgroundDocument returns the background document id, which answers the
// document calls of config's Document*Host interfaces from any goroutine.
func (a *App) BackgroundDocument(id int) (config.BackgroundDocumentInfo, any) {
	s := a.windowGroup().backgroundDocuments()
	d := s.open[id]
	if d == nil {
		return config.BackgroundDocumentInfo{State: "closed"}, nil
	}
	s.touch(d)
	return d.Info(), d
}

// CloseBackgroundDocument closes the background document id, with the
// pages every window drew of it.
func (a *App) CloseBackgroundDocument(id int) {
	s := a.windowGroup().backgroundDocuments()
	d := s.open[id]
	if d == nil {
		return
	}
	delete(s.open, id)
	d.pages.drop(nil)
	d.mu.Lock()
	d.state, d.pageCount = "closed", 0
	d.mu.Unlock()
	close(d.done)
}

// closeBackgroundDocuments closes every background document, as the
// process ends.
func (a *App) closeBackgroundDocuments() {
	for id := range a.windowGroup().backgroundDocuments().open {
		a.CloseBackgroundDocument(id)
	}
}

func (d *backgroundDocument) Info() config.BackgroundDocumentInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	return config.BackgroundDocumentInfo{State: d.state, PageCount: d.pageCount, Error: d.err}
}

func (d *backgroundDocument) settle(state string, doc *mupdf.Document, pages int, err error) {
	d.mu.Lock()
	d.state, d.doc, d.pageCount = state, doc, pages
	if err != nil {
		d.err = err.Error()
	}
	d.mu.Unlock()
	close(d.opened)
}

// run opens the document, renders the pages asked for until it closes,
// then closes it.
func (d *backgroundDocument) run(opts mupdf.OpenOptions, results chan<- backgroundPageResult, waker *loopWaker) {
	notify := func(r backgroundPageResult) bool {
		select {
		case results <- r:
			waker.wake()
			return true
		case <-d.done:
			r.rendered.Close()
			return false
		}
	}
	doc, err := mupdf.Open(d.path, opts)
	if err != nil {
		if mupdf.IsPasswordError(err) {
			err = errors.New("password required")
		}
		d.settle("failed", nil, 0, err)
		notify(backgroundPageResult{document: d, opened: true})
		return
	}
	defer doc.Close()
	pages, err := doc.PageCount()
	if err != nil {
		d.settle("failed", nil, 0, err)
		notify(backgroundPageResult{document: d, opened: true})
		return
	}
	d.settle("open", doc, pages, nil)
	if !notify(backgroundPageResult{document: d, opened: true}) {
		return
	}
	d.renderPages(doc, notify)
}

// document is the open document, and for a question about one of its
// pages (1-based; noPage for none) the page index MuPDF takes.
func (d *backgroundDocument) document(page int) (*mupdf.Document, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == nil || d.state != "open" {
		return nil, 0, errNotOpen
	}
	if page != noPage && (page < 1 || page > d.pageCount) {
		return nil, 0, fmt.Errorf("page %d out of range [1,%d]", page, d.pageCount)
	}
	return d.doc, page - 1, nil
}

func (d *backgroundDocument) Metadata() (config.DocumentMetadata, error) {
	doc, _, err := d.document(noPage)
	if err != nil {
		return config.DocumentMetadata{}, err
	}
	return documentMetadata(doc)
}

func (d *backgroundDocument) Outline() ([]config.DocumentOutlineItem, error) {
	doc, _, err := d.document(noPage)
	if err != nil {
		return nil, err
	}
	return documentOutline(doc)
}

func (d *backgroundDocument) PageInfo(page int) (config.DocumentPageInfo, error) {
	doc, index, err := d.document(page)
	if err != nil {
		return config.DocumentPageInfo{}, err
	}
	return documentPageInfo(doc, index)
}

func (d *backgroundDocument) PageText(page int) (string, error) {
	doc, index, err := d.document(page)
	if err != nil {
		return "", err
	}
	return doc.PageText(index)
}

func (d *backgroundDocument) PageLinks(page int) ([]config.DocumentPageLink, error) {
	doc, index, err := d.document(page)
	if err != nil {
		return nil, err
	}
	links, err := doc.Links(index)
	if err != nil {
		return nil, err
	}
	return pageLinks(links), nil
}
