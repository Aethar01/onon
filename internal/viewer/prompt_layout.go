package viewer

import (
	"math"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// promptLayout is where the open prompt's line of text goes: in the
// status bar, on its left, or in a box of its own where the prompts
// option places it.
type promptLayout struct {
	inBar    bool
	box      sdl.FRect // behind the prompt
	textX    int       // where its line starts
	textEnd  int       // where its line must end
	start    int       // how far into the line the prompt begins
	baseline int
}

// promptName is the name the prompts option gives the open prompt, and
// "" outside one.
func (a *App) promptName() string {
	switch a.mode {
	case modeCommand:
		return "command"
	case modeGotoPage:
		return "go_to"
	case modePassword:
		return "password"
	case modePrompt:
		return "prompt"
	case modeSearch:
		return "search"
	}
	return ""
}

// floatingPrompt is where the open prompt's box sits, and false when no
// prompt is open or it is in the status bar.
func (a *App) floatingPrompt() (config.PromptPlacement, bool) {
	p, ok := a.config.Prompts[a.promptName()]
	return p, ok && p.Anchor != ""
}

// promptInBar reports whether a prompt is open in the status bar, taking
// the place of its left text.
func (a *App) promptInBar() bool {
	_, floating := a.floatingPrompt()
	return a.mode != modeNormal && !floating
}

func (a *App) promptLayout() promptLayout {
	p, floating := a.floatingPrompt()
	if !floating {
		l := a.statusLayout()
		return promptLayout{inBar: true, box: l.leftArea, textX: l.textX, textEnd: l.textEnd, start: a.promptStart(), baseline: l.baseline}
	}
	st := a.style(config.ElementPromptBox)
	top, right, bottom, left := a.insets(st.Padding.V)
	line := a.uiLineHeight()
	h := line + int(math.Round(float64(top+bottom)))
	area := a.documentRect()
	gap := 0
	if p.Anchor != "center" {
		gap = a.ipx(12)
	}
	width := p.Width
	if width.Value == 0 {
		width = config.UISize{Value: 50, Percent: true}
	}
	areaW := max(1, int(area.W)-2*gap)
	least := 1
	if width.Percent {
		least = min(a.ipx(320), areaW) // a share of a small window still holds a command
	}
	w := clampInt(int(math.Round(a.uiLength(width, float64(a.winW)))), least, areaW)
	x, y := anchorIn(area, p.Anchor, w, h, gap)
	return promptLayout{
		box:      sdl.FRect{X: float32(x), Y: float32(y), W: float32(w), H: float32(h)},
		textX:    x + int(math.Round(float64(left))),
		textEnd:  x + w - int(math.Round(float64(right))),
		baseline: y + int(math.Round(float64(top))) + a.statusBaselineOffset(line),
	}
}

// drawPromptBox draws a prompt the prompts option places in a box of its
// own.
func (a *App) drawPromptBox(renderer *sdl.Renderer) error {
	p := a.promptLayout()
	st := a.style(config.ElementPromptBox)
	a.drawBox(renderer, &st, p.box)
	// Long input scrolls in the box, its cursor kept in sight.
	clip := sdl.FRect{X: float32(p.textX), Y: p.box.Y, W: float32(p.textEnd-p.textX) + a.hairline(), H: p.box.H}
	return a.withClip(renderer, clip, func() error { return a.drawPromptText(renderer, p, &st) })
}
