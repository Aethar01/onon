//go:build !darwin

package platform

// WatchKeyModifiers corrects SDL's key modifiers on macOS; elsewhere SDL's
// are right.
func WatchKeyModifiers() {}
