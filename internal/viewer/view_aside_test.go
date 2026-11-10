package viewer

import (
	"strings"
	"testing"
)

func TestViewAsideWidensTheViewBesideItsRows(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `
view = onon.ui.create({ title = "Files", width = 400, rows = { "a", "b" } })
view:show()`)
	view := app.activeUIView()
	before, _ := view.frameGeometry(app)

	evalLua(t, app, `
view:set_aside({ width = 200, draw = function(canvas, box, state)
  drawn = string.format("%gx%g", state.w, state.h)
end })`)
	frame, _ := view.frameGeometry(app)
	if frame.W != before.W+200 {
		t.Fatalf("width with an aside = %v, want %v", frame.W, before.W+200)
	}
	if frame.H <= before.H {
		t.Fatalf("a view with an aside kept the height of its two rows: %v", frame.H)
	}
	rows := app.listPart(view, frame)
	aside := app.asideBox(view)
	if rows.X != frame.X || rows.W != frame.W-200 || aside.X < rows.X+rows.W || aside.X+aside.W > frame.X+frame.W {
		t.Fatalf("rows %+v and aside %+v do not share frame %+v", rows, aside, frame)
	}
	if box := app.viewRowBox(view, frame, int(rows.Y), 20); box.X+box.W > rows.X+rows.W {
		t.Fatalf("a row %+v reaches into the aside", box)
	}
	// A click in the aside, level with a row, is not on the row.
	head := app.uiViewHeadHeight(view)
	y := int(frame.Y) + head + app.modalListRowHeight()/2
	if _, ok := app.uiViewIndexAt(view, int(aside.X+aside.W/2), y); ok {
		t.Fatal("the aside hit a row")
	}
	if row, ok := app.uiViewIndexAt(view, int(rows.X+rows.W/2), y); !ok || row.text != "a" {
		t.Fatalf("the rows beside the aside: %v %v", row.text, ok)
	}

	app.drawViewAside(nil, view)
	evalLua(t, app, `assert(drawn, "the aside was not drawn")`)

	evalLua(t, app, `view:set_aside({ side = "left", width = 150, draw = function() end })`)
	frame, _ = view.frameGeometry(app)
	rows = app.listPart(view, frame)
	if rows.X != frame.X+150 || app.asideBox(view).X >= rows.X {
		t.Fatalf("a left aside: frame %+v rows %+v", frame, rows)
	}
	evalLua(t, app, `view:set_aside(nil)`)
	if frame, _ = view.frameGeometry(app); frame != before {
		t.Fatalf("without its aside the view is %+v, not %+v", frame, before)
	}
}

func TestViewAsideFromTheSpecAndItsErrors(t *testing.T) {
	app := luaUITestApp(t, "")
	evalLua(t, app, `onon.ui.create({ rows = { "a" }, aside = { side = "left", draw = function() error("boom") end } }):show()`)
	view := app.activeUIView()
	if view.aside == nil || view.aside.side != "left" || !view.aside.width.Percent || view.aside.width.Value != 40 {
		t.Fatalf("aside from the spec = %+v", view.aside)
	}
	app.drawViewAside(nil, view)
	if !app.themeErrors[view.aside] {
		t.Fatal("a failing aside was not reported")
	}
	for _, bad := range []string{
		`{ side = "top", draw = function() end }`,
		`{ side = "left" }`,
		`{ width = "wide", draw = function() end }`,
		`"right"`,
	} {
		if _, err := app.runtime.Eval(`onon.ui.create({ rows = { "a" }, aside = ` + bad + ` })`); err == nil || !strings.Contains(err.Error(), "aside") {
			t.Errorf("aside %s: err = %v", bad, err)
		}
	}
}
