package viewer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"onon/internal/config"
	"onon/internal/remote"
)

// Remote documents are fetched into the cache, and saved back, away from
// the event loop, which shows progress until the result arrives. Esc
// cancels. One task runs at a time; starting another cancels it.

type remoteTaskKind int

const (
	remoteOpen remoteTaskKind = iota
	remoteReload
	remoteSave
)

type remoteTask struct {
	kind   remoteTaskKind
	uri    string
	cancel context.CancelFunc
	done   chan remoteResult

	bytesDone  atomic.Int64
	bytesTotal atomic.Int64
	lastWake   atomic.Int64 // unix nanoseconds
	shown      int64        // the bytes the message last showed

	// remoteOpen
	opts     openDocumentOptions
	password string
	// remoteReload
	change documentChange
	// remoteSave, and remoteOpen to tell edits made while it ran
	editPos   int
	quitAfter bool   // :wq, once saved
	other     bool   // saved to another file, which leaves the document as it is
	temp      string // a directory holding the copy uploaded, removed after
}

type remoteResult struct {
	info remote.Info
	err  error
}

// progressWakeInterval spaces out the redraws progress asks for.
const progressWakeInterval = 100 * time.Millisecond

func (a *App) startRemoteTask(task *remoteTask, run func(ctx context.Context, progress func(done, total int64)) (remote.Info, error)) {
	a.cancelRemoteTask()
	ctx, cancel := context.WithCancel(context.Background())
	task.cancel = cancel
	task.done = make(chan remoteResult, 1)
	task.bytesTotal.Store(-1)
	a.remoteTask = task
	progress := func(done, total int64) {
		task.bytesDone.Store(done)
		task.bytesTotal.Store(total)
		now := time.Now().UnixNano()
		if last := task.lastWake.Load(); now-last >= int64(progressWakeInterval) && task.lastWake.CompareAndSwap(last, now) {
			a.wakeLoop()
		}
	}
	go func() {
		info, err := run(ctx, progress)
		task.done <- remoteResult{info: info, err: err}
		a.wakeLoop()
	}()
	a.logf("remote %s started uri=%q", task.verb(), remote.Display(task.uri))
	a.message = task.status()
	a.pendingRedraw = true
}

// cancelRemoteTask stops the task in progress, reporting whether there was one.
func (a *App) cancelRemoteTask() bool {
	task := a.remoteTask
	if task == nil {
		return false
	}
	a.remoteTask = nil
	task.cancel()
	if task.temp != "" {
		go func() {
			<-task.done
			os.RemoveAll(task.temp)
		}()
	}
	a.logf("remote %s cancelled uri=%q", task.verb(), remote.Display(task.uri))
	return true
}

func (a *App) pollRemoteTask() {
	task := a.remoteTask
	if task == nil {
		return
	}
	select {
	case result := <-task.done:
		a.remoteTask = nil
		task.cancel()
		a.finishRemoteTask(task, result)
		a.pendingRedraw = true
	default:
		if done := task.bytesDone.Load(); done != task.shown {
			task.shown = done
			a.message = task.status()
			a.pendingRedraw = true
		}
	}
}

