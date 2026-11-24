//go:build !darwin && !linux && !windows

package platform

// NoteRecentDocument adds path to the system's recent documents, on systems
// onon keeps them for.
func NoteRecentDocument(path string) {}
