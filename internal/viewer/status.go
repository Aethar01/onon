package viewer

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

// statusLayout is where the status bar goes: the status element, along
// the bottom of the window less its margin, holding a box for each side.
type statusLayout struct {
	bar         sdl.FRect // the status bar as a whole
	left, right string    // the text of each side, fitted to the space
	leftArea    sdl.FRect // behind the left text; empty when it has none
	rightArea   sdl.FRect // behind the right text; empty when it has none
	textX       int       // where the left text starts
	textEnd     int       // where the left text must end
	rightX      int       // where the right text starts
	baseline    int
}

// statusLayout lays out the status bar. While a frame is drawn it is laid
// out once, for the bar and the prompt's cursor and completion alike.
func (a *App) statusLayout() statusLayout {
	if a.frameStatus != nil {
		return *a.frameStatus
	}
	l := a.layoutStatus()
	if a.drawing {
		a.frameStatus = &l
	}
	return l
}

func (a *App) layoutStatus() statusLayout {
	status, leftStyle, rightStyle := a.style(config.ElementStatus), a.style(config.ElementStatusLeft), a.style(config.ElementStatusRight)
	h := a.statusBarHeight()
	_, marginRight, marginBottom, marginLeft := a.insets(status.Margin.V)
	_, leftPadR, _, leftPadL := a.insets(leftStyle.Padding.V)
	_, rightPadR, _, rightPadL := a.insets(rightStyle.Padding.V)
	gap := a.ipx(status.Gap.V)
	x0, x1 := int(math.Round(float64(marginLeft))), a.winW-int(math.Round(float64(marginRight)))
	y := a.winH - int(math.Round(float64(marginBottom))) - h
	l := statusLayout{bar: sdl.FRect{X: float32(x0), Y: float32(y), W: float32(x1 - x0), H: float32(h)}}
	input := a.mode != modeNormal
	pads := int(leftPadL + leftPadR + rightPadL + rightPadR)
	l.left, l.right = fitStatusText(a.fontFace, a.formatStatusBar(a.config.StatusBarLeft), a.formatStatusBar(a.config.StatusBarRight), x1-x0-pads, gap, input)
	rightW := 0
	if l.right != "" {
		rightW = measureText(a.fontFace, l.right) + int(rightPadL+rightPadR)
		l.rightArea = sdl.FRect{X: float32(x1 - rightW), Y: float32(y), W: float32(rightW), H: float32(h)}
		l.rightX = x1 - rightW + int(rightPadL)
	}
	leftW := 0
	switch {
	case input && rightW > 0:
		leftW = x1 - x0 - rightW - gap
	case input:
		leftW = x1 - x0
	case l.left != "":
		leftW = measureText(a.fontFace, l.left) + int(leftPadL+leftPadR)
	}
	if leftW > 0 {
		l.leftArea = sdl.FRect{X: float32(x0), Y: float32(y), W: float32(leftW), H: float32(h)}
	}
	l.textX, l.textEnd = x0+int(leftPadL), x0+leftW-int(leftPadR)
	l.baseline = y + a.statusBaselineOffset(h)
	return l
}

// statusReservedHeight is the height the status bar takes from the page
// view: none when it floats over the page.
func (a *App) statusReservedHeight() int {
	status := a.style(config.ElementStatus)
	if status.Floating.V {
		return 0
	}
	_, _, marginBottom, _ := a.insets(status.Margin.V)
	return a.statusBarHeight() + int(math.Round(float64(marginBottom)))
}

// statusTop is the top of the status bar, which completion opens above.
func (a *App) statusTop() int {
	return int(a.statusLayout().bar.Y)
}

func (a *App) statusBaselineOffset(h int) int {
	m := a.fontFace.Metrics()
	return (h + m.Ascent.Ceil() - m.Descent.Ceil()) / 2
}

