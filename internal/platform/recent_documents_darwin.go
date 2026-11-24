//go:build darwin

package platform

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void ononNoteRecentDocument(const char *path);
*/
import "C"

import "unsafe"

// NoteRecentDocument adds path to the recent documents macOS lists in the
// Dock menu and the menu bar's Open Recent.
func NoteRecentDocument(path string) {
	text := C.CString(path)
	defer C.free(unsafe.Pointer(text))
	C.ononNoteRecentDocument(text)
}
