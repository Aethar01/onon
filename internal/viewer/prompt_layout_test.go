package viewer

import (
	"testing"

	"onon/internal/config"
)

func TestPromptsInTheBarByDefault(t *testing.T) {
	for _, style := range []string{"bar", "pill"} {
		app := testStyleApp(style)
		app.mode = modeCommand
		app.input.Set("open")
		want := app.statusLayout()
		// Naming the bar is the same as leaving the prompt out.
		app.config.Prompts = map[string]config.PromptPlacement{"command": {}}
		l := app.statusLayout()
		if l != want {
			t.Fatalf("%s: command = \"bar\" laid out %+v, want %+v", style, l, want)
		}
		p := app.promptLayout()
		if !p.inBar || p.box != l.leftArea || p.textX != l.textX || p.textEnd != l.textEnd || p.baseline != l.baseline {
			t.Fatalf("%s: prompt %+v, want the bar's left %+v", style, p, l)
		}
	}
}

func TestPromptInABoxOfItsOwn(t *testing.T) {
	app := testStyleApp("bar")
	app.statusBarShown = false
	app.message = "saved"
	app.config.Prompts = map[string]config.PromptPlacement{"command": {Anchor: "center"}}
	app.mode = modeCommand
	app.input.Set("open")
	if app.statusVisible() {
		t.Fatal("a prompt in a box showed the hidden status bar")
	}
	p := app.promptLayout()
	if p.inBar || p.box.W != 400 || int(p.box.X) != 200 || p.box.Y+p.box.H/2 < 290 || p.box.Y+p.box.H/2 > 310 {
		t.Fatalf("command box %+v, want half the window wide in its middle", p.box)
	}
	if p.textX <= int(p.box.X) || p.textEnd >= int(p.box.X+p.box.W) || p.baseline <= int(p.box.Y) || p.baseline >= int(p.box.Y+p.box.H) {
		t.Fatalf("text at %d to %d on %d, want it inside %+v", p.textX, p.textEnd, p.baseline, p.box)
	}
	if got := app.promptOrigin(); got != p.textX {
		t.Fatalf("prompt starts at %d, want the box's text at %d", got, p.textX)
	}
	if pos, ok := app.inputPositionAt(float64(p.textX+measureText(app.fontFace, ":op")), float64(p.box.Y+p.box.H/2), false); !ok || pos != 2 {
		t.Fatalf("click in the box = %d, %v; want rune 2", pos, ok)
	}

	// The bar keeps its message while the prompt is in a box.
	app.statusBarShown = true
	if l := app.statusLayout(); l.left != "saved" {
		t.Fatalf("bar shows %q, want the message", l.left)
	}

	// Completion opens below the box.
	app.showCompletion()
	if app.completion.view == nil || !app.completion.view.visible {
		t.Fatal("no completion")
	}
	rect, _ := app.viewGeometry(app.completion.view)
	p = app.promptLayout() // above the bar now shown
	if rect.Y < p.box.Y+p.box.H {
		t.Fatalf("completion at %+v, want it below the box %+v", rect, p.box)
	}

	// Other prompts stay in the bar.
	app.closeCompletion()
	app.mode = modeSearch
	if !app.promptInBar() || !app.promptLayout().inBar {
		t.Fatal("search left the bar")
	}
}

func TestPromptBoxAnchoredToAnEdge(t *testing.T) {
	app := testStyleApp("bar")
	app.config.Prompts = map[string]config.PromptPlacement{"go_to": {Anchor: "top", Width: config.UISize{Value: 200}}}
	app.mode = modeGotoPage
	p := app.promptLayout()
	if p.box.W != 200 || int(p.box.X) != 300 || int(p.box.Y) != app.ipx(12) {
		t.Fatalf("goto box %+v, want 200 wide at the top", p.box)
	}
}