func (a *App) drawStatusBar(renderer *sdl.Renderer) error {
	l := a.statusLayout()
	status, leftStyle, rightStyle := a.style(config.ElementStatus), a.style(config.ElementStatusLeft), a.style(config.ElementStatusRight)
	a.drawBox(renderer, &status, l.bar)
	// The sides grow and shrink with their text, the left from the bar's
	// left edge and the right from its right.
	tr := a.config.Theme.Motion.Prompt
	leftW := float32(a.animate(tweenKey{kind: "status left"}, float64(l.leftArea.W), tr))
	rightW := float32(a.animate(tweenKey{kind: "status right"}, float64(l.rightArea.W), tr))
	leftBox := sdl.FRect{X: l.bar.X, Y: l.bar.Y, W: leftW, H: l.bar.H}
	rightBox := sdl.FRect{X: l.bar.X + l.bar.W - rightW, Y: l.bar.Y, W: rightW, H: l.bar.H}
	a.drawBox(renderer, &leftStyle, leftBox)
	a.drawBox(renderer, &rightStyle, rightBox)
	// The left text scrolls with a long prompt, so it is clipped to its
	// side, and to the side's box while that grows; the cursor's width
	// past the end is kept visible. Text fading out keeps its room.
	_, padRight, _, _ := a.insets(leftStyle.Padding.V)
	end := float32(l.textEnd)
	if l.left == "" && a.motion.fadingText != "" {
		end = l.bar.X + l.bar.W - padRight
	}
	if boxDrawn(&leftStyle) {
		end = min(end, leftBox.X+leftBox.W-padRight)
	}
	textBox := sdl.FRect{X: float32(l.textX), Y: l.bar.Y, W: end - float32(l.textX) + a.hairline(), H: l.bar.H}
	err := a.withClip(renderer, textBox, func() error {
		// A prompt's text is laid out once for its selection, its
		// composition and its cursor.
		var layout textLayout
		if a.mode != modeNormal {
			layout = a.statusTextLayout(l)
			a.drawTextSelection(renderer, &layout, &a.input)
		}
		if err := a.drawStatusLeft(renderer, l, &leftStyle, &layout); err != nil {
			return err
		}
		if a.mode != modeNormal {
			display, _ := a.inputDisplay()
			a.drawTextCursor(renderer, &layout, &a.input, tweenKey{kind: "cursor", text: display})
		}
		return nil
	})
	if err != nil {
		return err
	}
	drawRight := func() error {
		return a.drawText(renderer, l.right, l.rightX, l.baseline, a.textColor(&rightStyle, false))
	}
	if boxDrawn(&rightStyle) {
		return a.withClip(renderer, rightBox, drawRight)
	}
	return drawRight()
}

// boxDrawn reports whether st draws anything for its box, so that what
// is inside the box should stay inside it.
func boxDrawn(st *config.Style) bool {
	return st.Fill.V.Alpha > 0 || st.BorderWidth.V > 0 || st.Shadow.V.N > 0 || st.Draw.V != nil
}

// drawStatusLeft draws the left text; while a prompt is open, its prefix,
// such as : or /, is drawn in the accent colour.
func (a *App) drawStatusLeft(renderer *sdl.Renderer, l statusLayout, st *config.Style, layout *textLayout) error {
	if a.mode == modeNormal {
		return a.drawStatusMessage(renderer, l, st)
	}
	a.motion.fadingText = ""
	fg := a.textColor(st, false)
	x := a.promptOrigin() - a.promptStart()
	prompt := a.style(config.ElementPrompt)
	before, after, found := strings.Cut(a.config.StatusBarLeft, "{message}")
	if !found {
		return a.drawText(renderer, l.left, x, l.baseline, fg)
	}
	prefix := a.inputPrefix()
	display, _ := a.inputDisplay()
	display, composeStart, composeLen := a.compose(&a.input, display)
	parts := []struct {
		text string
		clr  color.RGBA
	}{
		{a.formatStatusBar(before), fg},
		{prefix, a.textColor(&prompt, false)},
		{display, fg},
		{a.formatStatusBar(after), fg},
	}
	for _, part := range parts {
		if part.text == "" {
			continue
		}
		if err := a.drawText(renderer, part.text, x, l.baseline, part.clr); err != nil {
			return err
		}
		x += measureText(a.fontFace, part.text)
	}
	a.drawComposition(renderer, layout, composeStart, composeLen, st.Text.V)
	return nil
}

// statusTextLayout is the status bar prompt's text as drawn, after its
// prefix, scrolled with it.
func (a *App) statusTextLayout(l statusLayout) textLayout {
	display, _ := a.inputDisplay()
	display, _, _ = a.compose(&a.input, display)
	top, bottom := a.statusTextSpan(l)
	return textLayout{
		face:       a.fontFace,
		lines:      layoutLines(a.fontFace, display, 0, false),
		x:          float32(a.promptOrigin() + measureText(a.fontFace, a.inputPrefix())),
		y:          float32(top),
		lineHeight: bottom - top,
	}
}

