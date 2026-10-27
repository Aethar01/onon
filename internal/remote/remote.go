package remote

import (
	"context"
	"time"
)

// PollInterval is how often an open SSH document is checked for changes.
const PollInterval = 5 * time.Second

// Fetch makes sure there is a current copy of the document at uri in the
// cache and returns it, reporting download progress as it goes; total is -1
// when the size is not known. A copy that is still current is not fetched
// again.
func Fetch(ctx context.Context, uri string, progress func(done, total int64)) (Info, error) {
	takeURLLogin(uri)
	t, err := parseTarget(uri)
	if err != nil {
		return Info{}, err
	}
	if t.url.Scheme == "sftp" {
		return fetchSFTP(ctx, t, progress)
	}
	uri = t.url.String()
	entry, err := entryFor(uri)
	if err != nil {
		return Info{}, err
	}
	return fetchHTTP(ctx, uri, entry, progress)
}

// Changed reports how the document at uri is now, without connecting: an
// SSH host already connected is asked, anything else reports ok false.
func Changed(uri string) (Info, bool, error) {
	if !IsSFTP(uri) {
		return Info{}, false, nil
	}
	t, err := parseTarget(uri)
	if err != nil {
		return Info{}, false, err
	}
	return statSFTP(t)
}

// Upload saves the local copy over the document at uri, refusing with
// ErrConflict if the remote file is no longer the version opened unless
// force is set. The cache then records the copy as the current version.
func Upload(ctx context.Context, uri, local string, opened Info, force bool) (Info, error) {
	if !Writable(uri) {
		return Info{}, ErrReadOnly
	}
	t, err := parseTarget(uri)
	if err != nil {
		return Info{}, err
	}
	info, err := uploadSFTP(ctx, t, local, opened, force)
	entry, entryErr := entryFor(t.url.String())
	if entryErr != nil {
		return info, err
	}
	cached, ok := entry.cached()
	if !ok || cached.Local != local {
		return info, err
	}
	if err != nil {
		// The copy holds edits the remote file does not, so it is no longer
		// that version; the next open fetches it again.
		entry.forget()
		return Info{}, err
	}
	_ = entry.record(info)
	return info, nil
}
