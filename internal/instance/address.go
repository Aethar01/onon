package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"onon/internal/remote"
)

// AddressFor returns the socket path for one document. Instances are per
// document, so opening a file that is already on screen reaches that window
// rather than any other.
//
// The name is a hash of the document path rather than the path itself: socket
// paths are capped at 104 bytes on macOS and 108 on Linux, which a real
// document path would often exceed.
func AddressFor(docPath string) (string, error) {
	if docPath == "" {
		return "", errors.New("no document to identify an instance by")
	}
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("no per-user runtime directory is available")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, documentKey(docPath)+".sock"), nil
}

// documentKey identifies a document across processes. Symlinks are resolved so
// two names for one file share an instance; an unresolvable path is used as
// given, which at worst means a separate instance. A remote URI is already
// canonical.
func documentKey(docPath string) string {
	if remote.IsURI(docPath) {
		sum := sha256.Sum256([]byte(docPath))
		return hex.EncodeToString(sum[:8])
	}
	canonical := docPath
	if absolute, err := filepath.Abs(canonical); err == nil {
		canonical = absolute
	}
	if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
		canonical = resolved
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		// These platforms are conventionally case-insensitive, so two spellings
		// of one path must not become two instances.
		canonical = strings.ToLower(canonical)
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:8])
}

func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	// A leftover socket file also reports "bind: address already in use" on
	// some platforms and a plain file-exists error on others.
	if errors.Is(err, os.ErrExist) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		message := strings.ToLower(opErr.Err.Error())
		return strings.Contains(message, "in use") ||
			strings.Contains(message, "exists") ||
			strings.Contains(message, "only one usage")
	}
	return false
}
