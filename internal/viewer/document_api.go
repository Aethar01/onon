package viewer

import (
	"fmt"
	"math"

	"onon/internal/config"
	"onon/internal/mupdf"
)

func (a *App) Metadata() (config.DocumentMetadata, error) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	if a.doc == nil {
		return config.DocumentMetadata{}, fmt.Errorf("document unavailable")
	}
	metadata, err := a.doc.Metadata()
	if err != nil {
		return config.DocumentMetadata{}, err
	}
	return config.DocumentMetadata{
		Format: metadata.Format, Encryption: metadata.Encryption, Title: metadata.Title, Author: metadata.Author,
		Subject: metadata.Subject, Keywords: metadata.Keywords, Creator: metadata.Creator, Producer: metadata.Producer,
		CreationDate: metadata.CreationDate, ModificationDate: metadata.ModificationDate,
	}, nil
}

func (a *App) Outline() ([]config.DocumentOutlineItem, error) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	if a.doc == nil {
		return nil, fmt.Errorf("document unavailable")
	}
	if a.outline == nil {
		outline, err := a.doc.Outline()
		if err != nil {
			return nil, err
		}
		a.outline = outline
	}
	items := make([]config.DocumentOutlineItem, len(a.outline))
	children := make([][]int, len(a.outline))
	rootIndices := make([]int, 0)
	for i, item := range a.outline {
		items[i] = config.DocumentOutlineItem{Title: item.Title, URI: item.URI, External: item.External, Page: documentPage(item.Page), X: optionalCoordinate(item.X, item.HasX), Y: optionalCoordinate(item.Y, item.HasY)}
		parent := item.Parent
		if parent >= 0 && parent < i {
			children[parent] = append(children[parent], i)
		} else {
			rootIndices = append(rootIndices, i)
		}
	}
	var build func(int) config.DocumentOutlineItem
	build = func(index int) config.DocumentOutlineItem {
		item := items[index]
		for _, child := range children[index] {
			item.Children = append(item.Children, build(child))
		}
		return item
	}
	roots := make([]config.DocumentOutlineItem, 0, len(rootIndices))
	for _, index := range rootIndices {
		roots = append(roots, build(index))
	}
	return roots, nil
}

func (a *App) PageInfo(page int) (config.DocumentPageInfo, error) {
	a.documentAPIMu.Lock()
	defer a.documentAPIMu.Unlock()
	index, err := a.documentPageIndex(page)
	if err != nil {
		return config.DocumentPageInfo{}, err
	}
	metric := a.pageMetrics[index]
	if !metric.loaded {
		info, err := a.doc.PageInfo(index)
		if err != nil {
			return config.DocumentPageInfo{}, err
		}
		metric = newPageMetrics(info)
		a.pageMetrics[index] = metric
	}
	return config.DocumentPageInfo{Page: page, Label: metric.label, Width: float64(metric.bounds.X1 - metric.bounds.X0), Height: float64(metric.bounds.Y1 - metric.bounds.Y0), Bounds: documentRect(metric.bounds)}, nil
}

// Selection reports the current text selection. It reads viewer state owned by
// the main thread, so it is exposed synchronously rather than through a
// plugin operation.
func (a *App) Selection() (config.DocumentSelection, error) {
	selection := config.DocumentSelection{Active: a.selection.active, Text: a.selection.text}
	for _, part := range a.selection.parts {
		quads := make([]config.DocumentRect, len(part.quads))
		for i, quad := range part.quads {
			quads[i] = quadRect(quad)
		}
		selection.Pages = append(selection.Pages, config.DocumentSelectionPage{Page: part.page + 1, Quads: quads})
	}
	return selection, nil
}

// PageText is a page's text, for plugins. The tab answers it rather than
// App, so a plugin's job, which asks from another goroutine, reads the
// document it was started for; see CurrentDocumentHost.
func (t *tab) PageText(page int) (string, error) {
	t.documentAPIMu.Lock()
	defer t.documentAPIMu.Unlock()
	index, err := t.documentPageIndex(page)
	if err != nil {
		return "", err
	}
	return t.doc.PageText(index)
}

// PageLinks is a page's links, for plugins, answered by the tab as
// PageText is.
func (t *tab) PageLinks(page int) ([]config.DocumentPageLink, error) {
	t.documentAPIMu.Lock()
	defer t.documentAPIMu.Unlock()
	index, err := t.documentPageIndex(page)
	if err != nil {
		return nil, err
	}
	links, err := t.linksForPageLocked(index)
	if err != nil {
		return nil, err
	}
	result := make([]config.DocumentPageLink, len(links))
	for i, link := range links {
		result[i] = config.DocumentPageLink{Bounds: documentRect(link.Bounds), URI: link.URI, External: link.External, Page: documentPage(link.Page), X: optionalCoordinate(link.X, link.HasX), Y: optionalCoordinate(link.Y, link.HasY)}
	}
	return result, nil
}

func (t *tab) documentPageIndex(page int) (int, error) {
	if t.doc == nil {
		return 0, fmt.Errorf("document unavailable")
	}
	if page < 1 || page > t.pageCount {
		return 0, fmt.Errorf("page %d out of range [1,%d]", page, t.pageCount)
	}
	return page - 1, nil
}

// CurrentDocumentHost is what answers for the document shown: its tab,
// which plugins' jobs keep hold of while other tabs are shown.
func (a *App) CurrentDocumentHost() any { return a.tab }

func documentPage(page int) int {
	if page < 0 {
		return 0
	}
	return page + 1
}

func optionalCoordinate(value float64, present bool) *float64 {
	if !present {
		return nil
	}
	return &value
}

func quadRect(quad mupdf.Quad) config.DocumentRect {
	x0 := math.Min(math.Min(float64(quad.UL.X), float64(quad.UR.X)), math.Min(float64(quad.LL.X), float64(quad.LR.X)))
	y0 := math.Min(math.Min(float64(quad.UL.Y), float64(quad.UR.Y)), math.Min(float64(quad.LL.Y), float64(quad.LR.Y)))
	x1 := math.Max(math.Max(float64(quad.UL.X), float64(quad.UR.X)), math.Max(float64(quad.LL.X), float64(quad.LR.X)))
	y1 := math.Max(math.Max(float64(quad.UL.Y), float64(quad.UR.Y)), math.Max(float64(quad.LL.Y), float64(quad.LR.Y)))
	return config.DocumentRect{X0: x0, Y0: y0, X1: x1, Y1: y1}
}

func documentRect(rect mupdf.Rect) config.DocumentRect {
	return config.DocumentRect{X0: float64(rect.X0), Y0: float64(rect.Y0), X1: float64(rect.X1), Y1: float64(rect.Y1)}
}

// SupportedExtensions and SupportsPath report what the linked MuPDF build can
// open, so pickers and plugins offer the real format set rather than assuming
// PDF only.
func (a *App) SupportedExtensions() []string { return mupdf.SupportedExtensions() }

func (a *App) SupportsPath(path string) bool { return mupdf.SupportsPath(path) }
