package viewer

import (
	"testing"

	"onon/internal/config"

	"golang.org/x/image/font/basicfont"
)

func TestFormatStatusBarReplacesTemplateTokens(t *testing.T) {
	app := &App{
		tab: &tab{
			documentState: documentState{
				page:      2,
				pageCount: 9,
				docName:   "paper.pdf",
			},
			viewStateFields: viewStateFields{renderMode: renderContinuous, fitMode: fitWidth, rotation: 90, zoom: 1.25},
			search:          searchState{query: "term", current: 0, order: []searchHitRef{{page: 2}}},
		},
		config:     config.Default(),
		inputState: inputState{message: "ready"},
	}

	got := app.formatStatusBar("{document} {message} {page}/{total} {mode} {fit} {rot} {zoom} {dual} {cover} {search} $$")
	want := "paper.pdf ready 3/9 continuous width 90 125% single flat [1/1] $"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestFormatStatusBarUsesInputModeMessage(t *testing.T) {
	tests := []struct {
		name  string
		mode  mode
		input string
		back  bool
		tmpl  string
		want  string
	}{
		{name: "command", mode: modeCommand, input: "open file.pdf", tmpl: "{message}|{input}", want: ":open file.pdf|open file.pdf"},
		{name: "goto", mode: modeGotoPage, input: "12", tmpl: "{message}|{input}", want: " GOTO 12|12"},
		{name: "forward search", mode: modeSearch, input: "abc", tmpl: "{message}|{input}|{prompt}", want: "/abc|abc|/"},
		{name: "backward search", mode: modeSearch, input: "abc", back: true, tmpl: "{message}|{input}|{prompt}", want: "?abc|abc|?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{tab: &tab{}, inputState: inputState{mode: tt.mode, input: textInput{Value: tt.input}}}
			if tt.back {
				app.searchInput = searchModeBackward
			}
			if got := app.formatStatusBar(tt.tmpl); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestFormatStatusBarShowsDualPageSpread(t *testing.T) {
	app := &App{
		tab: &tab{
			documentState:   documentState{page: 1, pageCount: 4},
			viewStateFields: viewStateFields{dualPage: true},
			layoutState: layoutState{
				rows: []rowLayout{
					{pages: []int{0}},
					{pages: []int{1, 2}},
					{pages: []int{3}},
				},
				pageToRow: []int{0, 1, 1, 2},
			},
		},
	}

	if got := app.formatStatusBar("{page}/{total}"); got != "2-3/4" {
		t.Fatalf("expected dual-page spread in status, got %q", got)
	}
}

func TestFormatStatusBarUsesPageLabels(t *testing.T) {
	app := testLayoutApp(3)
	app.page = 1
	app.pageMetrics[1].label = "iv"
	if got := app.formatStatusBar("{page}:{label}"); got != "2:iv" {
		t.Fatalf("expected page label in status bar, got %q", got)
	}
}

func TestStatusBarHeightFitsFont(t *testing.T) {
	app := &App{
		tab:         &tab{viewStateFields: viewStateFields{statusBarShown: true}},
		sdlState:    sdlState{fontFace: basicfont.Face7x13},
		windowState: windowState{winW: 800, winH: 600},
	}

	metrics := basicfont.Face7x13.Metrics()
	wantHeight := max(metrics.Height.Ceil(), metrics.Ascent.Ceil()+metrics.Descent.Ceil()) + 4
	if got := app.statusBarHeight(); got != wantHeight {
		t.Fatalf("expected status bar height %d to fit font, got %d", wantHeight, got)
	}
	if _, got := app.viewportSize(); got != app.winH-wantHeight {
		t.Fatalf("expected viewport to reserve expanded status bar height, got %d", got)
	}
}

func TestToggleStatusBarLeavesThePageWhereItIs(t *testing.T) {
	app := testLayoutApp(5)
	app.winW = 800
	app.winH = 600
	app.fontFace = basicfont.Face7x13
	app.recomputeLayout(app.viewportSize())
	app.scrollY = 75
	x0, y0, _ := app.pageScreenOrigin(1)

	for range 2 { // hidden, then shown again
		if err := app.runBuiltinAction("toggle_status_bar"); err != nil {
			t.Fatal(err)
		}
		x, y, ok := app.pageScreenOrigin(1)
		if !ok {
			t.Fatal("expected the page to remain placed")
		}
		assertClose(t, x, x0)
		assertClose(t, y, y0)
	}
}
