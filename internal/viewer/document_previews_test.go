package viewer

import (
	"strconv"
	"testing"
	"time"

	"onon/internal/testpdf"
)

// pollPreviewsUntil runs the window's polls until ready says so.
func pollPreviewsUntil(t *testing.T, app *App, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.runtime.PollPluginOperations()
		app.pollDocumentPreviews()
		if ready() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for the preview")
}

func luaBool(t *testing.T, app *App, expression string) bool {
	t.Helper()
	_, err := app.runtime.Eval("assert(" + expression + ")")
	return err == nil
}

func TestDocumentPreviewOpensAndDrawsPagesWithoutATab(t *testing.T) {
	path := testpdf.WritePages(t, []string{"one"}, []string{"two"}, []string{"three"})
	app := luaUITestApp(t, "")
	evalLua(t, app, `
doc = onon.config:open_document(`+strconv.Quote(path)+`, function(r) result = r end)
assert(doc:state() == "opening" or doc:state() == "open")
assert(doc.path == `+strconv.Quote(path)+`)`)
	pollPreviewsUntil(t, app, func() bool { return luaBool(t, app, "result ~= nil") })
	evalLua(t, app, `
assert(result.success and result.error == "" and result.page_count == 3, "result")
assert(doc:state() == "open" and doc:page_count() == 3 and doc:error() == "")`)
	if len(app.tabList()) != 1 || app.docPath != "" {
		t.Fatalf("a preview opened a tab: %d tabs, document %q", len(app.tabList()), app.docPath)
	}

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

	preview := app.windowGroup().previewState().previews[1]
	pollPreviewsUntil(t, app, func() bool {
		drawLuaView(t, app, view)
		page := preview.pages[previewKey{app, 1}]
		return page != nil && page.scale > 0
	})
	bounds := preview.bounds[1]
	want := 100 * float64(bounds.Y1-bounds.Y0) / float64(bounds.X1-bounds.X0)
	evalLua(t, app, `assert(size == string.format("100x%.1f 0x0", `+strconv.FormatFloat(want, 'f', -1, 64)+`), size)`)
	if len(preview.pages) != 1 {
		t.Fatalf("pages kept = %d, want only the one drawn", len(preview.pages))
	}

	evalLua(t, app, `doc:close(); assert(doc:state() == "closed" and doc:page_count() == 0)`)
	if len(app.windowGroup().previewState().previews) != 0 {
		t.Fatal("close kept the preview")
	}
	drawLuaView(t, app, view) // drawing a closed preview draws nothing
	evalLua(t, app, `assert(size == "0x0.0 0x0", size)`)
}

func TestDocumentPreviewReportsAFileItCannotOpen(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `doc = onon.config:open_document(`+strconv.Quote(t.TempDir()+"/missing.pdf")+`, function(r) result = r end)`)
	pollPreviewsUntil(t, app, func() bool { return luaBool(t, app, "result ~= nil") })
	evalLua(t, app, `
assert(not result.success and result.error ~= "" and result.page_count == 0, "result")
assert(doc:state() == "failed" and doc:error() == result.error)`)
	if _, err := app.runtime.Eval(`onon.config:open_document("ssh://host/file.pdf")`); err == nil {
		t.Fatal("a remote document opened as a preview")
	}
}

func TestDocumentPreviewsCloseTheOldestBeyondTheLimitAndOnReload(t *testing.T) {
	path := testpdf.Write(t, "page")
	app := luaUITestApp(t, "")
	evalLua(t, app, `
docs = {}
for i = 1, `+strconv.Itoa(maxPreviews+1)+` do docs[i] = onon.config:open_document(`+strconv.Quote(path)+`) end`)
	if got := len(app.windowGroup().previewState().previews); got != maxPreviews {
		t.Fatalf("previews open = %d, want %d", got, maxPreviews)
	}
	evalLua(t, app, `assert(docs[1]:state() == "closed" and docs[2]:state() ~= "closed")`)
	if err := app.runtime.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := len(app.windowGroup().previewState().previews); got != 0 {
		t.Fatalf("previews open after a reload = %d", got)
	}
}
