package remote

import "os"

// holdFile needs no lock: Windows refuses to remove a file that is open,
// so keeping f open holds it.
func holdFile(*os.File) error { return nil }

// lockForRemoval needs no lock either: removing a file held open fails by
// itself.
func lockForRemoval(string) (unlock func(), ok bool) { return func() {}, true }
