package remote

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Info describes a fetched copy of a remote document.
type Info struct {
	URI   string // canonical, and after any fallback to the home directory
	Local string // the cached copy the viewer reads

	// What the copy was fetched from, to tell later whether it changed.
	ModTime      time.Time
	Size         int64
	ETag         string
	LastModified string
}

// SameVersion reports whether two infos describe the same remote file: by
// ETag when a web server gave both, else by size and modification time, to
// the second, which is all SFTP gives.
func (i Info) SameVersion(other Info) bool {
	if i.ETag != "" && other.ETag != "" {
		return i.ETag == other.ETag
	}
	return i.Size == other.Size && i.ModTime.Unix() == other.ModTime.Unix()
}

// cacheMaxAge is how long a copy that has not been opened is kept.
const cacheMaxAge = 30 * 24 * time.Hour

// CacheDir is where fetched copies are kept. Tests point it elsewhere.
var CacheDir = func() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "onon", "remote"), nil
}

// cacheEntry is one document's directory in the cache. The copy sits in a
// subdirectory per version, so fetching a new version never writes over a
// file the viewer still has open, which Windows would refuse.
type cacheEntry struct {
	dir string
}

type cacheMeta struct {
	URI          string    `json:"uri"`
	File         string    `json:"file"` // relative to the entry
	ModTime      time.Time `json:"mod_time"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
}

var pruneOnce sync.Once

func entryFor(uri string) (cacheEntry, error) {
	root, err := CacheDir()
	if err != nil {
		return cacheEntry{}, fmt.Errorf("cache directory: %w", err)
	}
	pruneOnce.Do(func() { go pruneCache(root, time.Now()) })
	sum := sha256.Sum256([]byte(uri))
	return cacheEntry{dir: filepath.Join(root, hex.EncodeToString(sum[:12]))}, nil
}

// cached returns the copy recorded for the entry, if its file is still there.
func (e cacheEntry) cached() (Info, bool) {
	data, err := os.ReadFile(filepath.Join(e.dir, "meta.json"))
	if err != nil {
		return Info{}, false
	}
	var meta cacheMeta
	if json.Unmarshal(data, &meta) != nil || meta.File == "" {
		return Info{}, false
	}
	local := filepath.Join(e.dir, filepath.FromSlash(meta.File))
	if _, err := os.Stat(local); err != nil {
		return Info{}, false
	}
	now := time.Now()
	_ = os.Chtimes(filepath.Join(e.dir, "meta.json"), now, now) // last used
	return Info{URI: meta.URI, Local: local, ModTime: meta.ModTime, Size: meta.Size, ETag: meta.ETag, LastModified: meta.LastModified}, true
}

// forget drops the record of the entry's copy, which stays on disk for the
// viewer that has it open. meta.json stays, without a file, so pruneCache
// does not take the entry for a fetch that never finished.
func (e cacheEntry) forget() {
	_ = e.writeMeta(cacheMeta{})
}

// record notes info as the entry's copy.
func (e cacheEntry) record(info Info) error {
	rel, err := filepath.Rel(e.dir, info.Local)
	if err != nil {
		return err
	}
	return e.writeMeta(cacheMeta{
		URI: info.URI, File: filepath.ToSlash(rel), ModTime: info.ModTime, Size: info.Size,
		ETag: info.ETag, LastModified: info.LastModified,
	})
}

func (e cacheEntry) writeMeta(meta cacheMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(e.dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(e.dir, "meta.json"))
}

// store writes a new version of the document called name from write, and
// records it. Older versions are removed unless a viewer holds them.
func (e cacheEntry) store(info Info, name string, write func(io.Writer) error) (Info, error) {
	version := strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := filepath.Join(e.dir, version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Info{}, err
	}
	local := filepath.Join(dir, safeName(name))
	part := local + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		os.RemoveAll(dir)
		return Info{}, err
	}
	err = write(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part, local)
	}
	var st os.FileInfo
	if err == nil {
		st, err = os.Stat(local)
	}
	if err != nil {
		os.RemoveAll(dir)
		return Info{}, err
	}
	// What arrived, which a reply sent in chunks gives no length for.
	info.Local, info.Size = local, st.Size()
	if err := e.record(info); err != nil {
		os.RemoveAll(dir)
		return Info{}, err
	}
	e.removeVersionsExcept(version)
	return info, nil
}

func (e cacheEntry) removeVersionsExcept(keep string) {
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != keep {
			removeVersion(filepath.Join(e.dir, entry.Name()))
		}
	}
}

// removeVersion removes a version's directory unless a viewer, in this
// process or another, holds its copy.
func removeVersion(dir string) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, f := range files {
		if held(filepath.Join(dir, f.Name())) {
			return
		}
	}
	_ = os.RemoveAll(dir)
}

// contains reports whether local is a copy in the entry, not another file.
func (e cacheEntry) contains(local string) bool {
	return filepath.Dir(filepath.Dir(local)) == e.dir
}

// Hold marks local, a copy Fetch returned, as open in a viewer until release
// is called, so a newer version fetched meanwhile, here or by another
// process, leaves it in place: the viewer reads it, and saves edits into it.
// Release removes it once it is no longer the entry's current copy.
func Hold(local string) (release func()) {
	root, err := CacheDir()
	if err != nil {
		return func() {}
	}
	entry := cacheEntry{dir: filepath.Dir(filepath.Dir(local))}
	if filepath.Dir(entry.dir) != filepath.Clean(root) {
		return func() {}
	}
	f, err := os.Open(local)
	if err != nil {
		return func() {}
	}
	if err := holdFile(f); err != nil {
		f.Close()
		return func() {}
	}
	return func() {
		f.Close()
		if cached, ok := entry.cached(); !ok || cached.Local != local {
			removeVersion(filepath.Dir(local))
		}
	}
}

// safeName keeps a remote file name usable as a local one, keeping its
// extension, which MuPDF goes by to pick a format.
func safeName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < ' ' || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" {
		return "document"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		// On a character boundary: APFS refuses a name that is not UTF-8.
		cut := 200 - len(ext)
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut] + ext
	}
	return name
}

// pruneCache removes copies not opened within cacheMaxAge.
func pruneCache(root string, now time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		info, err := os.Stat(filepath.Join(dir, "meta.json"))
		if errors.Is(err, os.ErrNotExist) {
			// A fetch that never finished; leave one still in progress.
			if di, err := entry.Info(); err == nil && now.Sub(di.ModTime()) > time.Hour {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		if err == nil && now.Sub(info.ModTime()) > cacheMaxAge {
			_ = os.RemoveAll(dir)
		}
	}
}

// progressWriter reports the bytes written through it.
type progressWriter struct {
	w        io.Writer
	done     int64
	total    int64
	progress func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.progress != nil {
		p.progress(p.done, p.total)
	}
	return n, err
}
