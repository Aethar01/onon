package viewer

import (
	"errors"
	"math"

	"onon/internal/config"
	"onon/internal/mupdf"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// The pages of background documents, as canvas:document_page draws them:
// each page rendered whole, at the scale it is drawn, on the document's own
// goroutine, and kept as a texture of the window that drew it, as textures
// belong to one renderer.

const (
	// maxBackgroundPages is how many rendered pages a background document
	// keeps for each window before those drawn least recently go.
	maxBackgroundPages = 32
	// unknownPageAspect is the height to width a page is drawn at before
	// its size is known: A4's.
	unknownPageAspect = math.Sqrt2
)

type backgroundPageKey struct {
	window *App
	page   int
}

type backgroundPage struct {
	texture *sdl.Texture // nil in a window with no renderer, as tests make
	scale   float64      // device pixels to the point
	used    uint64
}

type backgroundPageRequest struct {
	window *App
	page   int
	scale  float64 // device pixels to the point wanted, 0 for the page's size only
	aa     int
}

type backgroundPageResult struct {
	document *backgroundDocument
	opened   bool // it opened, or failed to
	request  backgroundPageRequest
	bounds   mupdf.Rect
	scale    float64
	rendered *mupdf.RenderedPage
	err      error
}

// backgroundPages is what a background document's pages are to the main
// goroutine, but for requests, which its own goroutine takes.
type backgroundPages struct {
	requests chan backgroundPageRequest
	bounds   map[int]mupdf.Rect
	kept     map[backgroundPageKey]*backgroundPage
	pending  map[backgroundPageKey]float64
	waiting  map[backgroundPageKey][]backgroundPageWaiter
}

// backgroundPageWaiter is someone waiting for a page rendered at scale or
// sharper, as PrepareBackgroundPage's caller is: done is sent nil once it
// is, or why it will not be.
type backgroundPageWaiter struct {
	scale float64
	done  chan error // buffered, for the one answer
}

func newBackgroundPages() backgroundPages {
	return backgroundPages{
		requests: make(chan backgroundPageRequest, 16),
		bounds:   map[int]mupdf.Rect{},
		kept:     map[backgroundPageKey]*backgroundPage{},
		pending:  map[backgroundPageKey]float64{},
		waiting:  map[backgroundPageKey][]backgroundPageWaiter{},
	}
}

// drop forgets the pages window drew, or every window's for nil.
func (p *backgroundPages) drop(window *App) {
	for key, page := range p.kept {
		if window == nil || key.window == window {
			if page.texture != nil {
				sdl.DestroyTexture(page.texture)
			}
			delete(p.kept, key)
		}
	}
	for key := range p.pending {
		if window == nil || key.window == window {
			delete(p.pending, key)
		}
	}
	for key := range p.waiting {
		if window == nil || key.window == window {
			p.settle(key, errNotOpen)
		}
	}
}

// dropBackgroundPages forgets the pages a drew, as its renderer goes.
func (a *App) dropBackgroundPages() {
	for _, d := range a.windowGroup().backgroundDocuments().open {
		d.pages.drop(a)
	}
}

// renderPages renders the pages asked for until d closes, on d's goroutine.
func (d *backgroundDocument) renderPages(doc *mupdf.Document, notify func(backgroundPageResult) bool) {
	var renderer *mupdf.Renderer
	defer func() {
		if renderer != nil {
			renderer.Close()
		}
	}()
	for {
		var req backgroundPageRequest
		select {
		case <-d.done:
			return
		case req = <-d.pages.requests:
		}
		result := backgroundPageResult{document: d, request: req}
		info, err := doc.PageInfo(req.page)
		result.bounds, result.err = info.Bounds, err
		if err == nil && req.scale > 0 {
			if renderer == nil {
				renderer, result.err = doc.NewRenderer()
			}
			if result.err == nil {
				result.scale = req.scale
				renderer.Arm()
				result.rendered, result.err = renderer.Render(req.page, req.scale, mupdf.DeviceRect(info.Bounds, req.scale), req.aa)
			}
		}
		if !notify(result) {
			return
		}
	}
}

// pollBackgroundDocuments takes what background documents' goroutines
// sent: that they opened, and the pages they rendered, made textures of the
// window that asked.
func (a *App) pollBackgroundDocuments() {
	s := a.windowGroup().backgroundDocuments()
	for {
		select {
		case r := <-s.results:
			a.acceptBackgroundPage(s, r)
		default:
			return
		}
	}
}

func (a *App) acceptBackgroundPage(s *backgroundDocuments, r backgroundPageResult) {
	defer r.rendered.Close()
	d := r.document
	if s.open[d.id] != d {
		return // closed meanwhile
	}
	if r.opened {
		for _, w := range a.windowGroup().windows {
			w.pendingRedraw = true
		}
		return
	}
	key := backgroundPageKey{r.request.window, r.request.page}
	delete(d.pages.pending, key)
	// Those waiting for the page hear what became of it, and the pages
	// others wait for are asked for again, should the asking have been
	// dropped.
	var failed error
	defer func() {
		d.pages.settle(key, failed)
		for other := range d.pages.waiting {
			d.pages.settle(other, nil)
		}
	}()
	if r.err != nil {
		a.logf("background document %q page %d: %v", d.path, r.request.page+1, r.err)
		failed = r.err
		return
	}
	d.pages.bounds[r.request.page] = r.bounds
	w := r.request.window
	if !a.windowGroupHas(w) {
		failed = errNotOpen
		return // the window closed meanwhile
	}
	w.pendingRedraw = true
	if r.rendered == nil {
		return // its size only
	}
	if r.rendered.Image.Bounds().Empty() {
		failed = errors.New("page is empty")
		return
	}
	page := &backgroundPage{scale: r.scale, used: s.clock}
	if w.renderer != nil {
		tex, err := textureFromRGBA(w.renderer, r.rendered.Image)
		if err != nil {
			a.logf("background document %q page %d texture: %v", d.path, r.request.page+1, err)
			failed = err
			return
		}
		page.texture = tex
	}
	if old := d.pages.kept[key]; old != nil && old.texture != nil {
		sdl.DestroyTexture(old.texture)
	}
	d.pages.kept[key] = page
	d.pages.evict(w)
}

// evict keeps at most maxBackgroundPages of the pages w drew.
func (p *backgroundPages) evict(w *App) {
	for {
		var oldest backgroundPageKey
		count := 0
		for key, page := range p.kept {
			if key.window != w {
				continue
			}
			count++
			if count == 1 || page.used < p.kept[oldest].used {
				oldest = key
			}
		}
		if count <= maxBackgroundPages {
			return
		}
		if tex := p.kept[oldest].texture; tex != nil {
			sdl.DestroyTexture(tex)
		}
		delete(p.kept, oldest)
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

// want asks the document's goroutine for page at scale for w, unless what
// w has is as sharp or a render is on its way; scale 0 asks for the page's
// size only.
func (p *backgroundPages) want(w *App, page int, scale float64) {
	key := backgroundPageKey{w, page}
	if kept := p.kept[key]; kept != nil && kept.scale >= scale*renderUpgradeTolerance {
		return
	}
	if pending, ok := p.pending[key]; ok && pending >= scale*renderUpgradeTolerance {
		return
	}
	select {
	case p.requests <- backgroundPageRequest{window: w, page: page, scale: scale, aa: w.config.AntiAliasing}:
		p.pending[key] = scale
	default: // asked again on a later frame
	}
}

// DocumentPage draws page (0-based) of the background document id fitted
// to a rect, a height of 0 keeping its proportions: the page's colour until
// it renders. In a panel with round corners, the page is cut to them.
func (c *elementCanvas) DocumentPage(id, page int, x, y, w, h float64) (float64, float64) {
	a := c.app
	s := a.windowGroup().backgroundDocuments()
	d := s.open[id]
	if d == nil || page < 0 {
		return 0, 0
	}
	now := s.touch(d)
	info := d.Info()
	if info.State == "failed" || info.State == "open" && page >= info.PageCount {
		return 0, 0
	}
	bounds, known := d.pages.bounds[page]
	pw, ph := 1.0, unknownPageAspect
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
			scale = backgroundPageScale(bounds, float64(box.W))
		}
		d.pages.want(a, page, scale)
	}
	kept := d.pages.kept[backgroundPageKey{a, page}]
	if kept != nil {
		kept.used = now
	}
	if c.renderer == nil {
		return w, h
	}
	outline := c.panelOutline()
	switch {
	case kept != nil && kept.texture != nil:
		alpha := a.styleColor(config.Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1}, c.style.Opacity.V).A
		sdl.SetTextureAlphaMod(kept.texture, alpha)
		if outline != nil {
			drawClippedTexture(c.renderer, kept.texture, box, outline)
		} else {
			sdl.RenderTexture(c.renderer, kept.texture, nil, &box)
		}
		sdl.SetTextureAlphaMod(kept.texture, 0xff)
	case outline != nil:
		fillClipped(c.renderer, box, outline, a.styleColor(config.Color{RGB: a.palette().Page, Alpha: 1}, c.style.Opacity.V))
	default:
		fillRect(c.renderer, box, a.styleColor(config.Color{RGB: a.palette().Page, Alpha: 1}, c.style.Opacity.V))
	}
	return w, h
}

