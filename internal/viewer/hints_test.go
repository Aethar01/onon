package viewer

import (
	"slices"
	"testing"

	"onon/internal/mupdf"
)

func TestHintLabelsAreUniqueAndShortest(t *testing.T) {
	if got := hintLabels(3, "ab"); !slices.Equal(got, []string{"aa", "ab", "ba"}) {
		t.Fatalf("hintLabels(3, ab) = %v", got)
	}
	if got := hintLabels(2, "asd"); !slices.Equal(got, []string{"a", "s"}) {
		t.Fatalf("hintLabels(2, asd) = %v", got)
	}
	// Duplicate and too few characters fall back sensibly.
	if got := hintLabels(2, "aa"); len(got[0]) != 1 || got[0] == got[1] {
		t.Fatalf("hintLabels with a one-letter alphabet = %v", got)
	}
}

func TestLinkHintsFollowTypedLabel(t *testing.T) {
	app := testLayoutApp(3)
	app.config.HintChars = "ab"
	app.winW, app.winH = 1000, 1000
	app.recomputeLayout(app.viewportSize())
	app.pageLinks = map[int][]mupdf.Link{0: {
		{Bounds: mupdf.Rect{X1: 50, Y1: 10}, Page: 1},
		{Bounds: mupdf.Rect{Y0: 20, X1: 50, Y1: 30}, Page: 2},
		{Bounds: mupdf.Rect{Y0: 40, X1: 50, Y1: 50}, Page: 1},
	}, 1: nil, 2: nil}
	app.startLinkHints()
	if app.hints == nil || len(app.hints.hints) < 3 {
		t.Fatalf("hints = %+v", app.hints)
	}
	app.handleHintToken("z") // not a hint character: ignored
	app.handleHintToken("a")
	app.handleHintToken("<BS>")
	app.handleHintToken("A") // labels match in either case
	if app.hints == nil || app.hints.typed != "a" {
		t.Fatalf("typed = %+v", app.hints)
	}
	app.handleHintToken("B") // "ab" is the second link, to page 2
	if app.hints != nil {
		t.Fatal("hints still shown after a full label")
	}
	if app.page != 2 {
		t.Fatalf("page = %d, want 2", app.page)
	}

	app.startLinkHints()
	app.handleHintToken("<Esc>")
	if app.hints != nil {
		t.Fatal("Esc did not cancel hints")
	}
}

func TestHintLabelsSitClearOfTheirLink(t *testing.T) {
	if box := hintBox(100, 100, 114, 30, 20, 1000, 800, 1); box.X != 96 || box.Y != 80 {
		t.Fatalf("label at %v, want directly above the link and nudged left", box)
	}
	if box := hintBox(100, 100, 128, 60, 40, 2000, 1600, 2); box.X != 92 || box.Y != 60 {
		t.Fatalf("label at %v, want the nudge scaled with the UI", box)
	}
	if box := hintBox(1, 5, 19, 30, 20, 1000, 800, 1); box.X != 2 || box.Y != 19 {
		t.Fatalf("label at %v, want flipped below a link at the top edge", box)
	}
	if box := hintBox(990, 795, 800, 30, 20, 1000, 800, 1); box.X != 968 || box.Y != 775 {
		t.Fatalf("label at %v, want kept inside the bottom-right edge", box)
	}
}
