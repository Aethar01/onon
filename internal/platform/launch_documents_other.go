//go:build !darwin

package platform

// LaunchDocuments returns the documents the system launched onon to open
// without passing them as arguments. Only macOS does that.
func LaunchDocuments(initSDL func() error, verbose bool) []string {
	return nil
}