func (a *App) finishRemoteTask(task *remoteTask, result remoteResult) {
	if task.temp != "" {
		os.RemoveAll(task.temp)
	}
	if err := result.err; err != nil {
		a.logf("remote %s failed uri=%q err=%v", task.verb(), remote.Display(task.uri), err)
		if task.kind == remoteReload {
			// Retrying would reach for the host every second; the next change
			// it reports tries again.
			a.document.commit(task.change)
		}
		switch {
		case errors.Is(err, context.Canceled):
		case errors.Is(err, remote.ErrConflict):
			a.message = config.DocumentName(task.uri) + " changed on " + remote.Host(task.uri) + " since it was opened: :w! overwrites it"
		case task.kind == remoteSave:
			a.message = "save to " + remote.Host(task.uri) + " failed: " + err.Error()
		default:
			a.message = err.Error()
		}
		return
	}
	info := result.info
	a.logf("remote %s done uri=%q local=%q", task.verb(), remote.Display(info.URI), info.Local)
	switch task.kind {
	case remoteOpen:
		if a.unsaved && a.editPos != task.editPos {
			// Edited while it downloaded: opening was agreed to before those.
			a.discardWarned = true
			a.message = "kept the edits made while fetching " + config.DocumentName(info.URI) + ": :w saves them, opening it again discards them"
			return
		}
		opts := task.opts
		opts.remote = &info
		if err := a.openDocumentWithPassword(info.URI, opts, task.password); err != nil {
			a.message = err.Error()
		}
	case remoteReload:
		if info.URI != a.docPath {
			return // another document was opened meanwhile
		}
		if a.unsaved {
			// Edited while it downloaded; :w then finds the change and asks
			// for :w! before saving over it.
			a.document.commit(task.change)
			a.message = a.docName + " changed on " + remote.Host(info.URI) + "; keeping unsaved edits"
			return
		}
		if err := a.softReloadDocument(info.URI, info.Local, a.currentViewState()); err != nil {
			a.message = err.Error()
			return
		}
		a.remoteInfo = info
		a.document.commit(task.change)
		a.message = "reloaded " + a.docName
	case remoteSave:
		if !task.other && info.URI == a.docPath {
			a.remoteInfo = info
			a.document.recordRemote(info)
			a.savedPos = task.editPos
			a.setUnsaved(a.editPos != a.savedPos)
		}
		a.message = "wrote " + config.DocumentName(info.URI) + " to " + remote.Host(info.URI)
		if task.quitAfter && !a.unsaved {
			a.quit = true
		}
	}
}

// fetchRemoteDocument fetches uri and then opens it with opts.
func (a *App) fetchRemoteDocument(uri string, opts openDocumentOptions, password string) {
	task := &remoteTask{kind: remoteOpen, uri: uri, opts: opts, password: password, editPos: a.editPos}
	a.startRemoteTask(task, func(ctx context.Context, progress func(done, total int64)) (remote.Info, error) {
		return remote.Fetch(ctx, uri, progress)
	})
}

// refetchRemoteDocument fetches the open document again after it changed.
func (a *App) refetchRemoteDocument(change documentChange) {
	uri := a.docPath
	task := &remoteTask{kind: remoteReload, uri: uri, change: change}
	a.startRemoteTask(task, func(ctx context.Context, progress func(done, total int64)) (remote.Info, error) {
		return remote.Fetch(ctx, uri, progress)
	})
}

// writeRemoteDocument saves the open remote document back where it came
// from, or to target, another remote file. The edits are written to the
// cached copy, or a temporary file for another target, and that is uploaded.
func (a *App) writeRemoteDocument(target string, force bool) {
	if !remote.Writable(target) {
		a.message = "cannot write to " + remote.Display(target) + "; :w <path> saves a local copy"
		return
	}
	if a.remoteTask != nil && a.remoteTask.kind == remoteSave {
		a.message = "already saving " + config.DocumentName(a.remoteTask.uri)
		return
	}
	task := &remoteTask{kind: remoteSave, uri: target, editPos: a.editPos, other: target != a.docPath}
	local, opened := a.docFile, a.remoteInfo
	if task.other {
		dir, err := os.MkdirTemp("", "onon-save-")
		if err != nil {
			a.message = err.Error()
			return
		}
		local = filepath.Join(dir, config.DocumentName(target))
		task.temp = dir
		// Another file is written whatever it holds, as :w path does locally.
		force, opened = true, remote.Info{}
	} else {
		remote.Forget(target, local)
	}
	if err := a.doc.Save(local, a.docFile); err != nil {
		if task.temp != "" {
			os.RemoveAll(task.temp)
		}
		a.message = err.Error()
		return
	}
	a.startRemoteTask(task, func(ctx context.Context, progress func(done, total int64)) (remote.Info, error) {
		return remote.Upload(ctx, target, local, opened, force)
	})
}

func (t *remoteTask) verb() string {
	switch t.kind {
	case remoteSave:
		return "save"
	case remoteReload:
		return "reload"
	}
	return "fetch"
}

// status is the message shown while the task runs.
func (t *remoteTask) status() string {
	name, host := config.DocumentName(t.uri), remote.Host(t.uri)
	text := "fetching " + name + " from " + host
	if t.kind == remoteSave {
		text = "saving " + name + " to " + host
	}
	if done := t.bytesDone.Load(); done > 0 {
		if total := t.bytesTotal.Load(); total > 0 {
			text += fmt.Sprintf(" %s of %s", formatBytes(done), formatBytes(total))
		} else {
			text += " " + formatBytes(done)
		}
	}
	return text + " (Esc cancels)"
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
