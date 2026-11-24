//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procSHAddToRecentDocs = windows.NewLazySystemDLL("shell32.dll").NewProc("SHAddToRecentDocs")

const shardPathW = 3 // SHARD_PATHW

// NoteRecentDocument adds path to the recent documents Windows lists in
// onon's jump list and in Quick Access.
func NoteRecentDocument(path string) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	procSHAddToRecentDocs.Call(shardPathW, uintptr(unsafe.Pointer(name)))
}
