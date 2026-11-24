//go:build !windows

package platform

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

// LoadSDLSymbol points dst, a pointer to a func variable, at the SDL function
// name, for functions the SDL binding leaves out.
func LoadSDLSymbol(name string, dst any) error {
	var lastErr error
	for _, library := range sdlLibraryNames() {
		handle, err := purego.Dlopen(library, 1)
		if err != nil {
			lastErr = err
			continue
		}
		sym, err := purego.Dlsym(handle, name)
		if err != nil {
			lastErr = err
			continue
		}
		purego.RegisterFunc(dst, sym)
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("symbol not found")
}

func sdlLibraryNames() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"./libSDL3.dylib", "libSDL3.dylib"}
	default:
		return []string{"./libSDL3.so.0", "libSDL3.so.0"}
	}
}
