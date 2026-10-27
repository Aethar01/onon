//go:build !windows

package remote

import (
	"errors"
	"os"
	"syscall"
)

// holdFile takes a shared lock on f, which lasts until f is closed or the
// process ends.
func holdFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
}

// held reports whether a viewer holds the file at path: an exclusive lock
// cannot be had while anyone, this process included, has a shared one.
func held(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	return errors.Is(syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB), syscall.EWOULDBLOCK)
}
