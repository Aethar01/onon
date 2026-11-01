//go:build !darwin

package viewer

// watchKeyModifiers corrects SDL's key modifiers on macOS; elsewhere SDL's
// are right.
func watchKeyModifiers() {}
