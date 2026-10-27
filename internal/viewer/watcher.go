package viewer

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"onon/internal/remote"

	"github.com/fsnotify/fsnotify"
)

type documentWatcher struct {
	path    string
	mod     time.Time
	size    int64
	changed chan struct{}
	pending watcherChange
	delay   atomic.Int64 // the time.Duration writes must settle for

	watcher   *fsnotify.Watcher
	done      chan struct{}
	mu        sync.Mutex
	closeOnce sync.Once
	started   bool
}

type watcherChange struct {
	mod  time.Time
	size int64
}

func newDocumentWatcher(path string, delay time.Duration) (*documentWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dw := &documentWatcher{
		path:    path,
		watcher: w,
		done:    make(chan struct{}),
		changed: make(chan struct{}, 1),
	}
	dw.delay.Store(int64(delay))

	dir := filepath.Dir(path)
	if dir == "" {
		dir = "."
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		return nil, err
	}

	return dw, nil
}

func (dw *documentWatcher) record(path string) {
	dw.mu.Lock()
	dw.path = path
	dw.mu.Unlock()

	if info, err := os.Stat(path); err == nil {
		dw.mu.Lock()
		dw.mod = info.ModTime()
		dw.size = info.Size()
		dw.mu.Unlock()
	}

	dw.mu.Lock()
	if !dw.started {
		dw.started = true
		dw.mu.Unlock()
		go dw.loop()
	} else {
		dw.mu.Unlock()
	}
}

func (dw *documentWatcher) loop() {
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}

	for {
		select {
		case <-dw.done:
			debounce.Stop()
			return
		case event, ok := <-dw.watcher.Events:
			if !ok {
				return
			}
			if !dw.isRelevantEvent(event) {
				continue
			}
			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounce.Reset(time.Duration(dw.delay.Load()))
		case <-dw.watcher.Errors:
			// keep running
		case <-debounce.C:
			dw.checkAndNotify()
		}
	}
}

func (dw *documentWatcher) isRelevantEvent(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write) || event.Has(fsnotify.Chmod) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename)
}

func (dw *documentWatcher) checkAndNotify() {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	info, err := os.Stat(dw.path)
	if err != nil || info.IsDir() {
		return
	}

	mod, size := info.ModTime(), info.Size()
	if mod.Equal(dw.mod) && size == dw.size {
		return
	}

	dw.mod = mod
	dw.size = size
	dw.pending = watcherChange{
		mod:  mod,
		size: size,
	}

	select {
	case dw.changed <- struct{}{}:
	default:
	}
}

func (dw *documentWatcher) waitForChange(timeout time.Duration) (watcherChange, bool) {
	if timeout <= 0 {
		select {
		case <-dw.changed:
			dw.mu.Lock()
			change := dw.pending
			dw.pending = watcherChange{}
			dw.mu.Unlock()
			return change, true
		default:
			return watcherChange{}, false
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-dw.changed:
		dw.mu.Lock()
		change := dw.pending
		dw.pending = watcherChange{}
		dw.mu.Unlock()
		return change, true
	case <-timer.C:
		return watcherChange{}, false
	}
}

func (dw *documentWatcher) Close() {
	dw.closeOnce.Do(func() {
		close(dw.done)
		dw.watcher.Close()
	})
}

// remoteWatcher checks a document on an SSH host for changes every
// remote.PollInterval. It asks only over a connection that is already open,
// so a host that went away is not reconnected to, and perhaps prompted for a
// password, in the background.
//
// It checks only while enabled, which follows auto_reload, since a change
// would wait unread anyway.
type remoteWatcher struct {
	uri     string
	changed chan watcherChange
	done    chan struct{}
	enabled atomic.Bool

	mu    sync.Mutex
	known remote.Info
}

func newRemoteWatcher(info remote.Info, enabled bool) *remoteWatcher {
	w := &remoteWatcher{uri: info.URI, known: info, changed: make(chan watcherChange, 1), done: make(chan struct{})}
	w.enabled.Store(enabled)
	go w.loop()
	return w
}

// setKnown records info as the file's version, as after our own save, and
// drops a change already seen, which may have been that save.
func (w *remoteWatcher) setKnown(info remote.Info) {
	w.mu.Lock()
	w.known = info
	select {
	case <-w.changed:
	default:
	}
	w.mu.Unlock()
}

func (w *remoteWatcher) loop() {
	ticker := time.NewTicker(remote.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
		}
		if !w.enabled.Load() {
			continue
		}
		now, connected, err := remote.Changed(w.uri)
		if !connected || err != nil {
			continue
		}
		// Under the lock, so setKnown cannot fall between the comparison
		// and the report.
		w.mu.Lock()
		if !now.SameVersion(w.known) {
			w.known = now
			select {
			case <-w.changed: // a newer change replaces one not yet taken
			default:
			}
			w.changed <- watcherChange{mod: now.ModTime, size: now.Size}
		}
		w.mu.Unlock()
	}
}

func (w *remoteWatcher) waitForChange(time.Duration) (watcherChange, bool) {
	select {
	case change := <-w.changed:
		return change, true
	default:
		return watcherChange{}, false
	}
}

func (w *remoteWatcher) Close() {
	close(w.done)
}