// drawStatusMessage draws the left text outside a prompt: a new message
// fades in, and the last text fades out once there is none.
func (a *App) drawStatusMessage(renderer *sdl.Renderer, l statusLayout, st *config.Style) error {
	m := &a.motion
	tr := a.config.Theme.Motion.Message
	text, opacity := l.left, 1.0
	switch {
	case text != "":
		m.fadingText = text
		opacity = a.animateFrom(tweenKey{kind: "message in", text: a.message}, 0, 1, tr)
	case m.fadingText != "":
		text, opacity = m.fadingText, a.animateFrom(tweenKey{kind: "message out", text: m.fadingText}, 1, 0, tr)
		if opacity <= 0 {
			m.fadingText = ""
		}
	}
	if text == "" || opacity <= 0 {
		return nil
	}
	return a.faded(opacity, func() error { return a.drawText(renderer, text, l.textX, l.baseline, a.textColor(st, false)) })
}

// promptStart is how far into the left status text the prompt begins: the
// width of whatever the template shows before {message}.
func (a *App) promptStart() int {
	before, _, found := strings.Cut(a.config.StatusBarLeft, "{message}")
	if !found {
		return 0
	}
	return measureText(a.fontFace, a.formatStatusBar(before))
}

// inputDisplay returns the input as shown, masked for passwords, and the
// part of it before the cursor.
func (a *App) inputDisplay() (display, left string) {
	display, left = a.input.Value, a.input.Left()
	if a.inputMasked() {
		display, left = maskText(display), maskText(left)
	}
	return display, left
}

// promptOrigin is the x at which the prompt starts. Long input scrolls left,
// keeping its start hidden, so the cursor stays on screen.
func (a *App) promptOrigin() int {
	l := a.statusLayout()
	start := l.textX + a.promptStart()
	display, left := a.inputDisplay()
	prefix := a.inputPrefix()
	prefixWidth := measureText(a.fontFace, prefix)
	cursor := start + measureText(a.fontFace, prefix+left)
	limit := l.textEnd - measureText(a.fontFace, "  ") // leave room past the cursor
	end := start + measureText(a.fontFace, prefix+display)
	switch {
	case cursor-a.inputScroll > limit:
		a.inputScroll = cursor - limit
	case cursor-a.inputScroll < start+prefixWidth: // keep a prefix's width of context
		a.inputScroll = cursor - start - prefixWidth
	}
	a.inputScroll = clampInt(a.inputScroll, 0, max(0, end-limit))
	return start - a.inputScroll
}

// statusTextSpan is the top and bottom of the status text's glyphs.
func (a *App) statusTextSpan(l statusLayout) (int, int) {
	m := a.fontFace.Metrics()
	return l.baseline - m.Ascent.Ceil(), l.baseline + m.Descent.Ceil()
}

// fitStatusText keeps the two sides of the status bar from overlapping in
// width. Each side is truncated towards a fair share of the space; while an
// input prompt is on the left it stays whole, and the right side is hidden
// if the two would collide.
func fitStatusText(face font.Face, left, right string, width, gap int, keepLeft bool) (string, string) {
	lw, rw := measureText(face, left), measureText(face, right)
	if right == "" || lw+gap+rw <= width {
		return left, right
	}
	if keepLeft {
		return left, ""
	}
	right = truncateText(face, right, max(width/2, width-gap-lw))
	left = truncateText(face, left, width-gap-measureText(face, right))
	if measureText(face, left)+gap+measureText(face, right) > width {
		return truncateText(face, left, width), "" // too narrow for both
	}
	return left, right
}

// statusBarHeight is the height of the status bar: a line of text and the
// left side's padding above and below it.
func (a *App) statusBarHeight() int {
	top, _, bottom, _ := a.insets(a.style(config.ElementStatusLeft).Padding.V)
	return a.uiLineHeight() + int(math.Round(float64(top+bottom)))
}

// uiLineHeight is the height of a line of UI text.
func (a *App) uiLineHeight() int {
	if a.fontFace == nil {
		return 0
	}
	metrics := a.fontFace.Metrics()
	return max(metrics.Height.Ceil(), metrics.Ascent.Ceil()+metrics.Descent.Ceil())
}

