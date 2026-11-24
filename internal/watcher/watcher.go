package watcher

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"onon/internal/remote"

	"github.com/fsnotify/fsnotify"
)

// Change describes an observed file modification.
type Change struct {
	Mod  time.Time
	Size int64
}

// Document watches a local document file for modifications, debouncing
// writes until the file stays unchanged for the configured delay.
type Document struct {
	path    string
	mod     time.Time
	size    int64
	changed chan struct{}
	pending Change
	delay   atomic.Int64 // the time.Duration writes must settle for

	watcher   *fsnotify.Watcher
	done      chan struct{}
	mu        sync.Mutex
	closeOnce sync.Once
	started   bool
}

// NewDocument creates a watcher for path's directory with the given debounce delay.
func NewDocument(path string, delay time.Duration) (*Document, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dw := &Document{
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

// SetDelay updates how long writes must settle before a change is reported.
func (dw *Document) SetDelay(delay time.Duration) {
	dw.delay.Store(int64(delay))
}

// Record updates the baseline stat for path and starts the watcher loop if not yet running.
func (dw *Document) Record(path string) {
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

func (dw *Document) loop() {
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

func (dw *Document) isRelevantEvent(event fsnotify.Event) bool {
	return event.Has(fsnotify.Write) || event.Has(fsnotify.Chmod) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename)
}

func (dw *Document) checkAndNotify() {
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
	dw.pending = Change{
		Mod:  mod,
		Size: size,
	}

	select {
	case dw.changed <- struct{}{}:
	default:
	}
}

// WaitForChange returns any pending change, waiting up to timeout when positive.
func (dw *Document) WaitForChange(timeout time.Duration) (Change, bool) {
	if timeout <= 0 {
		select {
		case <-dw.changed:
			dw.mu.Lock()
			change := dw.pending
			dw.pending = Change{}
			dw.mu.Unlock()
			return change, true
		default:
			return Change{}, false
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-dw.changed:
		dw.mu.Lock()
		change := dw.pending
		dw.pending = Change{}
		dw.mu.Unlock()
		return change, true
	case <-timer.C:
		return Change{}, false
	}
}

// Close stops watching and releases underlying resources.
func (dw *Document) Close() {
	dw.closeOnce.Do(func() {
		close(dw.done)
		dw.watcher.Close()
	})
}

// Remote checks a document on an SSH host or WebDAV server for changes every
// remote.PollInterval. It checks only while enabled, which follows auto_reload,
// since a change would wait unread anyway.
type Remote struct {
	uri     string
	changed chan Change
	done    chan struct{}
	enabled atomic.Bool

	mu    sync.Mutex
	known remote.Info
	saves int // SetKnown calls, to tell a check made across one
}

// NewRemote starts polling a remote document for version changes.
func NewRemote(info remote.Info, enabled bool) *Remote {
	w := &Remote{uri: info.URI, known: info, changed: make(chan Change, 1), done: make(chan struct{})}
	w.enabled.Store(enabled)
	go w.loop()
	return w
}

// URI returns the watched remote URI.
func (w *Remote) URI() string { return w.uri }

// SetEnabled turns remote polling on or off.
func (w *Remote) SetEnabled(on bool) { w.enabled.Store(on) }

// SetKnown records info as the file's version, as after our own save, and
// drops a change already seen, which may have been that save.
func (w *Remote) SetKnown(info remote.Info) {
	w.mu.Lock()
	w.known = info
	w.saves++
	select {
	case <-w.changed:
	default:
	}
	w.mu.Unlock()
}

func (w *Remote) loop() {
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
		w.mu.Lock()
		saves := w.saves
		w.mu.Unlock()
		now, connected, err := remote.Changed(w.uri)
		if !connected || err != nil {
			continue
		}
		// Under the lock, so SetKnown cannot fall between the comparison
		// and the report. A check that began before our own save may have
		// seen the file from before it, so is dropped.
		w.mu.Lock()
		if w.saves == saves && !now.SameVersion(w.known) {
			w.known = now
			select {
			case <-w.changed: // a newer change replaces one not yet taken
			default:
			}
			w.changed <- Change{Mod: now.ModTime, Size: now.Size}
		}
		w.mu.Unlock()
	}
}

// WaitForChange returns any pending remote change without blocking.
func (w *Remote) WaitForChange(time.Duration) (Change, bool) {
	select {
	case change := <-w.changed:
		return change, true
	default:
		return Change{}, false
	}
}

// Close stops polling.
func (w *Remote) Close() {
	close(w.done)
}
