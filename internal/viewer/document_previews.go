package viewer

import (
	"errors"
	"math"
	"sync"

	"onon/internal/config"
	"onon/internal/mupdf"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A preview is a document opened without a tab, for a plugin to draw pages
// of, as a file browser draws the file selected. It has no view, session,
// history or edits: only its pages, rendered on a goroutine of its own as
// canvases ask for them. The window group keeps previews, as the Lua
// runtime acts for whichever window is focused; the pages each window has
// drawn are kept as textures of that window's renderer.

const (
	// maxPreviews is how many previews a process keeps open; opening
	// another closes the one drawn least recently.
	maxPreviews = 8
	// maxPreviewPages is how many rendered pages a preview keeps, for each
	// window, before those drawn least recently go.
	maxPreviewPages = 32
	// previewAspect is the height to width a page is drawn at before its
	// size is known: A4's.
	previewAspect = math.Sqrt2
)

var errPreviewRemote = errors.New("previews are of local files")

type previewKey struct {
	window *App
	page   int
}

type previewPage struct {
	texture *sdl.Texture // nil in a window with no renderer, as tests make
	scale   float64      // device pixels to the point
	used    uint64
}

type previewRequest struct {
	window *App
	page   int
	scale  float64 // device pixels to the point wanted, 0 for the page's size only
	aa     int
}

type previewResult struct {
	preview  *detachedDocument
	opened   bool // the document opened, or failed to
	request  previewRequest
	bounds   mupdf.Rect
	scale    float64
	rendered *mupdf.RenderedPage
	err      error
}

type detachedDocument struct {
	id   int
	path string

	mu        sync.Mutex // guards state, pageCount and err, read by plugin goroutines
	state     string     // opening, open, failed or closed
	pageCount int
	err       string
	opened    chan struct{} // closed once state leaves opening

	requests chan previewRequest
	done     chan struct{} // closed when the preview closes

	// The rest belongs to the main goroutine.
	bounds  map[int]mupdf.Rect
	pages   map[previewKey]*previewPage
	pending map[previewKey]float64
	used    uint64
}

type previewState struct {
	previews    map[int]*detachedDocument
	nextPreview int
	results     chan previewResult
	clock       uint64 // counts draws, for what was drawn least recently
}

func (p *detachedDocument) Info() config.DocumentPreviewInfo {
	p.mu.Lock()
	defer p.mu.Unlock()
	return config.DocumentPreviewInfo{State: p.state, PageCount: p.pageCount, Error: p.err}
}

func (p *detachedDocument) settle(state string, pages int, err error) {
	p.mu.Lock()
	p.state, p.pageCount = state, pages
	if err != nil {
		p.err = err.Error()
	}
	p.mu.Unlock()
	close(p.opened)
}

func (g *windowGroup) previewState() *previewState {
	if g.previews.previews == nil {
		g.previews.previews = map[int]*detachedDocument{}
		g.previews.results = make(chan previewResult, 64)
	}
	return &g.previews
}

// OpenDocumentPreview starts opening path without a tab, returning the
// preview's id and a channel closed once it opened or failed.
func (a *App) OpenDocumentPreview(path string) (int, <-chan struct{}, error) {
	if remote.IsURI(path) {
		return 0, nil, errPreviewRemote
	}
	g := a.windowGroup()
	s := g.previewState()
	if len(s.previews) >= maxPreviews {
		var oldest *detachedDocument
		for _, p := range s.previews {
			if oldest == nil || p.used < oldest.used {
				oldest = p
			}
		}
		a.CloseDocumentPreview(oldest.id)
	}
	s.nextPreview++
	s.clock++
	p := &detachedDocument{
		id: s.nextPreview, path: config.AbsoluteDocumentPath(path), state: "opening",
		opened: make(chan struct{}), requests: make(chan previewRequest, 16), done: make(chan struct{}),
		bounds: map[int]mupdf.Rect{}, pages: map[previewKey]*previewPage{}, pending: map[previewKey]float64{},
		used: s.clock,
	}
	s.previews[p.id] = p
	go p.run(a.openOptions(""), s.results, g.waker)
	return p.id, p.opened, nil
}

// DocumentPreviewInfo says how the preview id is: closed if there is none.
func (a *App) DocumentPreviewInfo(id int) config.DocumentPreviewInfo {
	if p := a.windowGroup().previewState().previews[id]; p != nil {
		return p.Info()
	}
	return config.DocumentPreviewInfo{State: "closed"}
}

// CloseDocumentPreview closes the preview id, with the pages every window
// drew of it.
func (a *App) CloseDocumentPreview(id int) {
	s := a.windowGroup().previewState()
	p := s.previews[id]
	if p == nil {
		return
	}
	delete(s.previews, id)
	for key, page := range p.pages {
		if page.texture != nil {
			sdl.DestroyTexture(page.texture)
		}
		delete(p.pages, key)
	}
	p.mu.Lock()
	p.state = "closed"
	p.mu.Unlock()
	close(p.done)
}

// closeDocumentPreviews closes every preview, as the process ends.
func (a *App) closeDocumentPreviews() {
	for id := range a.windowGroup().previewState().previews {
		a.CloseDocumentPreview(id)
	}
}

// dropPreviewPages forgets the pages a drew, as its renderer goes.
func (a *App) dropPreviewPages() {
	for _, p := range a.windowGroup().previewState().previews {
		for key, page := range p.pages {
			if key.window == a {
				if page.texture != nil {
					sdl.DestroyTexture(page.texture)
				}
				delete(p.pages, key)
			}
		}
		for key := range p.pending {
			if key.window == a {
				delete(p.pending, key)
			}
		}
	}
}

// run opens the document, then renders the pages asked for until the
// preview closes. Each render is of the whole page at the scale asked.
func (p *detachedDocument) run(opts mupdf.OpenOptions, results chan<- previewResult, waker *loopWaker) {
	send := func(r previewResult) bool {
		select {
		case results <- r:
			waker.wake()
			return true
		case <-p.done:
			r.rendered.Close()
			return false
		}
	}
	doc, err := mupdf.Open(p.path, opts)
	if err != nil {
		if mupdf.IsPasswordError(err) {
			err = errors.New("password required")
		}
		p.settle("failed", 0, err)
		send(previewResult{preview: p, opened: true, err: err})
		return
	}
	defer doc.Close()
	pages, err := doc.PageCount()
	var renderer *mupdf.Renderer
	if err == nil {
		renderer, err = doc.NewRenderer()
	}
	if err != nil {
		p.settle("failed", 0, err)
		send(previewResult{preview: p, opened: true, err: err})
		return
	}
	defer renderer.Close()
	p.settle("open", pages, nil)
	if !send(previewResult{preview: p, opened: true}) {
		return
	}
	for {
		var req previewRequest
		select {
		case <-p.done:
			return
		case req = <-p.requests:
		}
		result := previewResult{preview: p, request: req}
		info, err := doc.PageInfo(req.page)
		result.bounds, result.err = info.Bounds, err
		if err == nil && req.scale > 0 {
			result.scale = req.scale
			renderer.Arm()
			result.rendered, result.err = renderer.Render(req.page, req.scale, mupdf.DeviceRect(info.Bounds, req.scale), req.aa)
		}
		if !send(result) {
			return
		}
	}
}

// pollDocumentPreviews takes what previews' goroutines sent: that they
// opened, and the pages they rendered, made textures of the window that
// asked.
func (a *App) pollDocumentPreviews() {
	s := a.windowGroup().previewState()
	for {
		var r previewResult
		select {
		case r = <-s.results:
		default:
			return
		}
		a.acceptPreviewResult(s, r)
	}
}

func (a *App) acceptPreviewResult(s *previewState, r previewResult) {
	defer r.rendered.Close()
	p := r.preview
	if s.previews[p.id] != p {
		return // closed meanwhile
	}
	if r.opened {
		a.redrawAllWindows()
		return
	}
	key := previewKey{r.request.window, r.request.page}
	delete(p.pending, key)
	if r.err != nil {
		a.logf("preview %q page %d: %v", p.path, r.request.page+1, r.err)
		return
	}
	p.bounds[r.request.page] = r.bounds
	w := r.request.window
	if !a.windowGroupHas(w) {
		return // the window closed meanwhile
	}
	w.pendingRedraw = true
	if r.rendered == nil || r.rendered.Image.Bounds().Empty() {
		return
	}
	page := &previewPage{scale: r.scale, used: s.clock}
	if w.renderer != nil {
		tex, err := textureFromRGBA(w.renderer, r.rendered.Image)
		if err != nil {
			a.logf("preview texture %q page %d: %v", p.path, r.request.page+1, err)
			return
		}
		page.texture = tex
	}
	if old := p.pages[key]; old != nil && old.texture != nil {
		sdl.DestroyTexture(old.texture)
	}
	p.pages[key] = page
	p.evictPages(w)
}

// evictPages keeps at most maxPreviewPages of the pages w drew of p.
func (p *detachedDocument) evictPages(w *App) {
	for {
		var oldest previewKey
		count := 0
		for key, page := range p.pages {
			if key.window != w {
				continue
			}
			count++
			if count == 1 || page.used < p.pages[oldest].used {
				oldest = key
			}
		}
		if count <= maxPreviewPages {
			return
		}
		if tex := p.pages[oldest].texture; tex != nil {
			sdl.DestroyTexture(tex)
		}
		delete(p.pages, oldest)
	}
}

func (a *App) windowGroupHas(w *App) bool {
	for _, other := range a.windowGroup().windows {
		if other == w {
			return true
		}
	}
	return false
}

func (a *App) redrawAllWindows() {
	for _, w := range a.windowGroup().windows {
		w.pendingRedraw = true
	}
}

// want asks p's goroutine for page at scale for w, unless what w has is
// as sharp or a render is on its way; scale 0 asks for the page's size.
func (p *detachedDocument) want(w *App, page int, scale float64) {
	key := previewKey{w, page}
	if kept := p.pages[key]; kept != nil && kept.scale >= scale*renderUpgradeTolerance {
		return
	}
	if pending, ok := p.pending[key]; ok && pending >= scale*renderUpgradeTolerance {
		return
	}
	select {
	case p.requests <- previewRequest{window: w, page: page, scale: scale, aa: w.config.AntiAliasing}:
		p.pending[key] = scale
	default: // asked again on a later frame
	}
}

// DocumentPage draws page (0-based) of the preview id fitted to a rect, a
// height of 0 keeping its proportions: a plain page until it renders.
func (c *elementCanvas) DocumentPage(id, page int, x, y, w, h float64) (float64, float64) {
	a := c.app
	s := a.windowGroup().previewState()
	p := s.previews[id]
	if p == nil || page < 0 {
		return 0, 0
	}
	s.clock++
	p.used = s.clock
	info := p.Info()
	if info.State == "open" && page >= info.PageCount || info.State == "failed" {
		return 0, 0
	}
	bounds, known := p.bounds[page]
	pw, ph := 1.0, previewAspect
	if known {
		pw, ph = float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0)
	}
	w, h = fitSize(pw, ph, w, h, true)
	box := c.rect(x, y, w, h)
	if !c.visible(box) {
		return w, h
	}
	if info.State == "open" {
		scale := 0.0 // the page's size first, to fit it
		if known {
			scale = math.Min(float64(box.W)/math.Max(1, pw), overviewThumbLongSide/math.Max(1, math.Max(pw, ph)))
		}
		p.want(a, page, scale)
	}
	kept := p.pages[previewKey{a, page}]
	if kept != nil {
		kept.used = s.clock
	}
	if c.renderer == nil {
		return w, h
	}
	if kept != nil && kept.texture != nil {
		alpha := a.styleColor(config.Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1}, c.style.Opacity.V).A
		sdl.SetTextureAlphaMod(kept.texture, alpha)
		sdl.RenderTexture(c.renderer, kept.texture, nil, &box)
		sdl.SetTextureAlphaMod(kept.texture, 0xff)
	} else {
		fillRect(c.renderer, box, a.styleColor(config.Color{RGB: a.palette().Page, Alpha: 1}, c.style.Opacity.V))
	}
	return w, h
}
