//go:build !windows

package remote

import (
	"os"
	"syscall"
)

// holdFile takes a shared lock on f, which lasts until f is closed or the
// process ends. It waits out removeVersion's brief check of the file.
func holdFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_SH)
}

// lockForRemoval takes an exclusive lock on the file at path, which cannot
// be had while a viewer, in this process or another, holds it. ok is false
// when one does.
func lockForRemoval(path string) (unlock func(), ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return func() {}, true // gone already, or not ours to hold
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		f.Close()
		return nil, false
	}
	return func() { f.Close() }, true
}
