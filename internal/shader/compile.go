package shader

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Program is a theme's shader compiled for a GPU: its code in the form
// the GPU takes, the function it starts at, and what it reads.
type Program struct {
	Code  []byte
	Entry string
	Usage Usage
}

// build identifies the onon binary running, by its size and when it was
// written, as what a program was compiled by: a program cached by any
// other build, older or newer, is compiled again, so a change to how
// shaders compile needs nothing done to the cache. "" when it cannot be
// told, and then nothing is cached.
var build = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	info, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d/%d", info.Size(), info.ModTime().UnixNano())
})

// cacheMaxAge is how long a program not used is kept in the cache.
const cacheMaxAge = 30 * 24 * time.Hour

// CacheDir is where compiled programs are kept, so a shader compiles once
// rather than on every start. Tests point it elsewhere; "" keeps none.
var CacheDir = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "onon", "shaders")
}

// compiling lets one shader compile at a time, however many windows ask.
var compiling sync.Mutex

// Compile compiles source, a theme's shader, to format, from the cache
// when it was compiled before. It may take a while, so it is called off
// the thread that draws.
func Compile(source string, format Format) (Program, error) {
	compiling.Lock()
	defer compiling.Unlock()
	// Direct3D's compiler is a COM object, kept to the thread it was
	// made on.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	wrapped := Wrap(source)
	key := cacheKey(build(), wrapped, format)
	if p, ok := loadCached(key); ok {
		return p, nil
	}
	spirv, err := SPIRV(wrapped)
	if err != nil {
		return Program{}, err
	}
	code, entry, err := Translate(spirv, format)
	if err != nil {
		return Program{}, err
	}
	p := Program{Code: append([]byte(nil), code...), Entry: entry, Usage: Reflect(spirv)}
	storeCached(key, p)
	return p, nil
}

// cacheKey is the name a program is cached under, "" for none.
func cacheKey(build, wrapped string, format Format) string {
	if build == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(build))
	h.Write([]byte{0, byte(format), 0})
	h.Write([]byte(runtime.GOOS + "/" + runtime.GOARCH + "\x00"))
	h.Write([]byte(wrapped))
	return hex.EncodeToString(h.Sum(nil))
}

// A cached program is its magic, its usage, its entry's length and entry,
// then its code.
const cacheMagic = "ONSH"

const (
	usageTime = 1 << iota
	usageBackdrop
)

func loadCached(key string) (Program, bool) {
	dir := CacheDir()
	if dir == "" || key == "" {
		return Program{}, false
	}
	path := filepath.Join(dir, key)
	data, err := os.ReadFile(path)
	if err != nil || len(data) < len(cacheMagic)+3 || string(data[:len(cacheMagic)]) != cacheMagic {
		return Program{}, false
	}
	data = data[len(cacheMagic):]
	flags, n := data[0], int(binary.LittleEndian.Uint16(data[1:3]))
	data = data[3:]
	if len(data) <= n {
		return Program{}, false
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now) // kept while it is used
	return Program{
		Code:  data[n:],
		Entry: string(data[:n]),
		Usage: Usage{Time: flags&usageTime != 0, Backdrop: flags&usageBackdrop != 0},
	}, true
}

func storeCached(key string, p Program) {
	dir := CacheDir()
	if dir == "" || key == "" || len(p.Entry) > 0xffff {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	pruneCache(dir)
	var flags byte
	if p.Usage.Time {
		flags |= usageTime
	}
	if p.Usage.Backdrop {
		flags |= usageBackdrop
	}
	var b bytes.Buffer
	b.WriteString(cacheMagic)
	b.WriteByte(flags)
	b.Write(binary.LittleEndian.AppendUint16(nil, uint16(len(p.Entry))))
	b.WriteString(p.Entry)
	b.Write(p.Code)
	tmp, err := os.CreateTemp(dir, key+".*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b.Bytes())
	if cerr := tmp.Close(); werr != nil || cerr != nil || os.Rename(tmp.Name(), filepath.Join(dir, key)) != nil {
		os.Remove(tmp.Name())
	}
}

// pruneCache removes programs not used for cacheMaxAge, as a shader being
// written leaves one for each version of it.
func pruneCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		stale := time.Since(info.ModTime()) > cacheMaxAge
		// A temporary file left by a write that never finished.
		abandoned := strings.HasSuffix(e.Name(), ".tmp") && time.Since(info.ModTime()) > time.Hour
		if stale || abandoned {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