// backgroundPageScale is the scale, in device pixels to the point, a page
// of those bounds is rendered at to be drawn width output pixels wide: no
// sharper than a thumbnail.
func backgroundPageScale(bounds mupdf.Rect, width float64) float64 {
	pw, ph := float64(bounds.X1-bounds.X0), float64(bounds.Y1-bounds.Y0)
	return math.Min(width/math.Max(1, pw), overviewThumbLongSide/math.Max(1, math.Max(pw, ph)))
}

// PrepareBackgroundPage renders page (1-based) of the background document
// id for this window, as it is drawn width logical pixels wide, before it
// is drawn. The channel it returns is sent nil once the page is rendered,
// at once if it already is, or why it could not be.
func (a *App) PrepareBackgroundPage(id, page int, width float64) (<-chan error, error) {
	s := a.windowGroup().backgroundDocuments()
	d := s.open[id]
	if d == nil {
		return nil, errNotOpen
	}
	s.touch(d)
	doc, index, err := d.document(page)
	if err != nil {
		return nil, err
	}
	bounds, known := d.pages.bounds[index]
	if !known {
		info, err := doc.PageInfo(index)
		if err != nil {
			return nil, err
		}
		bounds = info.Bounds
		d.pages.bounds[index] = bounds
	}
	key := backgroundPageKey{a, index}
	waiter := backgroundPageWaiter{scale: backgroundPageScale(bounds, float64(a.px(width))), done: make(chan error, 1)}
	d.pages.waiting[key] = append(d.pages.waiting[key], waiter)
	d.pages.settle(key, nil)
	return waiter.done, nil
}

// settle tells those waiting for key's page what became of it: err, if it
// failed, or that it is rendered as sharp as they asked. For the rest it
// is asked for again.
func (p *backgroundPages) settle(key backgroundPageKey, err error) {
	waiting := p.waiting[key]
	if len(waiting) == 0 {
		return
	}
	kept := p.kept[key]
	var rest []backgroundPageWaiter
	for _, w := range waiting {
		switch {
		case err != nil:
			w.done <- err
		case kept != nil && kept.scale >= w.scale*renderUpgradeTolerance:
			w.done <- nil
		default:
			p.want(key.window, key.page, w.scale)
			rest = append(rest, w)
		}
	}
	if len(rest) == 0 {
		delete(p.waiting, key)
	} else {
		p.waiting[key] = rest
	}
}
