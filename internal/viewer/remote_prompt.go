package viewer

import (
	"context"

	"onon/internal/remote"
)

// Questions from remote documents arrive from the remote package and are
// asked in the status bar, one at a time: ssh's while connecting, such as for
// a password or to accept a new host key, and a web server's login. A
// question goes away unanswered if what asked it ends first, as when Esc
// cancels the fetch.

type remotePrompt struct {
	ctx    context.Context
	prompt remote.Prompt
	reply  chan remoteAnswer // buffered: answered once, perhaps after askRemote gave up
}

type remoteAnswer struct {
	text string
	ok   bool
}

// askRemote is the remote.Prompter. It runs away from the event loop and
// waits there for the answer.
func (a *App) askRemote(ctx context.Context, p remote.Prompt) (string, bool) {
	q := &remotePrompt{ctx: ctx, prompt: p, reply: make(chan remoteAnswer, 1)}
	select {
	case a.remotePrompts <- q:
	case <-ctx.Done():
		return "", false
	}
	a.wakeLoop()
	select {
	case answer := <-q.reply:
		return answer.text, answer.ok
	case <-ctx.Done():
		a.wakeLoop() // to take the question down
		return "", false
	}
}

func (a *App) pollRemotePrompts() {
	if q := a.remotePrompt; q != nil {
		if q.ctx.Err() != nil {
			a.closeAllUI() // cancels the prompt, which forgets q
			a.remotePrompt = nil
			a.pendingRedraw = true
		}
		return
	}
	if a.mode != modeNormal {
		return // asked once what is being typed is done with
	}
	select {
	case q := <-a.remotePrompts:
		if q.ctx.Err() == nil {
			a.showRemotePrompt(q)
			a.pendingRedraw = true
		}
	default:
	}
}

func (a *App) showRemotePrompt(q *remotePrompt) {
	p := q.prompt
	if p.Kind == remote.PromptNotice {
		a.message = p.Text
		q.reply <- remoteAnswer{ok: true}
		return
	}
	label := p.Text
	if p.Kind == remote.PromptConfirm {
		label += " (Enter allows, Esc refuses)"
	}
	a.askPrompt(label, p.Initial, func(text string) {
		a.remotePrompt = nil
		q.reply <- remoteAnswer{text: text, ok: true}
	})
	a.promptState.masked = p.Kind == remote.PromptSecret
	a.promptState.cancel = func() {
		a.remotePrompt = nil
		q.reply <- remoteAnswer{}
	}
	a.remotePrompt = q
	a.raiseWindow()
}
