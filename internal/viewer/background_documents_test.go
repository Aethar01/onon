package viewer

import (
	"strconv"
	"testing"
	"time"

	"onon/internal/testpdf"
)

// pollBackgroundUntil runs the window's polls until ready says so.
func pollBackgroundUntil(t *testing.T, app *App, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.runtime.PollPluginOperations()
		app.pollBackgroundDocuments()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for the background document")
}

func luaHolds(app *App, expression string) bool {
	_, err := app.runtime.Eval("assert(" + expression + ")")
	return err == nil
}

// openBackgroundTestDocument opens a three-page PDF as doc, and waits for
// it to open.
func openBackgroundTestDocument(t *testing.T) (*App, string) {
	t.Helper()
	path := testpdf.WritePages(t, []string{"one"}, []string{"two"}, []string{"three"})
	app := luaUITestApp(t, "")
	evalLua(t, app, `
doc = onon.load_document(`+strconv.Quote(path)+`, function(r) result = r end)
assert(doc:state() == "opening" or doc:state() == "open")
assert(doc.path == `+strconv.Quote(path)+`)`)
	pollBackgroundUntil(t, app, func() bool { return luaHolds(app, "result ~= nil") })
	evalLua(t, app, `
assert(result.success and result.error == "" and result.page_count == 3, "result")
assert(doc:state() == "open" and doc:page_count() == 3 and doc:error() == "")`)
	return app, path
}

func TestBackgroundDocumentOpensWithoutATabAndAnswersAboutItself(t *testing.T) {
	app, _ := openBackgroundTestDocument(t)
	if len(app.tabList()) != 1 || app.docPath != "" {
		t.Fatalf("a background document opened a tab: %d tabs, document %q", len(app.tabList()), app.docPath)
	}
	evalLua(t, app, `
local metadata = doc:metadata()
assert(metadata.format:match("^PDF"), metadata.format)
assert(#doc:outline() == 0)
local info = doc:page_info(2)
assert(info.page == 2 and info.width > 0 and info.height > 0)
assert(not pcall(doc.page_info, doc, 4), "a page it does not have")
doc:page_text(2, function(r) text = r end)
doc:page_links(1, function(r) links = r end)`)
	pollBackgroundUntil(t, app, func() bool { return luaHolds(app, "text ~= nil and links ~= nil") })
	evalLua(t, app, `
assert(text.success and text.page == 2 and text.text:match("two"), text.text)
assert(links.success and #links.links == 0)`)

	evalLua(t, app, `doc:close(); assert(doc:state() == "closed" and doc:page_count() == 0)
assert(not pcall(doc.metadata, doc), "a closed document answers")`)
	if len(app.windowGroup().backgroundDocuments().open) != 0 {
		t.Fatal("close kept the document")
	}
}

func TestBackgroundDocumentPagesDrawAsTheyRender(t *testing.T) {
	app, _ := openBackgroundTestDocument(t)
	evalLua(t, app, `
view = onon.ui.create({
  draw = function(canvas)
    local w, h = canvas:document_page(doc, 2, 0, 0, 100)
    local mw, mh = canvas:document_page(doc, 9, 0, 0, 100)
    size = string.format("%gx%.1f %gx%g", w, h, mw, mh)
  end,
})
view:show()`)
	view := app.activeUIView()
	drawLuaView(t, app, view)
	evalLua(t, app, `assert(size == "100x141.4 0x0", size)`) // A4's shape, until its size is known

	d := app.windowGroup().backgroundDocuments().open[1]
	pollBackgroundUntil(t, app, func() bool {
		drawLuaView(t, app, view)
		page := d.pages.kept[backgroundPageKey{app, 1}]
		return page != nil && page.scale > 0
	})
	bounds := d.pages.bounds[1]
	want := 100 * float64(bounds.Y1-bounds.Y0) / float64(bounds.X1-bounds.X0)
	evalLua(t, app, `assert(size == string.format("100x%.1f 0x0", `+strconv.FormatFloat(want, 'f', -1, 64)+`), size)`)
	if len(d.pages.kept) != 1 {
		t.Fatalf("pages kept = %d, want only the one drawn", len(d.pages.kept))
	}
	evalLua(t, app, `doc:close()`)
	drawLuaView(t, app, view) // a closed document draws nothing
	evalLua(t, app, `assert(size == "0x0.0 0x0", size)`)
}

func TestBackgroundDocumentReportsAFileItCannotOpen(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `doc = onon.load_document(`+strconv.Quote(t.TempDir()+"/missing.pdf")+`, function(r) result = r end)`)
	pollBackgroundUntil(t, app, func() bool { return luaHolds(app, "result ~= nil") })
	evalLua(t, app, `
assert(not result.success and result.error ~= "" and result.page_count == 0, "result")
assert(doc:state() == "failed" and doc:error() == result.error)
assert(not pcall(doc.metadata, doc), "a failed document answers")`)
	if _, err := app.runtime.Eval(`onon.load_document("ssh://host/file.pdf")`); err == nil {
		t.Fatal("a remote document opened in the background")
	}
}

func TestBackgroundDocumentsCloseTheOldestBeyondTheLimitAndOnReload(t *testing.T) {
	path := testpdf.Write(t, "page")
	app := luaUITestApp(t, "")
	evalLua(t, app, `
docs = {}
for i = 1, `+strconv.Itoa(maxBackgroundDocuments+1)+` do docs[i] = onon.load_document(`+strconv.Quote(path)+`) end`)
	if got := len(app.windowGroup().backgroundDocuments().open); got != maxBackgroundDocuments {
		t.Fatalf("documents open = %d, want %d", got, maxBackgroundDocuments)
	}
	evalLua(t, app, `assert(docs[1]:state() == "closed" and docs[2]:state() ~= "closed")`)
	if err := app.runtime.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := len(app.windowGroup().backgroundDocuments().open); got != 0 {
		t.Fatalf("documents open after a reload = %d", got)
	}
}
