package remote

import "os"

// holdFile needs no lock: Windows refuses to remove a file that is open,
// so keeping f open holds it.
func holdFile(*os.File) error { return nil }

// held is false: removing a file held open fails by itself.
func held(string) bool { return false }