func (a *App) formatStatusBar(template string) string {
	// The template is read once, each token's value worked out only if
	// it is there; the status bar is formatted several times a frame.
	if !strings.ContainsAny(template, "{$") {
		return template
	}
	var b strings.Builder
	b.Grow(len(template) + 32)
	for i := 0; i < len(template); {
		switch c := template[i]; {
		case c == '$' && strings.HasPrefix(template[i:], "$$"):
			b.WriteByte('$')
			i += 2
			continue
		case c == '{':
			if end := strings.IndexByte(template[i:], '}'); end > 0 {
				if value, ok := a.statusToken(template[i+1 : i+end]); ok {
					b.WriteString(value)
					i += end + 1
					continue
				}
			}
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String()
}

// statusToken is the value of the status bar token {name}, and false for
// a name that is none.
func (a *App) statusToken(name string) (string, bool) {
	switch name {
	case "message", "input", "prompt":
		message, input, prompt := a.statusMessage()
		switch name {
		case "input":
			return input, true
		case "prompt":
			return prompt, true
		}
		return message, true
	case "page", "label":
		page, label := a.statusPage()
		if name == "page" {
			return page, true
		}
		return label, true
	case "total":
		return strconv.Itoa(a.pageCount), true
	case "mode":
		return a.renderMode.String(), true
	case "fit":
		return a.fitMode.String(), true
	case "rot":
		return strconv.FormatFloat(a.rotation, 'f', 0, 64), true
	case "zoom":
		return strconv.FormatFloat(a.zoom*100, 'f', 0, 64) + "%", true
	case "dual":
		return boolWord(a.dualPage, "dual", "single"), true
	case "cover":
		return boolWord(a.firstPageOffset, "cover", "flat"), true
	case "search":
		return a.searchStatusCounter(), true
	case "document":
		return a.docName, true
	case "modified":
		return boolWord(a.unsaved, "[+] ", ""), true
	}
	return "", false
}

// statusMessage is the status bar's message, a prompt's text in place of
// it, with the text typed and the search prompt's token.
func (a *App) statusMessage() (message, input, prompt string) {
	message = a.message
	switch a.mode {
	case modeCommand:
		message, input = ":"+a.input.Value, a.input.Value
	case modeGotoPage:
		message, input = " GOTO "+a.input.Value, a.input.Value
	case modeSearch:
		prompt = a.searchPromptToken()
		message, input = prompt+a.input.Value, a.input.Value
	case modePassword:
		message = a.inputPrefix() + maskText(a.input.Value)
		input = message
	case modePrompt:
		message, input = a.inputPrefix()+a.input.Value, a.input.Value
		if a.promptState.masked {
			message = a.inputPrefix() + maskText(a.input.Value)
			input = message
		}
	}
	return message, input, prompt
}

// statusPage is the page shown, or the pages of a spread, as numbers and
// as the document labels them.
func (a *App) statusPage() (page, label string) {
	page, label = strconv.Itoa(a.page+1), a.pageLabel(a.page)
	if a.dualPage && len(a.rows) > 0 && a.page >= 0 && a.page < len(a.pageToRow) {
		row := a.rows[a.pageToRow[a.page]]
		if len(row.pages) >= 2 {
			page = strconv.Itoa(row.pages[0]+1) + "-" + strconv.Itoa(row.pages[len(row.pages)-1]+1)
			label = a.pageLabel(row.pages[0]) + "-" + a.pageLabel(row.pages[len(row.pages)-1])
		}
	}
	return page, label
}

func (a *App) pageLabel(page int) string {
	if page >= 0 && page < len(a.pageMetrics) && a.pageMetrics[page].label != "" {
		return a.pageMetrics[page].label
	}
	return fmt.Sprintf("%d", page+1)
}

func (a *App) inputPrefix() string {
	switch a.mode {
	case modeCommand:
		return ":"
	case modeGotoPage:
		return " GOTO "
	case modeSearch:
		return a.searchPromptToken()
	case modePassword:
		return " Password: "
	case modePrompt:
		return " " + a.promptState.label + ": "
	default:
		return ""
	}
}

func (a *App) searchPromptToken() string {
	if a.searchInput == searchModeBackward {
		return "?"
	}
	return "/"
}
