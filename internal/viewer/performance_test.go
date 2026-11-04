package viewer

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"onon/internal/mupdf"
)

func heavyPDFPath(b *testing.B) string {
	b.Helper()
	path := os.Getenv("ONON_PERF_PDF")
	if path == "" {
		path = filepath.Join("..", "..", "testdata", "perf", "heavy.pdf")
	}
	if _, err := os.Stat(path); err != nil {
		b.Skipf("heavy PDF not found; set ONON_PERF_PDF or add %s", path)
	}
	return path
}

func BenchmarkRowIndexAtContentY(b *testing.B) {
	app := testLayoutApp(100000)
	app.recomputeLayout(1000, 800)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.rowIndexAtContentY(float64((i % len(app.rows)) * 210))
	}
}

func BenchmarkPrefetchVisiblePagesLargeDocument(b *testing.B) {
	app := testLayoutApp(100000)
	app.recomputeLayout(1000, 800)
	app.renderPending = map[tileKey]renderRequest{}
	app.renderBaseScale = 1
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.scrollY = float64((i % len(app.rows)) * 210)
		app.prefetchVisiblePages()
	}
}

func BenchmarkVisibleOutlineIndicesCached(b *testing.B) {
	const count = 50000
	outline := make([]mupdf.OutlineItem, count)
	expanded := map[int]bool{}
	for i := range outline {
		parent := -1
		depth := 0
		if i%10 != 0 {
			parent = i - i%10
			depth = 1
			expanded[parent] = true
		}
		outline[i] = mupdf.OutlineItem{Title: fmt.Sprintf("Item %d", i), Page: i, Parent: parent, Depth: depth, HasChildren: i%10 == 0}
	}
	app := &App{tab: &tab{documentState: documentState{outline: outline}}, uiState: uiState{outlineMenu: outlineMenuState{expanded: expanded}}}
	_ = app.visibleOutlineIndices()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.visibleOutlineIndices()
	}
}

func BenchmarkPerfHeavyPDFOpen(b *testing.B) {
	path := heavyPDFPath(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, err := mupdf.Open(path, mupdf.OpenOptions{})
		if err != nil {
			b.Fatal(err)
		}
		doc.Close()
	}
}

func BenchmarkPerfHeavyPDFPageInfoAllPages(b *testing.B) {
	path := heavyPDFPath(b)
	doc, err := mupdf.Open(path, mupdf.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	pageCount := doc.CachedPageCount()
	if pageCount == 0 {
		b.Skip("PDF has no pages")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for page := 0; page < pageCount; page++ {
			if _, err := doc.PageInfo(page); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPerfHeavyPDFRenderPages(b *testing.B) {
	path := heavyPDFPath(b)
	doc, err := mupdf.Open(path, mupdf.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	renderer, err := doc.NewRenderer()
	if err != nil {
		b.Fatal(err)
	}
	defer renderer.Close()
	pageCount := doc.CachedPageCount()
	if pageCount == 0 {
		b.Skip("PDF has no pages")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page := i % pageCount
		rendered, err := renderer.Render(page, 1.5, image.Rect(-1<<30, -1<<30, 1<<30, 1<<30), 8)
		if err != nil {
			b.Fatal(err)
		}
		rendered.Close()
	}
}

func BenchmarkPerfHeavyPDFSearchAllPages(b *testing.B) {
	path := heavyPDFPath(b)
	query := os.Getenv("ONON_PERF_QUERY")
	if query == "" {
		query = "the"
	}
	doc, err := mupdf.Open(path, mupdf.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	pageCount := doc.CachedPageCount()
	if pageCount == 0 {
		b.Skip("PDF has no pages")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for page := 0; page < pageCount; page++ {
			if _, err := doc.SearchPage(page, query); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPerfHeavyPDFOutline(b *testing.B) {
	path := heavyPDFPath(b)
	doc, err := mupdf.Open(path, mupdf.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := doc.Outline(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPerfHeavyPDFExtractSelection(b *testing.B) {
	doc, err := mupdf.Open(heavyPDFPath(b), mupdf.OpenOptions{})
	if err != nil {
		b.Fatal(err)
	}
	defer doc.Close()
	info, err := doc.PageInfo(0)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// A drag across the page, as refreshSelection does per pointer move.
		focus := mupdf.Point{X: float64(info.Bounds.X1) * float64(i%10+1) / 10, Y: float64(info.Bounds.Y1) / 2}
		if _, err := doc.ExtractSelection(0, mupdf.Point{}, focus, mupdf.SelectChars); err != nil {
			b.Fatal(err)
		}
	}
}
