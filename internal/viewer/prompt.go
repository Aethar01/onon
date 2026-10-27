package viewer

// A prompt asks one question in the status bar and passes the answer, taken
// exactly as typed, to a callback when Enter is pressed.

type promptState struct {
	label  string
	answer func(string)
	masked bool   // the answer is a secret, shown as stars
	cancel func() // called if the prompt is closed without an answer
}

func (a *App) askPrompt(label, initial string, answer func(string)) {
	a.closeAllUI()
	a.promptState = promptState{label: label, answer: answer}
	a.mode = modePrompt
	a.input.Set(initial)
}

// cancelPrompt forgets the prompt without an answer.
func (a *App) cancelPrompt() {
	cancel := a.promptState.cancel
	a.promptState = promptState{}
	if cancel != nil {
		cancel()
	}
}

// inputMasked reports whether what is typed is a secret.
func (a *App) inputMasked() bool {
	return a.mode == modePassword || a.mode == modePrompt && a.promptState.masked
}

func (a *App) answerPrompt(value string) {
	answer := a.promptState.answer
	a.promptState = promptState{}
	if answer != nil {
		answer(value)
	}
}
