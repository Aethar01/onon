package viewer

import (
	"fmt"
	"testing"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font/basicfont"
)

// benchUIApp is an app drawing into a software renderer 1000 by 800, with
// a 200-page document laid out and the default bindings.
func benchUIApp(b *testing.B) *App {
	b.Helper()
	surface := sdl.CreateSurface(1000, 800, sdl.PixelFormatARGB8888)
	renderer := sdl.CreateSoftwareRenderer(surface)
	if renderer == nil {
		b.Skip("no software renderer")
	}
	b.Cleanup(func() { sdl.DestroyRenderer(renderer); sdl.DestroySurface(surface) })
	app := testLayoutApp(200)
	app.config = config.Default()
	app.config.StatusBarLeft = "{message}"
	app.statusBarShown = true
	app.winW, app.winH = 1000, 800
	app.renderer = renderer
	app.fontFace, app.headingFace = basicfont.Face7x13, basicfont.Face7x13
	app.setKeyBindings(app.config.KeyBindings)
	app.recomputeLayout(app.viewportSize())
	return app
}

// flush has the renderer carry out what a frame drew, untimed: the
// software renderer is not what draws on screen.
func flush(b *testing.B, app *App) {
	b.StopTimer()
	sdl.FlushRenderer(app.renderer)
	b.StartTimer()
}

func benchRows(n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("row %d of the menu, with some text", i)
	}
	return rows
}

func BenchmarkUIMenuFrame(b *testing.B) {
	app := benchUIApp(b)
	app.showCoreList("bench", "Bench", benchRows(2000), nil)
	view := app.activeUIView()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.moveUIViewSelection(view, 1-2*(i/500%2))
		if err := app.drawUIViews(app.renderer); err != nil {
			b.Fatal(err)
		}
		flush(b, app)
	}
}

func BenchmarkUIMenuPointer(b *testing.B) {
	app := benchUIApp(b)
	app.showCoreList("bench", "Bench", benchRows(2000), nil)
	rect, _ := app.activeUIView().frameGeometry(app)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		y := rect.Y + 60 + float32(i%200)
		app.handleSDLMouseMotion(&sdl.MouseMotionEvent{X: rect.X + 40, Y: y})
	}
}

func BenchmarkUIMenuKeys(b *testing.B) {
	app := benchUIApp(b)
	app.showCoreList("bench", "Bench", benchRows(2000), nil)
	down := &sdl.KeyboardEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventKeyDown}, Key: sdl.KeycodeDown}
	up := &sdl.KeyboardEvent{CommonEvent: sdl.CommonEvent{Type: sdl.EventKeyDown}, Key: sdl.KeycodeUp}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i/500%2 == 0 {
			app.handleSDLKeyDown(down)
		} else {
			app.handleSDLKeyDown(up)
		}
	}
}

func BenchmarkUIKeybindMenuFrame(b *testing.B) {
	app := benchUIApp(b)
	app.toggleKeybindMenu()
	view := app.activeUIView()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.moveUIViewSelection(view, 1-2*(i/40%2))
		if err := app.drawUIViews(app.renderer); err != nil {
			b.Fatal(err)
		}
		flush(b, app)
	}
}

func BenchmarkUIStatusPromptFrame(b *testing.B) {
	app := benchUIApp(b)
	app.mode = modeCommand
	app.input.Set("open some/long/path/to/a/document/somewhere.pdf")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.frameStatus = nil
		if err := app.drawStatusBar(app.renderer); err != nil {
			b.Fatal(err)
		}
		flush(b, app)
	}
}

func BenchmarkUIDocumentPointer(b *testing.B) {
	app := benchUIApp(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.handleSDLMouseMotion(&sdl.MouseMotionEvent{X: float32(100 + i%800), Y: float32(100 + i%600)})
	}
}

func BenchmarkUIViewportSize(b *testing.B) {
	app := benchUIApp(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.viewportSize()
	}
}

func BenchmarkUIPagePointAtScreen(b *testing.B) {
	app := benchUIApp(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = app.pagePointAtScreen(float64(400+i%100), float64(100+i%500))
	}
}

func BenchmarkUIPrefetch(b *testing.B) {
	app := benchUIApp(b)
	app.renderPending = map[tileKey]renderRequest{}
	app.renderBaseScale = 1
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.scrollY = float64((i % len(app.rows)) * 210)
		app.prefetchVisiblePages()
	}
}

// A plugin's docked panel drawing text, boxes and regions through Lua on
// every frame, as a sidebar does.
func BenchmarkUILuaPanelFrame(b *testing.B) {
	app := benchUIApp(b)
	runtime, err := config.Open(b.TempDir()+"/config.lua", "")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(runtime.Close)
	app.runtime = runtime
	runtime.AttachHost(app)
	if _, err := runtime.Eval(`
onon.ui.create({
  dock = "left", width = 220,
  draw = function(canvas, box)
    for i = 1, 20 do
      local y = (i - 1) * 24
      if canvas:region(i, 8, y, box.w - 16, 22) then canvas:rect(8, y, box.w - 16, 22, "selection", 4) end
      canvas:text(14, y + 4, "Bookmark " .. i, "foreground", { width = box.w - 28 })
    end
  end,
}):show()`); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := app.drawUIViews(app.renderer); err != nil {
			b.Fatal(err)
		}
		flush(b, app)
	}
}
