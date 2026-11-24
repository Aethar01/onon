package watcher

import (
	"testing"

	"onon/internal/remote"
)

func TestRemoteWatcherForgetsOurOwnSave(t *testing.T) {
	w := NewRemote(remote.Info{URI: "sftp://host/~/a.pdf"}, false)
	defer w.Close()
	w.changed <- Change{Size: 1} // seen before our save was recorded
	w.SetKnown(remote.Info{URI: "sftp://host/~/a.pdf", Size: 1})
	if _, ok := w.WaitForChange(0); ok {
		t.Fatal("our own save was reported as a change")
	}
}
