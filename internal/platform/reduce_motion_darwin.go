//go:build darwin

package platform

/*
int ononReduceMotion(void);
*/
import "C"

// ReducesMotion reports whether Reduce motion is on in macOS's accessibility
// settings.
func ReducesMotion() bool { return C.ononReduceMotion() != 0 }
